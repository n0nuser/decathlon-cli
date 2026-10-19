package cli

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n0nuser/decathlon-cli/internal/store"
)

func page(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "api", "testdata", name+".html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fake is a stand-in storefront serving the captured pages, behind a
// Cloudflare-like gate that wants the cookie the fake FlareSolverr hands out.
type fake struct {
	t        *testing.T
	shop     *httptest.Server
	solver   *httptest.Server
	mints    atomic.Int32
	cookie   atomic.Value // the cookie value the gate currently accepts
	soft     atomic.Bool  // serve the challenge with 200, as Cloudflare sometimes does
	pages    map[string][]byte
	redirect map[string]string
}

func newFake(t *testing.T) *fake {
	f := &fake{t: t, pages: map[string][]byte{
		"/es/search?Ntt=hornillo+gas":                                                page(t, "listing_keyword"),
		"/es/search?Ntt=hornillo+gas&from=40&size=40":                                page(t, "listing_keyword"),
		"/es/deportes/camping/tiendas-campana":                                       page(t, "listing_redirect"),
		"/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471": page(t, "product_mh100"),
		"/es/store-locator":                                                          page(t, "store_locator"),
		"/es/":                                                                       page(t, "home"),
	}, redirect: map[string]string{
		"/es/search?Ntt=tienda+campa%C3%B1a": "/es/deportes/camping/tiendas-campana?opeco=opeco:LiP_search_&type=opeco",
		"/es/p/_/303295/m8513471":            "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471",
	}}
	f.cookie.Store("good-1")
	f.shop = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("__cf_bm")
		if err != nil || c.Value != f.cookie.Load().(string) || r.UserAgent() != "FakeBrowser/1.0" {
			if !f.soft.Load() {
				w.Header().Set("Cf-Mitigated", "challenge")
				w.WriteHeader(http.StatusForbidden)
			}
			io.WriteString(w, "<html><title>Just a moment...</title><script>window._cf_chl_opt={}</script></html>")
			return
		}
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		if to, ok := f.redirect[key]; ok {
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		body, ok := f.pages[key]
		if !ok {
			body, ok = f.pages[r.URL.Path]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	f.solver = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"msg":"FlareSolverr is ready!","version":"3.5.2"}`)
			return
		}
		f.mints.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "message": "Challenge solved!", "solution": map[string]any{
			"status": 200, "userAgent": "FakeBrowser/1.0",
			"cookies": []map[string]any{{"name": "__cf_bm", "value": f.cookie.Load().(string), "domain": ".decathlon.es", "path": "/", "expires": 1.9e9}},
		}})
	}))
	t.Cleanup(func() { f.shop.Close(); f.solver.Close() })
	home := t.TempDir()
	for k, v := range map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_DATA_HOME": filepath.Join(home, "data"),
		"XDG_CACHE_HOME": filepath.Join(home, "cache"), "XDG_STATE_HOME": filepath.Join(home, "state"),
		"DECATHLON_BASE_URL": f.shop.URL, "FLARESOLVERR_URL": f.solver.URL, "DECATHLON_REQUEST_GAP": "0s",
		"DECATHLON_NO_UPDATE_CHECK": "1", "DECATHLON_CONFIG": "", "DECATHLON_ERROR_FORMAT": "",
	} {
		t.Setenv(k, v)
	}
	return f
}

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Execute("1.2.3", args, strings.NewReader(""), &out, &errb)
	return out.String(), errb.String(), code
}

func TestSearchJSON(t *testing.T) {
	f := newFake(t)
	out, errs, code := run(t, "search", "tienda campaña")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var r struct {
		Total    int `json:"total"`
		Category struct {
			Path string `json:"path"`
		} `json:"category"`
		Products []struct {
			Ref   string  `json:"ref"`
			Price float64 `json:"price"`
		} `json:"products"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	if r.Category.Path != "deportes/camping/tiendas-campana" || len(r.Products) != 40 || r.Products[0].Ref != "303295/8513471" {
		t.Fatalf("%+v", r)
	}
	if f.mints.Load() != 1 {
		t.Fatalf("minted %d times", f.mints.Load())
	}
	// A second command reuses the stored session instead of minting.
	if _, errs, code := run(t, "search", "hornillo gas", "--format", "jsonl"); code != 0 {
		t.Fatal(errs)
	}
	if f.mints.Load() != 1 {
		t.Fatalf("stored session not reused: %d mints", f.mints.Load())
	}
}

// Regression (bug 3): when Cloudflare stops accepting the stored cookie the
// client must mint once and retry once, not fail and not loop.
func TestStaleSessionIsReminted(t *testing.T) {
	f := newFake(t)
	if _, errs, code := run(t, "store", "list"); code != 0 {
		t.Fatal(errs)
	}
	f.cookie.Store("good-2") // Cloudflare rotated; the stored cookie is stale
	_, errs, code := run(t, "search", "hornillo gas")
	if code != 0 || f.mints.Load() != 2 || !strings.Contains(errs, "minting a new one") {
		t.Fatalf("exit %d mints %d stderr %q", code, f.mints.Load(), errs)
	}
}

// Regression (bug 7): a challenge page served with 200 failed flight parsing
// and was reported as a storefront change (exit 7) instead of re-minting.
func TestChallengeWith200IsReminted(t *testing.T) {
	f := newFake(t)
	f.soft.Store(true)
	if _, errs, code := run(t, "store", "list"); code != 0 {
		t.Fatal(errs)
	}
	f.cookie.Store("good-2")
	if _, errs, code := run(t, "search", "hornillo gas"); code != 0 || f.mints.Load() != 2 {
		t.Fatalf("exit %d mints %d: %s", code, f.mints.Load(), errs)
	}
}

func TestSessionFileIsPrivate(t *testing.T) {
	newFake(t)
	if _, errs, code := run(t, "store", "list"); code != 0 {
		t.Fatal(errs)
	}
	fi, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli", "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("session.json mode %v", fi.Mode().Perm())
	}
}

func TestExitCodes(t *testing.T) {
	newFake(t)
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"search"}, 2},
		{[]string{"search", "hornillo gas", "--store", "169"}, 2},
		{[]string{"search", "hornillo gas", "--sort", "cheapest"}, 2},
		{[]string{"search", "hornillo gas", "--page", "-1"}, 2},
		{[]string{"search", "hornillo gas", "--limit", "-1"}, 2},
		{[]string{"product", "show", "303295"}, 2},
		{[]string{"product", "show", "1/2"}, 4},
		{[]string{"store", "show", "99999"}, 4},
		{[]string{"--format", "yaml", "store", "list"}, 2},
	}
	for _, c := range cases {
		_, errs, code := run(t, c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d (%s)", c.args, code, c.code, errs)
		}
	}
	// No stored session and FlareSolverr down: a page that is not cached
	// cannot be fetched.
	t.Setenv("FLARESOLVERR_URL", "http://127.0.0.1:1")
	os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli", "session.json"))
	_, errs, code := run(t, "--error-format", "json", "search", "hornillo gas")
	var env struct {
		Code     int    `json:"code"`
		Category string `json:"category"`
	}
	if code != 6 || json.Unmarshal([]byte(errs), &env) != nil || env.Category != "network" {
		t.Fatalf("flaresolverr down: exit %d %s", code, errs)
	}
}

