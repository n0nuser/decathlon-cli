package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SearchParams is everything `search` can ask for. Filters are matched
// against the facets the unfiltered page offers, so any filter the website
// shows can be used without the CLI knowing it in advance.
type SearchParams struct {
	Query    string      `json:"query,omitempty"`
	Category string      `json:"category,omitempty"` // category path, or a full listing URL
	Filters  []FilterArg `json:"filters,omitempty"`
	MinPrice float64     `json:"min_price,omitempty"`
	MaxPrice float64     `json:"max_price,omitempty"`
	Sort     string      `json:"sort,omitempty"`
	// Page is a 1-based page number, the way the site and the header count
	// them. The zero value means the first page, so a zero-value SearchParams
	// costs one request.
	Page int `json:"page,omitempty"`
}

type FilterArg struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// sortTable maps CLI sort names to the site's sort ids, in the order the CLI
// documents them.
var sortTable = []struct{ name, id string }{
	{"relevance", "rankingRule"},
	{"price_asc", "priceAscending"},
	{"price_desc", "priceDescending"},
	{"discount", "discountRateDescending"},
	{"rating", "reviewDescending"},
	{"newest", "newArrivals"},
}

// SortID maps a CLI sort name to the site's sort id.
func SortID(name string) (string, bool) {
	for _, s := range sortTable {
		if s.name == name {
			return s.id, true
		}
	}
	return "", false
}

// SortNames lists the CLI sort names in documented order.
func SortNames() []string {
	out := make([]string, len(sortTable))
	for i, s := range sortTable {
		out[i] = s.name
	}
	return out
}

// facetAliases are CLI-friendly names for the facet ids pages use.
var facetAliases = map[string][]string{
	"brand":       {"brand"},
	"store":       {"store"},
	"delivery":    {"leadTime"},
	"seller":      {"partner"},
	"condition":   {"offerCondition", "structurationValue.748"},
	"size":        {"size", "sizeGroupItem"},
	"subcategory": {"categories"},
}

// deliveryTable maps --delivery names to the leadTime facet's value ids.
var deliveryTable = []struct{ name, id string }{
	{"24h", "ud-0-24"},
	{"48h", "ud-0-48"},
}

// DeliveryID maps a --delivery name to the facet value id.
func DeliveryID(name string) (string, bool) {
	for _, d := range deliveryTable {
		if d.name == name {
			return d.id, true
		}
	}
	return "", false
}

// DeliveryNames lists the --delivery names in documented order.
func DeliveryNames() []string {
	out := make([]string, len(deliveryTable))
	for i, d := range deliveryTable {
		out[i] = d.name
	}
	return out
}

// resolveTTL is how long the unfiltered page used to resolve filter values is
// cached.
const resolveTTL = 10 * time.Minute

// IsLandingPage reports whether err is ParseListing's "no product list here".
func IsLandingPage(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Landing
}

func (p SearchParams) modified() bool {
	return len(p.Filters) > 0 || p.MinPrice > 0 || p.MaxPrice > 0 || p.Sort != "" || p.Page > 1
}

func (p SearchParams) entry() (string, error) {
	switch {
	case p.Category != "" && p.Query != "":
		return "", Usage("pass keywords or --category, not both: the site has no keyword search inside a category")
	case p.Category != "":
		c := p.Category
		if u, err := url.Parse(c); err == nil && u.IsAbs() {
			return u.String(), nil
		}
		c = strings.Trim(c, "/")
		c = strings.TrimPrefix(c, "es/")
		return "/es/" + c, nil
	case p.Query != "":
		return "/es/search?" + url.Values{"Ntt": {p.Query}}.Encode(), nil
	}
	return "", Usage("give keywords or --category")
}

// Search runs a search. An unmodified search is one request; filters, a
// price range, a sort or a page cost one more, because the values are
// resolved against the unfiltered page first (cached for ten minutes).
func (c *Client) Search(ctx context.Context, p SearchParams) (*SearchResult, error) {
	entry, err := p.entry()
	if err != nil {
		return nil, err
	}
	modified := p.modified()
	ttl := time.Duration(0)
	if modified {
		ttl = resolveTTL
	}
	page, err := c.Fetch(ctx, entry, ttl)
	if err != nil {
		return nil, err
	}
	base, err := ParseListing(page)
	if err != nil {
		return nil, err
	}
	if !modified {
		// Keep it for the filter, sort or page request that may follow.
		c.store(c.Abs(entry), page)
		return base, nil
	}
	target, empty, err := base.Build(p)
	if err != nil {
		return nil, err
	}
	if empty != nil {
		return empty, nil
	}
	page, err = c.Fetch(ctx, target, 0)
	if err != nil {
		return nil, err
	}
	return ParseListing(page)
}

