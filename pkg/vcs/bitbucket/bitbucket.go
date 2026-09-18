// Package bitbucket implements the vcs.VCS interface for Bitbucket pull
// requests, on both Bitbucket Cloud and Bitbucket Server / Data Center.
//
// Bitbucket renders neither HTML nor hidden markdown comments, so this
// provider uses comment.FlatTemplate and tags its comments with a visible
// italic footer. There is no minimize API, so BehaviorHideAndNew degrades to
// delete-and-new, as it does for GitLab and Azure DevOps.
package bitbucket

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/infracost/vcs/pkg/vcs"
	"github.com/infracost/vcs/pkg/vcs/comment"
	"golang.org/x/oauth2"
)

// cloudHost is Bitbucket Cloud; any other host is Server / Data Center.
const cloudHost = "bitbucket.org"

// cloudAPIURL is the Cloud REST base. Server hangs its API off its own host.
const cloudAPIURL = "https://api.bitbucket.org/2.0"

// maxCommentSize is the Bitbucket comment body limit, in characters (runes).
// comment.Render reserves headroom inside this limit for the footer tag and
// truncation imprecision; do not subtract from it here.
const maxCommentSize = 32768

// validAtSkew is how far ahead of us another run's valid-at may legitimately
// be. The footer tag is visible text, so a copied comment carrying a far-future
// stamp must not suppress every later run.
const validAtSkew = 5 * time.Minute

// invalidTagChars are the characters the visible footer cannot round-trip:
// footerTagContentPattern delimits on "*(...)*" and splits the content on ",".
var invalidTagChars = regexp.MustCompile(`[,()*\r\n]`)

// Options configures a Bitbucket VCS provider.
type Options struct {
	// ServerURL is the base Bitbucket URL. Leave empty for Bitbucket Cloud;
	// set it for Bitbucket Server / Data Center.
	ServerURL string

	// TLSConfig is optional TLS configuration for Bitbucket Server.
	TLSConfig *tls.Config

	// Tag is used to identify Infracost comments. Defaults to "infracost-comment".
	Tag string

	// Template overrides the comment template. If nil, comment.FlatTemplate is
	// used — the default template's HTML does not render on Bitbucket.
	Template *template.Template
}

// Bitbucket implements vcs.VCS for Bitbucket pull requests.
type Bitbucket struct {
	api      api
	isServer bool
	tag      string
	tmpl     *template.Template
}

// New creates a Bitbucket VCS provider for the given repository and pull
// request. `repo` is the full path, "workspace/repo" on Cloud and
// "project/repo" on Server.
func New(ctx context.Context, repo, token string, prNumber int, opts Options) (*Bitbucket, error) {
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("repo %q must have the format workspace/repo", repo)
	}
	// Escaped: a "?" or "#" in a segment would otherwise re-target the request.
	owner, name := url.PathEscape(parts[0]), url.PathEscape(parts[1])

	tag := opts.Tag
	if tag == "" {
		tag = "infracost-comment"
	}
	if invalidTagChars.MatchString(tag) {
		return nil, fmt.Errorf(`tag %q must not contain "," "(" ")" "*" or a newline`, tag)
	}

	httpClient := newClient(ctx, token, opts.TLSConfig)

	tmpl := opts.Template
	if tmpl == nil {
		tmpl = comment.FlatTemplate
	}

	isServer := opts.ServerURL != "" && !isCloudURL(opts.ServerURL)

	b := &Bitbucket{isServer: isServer, tag: tag, tmpl: tmpl}
	if isServer {
		b.api = &serverAPI{
			httpClient: httpClient,
			baseURL: fmt.Sprintf("%s/rest/api/1.0/projects/%s/repos/%s",
				strings.TrimSuffix(opts.ServerURL, "/"), owner, name),
			prNumber: prNumber,
			tag:      tag,
		}
		return b, nil
	}

	b.api = &cloudAPI{
		httpClient: httpClient,
		baseURL:    fmt.Sprintf("%s/repositories/%s/%s", cloudAPIURL, owner, name),
		prNumber:   prNumber,
		tag:        tag,
	}
	return b, nil
}

// GenerateComment renders a pull request comment from the given data.
func (b *Bitbucket) GenerateComment(data comment.Data) (string, error) {
	size, unit := b.MaxCommentSize()
	return comment.Render(b.tmpl, size, unit, b.SourceLink, data)
}