// due is the check scheduler: a watch is due once its interval has passed.
// Regression (bug 11): the grace that absorbs cron jitter used to be a flat
// minute, so a five-minute watch could fire every four minutes, faster than
// the documented minimum.
func TestDue(t *testing.T) {
	now := time.Now()
	mk := func(iv time.Duration, last time.Duration) store.Watch {
		w := store.Watch{Interval: store.Duration(iv)}
		if last != 0 {
			w.LastCheckAt = now.Add(-last)
		}
		return w
	}
	cases := []struct {
		name string
		w    store.Watch
		want bool
	}{
		{"never checked", mk(30*time.Minute, 0), true},
		{"5m watch, 4m in", mk(5*time.Minute, 4*time.Minute), false},
		{"5m watch, 5m in", mk(5*time.Minute, 5*time.Minute), true},
		{"30m watch, 28m in", mk(30*time.Minute, 28*time.Minute), false},
		{"30m watch, 29m in", mk(30*time.Minute, 29*time.Minute), true},
	}
	for _, c := range cases {
		if got := due(c.w, now); got != c.want {
			t.Errorf("%s: due = %v, want %v", c.name, got, c.want)
		}
	}
}

// Regression (bug 10): `doctor` is what a person runs when something is
// broken, but a broken config.toml made setup refuse before any check ran, so
// the config failure never appeared in the table.
func TestDoctorReportsBrokenConfig(t *testing.T) {
	newFake(t)
	cfgDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("flaresolverr_url = \"http://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errs, code := run(t, "doctor")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (%s)", code, errs)
	}
	if !strings.Contains(out, `"name": "config"`) || !strings.Contains(out, `"ok": false`) {
		t.Fatalf("doctor hid the config failure: %s", out)
	}
	if !strings.Contains(errs, "doctor found problems") {
		t.Fatalf("stderr: %s", errs)
	}
}

