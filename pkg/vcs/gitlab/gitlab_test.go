package gitlab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

// compile-time check that GitLab implements vcs.VCS.
var _ vcs.VCS = (*GitLab)(nil)

func TestSourceLink(t *testing.T) {
	g := &GitLab{}
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
			repoURL:   "https://gitlab.com/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://gitlab.com/my-org/my-repo/-/blob/abc123/main.tf#L42",
		},
		{
			name:      "no line",
			repoURL:   "https://gitlab.com/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      0,
			want:      "https://gitlab.com/my-org/my-repo/-/blob/abc123/main.tf",
		},
		{
			name:      "strips .git suffix",
			repoURL:   "https://gitlab.com/my-org/my-repo.git",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://gitlab.com/my-org/my-repo/-/blob/abc123/main.tf#L42",
		},
		{
			name:      "subgroup",
			repoURL:   "https://gitlab.com/group/subgroup/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://gitlab.com/group/subgroup/my-repo/-/blob/abc123/main.tf#L42",
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
			name:      "empty commitSHA",
			repoURL:   "https://gitlab.com/x/y",
			commitSHA: "",
			path:      "main.tf",
			line:      42,
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.SourceLink(tt.repoURL, tt.commitSHA, tt.path, tt.line)
			if got != tt.want {
				t.Errorf("SourceLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGraphQLMutationTypeNames guards against a subtle trap in shurcooL/graphql:
// it derives the GraphQL input type name from the Go struct name verbatim, so an
// unexported struct (e.g. updateNoteInput) produces a mutation GitLab rejects with
// "updateNoteInput isn't a defined input type". The struct name must match GitLab's
// schema type (UpdateNoteInput / DestroyNoteInput) exactly, including case.
func TestGraphQLMutationTypeNames(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	g, err := New(context.Background(), "group/repo", "token", 1, Options{ServerURL: srv.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name     string
		call     func() error
		wantType string
		badType  string
	}{
		{
			name: "updateNote",
			call: func() error {
				return g.callUpdateComment(context.Background(), gitlabComment{id: "gid://gitlab/Note/1"}, "hi")
			},
			wantType: "UpdateNoteInput!",
			badType:  "updateNoteInput",
		},
		{
			name: "destroyNote",
			call: func() error {
				return g.callDeleteComment(context.Background(), gitlabComment{id: "gid://gitlab/Note/1"})
			},
			wantType: "DestroyNoteInput!",
			badType:  "destroyNoteInput",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body = ""
			if err := tt.call(); err != nil {
				t.Fatalf("call error = %v", err)
			}
			if !strings.Contains(body, tt.wantType) {
				t.Errorf("mutation body missing %q\ngot: %s", tt.wantType, body)
			}
			if strings.Contains(body, "$input:"+tt.badType+"!") {
				t.Errorf("mutation body uses lowercase input type %q (GitLab will reject)\ngot: %s", tt.badType, body)
			}
		})
	}
}

func TestMaxCommentSize(t *testing.T) {
	g := &GitLab{}
	size, unit := g.MaxCommentSize()
	if size != maxCommentSize {
		t.Errorf("MaxCommentSize() size = %d, want %d", size, maxCommentSize)
	}
	if unit != comment.SizeUnitBytes {
		t.Errorf("MaxCommentSize() unit = %d, want SizeUnitBytes", unit)
	}
}
