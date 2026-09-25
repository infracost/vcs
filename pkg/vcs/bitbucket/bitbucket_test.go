package bitbucket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/infracost/go-proto/pkg/diagnostic"
	"github.com/infracost/go-proto/pkg/rat"
	parserpb "github.com/infracost/proto/gen/go/infracost/parser"
	"github.com/infracost/proto/gen/go/infracost/provider"
	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

// compile-time check that Bitbucket implements vcs.VCS.
var _ vcs.VCS = (*Bitbucket)(nil)

func TestNewRejectsMalformedRepo(t *testing.T) {
	for _, repo := range []string{"", "repo", "workspace/repo/extra", "/repo", "workspace/"} {
		if _, err := New(context.Background(), repo, "token", 1, Options{}); err == nil {
			t.Errorf("New(%q) = nil error, want error", repo)
		}
	}
}

func TestSourceLink(t *testing.T) {
	tests := []struct {
		name      string
		isServer  bool
		repoURL   string
		commitSHA string
		path      string
		line      int
		want      string
	}{
		{
			name:      "cloud",
			repoURL:   "https://bitbucket.org/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://bitbucket.org/my-org/my-repo/src/abc123/main.tf#lines-42",
		},
		{
			name:      "cloud no line",
			repoURL:   "https://bitbucket.org/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			want:      "https://bitbucket.org/my-org/my-repo/src/abc123/main.tf",
		},
		{
			name:      "cloud strips .git suffix",
			repoURL:   "https://bitbucket.org/my-org/my-repo.git",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      7,
			want:      "https://bitbucket.org/my-org/my-repo/src/abc123/main.tf#lines-7",
		},
		{
			name:      "server",
			isServer:  true,
			repoURL:   "https://bb.corp/projects/PROJ/repos/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://bb.corp/projects/PROJ/repos/my-repo/browse/main.tf?at=abc123#42",
		},
		{
			name:      "cloud escapes the path",
			repoURL:   "https://bitbucket.org/my-org/my-repo",
			commitSHA: "abc123",
			path:      "envs/prod (eu)/main#1.tf",
			line:      3,
			want:      "https://bitbucket.org/my-org/my-repo/src/abc123/envs/prod%20%28eu%29/main%231.tf#lines-3",
		},
		{
			// A "?" in the path would otherwise swallow the revision.
			name:      "server escapes the path",
			isServer:  true,
			repoURL:   "https://bb.corp/projects/PROJ/repos/my-repo",
			commitSHA: "abc123",
			path:      "envs/prod?/main.tf",
			want:      "https://bb.corp/projects/PROJ/repos/my-repo/browse/envs/prod%3F/main.tf?at=abc123",
		},
		{
			name:      "empty repoURL",
			commitSHA: "abc123",
			path:      "main.tf",
			want:      "",
		},
		{
			name:    "empty commitSHA",
			repoURL: "https://bitbucket.org/x/y",
			path:    "main.tf",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Bitbucket{isServer: tt.isServer}
			if got := b.SourceLink(tt.repoURL, tt.commitSHA, tt.path, tt.line); got != tt.want {
				t.Errorf("SourceLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGenerateCommentIsFlat guards the reason this provider exists: the default
// template's HTML shows as literal text on Bitbucket.
func TestGenerateCommentIsFlat(t *testing.T) {
	b, err := New(context.Background(), "my-org/my-repo", "token", 1, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	body, err := b.GenerateComment(commentData())
	if err != nil {
		t.Fatalf("GenerateComment() error = %v", err)
	}

	for _, tag := range []string{"<details", "<summary", "<table", "<td", "<tr", "<h3", "<h4", "<sub", "<span", "<p>", "<b", "<code", "<ul", "<li", "<pre", "<hr"} {
		if strings.Contains(body, tag) {
			t.Errorf("GenerateComment() contains raw HTML %q:\n%s", tag, body)
		}
	}

	// The sections the tags above would have come from have to be in the body,
	// or the check above passes by rendering nothing.
	for _, want := range []string{"Infracost report", "Use reserved instances", "Cost changes & budgets", "New Project Errors"} {
		if !strings.Contains(body, want) {
			t.Errorf("GenerateComment() missing %q:\n%s", want, body)
		}
	}
}

func TestPostCommentTagsWithVisibleFooter(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, http.StatusOK, map[string]any{"values": []any{}})
		case http.MethodPost:
			posted = readRaw(t, r)
			writeJSON(t, w, http.StatusCreated, cloudComment{ID: 1})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	b := cloudAt(t, srv.URL)
	res, err := b.PostComment(context.Background(), "body", vcs.BehaviorUpdate)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if !res.Posted {
		t.Errorf("PostComment() Posted = false, want true")
	}

	// The hidden markdown form would render as text on Bitbucket.
	if strings.Contains(posted, "[//]:") {
		t.Errorf("comment carries a hidden markdown tag: %q", posted)
	}
	if !vcs.HasFooterTagKey(posted, "infracost-comment") {
		t.Errorf("comment is missing its footer tag: %q", posted)
	}
	if vcs.ExtractFooterValidAt(posted) == nil {
		t.Errorf("comment is missing its valid-at footer tag: %q", posted)
	}
}

func TestPostCommentUpdatesMatchingComment(t *testing.T) {
	var updatedURL, updatedBody string
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			existing := cloudComment{ID: 7}
			existing.Content.Raw = vcs.AddFooterTags("old", "infracost-comment", nil)
			other := cloudComment{ID: 8}
			other.Content.Raw = "someone else's comment"
			writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{existing, other}})
		case http.MethodPut:
			updatedURL, updatedBody = r.URL.Path, readRaw(t, r)
			writeJSON(t, w, http.StatusOK, cloudComment{ID: 7})
		case http.MethodPost:
			created = true
			writeJSON(t, w, http.StatusCreated, cloudComment{ID: 9})
		}
	}))
	defer srv.Close()

	if _, err := cloudAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorUpdate); err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}

	if created {
		t.Errorf("PostComment() created a comment instead of updating")
	}
	if updatedURL != "/pullrequests/1/comments/7" {
		t.Errorf("updated %q, want the matching comment's own URL", updatedURL)
	}
	if !strings.HasPrefix(updatedBody, "new") {
		t.Errorf("updated body = %q, want the new body", updatedBody)
	}
}

