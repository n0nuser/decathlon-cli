// Package output prints command results and errors. Results go to stdout as
// json (default), jsonl, pretty or toon; errors go to stderr as text or a
// JSON envelope. It also owns the error-to-exit-code mapping.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"text/tabwriter"
	"unicode"

	toon "github.com/toon-format/toon-go"

	"github.com/n0nuser/decathlon-cli/internal/api"
)

type Format string

const (
	JSON   Format = "json"
	JSONL  Format = "jsonl"
	Pretty Format = "pretty"
	Toon   Format = "toon"
)

var Formats = []string{string(JSON), string(JSONL), string(Pretty), string(Toon)}

func ParseFormat(s string) (Format, error) {
	for _, f := range Formats {
		if s == f {
			return Format(s), nil
		}
	}
	return "", Usagef("--format must be one of %s", strings.Join(Formats, ", "))
}

// Prettier is a result with a human rendering. Others print as indented JSON
// under --format pretty.
type Prettier interface {
	Pretty(w io.Writer, color bool)
}

// Lister is a result whose jsonl rendering is one line per element rather
// than the whole document on one line.
type Lister interface {
	JSONLines() []any
}

type Printer struct {
	W      io.Writer
	Format Format
	Color  bool
}

func (p Printer) Print(v any) error {
	switch p.Format {
	case Pretty:
		if pr, ok := v.(Prettier); ok {
			pr.Pretty(p.W, p.Color)
			return nil
		}
		return indent(p.W, v)
	case JSONL:
		enc := json.NewEncoder(p.W)
		if l, ok := v.(Lister); ok {
			for _, it := range l.JSONLines() {
				if err := enc.Encode(it); err != nil {
					return err
				}
			}
			return nil
		}
		if items, ok := elements(v); ok {
			for _, it := range items {
				if err := enc.Encode(it); err != nil {
					return err
				}
			}
			return nil
		}
		return enc.Encode(v)
	case Toon:
		// Go through JSON first so toon field names are the json tags.
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic any
		if err := json.Unmarshal(raw, &generic); err != nil {
			return err
		}
		out, err := toon.Marshal(generic)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(p.W, string(out))
		return err
	}
	return indent(p.W, v)
}

func indent(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func elements(v any) ([]json.RawMessage, bool) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) == 0 || raw[0] != '[' {
		return nil, false
	}
	var items []json.RawMessage
	return items, json.Unmarshal(raw, &items) == nil
}

// Table prints aligned columns; the header is bold when colour is on.
func Table(w io.Writer, color bool, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(header) > 0 {
		fmt.Fprintln(tw, Bold(strings.Join(header, "\t"), color))
	}
	for _, r := range rows {
		for i := range r {
			r[i] = cleanCell(r[i])
		}
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// Clean drops control characters (and so ANSI/OSC escapes) from text that
// came from the storefront before it reaches a terminal.
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}

var sgrRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// cleanCell is Clean that keeps the colour codes Bold/Dim/Green added:
// a cell is cleaned around them, so only plain SGR survives, never cursor
// movement or OSC sequences.
func cleanCell(s string) string {
	locs := sgrRE.FindAllStringIndex(s, -1)
	if locs == nil {
		return Clean(s)
	}
	var b strings.Builder
	last := 0
	for _, l := range locs {
		b.WriteString(Clean(s[last:l[0]]))
		b.WriteString(s[l[0]:l[1]])
		last = l[1]
	}
	b.WriteString(Clean(s[last:]))
	return b.String()
}

// Truncate shortens s to n runes, ending in an ellipsis.
func Truncate(s string, n int) string {
	s = Clean(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func Bold(s string, color bool) string { return sgr("1", s, color) }
func Dim(s string, color bool) string  { return sgr("2", s, color) }
func Green(s string, color bool) string {
	return sgr("32", s, color)
}

func sgr(code, s string, color bool) string {
	if !color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Money formats a price the way the site shows it.
func Money(v float64, currency string) string {
	if v == 0 {
		return "-"
	}
	sym := currency
	if currency == "EUR" || currency == "" {
		sym = "€"
	}
	return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1) + " " + sym
}

// errors

type ErrorFormat string

const (
	ErrText ErrorFormat = "text"
	ErrJSON ErrorFormat = "json"
)

// Envelope is the error printed with --error-format json.
type Envelope struct {
	Code              int      `json:"code"`
	Category          string   `json:"category"`
	Retryable         bool     `json:"retryable"`
	Message           string   `json:"message"`
	HTTPStatus        int      `json:"http_status,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
	IssueURL          string   `json:"issue_url,omitempty"`
}

// UsageError is a caller mistake caught in the CLI layer.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

const Repo = "https://github.com/n0nuser/decathlon-cli"

// Classify maps an error to its envelope. command is the command path with
// no argument values, so search terms never land in an issue link.
func Classify(err error, version, command string) Envelope {
	env := Envelope{Code: 1, Category: "generic", Message: err.Error()}
	var ue *UsageError
	var ae *api.Error
	switch {
	case errors.As(err, &ue):
		env.Code, env.Category = 2, "usage"
	case errors.As(err, &ae):
		env.Code, env.Category = ae.Kind.ExitCode(), ae.Kind.String()
		env.Retryable, env.HTTPStatus, env.Endpoint = ae.Retryable, ae.Status, ae.Endpoint
		switch ae.Kind {
		case api.KindAPIChanged:
			env.IssueURL = IssueURL(version, command, ae)
			env.SuggestedCommands = []string{"decathlon doctor", "rerun with --debug"}
		case api.KindBlocked:
			env.SuggestedCommands = []string{"wait a few minutes and retry", "decathlon doctor"}
		case api.KindNetwork:
			env.SuggestedCommands = []string{"decathlon doctor"}
		}
	}
	return env
}

// IssueURL prefills .github/ISSUE_TEMPLATE/api-change.yml.
func IssueURL(version, command string, e *api.Error) string {
	q := url.Values{}
	q.Set("template", "api-change.yml")
	q.Set("title", "[api change] "+strings.TrimSpace(command+" "+e.Endpoint))
	q.Set("version", version)
	q.Set("os", runtime.GOOS+"/"+runtime.GOARCH)
	q.Set("command", command)
	q.Set("endpoint", e.Endpoint)
	if e.Status != 0 {
		q.Set("status", fmt.Sprint(e.Status))
	}
	q.Set("report", e.Msg+"\n"+e.Body)
	return Repo + "/issues/new?" + q.Encode()
}

// PrintError writes err to w and returns the exit code.
func PrintError(w io.Writer, err error, format ErrorFormat, version, command string) int {
	env := Classify(err, version, command)
	if format == ErrJSON {
		_ = json.NewEncoder(w).Encode(env)
		return env.Code
	}
	fmt.Fprintf(w, "decathlon: %s\n", Clean(env.Message))
	switch {
	case env.IssueURL != "":
		fmt.Fprintf(w, "\nthe storefront may have changed. Report it with this prefilled link:\n  %s\nrerun with --debug to see the requests.\n", env.IssueURL)
	case len(env.SuggestedCommands) > 0:
		fmt.Fprintf(w, "  try: %s\n", strings.Join(env.SuggestedCommands, "  |  "))
	}
	return env.Code
}

// ColorEnabled is false under --no-color, NO_COLOR, TERM=dumb, or off a TTY.
func ColorEnabled(noColor bool, f *os.File) bool {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return IsTerminal(f)
}

func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
