package github

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Options configures a GitHub VCS provider.
type Options struct {
	// APIURL is the GitHub API URL. Leave empty for github.com;
	// set for GitHub Enterprise.
	APIURL string

	// TLSConfig is optional TLS configuration for GitHub Enterprise.
	TLSConfig *tls.Config

	// Tag is used to identify Infracost comments. Defaults to "infracost-comment".
	Tag string

	// Template overrides the default comment template. If nil, the
	// default template is used.
	Template *template.Template
}

// GitHub implements vcs.VCS for GitHub pull requests using the GraphQL API.
type GitHub struct {
	client   *githubv4.Client
	owner    string
	repo     string
	prNumber int32
	tag      string
	tmpl     *template.Template

	// prNodeID is lazily fetched on first use.
	prNodeID string
}

// New creates a new GitHub VCS provider for the given repository and pull request.
func New(ctx context.Context, owner, repo, token string, prNumber int32, opts Options) (*GitHub, error) {
	client, err := newClient(ctx, token, opts.APIURL, opts.TLSConfig)
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

	return &GitHub{
		client:   client,
		owner:    owner,
		repo:     repo,
		prNumber: prNumber,
		tag:      tag,
		tmpl:     tmpl,
	}, nil
}

// GenerateComment renders a PR comment from the given data.
func (g *GitHub) GenerateComment(data comment.Data) (string, error) {
	return comment.Render(g.tmpl, true, 65000, data)
}

// PostComment publishes a comment to the pull request using the given behavior.
func (g *GitHub) PostComment(ctx context.Context, body string, behavior vcs.Behavior) (vcs.PostResult, error) {
	now := time.Now()
	taggedBody := vcs.AddMarkdownTags(body, g.tag, &now)

	switch behavior {
	case vcs.BehaviorUpdate:
		return g.updateComment(ctx, taggedBody, &now)
	case vcs.BehaviorNew:
		return g.newComment(ctx, taggedBody)
	case vcs.BehaviorHideAndNew:
		return g.hideAndNewComment(ctx, taggedBody, &now)
	case vcs.BehaviorDeleteAndNew:
		return g.deleteAndNewComment(ctx, taggedBody, &now)
	default:
		return vcs.PostResult{}, fmt.Errorf("unknown behavior: %s", behavior)
	}
}

func (g *GitHub) updateComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
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

	if err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (g *GitHub) newComment(ctx context.Context, body string) (vcs.PostResult, error) {
	if err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (g *GitHub) hideAndNewComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
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
		if !c.isMinimized {
			if err := g.callHideComment(ctx, c); err != nil {
				return vcs.PostResult{}, err
			}
		}
	}

	if err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (g *GitHub) deleteAndNewComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
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

	if err := g.callCreateComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

// githubComment represents a comment found on a GitHub pull request.
type githubComment struct {
	globalID    string
	body        string
	createdAt   time.Time
	url         string
	isMinimized bool
}

