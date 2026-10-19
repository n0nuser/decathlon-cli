package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustRun(t *testing.T, args ...string) (string, string) {
	t.Helper()
	out, errs, code := run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, errs)
	}
	return out, errs
}

func TestSearchFormatsFiltersAndErrors(t *testing.T) {
	f := newFake(t)
	out, _ := mustRun(t, "search", "hornillo gas", "--format", "jsonl", "--limit", "2")
	if strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Fatalf("jsonl: %q", out)
	}
	if _, errs := mustRun(t, "search", "hornillo gas", "--format", "toon", "--limit", "1"); errs != "" {
		t.Fatalf("toon stderr: %s", errs)
	}
	out, _ = mustRun(t, "--no-color", "search", "hornillo gas", "--limit", "1", "--format", "pretty")
	if strings.Contains(out, "\x1b") {
		t.Fatalf("colour leaked with --no-color: %q", out)
	}
	out, _ = mustRun(t, "search", "filters", "hornillo gas")
	if !strings.Contains(out, `"id"`) {
		t.Fatalf("filters json: %s", out)
	}
	mustRun(t, "search", "filters", "--category", "deportes/camping/tiendas-campana", "--format", "pretty")
	mustRun(t, "search", "hornillo gas", "--brand", "campingaz", "--max-price", "40", "--sort", "price_asc")
	_, errs := mustRun(t, "search", "hornillo gas", "--page", "2", "--debug", "--limit", "1")
	if !strings.Contains(errs, "from=40") {
		t.Fatalf("page 2 did not fetch from=40: %s", errs)
	}
	mustRun(t, "search", "hornillo gas", "--pages", "2", "--limit", "3")
	// A filter that only the details page offers is a usage error here.
	_ = f

	for _, args := range [][]string{
		{"search", "hornillo gas", "--delivery", "24h"},
		{"search", "hornillo gas", "--filter", "Nº de plazas=2 plazas"},
		{"search", "hornillo gas", "--min-price", "10", "--max-price", "5"},
		{"search", "filters", "--category", "no/such"},
		{"search", "--category", "no/such"},
		{"search", "hornillo gas", "--filter", "noequals"},
	} {
		if _, _, code := run(t, args...); code == 0 {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestProductAndCategoryCommands(t *testing.T) {
	newFake(t)
	out, _ := mustRun(t, "product", "show", "303295/8513471", "--format", "pretty")
	if !strings.Contains(out, "Talla única") {
		t.Fatalf("product pretty: %s", out)
	}
	out, _ = mustRun(t, "product", "show", "303295/8513471")
	if !strings.Contains(out, `"skus"`) {
		t.Fatalf("product json: %s", out)
	}
	mustRun(t, "item", "show", "303295/8513471")
	if _, _, code := run(t, "product", "show", "bad"); code != 2 {
		t.Fatalf("bad ref exit %d", code)
	}
	if _, _, code := run(t, "product", "show", "1/2"); code != 4 {
		t.Fatalf("missing product exit %d", code)
	}

	mustRun(t, "category", "list", "--depth", "2", "--format", "pretty")
	out, _ = mustRun(t, "category", "list", "--depth", "3")
	if !strings.Contains(out, `"children"`) {
		t.Fatalf("depth 3: %s", out)
	}
	out, _ = mustRun(t, "category", "show", "deportes/camping/tiendas-campana", "--format", "pretty")
	if !strings.Contains(out, "Tiendas campaña") {
		t.Fatalf("category show: %s", out)
	}
	out, _ = mustRun(t, "category", "show", "deportes/camping", "--format", "pretty")
	if !strings.Contains(out, "PATH") {
		t.Fatalf("landing show: %s", out)
	}
	if _, _, code := run(t, "category", "show", "no/such"); code != 4 {
		t.Fatalf("missing category exit %d", code)
	}
	if _, _, code := run(t, "category", "list", "--depth", "0"); code != 2 {
		t.Fatalf("depth 0 exit %d", code)
	}
}

func TestStoreCommands(t *testing.T) {
	newFake(t)
	out, _ := mustRun(t, "store", "list", "--near", "42.81,-1.64", "--limit", "3", "--format", "pretty")
	if !strings.Contains(out, "DISTANCE") {
		t.Fatalf("store list: %s", out)
	}
	out, _ = mustRun(t, "store", "list", "--city", "pamplona", "--limit", "1", "--format", "pretty")
	if strings.Count(strings.TrimSpace(out), "\n") != 1 {
		t.Fatalf("city filter: %q", out)
	}
	if _, _, code := run(t, "store", "list", "--near", "bad"); code != 2 {
		t.Fatalf("bad near exit %d", code)
	}
	out, _ = mustRun(t, "store", "show", "169", "--format", "pretty")
	if !strings.Contains(out, "Pamplona") || !strings.Contains(out, "MONDAY") && !strings.Contains(out, "monday") {
		t.Fatalf("store show: %s", out)
	}
	mustRun(t, "store", "show", "00169")
	mustRun(t, "store", "show", "Pamplona")
	if _, _, code := run(t, "store", "show", "Pamp"); code != 2 {
		t.Fatalf("ambiguous store exit %d", code)
	}
	if _, _, code := run(t, "store", "show", "99999"); code != 4 {
		t.Fatalf("missing store exit %d", code)
	}
	if _, _, code := run(t, "store", "show", "zzz"); code != 4 {
		t.Fatalf("missing name exit %d", code)
	}
	mustRun(t, "store", "stock", "303295/8513471", "--store", "169", "--all", "--limit", "2", "--near", "42.81,-1.64")
	mustRun(t, "store", "stock", "303295/8513471", "--size", "Talla única", "--model", "8513471", "--store", "169")
	if _, _, code := run(t, "store", "stock", "303295/8513471", "--size", "nope"); code != 4 {
		t.Fatalf("bad size exit %d", code)
	}
	if _, _, code := run(t, "store", "stock", "303295/8513471", "--model", "nope"); code != 4 {
		t.Fatalf("bad model exit %d", code)
	}
	if _, _, code := run(t, "store", "stock", "303295/8513471", "--store", "99999"); code != 4 {
		t.Fatalf("bad store exit %d", code)
	}
	// Config stores are the default for stock.
	mustRun(t, "config", "set", "stores", `["169"]`)
	out, _ = mustRun(t, "store", "stock", "303295/8513471", "--format", "jsonl")
	if !strings.Contains(out, `"store_id":"169"`) {
		t.Fatalf("config stores: %s", out)
	}
}

func TestWatchCommands(t *testing.T) {
	f := newFake(t)
	mustRun(t, "watch", "add", "search", "hornillo gas", "--name", "s1", "--pages", "2", "--format", "pretty")
	mustRun(t, "watch", "add", "product", "303295/8513471", "--store", "169", "--name", "p1")
	mustRun(t, "watch", "add", "product", "303295/8513471", "--size", "Talla única", "--name", "p2", "--emit-initial")
	mustRun(t, "watch", "list", "--format", "pretty")
	mustRun(t, "watch", "list")
	for _, args := range [][]string{
		{"watch", "add", "search", "hornillo gas", "--name", "s1"},
		{"watch", "add", "search", "hornillo gas", "--name", "bad name"},
		{"watch", "add", "search", "hornillo gas", "--name", "x", "--notify", "nope"},
		{"watch", "add", "search", "hornillo gas", "--name", "x", "--interval", "1m"},
		{"watch", "add", "search", "hornillo gas", "--name", "x", "--pages", "6"},
		{"watch", "add", "product", "303295/8513471", "--name", "x", "--size", "nope"},
	} {
		if _, _, code := run(t, args...); code == 0 {
			t.Errorf("%v accepted", args)
		}
	}

	mustRun(t, "watch", "check", "--all", "--format", "jsonl")
	mustRun(t, "watch", "check", "p1")
	if _, _, code := run(t, "watch", "check", "nope"); code != 4 {
		t.Fatalf("unknown watch exit %d", code)
	}
	// A store sells out; the check reports it and the event is stored.
	key := "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471"
	before := f.pages[key]
	f.pages[key] = bytes.Replace(before, []byte(`\"622\",\"169\",\"347\"`), []byte(`\"622\",\"347\"`), 1)
	if bytes.Equal(before, f.pages[key]) {
		t.Fatal("fixture patch did not apply")
	}
	out, _ := mustRun(t, "watch", "check", "--all")
	if !strings.Contains(out, "store.out_of_stock") {
		t.Fatalf("check: %s", out)
	}
	mustRun(t, "watch", "events", "--format", "pretty", "--limit", "2")
	mustRun(t, "watch", "events", "p1", "--since", "24h")

	if _, _, code := run(t, "watch", "remove", "p1"); code != 2 {
		t.Fatalf("remove without --yes exit %d", code)
	}
	mustRun(t, "watch", "remove", "p1", "--yes")
	if _, _, code := run(t, "watch", "remove", "p1", "--yes"); code != 4 {
		t.Fatalf("remove missing exit %d", code)
	}
	if _, _, code := run(t, "watch", "run", "--interval", "1m"); code != 2 {
		t.Fatalf("watch run interval exit %d", code)
	}
}

func TestWatchRunLoop(t *testing.T) {
	newFake(t)
	var out, errb bytes.Buffer
	a := &App{Version: "1.2.3", Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb}
	root := a.rootCmd()
	root.SetArgs([]string{"watch", "run", "--interval", "5m"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := root.ExecuteContextC(ctx)
	if a.store != nil {
		a.store.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("watch run: %v", err)
	}
}

func TestSinkCommands(t *testing.T) {
	newFake(t)
	out, _ := mustRun(t, "sink", "list", "--format", "pretty")
	if !strings.Contains(out, "SINK") {
		t.Fatalf("empty sink list: %s", out)
	}
	body := filepath.Join(t.TempDir(), "event.json")
	mustRun(t, "config", "set", "sinks.phone.type", "ntfy")
	mustRun(t, "config", "set", "sinks.phone.url", "http://127.0.0.1:1")
	mustRun(t, "config", "set", "sinks.phone.topic", "alerts")
	mustRun(t, "config", "set", "sinks.script.type", "exec")
	mustRun(t, "config", "set", "sinks.script.command", `["/bin/sh","-c","cat > `+body+`"]`)
	mustRun(t, "config", "set", "sinks.bad.type", "bogus")
	out, _ = mustRun(t, "sink", "list")
	if !strings.Contains(out, `"problem"`) {
		t.Fatalf("sink list: %s", out)
	}
	out, _ = mustRun(t, "sink", "test", "script")
	if !strings.Contains(out, `"sent": true`) {
		t.Fatalf("sink test: %s", out)
	}
	if raw, err := os.ReadFile(body); err != nil || !strings.Contains(string(raw), "sink-test") {
		t.Fatalf("exec sink body %s %v", raw, err)
	}
	if _, _, code := run(t, "sink", "test", "nope"); code != 2 {
		t.Fatalf("unknown sink exit %d", code)
	}
	if _, _, code := run(t, "sink", "test", "bad"); code != 1 {
		t.Fatalf("invalid sink exit %d", code)
	}
}

func TestConfigCommands(t *testing.T) {
	newFake(t)
	mustRun(t, "config", "path")
	mustRun(t, "config", "list")
	if _, _, code := run(t, "config", "get", "nope"); code != 2 {
		t.Fatalf("get missing exit %d", code)
	}
	mustRun(t, "config", "set", "location.lat", "42.8")
	mustRun(t, "config", "get", "location")
	mustRun(t, "config", "set", "watch.interval", "45m")
	mustRun(t, "config", "set", "stores", `["169"]`)
	mustRun(t, "config", "get", "stores")
	mustRun(t, "config", "set", "sinks.phone.type", "ntfy")
	mustRun(t, "config", "get", "sinks")
	mustRun(t, "config", "unset", "location")
	if _, _, code := run(t, "config", "unset", "location"); code != 2 {
		t.Fatalf("unset missing exit %d", code)
	}
	for _, args := range [][]string{
		{"config", "set", "stores", `[bad`},
		{"config", "set", "flaresolverr_url", "not a url"},
		{"config", "set", "stores", "169"},
		{"config", "set", "", "x"},
	} {
		if _, _, code := run(t, args...); code != 2 {
			t.Errorf("%v exit %d", args, code)
		}
	}
}

func TestDoctorSkillsAndMCPInstall(t *testing.T) {
	newFake(t)
	mustRun(t, "doctor", "--format", "pretty")
	mustRun(t, "doctor")
	mustRun(t, "doctor", "--mint")
	mustRun(t, "skills", "list")
	mustRun(t, "skills", "get", "decathlon-usage")
	if _, _, code := run(t, "skills", "get", "nope"); code != 2 {
		t.Fatalf("unknown skill exit %d", code)
	}

	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CODEX_HOME", dir)
	t.Setenv("HOME", dir)
	mustRun(t, "mcp", "install", "claude-code")
	mustRun(t, "mcp", "install", "claude-code")
	mustRun(t, "mcp", "install", "codex")
	mustRun(t, "mcp", "install", "cursor")
	if _, _, code := run(t, "mcp", "install", "vim"); code != 2 {
		t.Fatalf("unknown harness exit %d", code)
	}
}

func TestDoctorFailures(t *testing.T) {
	newFake(t)
	mustRun(t, "config", "set", "sinks.bad.type", "bogus")
	if _, _, code := run(t, "doctor"); code != 1 {
		t.Fatalf("doctor with a bad sink exit %d", code)
	}
	// A solver that is down and no mintable session is a failed check.
	t.Setenv("FLARESOLVERR_URL", "http://127.0.0.1:1")
	os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "decathlon-cli", "session.json"))
	if out, errs, code := run(t, "doctor", "--format", "pretty"); code != 1 || !strings.Contains(out, "FAIL") {
		t.Fatalf("doctor down: exit %d %s %s", code, out, errs)
	}
}

func TestRootAndUsageErrors(t *testing.T) {
	newFake(t)
	out, _ := mustRun(t, "--version")
	if !strings.Contains(out, "1.2.3") {
		t.Fatalf("version: %s", out)
	}
	out, _ = mustRun(t, "--help")
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help: %s", out)
	}
	mustRun(t, "help")
	out, _ = mustRun(t, "completion", "bash")
	if !strings.Contains(out, "bash completion") && !strings.Contains(out, "_decathlon") {
		t.Fatalf("completion: %s", out)
	}
	if _, _, code := run(t, "--format", "yaml", "search", "hornillo gas"); code != 2 {
		t.Fatalf("bad --format exit %d", code)
	}
	if _, _, code := run(t, "--error-format", "yaml", "search", "hornillo gas"); code != 2 {
		t.Fatalf("bad --error-format exit %d", code)
	}
	_, errs, code := run(t, "--error-format", "json", "search")
	var env struct {
		Code     int    `json:"code"`
		Category string `json:"category"`
	}
	if code != 2 || json.Unmarshal([]byte(errs), &env) != nil || env.Category != "usage" {
		t.Fatalf("json usage: exit %d %q", code, errs)
	}
	t.Setenv("DECATHLON_ERROR_FORMAT", "json")
	_, errs, code = run(t, "search")
	if code != 2 || json.Unmarshal([]byte(errs), &env) != nil || env.Category != "usage" {
		t.Fatalf("env json usage: exit %d %q", code, errs)
	}
	t.Setenv("DECATHLON_REQUEST_GAP", "garbage")
	if _, _, code := run(t, "store", "list"); code != 2 {
		t.Fatalf("bad gap exit %d", code)
	}
	t.Setenv("DECATHLON_REQUEST_GAP", "0s")
	_, errs = mustRun(t, "--debug", "search", "hornillo gas", "--limit", "1")
	if !strings.Contains(errs, "GET ") {
		t.Fatalf("debug: %s", errs)
	}

	for _, args := range [][]string{
		{"nope"},
		{"store", "show"},
		{"store", "list", "extra"},
		{"product", "show", "1", "2"},
	} {
		if _, _, code := run(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	newFake(t)
	var out, errs bytes.Buffer
	if code := Execute("dev", []string{"update"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Fatalf("dev update exit %d: %s", code, errs.String())
	}
	if code := Execute("dev", []string{"update", "--check"}, strings.NewReader(""), &out, &errs); code != 2 {
		t.Fatalf("dev update --check exit %d", code)
	}
}
