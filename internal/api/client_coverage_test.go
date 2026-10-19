package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeSolver(t *testing.T, cookie string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"msg":"FlareSolverr is ready!","version":"3.5.2"}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"message": "Challenge solved!",
			"solution": map[string]any{
				"status": 200, "userAgent": "Test/1.0",
				"cookies": []map[string]any{{"name": "__cf_bm", "value": cookie, "domain": ".decathlon.es", "path": "/", "expires": 1.9e9}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func solverReturning(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"msg":"FlareSolverr is ready!","version":"3.5.2"}`)
			return
		}
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sessionFile(t *testing.T, cookie, ua string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.json")
	raw, _ := json.Marshal(Session{UserAgent: ua, Cookies: []StoredCookie{{Name: "__cf_bm", Value: cookie}}})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testClient(shop, solver, session, cache string) *Client {
	c := New()
	c.BaseURL = shop
	c.SolverURL = solver
	c.SessionFile = session
	c.CacheDir = cache
	c.Gap = 0
	return c
}

func challenge(w http.ResponseWriter) {
	w.Header().Set("Cf-Mitigated", "challenge")
	w.WriteHeader(http.StatusForbidden)
	io.WriteString(w, "<html>Just a moment...</html>")
}

func TestMintReplayAndForget(t *testing.T) {
	solver := fakeSolver(t, "c1")
	path := filepath.Join(t.TempDir(), "session.json")
	c := testClient("http://shop.invalid", solver.URL, path, "")
	if err := c.Mint(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := c.StoredSession()
	if err != nil || s.UserAgent != "Test/1.0" || len(s.Cookies) != 1 || s.Cookies[0].Value != "c1" {
		t.Fatalf("%+v %v", s, err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("session mode %v %v", fi.Mode(), err)
	}
	if v, err := c.SolverStatus(context.Background()); err != nil || v != "3.5.2" {
		t.Fatalf("%q %v", v, err)
	}
	if err := c.ForgetSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StoredSession(); err == nil {
		t.Fatal("session survived ForgetSession")
	}
	if err := c.ForgetSession(); err != nil {
		t.Fatalf("second forget: %v", err)
	}
}

func TestFetchMintsThenReplays(t *testing.T) {
	var mints atomic.Int32
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mints.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "solution": map[string]any{
				"status": 200, "userAgent": "Test/1.0",
				"cookies": []map[string]any{{"name": "__cf_bm", "value": "c2"}},
			},
		})
	}))
	defer solver.Close()
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("__cf_bm")
		if err != nil || c.Value != "c2" || r.UserAgent() != "Test/1.0" {
			challenge(w)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer shop.Close()
	path := filepath.Join(t.TempDir(), "session.json")
	c := testClient(shop.URL, solver.URL, path, "")
	p, err := c.Fetch(context.Background(), "/es/x", 0)
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%q %v", p.Body, err)
	}
	if mints.Load() != 1 {
		t.Fatalf("minted %d times", mints.Load())
	}
	if _, err := c.Fetch(context.Background(), "/es/x", 0); err != nil || mints.Load() != 1 {
		t.Fatalf("stored session not reused: %v %d", err, mints.Load())
	}
}

func TestFetchRotatesCookies(t *testing.T) {
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("__cf_bm")
		if err != nil || c.Value == "" {
			challenge(w)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "__cf_bm", Value: "rotated", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "extra", Value: "1", Path: "/"})
		io.WriteString(w, "ok")
	}))
	defer shop.Close()
	path := sessionFile(t, "c1", "Test/1.0")
	c := testClient(shop.URL, "http://127.0.0.1:1", path, "")
	if _, err := c.Fetch(context.Background(), "/es/x", 0); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range s.Cookies {
		got[c.Name] = c.Value
	}
	if got["__cf_bm"] != "rotated" || got["extra"] != "1" {
		t.Fatalf("%+v", got)
	}
}

func TestFetchRemintsOnceThenSurfaces(t *testing.T) {
	solver := fakeSolver(t, "new")
	var notices []string
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("__cf_bm")
		if c == nil || c.Value != "new" {
			challenge(w)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer shop.Close()
	path := sessionFile(t, "old", "Test/1.0")
	c := testClient(shop.URL, solver.URL, path, "")
	c.Notice = func(s string) { notices = append(notices, s) }
	if _, err := c.Fetch(context.Background(), "/es/x", 0); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "minting a new one") {
		t.Fatalf("%v", notices)
	}

	// A shop that never accepts the new session reports blocked, not a loop.
	always := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { challenge(w) }))
	defer always.Close()
	c2 := testClient(always.URL, solver.URL, sessionFile(t, "x", "Test/1.0"), "")
	_, err := c2.Fetch(context.Background(), "/es/x", 0)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindBlocked {
		t.Fatalf("want blocked, got %v", err)
	}

	// A solver that is down turns the challenge into a network error.
	c3 := testClient(always.URL, "http://127.0.0.1:1", sessionFile(t, "y", "Test/1.0"), "")
	if _, err := c3.Fetch(context.Background(), "/es/x", 0); err == nil {
		t.Fatal("challenge with no solver succeeded")
	}
}

func TestFetchChallengeServedWith200(t *testing.T) {
	solver := fakeSolver(t, "new")
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie("__cf_bm")
		if c == nil || c.Value != "new" {
			io.WriteString(w, "<html><script>window._cf_chl_opt={}</script></html>")
			return
		}
		io.WriteString(w, "ok")
	}))
	defer shop.Close()
	c := testClient(shop.URL, solver.URL, sessionFile(t, "old", "Test/1.0"), "")
	p, err := c.Fetch(context.Background(), "/es/x", 0)
	if err != nil || string(p.Body) != "ok" {
		t.Fatalf("%q %v", p.Body, err)
	}
}

func TestMintFailures(t *testing.T) {
	for name, solver := range map[string]*httptest.Server{
		"status error":  solverReturning(t, `{"status":"error","message":"nope"}`),
		"page status":   solverReturning(t, `{"status":"ok","solution":{"status":403,"userAgent":"U","cookies":[{"name":"a","value":"b"}]}}`),
		"no user agent": solverReturning(t, `{"status":"ok","solution":{"status":200,"cookies":[{"name":"a","value":"b"}]}}`),
		"no cookies":    solverReturning(t, `{"status":"ok","solution":{"status":200,"userAgent":"U"}}`),
		"invalid json":  solverReturning(t, `not json`),
	} {
		t.Run(name, func(t *testing.T) {
			c := testClient("http://shop.invalid", solver.URL, filepath.Join(t.TempDir(), "s.json"), "")
			if err := c.Mint(context.Background()); err == nil {
				t.Fatal("mint accepted")
			}
		})
	}
	c := testClient("http://shop.invalid", "http://127.0.0.1:1", filepath.Join(t.TempDir(), "s.json"), "")
	if err := c.Mint(context.Background()); err == nil {
		t.Fatal("unreachable solver accepted")
	}
	if _, err := c.SolverStatus(context.Background()); err == nil {
		t.Fatal("unreachable solver status accepted")
	}
	notSolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	defer notSolver.Close()
	if _, err := testClient("http://shop.invalid", notSolver.URL, "", "").SolverStatus(context.Background()); err == nil {
		t.Fatal("a body without a version was accepted as FlareSolverr")
	}
}

func TestCache(t *testing.T) {
	var hits atomic.Int32
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, "ok")
	}))
	defer shop.Close()
	cache := t.TempDir()
	c := testClient(shop.URL, "http://127.0.0.1:1", sessionFile(t, "c", "UA"), cache)
	ctx := context.Background()
	if _, err := c.Fetch(ctx, "/es/x", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(ctx, "/es/x", time.Hour); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("%d hits, want the second from cache", hits.Load())
	}
	if fi, err := os.Stat(c.cachePath(c.Abs("/es/x"))); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache mode %v %v", fi.Mode(), err)
	}
	if _, err := c.Fetch(ctx, "/es/x", 0); err != nil || hits.Load() != 2 {
		t.Fatalf("ttl 0 did not fetch: %v %d", err, hits.Load())
	}
	// A corrupt cache entry is ignored and replaced.
	if err := os.WriteFile(c.cachePath(c.Abs("/es/x")), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Fetch(ctx, "/es/x", time.Hour); err != nil || hits.Load() != 3 {
		t.Fatalf("corrupt cache: %v %d", err, hits.Load())
	}
	// An expired entry is ignored.
	raw, _ := json.Marshal(cacheEntry{URL: c.Abs("/es/x"), At: time.Now().Add(-2 * time.Hour), Body: []byte("stale")})
	if err := os.WriteFile(c.cachePath(c.Abs("/es/x")), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := c.Fetch(ctx, "/es/x", time.Hour)
	if err != nil || string(p.Body) != "ok" || hits.Load() != 4 {
		t.Fatalf("expired cache: %q %v %d", p.Body, err, hits.Load())
	}
	// No cache dir means no caching at all.
	c.CacheDir = ""
	if _, err := c.Fetch(ctx, "/es/x", time.Hour); err != nil || hits.Load() != 5 {
		t.Fatalf("uncached: %v %d", err, hits.Load())
	}
}

func TestIsChallenge(t *testing.T) {
	resp := func(status int, header string) *http.Response {
		h := make(http.Header)
		if header != "" {
			h.Set("Cf-Mitigated", header)
		}
		return &http.Response{StatusCode: status, Header: h}
	}
	small := []byte("<html>challenge</html>")
	for _, c := range []struct {
		name string
		resp *http.Response
		body []byte
		want bool
	}{
		{"header", resp(403, "challenge"), small, true},
		{"cf token", resp(200, ""), []byte("<script>window._cf_chl_opt={}</script>"), true},
		{"attention", resp(403, ""), []byte("Attention Required!"), true},
		{"challenges host", resp(503, ""), []byte("https://challenges.cloudflare.com/x"), true},
		{"large body", resp(403, ""), make([]byte, 70<<10), false},
		{"normal 403", resp(403, ""), []byte("forbidden"), false},
		{"normal 200", resp(200, ""), []byte("<html>ok</html>"), false},
	} {
		if got := isChallenge(c.resp, c.body); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestAbsAllowAndMalformedBase(t *testing.T) {
	c := New()
	if got := c.Abs("deportes/camping"); got != DefaultBaseURL+"/es/deportes/camping" {
		t.Fatalf("%q", got)
	}
	if got := c.Abs("/es/x"); got != DefaultBaseURL+"/es/x" {
		t.Fatalf("%q", got)
	}
	if got := c.Abs("https://example.test/x"); got != "https://example.test/x" {
		t.Fatalf("%q", got)
	}
	if err := c.allow("://"); err == nil {
		t.Fatal("invalid URL allowed")
	}
	if err := c.allow("https://other.test/x"); err == nil {
		t.Fatal("foreign host allowed")
	}
	bad := New()
	bad.BaseURL = "http://bad host"
	if _, err := bad.Fetch(context.Background(), "/es/", 0); err == nil {
		t.Fatal("malformed base accepted")
	}
}

func TestFetchNotFound(t *testing.T) {
	shop := httptest.NewServer(http.NotFoundHandler())
	defer shop.Close()
	c := testClient(shop.URL, "http://127.0.0.1:1", sessionFile(t, "c", "UA"), "")
	_, err := c.Fetch(context.Background(), "/es/missing", 0)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindNotFound {
		t.Fatalf("%v", err)
	}
}
