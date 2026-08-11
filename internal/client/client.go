// Package client is a thin wrapper over the Aikido REST API. It holds the
// authenticated HTTP client and base URL, and centralizes request building,
// JSON encoding/decoding, and error handling so resources stay focused on
// mapping Terraform state to API calls.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://app.aikido.dev/api"

// The API allows 20 calls/min per workspace and returns 429 with a Retry-After
// header when exceeded. maxRetries bounds how many times a single call waits and
// retries; maxRetryDelay caps any single wait so a hostile header can't hang us.
const (
	maxRetries               = 4
	maxRetryDelay            = 60 * time.Second
	DefaultRequestsPerMinute = 20
	MaxRequestsPerMinute     = 100
	requestBurst             = 10
)

type Client struct {
	http    *http.Client
	baseURL string
	limiter *rate.Limiter
}

// Option configures a Client.
type Option func(*Client)

// WithRateLimiter overrides the default request rate limiter, chiefly so tests
// can disable pacing with rate.NewLimiter(rate.Inf, 1).
func WithRateLimiter(limiter *rate.Limiter) Option {
	return func(c *Client) { c.limiter = limiter }
}

// WithRequestsPerMinute paces outbound requests to the given rate. Values are
// clamped to [DefaultRequestsPerMinute, MaxRequestsPerMinute]: anything below
// the standard rate snaps up to DefaultRequestsPerMinute.
func WithRequestsPerMinute(rpm int) Option {
	return func(c *Client) {
		if rpm < DefaultRequestsPerMinute {
			rpm = DefaultRequestsPerMinute
		}
		if rpm > MaxRequestsPerMinute {
			rpm = MaxRequestsPerMinute
		}
		c.limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rpm)), requestBurst)
	}
}

// New wraps an already-authenticated HTTP client (see the auth package) with a
// base URL. The httpClient is expected to inject the bearer token itself.
func New(httpClient *http.Client, baseURL string, opts ...Option) *Client {
	c := &Client{
		http:    httpClient,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.limiter == nil {
		c.limiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(DefaultRequestsPerMinute)), requestBurst)
	}
	return c
}

// APIError is returned when the API responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aikido API %s %s: status %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// NotFound reports whether the error is a 404, which callers use to detect a
// resource that has been deleted outside of Terraform.
func NotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}

// Do sends an HTTP request. If body is non-nil it is JSON-encoded. If out is
// non-nil the JSON response is decoded into it. path is joined to the base URL.
// Requests are paced client-side to stay under the API rate limit; on a 429 it
// still waits per the Retry-After header and retries, up to maxRetries.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter wait: %w", err)
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(encoded)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("performing request: %w", err)
		}

		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("reading response body: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxRetries {
			if err := wait(ctx, retryDelay(resp.Header, attempt)); err != nil {
				return err
			}
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &APIError{
				StatusCode: resp.StatusCode,
				Method:     method,
				Path:       path,
				Body:       string(raw),
			}
		}

		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("decoding response body: %w", err)
			}
		}
		return nil
	}
}

// retryDelay returns how long to wait before retrying a 429. It honors the
// Retry-After header (integer seconds per the Aikido docs), falling back to
// exponential backoff, and never exceeds maxRetryDelay.
func retryDelay(header http.Header, attempt int) time.Duration {
	delay := time.Duration(1<<attempt) * time.Second
	if v := header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			delay = time.Duration(secs) * time.Second
		}
	}
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// wait sleeps for d, returning early if the context is cancelled.
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
