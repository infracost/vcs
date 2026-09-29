package azure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

// compile-time check that Azure implements vcs.VCS.
var _ vcs.VCS = (*Azure)(nil)

func TestSourceLink(t *testing.T) {
	a := &Azure{}
	tests := []struct {
		name      string
		repoURL   string
		commitSHA string
		path      string
		line      int
		want      string
	}{
		{
			name:      "simple",
			repoURL:   "https://dev.azure.com/org/project/_git/repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://dev.azure.com/org/project/_git/repo?path=main.tf&version=GCabc123&line=42&lineStyle=plain&_a=contents",
		},
		{
			name:      "no line",
			repoURL:   "https://dev.azure.com/org/project/_git/repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      0,
			want:      "https://dev.azure.com/org/project/_git/repo?path=main.tf&version=GCabc123&lineStyle=plain&_a=contents",
		},
		{
			name:      "empty repoURL",
			repoURL:   "",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "",
		},
		{
			name:      "empty path",
			repoURL:   "https://dev.azure.com/o/p/_git/r",
			commitSHA: "abc",
			path:      "",
			line:      42,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.SourceLink(tt.repoURL, tt.commitSHA, tt.path, tt.line)
			if got != tt.want {
				t.Errorf("SourceLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildAPIURL(t *testing.T) {
	tests := []struct {
		name    string
		repoURL string
		want    string
		wantErr bool
	}{
		{
			name:    "dev.azure.com",
			repoURL: "https://dev.azure.com/org/project/_git/repo",
			want:    "https://dev.azure.com/org/project/_apis/git/repositories/repo/",
		},
		{
			name:    "trailing slash",
			repoURL: "https://dev.azure.com/org/project/_git/repo/",
			want:    "https://dev.azure.com/org/project/_apis/git/repositories/repo/",
		},
		{
			name:    "strips userinfo",
			repoURL: "https://org@dev.azure.com/org/project/_git/repo",
			want:    "https://dev.azure.com/org/project/_apis/git/repositories/repo/",
		},
		{
			name:    "no _git segment",
			repoURL: "https://dev.azure.com/org/project/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildAPIURL(tt.repoURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildAPIURL() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("buildAPIURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaxCommentSize(t *testing.T) {
	a := &Azure{}
	size, unit := a.MaxCommentSize()
	if size != maxCommentSize {
		t.Errorf("MaxCommentSize() size = %d, want %d", size, maxCommentSize)
	}
	if unit != comment.SizeUnitRunes {
		t.Errorf("MaxCommentSize() unit = %d, want SizeUnitRunes", unit)
	}
}

// azureAt builds a provider pointed at a stub server.
func azureAt(t *testing.T, serverURL string) *Azure {
	t.Helper()
	a, err := New(context.Background(), serverURL+"/org/project/_git/repo", "token", 1, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return a
}

// writeThreads answers the thread lookup, optionally with one of our comments.
func writeThreads(t *testing.T, w http.ResponseWriter, commentURL string, existing bool) {
	t.Helper()
	threads := []map[string]any{}
	if existing {
		threads = append(threads, map[string]any{"comments": []map[string]any{{
			"id":      1,
			"content": vcs.AddMarkdownTags("old", "infracost-comment", nil),
			"_links":  map[string]any{"self": map[string]string{"href": commentURL}},
		}}})
	}
	writeJSON(t, w, http.StatusOK, map[string]any{"value": threads})
}

// writeComment answers a create with the comment Azure would have stored.
func writeComment(t *testing.T, w http.ResponseWriter, commentURL string) {
	t.Helper()
	writeJSON(t, w, http.StatusOK, map[string]any{"comments": []map[string]any{{
		"id":     1,
		"_links": map[string]any{"self": map[string]string{"href": commentURL}},
	}}})
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encoding stub response: %v", err)
	}
}

// postError recovers the *vcs.PostError err is expected to be.
func postError(t *testing.T, err error) *vcs.PostError {
	t.Helper()
	var postErr *vcs.PostError
	if !errors.As(err, &postErr) {
		t.Fatalf("error = %v (%T), want a *vcs.PostError", err, err)
	}
	return postErr
}

// A 203 is the sign-in page, not a write. Accepting it reported a comment on the
// dashboard that was never on the pull request, and exited 0.
func TestPostCommentRejectsUndocumented2xx(t *testing.T) {
	tests := []struct {
		name     string
		fail     string
		existing bool
		wantOp   string
	}{
		{name: "update", fail: http.MethodPatch, existing: true, wantOp: vcs.OpUpdate},
		{name: "create", fail: http.MethodPost, wantOp: vcs.OpCreate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commentURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == tt.fail {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusNonAuthoritativeInfo)
					_, _ = w.Write([]byte("<html><body>sign in</body></html>"))
					return
				}
				if r.Method == http.MethodGet {
					writeThreads(t, w, commentURL, tt.existing)
					return
				}
				t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			}))
			defer srv.Close()
			commentURL = srv.URL + "/comment/1"

			res, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorUpdate)
			if err == nil {
				t.Fatalf("PostComment() = %+v, nil error, want the 203 surfaced", res)
			}
			if res.Posted {
				t.Error("PostComment() reported Posted with nothing on the pull request")
			}
			if res.Body != "" {
				t.Errorf("PostComment() Body = %q, want empty: the dashboard records it", res.Body)
			}

			postErr := postError(t, err)
			if postErr.StatusCode != http.StatusNonAuthoritativeInfo {
				t.Errorf("StatusCode = %d, want 203", postErr.StatusCode)
			}
			if postErr.Op != tt.wantOp {
				t.Errorf("Op = %q, want %q", postErr.Op, tt.wantOp)
			}
		})
	}
}

// Contribute to pull requests is only the answer for a write, so a 403 has to
// say which request it came from.
func TestPostErrorCarriesOp(t *testing.T) {
	tests := []struct {
		name     string
		behavior vcs.Behavior
		fail     string
		existing bool
		wantOp   string
	}{
		{name: "list", behavior: vcs.BehaviorUpdate, fail: http.MethodGet, wantOp: vcs.OpList},
		{name: "create", behavior: vcs.BehaviorUpdate, fail: http.MethodPost, wantOp: vcs.OpCreate},
		{name: "update", behavior: vcs.BehaviorUpdate, fail: http.MethodPatch, existing: true, wantOp: vcs.OpUpdate},
		{name: "delete", behavior: vcs.BehaviorDeleteAndNew, fail: http.MethodDelete, existing: true, wantOp: vcs.OpDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commentURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == tt.fail {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				switch r.Method {
				case http.MethodGet:
					writeThreads(t, w, commentURL, tt.existing)
				case http.MethodPost:
					writeJSON(t, w, http.StatusOK, map[string]any{"comments": []map[string]any{{"id": 2}}})
				default:
					writeJSON(t, w, http.StatusOK, map[string]any{})
				}
			}))
			defer srv.Close()
			commentURL = srv.URL + "/comment/1"

			_, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", tt.behavior)
			if err == nil {
				t.Fatal("PostComment() = nil error, want the 403 surfaced")
			}

			postErr := postError(t, err)
			if postErr.StatusCode != http.StatusForbidden {
				t.Errorf("StatusCode = %d, want 403", postErr.StatusCode)
			}
			if postErr.Op != tt.wantOp {
				t.Errorf("Op = %q, want %q", postErr.Op, tt.wantOp)
			}
		})
	}
}

