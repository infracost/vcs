package vcs

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestHTTPPostError(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		wantRetryable bool
	}{
		{"401 unauthorized", http.StatusUnauthorized, false},
		{"403 forbidden", http.StatusForbidden, false},
		{"404 not found", http.StatusNotFound, false},
		{"422 unprocessable", http.StatusUnprocessableEntity, false},
		{"408 timeout", http.StatusRequestTimeout, true},
		{"429 too many requests", http.StatusTooManyRequests, true},
		{"500 internal", http.StatusInternalServerError, true},
		{"502 bad gateway", http.StatusBadGateway, true},
		{"503 unavailable", http.StatusServiceUnavailable, true},
		{"504 gateway timeout", http.StatusGatewayTimeout, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := HTTPPostError(tt.statusCode, nil, fmt.Errorf("boom"))

			var postErr *PostError
			if !errors.As(err, &postErr) {
				t.Fatalf("expected a *PostError, got %T", err)
			}
			if postErr.StatusCode != tt.statusCode {
				t.Errorf("StatusCode = %d, want %d", postErr.StatusCode, tt.statusCode)
			}
			if postErr.Retryable != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", postErr.Retryable, tt.wantRetryable)
			}
			if got := IsRetryable(err); got != tt.wantRetryable {
				t.Errorf("IsRetryable = %v, want %v", got, tt.wantRetryable)
			}
			if postErr.Error() != "boom" {
				t.Errorf("Error() = %q, want %q", postErr.Error(), "boom")
			}
		})
	}
}

func TestHTTPPostError_nil(t *testing.T) {
	if err := HTTPPostError(http.StatusServiceUnavailable, nil, nil); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestHTTPPostError_retryAfterHeader(t *testing.T) {
	header := http.Header{"Retry-After": []string{"30"}}
	// A 403 is normally non-retryable, but a Retry-After header (as GitHub sends
	// for secondary rate limits) makes it worth retrying.
	err := HTTPPostError(http.StatusForbidden, header, fmt.Errorf("rate limited"))

	var postErr *PostError
	if !errors.As(err, &postErr) {
		t.Fatalf("expected a *PostError, got %T", err)
	}
	if !postErr.Retryable {
		t.Error("expected Retryable to be true when Retry-After is present")
	}
	if postErr.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", postErr.RetryAfter)
	}
}

func TestHTTPPostError_rateLimitExhausted(t *testing.T) {
	// GitHub's GraphQL primary rate limit returns HTTP 200 with X-RateLimit-Remaining: 0.
	header := http.Header{"X-Ratelimit-Remaining": []string{"0"}}
	err := HTTPPostError(http.StatusOK, header, fmt.Errorf("API rate limit exceeded"))

	if !IsRetryable(err) {
		t.Error("expected an exhausted rate-limit budget on a 200 to be retryable")
	}
}

func TestHTTPPostError_rateLimitRemaining(t *testing.T) {
	// A 200 with budget left is not retryable.
	header := http.Header{"X-Ratelimit-Remaining": []string{"42"}}
	err := HTTPPostError(http.StatusOK, header, fmt.Errorf("some graphql error"))

	if IsRetryable(err) {
		t.Error("expected a 200 with remaining budget to be non-retryable")
	}
}