func TestPostCommentSkipsIdenticalComment(t *testing.T) {
	// Identical means identical including the valid-at footer. PostComment
	// stamps that from the clock, so drive updateComment with a fixed time
	// instead: otherwise the test only passes while both stamps land in the
	// same second.
	validAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	body := vcs.AddFooterTags("same", "infracost-comment", &validAt)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s, want no mutation", r.Method)
		}
		existing := cloudComment{ID: 7}
		existing.Content.Raw = body
		writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{existing}})
	}))
	defer srv.Close()

	res, err := cloudAt(t, srv.URL).updateComment(context.Background(), body, &validAt)
	if err != nil {
		t.Fatalf("updateComment() error = %v", err)
	}
	if res.Posted || !strings.Contains(res.SkipReason, "matches exactly") {
		t.Errorf("updateComment() = %+v, want an identical-content skip", res)
	}
	if res.Body != body || res.URL != srv.URL+"/pullrequests/1/comments/7" {
		t.Errorf("updateComment() result = %+v, want the existing comment", res)
	}
}

func TestPostCommentSkipsNewerComment(t *testing.T) {
	// A concurrent run's comment carries a later valid-at; ours must not
	// overwrite it with staler numbers.
	newer := time.Now().Add(time.Minute)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s, want no mutation", r.Method)
		}
		existing := cloudComment{ID: 7}
		existing.Content.Raw = vcs.AddFooterTags("newer", "infracost-comment", &newer)
		writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{existing}})
	}))
	defer srv.Close()

	res, err := cloudAt(t, srv.URL).PostComment(context.Background(), "mine", vcs.BehaviorUpdate)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if res.Posted || !strings.Contains(res.SkipReason, "is newer") {
		t.Errorf("PostComment() = %+v, want a newer-comment skip", res)
	}
}

