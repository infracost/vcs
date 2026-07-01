// Package gitlab implements the vcs.VCS interface for GitLab merge requests.
//
// It uses GitLab's GraphQL API to find existing comments and its REST API
// to create / update / delete them. GitLab has no equivalent of GitHub's
// minimizeComment mutation, so BehaviorHideAndNew degrades to delete-and-new.
package gitlab

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"github.com/shurcooL/graphql"
	"golang.org/x/oauth2"
)

// defaultServerURL is gitlab.com; override via Options.ServerURL for self-managed.
const defaultServerURL = "https://gitlab.com"

// maxCommentSize is the GitLab merge-request note body limit, in bytes
// (GitLab caps notes at ~1 MB). comment.Render reserves headroom inside this
// limit for the markdown tag and truncation imprecision; do not subtract from
// it here.
const maxCommentSize = 1000000

// Options configures a GitLab VCS provider.
type Options struct {
	// ServerURL is the base GitLab URL. Leave empty for gitlab.com; set for
	// self-managed GitLab.
	ServerURL string

	// TLSConfig is optional TLS configuration for self-managed GitLab.
	TLSConfig *tls.Config

	// Tag is used to identify Infracost comments. Defaults to "infracost-comment".
	Tag string

	// Template overrides the default comment template. If nil, the
	// default template is used.
	Template *template.Template
}

// GitLab implements vcs.VCS for GitLab merge requests.
type GitLab struct {
	httpClient    *http.Client
	graphqlClient *graphql.Client
	recorder      *vcs.StatusRecorder
	serverURL     string
	project       string
	mrNumber      int
	tag           string
	tmpl          *template.Template
}

// New creates a new GitLab VCS provider for the given project and merge request.
// `project` is the full path of the project, e.g. "group/subgroup/repo".
func New(ctx context.Context, project, token string, mrNumber int, opts Options) (*GitLab, error) {
	serverURL := opts.ServerURL
	if serverURL == "" {
		serverURL = defaultServerURL
	}

	httpClient, gqlClient, recorder, err := newClients(ctx, token, serverURL, opts.TLSConfig)
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

	return &GitLab{
		httpClient:    httpClient,
		graphqlClient: gqlClient,
		recorder:      recorder,
		serverURL:     serverURL,
		project:       project,
		mrNumber:      mrNumber,
		tag:           tag,
		tmpl:          tmpl,
	}, nil
}

// GenerateComment renders a merge-request comment from the given data.
func (g *GitLab) GenerateComment(data comment.Data) (string, error) {
	size, unit := g.MaxCommentSize()
	return comment.Render(g.tmpl, size, unit, g.SourceLink, data)
}

// MaxCommentSize returns the GitLab note body size limit. GitLab measures the
// limit in bytes.
func (g *GitLab) MaxCommentSize() (int, comment.SizeUnit) {
	return maxCommentSize, comment.SizeUnitBytes
}

// SourceLink returns a URL to a file/line in the GitLab web UI following the
// /-/blob/<sha>/<path>#L<line> shape.
func (g *GitLab) SourceLink(repoURL, commitSHA, path string, startLine int) string {
	if repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}
	cleanURL := strings.TrimSuffix(repoURL, ".git")
	link := fmt.Sprintf("%s/-/blob/%s/%s", cleanURL, commitSHA, path)
	if startLine > 0 {
		link = fmt.Sprintf("%s#L%d", link, startLine)
	}
	return link
}

// PostComment publishes a comment to the merge request using the given behavior.
// GitLab has no minimize API, so BehaviorHideAndNew falls back to delete-and-new.
func (g *GitLab) PostComment(ctx context.Context, body string, behavior vcs.Behavior) (vcs.PostResult, error) {
	now := time.Now()
	taggedBody := vcs.AddMarkdownTags(body, g.tag, &now)

	switch behavior {
	case vcs.BehaviorUpdate:
		return g.updateComment(ctx, taggedBody, &now)
	case vcs.BehaviorNew:
		return g.newComment(ctx, taggedBody)
	case vcs.BehaviorHideAndNew, vcs.BehaviorDeleteAndNew:
		return g.deleteAndNewComment(ctx, taggedBody, &now)
	default:
		return vcs.PostResult{}, fmt.Errorf("unknown behavior: %s", behavior)
	}
}

func (g *GitLab) updateComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := g.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 {
		latest := comments[len(comments)-1]

		if latestValidAt := vcs.ExtractValidAt(latest.body); validAt != nil && latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one is newer: %s", latest.url)}, nil
		}

		if latest.body == body {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one matches exactly: %s", latest.url)}, nil
		}

		if err := g.callUpdateComment(ctx, latest, body); err != nil {
			return vcs.PostResult{}, err
		}
		return vcs.PostResult{Posted: true}, nil
	}

	if _, err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (g *GitLab) newComment(ctx context.Context, body string) (vcs.PostResult, error) {
	if _, err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (g *GitLab) deleteAndNewComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := g.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 && validAt != nil {
		latest := comments[len(comments)-1]
		if latestValidAt := vcs.ExtractValidAt(latest.body); latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not adding comment since the latest one is newer: %s", latest.url)}, nil
		}
	}

	for _, c := range comments {
		if err := g.callDeleteComment(ctx, c); err != nil {
			return vcs.PostResult{}, err
		}
	}

	if _, err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

// gitlabComment represents a comment found on a GitLab merge request.
type gitlabComment struct {
	id        string
	body      string
	createdAt string
	url       string
}

