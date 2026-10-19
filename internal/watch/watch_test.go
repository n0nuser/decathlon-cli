package watch

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

const (
	listingKey  = "/es/search?Ntt=hornillo+gas"
	productKey  = "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471"
	productPath = "/es/p/_/303295/m8513471"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "api", "testdata", name+".html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// shop is the captured storefront: the two listing shapes, the product and the
// store locator, with the product's short URL redirecting to its canonical one.
type shop struct {
	srv      *httptest.Server
	pages    map[string][]byte
	redirect map[string]string
	hits     map[string]int
}

func newShop(t *testing.T) *shop {
	t.Helper()
	s := &shop{
		pages: map[string][]byte{
			listingKey:          fixture(t, "listing_keyword"),
			"/es/store-locator": fixture(t, "store_locator"),
			productKey:          fixture(t, "product_mh100"),
		},
		redirect: map[string]string{productPath: productKey},
		hits:     map[string]int{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		s.hits[key]++
		if to, ok := s.redirect[key]; ok {
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		body, ok := s.pages[key]
		if !ok {
			body, ok = s.pages[r.URL.Path]
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *shop) client(t *testing.T) *api.Client {
	t.Helper()
	sessionFile := filepath.Join(t.TempDir(), "session.json")
	raw, _ := json.Marshal(api.Session{UserAgent: "Test/1.0", Cookies: []api.StoredCookie{{Name: "x", Value: "y"}}})
	if err := os.WriteFile(sessionFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c := api.New()
	c.BaseURL = s.srv.URL
	c.SessionFile = sessionFile
	c.Gap = 0
	c.CacheDir = ""
	return c
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func addWatch(t *testing.T, st *store.Store, name string, kind store.Kind, target any) store.Watch {
	t.Helper()
	raw, _ := json.Marshal(target)
	w := store.Watch{Name: name, Kind: kind, Target: raw, Interval: store.Duration(time.Hour)}
	if err := st.AddWatch(context.Background(), &w); err != nil {
		t.Fatal(err)
	}
	return w
}

func run(t *testing.T, c *api.Client, st *store.Store, w store.Watch, emit bool) Result {
	t.Helper()
	res, err := Check(context.Background(), c, st, w, emit)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func commit(t *testing.T, st *store.Store, w store.Watch, res Result) {
	t.Helper()
	if err := st.CommitCheck(context.Background(), w, res.Upserts, res.Removed, res.Events); err != nil {
		t.Fatal(err)
	}
}

func reload(t *testing.T, st *store.Store, w store.Watch) store.Watch {
	t.Helper()
	got, err := st.Watch(context.Background(), w.Name)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func types(evs []store.Event) map[string]int {
	out := map[string]int{}
	for _, e := range evs {
		out[e.Type]++
	}
	return out
}

func TestTargetKinds(t *testing.T) {
	if (SearchTarget{}).Kind() != store.KindSearch || (ProductTarget{}).Kind() != store.KindProduct {
		t.Fatal("target kinds")
	}
}

func TestJitter(t *testing.T) {
	if Jitter(0, 1) != 0 || Jitter(-time.Second, 1) != 0 {
		t.Fatal("no jitter without a positive interval")
	}
	for _, seed := range []int64{0, 1, 12345, -7} {
		d := Jitter(10*time.Second, seed)
		if d < 0 || d >= time.Second {
			t.Fatalf("Jitter(10s, %d) = %s", seed, d)
		}
	}
}

func TestStoreDiff(t *testing.T) {
	ref := store.ProductRef{ID: "1"}
	sku := &store.SKURef{SkuID: "s", Size: "42"}
	byID := map[string]api.Store{"1": {ID: "1", Name: "Pamplona"}, "2": {ID: "2", Name: "Logroño"}}
	evs := storeDiff(ref, sku, []string{"1", "2"}, []string{"2", "3"}, byID)
	got := map[string]int{}
	for _, e := range evs {
		got[e.typ]++
		switch e.typ {
		case store.StoreInStock:
			if e.store.ID != "3" || e.store.Name != "" {
				t.Fatalf("in stock %+v", e.store)
			}
		case store.StoreOutOfStock:
			if e.store.ID != "1" || e.store.Name != "Pamplona" {
				t.Fatalf("out of stock %+v", e.store)
			}
		}
	}
	if got[store.StoreInStock] != 1 || got[store.StoreOutOfStock] != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestCheckSearchBaselinePriceChangeAndPages(t *testing.T) {
	s := newShop(t)
	c, st := s.client(t), openStore(t)
	w := addWatch(t, st, "stoves", store.KindSearch, SearchTarget{Params: api.SearchParams{Query: "hornillo gas"}, Pages: 1})

	base := run(t, c, st, w, false)
	if len(base.Events) != 0 || len(base.Upserts) == 0 {
		t.Fatalf("baseline %+v", base)
	}
	commit(t, st, w, base)
	w = reload(t, st, w)

	// The first tile's price moves.
	key := listingKey
	s.pages[key] = bytes.Replace(s.pages[key], []byte(`valueWithTaxes\":24.99`), []byte(`valueWithTaxes\":19.99`), 1)
	res := run(t, c, st, w, false)
	got := types(res.Events)
	if got[store.ProductPriceChanged] != 1 || len(res.Events) != 1 {
		t.Fatalf("price change %+v", res.Events)
	}
	commit(t, st, w, res)
	w = reload(t, st, w)
	if again := run(t, c, st, w, false); len(again.Events) != 0 {
		t.Fatalf("repeat %+v", again.Events)
	}

	// Pages > 1 walks forward; the fixture is the same page, which the seen
	// map folds into one report.
	s.pages["/es/search?Ntt=hornillo+gas&from=40&size=40"] = s.pages[listingKey]
	paged := addWatch(t, st, "pages", store.KindSearch, SearchTarget{Params: api.SearchParams{Query: "hornillo gas"}, Pages: 2})
	if res := run(t, c, st, paged, false); len(res.Events) != 0 {
		t.Fatalf("baseline pages %+v", res.Events)
	}
	if s.hits["/es/search?Ntt=hornillo+gas&from=40&size=40"] == 0 {
		t.Fatal("the second page was never fetched")
	}

	// --emit-initial reports what is already there.
	initial := addWatch(t, st, "initial", store.KindSearch, SearchTarget{Params: api.SearchParams{Query: "hornillo gas"}, Pages: 1})
	if res := run(t, c, st, initial, true); types(res.Events)[store.ProductNew] == 0 {
		t.Fatalf("emit initial: %+v", res.Events)
	}
}

func TestCheckProductStoreAndOnlineDiff(t *testing.T) {
	s := newShop(t)
	c, st := s.client(t), openStore(t)
	w := addWatch(t, st, "tent", store.KindProduct, ProductTarget{Ref: "303295/8513471", Stores: []string{"169"}})

	base := run(t, c, st, w, false)
	if len(base.Events) != 0 || len(base.Upserts) == 0 {
		t.Fatalf("baseline %+v", base)
	}
	commit(t, st, w, base)
	w = reload(t, st, w)

	// Pamplona sells out and the size leaves the web shop.
	s.pages[productKey] = bytes.Replace(s.pages[productKey], []byte(`\"622\",\"169\",\"347\"`), []byte(`\"622\",\"347\"`), 1)
	s.pages[productKey] = bytes.Replace(s.pages[productKey], []byte(`\"isAvailable\":true`), []byte(`\"isAvailable\":false`), 1)
	res := run(t, c, st, w, false)
	got := types(res.Events)
	if got[store.SKUSoldOutOnline] != 1 || got[store.StoreOutOfStock] != 1 {
		t.Fatalf("events %+v", got)
	}
	commit(t, st, w, res)
	w = reload(t, st, w)
	if again := run(t, c, st, w, false); len(again.Events) != 0 {
		t.Fatalf("repeat %+v", again.Events)
	}
}

// A size sold out everywhere disappears from the page; the check reports the
// vanished SKU and keeps an empty state so its return is noticed (bug 6).
func TestCheckProductVanishedSize(t *testing.T) {
	s := newShop(t)
	c, st := s.client(t), openStore(t)
	w := addWatch(t, st, "tent", store.KindProduct, ProductTarget{Ref: "303295/8513471", Stores: []string{"169"}})
	base := run(t, c, st, w, false)
	commit(t, st, w, base)
	w = reload(t, st, w)

	s.pages[productKey] = bytes.ReplaceAll(s.pages[productKey],
		[]byte("4ea60a8f-fc26-430a-85dc-d522a07a104a"), []byte("11111111-2222-4333-8444-555555555555"))
	res := run(t, c, st, w, false)
	got := types(res.Events)
	for _, want := range []string{store.SKUNew, store.StoreInStock, store.SKUSoldOutOnline, store.StoreOutOfStock} {
		if got[want] == 0 {
			t.Fatalf("missing %s in %+v", want, got)
		}
	}
	commit(t, st, w, res)
	w = reload(t, st, w)
	if again := run(t, c, st, w, false); len(again.Events) != 0 {
		t.Fatalf("repeat %+v", again.Events)
	}
}

func TestCheckProductRemoved(t *testing.T) {
	s := newShop(t)
	c, st := s.client(t), openStore(t)
	w := addWatch(t, st, "tent", store.KindProduct, ProductTarget{Ref: "303295/8513471"})
	base := run(t, c, st, w, false)
	commit(t, st, w, base)
	w = reload(t, st, w)

	delete(s.redirect, productPath)
	res := run(t, c, st, w, false)
	got := types(res.Events)
	if got[store.ProductRemoved] == 0 || len(res.Removed) == 0 {
		t.Fatalf("removed %+v %+v", res, got)
	}
}

func TestCheckProductNoMatch(t *testing.T) {
	s := newShop(t)
	c, st := s.client(t), openStore(t)
	w := addWatch(t, st, "tent", store.KindProduct, ProductTarget{Ref: "303295/8513471", Size: "no such size"})
	_, err := Check(context.Background(), c, st, w, false)
	var e *api.Error
	if !errors.As(err, &e) || e.Kind != api.KindNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestCheckCorruptOrUnknownWatch(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)

	bad := store.Watch{Name: "bad", Kind: store.KindSearch, Target: json.RawMessage("{"), Interval: store.Duration(time.Hour)}
	if err := st.AddWatch(ctx, &bad); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, nil, st, bad, false); err == nil || !strings.Contains(err.Error(), "corrupt target") {
		t.Fatalf("corrupt target: %v", err)
	}

	unknown := store.Watch{Name: "unknown", Kind: store.Kind("other"), Target: json.RawMessage(`{}`), Interval: store.Duration(time.Hour)}
	if err := st.AddWatch(ctx, &unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(ctx, nil, st, unknown, false); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("unknown kind: %v", err)
	}
}
