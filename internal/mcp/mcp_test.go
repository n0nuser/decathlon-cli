package mcp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestArgv(t *testing.T) {
	got, err := Argv("search", map[string]any{"keywords": "tienda campaña", "brand": []any{"quechua"}, "max_price": 60.0, "filter": map[string]any{"Nº de plazas": "2 plazas"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"search", "--brand=quechua", "--filter=Nº de plazas=2 plazas", "--max-price=60", "--", "tienda campaña"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	for name, args := range map[string]map[string]any{
		"search":        {"keywords": 3.0},
		"store_stock":   {"store": []any{"169"}},
		"product_show":  {"product": "1/2", "extra": true},
		"category_list": {"depth": 1.5},
		"nope":          {},
	} {
		if _, err := Argv(name, args); err == nil {
			t.Errorf("%s %v accepted", name, args)
		}
	}
}

// Regression (bug 12): the installer re-marshalled the whole harness file
// through float64, so a big id or a precise float came back changed.
func TestInstallKeepsNumbers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	cfg := filepath.Join(dir, ".claude.json")
	os.WriteFile(cfg, []byte(`{"n":12345678901234567890,"f":0.30000000000000004,"mcpServers":{}}`), 0o600)
	if _, err := Install("claude-code", "/bin/decathlon"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfg)
	for _, want := range []string{"12345678901234567890", "0.30000000000000004"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s did not survive: %s", want, raw)
		}
	}
}

func TestInstallKeepsOtherEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CODEX_HOME", dir)
	cfg := filepath.Join(dir, ".claude.json")
	os.WriteFile(cfg, []byte(`{"theme":"dark","mcpServers":{"other":{"command":"x"}}}`), 0o600)
	for _, want := range []string{"added", "unchanged"} {
		r, err := Install("claude-code", "/usr/local/bin/decathlon")
		if err != nil || r.Action != want {
			t.Fatalf("%+v %v", r, err)
		}
	}
	raw, _ := os.ReadFile(cfg)
	if !strings.Contains(string(raw), `"theme": "dark"`) || !strings.Contains(string(raw), `"other"`) {
		t.Fatal(string(raw))
	}
	toml := filepath.Join(dir, "config.toml")
	os.WriteFile(toml, []byte("# mine\nmodel = \"x\"\n"), 0o600)
	if r, err := Install("codex", "/bin/decathlon"); err != nil || r.Action != "added" {
		t.Fatal(r, err)
	}
	if r, err := Install("codex", "/opt/decathlon"); err != nil || r.Action != "updated" {
		t.Fatal(r, err)
	}
	raw, _ = os.ReadFile(toml)
	if !strings.HasPrefix(string(raw), "# mine\n") || strings.Count(string(raw), "[mcp_servers.decathlon]") != 1 || !strings.Contains(string(raw), `command = "/opt/decathlon"`) {
		t.Fatal(string(raw))
	}
}
