package gitlab

import (
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
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

func TestMaxCommentSize(t *testing.T) {
	g := &GitLab{}
	if g.MaxCommentSize() != maxCommentSize {
		t.Errorf("MaxCommentSize() = %d, want %d", g.MaxCommentSize(), maxCommentSize)
	}
}