// Regression (bug 13): --page was read as a 0-based index while the header and
// the site count pages from 1, so --page 2 fetched the third page and the
// more-hint named a page other than the one it led to.
func TestSearchPageNumbering(t *testing.T) {
	newFake(t)

	// Page 1 is the default, and the header and hint agree with it.
	out, errs, code := run(t, "search", "hornillo gas", "--format", "pretty")
	if code != 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(out, "page 1 of 11") || !strings.Contains(out, "more: --page 2") {
		t.Fatalf("page 1 header/hint:\n%s", out)
	}

	// The JSON field is the same 1-based number.
	out, errs, code = run(t, "search", "hornillo gas")
	if code != 0 {
		t.Fatal(errs)
	}
	var r struct {
		Page  int `json:"page"`
		Pages int `json:"pages"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err, out)
	}
	if r.Page != 1 || r.Pages != 11 {
		t.Fatalf("json page %d of %d, want 1 of 11", r.Page, r.Pages)
	}

	// --page 2 fetches the second page, not the third.
	_, errs, code = run(t, "search", "hornillo gas", "--page", "2", "--debug")
	if code != 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(errs, "from=40&size=40") {
		t.Fatalf("--page 2 did not fetch the second page:\n%s", errs)
	}
	if strings.Contains(errs, "from=80") {
		t.Fatalf("--page 2 skipped to the third page:\n%s", errs)
	}

	// --page 1 is the first page, fetched as-is.
	_, errs, code = run(t, "search", "hornillo gas", "--page", "1", "--debug")
	if code != 0 {
		t.Fatal(errs)
	}
	if strings.Contains(errs, "from=") {
		t.Fatalf("--page 1 asked for a later page:\n%s", errs)
	}

	if _, _, code := run(t, "search", "hornillo gas", "--page", "0"); code != 2 {
		t.Fatalf("--page 0 accepted, exit %d", code)
	}
}

// Regression (bug 13): the search-watch loop advances a page number, so with
// 1-based numbering it must start at 1 and still walk forward.
func TestWatchSearchPagesAreOneBased(t *testing.T) {
	newFake(t)
	_, errs, code := run(t, "watch", "add", "search", "hornillo gas", "--pages", "2", "--name", "stoves", "--debug")
	if code != 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(errs, "from=40&size=40") {
		t.Fatalf("the second watched page was never fetched:\n%s", errs)
	}
	if strings.Contains(errs, "from=80") {
		t.Fatalf("the watch asked for a third page it did not configure:\n%s", errs)
	}
}

func TestStoreStock(t *testing.T) {
	newFake(t)
	out, errs, code := run(t, "store", "stock", "303295/8513471", "--store", "169", "--store", "Badalona", "--all", "--format", "jsonl")
	if code != 0 {
		t.Fatal(errs)
	}
	got := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		var l struct {
			StoreID string `json:"store_id"`
			InStock bool   `json:"in_stock"`
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		got[l.StoreID] = l.InStock
	}
	if len(got) != 2 || !got["169"] || got["86"] {
		t.Fatalf("%v", got)
	}
}

func TestWatchReportsStoreStockChange(t *testing.T) {
	f := newFake(t)
	if _, errs, code := run(t, "watch", "add", "product", "303295/8513471", "--store", "169", "--name", "tent"); code != 0 {
		t.Fatal(errs)
	}
	// Pamplona (169) sells out.
	key := "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471"
	before := f.pages[key]
	after := bytes.Replace(before, []byte(`\"622\",\"169\",\"347\"`), []byte(`\"622\",\"347\"`), 1)
	if bytes.Equal(before, after) {
		t.Fatal("fixture edit did not apply")
	}
	f.pages[key] = after
	out, errs, code := run(t, "watch", "check", "--all")
	if code != 0 {
		t.Fatal(errs)
	}
	var evs []struct {
		Type  string `json:"type"`
		Store struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(out), &evs); err != nil {
		t.Fatal(err, out)
	}
	if len(evs) != 1 || evs[0].Type != "store.out_of_stock" || evs[0].Store.ID != "169" || evs[0].Store.Name != "Pamplona" {
		t.Fatalf("%s", out)
	}
	// Idempotent: nothing changed since.
	out, _, _ = run(t, "watch", "check", "--all")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("second check: %s", out)
	}
}

