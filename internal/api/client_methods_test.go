package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func shopServing(t *testing.T, routes map[string][]byte, redirects map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		if to, ok := redirects[key]; ok {
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		if body, ok := routes[key]; ok {
			w.Write(body)
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clientFor(t *testing.T, shop, cache string) *Client {
	return testClient(shop, "http://127.0.0.1:1", sessionFile(t, "c", "UA"), cache)
}

func TestSearchThroughClient(t *testing.T) {
	listing := fixture(t, "listing_keyword").Body
	shop := shopServing(t, map[string][]byte{
		"/es/search": listing,
	}, nil)
	c := clientFor(t, shop.URL, "")
	ctx := context.Background()

	r, err := c.Search(ctx, SearchParams{Query: "hornillo gas"})
	if err != nil || r.Total == 0 || len(r.Products) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	// Filters force the resolve-then-build path and a second request.
	r, err = c.Search(ctx, SearchParams{Query: "hornillo gas", MinPrice: 10, MaxPrice: 40, Sort: "price_asc", Page: 2})
	if err != nil || len(r.Products) == 0 {
		t.Fatalf("filtered: %+v %v", r, err)
	}
	// A category URL is a category listing even when the body came from a
	// keyword-shaped page; a store filter then has no facet to resolve.
	if _, err := c.Search(ctx, SearchParams{Category: "deportes/camping", Filters: []FilterArg{{Key: "store", Value: "169"}}}); err == nil {
		t.Fatal("store filter on a page without the facet accepted")
	}
	if _, err := c.Search(ctx, SearchParams{Query: "x", Category: "y"}); err == nil {
		t.Fatal("keywords plus category accepted")
	}
	// A filter value URL that is not a URL is an API change.
	base, _ := ParseListing(fixture(t, "listing_keyword"))
	base.Facets = append(base.Facets, Facet{ID: "brand", Values: []FacetValue{{ID: "x", Label: "X", url: "://"}}})
	if _, _, err := base.Build(SearchParams{Filters: []FilterArg{{Key: "brand", Value: "x"}}}); err == nil {
		t.Fatal("bad facet URL accepted")
	}
}

func TestProductThroughClient(t *testing.T) {
	canonical := "/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471"
	market := "/es/p/mp/tienda-de-campana-4-personas-205x205x130-cm-aktive/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251"
	shop := shopServing(t, map[string][]byte{
		canonical: fixture(t, "product_mh100").Body,
		market:    fixture(t, "product_marketplace").Body,
	}, map[string]string{
		"/es/p/_/303295/m8513471":                              canonical,
		"/es/p/mp/_/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251": market,
	})
	c := clientFor(t, shop.URL, "")
	ctx := context.Background()
	d, err := c.Product(ctx, "303295/8513471")
	if err != nil || d.ID != "303295" || len(d.Models) == 0 {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := c.Product(ctx, "mp/a5d8d195-6a1b-4505-9621-2a2fd42d3e81/c251"); err != nil {
		t.Fatalf("marketplace: %v", err)
	}
	if _, err := c.Product(ctx, "not a ref"); err == nil {
		t.Fatal("bad ref accepted")
	}
	if _, err := c.Product(ctx, "404/1"); err == nil {
		t.Fatal("missing product accepted")
	}
}

func TestStoresAndCategoriesThroughClient(t *testing.T) {
	shop := shopServing(t, map[string][]byte{
		"/es/store-locator": fixture(t, "store_locator").Body,
		"/es/":              fixture(t, "home").Body,
	}, nil)
	c := clientFor(t, shop.URL, "")
	ctx := context.Background()
	stores, err := c.Stores(ctx)
	if err != nil || len(stores) < 150 {
		t.Fatalf("%d %v", len(stores), err)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) == 0 {
		t.Fatalf("%d %v", len(cats), err)
	}
	if _, err := ParseStores(&Page{Body: []byte("<html>nope</html>")}); err == nil {
		t.Fatal("store page without flight accepted")
	}
	if _, err := ParseStores(&Page{Body: fixture(t, "listing_keyword").Body}); err == nil {
		t.Fatal("listing accepted as a store locator")
	}
	if _, err := ParseCategories(&Page{Body: []byte("<html>nope</html>")}); err == nil {
		t.Fatal("menu page without flight accepted")
	}
	// A menu item whose URL is external is skipped, not turned into a path.
	external := &Page{Body: pushes(`1:{"menuItem":{"id":"x","name":"X","url":"https://external.test/x"}}` + "\n")}
	if cats, err := ParseCategories(external); err != nil || len(cats) != 0 {
		t.Fatalf("%+v %v", cats, err)
	}
}

func TestFlightEdgeRows(t *testing.T) {
	if _, err := ParseFlight([]byte(`<script>self.__next_f.push([1,"\x"])</script>`)); err == nil {
		t.Fatal("invalid escape accepted")
	}
	if rows := parseRows([]byte("nocolon")); len(rows.rows) != 0 {
		t.Fatal("row without a colon")
	}
	if rows := parseRows([]byte("1:T7")); len(rows.rows) != 0 {
		t.Fatal("text row without a comma")
	}
	if rows := parseRows([]byte("1:{bad}\n")); len(rows.rows) != 0 {
		t.Fatal("invalid JSON row survived")
	}
	var dst map[string]any
	if err := decodeInto(map[string]any{"bad": make(chan int)}, &dst); err == nil {
		t.Fatal("unmarshalable object accepted")
	}
}

func TestListingErrorPaths(t *testing.T) {
	if _, err := ParseListing(&Page{Body: []byte("<html>nope</html>")}); err == nil {
		t.Fatal("page without flight accepted")
	}
	// No grid and no menu: a storefront change, not a landing page.
	p := &Page{URL: "https://www.decathlon.es/es/x", Body: pushes(`1:{"a":1}` + "\n")}
	if _, err := ParseListing(p); err == nil {
		t.Fatal("page without a grid accepted")
	}
	// A grid on a URL that does not parse is an error.
	p = fixture(t, "listing_keyword")
	p.URL = "http://[::1"
	if _, err := ParseListing(p); err == nil {
		t.Fatal("unparseable page URL accepted")
	}
	if got := absURL("https://x", "://"); got != "://" {
		t.Fatalf("%q", got)
	}
}
