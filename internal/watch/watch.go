// Package watch runs one check of a watch: fetch the target, compare it with
// the stored state, and return events plus the new state. Delivering events
// and committing state is the CLI's job, in that order.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

// Event types.
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

var Types = []string{ProductNew, ProductPriceChanged, ProductRemoved, SKUNew, SKUBackOnline, SKUSoldOutOnline, StoreInStock, StoreOutOfStock}

type SearchTarget struct {
	Params api.SearchParams `json:"params"`
	Pages  int              `json:"pages"`
}

// ProductTarget watches one product; Model and Size narrow it, Stores picks
// which stores report store.* events (empty: every store).
type ProductTarget struct {
	Ref    string   `json:"ref"`
	Model  string   `json:"model,omitempty"`
	Size   string   `json:"size,omitempty"`
	Stores []string `json:"stores,omitempty"`
}

type Result struct {
	Events  []store.Event
	Upserts []store.Seen
	Removed []string
}

// Payloads carried by events.
type ProductRef struct {
	ID       string  `json:"id"`
	ModelID  string  `json:"model_id,omitempty"`
	Title    string  `json:"title"`
	Price    float64 `json:"price,omitempty"`
	Currency string  `json:"currency,omitempty"`
	URL      string  `json:"url"`
}

type SKURef struct {
	SkuID string `json:"sku_id"`
	Size  string `json:"size"`
	Color string `json:"color,omitempty"`
}

type StoreRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	City string `json:"city,omitempty"`
}

type Change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

type ev struct {
	typ     string
	product ProductRef
	sku     *SKURef
	store   *StoreRef
	change  *Change
}

func (e ev) event(w store.Watch, at time.Time) store.Event {
	out := store.Event{Type: e.typ, Watch: w.Name, At: at}
	out.Product, _ = json.Marshal(e.product)
	if e.sku != nil {
		out.SKU, _ = json.Marshal(e.sku)
	}
	if e.store != nil {
		out.Store, _ = json.Marshal(e.store)
	}
	if e.change != nil {
		out.Change, _ = json.Marshal(e.change)
	}
	return out
}

// Check runs one check. A watch that has never been checked records a
// silent baseline unless emitInitial is set.
func Check(ctx context.Context, c *api.Client, st *store.Store, w store.Watch, emitInitial bool) (Result, error) {
	seen, err := st.SeenFor(ctx, w.ID)
	if err != nil {
		return Result{}, err
	}
	baseline := !w.Baselined && !emitInitial
	var evs []ev
	var res Result
	switch w.Kind {
	case store.KindSearch:
		var t SearchTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a corrupt target: %w", w.Name, err)
		}
		evs, res, err = checkSearch(ctx, c, t, seen, baseline)
	case store.KindProduct:
		var t ProductTarget
		if err := json.Unmarshal(w.Target, &t); err != nil {
			return Result{}, fmt.Errorf("watch %s has a corrupt target: %w", w.Name, err)
		}
		var stores []api.Store
		if stores, err = c.Stores(ctx); err == nil {
			evs, res, err = checkProduct(ctx, c, t, stores, seen, baseline)
		}
	default:
		return Result{}, fmt.Errorf("watch %s has unknown kind %q", w.Name, w.Kind)
	}
	if err != nil {
		return Result{}, err
	}
	now := time.Now().UTC()
	for _, e := range evs {
		res.Events = append(res.Events, e.event(w, now))
	}
	return res, nil
}

type tileState struct {
	Price float64    `json:"price"`
	Ref   ProductRef `json:"ref"`
}

