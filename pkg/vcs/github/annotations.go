package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"github.com/shurcooL/githubv4"
)

type annotationThread struct {
	id       string
	key      string
	resolved bool
	outdated bool
}

// PostAnnotations posts one review comment per annotation. A key that already
// has a current thread is not posted again. Threads whose key is gone are
// resolved. Outdated threads, whose line has since changed, are resolved and
// posted again on the new line.
func (g *GitHub) PostAnnotations(ctx context.Context, annotations []comment.Annotation) (vcs.AnnotationResult, error) {
	if g.headSHA == "" {
		return vcs.AnnotationResult{SkipReason: "no head commit to comment on"}, nil
	}

	threads, err := g.findAnnotationThreads(ctx)
	if err != nil {
		return vcs.AnnotationResult{}, err
	}

	existing := map[string]annotationThread{}
	for _, t := range threads {
		if !t.outdated {
			existing[t.key] = t
		}
	}

	var res vcs.AnnotationResult
	wanted := map[string]bool{}

	for _, a := range annotations {
		wanted[a.Key] = true
		if t, ok := existing[a.Key]; ok {
			if t.resolved {
				if err := g.callResolveThread(ctx, t.id, false); err != nil {
					return res, err
				}
			}
			continue
		}
		if err := g.callCreateReviewComment(ctx, a); err != nil {
			if vcs.IsRejected(err) {
				res.Rejected++
				continue
			}
			return res, err
		}
		res.Posted++
	}

	for _, t := range threads {
		if t.resolved {
			continue
		}
		if wanted[t.key] && !t.outdated {
			continue
		}
		if err := g.callResolveThread(ctx, t.id, true); err != nil {
			return res, err
		}
		if !wanted[t.key] {
			res.Resolved++
		}
	}

	return res, nil
}

func (g *GitHub) findAnnotationThreads(ctx context.Context) ([]annotationThread, error) {
	var q struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID         githubv4.String
						IsResolved githubv4.Boolean
						IsOutdated githubv4.Boolean
						Comments   struct {
							Nodes []struct {
								Body githubv4.String
							}
						} `graphql:"comments(first: 1)"`
					}
					PageInfo struct {
						EndCursor   githubv4.String
						HasNextPage bool
					}
				} `graphql:"reviewThreads(first: 100, after: $after)"`
			} `graphql:"pullRequest(number: $prNumber)"`
		} `graphql:"repository(owner: $owner, name: $repo)"`
	}
	variables := map[string]interface{}{
		"owner":    githubv4.String(g.owner),
		"repo":     githubv4.String(g.repo),
		"prNumber": githubv4.Int(g.prNumber),
		"after":    (*githubv4.String)(nil),
	}

	var threads []annotationThread
	for {
		if err := g.client.Query(ctx, &q, variables); err != nil {
			return nil, g.recorder.WrapError(err)
		}
		for _, node := range q.Repository.PullRequest.ReviewThreads.Nodes {
			if len(node.Comments.Nodes) == 0 {
				continue
			}
			key, ok := vcs.ExtractAnnotationKey(string(node.Comments.Nodes[0].Body), g.tag)
			if !ok {
				continue
			}
			threads = append(threads, annotationThread{
				id:       string(node.ID),
				key:      key,
				resolved: bool(node.IsResolved),
				outdated: bool(node.IsOutdated),
			})
		}
		if !q.Repository.PullRequest.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		variables["after"] = githubv4.NewString(q.Repository.PullRequest.ReviewThreads.PageInfo.EndCursor)
	}
	return threads, nil
}

func (g *GitHub) callCreateReviewComment(ctx context.Context, a comment.Annotation) error {
	reqData, err := json.Marshal(map[string]any{
		"body":      vcs.AddAnnotationTag(a.Markdown(), g.tag, a.Key),
		"commit_id": g.headSHA,
		"path":      a.Path,
		"line":      a.Line,
		"side":      "RIGHT",
	})
	if err != nil {
		return fmt.Errorf("marshaling review comment: %w", err)
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments", g.restURL, g.owner, g.repo, g.prNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBuffer(reqData))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := g.restClient.Do(req)
	if err != nil {
		return vcs.RetryablePostError(fmt.Errorf("creating review comment: %w", err))
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		return vcs.HTTPPostError(res.StatusCode, res.Header, fmt.Errorf("creating review comment: %s", res.Status))
	}
	return nil
}

func (g *GitHub) callResolveThread(ctx context.Context, id string, resolve bool) error {
	if resolve {
		var m struct {
			ResolveReviewThread struct {
				ClientMutationId githubv4.String //nolint
			} `graphql:"resolveReviewThread(input: $input)"`
		}
		return g.recorder.WrapError(g.client.Mutate(ctx, &m, githubv4.ResolveReviewThreadInput{ThreadID: githubv4.ID(id)}, nil))
	}
	var m struct {
		UnresolveReviewThread struct {
			ClientMutationId githubv4.String //nolint
		} `graphql:"unresolveReviewThread(input: $input)"`
	}
	return g.recorder.WrapError(g.client.Mutate(ctx, &m, githubv4.UnresolveReviewThreadInput{ThreadID: githubv4.ID(id)}, nil))
}