// Regression (bug 4): `config set location.lng -1.64` read the longitude as
// a shorthand flag and failed.
func TestConfigRoundTrip(t *testing.T) {
	newFake(t)
	for _, args := range [][]string{
		{"config", "set", "sinks.hook.type", "webhook"},
		{"config", "set", "sinks.hook.url", "https://example.invalid/secret"},
		{"config", "set", "stores", `["169", "86"]`},
		{"config", "set", "location.lat", "42.81"},
		{"config", "set", "location.lng", "-1.64"},
	} {
		if _, errs, code := run(t, args...); code != 0 {
			t.Fatal(args, errs)
		}
	}
	if fi, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli", "config.toml")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config.toml mode: %v %v", fi.Mode(), err)
	}
	// The config directory holds webhook secrets too; it is private, not 0755.
	if fi, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("config dir mode: %v %v", fi.Mode(), err)
	}
	if _, _, code := run(t, "config", "set", "nonsense", "1"); code != 2 {
		t.Fatal("unknown key accepted")
	}
	out, _, _ := run(t, "config", "get", "stores")
	if !strings.Contains(out, `"169"`) {
		t.Fatal(out)
	}
	// Config stores and location now drive store stock.
	out, errs, code := run(t, "store", "stock", "303295/8513471", "--format", "jsonl")
	if code != 0 || strings.Count(out, "\n") != 1 || !strings.Contains(out, `"store_id":"169"`) {
		t.Fatalf("exit %d %s %s", code, out, errs)
	}
}

func TestMCP(t *testing.T) {
	newFake(t)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"store_stock","arguments":{"product":"303295/8513471","store":["169"]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search","arguments":{"keywords":"x","bogus":1}}}`,
	}, "\n") + "\n"
	var out, errb bytes.Buffer
	if code := Execute("1.2.3", []string{"mcp"}, strings.NewReader(in), &out, &errb); code != 0 {
		t.Fatalf("exit %d %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d responses: %s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-03-26"`) || !strings.Contains(lines[1], `"store_stock"`) {
		t.Fatal(lines[:2])
	}
	if !strings.Contains(lines[2], `\"store_id\": \"169\"`) || strings.Contains(lines[2], `"isError":true`) {
		t.Fatal(lines[2])
	}
	if !strings.Contains(lines[3], `-32602`) {
		t.Fatal(lines[3])
	}
}

// Regression (bug 6): product pages list only sizes that exist. A size sold
// out everywhere disappears from the page, and the watch said nothing,
// because it only diffed the SKUs it could still see.
func TestWatchReportsVanishedSize(t *testing.T) {
	f := newFake(t)
	if _, errs, code := run(t, "watch", "add", "product", "303295/8513471", "--store", "169", "--name", "tent"); code != 0 {
		t.Fatal(errs)
	}
	key := "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471"
	// The old SKU is gone and a new one took its place.
	f.pages[key] = bytes.ReplaceAll(f.pages[key], []byte("4ea60a8f-fc26-430a-85dc-d522a07a104a"), []byte("11111111-2222-4333-8444-555555555555"))
	out, errs, code := run(t, "watch", "check", "--all", "--format", "jsonl")
	if code != 0 {
		t.Fatal(errs)
	}
	var types []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var e struct {
			Type string `json:"type"`
			SKU  struct {
				SkuID string `json:"sku_id"`
			} `json:"sku"`
		}
		json.Unmarshal([]byte(l), &e)
		types = append(types, e.Type+":"+e.SKU.SkuID[:4])
	}
	want := "sku.new:1111 store.in_stock:1111 sku.sold_out_online:4ea6 store.out_of_stock:4ea6"
	if strings.Join(types, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(types, " "), want)
	}
	// Still gone: no repeat.
	out, _, _ = run(t, "watch", "check", "--all")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("repeat: %s", out)
	}
}

// Regression (review T13): the depth limit was enforced twice, once by prune
// and once by Pretty; the second check and its field are gone.
func TestCategoryListDepth(t *testing.T) {
	newFake(t)
	out, errs, code := run(t, "category", "list", "--depth", "1")
	if code != 0 {
		t.Fatal(errs)
	}
	var top []struct {
		Path     string `json:"path"`
		Children []any  `json:"children"`
	}
	if err := json.Unmarshal([]byte(out), &top); err != nil || len(top) == 0 {
		t.Fatalf("depth 1: %v %s", err, out)
	}
	for _, c := range top {
		if len(c.Children) != 0 {
			t.Fatalf("depth 1 kept children under %q", c.Path)
		}
	}
	out, errs, code = run(t, "category", "list", "--depth", "3")
	if code != 0 {
		t.Fatal(errs)
	}
	if !strings.Contains(out, `"children"`) {
		t.Fatalf("depth 3 lost the tree: %s", out)
	}
}
