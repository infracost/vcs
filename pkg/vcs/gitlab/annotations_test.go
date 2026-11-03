package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

func TestPostAnnotations(t *testing.T) {
	const tag = "infracost-comment"
	discussions := []map[string]any{
		{"id": "keep", "notes": []map[string]any{{"body": vcs.AddAnnotationTag("x", tag, "k1"), "resolved": false}}},
		{"id": "stale", "notes": []map[string]any{{"body": vcs.AddAnnotationTag("x", tag, "gone"), "resolved": false}}},
		{"id": "other", "notes": []map[string]any{{"body": "a human comment"}}},
	}

	var created []map[string]any
	var resolved []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v4/projects/g/r/merge_requests/3")
		switch {
		case r.Method == http.MethodGet && path == "/discussions":
			_ = json.NewEncoder(w).Encode(discussions)
		case r.Method == http.MethodGet && path == "":
			_ = json.NewEncoder(w).Encode(map[string]any{"diff_refs": map[string]string{"base_sha": "b", "head_sha": "h", "start_sha": "s"}})
		case r.Method == http.MethodPost && path == "/discussions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{}"))
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/discussions/"):
			resolved = append(resolved, strings.TrimPrefix(path, "/discussions/")+"?"+r.URL.RawQuery)
			_, _ = w.Write([]byte("{}"))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	g, err := New(context.Background(), "g/r", "token", 3, Options{ServerURL: srv.URL, Tag: tag})
	if err != nil {
		t.Fatal(err)
	}

	res, err := g.PostAnnotations(context.Background(), []comment.Annotation{
		{Key: "k1", Path: "main.tf", Line: 1},
		{Key: "k2", Path: "main.tf", Line: 9, PolicyType: "FinOps", PolicyName: "Use GP3", Address: "aws_ebs_volume.a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Posted != 1 || res.Resolved != 1 {
		t.Errorf("result = %+v", res)
	}
	if len(created) != 1 {
		t.Fatalf("created %d discussions", len(created))
	}
	pos := created[0]["position"].(map[string]any)
	if pos["new_path"] != "main.tf" || pos["new_line"] != float64(9) || pos["head_sha"] != "h" {
		t.Errorf("position = %v", pos)
	}
	if body := created[0]["body"].(string); !strings.Contains(body, "### ⚠️ FinOps issue found") {
		t.Errorf("body = %q", body)
	}
	if key, _ := vcs.ExtractAnnotationKey(created[0]["body"].(string), tag); key != "k2" {
		t.Errorf("body = %q", created[0]["body"])
	}
	if len(resolved) != 1 || resolved[0] != "stale?resolved=true" {
		t.Errorf("resolved = %v", resolved)
	}
}
