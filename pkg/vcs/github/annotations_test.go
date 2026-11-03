package github

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
	thread := func(id, key string, resolved, outdated bool) map[string]any {
		return map[string]any{
			"id": id, "isResolved": resolved, "isOutdated": outdated,
			"comments": map[string]any{"nodes": []map[string]any{{"body": vcs.AddAnnotationTag("x", tag, key)}}},
		}
	}
	threads := []map[string]any{
		thread("T_keep", "keep", false, false),
		thread("T_back", "back", true, false),
		thread("T_moved", "moved", false, true),
		thread("T_gone", "gone", false, false),
		{"id": "T_human", "isResolved": false, "isOutdated": false,
			"comments": map[string]any{"nodes": []map[string]any{{"body": "a human review comment"}}}},
	}

	var created []map[string]any
	mutations := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/graphql":
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch {
			case strings.Contains(body.Query, "unresolveReviewThread"):
				mutations[body.Variables["input"].(map[string]any)["threadId"].(string)] = "unresolve"
				_, _ = w.Write([]byte(`{"data":{"unresolveReviewThread":{}}}`))
			case strings.Contains(body.Query, "resolveReviewThread"):
				mutations[body.Variables["input"].(map[string]any)["threadId"].(string)] = "resolve"
				_, _ = w.Write([]byte(`{"data":{"resolveReviewThread":{}}}`))
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{
					"pullRequest": map[string]any{"reviewThreads": map[string]any{
						"nodes": threads, "pageInfo": map[string]any{"hasNextPage": false},
					}},
				}}})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/repos/o/r/pulls/1/comments":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["line"] == float64(99) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			created = append(created, body)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	g, err := New(context.Background(), "o", "r", "token", 1, Options{APIURL: srv.URL, Tag: tag, HeadSHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := g.PostAnnotations(context.Background(), []comment.Annotation{
		{Key: "keep", Path: "main.tf", Line: 1},
		{Key: "back", Path: "main.tf", Line: 2},
		{Key: "moved", Path: "main.tf", Line: 3, PolicyType: "FinOps", PolicyName: "P"},
		{Key: "new", Path: "dir/a.tf", Line: 7, PolicyType: "Tagging", PolicyName: "Team tag", Blocking: true},
		{Key: "bad", Path: "main.tf", Line: 99},
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Posted != 2 || res.Resolved != 1 || res.Rejected != 1 {
		t.Errorf("result = %+v", res)
	}
	want := map[string]string{"T_back": "unresolve", "T_moved": "resolve", "T_gone": "resolve"}
	if len(mutations) != len(want) {
		t.Errorf("mutations = %v, want %v", mutations, want)
	}
	for id, m := range want {
		if mutations[id] != m {
			t.Errorf("mutations = %v, want %v", mutations, want)
		}
	}

	if len(created) != 2 {
		t.Fatalf("created %d comments", len(created))
	}
	c := created[1]
	if c["path"] != "dir/a.tf" || c["line"] != float64(7) || c["side"] != "RIGHT" || c["commit_id"] != "abc" {
		t.Errorf("comment = %v", c)
	}
	body := c["body"].(string)
	if key, _ := vcs.ExtractAnnotationKey(body, tag); key != "new" {
		t.Errorf("body key = %q", key)
	}
	if !strings.Contains(body, "### ❌ Tagging issue found (blocking)") {
		t.Errorf("body = %q", body)
	}
}

func TestPostAnnotationsNoHeadSHA(t *testing.T) {
	g := &GitHub{}
	res, err := g.PostAnnotations(context.Background(), []comment.Annotation{{Path: "a", Line: 1}})
	if err != nil || res.Posted != 0 || res.SkipReason == "" {
		t.Errorf("got %+v, %v", res, err)
	}
}