// Following Azure's sign-in redirect loses the status it sent, and puts a
// credential-bearing request on a host the caller never named.
func TestPostCommentDoesNotFollowSignInRedirect(t *testing.T) {
	var signedIn atomic.Bool
	signIn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		signedIn.Store(true)
		w.WriteHeader(http.StatusNonAuthoritativeInfo)
	}))
	defer signIn.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, signIn.URL+"/_signin", http.StatusFound)
	}))
	defer srv.Close()

	_, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorUpdate)
	if err == nil {
		t.Fatal("PostComment() = nil error, want the 302 surfaced")
	}
	if signedIn.Load() {
		t.Error("PostComment() followed the redirect to the sign-in host")
	}
	if !strings.Contains(err.Error(), "credential not accepted") {
		t.Errorf("error = %v, want it to name the credential failure", err)
	}

	postErr := postError(t, err)
	if postErr.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want 302", postErr.StatusCode)
	}
	if postErr.Op != vcs.OpList {
		t.Errorf("Op = %q, want %q", postErr.Op, vcs.OpList)
	}
}

// 204 is the usual answer to a delete, and plausible for an update. Rejecting
// it fails mid delete-and-new, after comments are gone and before the new one.
func TestPostCommentAcceptsNoContent(t *testing.T) {
	tests := []struct {
		name      string
		behavior  vcs.Behavior
		noContent string
	}{
		{name: "delete", behavior: vcs.BehaviorDeleteAndNew, noContent: http.MethodDelete},
		{name: "update", behavior: vcs.BehaviorUpdate, noContent: http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commentURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case tt.noContent:
					w.WriteHeader(http.StatusNoContent)
				case http.MethodGet:
					writeThreads(t, w, commentURL, true)
				case http.MethodPost:
					writeComment(t, w, commentURL)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
				}
			}))
			defer srv.Close()
			commentURL = srv.URL + "/comment/1"

			res, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", tt.behavior)
			if err != nil {
				t.Fatalf("PostComment() error = %v, want 204 accepted", err)
			}
			if !res.Posted {
				t.Error("PostComment() Posted = false, want true")
			}
		})
	}
}

// A same-host 3xx is an org rename, a proxy or an old visualstudio.com URL, not
// a sign-in bounce, so it still has to be followed.
func TestPostCommentFollowsSameHostRedirect(t *testing.T) {
	var commentURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/moved"):
			http.Redirect(w, r, "/moved"+r.URL.Path+"?"+r.URL.RawQuery, http.StatusFound)
		case r.Method == http.MethodGet:
			writeThreads(t, w, commentURL, false)
		case r.Method == http.MethodPost:
			writeComment(t, w, commentURL)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	commentURL = srv.URL + "/comment/1"

	res, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorUpdate)
	if err != nil {
		t.Fatalf("PostComment() error = %v, want the same-host redirect followed", err)
	}
	if !res.Posted {
		t.Error("PostComment() Posted = false, want true")
	}
}

// The sign-in page is a whole HTML document. Quoting it puts it in the logs and
// on the dashboard.
func TestPostErrorOmitsNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNonAuthoritativeInfo)
		_, _ = w.Write([]byte("<html><body>sign in to Azure DevOps</body></html>"))
	}))
	defer srv.Close()

	_, err := azureAt(t, srv.URL).PostComment(context.Background(), "new", vcs.BehaviorUpdate)
	if err == nil {
		t.Fatal("PostComment() = nil error, want the 203 surfaced")
	}
	if strings.Contains(err.Error(), "<html>") {
		t.Errorf("error = %v, want the HTML sign-in page left out", err)
	}
}
