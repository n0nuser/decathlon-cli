// Package api reads decathlon.es. It fetches storefront pages over net/http
// with a Cloudflare session minted by a local FlareSolverr, and decodes the
// Next.js flight payload each page carries. It never writes to the store:
// no cart, no account, no checkout.
package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL   = "https://www.decathlon.es"
	DefaultSolverURL = "http://localhost:8191"
	// DefaultGap spaces requests to the storefront. One page is ~1 MB of HTML;
	// a human browsing is slower than this, and the CLI never runs in parallel.
	DefaultGap = 2 * time.Second
	maxBody    = 32 << 20
)

// Client is one invocation's connection to the storefront.
type Client struct {
	BaseURL     string
	SolverURL   string
	SessionFile string // where the minted cookies and user agent persist (0600)
	CacheDir    string // empty disables the response cache
	Gap         time.Duration
	Debug       io.Writer
	Notice      func(string)

	mu      sync.Mutex
	http    *http.Client
	session *Session
	last    time.Time
}

// Session is what FlareSolverr's browser was handed by Cloudflare: its
// cookies and the user agent they are bound to. Replaying the cookies under
// any other user agent is challenged again.
type Session struct {
	UserAgent string         `json:"user_agent"`
	MintedAt  time.Time      `json:"minted_at"`
	Cookies   []StoredCookie `json:"cookies"`
}

