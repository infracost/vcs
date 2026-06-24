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

	// RetryAfter is how long the server asked us to wait before retrying, parsed
	// from the Retry-After header. It is 0 when no such hint was provided.
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

	retryAfter := parseRetryAfter(header)
	return &PostError{
		StatusCode: statusCode,
		Retryable:  retryableStatus(statusCode) || retryAfter > 0 || rateLimitExhausted(header),
		RetryAfter: retryAfter,
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

// rateLimitExhausted reports whether a rate-limit-remaining header is present
// and reads as zero. GitHub uses X-RateLimit-Remaining and GitLab uses
// RateLimit-Remaining; both return HTTP 200 with a GraphQL error when a primary
// rate limit is hit, so this is the only reliable retry signal in that case.
func rateLimitExhausted(header http.Header) bool {
	if header == nil {
		return false
	}

	for _, name := range []string{"X-RateLimit-Remaining", "RateLimit-Remaining"} {
		v := header.Get(name)
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil && n <= 0 {
			return true
		}
	}

	return false
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
		s.last = recorded{
			statusCode:  resp.StatusCode,
			retryAfter:  parseRetryAfter(resp.Header),
			rateLimited: rateLimitExhausted(resp.Header),
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

	return &PostError{
		StatusCode: last.statusCode,
		Retryable:  retryableStatus(last.statusCode) || last.retryAfter > 0 || last.rateLimited,
		RetryAfter: last.retryAfter,
		Err:        err,
	}
}
