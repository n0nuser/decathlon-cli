package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Product is one listing tile: a model (one colour) of a product group.
type Product struct {
	Ref             string   `json:"ref"`      // pass to product show, store stock and watch add product
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
	Page        int             `json:"page"` // 1-based page number, as the site and the header show it
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
	ID  string `json:"id"`
	url string
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
		ID  string `json:"id"`
		URL string `json:"url"`
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
		if u, err := url.Parse(p.URL); err == nil && !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/search") && len(f.Find(func(m map[string]any) bool { return has(m, "menuItem") })) > 0 {
			// Top-level sections (deportes/camping, mujer...) are editorial
			// landing pages with no product list.
			return nil, &Error{Kind: KindUsage, Msg: fmt.Sprintf("%s is a landing page without a product list. Pick a subcategory: `decathlon category show %s`", u.Path, strings.TrimPrefix(strings.Trim(u.Path, "/"), "es/")), Endpoint: u.Path, Landing: true}
		}
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
	r := &SearchResult{URL: p.URL, base: u, Total: w.TotalNumberOfProducts, Page: w.Pagination.Current + 1, Pages: w.Pagination.Total, PerPage: w.Pagination.NbItemsPerPage, Applied: []AppliedFilter{}, Products: []Product{}}
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
		r.Sorts = append(r.Sorts, SortOption{ID: s.ID, url: s.URL})
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
	p.Ref = modelRef(p.ID, p.ModelID, p.URL)
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

// Path is the storefront path a value links to, without its query, in the
// form --category takes. Subcategory values are category pages.
func (v FacetValue) Path() string {
	p, _, _ := strings.Cut(v.url, "?")
	return strings.TrimPrefix(strings.Trim(p, "/"), "es/")
}
