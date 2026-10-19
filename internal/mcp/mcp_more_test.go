package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  struct {
		ProtocolVersion string           `json:"protocolVersion"`
		Tools           []map[string]any `json:"tools"`
		Content         []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func serve(t *testing.T, lines []string, run Runner) []rpcReply {
	t.Helper()
	if run == nil {
		run = func(context.Context, []string) Result { return Result{Stdout: "ok"} }
	}
	var out bytes.Buffer
	s := &Server{Version: "1.2.3", Run: run}
	if err := s.Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var replies []rpcReply
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var r rpcReply
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		replies = append(replies, r)
	}
	return replies
}

func TestServeProtocol(t *testing.T) {
	replies := serve(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"search","arguments":{"keywords":"x"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"search","arguments":{"bogus":1}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"nope"}`,
		`not json`,
		``,
	}, nil)
	if len(replies) != 9 {
		t.Fatalf("%d replies", len(replies))
	}
	if replies[0].Result.ProtocolVersion != "2025-03-26" {
		t.Errorf("known protocol: %q", replies[0].Result.ProtocolVersion)
	}
	if replies[1].Result.ProtocolVersion != versions[len(versions)-1] {
		t.Errorf("unknown protocol: %q", replies[1].Result.ProtocolVersion)
	}
	if len(replies[3].Result.Tools) == 0 {
		t.Error("tools/list is empty")
	}
	if replies[4].Result.Content[0].Text != "ok" || replies[4].Result.IsError {
		t.Errorf("tools/call: %+v", replies[4])
	}
	if replies[5].Error == nil || replies[5].Error.Code != -32602 {
		t.Errorf("unknown tool: %+v", replies[5])
	}
	if replies[6].Error == nil || replies[6].Error.Code != -32602 {
		t.Errorf("unknown argument: %+v", replies[6])
	}
	if replies[7].Error == nil || replies[7].Error.Code != -32601 {
		t.Errorf("unknown method: %+v", replies[7])
	}
	if replies[8].Error == nil || replies[8].Error.Code != -32700 {
		t.Errorf("parse error: %+v", replies[8])
	}
	if string(replies[0].ID) != "1" || replies[0].JSONRPC != "2.0" {
		t.Errorf("envelope: %+v", replies[0])
	}
}

func TestServeToolFailureCarriesOutputAndEnvelope(t *testing.T) {
	run := func(context.Context, []string) Result {
		return Result{Stdout: "partial\n", ErrorJSON: `{"code":2,"category":"usage"}`}
	}
	replies := serve(t, []string{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"keywords":"x"}}}`}, run)
	if !replies[0].Result.IsError || !strings.Contains(replies[0].Result.Content[0].Text, "partial") || !strings.Contains(replies[0].Result.Content[0].Text, `"code":2`) {
		t.Fatalf("%+v", replies[0])
	}
}

