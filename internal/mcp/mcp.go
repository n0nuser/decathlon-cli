// Package mcp serves the CLI's read commands over the Model Context
// Protocol's stdio transport: newline-delimited JSON-RPC 2.0. Each tool is a
// CLI argv run in-process, so a tool returns exactly what the command prints.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var versions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

// Result is one in-process CLI run.
type Result struct {
	Stdout    string
	ErrorJSON string // the --error-format json envelope; empty on success
}

type Runner func(ctx context.Context, argv []string) Result

type Server struct {
	Version string
	Run     Runner
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

// Serve answers requests until in closes or ctx ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	lines := make(chan []byte)
	done := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			select {
			case lines <- append([]byte(nil), sc.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
		done <- sc.Err()
		close(lines)
	}()
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return <-done
			}
			if strings.TrimSpace(string(line)) == "" {
				continue
			}
			if resp, reply := s.handle(ctx, line); reply {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) handle(ctx context.Context, line []byte) (rpcResp, bool) {
	var req rpcReq
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}}, true
	}
	if len(req.ID) == 0 {
		return rpcResp{}, false // a notification
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := versions[len(versions)-1]
		if slices.Contains(versions, p.ProtocolVersion) {
			v = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "decathlon", "version": s.Version},
			"instructions":    "Read-only access to decathlon.es: search, product pages, stores and per-store stock, and local watches. Store stock is presence only, never quantities.",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(Tools))
		for _, t := range Tools {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.schema()})
		}
		resp.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcErr{-32602, "invalid params"}
			break
		}
		argv, err := Argv(p.Name, p.Arguments)
		if err != nil {
			resp.Error = &rpcErr{-32602, err.Error()}
			break
		}
		r := s.Run(ctx, argv)
		text := r.Stdout
		if r.ErrorJSON != "" {
			text = strings.TrimSpace(text + "\n" + r.ErrorJSON)
		}
		resp.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": r.ErrorJSON != ""}
	default:
		resp.Error = &rpcErr{-32601, "method not found: " + req.Method}
	}
	return resp, true
}

// Arg is one tool argument and the flag it becomes.
type Arg struct {
	Name, Type, Description string
	Flag                    string // empty: positional
	Required                bool
}

type Tool struct {
	Name, Description string
	Command           []string
	Args              []Arg
}

func (t Tool) schema() map[string]any {
	props := map[string]any{}
	var req []string
	for _, a := range t.Args {
		p := map[string]any{"type": a.Type, "description": a.Description}
		switch a.Type {
		case "array":
			p["items"] = map[string]any{"type": "string"}
		case "object":
			p["additionalProperties"] = map[string]any{"type": "string"}
		}
		props[a.Name] = p
		if a.Required {
			req = append(req, a.Name)
		}
	}
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(req) > 0 {
		s["required"] = req
	}
	return s
}

