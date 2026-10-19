package sink

import (
	"encoding/json"
	"testing"

	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

func payload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The event vocabulary and payload types are shared with the producer; this
// pins the rendering of the shapes that cross that boundary.
func TestText(t *testing.T) {
	product := payload(t, store.ProductRef{ID: "303295", ModelID: "8513471", Title: "Tienda MH100", Price: 24.99, Currency: "EUR", URL: "https://www.decathlon.es/es/p/x/303295/m8513471"})
	sku := payload(t, store.SKURef{SkuID: "s1", Size: "Talla única"})
	shop := payload(t, store.StoreRef{ID: "169", Name: "Pamplona"})
	change := payload(t, store.Change{From: 24.99, To: 19.99})

	cases := []struct {
		name  string
		event store.Event
		title string
		body  string
	}{
		{"price move", store.Event{Type: store.ProductPriceChanged, Watch: "tent", Product: product, Change: change}, "Price: Tienda MH100", "24.99 -> 19.99 EUR [tent]"},
		{"back in store", store.Event{Type: store.StoreInStock, Watch: "tent", Product: product, SKU: sku, Store: shop}, "In stock at Pamplona (Talla única): Tienda MH100", "24.99 EUR [tent]"},
		{"gone from store", store.Event{Type: store.StoreOutOfStock, Watch: "tent", Product: product, SKU: sku, Store: shop}, "Out of stock at Pamplona (Talla única): Tienda MH100", "[tent]"},
		{"removed", store.Event{Type: store.ProductRemoved, Watch: "tent", Product: product}, "Gone: Tienda MH100", "[tent]"},
	}
	for _, c := range cases {
		title, body, link := Text(c.event)
		if title != c.title || body != c.body {
			t.Errorf("%s:\n title %q\n want  %q\n body  %q\n want  %q", c.name, title, c.title, body, c.body)
		}
		if link != "https://www.decathlon.es/es/p/x/303295/m8513471" {
			t.Errorf("%s: link %q", c.name, link)
		}
	}
}

func TestTarget(t *testing.T) {
	cases := []struct {
		sink config.SinkConfig
		want string
	}{
		{config.SinkConfig{Type: "ntfy", URL: "https://ntfy.sh/", Topic: "alerts"}, "https://ntfy.sh/alerts"},
		{config.SinkConfig{Type: "webhook", URL: "https://example.test/hook"}, "https://example.test/hook"},
		{config.SinkConfig{Type: "exec", Command: []string{"/bin/notify", "--flag"}}, "/bin/notify --flag"},
	}
	for _, c := range cases {
		if got := Target(c.sink); got != c.want {
			t.Errorf("%s: %q, want %q", c.sink.Type, got, c.want)
		}
	}
}
