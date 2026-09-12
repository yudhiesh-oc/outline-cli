package outline_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yudhiesh-oc/outline-cli/internal/outline"
)

func writeCredentials(t *testing.T, home, key string) {
	t.Helper()
	dir := filepath.Join(home, "outline")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"url":"https://outline.invalid","apiKey":"`+key+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An explicit environment key overrides the saved key for the configured workspace.
func TestAuthResolutionPrecedence(t *testing.T) {
	home := t.TempDir()
	writeCredentials(t, home, "ol_api_from_file")
	t.Setenv("OUTLINE_API_KEY", "")
	t.Setenv("OUTLINE_URL", "")
	t.Setenv("OUTLINE_CONFIG", filepath.Join(home, "outline", "config.json"))
	t.Setenv("HOME", home)

	c, err := outline.FromEnv()
	if err != nil {
		t.Fatalf("file fallback failed: %v", err)
	}
	if c.Token != "ol_api_from_file" {
		t.Errorf("file fallback token = %q", c.Token)
	}

	t.Setenv("OUTLINE_API_KEY", "ol_api_from_env")
	c, err = outline.FromEnv()
	if err != nil {
		t.Fatalf("env token failed: %v", err)
	}
	if c.Token != "ol_api_from_env" {
		t.Errorf("env token should win, got %q", c.Token)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *outline.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &outline.Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}
}

// An API-level failure (ok:false) surfaces as APIError with status + detail.
func TestAPIErrorMapping(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"ok":false,"error":"policy-deny"}`)
	})

	_, err := c.Do(t.Context(), "documents.info", map[string]any{"id": "d1"})

	var apiErr *outline.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *outline.APIError", err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Detail != "policy-deny" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

// A 429 with Retry-After is retried once; the retry succeeds.
func TestRateLimitRetriesOnce(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			io.WriteString(w, `{"ok":false,"error":"rate-limit"}`)
			return
		}
		io.WriteString(w, `{"ok":true,"data":{"id":"d1"}}`)
	})

	data, err := c.Do(t.Context(), "documents.info", map[string]any{"id": "d1"})
	if err != nil {
		t.Fatalf("Do after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if !strings.Contains(string(data), `"d1"`) {
		t.Errorf("data = %s", data)
	}
}

// Two consecutive 429s exhaust the retry budget and surface the APIError.
func TestRateLimitExhausted(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"ok":false,"error":"rate-limit"}`)
	})

	_, err := c.Do(t.Context(), "documents.info", map[string]any{"id": "d1"})
	if calls != 2 {
		t.Errorf("calls = %d, want exactly 2 attempts", calls)
	}
	var apiErr *outline.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want 429 APIError", err)
	}
}

// Paged stops after maxPages even if the server always advertises a nextPath.
func TestPagedCapsRunawayPagination(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, fmt.Sprintf(`{"ok":true,"data":[{"n":%d}],"pagination":{"nextPath":"/api/documents.list?offset=%d"}}`, calls, calls))
	})

	data, err := c.Paged(t.Context(), "documents.list", map[string]any{})
	if err == nil || data != nil {
		t.Fatalf("incomplete results reported as success: data=%s err=%v", data, err)
	}
	if calls != 50 {
		t.Errorf("calls = %d, want the 50-page cap", calls)
	}
}

func TestPagedStopsOnEmptyPageWithNextPath(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"ok":true,"data":[],"pagination":{"nextPath":"/api/documents.list?offset=25"}}`)
	})
	data, err := c.Paged(t.Context(), "documents.list", nil)
	if err != nil || string(data) != "[]" || calls != 1 {
		t.Fatalf("empty pagination: data=%s calls=%d err=%v", data, calls, err)
	}
}

func TestPagedRejectsUnsafeOrStalledContinuation(t *testing.T) {
	for _, next := range []string{
		"https://other.invalid/api/documents.list?offset=1",
		"/api/documents.delete?offset=1",
		"/api/documents.list?offset=0",
	} {
		t.Run(next, func(t *testing.T) {
			calls := 0
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": []int{1}, "pagination": map[string]any{"nextPath": next}})
			})
			data, err := c.Paged(t.Context(), "documents.list", nil)
			if err == nil || data != nil || calls != 1 {
				t.Fatalf("unsafe continuation: data=%s calls=%d err=%v", data, calls, err)
			}
		})
	}
}

func TestHTTPFailureCannotClaimSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"ok":true,"data":{"id":"not-saved"}}`)
	})
	data, err := c.Do(t.Context(), "documents.create", nil)
	var apiErr *outline.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable || data != nil {
		t.Fatalf("HTTP failure accepted: data=%s err=%v", data, err)
	}
}

func TestRetryWaitHonorsCancellation(t *testing.T) {
	requested := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"ok":false,"error":"rate-limit"}`)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := c.Do(ctx, "documents.info", nil)
		finished <- err
	}()
	<-requested
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored cancellation")
	}
}
