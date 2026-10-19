package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/n0nuser/decathlon-cli/internal/api"
)

func TestDefaultPaths(t *testing.T) {
	home := t.TempDir()
	for k, v := range map[string]string{
		"XDG_CONFIG_HOME":  filepath.Join(home, "cfg"),
		"XDG_DATA_HOME":    filepath.Join(home, "data"),
		"XDG_CACHE_HOME":   filepath.Join(home, "cache"),
		"XDG_STATE_HOME":   filepath.Join(home, "state"),
		"DECATHLON_CONFIG": "",
	} {
		t.Setenv(k, v)
	}
	p := DefaultPaths()
	for _, c := range []struct{ got, want string }{
		{p.ConfigFile, filepath.Join(home, "cfg", "decathlon-cli", "config.toml")},
		{p.SessionFile, filepath.Join(home, "cfg", "decathlon-cli", "session.json")},
		{p.StateDB, filepath.Join(home, "data", "decathlon-cli", "state.db")},
		{p.CacheDir, filepath.Join(home, "cache", "decathlon-cli")},
		{p.StateDir, filepath.Join(home, "state", "decathlon-cli")},
	} {
		if c.got != c.want {
			t.Errorf("path %q, want %q", c.got, c.want)
		}
	}

	t.Setenv("DECATHLON_CONFIG", filepath.Join(home, "elsewhere", "conf.toml"))
	if got := DefaultPaths().ConfigFile; got != filepath.Join(home, "elsewhere", "conf.toml") {
		t.Fatalf("DECATHLON_CONFIG not honoured: %q", got)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || !reflect.DeepEqual(c, Config{}) {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	want := Config{
		FlareSolverrURL: "http://solver:8191",
		Location:        &Location{Lat: 42.8125, Lng: -1.6458},
		Stores:          []string{"169", "86"},
		Watch:           WatchConfig{Interval: "30m"},
		Sinks: map[string]SinkConfig{
			"phone": {Type: "ntfy", URL: "https://ntfy.sh", Topic: "alerts", TokenFile: "/run/token"},
			"chat":  {Type: "webhook", URL: "https://example.test/hook", Template: "discord"},
			"exec":  {Type: "exec", Command: []string{"/bin/true", "--flag"}},
		},
	}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v", fi.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, want)
	}
}

func TestUnknownKeyIsAUsageError(t *testing.T) {
	var c Config
	if err := Decode([]byte("nonsense = 1\n"), &c); err == nil || !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("strict decode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("nonsense = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	var e *api.Error
	if !errors.As(err, &e) || e.Kind != api.KindUsage {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestSolverURLPrecedence(t *testing.T) {
	c := Config{FlareSolverrURL: "http://config:8191"}
	t.Setenv("FLARESOLVERR_URL", "")
	if got := c.SolverURL(); got != "http://config:8191" {
		t.Fatalf("config: %q", got)
	}
	t.Setenv("FLARESOLVERR_URL", "http://env:8191")
	if got := c.SolverURL(); got != "http://env:8191" {
		t.Fatalf("env should win: %q", got)
	}
	t.Setenv("FLARESOLVERR_URL", "")
	if got := (Config{}).SolverURL(); got != api.DefaultSolverURL {
		t.Fatalf("default: %q", got)
	}
}
