package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
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
	size, unit := g.MaxCommentSize()
	if size != maxCommentSize {
		t.Errorf("MaxCommentSize() size = %d, want %d", size, maxCommentSize)
	}
	if unit != comment.SizeUnitRunes {
		t.Errorf("MaxCommentSize() unit = %d, want SizeUnitRunes", unit)
	}
}

func TestRestAPIURL(t *testing.T) {
	tests := map[string]string{
		"":                                "https://api.github.com",
		"https://api.github.com":          "https://api.github.com",
		"https://ghes.example.com":        "https://ghes.example.com/api/v3",
		"https://ghes.example.com/":       "https://ghes.example.com/api/v3",
		"https://octocorp.ghe.com":        "https://api.octocorp.ghe.com",
		"https://api.octocorp.ghe.com/":   "https://api.octocorp.ghe.com",
		"https://octocorp.ghe.com/api/v3": "https://api.octocorp.ghe.com",
	}
	for in, want := range tests {
		if got := restAPIURL(in); got != want {
			t.Errorf("restAPIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientDoesNotFollowCrossHostRedirect(t *testing.T) {
	var leaked bool
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	_, client, _, err := newClient(context.Background(), "secret", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Get(srv.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if leaked || res.StatusCode != http.StatusFound {
		t.Errorf("followed the redirect: leaked=%v status=%d", leaked, res.StatusCode)
	}
}
