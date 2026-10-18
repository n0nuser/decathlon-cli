package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Product is one listing tile: a model (one colour) of a product group.
type Product struct {
	ID              string   `json:"id"`       // product group ("supermodel") id, the number in /p/.../<id>/
	ModelID         string   `json:"model_id"` // the colour shown
	SkuID           string   `json:"sku_id,omitempty"`
	Title           string   `json:"title"`
	Brand           string   `json:"brand"`
	Price           float64  `json:"price"`
	Currency        string   `json:"currency"`
	ReferencePrice  float64  `json:"reference_price,omitempty"` // the crossed-out price while discounted
	DiscountPercent float64  `json:"discount_percent,omitempty"`
	Rating          float64  `json:"rating,omitempty"`
	Reviews         int      `json:"reviews,omitempty"`
	AvailableOnline bool     `json:"available_online"`
	Marketplace     bool     `json:"marketplace"` // sold by a third-party seller, not Decathlon
	Seller          string   `json:"seller,omitempty"`
	Sizes           []string `json:"sizes,omitempty"`
	URL             string   `json:"url"`
	Image           string   `json:"image,omitempty"`
}

// SearchResult is one page of a keyword search or a category listing.
type SearchResult struct {
	Query       string          `json:"query,omitempty"`
	Category    *CategoryRef    `json:"category,omitempty"`
	URL         string          `json:"url"` // what the site served, after redirects
	Total       int             `json:"total"`
	Page        int             `json:"page"` // zero-based
	Pages       int             `json:"pages"`
	PerPage     int             `json:"per_page"`
	Applied     []AppliedFilter `json:"applied_filters"`
	Products    []Product       `json:"products"`
	Suggestions []string        `json:"suggestions,omitempty"`

	Facets []Facet       `json:"-"`
	Sorts  []SortOption  `json:"-"`
	mode   listingMode   `json:"-"`
	base   *url.URL      `json:"-"`
	rangeU *rangeURLSpec `json:"-"`
}

type CategoryRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"` // e.g. deportes/camping/tiendas-campana
}

type AppliedFilter struct {
	Facet string `json:"facet"`
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
}

// Facet is one filter the page offers, with the values it offers for it.
type Facet struct {
	ID     string       `json:"id"`
	Label  string       `json:"label,omitempty"`
	Kind   string       `json:"kind"` // singleMatch, multiMatch, span
	Values []FacetValue `json:"values,omitempty"`
	Min    float64      `json:"min,omitempty"`
	Max    float64      `json:"max,omitempty"`
}

type FacetValue struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label"`
	Count   int    `json:"count"`
	Applied bool   `json:"applied,omitempty"`
	url     string
}

type SortOption struct {
	ID      string `json:"id"`
	Applied bool   `json:"applied"`
	url     string
}

type listingMode int

const (
	modeKeyword listingMode = iota
	modeCategory
)

// wire shapes, as the flight payload carries them

type wireListing struct {
	Category *struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		Name string `json:"name"`
	} `json:"category"`
	Search *struct {
		Terms       []string `json:"terms"`
		Suggestions struct {
			Terms []struct {
				Label string `json:"label"`
			} `json:"terms"`
		} `json:"suggestions"`
	} `json:"search"`
	Filters struct {
		Applied []struct {
			CategoryID string `json:"categoryId"`
			ID         string `json:"id"`
			Label      string `json:"label"`
		} `json:"applied"`
		Items []wireFacet `json:"items"`
	} `json:"filters"`
	Products   []wireProduct `json:"products"`
	Pagination struct {
		Current        int `json:"current"`
		Total          int `json:"total"`
		NbItemsPerPage int `json:"nbItemsPerPage"`
	} `json:"pagination"`
	Sorts []struct {
		ID        string      `json:"id"`
		URL       string      `json:"url"`
		IsApplied lenientBool `json:"isApplied"`
	} `json:"sorts"`
	TotalNumberOfProducts int `json:"totalNumberOfProducts"`
}

