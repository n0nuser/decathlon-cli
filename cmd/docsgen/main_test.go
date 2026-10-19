package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainGeneratesReference runs the real entrypoint against a temporary
// output directory and checks the generated reference.
func TestMainGeneratesReference(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.mdx"), []byte("hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale.mdx"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := os.Args
	os.Args = []string{"docsgen", "-out", dir}
	defer func() { os.Args = old }()
	main()

	if _, err := os.Stat(filepath.Join(dir, "stale.mdx")); !os.IsNotExist(err) {
		t.Fatal("a stale generated page survived")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.mdx")); err != nil {
		t.Fatal("the hand-written index was removed:", err)
	}

	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Title string   `json:"title"`
		Pages []string `json:"pages"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"index", "global-flags", "search", "store", "watch"} {
		if !contains(meta.Pages, want) {
			t.Errorf("meta.json lacks %q: %v", want, meta.Pages)
		}
	}

	for name, want := range map[string][]string{
		"search.mdx":       {"## `decathlon search`", "| `--category` |", "decathlon search \"tienda campaña\""},
		"global-flags.mdx": {"| `--format` |", "| `--error-format` |"},
		"watch.mdx":        {"## `decathlon watch`", "### `decathlon watch add`"},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range want {
			if !strings.Contains(string(raw), s) {
				t.Errorf("%s lacks %q", name, s)
			}
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
