// Package config owns where the CLI keeps things on disk and the
// config.toml schema. Nothing here talks to the network.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/pelletier/go-toml/v2"

	"github.com/n0nuser/decathlon-cli/internal/api"
)

const appDir = "decathlon-cli"

// Paths are resolved once per invocation from the XDG base directories.
type Paths struct {
	ConfigFile  string // config.toml
	SessionFile string // the Cloudflare session minted by FlareSolverr (0600)
	StateDB     string // watches, seen state, events
	CacheDir    string // cached store directory, menu and filter pages
	StateDir    string // logs, update-check stamp
}

// DefaultPaths honours XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_CACHE_HOME and
// XDG_STATE_HOME. DECATHLON_CONFIG points at another config file.
func DefaultPaths() Paths {
	xdg.Reload()
	p := Paths{
		ConfigFile:  filepath.Join(xdg.ConfigHome, appDir, "config.toml"),
		SessionFile: filepath.Join(xdg.ConfigHome, appDir, "session.json"),
		StateDB:     filepath.Join(xdg.DataHome, appDir, "state.db"),
		CacheDir:    filepath.Join(xdg.CacheHome, appDir),
		StateDir:    filepath.Join(xdg.StateHome, appDir),
	}
	if v := os.Getenv("DECATHLON_CONFIG"); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			v = abs
		}
		p.ConfigFile = v
	}
	return p
}

// Config is config.toml. Unknown keys are an error, so a typo never
// silently does nothing.
type Config struct {
	// FlareSolverrURL is the FlareSolverr instance that mints sessions.
	// FLARESOLVERR_URL overrides it.
	FlareSolverrURL string `toml:"flaresolverr_url,omitempty" json:"flaresolverr_url,omitempty"`
	// Location is the default centre for `store list --near` and stock
	// reports, so the closest stores come first.
	Location *Location `toml:"location,omitempty" json:"location,omitempty"`
	// Stores are default store ids for `store stock` and product watches.
	Stores []string              `toml:"stores,omitempty" json:"stores,omitempty"`
	Watch  WatchConfig           `toml:"watch,omitempty" json:"watch,omitzero"`
	Sinks  map[string]SinkConfig `toml:"sinks,omitempty" json:"sinks,omitempty"`
}

type Location struct {
	Lat float64 `toml:"lat" json:"lat"`
	Lng float64 `toml:"lng" json:"lng"`
}

type WatchConfig struct {
	Interval string `toml:"interval,omitempty" json:"interval,omitempty"`
}

// SinkConfig is one notification target. Type picks which fields apply.
type SinkConfig struct {
	Type      string   `toml:"type" json:"type"` // ntfy, webhook, exec
	URL       string   `toml:"url,omitempty" json:"url,omitempty"`
	Topic     string   `toml:"topic,omitempty" json:"topic,omitempty"`
	TokenFile string   `toml:"token_file,omitempty" json:"token_file,omitempty"`
	Template  string   `toml:"template,omitempty" json:"template,omitempty"` // webhook: discord, slack, or raw event
	Command   []string `toml:"command,omitempty" json:"command,omitempty"`
}

// Load reads config.toml. A missing file is an empty config.
func Load(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := Decode(raw, &c); err != nil {
		return c, &api.Error{Kind: api.KindUsage, Msg: fmt.Sprintf("%s: %v", path, err)}
	}
	return c, nil
}

// Decode parses TOML strictly.
func Decode(raw []byte, c *Config) error {
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(c); err != nil {
		var sm *toml.StrictMissingError
		if errors.As(err, &sm) {
			return errors.New(strings.TrimSpace(sm.String()))
		}
		return err
	}
	return nil
}

// Save writes config.toml atomically. It holds no secrets (sink tokens live
// in token_file), so it is world-readable like any dotfile.
func Save(path string, c Config) error {
	raw, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return api.WriteFileAtomic(path, raw, 0o644)
}

// SolverURL picks FLARESOLVERR_URL, then config, then the default.
func (c Config) SolverURL() string {
	if v := os.Getenv("FLARESOLVERR_URL"); v != "" {
		return v
	}
	if c.FlareSolverrURL != "" {
		return c.FlareSolverrURL
	}
	return api.DefaultSolverURL
}
