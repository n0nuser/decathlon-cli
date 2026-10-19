package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKindMapping(t *testing.T) {
	cases := []struct {
		kind Kind
		code int
		name string
	}{
		{KindGeneric, 1, "generic"},
		{KindUsage, 2, "usage"},
		{KindNotFound, 4, "not_found"},
		{KindBlocked, 5, "blocked"},
		{KindNetwork, 6, "network"},
		{KindAPIChanged, 7, "api_changed"},
	}
	for _, c := range cases {
		if c.kind.ExitCode() != c.code || c.kind.String() != c.name {
			t.Errorf("%d: %d %q", c.kind, c.kind.ExitCode(), c.kind.String())
		}
	}
	for name, err := range map[string]error{
		"changed": apiChanged("x %d", 1),
		"usage":   Usage("x %d", 2),
		"missing": notFound("x %d", 3),
	} {
		var e *Error
		if !errors.As(err, &e) || e.Msg == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !strings.Contains(apiChanged("x %d", 1).Error(), "x 1") {
		t.Fatal("formatting")
	}
}

func TestSearchEntryAndModified(t *testing.T) {
	if _, err := (SearchParams{}).entry(); err == nil {
		t.Fatal("empty search accepted")
	}
	if _, err := (SearchParams{Query: "x", Category: "y"}).entry(); err == nil {
		t.Fatal("keywords and category accepted together")
	}
	entry, err := (SearchParams{Category: "/es/deportes/camping/"}).entry()
	if err != nil || entry != "/es/deportes/camping" {
		t.Fatalf("%q %v", entry, err)
	}
	entry, err = (SearchParams{Category: "es/deportes/camping"}).entry()
	if err != nil || entry != "/es/deportes/camping" {
		t.Fatalf("%q %v", entry, err)
	}
	abs := SearchParams{Category: "https://www.decathlon.es/es/deportes/camping"}
	if entry, err := abs.entry(); err != nil || entry != abs.Category {
		t.Fatalf("%q %v", entry, err)
	}
	entry, err = (SearchParams{Query: "hornillo gas"}).entry()
	if err != nil || entry != "/es/search?Ntt=hornillo+gas" {
		t.Fatalf("%q %v", entry, err)
	}
	for _, p := range []SearchParams{
		{}, {Page: 1}, {Query: "x"}, {Category: "x"},
		{Filters: []FilterArg{{Key: "brand", Value: "x"}}}, {MinPrice: 1}, {MaxPrice: 1},
		{Sort: "rating"}, {Page: 2},
	} {
		want := len(p.Filters) > 0 || p.MinPrice > 0 || p.MaxPrice > 0 || p.Sort != "" || p.Page > 1
		if p.modified() != want {
			t.Errorf("%+v: modified %v", p, p.modified())
		}
	}
}

func TestFacetResolutionAndErrors(t *testing.T) {
	kw, err := ParseListing(fixture(t, "listing_keyword"))
	if err != nil {
		t.Fatal(err)
	}
	cat, err := ParseListing(fixture(t, "listing_redirect"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kw.facet("brand"); err != nil {
		t.Fatal(err)
	}
	if _, err := kw.facet("BRAND"); err != nil {
		t.Fatal(err)
	}
	if _, err := kw.facet("nope"); err == nil || !strings.Contains(err.Error(), "has no filter") {
		t.Fatalf("unknown facet: %v", err)
	}
	if _, err := kw.facet("store"); err == nil || !strings.Contains(err.Error(), "--category") {
		t.Fatalf("store on keyword: %v", err)
	}
	if fc, err := cat.facet("store"); err != nil || fc.ID != "store" {
		t.Fatalf("category store facet: %+v %v", fc, err)
	}
	if fc, err := kw.facet("price"); err != nil || fc.Min == 0 {
		t.Fatalf("price facet: %+v %v", fc, err)
	}

	brands, _ := kw.facet("brand")
	if _, err := brands.value("Campingaz", "brand"); err != nil {
		t.Fatal(err)
	}
	if _, err := brands.value("no-such-brand", "brand"); err == nil || !strings.Contains(err.Error(), "has no value") {
		t.Fatalf("unknown value: %v", err)
	}
	stores, _ := cat.facet("store")
	for _, ref := range []string{"169", "00169", "Pamplona"} {
		if _, err := stores.value(ref, "store"); err != nil {
			t.Errorf("store %q: %v", ref, err)
		}
	}
	lead, _ := cat.facet("delivery")
	for _, ref := range []string{"24h", "48h"} {
		if _, err := lead.value(ref, "delivery"); err != nil {
			t.Errorf("delivery %q: %v", ref, err)
		}
	}
}

func TestBuildEdgeCases(t *testing.T) {
	cat, _ := ParseListing(fixture(t, "listing_redirect"))
	if _, _, err := cat.Build(SearchParams{Sort: "rating"}); err != nil {
		t.Fatalf("rating sort: %v", err)
	}
	if _, _, err := cat.Build(SearchParams{MinPrice: -1, MaxPrice: -1}); err != nil {
		// Negative prices are filtered by the CLI, and Build treats <=0 as unset.
		t.Fatalf("negative price treated as set: %v", err)
	}
	// No price facet on the page: Build refuses the range.
	noRange := *cat
	noRange.rangeU = nil
	if _, _, err := noRange.Build(SearchParams{MinPrice: 5}); err == nil {
		t.Fatal("price range accepted without a price facet")
	}
	// A zero-value PerPage falls back to 40.
	page := *cat
	page.PerPage = 0
	got, _, err := page.Build(SearchParams{Page: 2})
	if err != nil || !strings.Contains(got, "from=40") || !strings.Contains(got, "size=40") {
		t.Fatalf("%q %v", got, err)
	}
	if e := cat.emptied(); e.Total != 0 || len(e.Products) != 0 || e.Page != 0 {
		t.Fatalf("%+v", e)
	}
}

func TestFiltersThroughClient(t *testing.T) {
	body := fixture(t, "listing_keyword")
	shop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body.Body)
	}))
	defer shop.Close()
	c := testClient(shop.URL, "http://127.0.0.1:1", sessionFile(t, "c", "UA"), "")
	r, err := c.Filters(context.Background(), SearchParams{Query: "hornillo gas"})
	if err != nil || len(r.Facets) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := c.Filters(context.Background(), SearchParams{}); err == nil {
		t.Fatal("empty filters request accepted")
	}
}