// MaxCommentSize returns the Bitbucket comment body size limit, measured in
// characters (Unicode code points).
func (b *Bitbucket) MaxCommentSize() (int, comment.SizeUnit) {
	return maxCommentSize, comment.SizeUnitRunes
}

// SourceLink returns a URL to a file/line in the Bitbucket web UI. Cloud and
// Server use different shapes: /src/<sha>/<path>#lines-<n> against
// /browse/<path>?at=<sha>#<n>.
func (b *Bitbucket) SourceLink(repoURL, commitSHA, path string, startLine int) string {
	if repoURL == "" || commitSHA == "" || path == "" {
		return ""
	}
	cleanURL := strings.TrimSuffix(repoURL, ".git")
	// Escaped per segment: a "?" or "#" in a file name would otherwise truncate
	// the link, and the Server form carries the revision after the path.
	escapedPath := escapePathSegments(path)

	if b.isServer {
		link := fmt.Sprintf("%s/browse/%s?at=%s", cleanURL, escapedPath, commitSHA)
		if startLine > 0 {
			link = fmt.Sprintf("%s#%d", link, startLine)
		}
		return link
	}

	link := fmt.Sprintf("%s/src/%s/%s", cleanURL, commitSHA, escapedPath)
	if startLine > 0 {
		link = fmt.Sprintf("%s#lines-%d", link, startLine)
	}
	return link
}

// PostComment publishes a comment to the pull request using the given behavior.
// Bitbucket has no minimize API, so BehaviorHideAndNew falls back to
// delete-and-new.
func (b *Bitbucket) PostComment(ctx context.Context, body string, behavior vcs.Behavior) (vcs.PostResult, error) {
	now := time.Now()
	// Footer, not markdown comment: Bitbucket strips the [//]: <> () form the
	// other providers hide their tag in.
	taggedBody := vcs.AddFooterTags(body, b.tag, &now)

	switch behavior {
	case vcs.BehaviorUpdate:
		return b.updateComment(ctx, taggedBody, &now)
	case vcs.BehaviorNew:
		return b.newComment(ctx, taggedBody)
	case vcs.BehaviorHideAndNew, vcs.BehaviorDeleteAndNew:
		return b.deleteAndNewComment(ctx, taggedBody, &now)
	default:
		return vcs.PostResult{}, fmt.Errorf("unknown behavior: %s", behavior)
	}
}

func (b *Bitbucket) updateComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := b.api.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 {
		latest := comments[len(comments)-1]

		if latestValidAt := footerValidAt(latest.body, validAt); validAt != nil && latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one is newer: %s", latest.url)}, nil
		}

		if latest.body == body {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not updating comment since the latest one matches exactly: %s", latest.url)}, nil
		}

		if err := b.api.updateComment(ctx, latest, body); err != nil {
			return vcs.PostResult{}, err
		}
		return vcs.PostResult{Posted: true}, nil
	}

	if _, err := b.api.createComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (b *Bitbucket) newComment(ctx context.Context, body string) (vcs.PostResult, error) {
	if _, err := b.api.createComment(ctx, body); err != nil {
		return vcs.PostResult{}, err
	}
	return vcs.PostResult{Posted: true}, nil
}

func (b *Bitbucket) deleteAndNewComment(ctx context.Context, body string, validAt *time.Time) (vcs.PostResult, error) {
	comments, err := b.api.findMatchingComments(ctx)
	if err != nil {
		return vcs.PostResult{}, err
	}

	if len(comments) > 0 && validAt != nil {
		latest := comments[len(comments)-1]
		if latestValidAt := footerValidAt(latest.body, validAt); latestValidAt != nil && validAt.Before(*latestValidAt) {
			return vcs.PostResult{SkipReason: fmt.Sprintf("not adding comment since the latest one is newer: %s", latest.url)}, nil
		}
	}

	// A failed delete must not sink the post: a concurrent run may have removed
	// the comment already, and the new report still has to land.
	var deleteErrs []error
	for _, c := range comments {
		if err := b.api.deleteComment(ctx, c); err != nil {
			deleteErrs = append(deleteErrs, err)
		}
	}

	if _, err := b.api.createComment(ctx, body); err != nil {
		return vcs.PostResult{}, errors.Join(append(deleteErrs, err)...)
	}
	return vcs.PostResult{Posted: true}, nil
}

