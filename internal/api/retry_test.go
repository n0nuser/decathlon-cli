package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// retryClient is a client wired to srv with a session already loaded, so the
// request path under test is get() and nothing else.
func retryClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := New()
	c.BaseURL = srv.URL
	c.Gap = 0
	c.SessionFile = filepath.Join(t.TempDir(), "session.json")
	c.useSession(&Session{UserAgent: "Test/1.0", Cookies: []StoredCookie{{Name: "x", Value: "y"}}})
	return c
}

func get(t *testing.T, c *Client) (*Page, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.get(ctx, c.Abs("/x"))
}

func TestRetry5xxThenSuccess(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p, err := get(t, retryClient(t, srv))
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%v %q", err, p.Body)
	}
	if hits.Load() != 3 {
		t.Fatalf("%d attempts, want 3", hits.Load())
	}
}

func TestRetry5xxExhausted(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := get(t, retryClient(t, srv))
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindNetwork || e.Status != http.StatusServiceUnavailable {
		t.Fatalf("want network 503, got %v", err)
	}
	if hits.Load() != 3 {
		t.Fatalf("%d attempts, want 1 + 2 retries", hits.Load())
	}
}

func TestRetry429Seconds(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	start := time.Now()
	p, err := get(t, retryClient(t, srv))
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d attempts, want 2", hits.Load())
	}
	if waited := time.Since(start); waited < 900*time.Millisecond {
		t.Fatalf("retried after %s, before the requested 1s", waited)
	}
}

func TestRetry429HTTPDate(t *testing.T) {
	var hits atomic.Int32
	until := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", until)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p, err := get(t, retryClient(t, srv))
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d attempts, want 2", hits.Load())
	}
}

func TestRetry429TooLong(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := get(t, retryClient(t, srv))
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindBlocked || e.Status != http.StatusTooManyRequests {
		t.Fatalf("want blocked 429, got %v", err)
	}
	if !strings.Contains(e.Msg, "10m0s") {
		t.Fatalf("message hides the wait: %q", e.Msg)
	}
	if hits.Load() != 1 {
		t.Fatalf("%d attempts, want no retry past the cap", hits.Load())
	}
}

func TestNoRetryOn403Challenge(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := get(t, retryClient(t, srv))
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindBlocked {
		t.Fatalf("want blocked, got %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("%d attempts, want no retry on a block", hits.Load())
	}
}

func TestRetryNetworkError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			panic(http.ErrAbortHandler) // drop the connection once
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p, err := get(t, retryClient(t, srv))
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("%d attempts, want 2", hits.Load())
	}
}

func TestBackoff(t *testing.T) {
	for _, c := range []struct {
		attempt int
		want    time.Duration
	}{{0, 500 * time.Millisecond}, {1, time.Second}, {2, 2 * time.Second}, {10, retryCap}} {
		if got := backoff(c.attempt); got != c.want {
			t.Errorf("backoff(%d) = %s, want %s", c.attempt, got, c.want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"", 0, false},
		{"30", 30 * time.Second, true},
		{"0", 0, true},
		{"-1", 0, false},
		{"soon", 0, false},
		{"Mon, 19 Oct 2026 12:01:00 GMT", time.Minute, true},
		{"Mon, 19 Oct 2026 11:00:00 GMT", 0, true}, // a date in the past
	} {
		got, ok := retryAfter(c.header, now)
		if got != c.want || ok != c.ok {
			t.Errorf("retryAfter(%q) = %s %v, want %s %v", c.header, got, ok, c.want, c.ok)
		}
	}
}

func TestWaitGap(t *testing.T) {
	c := New()
	c.Gap = 40 * time.Millisecond
	c.last = time.Now()
	start := time.Now()
	if err := c.waitGap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < 30*time.Millisecond {
		t.Fatalf("waited only %s of a 40ms gap", waited)
	}
}
