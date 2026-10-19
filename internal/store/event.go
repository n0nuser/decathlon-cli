package store

import (
	"encoding/json"
	"time"
)

// Kind is what a Watch watches.
type Kind string

const (
	KindSearch  Kind = "search"
	KindProduct Kind = "product"
)

// Event types. They are the vocabulary a Check produces and a Sink renders.
const (
	ProductNew          = "product.new"           // a search lists a product it did not before
	ProductPriceChanged = "product.price_changed" // a model's price moved
	ProductRemoved      = "product.removed"       // the product page is gone
	SKUNew              = "sku.new"               // a size appeared on a watched product
	SKUBackOnline       = "sku.back_online"
	SKUSoldOutOnline    = "sku.sold_out_online"
	StoreInStock        = "store.in_stock"     // a watched store now holds a size
	StoreOutOfStock     = "store.out_of_stock" // a watched store no longer does
)

// Types is every event type, in the order the CLI documents them.
var Types = []string{ProductNew, ProductPriceChanged, ProductRemoved, SKUNew, SKUBackOnline, SKUSoldOutOnline, StoreInStock, StoreOutOfStock}

// Event is one change a check found. Product, SKU, Store and Change are JSON
// so each event type carries only what applies to it.
type Event struct {
	ID      int64           `json:"-"`
	Type    string          `json:"type"`
	Watch   string          `json:"watch"`
	At      time.Time       `json:"at"`
	Product json.RawMessage `json:"product,omitempty"`
	SKU     json.RawMessage `json:"sku,omitempty"`
	Store   json.RawMessage `json:"store,omitempty"`
	Change  json.RawMessage `json:"change,omitempty"`
}

// ProductRef is the product an event is about.
type ProductRef struct {
	ID       string  `json:"id"`
	ModelID  string  `json:"model_id,omitempty"`
	Title    string  `json:"title"`
	Price    float64 `json:"price,omitempty"`
	Currency string  `json:"currency,omitempty"`
	URL      string  `json:"url"`
}

// SKURef is the size an event is about.
type SKURef struct {
	SkuID string `json:"sku_id"`
	Size  string `json:"size"`
	Color string `json:"color,omitempty"`
}

// StoreRef is the physical store an event is about.
type StoreRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	City string `json:"city,omitempty"`
}

// Change is a price move.
type Change struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
}
