package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n0nuser/decathlon-cli/internal/api"
)

type prettyThing struct{ text string }

func (p prettyThing) Pretty(w io.Writer, _ bool) { fmt.Fprint(w, "pretty:"+p.text) }

type listThing []string

func (l listThing) JSONLines() []any {
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}

func TestParseFormat(t *testing.T) {
	for _, name := range Formats {
		if got, err := ParseFormat(name); err != nil || string(got) != name {
			t.Errorf("ParseFormat(%q) = %q %v", name, got, err)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Fatal("unknown format accepted")
	}
}

func TestPrintJSONIsIndentedAndUnescaped(t *testing.T) {
	var b bytes.Buffer
	if err := (Printer{W: &b, Format: JSON}).Print(map[string]string{"html": "<b>"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\n  \"html\": \"<b>\"") {
		t.Fatalf("%q", b.String())
	}
}

func TestPrintPretty(t *testing.T) {
	var b bytes.Buffer
	if err := (Printer{W: &b, Format: Pretty}).Print(prettyThing{"x"}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "pretty:x" {
		t.Fatalf("%q", b.String())
	}
	b.Reset()
	if err := (Printer{W: &b, Format: Pretty}).Print(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\"a\": 1") {
		t.Fatalf("fallback: %q", b.String())
	}
}

func TestPrintJSONL(t *testing.T) {
	var b bytes.Buffer
	if err := (Printer{W: &b, Format: JSONL}).Print(listThing{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "\"a\"\n\"b\"\n" {
		t.Fatalf("lister: %q", b.String())
	}
	b.Reset()
	if err := (Printer{W: &b, Format: JSONL}).Print([]int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if b.String() != "1\n2\n" {
		t.Fatalf("elements: %q", b.String())
	}
	b.Reset()
	if err := (Printer{W: &b, Format: JSONL}).Print(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(b.String(), "\n") != 1 || !strings.Contains(b.String(), "\"a\":1") {
		t.Fatalf("single: %q", b.String())
	}
}

func TestPrintToon(t *testing.T) {
	var b bytes.Buffer
	if err := (Printer{W: &b, Format: Toon}).Print(map[string]any{"a": 1, "b": "x"}); err != nil {
		t.Fatal(err)
	}
	if b.Len() == 0 || !strings.Contains(b.String(), "a") {
		t.Fatalf("%q", b.String())
	}
	if err := (Printer{W: &b, Format: Toon}).Print(func() {}); err == nil {
		t.Fatal("unmarshalable value accepted")
	}
}

func TestElements(t *testing.T) {
	if _, ok := elements(map[string]int{"a": 1}); ok {
		t.Fatal("object reported as elements")
	}
	if items, ok := elements([]int{1}); !ok || len(items) != 1 {
		t.Fatalf("%v %v", items, ok)
	}
}

func TestTable(t *testing.T) {
	var plain, colored bytes.Buffer
	Table(&plain, false, []string{"H"}, [][]string{{"a\tb"}})
	if !strings.HasPrefix(plain.String(), "H\n") || strings.Contains(plain.String(), "\x1b") {
		t.Fatalf("%q", plain.String())
	}
	Table(&colored, true, []string{"H"}, [][]string{{"x"}})
	if !strings.Contains(colored.String(), "\x1b[1mH\x1b[0m") {
		t.Fatalf("%q", colored.String())
	}
}

func TestCleanAndCells(t *testing.T) {
	if got := Clean("a\nb\tc\x00d"); got != "a b cd" {
		t.Fatalf("%q", got)
	}
	if got := cleanCell("plain"); got != "plain" {
		t.Fatalf("%q", got)
	}
	if got := cleanCell("\x1b[2mdim\x1b[0m and \x1b[2Jclear"); got != "\x1b[2mdim\x1b[0m and [2Jclear" {
		t.Fatalf("%q", got)
	}
}

func TestTruncate(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcd", 3, "ab…"},
		{"ñandú", 3, "ña…"},
	} {
		if got := Truncate(c.in, c.n); got != c.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestSGR(t *testing.T) {
	if Bold("x", false) != "x" || Dim("x", false) != "x" || Green("x", false) != "x" {
		t.Fatal("colour without colour")
	}
	if Bold("x", true) != "\x1b[1mx\x1b[0m" || Dim("x", true) != "\x1b[2mx\x1b[0m" || Green("x", true) != "\x1b[32mx\x1b[0m" {
		t.Fatal("colour codes")
	}
	if Bold("", true) != "" {
		t.Fatal("empty string got colour")
	}
}

func TestMoneyAndColourEnabled(t *testing.T) {
	if Money(24.99, "EUR") != "24,99 €" || Money(0, "EUR") != "-" || Money(1.5, "USD") != "1,50 USD" {
		t.Fatal("money")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "")
	if ColorEnabled(true, nil) || ColorEnabled(false, nil) {
		t.Fatal("colour without a terminal")
	}
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(false, nil) {
		t.Fatal("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if ColorEnabled(false, nil) {
		t.Fatal("TERM=dumb ignored")
	}
	if IsTerminal(nil) {
		t.Fatal("nil file is a terminal")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Fatal("regular file is a terminal")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		cat  string
	}{
		{"usage error", Usagef("bad"), 2, "usage"},
		{"api usage", &api.Error{Kind: api.KindUsage, Msg: "bad"}, 2, "usage"},
		{"not found", &api.Error{Kind: api.KindNotFound, Msg: "gone"}, 4, "not_found"},
		{"blocked", &api.Error{Kind: api.KindBlocked, Msg: "blocked"}, 5, "blocked"},
		{"network", &api.Error{Kind: api.KindNetwork, Msg: "down"}, 6, "network"},
		{"changed", &api.Error{Kind: api.KindAPIChanged, Msg: "changed", Endpoint: "/es/x", Status: 500, Body: "body"}, 7, "api_changed"},
		{"plain", errors.New("boom"), 1, "generic"},
	}
	for _, c := range cases {
		env := Classify(c.err, "1.2.3", "decathlon search")
		if env.Code != c.code || env.Category != c.cat || env.Message == "" {
			t.Errorf("%s: %+v", c.name, env)
		}
	}
	blocked := Classify(&api.Error{Kind: api.KindBlocked, Msg: "x"}, "1", "decathlon")
	if len(blocked.SuggestedCommands) == 0 {
		t.Error("blocked has no suggestion")
	}
	network := Classify(&api.Error{Kind: api.KindNetwork, Msg: "x", Retryable: true, Status: 503, Endpoint: "/es/x"}, "1", "decathlon")
	if !network.Retryable || network.HTTPStatus != 503 || network.Endpoint != "/es/x" {
		t.Errorf("network: %+v", network)
	}
	if network.SuggestedCommands[0] != "decathlon doctor" {
		t.Errorf("network suggestion: %v", network.SuggestedCommands)
	}
	changed := Classify(&api.Error{Kind: api.KindAPIChanged, Msg: "shape", Endpoint: "/es/x", Status: 500, Body: "body"}, "1.2.3", "decathlon search")
	if changed.IssueURL == "" || len(changed.SuggestedCommands) != 2 {
		t.Fatalf("changed: %+v", changed)
	}
	for _, part := range []string{"template=api-change.yml", "version=1.2.3", "command=decathlon+search", "endpoint=%2Fes%2Fx", "status=500", "report=shape"} {
		if !strings.Contains(changed.IssueURL, part) {
			t.Errorf("issue url lacks %q: %s", part, changed.IssueURL)
		}
	}
}

func TestPrintError(t *testing.T) {
	var text bytes.Buffer
	code := PrintError(&text, &api.Error{Kind: api.KindNetwork, Msg: "down", Retryable: true}, ErrText, "1", "decathlon")
	if code != 6 || !strings.Contains(text.String(), "decathlon: down") || !strings.Contains(text.String(), "try:") {
		t.Fatalf("%d %q", code, text.String())
	}
	text.Reset()
	PrintError(&text, &api.Error{Kind: api.KindAPIChanged, Msg: "changed", Endpoint: "/es/x"}, ErrText, "1", "decathlon")
	if !strings.Contains(text.String(), "prefilled link") {
		t.Fatalf("%q", text.String())
	}
	var js bytes.Buffer
	code = PrintError(&js, Usagef("bad flag"), ErrJSON, "1", "decathlon")
	var env Envelope
	if code != 2 || json.Unmarshal(js.Bytes(), &env) != nil || env.Code != 2 || env.Category != "usage" {
		t.Fatalf("%d %s", code, js.String())
	}
}

func TestEnvelopeFields(t *testing.T) {
	raw, err := json.Marshal(Envelope{Code: 7, Category: "api_changed", Message: "m", IssueURL: "u"})
	if err != nil || !strings.Contains(string(raw), "\"issue_url\":\"u\"") {
		t.Fatalf("%s %v", raw, err)
	}
}

func TestUsagef(t *testing.T) {
	err := Usagef("hello %s", "world")
	var u *UsageError
	if !errors.As(err, &u) || u.Error() != "hello world" {
		t.Fatalf("%v", err)
	}
}