// The footer tag is visible text, so anyone can post a copy of it. A valid-at
// far in the future must not stop us posting for good.
func TestPostCommentIgnoresFarFutureValidAt(t *testing.T) {
	forged := time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)

	var updated bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			existing := cloudComment{ID: 7}
			existing.Content.Raw = vcs.AddFooterTags("lgtm, merging", "infracost-comment", &forged)
			writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{existing}})
		case http.MethodPut:
			updated = true
			writeJSON(t, w, http.StatusOK, cloudComment{ID: 7})
		}
	}))
	defer srv.Close()

	res, err := cloudAt(t, srv.URL).PostComment(context.Background(), "mine", vcs.BehaviorUpdate)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if !res.Posted || !updated {
		t.Errorf("PostComment() = %+v, want the forged valid-at ignored", res)
	}
}

func TestDeleteAndNewDeletesEveryMatch(t *testing.T) {
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			var values []cloudComment
			for _, id := range []int64{1, 2} {
				c := cloudComment{ID: id}
				c.Content.Raw = vcs.AddFooterTags("old", "infracost-comment", nil)
				values = append(values, c)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"values": values})
		case http.MethodDelete:
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			writeJSON(t, w, http.StatusCreated, cloudComment{ID: 3})
		}
	}))
	defer srv.Close()

	// hide-and-new has no Bitbucket equivalent and must degrade, not error.
	if _, err := cloudAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorHideAndNew); err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}

	if len(deleted) != 2 {
		t.Errorf("deleted %v, want both matching comments", deleted)
	}
}

// A delete that fails is not fatal: the new report still has to land, or a
// concurrent run that deleted the same comment first leaves the PR with none.
func TestDeleteAndNewSurvivesFailedDelete(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			var values []cloudComment
			for _, id := range []int64{1, 2} {
				c := cloudComment{ID: id}
				c.Content.Raw = vcs.AddFooterTags("old", "infracost-comment", nil)
				values = append(values, c)
			}
			writeJSON(t, w, http.StatusOK, map[string]any{"values": values})
		case http.MethodDelete:
			// The first comment is already gone, as if another run removed it.
			if strings.HasSuffix(r.URL.Path, "/1") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPost:
			created = true
			writeJSON(t, w, http.StatusCreated, cloudComment{ID: 3})
		}
	}))
	defer srv.Close()

	res, err := cloudAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorDeleteAndNew)
	if err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if !res.Posted || !created {
		t.Errorf("PostComment() = %+v, want the new comment posted anyway", res)
	}
}

func TestNewRejectsUnreadableTag(t *testing.T) {
	for _, tag := range []string{"infracost (pr)", "team-a,team-b", "infra*cost", "infracost\ncomment"} {
		if _, err := New(context.Background(), "org/repo", "token", 1, Options{Tag: tag}); err == nil {
			t.Errorf("New(tag=%q) = nil error, want the unreadable tag rejected", tag)
		}
	}
}

func TestPostCommentUnknownBehavior(t *testing.T) {
	b, err := New(context.Background(), "my-org/my-repo", "token", 1, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := b.PostComment(context.Background(), "body", vcs.Behavior("nonsense")); err == nil {
		t.Error("PostComment() = nil error, want error")
	}
}

func TestServerPostComment(t *testing.T) {
	var createdBody string
	var updatePayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/activities"):
			tagged := vcs.AddFooterTags("old", "infracost-comment", nil)
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{
					// Inline comment: anchored, so not ours to update.
					{"action": "COMMENTED", "commentAction": "ADDED", "commentAnchor": map[string]any{},
						"comment": serverComment{ID: 1, Text: tagged, Version: 3}},
					{"action": "COMMENTED", "commentAction": "ADDED",
						"comment": serverComment{ID: 2, Text: tagged, Version: 5}},
				},
				"isLastPage": true,
			})
		case r.Method == http.MethodPut:
			updatePayload = readJSON(t, r)
			writeJSON(t, w, http.StatusOK, serverComment{ID: 2, Version: 6})
		case r.Method == http.MethodPost:
			createdBody = readRaw(t, r)
			writeJSON(t, w, http.StatusCreated, serverComment{ID: 9})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	b, err := New(context.Background(), "PROJ/my-repo", "user:pass", 42, Options{ServerURL: srv.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := b.PostComment(context.Background(), "new", vcs.BehaviorUpdate); err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}

	if createdBody != "" {
		t.Errorf("created a comment instead of updating the anchored-free match")
	}
	if got := updatePayload["version"]; got != float64(5) {
		t.Errorf("update version = %v, want 5 from the matched comment", got)
	}
}

