package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Fixtures are real pages captured through the CLI's own transport on
// 2026-10-19 (see docs/decathlon-api-discovery.md), gzipped, with the
// session's temporary user id replaced by a zero UUID.
var fixtureURLs = map[string]string{
	"listing_keyword":        "https://www.decathlon.es/es/search?Ntt=hornillo+gas",
	"listing_redirect":       "https://www.decathlon.es/es/deportes/camping/tiendas-campana?opeco=opeco:LiP_search_&type=opeco",
	"listing_category_store": "https://www.decathlon.es/es/deportes/camping/tiendas-campana/f-store_pamplona?price=from_10_to_40",
	"product_mh100":          "https://www.decathlon.es/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471",
	"product_nh500":          "https://www.decathlon.es/es/p/botas-de-montana-y-trekking-impermeables-de-cuero-hombre-quechua-nh500/336529/c392m8668913",
	"store_locator":          "https://www.decathlon.es/es/store-locator",
	"home":                   "https://www.decathlon.es/es",
	"product_marketplace":    "https://www.decathlon.es/es/p/mp/tienda-campana-4-personas-205x205x130-cm-aktive/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251",
}

func fixture(t testing.TB, name string) *Page {
	t.Helper()
	f, err := os.Open("testdata/" + name + ".html.gz")
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
	return &Page{URL: fixtureURLs[name], Body: body}
}

// roundTrip re-marshals v, decodes it into a fresh value and requires the
// two to match: what the CLI prints must decode back to what it parsed.
func roundTrip[T any](t *testing.T, v T) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back T
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(back)
	if !bytes.Equal(raw, again) {
		t.Fatalf("round trip drifted:\n%s\n%s", raw, again)
	}
}

func TestListingKeyword(t *testing.T) {
	r, err := ParseListing(fixture(t, "listing_keyword"))
	if err != nil {
		t.Fatal(err)
	}
	if r.mode != modeKeyword || r.Query != "hornillo gas" {
		t.Fatalf("mode %v query %q", r.mode, r.Query)
	}
	if r.Total < 100 || len(r.Products) != 40 || r.PerPage != 40 || r.Pages < 2 {
		t.Fatalf("total %d products %d per %d pages %d", r.Total, len(r.Products), r.PerPage, r.Pages)
	}
	p := r.Products[0]
	if p.ID == "" || p.ModelID == "" || p.Title == "" || p.Brand == "" || p.Price <= 0 || p.Currency != "EUR" || !strings.HasPrefix(p.URL, "https://www.decathlon.es/es/p/") {
		t.Fatalf("first product incomplete: %+v", p)
	}
	if fc, err := r.facet("brand"); err != nil || len(fc.Values) == 0 {
		t.Fatalf("brand facet: %v", err)
	}
	if r.rangeU == nil || r.rangeU.Key != "Nf" {
		t.Fatalf("price range spec %+v", r.rangeU)
	}
	roundTrip(t, *r)
}

func TestListingRedirectIsCategory(t *testing.T) {
	r, err := ParseListing(fixture(t, "listing_redirect"))
	if err != nil {
		t.Fatal(err)
	}
	if r.mode != modeCategory || r.Category == nil || r.Category.Path != "deportes/camping/tiendas-campana" {
		t.Fatalf("mode %v category %+v", r.mode, r.Category)
	}
	if _, err := r.facet("store"); err != nil {
		t.Fatal("category listings offer the store facet:", err)
	}
	roundTrip(t, *r)
}

func TestListingCategoryStore(t *testing.T) {
	r, err := ParseListing(fixture(t, "listing_category_store"))
	if err != nil {
		t.Fatal(err)
	}
	var facets []string
	for _, a := range r.Applied {
		facets = append(facets, a.Facet)
	}
	if !reflect.DeepEqual(facets, []string{"price", "store"}) {
		t.Fatalf("applied %+v", r.Applied)
	}
	for _, p := range r.Products {
		if p.Price < 10 || p.Price > 40 {
			t.Fatalf("price filter not honoured: %+v", p)
		}
	}
}