func TestRetryablePostError(t *testing.T) {
	if err := RetryablePostError(nil); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	inner := errors.New("connection refused")
	err := RetryablePostError(inner)

	var postErr *PostError
	if !errors.As(err, &postErr) {
		t.Fatalf("expected a *PostError, got %T", err)
	}
	if postErr.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", postErr.StatusCode)
	}
	if !postErr.Retryable {
		t.Error("expected Retryable to be true for a network error")
	}
	if !errors.Is(err, inner) {
		t.Error("expected the wrapped error to be unwrappable via errors.Is")
	}
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestStatusRecorder_WrapError(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		header        http.Header
		transportErr  error
		wantRetryable bool
		wantStatus    int
		wantAfter     time.Duration
	}{
		{
			name:          "503 is retryable",
			status:        http.StatusServiceUnavailable,
			wantRetryable: true,
			wantStatus:    http.StatusServiceUnavailable,
		},
		{
			name:          "401 is not retryable",
			status:        http.StatusUnauthorized,
			wantRetryable: false,
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "200 graphql error with no signal is not retryable",
			status:        http.StatusOK,
			wantRetryable: false,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "200 with exhausted rate limit is retryable",
			status:        http.StatusOK,
			header:        http.Header{"X-Ratelimit-Remaining": []string{"0"}},
			wantRetryable: true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "200 with Retry-After is retryable",
			status:        http.StatusOK,
			header:        http.Header{"Retry-After": []string{"5"}},
			wantRetryable: true,
			wantStatus:    http.StatusOK,
			wantAfter:     5 * time.Second,
		},
		{
			name:          "transport error is retryable with status 0",
			transportErr:  fmt.Errorf("connection refused"),
			wantRetryable: true,
			wantStatus:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &StatusRecorder{Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tt.transportErr != nil {
					return nil, tt.transportErr
				}
				return &http.Response{StatusCode: tt.status, Header: tt.header, Body: http.NoBody}, nil
			})}

			req, _ := http.NewRequest(http.MethodPost, "http://example.com/graphql", nil)
			resp, _ := rec.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			err := rec.WrapError(fmt.Errorf("graphql failed"))
			var postErr *PostError
			if !errors.As(err, &postErr) {
				t.Fatalf("expected a *PostError, got %T", err)
			}
			if postErr.Retryable != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", postErr.Retryable, tt.wantRetryable)
			}
			if postErr.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", postErr.StatusCode, tt.wantStatus)
			}
			if postErr.RetryAfter != tt.wantAfter {
				t.Errorf("RetryAfter = %v, want %v", postErr.RetryAfter, tt.wantAfter)
			}
		})
	}
}

// header builds an http.Header from name/value pairs, via Set so that the
// non-canonical rate-limit names are canonicalised the way a real response is.
func header(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

// epoch renders now+d as the epoch seconds a reset header carries.
func epoch(d time.Duration) string {
	return strconv.FormatInt(time.Now().Add(d).Unix(), 10)
}

// TestRateLimitReset covers deriving RetryAfter from a rate-limit reset header
// when the server sends no Retry-After. Every case runs through both entry
// points, which reach the same value along different paths.
func TestRateLimitReset(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		header        http.Header
		wantRetryable bool
		wantAfter     time.Duration // exact, when wantAfterMax is 0
		wantAfterMin  time.Duration // exclusive
		wantAfterMax  time.Duration // inclusive
	}{
		{
			name:          "Retry-After wins over the reset header",
			status:        http.StatusOK,
			header:        header("Retry-After", "30", "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(time.Hour)),
			wantRetryable: true,
			wantAfter:     30 * time.Second,
		},
		{
			name:          "reset alone derives the wait",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Limit", "5000", "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(40*time.Minute)),
			wantRetryable: true,
			wantAfterMin:  35 * time.Minute,
			wantAfterMax:  40 * time.Minute,
		},
		{
			name:          "reset in the past is 0 but still retryable",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(-5*time.Minute)),
			wantRetryable: true,
		},
		{
			name:   "reset with budget left is ignored",
			status: http.StatusOK,
			header: header("X-RateLimit-Limit", "5000", "X-RateLimit-Remaining", "42", "X-RateLimit-Reset", epoch(time.Hour)),
		},
		{
			name:   "negative remaining is ignored",
			status: http.StatusOK,
			header: header("X-RateLimit-Remaining", "-1", "X-RateLimit-Reset", epoch(time.Hour)),
		},
		{
			name:          "a reset beyond the cap clamps to the cap",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(6*time.Hour)),
			wantRetryable: true,
			wantAfter:     time.Hour,
		},
		{
			name:   "a zeroed limit is not an exhausted budget",
			status: http.StatusUnauthorized,
			header: header("X-RateLimit-Limit", "0", "X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(time.Hour)),
		},
		{
			name:          "an absent limit header changes nothing",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(40*time.Minute)),
			wantRetryable: true,
			wantAfterMin:  35 * time.Minute,
			wantAfterMax:  40 * time.Minute,
		},
		{
			name:          "azure devops sends epoch seconds under the X- names",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(40*time.Minute)),
			wantRetryable: true,
			wantAfterMin:  35 * time.Minute,
			wantAfterMax:  40 * time.Minute,
		},
		{
			name:          "bitbucket cloud sends a limit but no remaining or reset",
			status:        http.StatusTooManyRequests,
			header:        header("X-RateLimit-Limit", "5000", "X-RateLimit-NearLimit", "true"),
			wantRetryable: true,
		},
		{
			name:          "gitlab casing is read against its own reset",
			status:        http.StatusOK,
			header:        header("RateLimit-Limit", "2000", "RateLimit-Remaining", "0", "RateLimit-Reset", epoch(40*time.Minute)),
			wantRetryable: true,
			wantAfterMin:  35 * time.Minute,
			wantAfterMax:  40 * time.Minute,
		},
		{
			name:   "a github reset is not read against a gitlab remaining",
			status: http.StatusOK,
			header: header("RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(40*time.Minute)),
			// Retryable on the exhausted budget, but with no paired reset to derive from.
			wantRetryable: true,
		},
		{
			name:          "an unparseable reset falls back to 0",
			status:        http.StatusOK,
			header:        header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "soon"),
			wantRetryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := func(t *testing.T, entry string, postErr *PostError) {
				t.Helper()
				if postErr.Retryable != tt.wantRetryable {
					t.Errorf("%s: Retryable = %v, want %v", entry, postErr.Retryable, tt.wantRetryable)
				}
				if tt.wantAfterMax != 0 {
					if postErr.RetryAfter <= tt.wantAfterMin || postErr.RetryAfter > tt.wantAfterMax {
						t.Errorf("%s: RetryAfter = %v, want in (%v, %v]", entry, postErr.RetryAfter, tt.wantAfterMin, tt.wantAfterMax)
					}
				} else if postErr.RetryAfter != tt.wantAfter {
					t.Errorf("%s: RetryAfter = %v, want %v", entry, postErr.RetryAfter, tt.wantAfter)
				}
			}

			var postErr *PostError
			if !errors.As(HTTPPostError(tt.status, tt.header, fmt.Errorf("boom")), &postErr) {
				t.Fatal("HTTPPostError: expected a *PostError")
			}
			check(t, "HTTPPostError", postErr)

			rec := &StatusRecorder{Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Header: tt.header, Body: http.NoBody}, nil
			})}
			req, _ := http.NewRequest(http.MethodPost, "http://example.com/graphql", nil)
			resp, _ := rec.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}

			postErr = nil
			if !errors.As(rec.WrapError(fmt.Errorf("boom")), &postErr) {
				t.Fatal("WrapError: expected a *PostError")
			}
			check(t, "WrapError", postErr)
		})
	}
}