func TestFoldSlugAndFacetPath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"  Tienda CAMPAÑA ", "tienda-campana"},
		{"Nº de plazas", "no-de-plazas"},
		{"áéíóú", "aeiou"},
		{"", ""},
		{"--", ""},
	} {
		if got := slug(c.in); got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if fold("ÑANDÚ") != "nandu" {
		t.Fatal(fold("ÑANDÚ"))
	}
	v := FacetValue{url: "/es/deportes/camping/tiendas-campana?x=1"}
	if got := v.Path(); got != "deportes/camping/tiendas-campana" {
		t.Fatalf("%q", got)
	}
	if s, ok := slugOf("/es/x/f-brand_quechua"); !ok || s != "quechua" {
		t.Fatalf("%q %v", s, ok)
	}
	if _, ok := slugOf("/es/search?facets=x"); ok {
		t.Fatal("keyword URL reported a slug")
	}
	if _, ok := slugOf("/es/x/f-nounderscore"); ok {
		t.Fatal("slug without an underscore accepted")
	}
}

func TestLandingPageAndAbs(t *testing.T) {
	p := fixture(t, "home")
	p.URL = "https://www.decathlon.es/es/deportes/camping"
	_, err := ParseListing(p)
	if !IsLandingPage(err) || !strings.Contains(err.Error(), "landing page") {
		t.Fatalf("%v", err)
	}
	if IsLandingPage(errors.New("x")) {
		t.Fatal("plain error reported as a landing page")
	}
	if got := absURL("://", "/es/x"); got != "/es/x" {
		t.Fatalf("%q", got)
	}
	if got := absURL("/es/p/x/1/m2", "/es/p/y/3/m4"); got != "/es/p/y/3/m4" {
		t.Fatalf("%q", got)
	}
}

func TestCategoriesWithoutMenu(t *testing.T) {
	p := &Page{URL: "https://www.decathlon.es/es/x", Body: pushes(`1:{"a":1}` + "\n")}
	if _, err := ParseCategories(p); err == nil || !strings.Contains(err.Error(), "navigation menu") {
		t.Fatalf("%v", err)
	}
}

