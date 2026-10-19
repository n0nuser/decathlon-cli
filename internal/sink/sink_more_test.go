package sink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

func event(t *testing.T) store.Event {
	t.Helper()
	return store.Event{
		Type:    store.StoreInStock,
		Watch:   "sink-test",
		Product: payload(t, store.ProductRef{ID: "303295", ModelID: "8513471", Title: "Tienda MH100", Price: 24.99, Currency: "EUR", URL: "https://example.test/p"}),
		SKU:     payload(t, store.SKURef{SkuID: "s", Size: "Talla única"}),
		Store:   payload(t, store.StoreRef{ID: "169", Name: "Pamplona"}),
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		sink config.SinkConfig
		ok   bool
	}{
		{"ntfy", config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh", Topic: "t"}, true},
		{"ntfy no url", config.SinkConfig{Type: "ntfy", Topic: "t"}, false},
		{"ntfy no topic", config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh"}, false},
		{"webhook", config.SinkConfig{Type: "webhook", URL: "https://x"}, true},
		{"webhook no url", config.SinkConfig{Type: "webhook"}, false},
		{"webhook bad template", config.SinkConfig{Type: "webhook", URL: "https://x", Template: "teams"}, false},
		{"exec", config.SinkConfig{Type: "exec", Command: []string{"/bin/true"}}, true},
		{"exec no command", config.SinkConfig{Type: "exec"}, false},
		{"unknown", config.SinkConfig{Type: "email"}, false},
	}
	for _, c := range cases {
		err := Validate(c.name, c.sink)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func TestDeliverNtfy(t *testing.T) {
	var got *http.Request
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte(" secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Deliver(context.Background(), "phone", config.SinkConfig{Type: "ntfy", URL: srv.URL + "/", Topic: "alerts", TokenFile: token}, event(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/alerts" {
		t.Fatalf("%s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer secret" {
		t.Fatalf("auth %q", got.Header.Get("Authorization"))
	}
	if got.Header.Get("Title") == "" || got.Header.Get("Tags") != "in_stock" || got.Header.Get("Click") != "https://example.test/p" {
		t.Fatalf("headers %v", got.Header)
	}
	if !strings.Contains(got.Header.Get("Title"), "Pamplona") || !strings.Contains(body, "24.99") {
		t.Fatalf("title %q body %q", got.Header.Get("Title"), body)
	}
}

func TestDeliverWebhook(t *testing.T) {
	for _, c := range []struct {
		name     string
		template string
		key      string
	}{
		{"raw", "", ""},
		{"discord", "discord", "content"},
		{"slack", "slack", "text"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("content type %q", r.Header.Get("Content-Type"))
				}
				body, _ = io.ReadAll(r.Body)
			}))
			defer srv.Close()
			err := Deliver(context.Background(), "hook", config.SinkConfig{Type: "webhook", URL: srv.URL, Template: c.template}, event(t))
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("%s: %v", body, err)
			}
			if c.key == "" {
				if decoded["type"] != store.StoreInStock || decoded["watch"] != "sink-test" {
					t.Fatalf("%s", body)
				}
			} else if _, ok := decoded[c.key]; !ok {
				t.Fatalf("no %q in %s", c.key, body)
			}
		})
	}
}

func TestDeliverHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	err := Deliver(context.Background(), "hook", config.SinkConfig{Type: "webhook", URL: srv.URL}, event(t))
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("%v", err)
	}
}

func TestDeliverExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script sink test")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "sink.sh")
	bodyOut := filepath.Join(dir, "body")
	envOut := filepath.Join(dir, "env")
	src := "#!/bin/sh\ncat > \"$BODY_OUT\"\nprintf '%s|%s' \"$DECATHLON_EVENT_TYPE\" \"$DECATHLON_WATCH\" > \"$ENV_OUT\"\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BODY_OUT", bodyOut)
	t.Setenv("ENV_OUT", envOut)
	if err := Deliver(context.Background(), "script", config.SinkConfig{Type: "exec", Command: []string{script}}, event(t)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(bodyOut)
	if err != nil || !strings.Contains(string(body), `"watch":"sink-test"`) {
		t.Fatalf("%s %v", body, err)
	}
	env, err := os.ReadFile(envOut)
	if err != nil || string(env) != "store.in_stock|sink-test" {
		t.Fatalf("%s %v", env, err)
	}

	// A failing program is reported with its stderr.
	fail := filepath.Join(dir, "fail.sh")
	os.WriteFile(fail, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755)
	err = Deliver(context.Background(), "script", config.SinkConfig{Type: "exec", Command: []string{fail}}, event(t))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("%v", err)
	}
}

func TestTextAllTypes(t *testing.T) {
	p := payload(t, store.ProductRef{Title: "Thing", Price: 1.5, Currency: "EUR"})
	sku := payload(t, store.SKURef{Size: "42"})
	for _, c := range []struct {
		typ  string
		want string
	}{
		{store.ProductNew, "New: Thing"},
		{store.ProductRemoved, "Gone: Thing"},
		{store.SKUNew, "New size (42): Thing"},
		{store.SKUBackOnline, "Back online (42): Thing"},
		{store.SKUSoldOutOnline, "Sold out online (42): Thing"},
		{store.StoreOutOfStock, "Out of stock at store  (42): Thing"},
		{"other.event", "other.event: Thing"},
	} {
		title, _, _ := Text(store.Event{Type: c.typ, Product: p, SKU: sku})
		if title != c.want {
			t.Errorf("%s: %q, want %q", c.typ, title, c.want)
		}
	}
}

func TestHome(t *testing.T) {
	if got := home("/absolute/path"); got != "/absolute/path" {
		t.Fatalf("%q", got)
	}
	user, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if got := home("~/bin/x"); got != filepath.Join(user, "bin", "x") {
		t.Fatalf("%q", got)
	}
}
