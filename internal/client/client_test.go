package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aikido/terraform-provider-aikido/internal/client"
)

// newServer spins up a test server with the given handler and returns a Client
// pointed at it.
func newServer(t *testing.T, handler http.HandlerFunc) *client.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return client.New(srv.Client(), srv.URL)
}

func TestDo_DecodesSuccessBody(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept header = %q, want application/json", got)
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"id":7,"name":"platform"}`)
	})

	var out struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := c.Do(context.Background(), "GET", "/public/v1/repositories/code/7", nil, &out); err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if out.ID != 7 || out.Name != "platform" {
		t.Errorf("decoded %+v, want {ID:7 Name:platform}", out)
	}
}

func TestDo_EncodesRequestBody(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var body map[string]int64
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if body["code_repo_id"] != 1 {
			t.Errorf("request body code_repo_id = %d, want 1", body["code_repo_id"])
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"success":1}`)
	})

	err := c.Do(context.Background(), "POST", "/public/v1/repositories/code/activate", map[string]int64{"code_repo_id": 1}, nil)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
}

func TestDo_NoContentTypeWithoutBody(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q, want empty for bodyless request", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := c.Do(context.Background(), "DELETE", "/public/v1/repositories/code/1", nil, nil); err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
}

func TestDo_NonSuccessReturnsAPIError(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "invalid name")
	})

	err := c.Do(context.Background(), "POST", "/public/v1/repositories/code/activate", map[string]int64{"code_repo_id": 0}, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *client.APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Method != "POST" || apiErr.Path != "/public/v1/repositories/code/activate" {
		t.Errorf("Method/Path = %s %s, want POST /public/v1/repositories/code/activate", apiErr.Method, apiErr.Path)
	}
	if apiErr.Body != "invalid name" {
		t.Errorf("Body = %q, want %q", apiErr.Body, "invalid name")
	}
}

func TestDo_NotFoundDetectsDeletion(t *testing.T) {
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "not found")
	})

	err := c.Do(context.Background(), "GET", "/public/v1/repositories/code/999", nil, nil)
	if !client.NotFound(err) {
		t.Errorf("NotFound(%v) = false, want true", err)
	}
}

func TestNotFound_OnNonAPIError(t *testing.T) {
	if client.NotFound(io.EOF) {
		t.Error("NotFound(io.EOF) = true, want false for non-APIError")
	}
	if client.NotFound(nil) {
		t.Error("NotFound(nil) = true, want false")
	}
}

func TestDo_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"id":1}`)
	})

	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.Do(context.Background(), "GET", "/public/v1/repositories/code/1", nil, &out); err != nil {
		t.Fatalf("Do returned error after retries: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3 (two 429s then success)", got)
	}
	if out.ID != 1 {
		t.Errorf("decoded ID = %d, want 1", out.ID)
	}
}

func TestDo_GivesUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	c := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	err := c.Do(context.Background(), "GET", "/public/v1/repositories/code", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *client.APIError", err)
	}
	if apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
	}
	// 1 initial attempt + maxRetries (4) = 5 total calls.
	if got := calls.Load(); got != 5 {
		t.Errorf("server calls = %d, want 5", got)
	}
}

func TestDo_TrimsBaseURLAndJoinsPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Trailing slash on the base URL must not produce a doubled slash.
	c := client.New(srv.Client(), srv.URL+"/")
	if err := c.Do(context.Background(), "GET", "/public/v1/repositories/code", nil, nil); err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	if gotPath != "/public/v1/repositories/code" {
		t.Errorf("request path = %q, want /public/v1/repositories/code", gotPath)
	}
}
