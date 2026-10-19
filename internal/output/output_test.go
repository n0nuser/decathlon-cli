package output

import (
	"bytes"
	"strings"
	"testing"
)

// Regression (bug 5): Table cleaned every cell after styling, stripping the
// ESC of its own colour codes and printing "[2m...[0m" on a colour terminal.
func TestTableKeepsOwnColourDropsInjected(t *testing.T) {
	var b bytes.Buffer
	Table(&b, true, []string{"A"}, [][]string{{Dim("url", true)}, {"evil\x1b]0;title\x07name\x1b[2J"}})
	out := b.String()
	if !strings.Contains(out, "\x1b[2murl\x1b[0m") {
		t.Fatalf("own SGR lost: %q", out)
	}
	if strings.Contains(out, "\x1b]") || strings.Contains(out, "\x07") || strings.Contains(out, "\x1b[2J") {
		t.Fatalf("injected control kept: %q", out)
	}
}

func TestMoney(t *testing.T) {
	if Money(24.99, "EUR") != "24,99 €" || Money(0, "EUR") != "-" {
		t.Fatal(Money(24.99, "EUR"))
	}
}