func TestServerURLSelectsFlavour(t *testing.T) {
	tests := []struct {
		serverURL string
		isServer  bool
	}{
		{"", false},
		{"https://bitbucket.org", false},
		{"https://bitbucket.org/", false},
		{"https://BITBUCKET.org", false},
		{"http://bitbucket.org", false},
		{"https://www.bitbucket.org", false},
		{"https://bitbucket.org/my-org", false},
		{"https://api.bitbucket.org/2.0", false},
		{"bitbucket.org", false},
		{"https://bb.corp", true},
		{"https://bitbucket.org.evil.example", true},
	}

	for _, tt := range tests {
		t.Run(tt.serverURL, func(t *testing.T) {
			b, err := New(context.Background(), "org/repo", "token", 1, Options{ServerURL: tt.serverURL})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if b.isServer != tt.isServer {
				t.Errorf("isServer = %v, want %v", b.isServer, tt.isServer)
			}
		})
	}
}

func TestNewEscapesRepoPath(t *testing.T) {
	b, err := New(context.Background(), "org/repo?visibility=all", "token", 1, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	baseURL := b.api.(*cloudAPI).baseURL
	if strings.Contains(baseURL, "repo?") {
		t.Errorf("baseURL = %q, want the repo segment escaped", baseURL)
	}
	if !strings.Contains(baseURL, "repo%3Fvisibility") {
		t.Errorf("baseURL = %q, want the repo segment escaped", baseURL)
	}
}

func TestCloudFindMatchingCommentsFollowsPages(t *testing.T) {
	var nextURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tagged := vcs.AddFooterTags("old", "infracost-comment", nil)

		if r.URL.Path == "/page2" {
			second := cloudComment{ID: 1}
			second.Content.Raw = tagged
			writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{second}})
			return
		}

		first := cloudComment{ID: 2}
		first.Content.Raw = tagged
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values": []cloudComment{first},
			"next":   nextURL,
		})
	}))
	defer srv.Close()
	nextURL = srv.URL + "/page2"

	comments, err := cloudAt(t, srv.URL).api.findMatchingComments(context.Background())
	if err != nil {
		t.Fatalf("findMatchingComments() error = %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("findMatchingComments() = %d comments, want both pages", len(comments))
	}
	// The post loop takes the last element as the latest comment.
	if comments[len(comments)-1].id != 2 {
		t.Errorf("last comment id = %d, want the newest (2)", comments[len(comments)-1].id)
	}
}

func TestCloudRejectsForeignNextLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values": []cloudComment{}, "next": "https://evil.example/comments",
		})
	}))
	defer srv.Close()

	if _, err := cloudAt(t, srv.URL).api.findMatchingComments(context.Background()); err == nil {
		t.Error("findMatchingComments() = nil error, want the off-host next link rejected")
	}
}

// A self link we did not build is not requested with the token: comments are
// addressed by id instead.
func TestCloudIgnoresForeignSelfLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"values": []map[string]any{{
			"id":      1,
			"content": map[string]string{"raw": vcs.AddFooterTags("old", "infracost-comment", nil)},
			"links":   map[string]any{"self": map[string]string{"href": "https://evil.example/comments/1"}},
		}}})
	}))
	defer srv.Close()

	comments, err := cloudAt(t, srv.URL).api.findMatchingComments(context.Background())
	if err != nil {
		t.Fatalf("findMatchingComments() error = %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("findMatchingComments() = %d comments, want 1", len(comments))
	}
	if want := srv.URL + "/pullrequests/1/comments/1"; comments[0].url != want {
		t.Errorf("comment url = %q, want %q", comments[0].url, want)
	}
}

// Inline comments and replies come back from the same endpoint; only top-level
// comments are ours.
func TestCloudSkipsInlineAndReplyComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tagged := vcs.AddFooterTags("old", "infracost-comment", nil)
		writeJSON(t, w, http.StatusOK, map[string]any{"values": []map[string]any{
			{"id": 1, "content": map[string]string{"raw": tagged}, "inline": map[string]any{"path": "main.tf"}},
			{"id": 2, "content": map[string]string{"raw": tagged}, "parent": map[string]any{"id": 1}},
			{"id": 3, "content": map[string]string{"raw": tagged}},
		}})
	}))
	defer srv.Close()

	comments, err := cloudAt(t, srv.URL).api.findMatchingComments(context.Background())
	if err != nil {
		t.Fatalf("findMatchingComments() error = %v", err)
	}
	if len(comments) != 1 || comments[0].id != 3 {
		t.Errorf("findMatchingComments() = %+v, want only the top-level comment", comments)
	}
}

func TestServerFindMatchingCommentsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tagged := vcs.AddFooterTags("old", "infracost-comment", nil)
		activity := func(id int64) map[string]any {
			return map[string]any{"action": "COMMENTED", "commentAction": "ADDED",
				"comment": serverComment{ID: id, Text: tagged, Version: 1}}
		}

		// Server returns activities newest first.
		if r.URL.Query().Get("start") == "0" {
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{activity(9)}, "isLastPage": false, "nextPageStart": 100,
			})
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"values": []map[string]any{activity(4)}, "isLastPage": true,
		})
	}))
	defer srv.Close()

	comments, err := serverAt(t, srv.URL).api.findMatchingComments(context.Background())
	if err != nil {
		t.Fatalf("findMatchingComments() error = %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("findMatchingComments() = %d comments, want both pages", len(comments))
	}
	if comments[len(comments)-1].id != 9 {
		t.Errorf("last comment id = %d, want the newest (9)", comments[len(comments)-1].id)
	}
}

// A page that reports more to come without advancing must not be re-requested
// forever: nextPageStart is nullable.
func TestServerStopsWhenPageDoesNotAdvance(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests > 5 {
			t.Fatal("findMatchingComments() looped on the same page")
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"values": []map[string]any{}, "isLastPage": false})
	}))
	defer srv.Close()

	if _, err := serverAt(t, srv.URL).api.findMatchingComments(context.Background()); err != nil {
		t.Fatalf("findMatchingComments() error = %v", err)
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1", requests)
	}
}

// The page guards stop a server that keeps handing out another page from
// looping us forever.
func TestFindMatchingCommentsStopsAtPageLimit(t *testing.T) {
	t.Run("cloud", func(t *testing.T) {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []cloudComment{},
				"next":   "http://" + r.Host + "/next",
			})
		}))
		defer srv.Close()

		if _, err := cloudAt(t, srv.URL).api.findMatchingComments(context.Background()); err == nil {
			t.Error("findMatchingComments() = nil error, want the page limit hit")
		}
		if requests != cloudMaxPages {
			t.Errorf("made %d requests, want %d", requests, cloudMaxPages)
		}
	})

	t.Run("server", func(t *testing.T) {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			writeJSON(t, w, http.StatusOK, map[string]any{
				"values": []map[string]any{}, "isLastPage": false, "nextPageStart": requests * serverPageLimit,
			})
		}))
		defer srv.Close()

		if _, err := serverAt(t, srv.URL).api.findMatchingComments(context.Background()); err == nil {
			t.Error("findMatchingComments() = nil error, want the page limit hit")
		}
		if requests != serverMaxPages {
			t.Errorf("made %d requests, want %d", requests, serverMaxPages)
		}
	})
}

// cloudAt builds a Cloud provider pointed at a test server.
func cloudAt(t *testing.T, url string) *Bitbucket {
	t.Helper()
	b, err := New(context.Background(), "my-org/my-repo", "token", 1, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	b.api.(*cloudAPI).baseURL = url
	return b
}

// serverAt builds a Server provider pointed at a test server.
func serverAt(t *testing.T, url string) *Bitbucket {
	t.Helper()
	b, err := New(context.Background(), "PROJ/my-repo", "user:pass", 42, Options{ServerURL: url})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return b
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encoding response: %v", err)
	}
}

func readJSON(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading request body: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshaling request body: %v", err)
	}
	return out
}

// readRaw returns the comment text from a request, whichever flavour's shape
// it arrived in.
func readRaw(t *testing.T, r *http.Request) string {
	t.Helper()
	payload := readJSON(t, r)
	if text, ok := payload["text"].(string); ok {
		return text
	}
	content, ok := payload["content"].(map[string]any)
	if !ok {
		t.Fatalf("request body has neither text nor content: %v", payload)
	}
	raw, _ := content["raw"].(string)
	return raw
}

