// Package sink sends watch events somewhere besides stdout: an ntfy topic,
// a JSON webhook (raw, Discord or Slack), or a local program.
package sink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

// Validate checks a sink's config without sending anything.
func Validate(name string, s config.SinkConfig) error {
	switch s.Type {
	case "ntfy":
		if s.URL == "" || s.Topic == "" {
			return fmt.Errorf("sink %s: ntfy needs url and topic", name)
		}
	case "webhook":
		if s.URL == "" {
			return fmt.Errorf("sink %s: webhook needs url", name)
		}
		if s.Template != "" && s.Template != "discord" && s.Template != "slack" {
			return fmt.Errorf("sink %s: template must be discord or slack (or unset for the raw event)", name)
		}
	case "exec":
		if len(s.Command) == 0 {
			return fmt.Errorf("sink %s: exec needs command", name)
		}
	default:
		return fmt.Errorf("sink %s: type must be ntfy, webhook or exec, got %q", name, s.Type)
	}
	return nil
}

// Target is where a sink delivers, for display: the ntfy topic URL, the exec
// command, or the webhook URL.
func Target(s config.SinkConfig) string {
	switch s.Type {
	case "ntfy":
		return strings.TrimRight(s.URL, "/") + "/" + s.Topic
	case "exec":
		return strings.Join(s.Command, " ")
	}
	return s.URL
}

// Deliver sends one event. Failures are returned for the caller to report;
// they never stop other sinks or the state commit.
func Deliver(ctx context.Context, name string, s config.SinkConfig, e store.Event) error {
	if err := Validate(name, s); err != nil {
		return err
	}
	switch s.Type {
	case "ntfy":
		return ntfy(ctx, s, e)
	case "webhook":
		return webhook(ctx, s, e)
	}
	return run(ctx, s, e)
}

var client = &http.Client{Timeout: 15 * time.Second}

// Text is the one-line human summary used by ntfy and chat webhooks.
func Text(e store.Event) (title, body, link string) {
	var p store.ProductRef
	var sku store.SKURef
	var st store.StoreRef
	var ch store.Change
	_ = json.Unmarshal(e.Product, &p)
	_ = json.Unmarshal(e.SKU, &sku)
	_ = json.Unmarshal(e.Store, &st)
	_ = json.Unmarshal(e.Change, &ch)
	where := st.Name
	if where == "" {
		where = "store " + st.ID
	}
	size := ""
	if sku.Size != "" {
		size = " (" + sku.Size + ")"
	}
	switch e.Type {
	case store.ProductNew:
		title, body = "New: "+p.Title, fmt.Sprintf("%.2f %s", p.Price, p.Currency)
	case store.ProductPriceChanged:
		title, body = "Price: "+p.Title, fmt.Sprintf("%v -> %v %s", ch.From, ch.To, p.Currency)
	case store.ProductRemoved:
		title = "Gone: " + p.Title
	case store.SKUNew:
		title = "New size" + size + ": " + p.Title
	case store.SKUBackOnline:
		title = "Back online" + size + ": " + p.Title
	case store.SKUSoldOutOnline:
		title = "Sold out online" + size + ": " + p.Title
	case store.StoreInStock:
		title, body = "In stock at "+where+size+": "+p.Title, fmt.Sprintf("%.2f %s", p.Price, p.Currency)
	case store.StoreOutOfStock:
		title = "Out of stock at " + where + size + ": " + p.Title
	default:
		title = e.Type + ": " + p.Title
	}
	return title, strings.TrimSpace(body + " [" + e.Watch + "]"), p.URL
}

func ntfy(ctx context.Context, s config.SinkConfig, e store.Event) error {
	title, body, link := Text(e)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.URL, "/")+"/"+s.Topic, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	if _, kind, ok := strings.Cut(e.Type, "."); ok {
		// ntfy tags take letters, digits, - and _; the event kind is one.
		req.Header.Set("Tags", kind)
	}
	if link != "" {
		req.Header.Set("Click", link)
	}
	if s.TokenFile != "" {
		tok, err := os.ReadFile(home(s.TokenFile))
		if err != nil {
			return fmt.Errorf("ntfy token file: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	}
	return send(req)
}

func webhook(ctx context.Context, s config.SinkConfig, e store.Event) error {
	var payload any = e
	if s.Template != "" {
		title, body, link := Text(e)
		key := "content"
		if s.Template == "slack" {
			key = "text"
		}
		payload = map[string]string{key: strings.TrimSpace(title + "\n" + body + "\n" + link)}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return send(req)
}

func send(req *http.Request) error {
	req.Header.Set("User-Agent", "decathlon-cli")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered HTTP %d", req.URL.Host, resp.StatusCode)
	}
	return nil
}

// run starts the program with the event JSON on stdin and the type and
// watch name in its environment.
func run(ctx context.Context, s config.SinkConfig, e store.Event) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := make([]string, len(s.Command))
	for i, a := range s.Command {
		argv[i] = home(a)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(append(raw, '\n'))
	cmd.Env = append(os.Environ(), "DECATHLON_EVENT_TYPE="+e.Type, "DECATHLON_WATCH="+e.Watch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v %s", argv[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func home(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return p
}
