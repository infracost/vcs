package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
)

type annotationThread struct {
	id     int64
	key    string
	status string
}

// PostAnnotations opens one file thread per annotation. A key that already
// has a thread is not posted again. Threads whose key is gone are set to
// "fixed".
func (a *Azure) PostAnnotations(ctx context.Context, annotations []comment.Annotation) (vcs.AnnotationResult, error) {
	threads, err := a.findAnnotationThreads(ctx)
	if err != nil {
		return vcs.AnnotationResult{}, err
	}

	existing := map[string]annotationThread{}
	for _, t := range threads {
		existing[t.key] = t
	}

	var res vcs.AnnotationResult
	wanted := map[string]bool{}
	iteration := 0

	for _, an := range annotations {
		wanted[an.Key] = true
		if t, ok := existing[an.Key]; ok {
			if t.status == "fixed" || t.status == "closed" {
				if err := a.callSetThreadStatus(ctx, t.id, "active"); err != nil {
					return res, err
				}
			}
			continue
		}
		if iteration == 0 {
			if iteration, err = a.latestIteration(ctx); err != nil {
				return res, err
			}
		}
		if err := a.callCreateFileThread(ctx, iteration, an); err != nil {
			if vcs.IsRejected(err) {
				res.Rejected++
				continue
			}
			return res, err
		}
		res.Posted++
	}

	for _, t := range threads {
		if wanted[t.key] || t.status == "fixed" || t.status == "closed" {
			continue
		}
		if err := a.callSetThreadStatus(ctx, t.id, "fixed"); err != nil {
			return res, err
		}
		res.Resolved++
	}

	return res, nil
}

func (a *Azure) findAnnotationThreads(ctx context.Context) ([]annotationThread, error) {
	var resData struct {
		Value []struct {
			ID        int64        `json:"id"`
			Status    string       `json:"status"`
			IsDeleted bool         `json:"isDeleted"`
			Comments  []apiComment `json:"comments"`
		} `json:"value"`
	}
	apiURL := fmt.Sprintf("%spullRequests/%d/threads?api-version=7.1", a.repoAPIURL, a.prNumber)
	if err := a.doJSON(ctx, http.MethodGet, apiURL, nil, &resData, vcs.OpList, "listing threads"); err != nil {
		return nil, err
	}

	var threads []annotationThread
	for _, t := range resData.Value {
		if t.IsDeleted || len(t.Comments) == 0 {
			continue
		}
		key, ok := vcs.ExtractAnnotationKey(t.Comments[0].Content, a.tag)
		if !ok {
			continue
		}
		threads = append(threads, annotationThread{id: t.ID, key: key, status: t.Status})
	}
	return threads, nil
}

func (a *Azure) latestIteration(ctx context.Context) (int, error) {
	var resData struct {
		Value []struct {
			ID int `json:"id"`
		} `json:"value"`
	}
	apiURL := fmt.Sprintf("%spullRequests/%d/iterations?api-version=7.1", a.repoAPIURL, a.prNumber)
	if err := a.doJSON(ctx, http.MethodGet, apiURL, nil, &resData, vcs.OpList, "listing iterations"); err != nil {
		return 0, err
	}
	latest := 1
	for _, it := range resData.Value {
		latest = max(latest, it.ID)
	}
	return latest, nil
}

func (a *Azure) callCreateFileThread(ctx context.Context, iteration int, an comment.Annotation) error {
	body := vcs.AddAnnotationTag(an.Markdown(), a.tag, an.Key)
	reqData := map[string]any{
		"comments": []map[string]any{
			{"content": body, "parentCommentId": 0, "commentType": 1},
		},
		"status": "active",
		"threadContext": map[string]any{
			"filePath":       "/" + an.Path,
			"rightFileStart": map[string]int{"line": an.Line, "offset": 1},
			"rightFileEnd":   map[string]int{"line": an.Line, "offset": 1},
		},
		"pullRequestThreadContext": map[string]any{
			"iterationContext": map[string]int{
				"firstComparingIteration":  1,
				"secondComparingIteration": iteration,
			},
		},
	}
	apiURL := fmt.Sprintf("%spullRequests/%d/threads?api-version=7.1", a.repoAPIURL, a.prNumber)
	return a.doJSON(ctx, http.MethodPost, apiURL, reqData, nil, vcs.OpCreate, "creating thread")
}

func (a *Azure) callSetThreadStatus(ctx context.Context, id int64, status string) error {
	apiURL := fmt.Sprintf("%spullRequests/%d/threads/%d?api-version=7.1", a.repoAPIURL, a.prNumber, id)
	return a.doJSON(ctx, http.MethodPatch, apiURL, map[string]any{"status": status}, nil, vcs.OpUpdate, "updating thread")
}

// doJSON sends reqData as JSON and decodes the response into out. Either may
// be nil. Only 200 counts as success: see callCreateComment.
func (a *Azure) doJSON(ctx context.Context, method, apiURL string, reqData, out any, op, action string) error {
	var reqBody io.Reader
	if reqData != nil {
		b, err := json.Marshal(reqData)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		reqBody = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, reqBody)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	if reqData != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return withOp(op, vcs.RetryablePostError(fmt.Errorf("%s: %w", action, err)))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return responseError(op, action, res)
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}