// commentData is a report that reaches every section the default template
// wraps in HTML: costs, governance, budgets, fixed issues and project errors.
func commentData() comment.Data {
	return comment.Data{
		SupportsBotCommands:  true,
		Currency:             "USD",
		RepoName:             "my-repo",
		RepoURL:              "https://bitbucket.org/my-org/my-repo",
		CommitSHA:            "abc123",
		TotalMonthlyCost:     rat.New(150),
		PastTotalMonthlyCost: rat.New(100),
		Projects: []comment.ProjectResult{
			{
				Name:                 "my-project",
				TotalMonthlyCost:     rat.New(150),
				PastTotalMonthlyCost: rat.New(100),
				DiffBreakdown: &comment.CostBreakdown{
					TotalMonthlyCost: rat.New(50),
					Resources: []comment.BreakdownResource{
						{Name: "aws_instance.web", MonthlyCost: rat.New(50), Tags: map[string]string{"env": "production"}},
					},
				},
			},
			{
				Name: "broken-project",
				Diagnostics: []*diagnostic.Diagnostic{
					{Critical: true, Type: parserpb.DiagnosticType_DIAGNOSTIC_TYPE_HCL_PARSE_ERROR, Error: "Failed to parse: invalid HCL"},
				},
			},
		},
		FinOpsPolicyResults: []*provider.FinopsPolicyResult{
			{
				PolicyName:                  "Use reserved instances",
				PolicySlug:                  "use-reserved-instances",
				PolicyMessage:               "Consider using reserved instances for long-running workloads.",
				IncludeInPullRequestComment: true,
				FailingResources: []*provider.FinopsPolicyFailingResource{
					{
						Id:           "aws_instance.web",
						CauseAddress: "aws_instance.web",
						Path:         "main.tf",
						StartLine:    15,
						ProjectNames: []string{"my-project"},
						Issues:       []*provider.FinopsResourceIssue{{Description: "This instance runs 24/7"}},
					},
				},
				PassingResourceIds: []string{"aws_ebs_volume.old"},
			},
		},
		PreviousFinOpsPolicyResults: []*provider.FinopsPolicyResult{
			{
				PolicyName:                  "Use reserved instances",
				PolicySlug:                  "use-reserved-instances",
				IncludeInPullRequestComment: true,
				FailingResources:            []*provider.FinopsPolicyFailingResource{{Id: "aws_ebs_volume.old"}},
			},
		},
		BudgetResults: []comment.BudgetResult{
			{
				Tags:        []comment.BudgetTag{{Key: "env", Value: "production"}},
				StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
				Amount:      rat.New(1000),
				CurrentCost: rat.New(500),
			},
		},
	}
}

// The oauth2 transport re-attaches the token on every hop, so a same-host
// downgrade to http would put the credential on the wire in plaintext.
func TestCheckRedirectRejectsSchemeDowngrade(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
	}{
		{name: "same host and scheme", from: "https://bb.corp/a", to: "https://bb.corp/b"},
		{name: "https to http", from: "https://bb.corp/a", to: "http://bb.corp/b", wantErr: true},
		{name: "off host", from: "https://bb.corp/a", to: "https://evil.example/b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, err := http.NewRequest(http.MethodGet, tt.from, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			to, err := http.NewRequest(http.MethodGet, tt.to, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}

			if err := checkRedirect(to, []*http.Request{from}); (err != nil) != tt.wantErr {
				t.Errorf("checkRedirect() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// A delete that fails for any reason other than "already gone" is fatal:
// carrying on would post the new report alongside the old one.
func TestDeleteAndNewFailsOnRealDeleteError(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c := cloudComment{ID: 1}
			c.Content.Raw = vcs.AddFooterTags("old", "infracost-comment", nil)
			writeJSON(t, w, http.StatusOK, map[string]any{"values": []cloudComment{c}})
		case http.MethodDelete:
			w.WriteHeader(http.StatusForbidden)
		case http.MethodPost:
			created = true
			writeJSON(t, w, http.StatusCreated, cloudComment{ID: 2})
		}
	}))
	defer srv.Close()

	res, err := cloudAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorDeleteAndNew)
	if err == nil {
		t.Fatalf("PostComment() = %+v, nil error, want the 403 surfaced", res)
	}
	if created {
		t.Error("PostComment() created a duplicate comment after the delete failed")
	}
	if res.Posted {
		t.Error("PostComment() reported Posted with the old comment still up")
	}
}