type wireFacet struct {
	ID     string          `json:"id"`
	Kind   string          `json:"kind"`
	Label  *string         `json:"label"`
	Values json.RawMessage `json:"values"`
	Range  *struct {
		Min float64      `json:"min"`
		Max float64      `json:"max"`
		URL rangeURLSpec `json:"url"`
	} `json:"range"`
}

type rangeURLSpec struct {
	Key     string `json:"key"`
	Pattern string `json:"pattern"`
}

type wireFacetValue struct {
	ID        *string     `json:"id"`
	Count     int         `json:"count"`
	Label     string      `json:"label"`
	URL       string      `json:"url"`
	IsApplied lenientBool `json:"isApplied"`
}

type wireProduct struct {
	ID           string          `json:"id"`
	SupermodelID string          `json:"supermodelId"`
	SkuID        string          `json:"skuId"`
	Label        string          `json:"label"`
	URL          string          `json:"url"`
	Images       json.RawMessage `json:"images"`
	Models       []struct {
		ID              string          `json:"id"`
		SkuID           string          `json:"skuId"`
		Brand           string          `json:"brand"`
		Label           string          `json:"label"`
		Price           json.RawMessage `json:"price"`
		AvailableOnline bool            `json:"availableOnline"`
		AvailableSizes  json.RawMessage `json:"availableSizes"`
		URL             string          `json:"url"`
		Seller          *struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"seller"`
	} `json:"models"`
	Reviews *struct {
		Count    int     `json:"count"`
		Notation float64 `json:"notation"`
	} `json:"reviews"`
}

// lenientBool accepts true/false and treats anything else (null, the
// "$undefined" token) as false.
type lenientBool bool

func (b *lenientBool) UnmarshalJSON(raw []byte) error {
	*b = string(raw) == "true"
	return nil
}

// money is the price object both listings and product pages use.
type money struct {
	Currencies struct {
		Main struct {
			ValueWithTaxes          float64 `json:"valueWithTaxes"`
			ReferenceValueWithTaxes float64 `json:"referenceValueWithTaxes"`
			DiscountPercentage      float64 `json:"discountPercentage"`
			Currency                string  `json:"currency"`
		} `json:"main"`
	} `json:"currencies"`
}

// ParseListing decodes a search or category page.
func ParseListing(p *Page) (*SearchResult, error) {
	f, err := ParseFlight(p.Body)
	if err != nil {
		return nil, err
	}
	hits := f.Find(func(m map[string]any) bool { return has(m, "totalNumberOfProducts", "products", "filters") })
	if len(hits) == 0 {
		return nil, apiChanged("listing page has no product grid (totalNumberOfProducts/products/filters)")
	}
	var w wireListing
	if err := decodeInto(hits[0], &w); err != nil {
		return nil, apiChanged("listing grid changed shape: %v", err)
	}
	u, err := url.Parse(p.URL)
	if err != nil {
		return nil, err
	}
	r := &SearchResult{URL: p.URL, base: u, Total: w.TotalNumberOfProducts, Page: w.Pagination.Current, Pages: w.Pagination.Total, PerPage: w.Pagination.NbItemsPerPage, Applied: []AppliedFilter{}, Products: []Product{}}
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/search") {
		r.mode = modeKeyword
		r.Query = u.Query().Get("Ntt")
	} else {
		r.mode = modeCategory
	}
	if w.Category != nil {
		r.Category = &CategoryRef{ID: w.Category.ID, Name: w.Category.Name, Path: strings.Trim(w.Category.URL, "/")}
	}
	if w.Search != nil {
		if r.Query == "" {
			r.Query = strings.Join(w.Search.Terms, " ")
		}
		for _, s := range w.Search.Suggestions.Terms {
			r.Suggestions = append(r.Suggestions, s.Label)
		}
	}
	for _, a := range w.Filters.Applied {
		facet := a.CategoryID
		if facet == "" {
			facet = "price"
		}
		r.Applied = append(r.Applied, AppliedFilter{Facet: facet, ID: a.ID, Label: a.Label})
	}
	for _, wf := range w.Filters.Items {
		fc := Facet{ID: wf.ID, Kind: wf.Kind}
		if wf.Label != nil {
			fc.Label = *wf.Label
		}
		if wf.Range != nil {
			fc.Min, fc.Max = wf.Range.Min, wf.Range.Max
			spec := wf.Range.URL
			r.rangeU = &spec
		}
		var vals []wireFacetValue
		_ = json.Unmarshal(wf.Values, &vals) // a ref or null means no values
		for _, v := range vals {
			fv := FacetValue{Label: v.Label, Count: v.Count, Applied: bool(v.IsApplied), url: v.URL}
			if v.ID != nil {
				fv.ID = *v.ID
			}
			fc.Values = append(fc.Values, fv)
		}
		r.Facets = append(r.Facets, fc)
	}
	for _, s := range w.Sorts {
		r.Sorts = append(r.Sorts, SortOption{ID: s.ID, Applied: bool(s.IsApplied), url: s.URL})
	}
	for _, wp := range w.Products {
		r.Products = append(r.Products, wp.product(p.URL))
	}
	return r, nil
}

func (wp wireProduct) product(pageURL string) Product {
	p := Product{ID: wp.SupermodelID, SkuID: wp.SkuID, Title: strings.TrimSpace(wp.Label), URL: absURL(pageURL, wp.URL)}
	var imgs []struct {
		Src string `json:"src"`
	}
	if json.Unmarshal(wp.Images, &imgs) == nil && len(imgs) > 0 {
		p.Image = imgs[0].Src
	}
	if wp.Reviews != nil {
		p.Rating, p.Reviews = wp.Reviews.Notation, wp.Reviews.Count
	}
	if len(wp.Models) > 0 {
		m := wp.Models[0]
		p.ModelID, p.Brand, p.AvailableOnline = m.ID, m.Brand, m.AvailableOnline
		if m.SkuID != "" {
			p.SkuID = m.SkuID
		}
		var mo money
		if json.Unmarshal(m.Price, &mo) == nil {
			main := mo.Currencies.Main
			p.Price, p.Currency, p.ReferencePrice, p.DiscountPercent = main.ValueWithTaxes, main.Currency, main.ReferenceValueWithTaxes, main.DiscountPercentage
		}
		_ = json.Unmarshal(m.AvailableSizes, &p.Sizes)
		if m.Seller != nil {
			p.Seller = m.Seller.ID
			p.Marketplace = m.Seller.Type != "" && m.Seller.Type != "FIRST_PARTY"
		}
		if m.URL != "" {
			p.URL = absURL(pageURL, m.URL)
		}
	}
	if p.ID == "" {
		p.ID, _, _ = strings.Cut(wp.ID, "_")
	}
	return p
}

func absURL(pageURL, ref string) string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return ref
	}
	u, err := base.Parse(ref)
	if err != nil {
		return ref
	}
	return u.String()
}

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
	Page     int         `json:"page,omitempty"`
}

type FilterArg struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Sorts maps CLI names to the site's sort ids.
var Sorts = map[string]string{
	"relevance":  "rankingRule",
	"price_asc":  "priceAscending",
	"price_desc": "priceDescending",
	"discount":   "discountRateDescending",
	"rating":     "reviewDescending",
	"newest":     "newArrivals",
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

// DeliveryValues maps --delivery to the leadTime facet's value ids.
var DeliveryValues = map[string]string{"24h": "ud-0-24", "48h": "ud-0-48"}

const (
	resolveTTL = 10 * time.Minute // unfiltered page used to resolve filter values
)

func (p SearchParams) modified() bool {
	return len(p.Filters) > 0 || p.MinPrice > 0 || p.MaxPrice > 0 || p.Sort != "" || p.Page > 0
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
	ttl := time.Duration(0)
	if p.modified() {
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
	if !p.modified() {
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
	facets := q.Get("facets")
	var segs []string
	for _, fa := range p.Filters {
		fc, err := r.facet(fa.Key)
		if err != nil {
			return "", nil, err
		}
		v, err := fc.value(fa.Value, fa.Key)
		if err != nil {
			if fc.ID == "store" {
				return "", r.emptied(), nil
			}
			return "", nil, err
		}
		vu, err := r.base.Parse(v.url)
		if err != nil {
			return "", nil, apiChanged("facet value url %q: %v", v.url, err)
		}
		for _, s := range strings.Split(strings.Trim(vu.Path, "/"), "/") {
			if !strings.Contains("/"+strings.Trim(r.base.Path, "/")+"/", "/"+s+"/") {
				segs = append(segs, s)
			}
		}
		for k, vs := range vu.Query() {
			switch k {
			case "facets":
				facets += strings.TrimPrefix(vs[0], q.Get("facets"))
			case "Ntt", "from", "size":
			default:
				q.Set(k, vs[0])
			}
		}
	}
	if facets != "" {
		q.Set("facets", facets)
	}
	if p.MinPrice > 0 || p.MaxPrice > 0 {
		if r.rangeU == nil {
			return "", nil, Usage("this page offers no price filter")
		}
		lo, hi := p.MinPrice, p.MaxPrice
		fc, _ := r.facet("price")
		if fc != nil {
			if lo <= 0 {
				lo = fc.Min
			}
			if hi <= 0 {
				hi = fc.Max
			}
		}
		if hi > 0 && lo > hi {
			return "", nil, Usage("--min-price is above --max-price")
		}
		pattern, err := url.QueryUnescape(r.rangeU.Pattern)
		if err != nil {
			return "", nil, apiChanged("price pattern %q: %v", r.rangeU.Pattern, err)
		}
		pattern = strings.ReplaceAll(pattern, "${min}", num(lo))
		pattern = strings.ReplaceAll(pattern, "${max}", num(hi))
		q.Set(r.rangeU.Key, pattern)
	}
	if p.Sort != "" {
		id, ok := Sorts[p.Sort]
		if !ok {
			return "", nil, Usage("--sort must be one of %s", strings.Join(sortedKeys(Sorts), ", "))
		}
		var so *SortOption
		for i := range r.Sorts {
			if r.Sorts[i].ID == id {
				so = &r.Sorts[i]
			}
		}
		if so == nil {
			var offered []string
			for name, sid := range Sorts {
				for _, s := range r.Sorts {
					if s.ID == sid {
						offered = append(offered, name)
					}
				}
			}
			sort.Strings(offered)
			return "", nil, Usage("this page cannot sort by %s; it offers %s", p.Sort, strings.Join(offered, ", "))
		}
		su, err := r.base.Parse(so.url)
		if err != nil {
			return "", nil, apiChanged("sort url %q: %v", so.url, err)
		}
		q.Del("sort")
		q.Del("Ns")
		for _, k := range []string{"sort", "Ns"} {
			if v := su.Query().Get(k); v != "" {
				q.Set(k, v)
			}
		}
	}
	if p.Page > 0 {
		per := r.PerPage
		if per == 0 {
			per = 40
		}
		q.Set("from", strconv.Itoa(p.Page*per))
		q.Set("size", strconv.Itoa(per))
	}
	if len(segs) > 0 {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.Join(segs, "/")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil, nil
}

func (r *SearchResult) emptied() *SearchResult {
	e := *r
	e.Products, e.Total, e.Pages, e.Page = []Product{}, 0, 0, 0
	return &e
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// facet finds a facet by id, CLI alias or label.
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
		if id, ok := DeliveryValues[v]; ok {
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
			if (fv.ID != "" && fv.ID == c) || fold(fv.Label) == fold(c) || slugOf(fv.url) == slug(c) {
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
func slugOf(u string) string {
	path, _, _ := strings.Cut(u, "?")
	last := path[strings.LastIndex(path, "/")+1:]
	if !strings.HasPrefix(last, "f-") {
		return "\x00"
	}
	_, s, ok := strings.Cut(last, "_")
	if !ok {
		return "\x00"
	}
	return s
}

// accents covers the letters Spanish (and the odd brand name) uses; the
// site's own slugs drop them the same way.
var accents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c", "º", "o", "ª", "a")

// fold lowercases and strips accents, so "campaña" matches "CAMPAÑA" and
// "campana".
func fold(s string) string { return accents.Replace(strings.ToLower(strings.TrimSpace(s))) }

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range fold(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
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
