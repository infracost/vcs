package azure

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
	threadBy := func(id int, status, body, author string) map[string]any {
		return map[string]any{"id": id, "status": status, "comments": []map[string]any{{"content": body, "author": map[string]string{"id": author}}}}
	}
	thread := func(id int, status, body string) map[string]any {
		return threadBy(id, status, body, "BOT-ID")
	}
	threads := []map[string]any{
		thread(1, "fixed", vcs.AddAnnotationTag("x", tag, "back")),
		thread(2, "active", vcs.AddAnnotationTag("x", tag, "gone")),
		thread(3, "closed", vcs.AddMarkdownTags("main comment", tag, nil)),
		threadBy(4, "active", vcs.AddAnnotationTag("x", tag, "new"), "someone-else"),
	}

	var created []map[string]any
	statuses := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/org/project/_apis/git/repositories/repo/pullRequests/1")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/org/_apis/connectionData":
			writeJSON(t, w, http.StatusOK, map[string]any{"authenticatedUser": map[string]string{"id": "bot-id"}})
		case r.Method == http.MethodGet && path == "/threads":
			writeJSON(t, w, http.StatusOK, map[string]any{"value": threads})
		case r.Method == http.MethodGet && path == "/iterations":
			writeJSON(t, w, http.StatusOK, map[string]any{"value": []map[string]int{{"id": 1}, {"id": 4}}})
		case r.Method == http.MethodPost && path == "/threads":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body)
			writeJSON(t, w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPatch && strings.HasPrefix(path, "/threads/"):
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			statuses[strings.TrimPrefix(path, "/threads/")] = body["status"]
			writeJSON(t, w, http.StatusOK, map[string]any{})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	a, err := New(context.Background(), srv.URL+"/org/project/_git/repo", "token", 1, Options{Tag: tag})
	if err != nil {
		t.Fatal(err)
	}

	res, err := a.PostAnnotations(context.Background(), []comment.Annotation{
		{Key: "back", Path: "main.tf", Line: 1},
		{Key: "new", Path: "dir/main.tf", Line: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Posted != 1 || res.Resolved != 1 {
		t.Errorf("result = %+v", res)
	}
	if statuses["1"] != "active" || statuses["2"] != "fixed" || len(statuses) != 2 {
		t.Errorf("statuses = %v", statuses)
	}
	if len(created) != 1 {
		t.Fatalf("created %d threads", len(created))
	}
	ctx := created[0]["threadContext"].(map[string]any)
	if ctx["filePath"] != "/dir/main.tf" {
		t.Errorf("threadContext = %v", ctx)
	}
	it := created[0]["pullRequestThreadContext"].(map[string]any)["iterationContext"].(map[string]any)
	if it["secondComparingIteration"] != float64(4) {
		t.Errorf("iterationContext = %v", it)
	}
}