func snap(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// checkSearch reports new products and price changes across the first
// pages. Products dropping off the pages are not reported: rankings churn.
func checkSearch(ctx context.Context, c *api.Client, t SearchTarget, seen map[string]store.Seen, baseline bool) ([]ev, Result, error) {
	var evs []ev
	var res Result
	pages := t.Pages
	if pages < 1 {
		pages = 1
	}
	p := t.Params
	if p.Page < 1 {
		p.Page = 1 // page numbers are 1-based; an older target may not name one
	}
	for i := 0; i < pages; i++ {
		r, err := c.Search(ctx, p)
		if err != nil {
			return nil, Result{}, err
		}
		for _, pr := range r.Products {
			key := pr.ID + "/" + pr.ModelID
			ref := ProductRef{ID: pr.ID, ModelID: pr.ModelID, Title: pr.Title, Price: pr.Price, Currency: pr.Currency, URL: pr.URL}
			prev, known := seen[key]
			if known {
				var old tileState
				if json.Unmarshal(prev.Snapshot, &old) == nil && old.Price != pr.Price && pr.Price > 0 {
					evs = append(evs, ev{typ: ProductPriceChanged, product: ref, change: &Change{From: old.Price, To: pr.Price}})
				}
			} else if !baseline {
				evs = append(evs, ev{typ: ProductNew, product: ref})
			}
			res.Upserts = append(res.Upserts, store.Seen{Key: key, Snapshot: snap(tileState{Price: pr.Price, Ref: ref})})
			seen[key] = res.Upserts[len(res.Upserts)-1] // a product listed twice reports once
		}
		if r.Page >= r.Pages {
			break
		}
		p.Page++
	}
	return evs, res, nil
}

type skuState struct {
	Online bool     `json:"online"`
	Stores []string `json:"stores"`
	Size   string   `json:"size"`
	Color  string   `json:"color"`
	Model  string   `json:"model"`
}

type modelState struct {
	Price float64    `json:"price"`
	Ref   ProductRef `json:"ref"`
}

func checkProduct(ctx context.Context, c *api.Client, t ProductTarget, stores []api.Store, seen map[string]store.Seen, baseline bool) ([]ev, Result, error) {
	var evs []ev
	var res Result
	d, err := c.Product(ctx, t.Ref)
	if err != nil {
		var e *api.Error
		if errors.As(err, &e) && e.Kind == api.KindNotFound {
			// Gone. Report once, from the last state, then forget it.
			for k, s := range seen {
				if strings.HasPrefix(k, "m:") {
					var m modelState
					_ = json.Unmarshal(s.Snapshot, &m)
					evs = append(evs, ev{typ: ProductRemoved, product: m.Ref})
				}
				res.Removed = append(res.Removed, k)
			}
			sort.Slice(evs, func(i, j int) bool { return evs[i].product.ModelID < evs[j].product.ModelID })
			return evs, res, nil
		}
		return nil, Result{}, err
	}
	byID := map[string]api.Store{}
	for _, s := range stores {
		byID[s.ID] = s
	}
	watched := map[string]bool{}
	for _, id := range t.Stores {
		watched[id] = true
	}
	matched := false
	for _, m := range d.Models {
		if t.Model != "" && m.ModelID != t.Model {
			continue
		}
		color := ""
		if len(m.Colors) > 0 {
			color = m.Colors[0]
		}
		ref := ProductRef{ID: d.ID, ModelID: m.ModelID, Title: d.Title, Price: m.Price, Currency: m.Currency, URL: m.URL}
		mkey := "m:" + m.ModelID
		if prev, ok := seen[mkey]; ok {
			var old modelState
			if json.Unmarshal(prev.Snapshot, &old) == nil && old.Price != m.Price && m.Price > 0 {
				evs = append(evs, ev{typ: ProductPriceChanged, product: ref, change: &Change{From: old.Price, To: m.Price}})
			}
		}
		res.Upserts = append(res.Upserts, store.Seen{Key: mkey, Snapshot: snap(modelState{Price: m.Price, Ref: ref})})
		for _, s := range m.SKUs {
			if t.Size != "" && !strings.EqualFold(strings.TrimSpace(s.Size), strings.TrimSpace(t.Size)) {
				continue
			}
			matched = true
			var held []string
			for _, id := range s.StoresWithStock {
				if len(watched) == 0 || watched[id] {
					held = append(held, id)
				}
			}
			sort.Strings(held)
			cur := skuState{Online: s.AvailableOnline, Stores: held, Size: s.Size, Color: color, Model: m.ModelID}
			skuRef := &SKURef{SkuID: s.SkuID, Size: s.Size, Color: color}
			key := "s:" + s.SkuID
			prev, known := seen[key]
			var old skuState
			if known {
				_ = json.Unmarshal(prev.Snapshot, &old)
			}
			switch {
			case known:
				if cur.Online && !old.Online {
					evs = append(evs, ev{typ: SKUBackOnline, product: ref, sku: skuRef})
				}
				if !cur.Online && old.Online {
					evs = append(evs, ev{typ: SKUSoldOutOnline, product: ref, sku: skuRef})
				}
				evs = append(evs, storeDiff(ref, skuRef, old.Stores, cur.Stores, byID)...)
			case !baseline:
				evs = append(evs, ev{typ: SKUNew, product: ref, sku: skuRef})
				evs = append(evs, storeDiff(ref, skuRef, nil, cur.Stores, byID)...)
			}
			res.Upserts = append(res.Upserts, store.Seen{Key: key, Snapshot: snap(cur)})
		}
	}
	// The page lists only sizes that exist right now: a size sold out
	// everywhere disappears from it. A SKU seen before and missing now is
	// reported as gone from every store and from the web shop, and kept as
	// an empty state so its return is reported too.
	current := map[string]bool{}
	for _, u := range res.Upserts {
		current[u.Key] = true
	}
	var vanished []string
	for k := range seen {
		if strings.HasPrefix(k, "s:") && !current[k] {
			vanished = append(vanished, k)
		}
	}
	sort.Strings(vanished)
	for _, k := range vanished {
		var old skuState
		if json.Unmarshal(seen[k].Snapshot, &old) != nil {
			continue
		}
		ref := ProductRef{ID: d.ID, Title: d.Title, URL: d.URL}
		if prev, ok := seen["m:"+old.Model]; ok {
			var m modelState
			if json.Unmarshal(prev.Snapshot, &m) == nil {
				ref = m.Ref
			}
		}
		skuRef := &SKURef{SkuID: strings.TrimPrefix(k, "s:"), Size: old.Size, Color: old.Color}
		if old.Online {
			evs = append(evs, ev{typ: SKUSoldOutOnline, product: ref, sku: skuRef})
		}
		evs = append(evs, storeDiff(ref, skuRef, old.Stores, nil, byID)...)
		if old.Online || len(old.Stores) > 0 {
			gone := old
			gone.Online, gone.Stores = false, []string{}
			res.Upserts = append(res.Upserts, store.Seen{Key: k, Snapshot: snap(gone)})
		}
		matched = true
	}
	if !matched {
		return nil, Result{}, &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("product %s has no model %q / size %q", d.ID, t.Model, t.Size)}
	}
	return evs, res, nil
}

func storeDiff(p ProductRef, s *SKURef, before, after []string, byID map[string]api.Store) []ev {
	was := map[string]bool{}
	for _, id := range before {
		was[id] = true
	}
	is := map[string]bool{}
	for _, id := range after {
		is[id] = true
	}
	ref := func(id string) *StoreRef {
		st := byID[id]
		return &StoreRef{ID: id, Name: st.Name, City: st.City}
	}
	var out []ev
	for _, id := range after {
		if !was[id] {
			out = append(out, ev{typ: StoreInStock, product: p, sku: s, store: ref(id)})
		}
	}
	for _, id := range before {
		if !is[id] {
			out = append(out, ev{typ: StoreOutOfStock, product: p, sku: s, store: ref(id)})
		}
	}
	return out
}

// Jitter spreads watches sharing one timer: up to a tenth of the interval.
func Jitter(interval time.Duration, seed int64) time.Duration {
	max := int64(interval / 10)
	if max <= 0 {
		return 0
	}
	if seed < 0 {
		seed = -seed
	}
	return time.Duration(seed % max)
}
