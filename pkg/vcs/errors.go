package vcs

import (
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// PostError wraps an error returned while posting a comment with metadata that
// lets callers decide whether the operation is worth retrying. Providers return
// it from PostComment (and its helpers); recover it with errors.As:
//
//	var postErr *vcs.PostError
//	if errors.As(err, &postErr) && postErr.Retryable {
//		time.Sleep(postErr.RetryAfter) // RetryAfter may be 0
//		// ...try again
//	}
type PostError struct {
	// StatusCode is the HTTP status code associated with the error, or 0 if the
	// error did not originate from an HTTP response (e.g. a network failure or a
	// client-side error before any request was sent).
	StatusCode int

	// Retryable reports whether retrying the operation might succeed. It is true
	// for transient HTTP statuses (429 and most 5xx), network-level failures,
	// and responses that carry a rate-limit signal (a Retry-After header or an
	// exhausted rate-limit budget) even when the status is 200 — as GraphQL APIs
	// return for rate limiting.
	Retryable bool

	// RetryAfter is the Retry-After header, or time until an exhausted budget resets.
	// Derived values are capped at maxRetryAfter; it is 0 with no usable hint.
	RetryAfter time.Duration

	// Err is the underlying error.
	Err error
}

func (e *PostError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *PostError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HTTPPostError wraps err in a *PostError carrying the given HTTP status code,
// deriving Retryable from the status and from any rate-limit hints in header
// (which may be nil). It returns nil if err is nil.
func HTTPPostError(statusCode int, header http.Header, err error) error {
	if err == nil {
		return nil
	}

	retryAfter, resetAt, exhausted := rateLimitHints(header)
	delay := retryDelay(retryAfter, resetAt)
	return &PostError{
		StatusCode: statusCode,
		Retryable:  retryableStatus(statusCode) || delay > 0 || exhausted,
		RetryAfter: delay,
		Err:        err,
	}
}

// RetryablePostError wraps a non-HTTP error (such as a network or transport
// failure) that is safe to retry. StatusCode is left as 0. It returns nil if
// err is nil.
func RetryablePostError(err error) error {
	if err == nil {
		return nil
	}
	return &PostError{Retryable: true, Err: err}
}

// IsRetryable reports whether err (or any error it wraps) is a *PostError marked
// retryable. Errors that are not *PostError are treated as non-retryable.
func IsRetryable(err error) bool {
	var postErr *PostError
	return errors.As(err, &postErr) && postErr.Retryable
}

// retryableStatus reports whether an HTTP status code represents a transient
// failure that is worth retrying.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	default:
		return false
	}
}

// parseRetryAfter reads the Retry-After header, which is either a number of
// seconds or an HTTP date. It returns 0 if the header is absent or unparseable.
func parseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}

	v := header.Get("Retry-After")
	if v == "" {
		return 0
	}

	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}

	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}

	return 0
}

// maxRetryAfter caps a reset-derived delay to GitHub's longest primary window.
// A GHES instance, self-hosted GitLab, or proxy can send a far-future reset.
const maxRetryAfter = time.Hour

// rateLimitHints returns the Retry-After hint, reset time, and exhaustion state.
// GitHub uses X-RateLimit-* names and GitLab un-prefixed names; GraphQL APIs can
// return a primary rate limit as HTTP 200, so these headers are the retry signal.
func rateLimitHints(header http.Header) (time.Duration, time.Time, bool) {
	retryAfter := parseRetryAfter(header)
	if header == nil {
		return retryAfter, time.Time{}, false
	}

	// Reset is read with the same prefix as the Remaining that matched, so a
	// GitHub response is never measured against a GitLab-shaped reset.
	for _, prefix := range []string{"X-RateLimit-", "RateLimit-"} {
		if n, err := strconv.Atoi(header.Get(prefix + "Remaining")); err != nil || n != 0 {
			continue
		}
		// A zeroed limit is not a real budget: GitHub zeroes the whole set on auth
		// failures, where retrying cannot help.
		if n, err := strconv.Atoi(header.Get(prefix + "Limit")); err == nil && n <= 0 {
			continue
		}
		return retryAfter, rateLimitReset(header, prefix), true
	}

	return retryAfter, time.Time{}, false
}

// rateLimitReset converts a <prefix>Reset epoch value into local time.
// It uses the response Date to avoid clock skew and returns zero if unparseable.
func rateLimitReset(header http.Header, prefix string) time.Time {
	secs, err := strconv.ParseInt(header.Get(prefix+"Reset"), 10, 64)
	if err != nil {
		return time.Time{}
	}

	reset := time.Unix(secs, 0)
	if date, err := http.ParseTime(header.Get("Date")); err == nil {
		return time.Now().Add(reset.Sub(date))
	}

	return reset
}

// retryDelay picks the wait to report: an explicit Retry-After wins, otherwise
// the time until resetAt, capped at maxRetryAfter. A reset that has already
// passed gives 0, which is right — the window has rolled over.
func retryDelay(retryAfter time.Duration, resetAt time.Time) time.Duration {
	if retryAfter > 0 || resetAt.IsZero() {
		return retryAfter
	}

	d := time.Until(resetAt)
	if d <= 0 {
		return 0
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}

	return d
}

// StatusRecorder is an http.RoundTripper that remembers the outcome of the most
// recent response. GraphQL clients hide the HTTP layer, so wrapping their
// transport with a StatusRecorder lets PostComment recover the status code, the
// rate-limit headers, and any Retry-After hint of a failed request and decide
// whether the error is retryable via WrapError.
type StatusRecorder struct {
	// Base is the underlying transport. If nil, http.DefaultTransport is used.
	Base http.RoundTripper

	mu   sync.Mutex
	last recorded
}

// recorded holds the retry-relevant facts of the most recent response.
type recorded struct {
	statusCode  int
	transportEr bool
	retryAfter  time.Duration
	// resetAt is absolute so that the wait cannot go stale between RoundTrip and
	// WrapError.
	resetAt     time.Time
	rateLimited bool
}

func (s *StatusRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	base := s.Base
	if base == nil {
		base = http.DefaultTransport
	}

	resp, err := base.RoundTrip(req)

	s.mu.Lock()
	if err != nil {
		s.last = recorded{transportEr: true}
	} else {
		retryAfter, resetAt, exhausted := rateLimitHints(resp.Header)
		s.last = recorded{
			statusCode:  resp.StatusCode,
			retryAfter:  retryAfter,
			resetAt:     resetAt,
			rateLimited: exhausted,
		}
	}
	s.mu.Unlock()

	return resp, err
}

// WrapError converts an error from a GraphQL client into a *PostError using the
// recorded outcome of the most recent HTTP response. A transport-level failure
// (no response received) is treated as retryable, as is a rate-limited response
// even when its status is 200. It returns nil if err is nil.
func (s *StatusRecorder) WrapError(err error) error {
	if err == nil {
		return nil
	}

	s.mu.Lock()
	last := s.last
	s.mu.Unlock()

	if last.transportEr {
		return RetryablePostError(err)
	}

	delay := retryDelay(last.retryAfter, last.resetAt)
	return &PostError{
		StatusCode: last.statusCode,
		Retryable:  retryableStatus(last.statusCode) || delay > 0 || last.rateLimited,
		RetryAfter: delay,
		Err:        err,
	}
}