// findMatchingComments returns merge-request notes whose body contains our tag,
// ordered as returned by the API (oldest first per page).
func (g *GitLab) findMatchingComments(ctx context.Context) ([]gitlabComment, error) {
	var q struct {
		Project struct {
			MergeRequest struct {
				Notes struct {
					Nodes []struct {
						ID        graphql.String
						URL       graphql.String
						CreatedAt graphql.String
						Body      graphql.String
					}
					PageInfo struct {
						EndCursor   graphql.String
						HasNextPage bool
					}
				} `graphql:"notes(first: 100, after: $after)"`
			} `graphql:"mergeRequest(iid: $mrNumber)"`
		} `graphql:"project(fullPath: $project)"`
	}
	variables := map[string]any{
		"project":  graphql.ID(g.project),
		"mrNumber": graphql.String(strconv.Itoa(g.mrNumber)),
		"after":    (*graphql.String)(nil),
	}

	var matching []gitlabComment
	for {
		if err := g.graphqlClient.Query(ctx, &q, variables); err != nil {
			return nil, g.recorder.WrapError(err)
		}
		for _, node := range q.Project.MergeRequest.Notes.Nodes {
			body := string(node.Body)
			if !vcs.HasTagKey(body, g.tag) {
				continue
			}
			matching = append(matching, gitlabComment{
				id:        string(node.ID),
				body:      body,
				createdAt: string(node.CreatedAt),
				url:       string(node.URL),
			})
		}
		if !q.Project.MergeRequest.Notes.PageInfo.HasNextPage {
			break
		}
		variables["after"] = q.Project.MergeRequest.Notes.PageInfo.EndCursor
	}

	return matching, nil
}

func (g *GitLab) callCreateComment(ctx context.Context, body string) (gitlabComment, error) {
	reqData, err := json.Marshal(map[string]any{"body": body})
	if err != nil {
		return gitlabComment{}, fmt.Errorf("marshaling comment body: %w", err)
	}

	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes",
		g.serverURL, url.PathEscape(g.project), g.mrNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(reqData))
	if err != nil {
		return gitlabComment{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := g.httpClient.Do(req)
	if err != nil {
		return gitlabComment{}, vcs.RetryablePostError(fmt.Errorf("creating comment: %w", err))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return gitlabComment{}, vcs.HTTPPostError(res.StatusCode, res.Header, fmt.Errorf("creating comment: %s", res.Status))
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return gitlabComment{}, fmt.Errorf("reading response body: %w", err)
	}

	var resData struct {
		ID        int    `json:"id"`
		CreatedAt string `json:"created_at"`
		Body      string `json:"body"`
	}
	if err := json.Unmarshal(resBody, &resData); err != nil {
		return gitlabComment{}, fmt.Errorf("unmarshaling response body: %w", err)
	}

	return gitlabComment{
		id:        strconv.Itoa(resData.ID),
		body:      resData.Body,
		createdAt: resData.CreatedAt,
		url:       fmt.Sprintf("%s/%s/-/merge_requests/%d#note_%d", g.serverURL, g.project, g.mrNumber, resData.ID),
	}, nil
}

func (g *GitLab) callUpdateComment(ctx context.Context, c gitlabComment, body string) error {
	var m struct {
		UpdateNote struct {
			ClientMutationID graphql.ID `graphql:"clientMutationId"`
		} `graphql:"updateNote(input: $input)"`
	}

	// NOTE: the struct name is significant. shurcooL/graphql derives the GraphQL
	// input type name from the Go type name verbatim, so it must match GitLab's
	// schema type (UpdateNoteInput) exactly, including case.
	type UpdateNoteInput struct {
		ID   graphql.ID     `json:"id"`
		Body graphql.String `json:"body"`
	}

	variables := map[string]any{
		"input": UpdateNoteInput{
			ID:   graphql.String(c.id),
			Body: graphql.String(body),
		},
	}

	return g.recorder.WrapError(g.graphqlClient.Mutate(ctx, &m, variables))
}

func (g *GitLab) callDeleteComment(ctx context.Context, c gitlabComment) error {
	var m struct {
		DestroyNote struct {
			ClientMutationID graphql.ID `graphql:"clientMutationId"`
		} `graphql:"destroyNote(input: $input)"`
	}

	// NOTE: the struct name is significant, see callUpdateComment. Must match
	// GitLab's schema type (DestroyNoteInput) exactly, including case.
	type DestroyNoteInput struct {
		ID graphql.ID `json:"id"`
	}

	variables := map[string]any{
		"input": DestroyNoteInput{ID: graphql.String(c.id)},
	}

	return g.recorder.WrapError(g.graphqlClient.Mutate(ctx, &m, variables))
}

// newClients builds an authenticated HTTP client and a GraphQL client pointed at
// the GitLab GraphQL endpoint for the given server.
func newClients(ctx context.Context, token, serverURL string, tlsConfig *tls.Config) (*http.Client, *graphql.Client, *vcs.StatusRecorder, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	recorder := &vcs.StatusRecorder{Base: transport}
	rawClient := &http.Client{Transport: recorder}
	httpCtx := context.WithValue(ctx, oauth2.HTTPClient, rawClient)

	httpClient := oauth2.NewClient(httpCtx, ts)

	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing server URL: %w", err)
	}
	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}

	return httpClient, graphql.NewClient(fmt.Sprintf("%sapi/graphql", u.String()), httpClient), recorder, nil
}
