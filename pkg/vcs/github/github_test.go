package github

import (
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
)

// compile-time check that GitHub implements vcs.VCS.
var _ vcs.VCS = (*GitHub)(nil)

func TestSourceLink(t *testing.T) {
	g := &GitHub{}
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
			repoURL:   "https://github.com/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://github.com/my-org/my-repo/blob/abc123/main.tf#L42",
		},
		{
			name:      "no line",
			repoURL:   "https://github.com/my-org/my-repo",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      0,
			want:      "https://github.com/my-org/my-repo/blob/abc123/main.tf",
		},
		{
			name:      "strips .git suffix",
			repoURL:   "https://github.com/my-org/my-repo.git",
			commitSHA: "abc123",
			path:      "main.tf",
			line:      42,
			want:      "https://github.com/my-org/my-repo/blob/abc123/main.tf#L42",
		},
		{
			name:      "empty repoURL",
			repoURL:   "",
			commitSHA: "abc123",
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
	g := &GitHub{}
	if g.MaxCommentSize() != maxCommentSize {
		t.Errorf("MaxCommentSize() = %d, want %d", g.MaxCommentSize(), maxCommentSize)
	}
}