// footerValidAt reads the footer valid-at, ignoring a stamp further ahead of now
// than validAtSkew: anyone can copy the visible footer, and a future stamp would
// otherwise stop us posting for good.
func footerValidAt(body string, now *time.Time) *time.Time {
	t := vcs.ExtractFooterValidAt(body)
	if t == nil || now == nil {
		return t
	}
	if t.After(now.Add(validAtSkew)) {
		return nil
	}
	return t
}

// bitbucketComment is a comment we recognise as ours on a pull request.
type bitbucketComment struct {
	id   int64
	body string
	url  string
	// version is Server's optimistic lock. Every mutation must echo it back.
	version int64
}

// sortByID orders matches oldest first. Neither flavour promises an order —
// Server's activity stream is newest first — and the post loop treats the last
// element as the latest comment.
func sortByID(comments []bitbucketComment) {
	sort.Slice(comments, func(i, j int) bool { return comments[i].id < comments[j].id })
}

// api is the per-flavour REST surface. Cloud and Server disagree on paths,
// pagination, request shape and how a comment is identified.
type api interface {
	findMatchingComments(ctx context.Context) ([]bitbucketComment, error)
	createComment(ctx context.Context, body string) (bitbucketComment, error)
	updateComment(ctx context.Context, c bitbucketComment, body string) error
	deleteComment(ctx context.Context, c bitbucketComment) error
}

// newClient builds an authenticated HTTP client. A token containing ":" is
// treated as user:password and sent as Basic; anything else as Bearer.
func newClient(ctx context.Context, token string, tlsConfig *tls.Config) *http.Client {
	accessToken, tokenType := token, "Bearer"
	if strings.Contains(token, ":") {
		accessToken = base64.StdEncoding.EncodeToString([]byte(token))
		tokenType = "Basic"
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	rawClient := &http.Client{Transport: &vcs.StatusRecorder{Base: transport}}
	httpCtx := context.WithValue(ctx, oauth2.HTTPClient, rawClient)

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken, TokenType: tokenType})
	client := oauth2.NewClient(httpCtx, ts)
	// The oauth2 transport re-attaches the token on every hop, so Go's own
	// cross-host header stripping does not apply: stop at the host change.
	client.CheckRedirect = checkRedirect
	return client
}

// checkRedirect refuses a redirect that leaves the host or the scheme the
// request started on, and caps the chain: the default limit only applies when
// this is unset. Same-host https->http would otherwise put the token on the
// wire in plaintext.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) == 0 {
		return nil
	}
	if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return fmt.Errorf("refusing to follow a redirect to %s: the token would be sent off-host", req.URL.Host)
	}
	if !strings.EqualFold(req.URL.Scheme, via[0].URL.Scheme) {
		return fmt.Errorf("refusing to follow a redirect to %s: the token would be sent over %s", req.URL, req.URL.Scheme)
	}
	return nil
}

// isCloudURL reports whether serverURL names Bitbucket Cloud. Compared by host:
// the scheme, a "www." prefix and a trailing path do not pick the flavour.
func isCloudURL(serverURL string) bool {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		// No scheme, so the host parsed as the first path segment.
		host, _, _ = strings.Cut(strings.ToLower(strings.TrimPrefix(u.Path, "/")), "/")
	}
	host = strings.TrimPrefix(host, "www.")

	return host == cloudHost || host == "api."+cloudHost
}

// escapePathSegments percent-escapes each segment of a repo-relative path,
// leaving the separators alone.
func escapePathSegments(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// doJSON runs the request and returns the body, mapping transport and status
// failures onto the retry classification the post loop reads.
func doJSON(client *http.Client, req *http.Request, action string, wantStatus ...int) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, vcs.RetryablePostError(fmt.Errorf("%s: %w", action, err))
	}
	defer func() { _ = res.Body.Close() }()

	ok := false
	for _, want := range wantStatus {
		if res.StatusCode == want {
			ok = true
			break
		}
	}
	if !ok {
		return nil, vcs.HTTPPostError(res.StatusCode, res.Header, fmt.Errorf("%s: %s", action, res.Status))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return body, nil
}

func jsonRequest(ctx context.Context, method, url string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshaling comment body: %w", err)
		}
		body = bytes.NewBuffer(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