// Build turns params into the listing URL the site itself would produce,
// starting from this (unfiltered) page. A store filter on a store that holds
// none of these products yields an empty result instead of a URL: the site
// does not offer such a filter value at all.
func (r *SearchResult) Build(p SearchParams) (string, *SearchResult, error) {
	u := *r.base
	q := u.Query()
	q.Del("from")
	q.Del("size")
	segs, empty, err := r.applyFilters(q, p.Filters)
	if err != nil {
		return "", nil, err
	}
	if empty {
		return "", r.emptied(), nil
	}
	if err := r.applyPrice(q, p.MinPrice, p.MaxPrice); err != nil {
		return "", nil, err
	}
	if err := r.applySort(q, p.Sort); err != nil {
		return "", nil, err
	}
	applyPage(q, p.Page, r.PerPage)
	if len(segs) > 0 {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.Join(segs, "/")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil, nil
}

// applyFilters folds each filter into the query string or the path segments,
// using the fragments the page attaches to its own facet values. empty
// reports the store-holds-none case, which is an empty listing, not a URL.
func (r *SearchResult) applyFilters(q url.Values, filters []FilterArg) (segs []string, empty bool, err error) {
	baseFacets := q.Get("facets")
	facets := baseFacets
	for _, fa := range filters {
		fc, err := r.facet(fa.Key)
		if err != nil {
			return nil, false, err
		}
		v, err := fc.value(fa.Value, fa.Key)
		if err != nil {
			if fc.ID == "store" {
				return nil, true, nil
			}
			return nil, false, err
		}
		vu, err := r.base.Parse(v.url)
		if err != nil {
			return nil, false, apiChanged("facet value url %q: %v", v.url, err)
		}
		for _, s := range strings.Split(strings.Trim(vu.Path, "/"), "/") {
			if !strings.Contains("/"+strings.Trim(r.base.Path, "/")+"/", "/"+s+"/") {
				segs = append(segs, s)
			}
		}
		for k, vs := range vu.Query() {
			switch k {
			case "facets":
				facets += strings.TrimPrefix(vs[0], baseFacets)
			case "Ntt", "from", "size":
			default:
				q.Set(k, vs[0])
			}
		}
	}
	if facets != "" {
		q.Set("facets", facets)
	}
	return segs, false, nil
}

// applyPrice fills the price range pattern the page itself uses.
func (r *SearchResult) applyPrice(q url.Values, min, max float64) error {
	if min <= 0 && max <= 0 {
		return nil
	}
	if r.rangeU == nil {
		return Usage("this page offers no price filter")
	}
	lo, hi := min, max
	if fc, err := r.facet("price"); err == nil {
		if lo <= 0 {
			lo = fc.Min
		}
		if hi <= 0 {
			hi = fc.Max
		}
	}
	if hi > 0 && lo > hi {
		return Usage("--min-price is above --max-price")
	}
	pattern, err := url.QueryUnescape(r.rangeU.Pattern)
	if err != nil {
		return apiChanged("price pattern %q: %v", r.rangeU.Pattern, err)
	}
	pattern = strings.ReplaceAll(pattern, "${min}", num(lo))
	pattern = strings.ReplaceAll(pattern, "${max}", num(hi))
	q.Set(r.rangeU.Key, pattern)
	return nil
}

// applySort adds the sort key the page's own sort option uses.
func (r *SearchResult) applySort(q url.Values, name string) error {
	if name == "" {
		return nil
	}
	id, ok := SortID(name)
	if !ok {
		return Usage("--sort must be one of %s", strings.Join(SortNames(), ", "))
	}
	var so *SortOption
	for i := range r.Sorts {
		if r.Sorts[i].ID == id {
			so = &r.Sorts[i]
		}
	}
	if so == nil {
		return Usage("this page cannot sort by %s; it offers %s", name, strings.Join(r.offeredSorts(), ", "))
	}
	su, err := r.base.Parse(so.url)
	if err != nil {
		return apiChanged("sort url %q: %v", so.url, err)
	}
	q.Del("sort")
	q.Del("Ns")
	for _, k := range []string{"sort", "Ns"} {
		if v := su.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	return nil
}

// offeredSorts names the CLI sorts this page can do, in table order.
func (r *SearchResult) offeredSorts() []string {
	var out []string
	for _, s := range sortTable {
		for i := range r.Sorts {
			if r.Sorts[i].ID == s.id {
				out = append(out, s.name)
			}
		}
	}
	return out
}

// applyPage sets from/size for a 1-based page number; page 1 is the unfiltered
// first page.
func applyPage(q url.Values, page, perPage int) {
	if page <= 1 {
		return
	}
	per := perPage
	if per == 0 {
		per = 40
	}
	q.Set("from", strconv.Itoa((page-1)*per))
	q.Set("size", strconv.Itoa(per))
}

func (r *SearchResult) emptied() *SearchResult {
	e := *r
	e.Products, e.Total, e.Pages, e.Page = []Product{}, 0, 0, 0
	return &e
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func (r *SearchResult) facet(key string) (*Facet, error) {
	want := []string{key}
	if ids, ok := facetAliases[strings.ToLower(key)]; ok {
		want = append(ids, key)
	}
	for _, w := range want {
		for i := range r.Facets {
			f := &r.Facets[i]
			if strings.EqualFold(f.ID, w) || (f.Label != "" && fold(f.Label) == fold(w)) {
				return f, nil
			}
		}
	}
	if key == "store" {
		if r.mode == modeKeyword {
			return nil, Usage("the store filter exists only on category listings, and %q is a keyword search. Pass --category (see `decathlon category list`), or check one product with `decathlon store stock`", r.Query)
		}
		return nil, Usage("this listing offers no store filter")
	}
	var ids []string
	for _, f := range r.Facets {
		if f.Kind != "span" {
			ids = append(ids, f.ID)
		}
	}
	return nil, Usage("this page has no filter %q. It offers: %s (see `decathlon search filters`)", key, strings.Join(ids, ", "))
}

// value finds a facet value by id, label or URL slug. Store ids are accepted
// unpadded, the way product pages and `store list` print them.
func (f *Facet) value(v, key string) (*FacetValue, error) {
	if f.ID == "leadTime" {
		if id, ok := DeliveryID(v); ok {
			v = id
		}
	}
	cands := []string{v}
	if f.ID == "store" {
		if n, err := strconv.Atoi(v); err == nil {
			cands = append(cands, fmt.Sprintf("%05d", n))
		}
	}
	for _, c := range cands {
		for i := range f.Values {
			fv := &f.Values[i]
			if fv.ID != "" && fv.ID == c {
				return fv, nil
			}
			if fold(fv.Label) == fold(c) {
				return fv, nil
			}
			if s, ok := slugOf(fv.url); ok && s == slug(c) {
				return fv, nil
			}
		}
	}
	var labels []string
	for i, fv := range f.Values {
		if i == 12 {
			labels = append(labels, "...")
			break
		}
		labels = append(labels, fv.Label)
	}
	return nil, Usage("filter %s has no value %q here. Values include: %s", key, v, strings.Join(labels, ", "))
}

// slugOf returns the slug a category facet value URL ends with
// ("f-brand_quechua" -> "quechua"); keyword facet URLs have none.
func slugOf(u string) (string, bool) {
	path, _, _ := strings.Cut(u, "?")
	last := path[strings.LastIndex(path, "/")+1:]
	if !strings.HasPrefix(last, "f-") {
		return "", false
	}
	_, s, ok := strings.Cut(last, "_")
	return s, ok
}

// Filters returns the facets a search or category offers, for `search filters`.
func (c *Client) Filters(ctx context.Context, p SearchParams) (*SearchResult, error) {
	entry, err := p.entry()
	if err != nil {
		return nil, err
	}
	page, err := c.Fetch(ctx, entry, resolveTTL)
	if err != nil {
		return nil, err
	}
	return ParseListing(page)
}
