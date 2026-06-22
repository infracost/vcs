// Package azure implements the vcs.VCS interface for Azure DevOps Repos
// pull requests.
//
// Azure DevOps does not expose an API to minimize comments, so
// BehaviorHideAndNew degrades to delete-and-new. Comments are top-level
// thread comments, found / updated / deleted via the Threads REST API.
package azure

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"golang.org/x/oauth2"
)

// maxCommentSize is the Azure DevOps comment body limit, in characters.
// comment.Render reserves headroom inside this limit for the markdown
// tag and truncation imprecision; do not subtract from it here.
const maxCommentSize = 150000

// patLength is the standard length of an Azure DevOps Personal Access Token.
// Tokens of this length are sent via HTTP Basic; OAuth bearer tokens otherwise.
const patLength = 52

// Options configures an Azure DevOps VCS provider.
type Options struct {
	// TLSConfig is optional TLS configuration for the API client.
	TLSConfig *tls.Config

	// Tag is used to identify Infracost comments. Defaults to "infracost-comment".
	Tag string

	// Template overrides the default comment template. If nil, the default
	// template is used.
	Template *template.Template

	// InitAsActive opens new threads in the "active" status instead of "closed".
	// When closed (the default), the comment is collapsed and doesn't add a
	// blocking required-review.
	InitAsActive bool
}

// Azure implements vcs.VCS for Azure DevOps pull requests.
type Azure struct {
	httpClient   *http.Client
	repoAPIURL   string
	prNumber     int
	tag          string
	tmpl         *template.Template
	initAsActive bool
}

// New creates a new Azure DevOps VCS provider for the given repo and pull request.
// `repoURL` is the web URL of the repository, e.g. "https://dev.azure.com/org/project/_git/repo".
func New(ctx context.Context, repoURL, token string, prNumber int, opts Options) (*Azure, error) {
	httpClient, err := newClient(ctx, token, opts.TLSConfig)
	if err != nil {
		return nil, err
	}

	apiURL, err := buildAPIURL(repoURL)
	if err != nil {
		return nil, err
	}

	tag := opts.Tag
	if tag == "" {
		tag = "infracost-comment"
	}

	tmpl := opts.Template
	if tmpl == nil {
		tmpl = comment.DefaultTemplate
	}

	return &Azure{
		httpClient:   httpClient,
		repoAPIURL:   apiURL,
		prNumber:     prNumber,
		tag:          tag,
		tmpl:         tmpl,
		initAsActive: opts.InitAsActive,
	}, nil
}

// GenerateComment renders a PR comment from the given data.
func (a *Azure) GenerateComment(data comment.Data) (string, error) {
	return comment.Render(a.tmpl, a.MaxCommentSize(), a.SourceLink, data)
}

// MaxCommentSize returns the Azure DevOps comment body size limit in characters.
func (a *Azure) MaxCommentSize() int { return maxCommentSize }

// SourceLink returns a URL to a file/line in the Azure DevOps web UI following
// the ?path=&version=GC<sha>&line=<n> shape.
func (a *Azure) SourceLink(repoURL, commitSHA, path string, startLine int) string {
	if repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}
	cleanURL := strings.TrimSuffix(repoURL, ".git")
	link := fmt.Sprintf("%s?path=%s&version=GC%s", cleanURL, path, commitSHA)
	if startLine > 0 {
		link = fmt.Sprintf("%s&line=%d", link, startLine)
	}
	return link + "&lineStyle=plain&_a=contents"
}

// PostComment publishes a comment to the pull request using the given behavior.
// Azure has no minimize API, so BehaviorHideAndNew falls back to delete-and-new.
func (a *Azure) PostComment(ctx context.Context, body string, behavior vcs.Behavior) (vcs.PostResult, error) {
	now := time.Now()
	taggedBody := vcs.AddMarkdownTags(body, a.tag, &now)

	switch behavior {
	case vcs.BehaviorUpdate:
		return a.updateComment(ctx, taggedBody, &now)
	case vcs.BehaviorNew:
		return a.newComment(ctx, taggedBody)
	case vcs.BehaviorHideAndNew, vcs.BehaviorDeleteAndNew:
		return a.deleteAndNewComment(ctx, taggedBody, &now)
	default:
		return vcs.PostResult{}, fmt.Errorf("unknown behavior: %s", behavior)
	}
}

func (a *Azure) updateComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := a.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 {
		latest := comments[len(comments)-1]

		if latestValidAt := vcs.ExtractValidAt(latest.body); validAt != nil && latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one is newer: %s", latest.href)}, nil
		}

		if latest.body == body {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one matches exactly: %s", latest.href)}, nil
		}

		if err := a.callUpdateComment(ctx, latest, body); err != nil {
			return vcs.PostResult{}, err
		}
		return vcs.PostResult{Posted: true}, nil
	}

	if _, err := a.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (a *Azure) newComment(ctx context.Context, body string) (vcs.PostResult, error) {
	if _, err := a.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (a *Azure) deleteAndNewComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := a.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 && validAt != nil {
		latest := comments[len(comments)-1]
		if latestValidAt := vcs.ExtractValidAt(latest.body); latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not adding comment since the latest one is newer: %s", latest.href)}, nil
		}
	}

	for _, c := range comments {
		if err := a.callDeleteComment(ctx, c); err != nil {
			return vcs.PostResult{}, err
		}
	}

	if _, err := a.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