func TestServeBadParamsAndEOF(t *testing.T) {
	replies := serve(t, []string{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not an object"}`}, nil)
	if replies[0].Error == nil || replies[0].Error.Code != -32602 {
		t.Fatalf("%+v", replies[0])
	}
}

func TestArgvValuesAndErrors(t *testing.T) {
	got, err := Argv("store_stock", map[string]any{
		"product": "1/2",
		"store":   []any{"169", "86"},
		"all":     true,
		"limit":   3.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"store", "stock", "--all", "--limit=3", "--store=169", "--store=86", "--", "1/2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q\nwant %q", got, want)
	}
	// false boolean is omitted, not passed as --flag=false.
	got, _ = Argv("watch_check", map[string]any{"all": false, "names": []any{"a", "b"}})
	if !reflect.DeepEqual(got, []string{"watch", "check", "--", "a", "b"}) {
		t.Fatalf("%q", got)
	}
	for _, c := range []struct {
		name string
		args map[string]any
	}{
		{"search", map[string]any{"keywords": 1.0}},
		{"store_list", map[string]any{"limit": "x"}},
		{"store_list", map[string]any{"limit": 1.5}},
		{"store_stock", map[string]any{"product": "1/2", "all": "yes"}},
		{"store_stock", map[string]any{"product": "1/2", "store": "169"}},
		{"search", map[string]any{"keywords": "x", "filter": []any{"a"}}},
		{"search", map[string]any{"keywords": "x", "filter": map[string]any{"a": 1.0}}},
		{"product_show", map[string]any{}},
		{"category_list", map[string]any{"depth": true}},
	} {
		if _, err := Argv(c.name, c.args); err == nil {
			t.Errorf("%s %v accepted", c.name, c.args)
		}
	}
}

func TestConfigPathHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	claude, err := ConfigPath("claude-code")
	if err != nil || claude != filepath.Join(home, ".claude.json") {
		t.Fatalf("%q %v", claude, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "cc"))
	if got, _ := ConfigPath("claude-code"); got != filepath.Join(home, "cc", ".claude.json") {
		t.Fatalf("%q", got)
	}
	if got, _ := ConfigPath("cursor"); got != filepath.Join(home, ".cursor", "mcp.json") {
		t.Fatalf("%q", got)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, "cx"))
	if got, _ := ConfigPath("codex"); got != filepath.Join(home, "cx", "config.toml") {
		t.Fatalf("%q", got)
	}
	if _, err := ConfigPath("vim"); err == nil {
		t.Fatal("unknown harness accepted")
	}
}

func TestInstallJSONBranches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if action, err := installJSON(path, "/bin/decathlon"); err != nil || action != "added" {
		t.Fatalf("%q %v", action, err)
	}
	if action, err := installJSON(path, "/bin/decathlon"); err != nil || action != "unchanged" {
		t.Fatalf("%q %v", action, err)
	}
	// Another key and another server survive.
	raw, _ := os.ReadFile(path)
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	doc["theme"] = "dark"
	doc["mcpServers"].(map[string]any)["other"] = map[string]any{"command": "x"}
	out, _ := json.Marshal(doc)
	os.WriteFile(path, out, 0o600)
	if action, err := installJSON(path, "/opt/decathlon"); err != nil || action != "updated" {
		t.Fatalf("%q %v", action, err)
	}
	raw, _ = os.ReadFile(path)
	for _, want := range []string{`"theme": "dark"`, `"other"`, `"/opt/decathlon"`, `"type": "stdio"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s lacks %s", raw, want)
		}
	}
	os.WriteFile(path, []byte("{not json"), 0o600)
	if _, err := installJSON(path, "/bin/decathlon"); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("%v", err)
	}
}

func TestInstallTOMLBranches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if action, err := installTOML(path, "/bin/decathlon"); err != nil || action != "added" {
		t.Fatalf("%q %v", action, err)
	}
	if action, err := installTOML(path, "/bin/decathlon"); err != nil || action != "unchanged" {
		t.Fatalf("%q %v", action, err)
	}
	// Rewriting the table updates both keys once and keeps the comments.
	os.WriteFile(path, []byte("# mine\nmodel = \"x\"\n\n[mcp_servers.decathlon]\ncommand = \"/old\"\n"), 0o600)
	if action, err := installTOML(path, "/new"); err != nil || action != "updated" {
		t.Fatalf("%q %v", action, err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(raw), "# mine\n") || !strings.Contains(string(raw), `command = "/new"`) || !strings.Contains(string(raw), `args = ["mcp"]`) || strings.Count(string(raw), "[mcp_servers.decathlon]") != 1 {
		t.Fatalf("%q", raw)
	}
	// A nested table is refused rather than duplicated.
	os.WriteFile(path, []byte("[mcp_servers.decathlon.env]\nA = \"1\"\n"), 0o600)
	if _, err := installTOML(path, "/new"); err == nil || !strings.Contains(err.Error(), "does not rewrite") {
		t.Fatalf("%v", err)
	}
}

func TestInstallCodex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	res, err := Install("codex", "/bin/decathlon")
	if err != nil || res.Action != "added" || res.Command != "/bin/decathlon" {
		t.Fatalf("%+v %v", res, err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "config.toml")); err != nil || !strings.Contains(string(raw), `args = ["mcp"]`) {
		t.Fatalf("%s %v", raw, err)
	}
}
