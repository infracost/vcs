package vcs

import (
	"errors"
	"fmt"
	"net/http"
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