func TestBuildKeyword(t *testing.T) {
	r, _ := ParseListing(fixture(t, "listing_keyword"))
	got, empty, err := r.Build(SearchParams{Filters: []FilterArg{{"brand", "forclaz"}, {"brand", "Campingaz"}}, MinPrice: 10, MaxPrice: 40, Sort: "price_asc", Page: 1})
	if err != nil || empty != nil {
		t.Fatal(err, empty)
	}
	// The same filters the site itself combines (checked live 2026-10-19).
	want := "https://www.decathlon.es/es/search?Nf=sku.modelLowestPrice%7CBTWN%2B10%2B40&Ntt=hornillo+gas&facets=brandLabel%3AFORCLAZ_brandLabel%3ACAMPINGAZ_&from=40&size=40&sort=asc"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestBuildCategory(t *testing.T) {
	r, _ := ParseListing(fixture(t, "listing_redirect"))
	got, _, err := r.Build(SearchParams{Filters: []FilterArg{{"store", "169"}, {"delivery", "48h"}}, MaxPrice: 40, Sort: "newest"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"/es/deportes/camping/tiendas-campana/f-store_pamplona/f-lead-time_ud-0-48?", "price=from_13.02_to_40", "Ns=newArrivals"} {
		if !strings.Contains(got, part) {
			t.Fatalf("%s lacks %s", got, part)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	kw, _ := ParseListing(fixture(t, "listing_keyword"))
	cat, _ := ParseListing(fixture(t, "listing_redirect"))
	cases := []struct {
		name string
		r    *SearchResult
		p    SearchParams
	}{
		{"store on keyword search", kw, SearchParams{Filters: []FilterArg{{"store", "169"}}}},
		{"unknown facet", kw, SearchParams{Filters: []FilterArg{{"flavour", "x"}}}},
		{"unknown value", kw, SearchParams{Filters: []FilterArg{{"brand", "no-such-brand"}}}},
		{"newest on keyword search", kw, SearchParams{Sort: "newest"}},
		{"bad sort", cat, SearchParams{Sort: "cheapest"}},
		{"inverted range", cat, SearchParams{MinPrice: 50, MaxPrice: 10}},
	}
	for _, c := range cases {
		_, _, err := c.r.Build(c.p)
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindUsage {
			t.Errorf("%s: want usage error, got %v", c.name, err)
		}
	}
	// A store that holds none of these products is not offered as a facet
	// value; the honest answer is an empty listing, not an error.
	_, empty, err := cat.Build(SearchParams{Filters: []FilterArg{{"store", "99999"}}})
	if err != nil || empty == nil || len(empty.Products) != 0 {
		t.Fatalf("want empty listing, got %v %v", empty, err)
	}
}

func TestProductSingleSKU(t *testing.T) {
	d, err := ParseProduct(fixture(t, "product_mh100"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "303295" || d.Brand != "QUECHUA" || !strings.Contains(d.Title, "MH100") || d.Reviews == 0 {
		t.Fatalf("%+v", d)
	}
	if len(d.Models) != 1 || len(d.Models[0].SKUs) != 1 {
		t.Fatalf("models %+v", d.Models)
	}
	m := d.Models[0]
	if m.Price != 24.99 || m.Currency != "EUR" || len(m.Colors) == 0 {
		t.Fatalf("model %+v", m)
	}
	if len(m.SKUs[0].StoresWithStock) != 61 {
		t.Fatalf("stores with stock %d", len(m.SKUs[0].StoresWithStock))
	}
	roundTrip(t, *d)
}

// Regression (bug 1): product pages also embed recommendation and bundle
// item groups, each with its own storesWithStock. Taking the first item
// group found reported a sleeping mat's stock for the tent.
func TestProductPicksItsOwnGroup(t *testing.T) {
	d, err := ParseProduct(fixture(t, "product_nh500"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "336529" || !strings.Contains(d.Title, "NH500") {
		t.Fatalf("parsed another product: %s %s", d.ID, d.Title)
	}
	m := d.Models[0]
	if m.ReferencePrice <= m.Price || m.Price != 40.99 {
		t.Fatalf("discount not read: %+v", m)
	}
	var sizes []string
	for _, s := range m.SKUs {
		sizes = append(sizes, s.Size)
		if s.SkuID == "" || s.EAN == "" {
			t.Fatalf("sku %+v", s)
		}
	}
	if len(sizes) < 2 {
		t.Fatalf("sizes %v", sizes)
	}
}

func TestStores(t *testing.T) {
	stores, err := ParseStores(fixture(t, "store_locator"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) < 150 {
		t.Fatalf("%d stores", len(stores))
	}
	s, err := FindStore(stores, "00169")
	if err != nil || s.Name != "Pamplona" || s.Lat == 0 || len(s.Hours) != 7 {
		t.Fatalf("%+v %v", s, err)
	}
	roundTrip(t, stores)
	// Every id a product page reports must resolve in the directory.
	d, _ := ParseProduct(fixture(t, "product_mh100"))
	rep, err := Stock(d, stores, StockQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Lines) != 61 || len(rep.UnknownStores) != 0 {
		t.Fatalf("lines %d unknown %v", len(rep.Lines), rep.UnknownStores)
	}
	rep, _ = Stock(d, stores, StockQuery{Stores: []string{"169", "86"}, All: true})
	if len(rep.Lines) != 2 {
		t.Fatalf("named stores %+v", rep.Lines)
	}
}

func TestCategories(t *testing.T) {
	cats, err := ParseCategories(fixture(t, "home"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) < 5 || cats[0].Path != "deportes" || len(cats[0].Children) == 0 {
		t.Fatalf("%+v", cats)
	}
	roundTrip(t, cats)
}

func TestProductPath(t *testing.T) {
	for in, want := range map[string]string{
		"303295/8513471":             "/es/p/_/303295/m8513471",
		"303295/m8513471":            "/es/p/_/303295/m8513471",
		"X8880883/8880883":           "/es/p/_/X8880883/m8880883",
		fixtureURLs["product_mh100"]: fixtureURLs["product_mh100"],
	} {
		if got, err := ProductPath(in); err != nil || got != want {
			t.Errorf("%s: %s %v", in, got, err)
		}
	}
	for _, bad := range []string{"303295", "https://www.decathlon.es/es/search?Ntt=x", "../../etc"} {
		if _, err := ProductPath(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// Regression (bug 2): marketplace listings use /p/mp/<slug>/<uuid>/c<n>.
// The product URL pattern read the slug as the group id, so their pages
// were reported as not found, and search printed an unusable uuid/uuid_c251
// ref for them.
func TestMarketplace(t *testing.T) {
	r, _ := ParseListing(fixture(t, "listing_redirect"))
	var mp *Product
	for i := range r.Products {
		if r.Products[i].Marketplace {
			mp = &r.Products[i]
			break
		}
	}
	if mp == nil {
		t.Fatal("fixture has no marketplace product")
	}
	if !strings.HasPrefix(mp.Ref, "mp/") || strings.Contains(mp.Ref, "_") {
		t.Fatalf("ref %q", mp.Ref)
	}
	if _, err := ProductPath(mp.Ref); err != nil {
		t.Fatal(err)
	}
	d, err := ParseProduct(fixture(t, "product_marketplace"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "a5d8d195-6a1b-4505-9621-2a2fd42d3e81" || len(d.Models) != 1 || d.Models[0].Ref != "mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251" || !d.Models[0].Marketplace {
		t.Fatalf("%+v", d.Models)
	}
	if n := len(d.Models[0].SKUs[0].StoresWithStock); n != 0 {
		t.Fatalf("marketplace items are not in stores, got %d", n)
	}
}

func TestFirstPartyRef(t *testing.T) {
	r, _ := ParseListing(fixture(t, "listing_keyword"))
	p := r.Products[0]
	if p.Ref != p.ID+"/"+p.ModelID {
		t.Fatalf("ref %q", p.Ref)
	}
}

// Regression (bug 9): PRODUCT and --category accept URLs, and the client used
// to fetch whatever host they named, so a crafted or copied URL turned it
// into a fetcher for another site. Only the storefront host is read.
func TestFetchRefusesForeignHost(t *testing.T) {
	c := New()
	// Keep the test hermetic even if the guard regresses: there is no session
	// and no solver, so only the host check can answer.
	c.SessionFile = filepath.Join(t.TempDir(), "session.json")
	c.SolverURL = "http://127.0.0.1:1"
	if err := c.allow(DefaultBaseURL + "/es/p/_/303295/m8513471"); err != nil {
		t.Fatalf("storefront url refused: %v", err)
	}
	for _, target := range []string{"https://evil.example/es/p/x/1/m2", "http://169.254.169.254/latest/meta-data/"} {
		if err := c.allow(target); err == nil {
			t.Errorf("%s allowed", target)
		}
		var e *Error
		_, err := c.Fetch(context.Background(), target, 0)
		if !errors.As(err, &e) || e.Kind != KindUsage {
			t.Fatalf("Fetch(%s): want usage error, got %v", target, err)
		}
	}
}