// azureComment represents an Azure Repos comment we care about (the top-level
// comment in a thread).
type azureComment struct {
	id            int64
	body          string
	publishedDate string
	href          string
}

// apiComment is the wire shape of a comment returned by the Azure Repos API.
type apiComment struct {
	ID            int64  `json:"id"`
	Content       string `json:"content"`
	PublishedDate string `json:"publishedDate"`
	IsDeleted     bool   `json:"isDeleted"`
	Links         struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"_links"`
}

func (a *Azure) findMatchingComments(ctx context.Context) ([]azureComment, error) {
	apiURL := fmt.Sprintf("%spullRequests/%d/threads?api-version=6.0", a.repoAPIURL, a.prNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getting comments: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getting comments: %s", res.Status)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var resData struct {
		Value []struct {
			IsDeleted bool         `json:"isDeleted"`
			Comments  []apiComment `json:"comments"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resBody, &resData); err != nil {
		return nil, fmt.Errorf("unmarshaling response body: %w", err)
	}

	var matching []azureComment
	for _, thread := range resData.Value {
		if thread.IsDeleted {
			continue
		}

		// Threads we created always have our comment as the first entry, so
		// we only need to inspect the first non-deleted comment per thread.
		for _, c := range thread.Comments {
			if c.IsDeleted {
				continue
			}
			if !vcs.HasTagKey(c.Content, a.tag) {
				break
			}
			matching = append(matching, azureComment{
				id:            c.ID,
				body:          c.Content,
				href:          c.Links.Self.Href,
				publishedDate: c.PublishedDate,
			})
			break
		}
	}

	return matching, nil
}

func (a *Azure) callCreateComment(ctx context.Context, body string) (azureComment, error) {
	status := "closed"
	if a.initAsActive {
		status = "active"
	}

	reqData, err := json.Marshal(map[string]any{
		"comments": []map[string]any{
			{
				"content":         body,
				"parentCommentId": 0,
				"commentType":     1,
			},
		},
		"status": status,
	})
	if err != nil {
		return azureComment{}, fmt.Errorf("marshaling comment body: %w", err)
	}

	apiURL := fmt.Sprintf("%spullRequests/%d/threads?api-version=7.1", a.repoAPIURL, a.prNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(reqData))
	if err != nil {
		return azureComment{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := a.httpClient.Do(req)
	if err != nil {
		return azureComment{}, fmt.Errorf("creating comment: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		resBody, _ := io.ReadAll(res.Body)
		return azureComment{}, fmt.Errorf("creating comment: %s\n%s", res.Status, string(resBody))
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return azureComment{}, fmt.Errorf("reading response body: %w", err)
	}

	var resData struct {
		Comments []apiComment `json:"comments"`
	}
	if err := json.Unmarshal(resBody, &resData); err != nil {
		return azureComment{}, fmt.Errorf("unmarshaling response body: %w", err)
	}
	if len(resData.Comments) == 0 {
		return azureComment{}, fmt.Errorf("thread created with no comments")
	}

	first := resData.Comments[0]
	return azureComment{
		id:            first.ID,
		body:          first.Content,
		href:          first.Links.Self.Href,
		publishedDate: first.PublishedDate,
	}, nil
}

func (a *Azure) callUpdateComment(ctx context.Context, c azureComment, body string) error {
	reqData, err := json.Marshal(map[string]any{
		"content":         body,
		"parentCommentId": 0,
		"commentType":     1,
	})
	if err != nil {
		return fmt.Errorf("marshaling comment body: %w", err)
	}

	apiURL := fmt.Sprintf("%s?api-version=6.0", c.href)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, apiURL, bytes.NewBuffer(reqData))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("updating comment: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("updating comment: %s", res.Status)
	}
	return nil
}

func (a *Azure) callDeleteComment(ctx context.Context, c azureComment) error {
	apiURL := fmt.Sprintf("%s?api-version=6.0", c.href)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, apiURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	res, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting comment: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= 300 {
		return fmt.Errorf("deleting comment: %s", res.Status)
	}
	return nil
}

// buildAPIURL converts a repo's web URL to the corresponding API base URL.
// e.g. https://dev.azure.com/org/project/_git/repo →
//
//	https://dev.azure.com/org/project/_apis/git/repositories/repo/
func buildAPIURL(repoURL string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("parsing repo URL: %w", err)
	}

	parts := strings.SplitN(u.Path, "_git/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repo URL format %q (expected https://dev.azure.com/<org>/<project>/_git/<repo>)", repoURL)
	}

	// Strip any "org@" user-info from the URL — Azure rejects token auth when
	// it's present.
	u.User = nil
	u.Path = fmt.Sprintf("%s_apis/git/repositories/%s", parts[0], parts[1])
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u.String(), nil
}

// newClient builds an authenticated HTTP client. Tokens of patLength chars are
// treated as Personal Access Tokens and sent via HTTP Basic; anything else is
// sent as a Bearer token.
func newClient(ctx context.Context, token string, tlsConfig *tls.Config) (*http.Client, error) {
	accessToken, tokenType := token, "Bearer"
	if len(token) == patLength {
		accessToken = base64.StdEncoding.EncodeToString([]byte(":" + accessToken))
		tokenType = "Basic"
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: accessToken,
		TokenType:   tokenType,
	})

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	rawClient := &http.Client{Transport: transport}
	httpCtx := context.WithValue(ctx, oauth2.HTTPClient, rawClient)

	return oauth2.NewClient(httpCtx, ts), nil
}