// Tools are the read-only commands an agent may run. Nothing that changes
// local state beyond watch bookkeeping is exposed.
var Tools = []Tool{
	{Name: "search", Description: "Search decathlon.es by keywords, or list a category, with the website's filters. Returns products with id/model_id for product_show and store_stock.", Command: []string{"search"}, Args: []Arg{
		{Name: "keywords", Type: "string", Description: "search terms"},
		{Name: "category", Type: "string", Flag: "category", Description: "category path from category_list (instead of keywords)"},
		{Name: "brand", Type: "array", Flag: "brand", Description: "brands"},
		{Name: "store", Type: "string", Flag: "store", Description: "only products in stock at this store id or name (categories only)"},
		{Name: "delivery", Type: "string", Flag: "delivery", Description: "24h or 48h (categories only)"},
		{Name: "min_price", Type: "number", Flag: "min-price", Description: "euros"},
		{Name: "max_price", Type: "number", Flag: "max-price", Description: "euros"},
		{Name: "sort", Type: "string", Flag: "sort", Description: "relevance, price_asc, price_desc, discount, rating, newest"},
		{Name: "filter", Type: "object", Flag: "filter", Description: "other filters as {key: value}, keys from the page's facets"},
		{Name: "page", Type: "integer", Flag: "page", Description: "0-based page"},
		{Name: "limit", Type: "integer", Flag: "limit", Description: "max products"},
	}},
	{Name: "product_show", Description: "Read a product page: every colour and size, prices, online availability and the ids of stores holding each size.", Command: []string{"product", "show"}, Args: []Arg{
		{Name: "product", Type: "string", Required: true, Description: "product URL or GROUP/MODEL ids"},
	}},
	{Name: "store_list", Description: "List physical stores, optionally nearest first.", Command: []string{"store", "list"}, Args: []Arg{
		{Name: "near", Type: "string", Flag: "near", Description: "LAT,LNG"},
		{Name: "city", Type: "string", Flag: "city", Description: "city, province or name contains"},
		{Name: "limit", Type: "integer", Flag: "limit", Description: "max stores"},
	}},
	{Name: "store_stock", Description: "Which stores hold each size of a product (presence only, no quantities).", Command: []string{"store", "stock"}, Args: []Arg{
		{Name: "product", Type: "string", Required: true, Description: "product URL or GROUP/MODEL ids"},
		{Name: "store", Type: "array", Flag: "store", Description: "only these store ids or names"},
		{Name: "size", Type: "string", Flag: "size", Description: "only this size"},
		{Name: "model", Type: "string", Flag: "model", Description: "only this model id"},
		{Name: "near", Type: "string", Flag: "near", Description: "sort by distance from LAT,LNG"},
		{Name: "all", Type: "boolean", Flag: "all", Description: "with store, include sizes they lack"},
		{Name: "limit", Type: "integer", Flag: "limit", Description: "max lines"},
	}},
	{Name: "category_list", Description: "The category tree with the paths search takes.", Command: []string{"category", "list"}, Args: []Arg{
		{Name: "depth", Type: "integer", Flag: "depth", Description: "levels (default 2)"},
	}},
	{Name: "watch_list", Description: "List local watches.", Command: []string{"watch", "list"}},
	{Name: "watch_check", Description: "Check due watches (or the named ones) and return their price and stock change events.", Command: []string{"watch", "check"}, Args: []Arg{
		{Name: "names", Type: "array", Description: "watch names"},
		{Name: "all", Type: "boolean", Flag: "all", Description: "check every watch, due or not"},
	}},
}

// Argv turns a tool call into CLI arguments, refusing unknown or mistyped
// arguments rather than dropping them.
func Argv(name string, args map[string]any) ([]string, error) {
	var tool *Tool
	for i := range Tools {
		if Tools[i].Name == name {
			tool = &Tools[i]
		}
	}
	if tool == nil {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	known := map[string]Arg{}
	for _, a := range tool.Args {
		known[a.Name] = a
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		if _, ok := known[k]; !ok {
			return nil, fmt.Errorf("%s: unknown argument %q", name, k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	argv := append([]string{}, tool.Command...)
	var pos []string
	for _, a := range tool.Args {
		if _, ok := args[a.Name]; !ok && a.Required {
			return nil, fmt.Errorf("%s: %s is required", name, a.Name)
		}
	}
	for _, k := range keys {
		a, v := known[k], args[k]
		var vals []string
		switch a.Type {
		case "string":
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%s: %s must be a string", name, k)
			}
			vals = []string{s}
		case "number", "integer":
			f, ok := v.(float64)
			if !ok || (a.Type == "integer" && f != float64(int64(f))) {
				return nil, fmt.Errorf("%s: %s must be a%s", name, k, map[bool]string{true: "n integer", false: " number"}[a.Type == "integer"])
			}
			vals = []string{strconv.FormatFloat(f, 'f', -1, 64)}
		case "boolean":
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("%s: %s must be a boolean", name, k)
			}
			if b {
				argv = append(argv, "--"+a.Flag)
			}
			continue
		case "array":
			list, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("%s: %s must be an array of strings", name, k)
			}
			for _, it := range list {
				s, ok := it.(string)
				if !ok {
					return nil, fmt.Errorf("%s: %s must be an array of strings", name, k)
				}
				vals = append(vals, s)
			}
		case "object":
			m, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: %s must be an object", name, k)
			}
			mk := make([]string, 0, len(m))
			for kk := range m {
				mk = append(mk, kk)
			}
			sort.Strings(mk)
			for _, kk := range mk {
				s, ok := m[kk].(string)
				if !ok {
					return nil, fmt.Errorf("%s: %s values must be strings", name, k)
				}
				vals = append(vals, kk+"="+s)
			}
		}
		if a.Flag == "" {
			pos = append(pos, vals...)
			continue
		}
		for _, s := range vals {
			argv = append(argv, "--"+a.Flag+"="+s)
		}
	}
	if len(pos) > 0 {
		argv = append(argv, "--")
		argv = append(argv, pos...)
	}
	return argv, nil
}
