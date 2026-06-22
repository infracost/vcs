package azure

import (
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
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
	if a.MaxCommentSize() != maxCommentSize {
		t.Errorf("MaxCommentSize() = %d, want %d", a.MaxCommentSize(), maxCommentSize)
	}
}