// findMatchingComments queries the GitHub GraphQL API for PR comments matching our tag.
func (g *GitHub) findMatchingComments(ctx context.Context) ([]githubComment, error) {
	var q struct {
		Repository struct {
			PullRequest struct {
				Comments struct {
					Nodes []struct {
						ID          githubv4.String
						URL         githubv4.String
						CreatedAt   githubv4.DateTime
						PublishedAt githubv4.DateTime
						Body        githubv4.String
						IsMinimized githubv4.Boolean
					}
					PageInfo struct {
						EndCursor   githubv4.String
						HasNextPage bool
					}
				} `graphql:"comments(first: 100, after: $after)"`
			} `graphql:"pullRequest(number: $prNumber)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	variables := map[string]interface{}{
		"owner":    githubv4.String(g.owner),
		"repo":     githubv4.String(g.repo),
		"prNumber": githubv4.Int(g.prNumber),
		"after":    (*githubv4.String)(nil),
	}

	var matching []githubComment
	for {
		if err := g.client.Query(ctx, &q, variables); err != nil {
			return nil, err
		}
		for _, node := range q.Repository.PullRequest.Comments.Nodes {
			body := string(node.Body)
			if !vcs.HasTagKey(body, g.tag) {
				continue
			}
			createdAt := node.PublishedAt
			if createdAt.IsZero() {
				createdAt = node.CreatedAt
			}
			matching = append(matching, githubComment{
				globalID:    string(node.ID),
				body:        body,
				createdAt:   createdAt.Time,
				url:         string(node.URL),
				isMinimized: bool(node.IsMinimized),
			})
		}
		if !q.Repository.PullRequest.Comments.PageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.NewString(q.Repository.PullRequest.Comments.PageInfo.EndCursor)
	}

	return matching, nil
}

// getPRNodeID fetches and caches the PR's GraphQL node ID, needed for addComment.
func (g *GitHub) getPRNodeID(ctx context.Context) (string, error) {
	if g.prNodeID != "" {
		return g.prNodeID, nil
	}

	var q struct {
		Repository struct {
			PullRequest struct {
				ID githubv4.String
			} `graphql:"pullRequest(number: $prNumber)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	variables := map[string]interface{}{
		"owner":    githubv4.String(g.owner),
		"repo":     githubv4.String(g.repo),
		"prNumber": githubv4.Int(g.prNumber),
	}

	if err := g.client.Query(ctx, &q, variables); err != nil {
		return "", fmt.Errorf("fetching PR node ID: %w", err)
	}

	g.prNodeID = string(q.Repository.PullRequest.ID)
	return g.prNodeID, nil
}

func (g *GitHub) callCreateComment(ctx context.Context, body string) error {
	prNodeID, err := g.getPRNodeID(ctx)
	if err != nil {
		return err
	}

	var m struct {
		AddComment struct {
			ClientMutationId githubv4.ID //nolint
		} `graphql:"addComment(input: $input)"`
	}
	input := githubv4.AddCommentInput{
		SubjectID: githubv4.ID(prNodeID),
		Body:      githubv4.String(body),
	}
	return g.client.Mutate(ctx, &m, input, nil)
}

func (g *GitHub) callUpdateComment(ctx context.Context, c githubComment, body string) error {
	var m struct {
		UpdateIssueComment struct {
			ClientMutationId githubv4.ID //nolint
		} `graphql:"updateIssueComment(input: $input)"`
	}
	input := githubv4.UpdateIssueCommentInput{
		ID:   githubv4.NewString(githubv4.String(c.globalID)),
		Body: githubv4.String(body),
	}
	return g.client.Mutate(ctx, &m, input, nil)
}

func (g *GitHub) callDeleteComment(ctx context.Context, c githubComment) error {
	var m struct {
		DeleteIssueComment struct {
			ClientMutationId githubv4.ID //nolint
		} `graphql:"deleteIssueComment(input: $input)"`
	}
	input := githubv4.DeleteIssueCommentInput{
		ID: githubv4.NewString(githubv4.String(c.globalID)),
	}
	return g.client.Mutate(ctx, &m, input, nil)
}

func (g *GitHub) callHideComment(ctx context.Context, c githubComment) error {
	var m struct {
		MinimizeComment struct {
			ClientMutationId githubv4.ID //nolint
		} `graphql:"minimizeComment(input: $input)"`
	}
	input := githubv4.MinimizeCommentInput{
		SubjectID:  githubv4.NewString(githubv4.String(c.globalID)),
		Classifier: githubv4.ReportedContentClassifiersOutdated,
	}
	return g.client.Mutate(ctx, &m, input, nil)
}

// newClient creates a GitHub GraphQL client.
func newClient(ctx context.Context, token, apiURL string, tlsConfig *tls.Config) (*githubv4.Client, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	httpClient := &http.Client{Transport: transport}
	httpCtx := context.WithValue(ctx, oauth2.HTTPClient, httpClient)
	tc := oauth2.NewClient(httpCtx, ts)

	// Standard GitHub.com
	if apiURL == "" || apiURL == "https://api.github.com" {
		return githubv4.NewClient(tc), nil
	}

	// GitHub Enterprise — append /api/graphql to the base URL.
	graphqlURL := strings.TrimRight(apiURL, "/") + "/api/graphql"
	return githubv4.NewEnterpriseClient(graphqlURL, tc), nil
}