type StoredCookie struct {
	Name    string    `json:"name"`
	Value   string    `json:"value"`
	Domain  string    `json:"domain,omitempty"`
	Path    string    `json:"path,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
}

func New() *Client {
	return &Client{BaseURL: DefaultBaseURL, SolverURL: DefaultSolverURL, Gap: DefaultGap}
}

// Page is one fetched document.
type Page struct {
	URL  string // final URL after redirects
	Body []byte
}

func (c *Client) base() *url.URL {
	u, _ := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	return u
}

// Abs turns a storefront-relative path ("/es/p/...", "deportes/...") into an
// absolute URL on the base host.
func (c *Client) Abs(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/es/" + path
	}
	return strings.TrimRight(c.BaseURL, "/") + path
}

// Fetch GETs a storefront page. ttl > 0 serves it from the on-disk cache when
// a copy younger than ttl exists, and stores fresh copies there.
func (c *Client) Fetch(ctx context.Context, path string, ttl time.Duration) (*Page, error) {
	target := c.Abs(path)
	if ttl > 0 {
		if p := c.cached(target, ttl); p != nil {
			return p, nil
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}
	p, err := c.get(ctx, target)
	var e *Error
	if errors.As(err, &e) && e.Kind == KindBlocked && e.Status != http.StatusTooManyRequests {
		// The session went stale (cookies expire after ~30 minutes, or
		// Cloudflare rotated its view of us). Mint once and retry once;
		// a second refusal is reported, never looped on.
		c.notice("cloudflare challenged the stored session; minting a new one through flaresolverr")
		if err := c.mint(ctx); err != nil {
			return nil, err
		}
		p, err = c.get(ctx, target)
	}
	if err != nil {
		return nil, err
	}
	c.persistCookies()
	if ttl > 0 {
		c.store(target, p)
	}
	return p, nil
}

func (c *Client) notice(s string) {
	if c.Notice != nil {
		c.Notice(s)
	}
}

func (c *Client) get(ctx context.Context, target string) (*Page, error) {
	if wait := c.Gap - time.Since(c.last); wait > 0 && !c.last.IsZero() {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer func() { c.last = time.Now() }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.session.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-ES,es;q=0.9")
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Kind: KindNetwork, Msg: fmt.Sprintf("cannot reach %s: %v", req.URL.Host, err), Endpoint: req.URL.Path, Retryable: true}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, &Error{Kind: KindNetwork, Msg: "reading response: " + err.Error(), Endpoint: req.URL.Path, Retryable: true}
	}
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "GET %s -> %d %d bytes %s\n", resp.Request.URL, resp.StatusCode, len(body), time.Since(start).Round(time.Millisecond))
	}
	path := resp.Request.URL.Path
	switch {
	case isChallenge(resp, body):
		return nil, &Error{Kind: KindBlocked, Msg: "cloudflare challenged the request", Status: resp.StatusCode, Endpoint: path, Retryable: true}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &Error{Kind: KindBlocked, Msg: "decathlon is rate limiting this address; wait a few minutes", Status: resp.StatusCode, Endpoint: path, Retryable: true}
	case resp.StatusCode == http.StatusNotFound:
		return nil, &Error{Kind: KindNotFound, Msg: "decathlon has no page at " + path, Status: resp.StatusCode, Endpoint: path}
	case resp.StatusCode >= 500:
		return nil, &Error{Kind: KindNetwork, Msg: fmt.Sprintf("decathlon answered HTTP %d", resp.StatusCode), Status: resp.StatusCode, Endpoint: path, Retryable: true}
	case resp.StatusCode != http.StatusOK:
		return nil, &Error{Kind: KindAPIChanged, Msg: fmt.Sprintf("decathlon answered HTTP %d", resp.StatusCode), Status: resp.StatusCode, Endpoint: path, Body: excerpt(body)}
	}
	return &Page{URL: resp.Request.URL.String(), Body: body}, nil
}

func isChallenge(resp *http.Response, body []byte) bool {
	if resp.Header.Get("Cf-Mitigated") == "challenge" {
		return true
	}
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusServiceUnavailable {
		return false
	}
	return bytes.Contains(body, []byte("_cf_chl_opt")) || bytes.Contains(body, []byte("challenges.cloudflare.com")) || bytes.Contains(body, []byte("Attention Required!"))
}

func excerpt(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// ensureSession loads the stored session, minting one when none exists.
func (c *Client) ensureSession(ctx context.Context) error {
	if c.http != nil {
		return nil
	}
	if c.SessionFile != "" {
		if raw, err := os.ReadFile(c.SessionFile); err == nil {
			var s Session
			if json.Unmarshal(raw, &s) == nil && s.UserAgent != "" && len(s.Cookies) > 0 {
				c.useSession(&s)
				return nil
			}
		}
	}
	return c.mint(ctx)
}

func (c *Client) useSession(s *Session) {
	jar, _ := cookiejar.New(nil)
	base := c.base()
	cookies := make([]*http.Cookie, 0, len(s.Cookies))
	for _, sc := range s.Cookies {
		cookies = append(cookies, &http.Cookie{Name: sc.Name, Value: sc.Value, Path: "/"})
	}
	jar.SetCookies(base, cookies)
	c.session = s
	c.http = &http.Client{Jar: jar, Timeout: 60 * time.Second}
}

// persistCookies writes back cookies Cloudflare rotated (it re-issues
// __cf_bm as it goes), keeping the expiry FlareSolverr reported.
func (c *Client) persistCookies() {
	if c.SessionFile == "" || c.http == nil {
		return
	}
	changed := false
	for _, jc := range c.http.Jar.Cookies(c.base()) {
		found := false
		for i := range c.session.Cookies {
			if c.session.Cookies[i].Name == jc.Name {
				found = true
				if c.session.Cookies[i].Value != jc.Value {
					c.session.Cookies[i].Value = jc.Value
					changed = true
				}
			}
		}
		if !found {
			c.session.Cookies = append(c.session.Cookies, StoredCookie{Name: jc.Name, Value: jc.Value})
			changed = true
		}
	}
	if changed {
		_ = c.saveSession()
	}
}

func (c *Client) saveSession() error {
	if c.SessionFile == "" {
		return nil
	}
	raw, err := json.MarshalIndent(c.session, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(c.SessionFile, raw, 0o600)
}

// WriteFileAtomic writes through a temp file and a rename, then forces the
// mode so a relaxed umask cannot widen it.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ForgetSession deletes the stored session so the next request mints.
func (c *Client) ForgetSession() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.http, c.session = nil, nil
	if c.SessionFile == "" {
		return nil
	}
	err := os.Remove(c.SessionFile)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// StoredSession reports the session on disk without touching the network.
func (c *Client) StoredSession() (*Session, error) {
	raw, err := os.ReadFile(c.SessionFile)
	if err != nil {
		return nil, err
	}
	var s Session
	return &s, json.Unmarshal(raw, &s)
}

// solverResponse is the slice of FlareSolverr's /v1 answer the CLI reads.
type solverResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Version  string `json:"version"`
	Solution struct {
		URL       string `json:"url"`
		Status    int    `json:"status"`
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name    string  `json:"name"`
			Value   string  `json:"value"`
			Domain  string  `json:"domain"`
			Path    string  `json:"path"`
			Expires float64 `json:"expires"`
		} `json:"cookies"`
	} `json:"solution"`
}

// mint asks FlareSolverr's browser to load the home page, which solves
// whatever challenge Cloudflare serves, and keeps the cookies and user agent
// it ends up with. Only this call goes through FlareSolverr.
func (c *Client) mint(ctx context.Context) error {
	reqBody, _ := json.Marshal(map[string]any{"cmd": "request.get", "url": c.Abs("/es/"), "maxTimeout": 60000})
	endpoint := strings.TrimRight(c.SolverURL, "/") + "/v1"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Kind: KindNetwork, Msg: fmt.Sprintf("flaresolverr is not reachable at %s (%v). Start it (see README) or set FLARESOLVERR_URL, then run `decathlon doctor`", c.SolverURL, err), Endpoint: "/v1", Retryable: true}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	var sr solverResponse
	if err := json.Unmarshal(raw, &sr); err != nil {
		return &Error{Kind: KindNetwork, Msg: fmt.Sprintf("%s did not answer like flaresolverr (HTTP %d)", c.SolverURL, resp.StatusCode), Endpoint: "/v1"}
	}
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "POST %s request.get %s -> %s %d, %d cookies %s\n", endpoint, c.Abs("/es/"), sr.Status, sr.Solution.Status, len(sr.Solution.Cookies), time.Since(start).Round(time.Millisecond))
	}
	if sr.Status != "ok" || sr.Solution.Status != http.StatusOK || sr.Solution.UserAgent == "" || len(sr.Solution.Cookies) == 0 {
		msg := strings.TrimSpace(sr.Message)
		if msg == "" {
			msg = fmt.Sprintf("page status %d", sr.Solution.Status)
		}
		return &Error{Kind: KindBlocked, Msg: "flaresolverr could not open decathlon.es: " + msg, Status: sr.Solution.Status, Endpoint: "/v1", Retryable: true}
	}
	s := &Session{UserAgent: sr.Solution.UserAgent, MintedAt: time.Now().UTC()}
	for _, ck := range sr.Solution.Cookies {
		sc := StoredCookie{Name: ck.Name, Value: ck.Value, Domain: ck.Domain, Path: ck.Path}
		if ck.Expires > 0 {
			sc.Expires = time.Unix(int64(ck.Expires), 0).UTC()
		}
		s.Cookies = append(s.Cookies, sc)
	}
	c.useSession(s)
	return c.saveSession()
}

// Mint forces a fresh session (doctor, and `session refresh`).
func (c *Client) Mint(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mint(ctx)
}

// SolverStatus asks FlareSolverr for its banner, without starting a browser.
func (c *Client) SolverStatus(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.SolverURL, "/")+"/", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", &Error{Kind: KindNetwork, Msg: fmt.Sprintf("flaresolverr is not reachable at %s: %v", c.SolverURL, err)}
	}
	defer resp.Body.Close()
	var banner struct {
		Msg     string `json:"msg"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&banner); err != nil || banner.Version == "" {
		return "", &Error{Kind: KindNetwork, Msg: fmt.Sprintf("%s does not look like flaresolverr", c.SolverURL)}
	}
	return banner.Version, nil
}

// cache

type cacheEntry struct {
	URL  string    `json:"url"`
	At   time.Time `json:"at"`
	Body []byte    `json:"body"`
}

func (c *Client) cachePath(target string) string {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(c.CacheDir, hex.EncodeToString(sum[:12])+".json")
}

func (c *Client) cached(target string, ttl time.Duration) *Page {
	if c.CacheDir == "" {
		return nil
	}
	raw, err := os.ReadFile(c.cachePath(target))
	if err != nil {
		return nil
	}
	var e cacheEntry
	if json.Unmarshal(raw, &e) != nil || time.Since(e.At) > ttl {
		return nil
	}
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "GET %s (cached %s ago)\n", target, time.Since(e.At).Round(time.Second))
	}
	return &Page{URL: e.URL, Body: e.Body}
}

func (c *Client) store(target string, p *Page) {
	if c.CacheDir == "" {
		return
	}
	raw, err := json.Marshal(cacheEntry{URL: p.URL, At: time.Now(), Body: p.Body})
	if err == nil {
		_ = WriteFileAtomic(c.cachePath(target), raw, 0o600)
	}
}
