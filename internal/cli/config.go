package cli

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and change config.toml",
		Long: `Read and change config.toml. Keys are dotted paths; arrays are written as
TOML arrays.

Keys:
  flaresolverr_url        FlareSolverr endpoint (env FLARESOLVERR_URL wins)
  location.lat/.lng       default centre for distances
  stores                  default store ids for stock and product watches
  watch.interval          default watch interval (e.g. 30m)
  sinks.NAME.*            notification targets (see 'decathlon sink --help')

Examples:
  decathlon config path
  decathlon config set flaresolverr_url http://localhost:8191
  decathlon config set location.lat 42.8125
  decathlon config set location.lng -1.6458
  decathlon config set stores '["169", "86"]'
  decathlon config set sinks.phone.type ntfy
  decathlon config get sinks
  decathlon config unset sinks.phone`,
	}
	cmd.AddCommand(
		&cobra.Command{Use: "path", Short: "Print where config, session, state and cache live", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Printer.Print(map[string]string{"config": a.Paths.ConfigFile, "session": a.Paths.SessionFile, "state": a.Paths.StateDB, "cache": a.Paths.CacheDir})
		}},
		&cobra.Command{Use: "list", Short: "Print the whole config", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			return a.Printer.Print(a.Cfg)
		}},
		&cobra.Command{Use: "get KEY", Short: "Print one key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := a.configDoc()
			if err != nil {
				return err
			}
			v, ok := lookup(doc, strings.Split(args[0], "."))
			if !ok {
				return output.Usagef("config has no key %q", args[0])
			}
			return a.Printer.Print(v)
		}},
		a.configSetCmd(),
		&cobra.Command{Use: "unset KEY", Short: "Remove one key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return a.configWrite(args[0], "", true)
		}},
	)
	return cmd
}

func (a *App) configSetCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set KEY VALUE", Short: "Set one key", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return a.configWrite(args[0], args[1], false)
	}}
	// Stop flag parsing at KEY, so negative values (a longitude) are values.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func (a *App) configDoc() (map[string]any, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(a.Paths.ConfigFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return nil, output.Usagef("%s: %v", a.Paths.ConfigFile, err)
		}
	}
	return doc, nil
}

// configWrite edits the raw document, then validates it against the schema
// before anything touches the file.
func (a *App) configWrite(key, value string, unset bool) error {
	doc, err := a.configDoc()
	if err != nil {
		return err
	}
	path := strings.Split(key, ".")
	for _, p := range path {
		if p == "" {
			return output.Usagef("bad key %q", key)
		}
	}
	var v any
	if unset {
		if !remove(doc, path) {
			return output.Usagef("config has no key %q", key)
		}
	} else {
		v = typed(value)
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			var arr struct {
				V []any `toml:"v"`
			}
			if err := toml.Unmarshal([]byte("v = "+value), &arr); err != nil {
				return output.Usagef("%s: %q is not a TOML array: %v", key, value, err)
			}
			v = arr.V
		}
		assign(doc, path, v)
	}
	raw, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	var cfg config.Config
	if err := config.Decode(raw, &cfg); err != nil {
		return output.Usagef("%s: %v", key, err)
	}
	if cfg.FlareSolverrURL != "" {
		if u, err := url.Parse(cfg.FlareSolverrURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return output.Usagef("flaresolverr_url must be an http(s) URL such as http://localhost:8191")
		}
	}
	if err := config.Save(a.Paths.ConfigFile, cfg); err != nil {
		return err
	}
	a.Cfg = cfg
	if unset {
		return a.Printer.Print(map[string]any{"unset": key})
	}
	return a.Printer.Print(map[string]any{key: v})
}

func lookup(doc map[string]any, path []string) (any, bool) {
	var cur any = doc
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func assign(doc map[string]any, path []string, v any) {
	cur := doc
	for _, p := range path[:len(path)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[path[len(path)-1]] = v
}

func remove(doc map[string]any, path []string) bool {
	cur := doc
	for _, p := range path[:len(path)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return false
		}
		cur = next
	}
	last := path[len(path)-1]
	if _, ok := cur[last]; !ok {
		return false
	}
	delete(cur, last)
	return true
}

// typed reads a scalar: bool, number, else string. Store ids are strings
// in the schema, and the strict decode turns a mismatch into a clear error.
func typed(s string) any {
	if s == "true" || s == "false" {
		return s == "true"
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return f
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}
