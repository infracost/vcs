package bitbucket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/infracost/vcs/pkg/vcs"
)

// cloudMaxPages bounds the comment listing: the next link comes from the
// response, so a misbehaving API must not be able to loop us forever.
const cloudMaxPages = 100

// cloudAPI talks to Bitbucket Cloud's REST 2.0 API.
type cloudAPI struct {
	httpClient *http.Client
	baseURL    string
	prNumber   int
	tag        string
}

// cloudComment is the wire shape of a Cloud pull request comment.
type cloudComment struct {
	ID      int64 `json:"id"`
	Deleted bool  `json:"deleted"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	// Inline is set on file comments and Parent on replies; ours are top-level.
	Inline *struct{} `json:"inline"`
	Parent *struct{} `json:"parent"`
}

func (c *cloudAPI) findMatchingComments(ctx context.Context) ([]bitbucketComment, error) {
	pageURL := fmt.Sprintf("%s/pullrequests/%d/comments?pagelen=100", c.baseURL, c.prNumber)

	var matching []bitbucketComment
	for page := 0; pageURL != ""; page++ {
		if page >= cloudMaxPages {
			return nil, fmt.Errorf("getting comments: more than %d pages", cloudMaxPages)
		}

		req, err := jsonRequest(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, err
		}

		body, err := doJSON(c.httpClient, req, "getting comments", http.StatusOK)
		if err != nil {
			return nil, err
		}

		var resData struct {
			Values []cloudComment `json:"values"`
			Next   string         `json:"next"`
		}
		if err := json.Unmarshal(body, &resData); err != nil {
			return nil, fmt.Errorf("unmarshaling response body: %w", err)
		}

		for _, comment := range resData.Values {
			if comment.Deleted || comment.Inline != nil || comment.Parent != nil {
				continue
			}
			if !vcs.HasFooterTagKey(comment.Content.Raw, c.tag) {
				continue
			}
			matching = append(matching, bitbucketComment{
				id:   comment.ID,
				body: comment.Content.Raw,
				url:  c.commentURL(comment.ID),
			})
		}

		if resData.Next != "" {
			if err := c.checkURL(resData.Next); err != nil {
				return nil, err
			}
		}
		pageURL = resData.Next
	}

	sortByID(matching)
	return matching, nil
}

// commentURL addresses a comment by id rather than trusting the self link
// Cloud hands back, which may be absent or off-host.
func (c *cloudAPI) commentURL(id int64) string {
	return fmt.Sprintf("%s/pullrequests/%d/comments/%d", c.baseURL, c.prNumber, id)
}

// checkURL guards the URLs Cloud hands back in the response body: they are
// requested with the access token, so they must stay on the API host.
func (c *cloudAPI) checkURL(raw string) error {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("parsing base URL: %w", err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parsing comment URL %q: %w", raw, err)
	}
	if parsed.Scheme != base.Scheme || parsed.Host != base.Host {
		return fmt.Errorf("comment URL %q is not on %s", raw, base.Host)
	}
	return nil
}

func (c *cloudAPI) createComment(ctx context.Context, body string) (bitbucketComment, error) {
	createURL := fmt.Sprintf("%s/pullrequests/%d/comments", c.baseURL, c.prNumber)

	req, err := jsonRequest(ctx, http.MethodPost, createURL, cloudBody(body))
	if err != nil {
		return bitbucketComment{}, err
	}

	resBody, err := doJSON(c.httpClient, req, "creating comment", http.StatusCreated, http.StatusOK)
	if err != nil {
		return bitbucketComment{}, err
	}

	var resData cloudComment
	if err := json.Unmarshal(resBody, &resData); err != nil {
		return bitbucketComment{}, fmt.Errorf("unmarshaling response body: %w", err)
	}

	return bitbucketComment{
		id:   resData.ID,
		body: resData.Content.Raw,
		url:  c.commentURL(resData.ID),
	}, nil
}

func (c *cloudAPI) updateComment(ctx context.Context, comment bitbucketComment, body string) error {
	req, err := jsonRequest(ctx, http.MethodPut, comment.url, cloudBody(body))
	if err != nil {
		return err
	}

	_, err = doJSON(c.httpClient, req, "updating comment", http.StatusOK, http.StatusCreated)
	return err
}

func (c *cloudAPI) deleteComment(ctx context.Context, comment bitbucketComment) error {
	req, err := jsonRequest(ctx, http.MethodDelete, comment.url, nil)
	if err != nil {
		return err
	}

	_, err = doJSON(c.httpClient, req, "deleting comment", http.StatusNoContent, http.StatusOK)
	return err
}

func cloudBody(body string) map[string]any {
	return map[string]any{"content": map[string]string{"raw": body}}
}
