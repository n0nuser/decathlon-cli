package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/n0nuser/decathlon-cli/internal/api"
)

// Harnesses that `mcp install` knows.
var Harnesses = []string{"claude-code", "codex", "cursor"}

// InstallResult says what install did.
type InstallResult struct {
	Harness string   `json:"harness"`
	Path    string   `json:"path"`
	Action  string   `json:"action"` // added, updated, unchanged
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ConfigPath is the file each harness reads its MCP servers from.
func ConfigPath(harness string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch harness {
	case "claude-code":
		if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, ".claude.json"), nil
		}
		return filepath.Join(home, ".claude.json"), nil
	case "cursor":
		return filepath.Join(home, ".cursor", "mcp.json"), nil
	case "codex":
		if d := os.Getenv("CODEX_HOME"); d != "" {
			return filepath.Join(d, "config.toml"), nil
		}
		return filepath.Join(home, ".codex", "config.toml"), nil
	}
	return "", fmt.Errorf("unknown harness %q; use one of %s", harness, strings.Join(Harnesses, ", "))
}

// Install registers `<exe> mcp` as the "decathlon" server. Only the command
// and args keys of that one entry are written; everything else in the file
// is kept.
func Install(harness, exe string) (InstallResult, error) {
	path, err := ConfigPath(harness)
	if err != nil {
		return InstallResult{}, err
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // keep a dotfile manager's symlink pointing at its file
	}
	res := InstallResult{Harness: harness, Path: path, Command: exe, Args: []string{"mcp"}}
	if harness == "codex" {
		res.Action, err = installTOML(path, exe)
	} else {
		res.Action, err = installJSON(path, exe)
	}
	return res, err
}

func installJSON(path, exe string) (string, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return "", fmt.Errorf("%s is not valid JSON (%v); not touching it", path, err)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	entry, _ := servers["decathlon"].(map[string]any)
	action := "updated"
	if entry == nil {
		entry, action = map[string]any{}, "added"
	}
	want := []any{"mcp"}
	if entry["command"] == exe && reflect.DeepEqual(entry["args"], want) {
		return "unchanged", nil
	}
	entry["command"], entry["args"] = exe, want
	if _, ok := entry["type"]; !ok {
		entry["type"] = "stdio"
	}
	servers["decathlon"] = entry
	doc["mcpServers"] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return action, api.WriteFileAtomic(path, append(out, '\n'), 0o600)
}

// installTOML edits ~/.codex/config.toml as text so comments and ordering
// survive: it rewrites the command and args lines of [mcp_servers.decathlon],
// or appends that table.
func installTOML(path, exe string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	text := string(raw)
	cmdLine := "command = " + strconv.Quote(exe)
	argsLine := `args = ["mcp"]`
	lines := strings.Split(text, "\n")
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "[mcp_servers.decathlon]" {
			start = i
		} else if strings.Contains(t, "mcp_servers") && strings.Contains(t, "decathlon") && strings.HasPrefix(t, "[") {
			return "", fmt.Errorf("%s defines the decathlon server as %q, which this installer does not rewrite; set command = %q and args = [\"mcp\"] by hand", path, t, exe)
		}
	}
	if start < 0 {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n[mcp_servers.decathlon]\n" + cmdLine + "\n" + argsLine + "\n"
		return "added", api.WriteFileAtomic(path, []byte(text), 0o600)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	changed, sawCmd, sawArgs := false, false, false
	for i := start + 1; i < end; i++ {
		key, _, ok := strings.Cut(strings.TrimSpace(lines[i]), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "command":
			sawCmd = true
			if strings.TrimSpace(lines[i]) != cmdLine {
				lines[i], changed = cmdLine, true
			}
		case "args":
			sawArgs = true
			if strings.TrimSpace(lines[i]) != argsLine {
				lines[i], changed = argsLine, true
			}
		}
	}
	var add []string
	if !sawCmd {
		add = append(add, cmdLine)
	}
	if !sawArgs {
		add = append(add, argsLine)
	}
	if len(add) > 0 {
		lines = append(lines[:start+1], append(add, lines[start+1:]...)...)
		changed = true
	}
	if !changed {
		return "unchanged", nil
	}
	return "updated", api.WriteFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o600)
}