func TestParseRefBranches(t *testing.T) {
	cases := []struct {
		in, path, model string
	}{
		{"303295/8513471", "/es/p/_/303295/m8513471", "8513471"},
		{"303295/m8513471", "/es/p/_/303295/m8513471", "8513471"},
		{"mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251", "/es/p/mp/_/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251", ""},
		{"mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81", "/es/p/mp/_/a5d8d195-6a1b-4505-9621-2a2fd42d3e81", ""},
	}
	for _, c := range cases {
		path, model, err := ParseRef(c.in)
		if err != nil || path != c.path || model != c.model {
			t.Errorf("%q: %q %q %v", c.in, path, model, err)
		}
	}
	for _, bad := range []string{"", "303295", "mp/x/c1", "mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/not a variance", "https://example.test/x"} {
		if _, _, err := ParseRef(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestModelRefAndPick(t *testing.T) {
	if got := modelRef("303295", "8513471", "/es/p/x/303295/m8513471"); got != "303295/8513471" {
		t.Fatalf("%q", got)
	}
	if got := modelRef("uuid", "", "/es/p/mp/x/uuid/c251"); got != "mp/uuid/c251" {
		t.Fatalf("%q", got)
	}
	if got := modelRef("303295", "", "/es/p/x/303295/m8513471"); got != "303295" {
		t.Fatalf("%q", got)
	}
	first := priceTarget{TypeTarget: "country"}
	ecom := priceTarget{TypeTarget: "ecommerce"}
	if p := pick([]priceTarget{first, ecom}); p == nil || p.TypeTarget != "ecommerce" {
		t.Fatalf("%+v", p)
	}
	if p := pick([]priceTarget{first}); p == nil || p.TypeTarget != "country" {
		t.Fatalf("%+v", p)
	}
	if pick(nil) != nil {
		t.Fatal("pick(nil)")
	}
}

func TestParseProductErrorPaths(t *testing.T) {
	p := fixture(t, "product_mh100")
	p.URL = "https://www.decathlon.es/es/p/x/999999/m1"
	if _, err := ParseProduct(p); err == nil || !strings.Contains(err.Error(), "item group") {
		t.Fatalf("wrong group: %v", err)
	}
	p = fixture(t, "home")
	p.URL = "https://www.decathlon.es/es/p/x/999999/m1"
	if _, err := ParseProduct(p); err == nil {
		t.Fatal("a page with no product accepted")
	}
}

func TestFindStoreBranches(t *testing.T) {
	stores, err := ParseStores(fixture(t, "store_locator"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"169", "00169", "Pamplona", "Berriozar"} {
		if s, err := FindStore(stores, ref); err != nil || s.ID != "169" {
			t.Errorf("%q: %+v %v", ref, s, err)
		}
	}
	if _, err := FindStore(stores, "99999"); err == nil {
		t.Fatal("unknown id accepted")
	}
	if _, err := FindStore(stores, "no such store"); err == nil {
		t.Fatal("unknown name accepted")
	}
	if _, err := FindStore(stores, "Pamp"); err == nil || !strings.Contains(err.Error(), "several") {
		t.Fatalf("ambiguous name: %v", err)
	}
}

func TestNearAndStockBranches(t *testing.T) {
	stores := []Store{{ID: "2", Name: "B", Lat: 42.82, Lng: -1.65}, {ID: "1", Name: "A", Lat: 42.81, Lng: -1.64}}
	near := Near(stores, 42.81, -1.64)
	if near[0].ID != "1" || near[0].DistanceKm != 0 || near[1].ID != "2" || near[1].DistanceKm <= 0 {
		t.Fatalf("%+v", near)
	}
	d, err := ParseProduct(fixture(t, "product_mh100"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stock(d, near, StockQuery{ModelID: "nope"}); err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("bad model: %v", err)
	}
	if _, err := Stock(d, near, StockQuery{Size: "nope"}); err == nil || !strings.Contains(err.Error(), "no size") {
		t.Fatalf("bad size: %v", err)
	}
	rep, err := Stock(d, near, StockQuery{Stores: []string{"169", "86"}, All: true})
	if err != nil || len(rep.Lines) != 2 {
		t.Fatalf("%+v %v", rep, err)
	}
	// A store id the product reports but the directory does not know is listed.
	real, err := ParseStores(fixture(t, "store_locator"))
	if err != nil {
		t.Fatal(err)
	}
	d.Models[0].SKUs[0].StoresWithStock = append(d.Models[0].SKUs[0].StoresWithStock, "99999")
	rep, err = Stock(d, real, StockQuery{})
	if err != nil || len(rep.UnknownStores) != 1 || rep.UnknownStores[0] != "99999" {
		t.Fatalf("%+v %v", rep, err)
	}
	if !idLess("2", "10") || idLess("10", "2") || !idLess("a", "b") {
		t.Fatal("idLess")
	}
}