func TestRateLimitReset_measuredAgainstDateHeader(t *testing.T) {
	// A runner whose clock is 30m behind the server must still wait 40m, not 70m.
	date := time.Now().Add(30 * time.Minute)
	h := header(
		"Date", date.UTC().Format(http.TimeFormat),
		"X-RateLimit-Remaining", "0",
		"X-RateLimit-Reset", strconv.FormatInt(date.Add(40*time.Minute).Unix(), 10),
	)

	var postErr *PostError
	if !errors.As(HTTPPostError(http.StatusOK, h, fmt.Errorf("rate limited")), &postErr) {
		t.Fatal("expected a *PostError")
	}
	if postErr.RetryAfter <= 35*time.Minute || postErr.RetryAfter > 40*time.Minute {
		t.Errorf("RetryAfter = %v, want in (35m, 40m]", postErr.RetryAfter)
	}
}

func TestStatusRecorder_WrapError_resetStaysFresh(t *testing.T) {
	h := header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", epoch(2*time.Second))
	rec := &StatusRecorder{Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: h, Body: http.NoBody}, nil
	})}

	req, _ := http.NewRequest(http.MethodPost, "http://example.com/graphql", nil)
	resp, _ := rec.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}

	var postErr *PostError
	if !errors.As(rec.WrapError(fmt.Errorf("boom")), &postErr) {
		t.Fatal("expected a *PostError")
	}
	if postErr.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v immediately after the response, want > 0", postErr.RetryAfter)
	}

	time.Sleep(2100 * time.Millisecond)

	postErr = nil
	if !errors.As(rec.WrapError(fmt.Errorf("boom")), &postErr) {
		t.Fatal("expected a *PostError")
	}
	if postErr.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v once the reset has passed, want 0", postErr.RetryAfter)
	}
	if !postErr.Retryable {
		t.Error("expected a rolled-over rate limit to stay retryable")
	}
}

func TestStatusRecorder_WrapError_nil(t *testing.T) {
	rec := &StatusRecorder{}
	if err := rec.WrapError(nil); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestIsRetryable_nonPostError(t *testing.T) {
	if IsRetryable(errors.New("plain error")) {
		t.Error("expected a plain error to be treated as non-retryable")
	}
	if IsRetryable(nil) {
		t.Error("expected nil to be non-retryable")
	}
}
