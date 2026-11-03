package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

type diffRefs struct {
	BaseSHA  string `json:"base_sha"`
	HeadSHA  string `json:"head_sha"`
	StartSHA string `json:"start_sha"`
}

type annotationThread struct {
	id       string
	key      string
	resolved bool
}

// PostAnnotations opens one diff discussion per annotation. A key that already
// has a thread is not posted again. Threads whose failure is gone are resolved.
func (g *GitLab) PostAnnotations(ctx context.Context, annotations []comment.Annotation, failingKeys []string) (vcs.AnnotationResult, error) {
	threads, err := g.findAnnotationThreads(ctx)
	if err != nil {
		return vcs.AnnotationResult{}, err
	}

	existing := map[string]annotationThread{}
	for _, t := range threads {
		existing[t.key] = t
	}

	var res vcs.AnnotationResult
	var lastRejection error
	failing := vcs.StillFailing(annotations, failingKeys)
	var refs *diffRefs

	for _, a := range annotations {
		if t, ok := existing[a.Key]; ok {
			if t.resolved {
				if err := g.callResolveDiscussion(ctx, t.id, false); err != nil {
					return res, err
				}
			}
			continue
		}
		if refs == nil {
			if refs, err = g.getDiffRefs(ctx); err != nil {
				return res, err
			}
		}
		if err := g.callCreateDiscussion(ctx, *refs, a); err != nil {
			if vcs.IsRejected(err) {
				res.Rejected++
				lastRejection = err
				continue
			}
			return res, err
		}
		res.Posted++
	}
	if err := vcs.AllRejected(res, lastRejection); err != nil {
		return res, err
	}

	for _, t := range threads {
		if failing[t.key] || t.resolved {
			continue
		}
		if err := g.callResolveDiscussion(ctx, t.id, true); err != nil {
			return res, err
		}
		res.Resolved++
	}

	return res, nil
}

func (g *GitLab) mrAPIURL() string {
	return fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d", g.serverURL, url.PathEscape(g.project), g.mrNumber)
}

func (g *GitLab) getDiffRefs(ctx context.Context) (*diffRefs, error) {
	var mr struct {
		DiffRefs diffRefs `json:"diff_refs"`
	}
	if _, err := g.doJSON(ctx, http.MethodGet, g.mrAPIURL(), nil, &mr, vcs.OpList, "getting merge request"); err != nil {
		return nil, err
	}
	return &mr.DiffRefs, nil
}

func (g *GitLab) findAnnotationThreads(ctx context.Context) ([]annotationThread, error) {
	var user struct {
		ID int `json:"id"`
	}
	if _, err := g.doJSON(ctx, http.MethodGet, g.serverURL+"/api/v4/user", nil, &user, vcs.OpList, "getting current user"); err != nil {
		return nil, err
	}

	var threads []annotationThread
	page := "1"
	for page != "" {
		var discussions []struct {
			ID    string `json:"id"`
			Notes []struct {
				Body     string `json:"body"`
				Resolved bool   `json:"resolved"`
				Author   struct {
					ID int `json:"id"`
				} `json:"author"`
			} `json:"notes"`
		}
		apiURL := fmt.Sprintf("%s/discussions?per_page=100&page=%s", g.mrAPIURL(), page)
		header, err := g.doJSON(ctx, http.MethodGet, apiURL, nil, &discussions, vcs.OpList, "listing discussions")
		if err != nil {
			return nil, err
		}
		for _, d := range discussions {
			// Only threads we started: anyone can write our tag.
			if len(d.Notes) == 0 || d.Notes[0].Author.ID != user.ID {
				continue
			}
			key, ok := vcs.ExtractAnnotationKey(d.Notes[0].Body, g.tag)
			if !ok {
				continue
			}
			threads = append(threads, annotationThread{id: d.ID, key: key, resolved: d.Notes[0].Resolved})
		}
		page = header.Get("X-Next-Page")
	}
	return threads, nil
}

func (g *GitLab) callCreateDiscussion(ctx context.Context, refs diffRefs, a comment.Annotation) error {
	body := vcs.AddAnnotationTag(a.Markdown(), g.tag, a.Key)
	reqData := map[string]any{
		"body": escapeSnippetRefs(body),
		"position": map[string]any{
			"position_type": "text",
			"base_sha":      refs.BaseSHA,
			"start_sha":     refs.StartSHA,
			"head_sha":      refs.HeadSHA,
			"old_path":      a.Path,
			"new_path":      a.Path,
			"new_line":      a.Line,
		},
	}
	_, err := g.doJSON(ctx, http.MethodPost, g.mrAPIURL()+"/discussions", reqData, nil, vcs.OpCreate, "creating discussion")
	return err
}

func (g *GitLab) callResolveDiscussion(ctx context.Context, id string, resolved bool) error {
	apiURL := fmt.Sprintf("%s/discussions/%s?resolved=%t", g.mrAPIURL(), url.PathEscape(id), resolved)
	_, err := g.doJSON(ctx, http.MethodPut, apiURL, nil, nil, vcs.OpUpdate, "resolving discussion")
	return err
}

// doJSON sends reqData as JSON and decodes the response into out. Either may
// be nil.
func (g *GitLab) doJSON(ctx context.Context, method, apiURL string, reqData, out any, op, action string) (http.Header, error) {
	var reqBody io.Reader
	if reqData != nil {
		b, err := json.Marshal(reqData)
		if err != nil {
			return nil, fmt.Errorf("marshaling request: %w", err)
		}
		reqBody = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if reqData != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := g.httpClient.Do(req)
	if err != nil {
		return nil, withOp(op, vcs.RetryablePostError(fmt.Errorf("%s: %w", action, err)))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, withOp(op, vcs.HTTPPostError(res.StatusCode, res.Header, fmt.Errorf("%s: %s", action, res.Status)))
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
	}
	return res.Header, nil
}

func withOp(op string, err error) error {
	var postErr *vcs.PostError
	if errors.As(err, &postErr) {
		postErr.Op = op
	}
	return err
}
