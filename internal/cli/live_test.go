package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/config"
)

// TestLiveSearch is the only test that touches the network. It is off unless
// DECATHLON_LIVE_TEST=1 and skips when FlareSolverr or a session is missing,
// so `go test ./...` stays fully offline.
func TestLiveSearch(t *testing.T) {
	if os.Getenv("DECATHLON_LIVE_TEST") != "1" {
		t.Skip("live test: set DECATHLON_LIVE_TEST=1 (needs FlareSolverr and decathlon.es)")
	}
	paths := config.DefaultPaths()
	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		t.Skipf("live test skipped: config.toml does not parse: %v", err)
	}
	c := api.New()
	c.SolverURL = cfg.SolverURL()
	c.SessionFile = paths.SessionFile
	c.CacheDir = "" // no cache: the point is a real request
	c.Notice = func(s string) { t.Log(s) }

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := c.SolverStatus(ctx); err != nil {
		t.Skipf("live test skipped: FlareSolverr is not reachable at %s: %v", c.SolverURL, err)
	}
	if _, err := c.StoredSession(); err != nil {
		if err := c.Mint(ctx); err != nil {
			t.Skipf("live test skipped: no stored session and FlareSolverr could not mint one: %v", err)
		}
	}
	r, err := c.Search(ctx, api.SearchParams{Query: "hornillo gas"})
	if err != nil {
		t.Fatalf("live search: %v", err)
	}
	if r.Total <= 0 || len(r.Products) == 0 {
		t.Fatalf("live search returned no products (total %d)", r.Total)
	}
	t.Logf("live search: %d products, first %s", r.Total, r.Products[0].Ref)
}
