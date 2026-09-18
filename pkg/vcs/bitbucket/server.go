package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/infracost/vcs/pkg/vcs"
)

// serverPageLimit is the activities page size; Server caps pages server-side
// and answers with its own nextPageStart regardless.
const serverPageLimit = 100

// serverMaxPages bounds the activity walk, as cloudMaxPages does for Cloud.
const serverMaxPages = 100

// serverAPI talks to Bitbucket Server / Data Center's REST 1.0 API.
type serverAPI struct {
	httpClient *http.Client
	baseURL    string
	prNumber   int
	tag        string
}

// serverComment is the wire shape of a Server pull request comment.
type serverComment struct {
	ID      int64  `json:"id"`
	Text    string `json:"text"`
	Version int64  `json:"version"`
}

// findMatchingComments reads the activity stream: Server has no endpoint that
// lists a pull request's top-level comments directly.
func (s *serverAPI) findMatchingComments(ctx context.Context) ([]bitbucketComment, error) {
	var matching []bitbucketComment

	start := 0
	for page := 0; ; page++ {
		if page >= serverMaxPages {
			return nil, fmt.Errorf("getting comments: more than %d pages", serverMaxPages)
		}

		pageURL := fmt.Sprintf("%s/pull-requests/%d/activities?start=%d&limit=%d", s.baseURL, s.prNumber, start, serverPageLimit)

		req, err := jsonRequest(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}

		body, err := doJSON(s.httpClient, req, "getting comments", http.StatusOK)
		if err != nil {
			return nil, err
		}

		var resData struct {
			Values []struct {
				Action        string        `json:"action"`
				CommentAction string        `json:"commentAction"`
				Comment       serverComment `json:"comment"`
				// CommentAnchor is set on inline comments; ours are top-level.
				CommentAnchor *struct{} `json:"commentAnchor"`
			} `json:"values"`
			IsLastPage    bool `json:"isLastPage"`
			NextPageStart int  `json:"nextPageStart"`
		}
		if err := json.Unmarshal(body, &resData); err != nil {
			return nil, fmt.Errorf("unmarshaling response body: %w", err)
		}

		for _, activity := range resData.Values {
			if activity.Action != "COMMENTED" || activity.CommentAction != "ADDED" || activity.CommentAnchor != nil {
				continue
			}
			if !vcs.HasFooterTagKey(activity.Comment.Text, s.tag) {
				continue
			}
			matching = append(matching, s.toComment(activity.Comment))
		}

		// nextPageStart is nullable, so a page that does not advance ends the
		// walk: taking it at face value would re-request the same page forever.
		if resData.IsLastPage || resData.NextPageStart <= start {
			break
		}
		start = resData.NextPageStart
	}

	sortByID(matching)
	return matching, nil
}

func (s *serverAPI) createComment(ctx context.Context, body string) (bitbucketComment, error) {
	url := fmt.Sprintf("%s/pull-requests/%d/comments", s.baseURL, s.prNumber)

	req, err := jsonRequest(ctx, http.MethodPost, url, map[string]string{"text": body})
	if err != nil {
		return bitbucketComment{}, err
	}

	resBody, err := doJSON(s.httpClient, req, "creating comment", http.StatusCreated, http.StatusOK)
	if err != nil {
		return bitbucketComment{}, err
	}

	var resData serverComment
	if err := json.Unmarshal(resBody, &resData); err != nil {
		return bitbucketComment{}, fmt.Errorf("unmarshaling response body: %w", err)
	}

	return s.toComment(resData), nil
}

// updateComment echoes the comment's version back: Server rejects a mutation
// that does not carry the version it last handed out.
func (s *serverAPI) updateComment(ctx context.Context, comment bitbucketComment, body string) error {
	req, err := jsonRequest(ctx, http.MethodPut, s.commentURL(comment.id), map[string]any{
		"text":    body,
		"version": comment.version,
	})
	if err != nil {
		return err
	}

	_, err = doJSON(s.httpClient, req, "updating comment", http.StatusOK)
	return err
}

func (s *serverAPI) deleteComment(ctx context.Context, comment bitbucketComment) error {
	url := fmt.Sprintf("%s?version=%d", s.commentURL(comment.id), comment.version)

	req, err := jsonRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	_, err = doJSON(s.httpClient, req, "deleting comment", http.StatusNoContent, http.StatusOK)
	return err
}

func (s *serverAPI) commentURL(id int64) string {
	return fmt.Sprintf("%s/pull-requests/%d/comments/%d", s.baseURL, s.prNumber, id)
}

func (s *serverAPI) toComment(c serverComment) bitbucketComment {
	return bitbucketComment{
		id:      c.ID,
		body:    c.Text,
		url:     s.commentURL(c.ID),
		version: c.Version,
	}
}
