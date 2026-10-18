package api

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// ProductDetail is a product group: every colour (model) and every size
// (SKU) the product page offers, with per-SKU store stock.
type ProductDetail struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Brand       string   `json:"brand"`
	Description string   `json:"description,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Reviews     int      `json:"reviews,omitempty"`
	URL         string   `json:"url"`
	Image       string   `json:"image,omitempty"`
	Breadcrumbs []string `json:"breadcrumbs,omitempty"`
	Models      []Model  `json:"models"`
}

type Model struct {
	ModelID         string   `json:"model_id"`
	Colors          []string `json:"colors,omitempty"`
	Selected        bool     `json:"selected"` // the model the URL pointed at
	Price           float64  `json:"price"`
	Currency        string   `json:"currency"`
	ReferencePrice  float64  `json:"reference_price,omitempty"`
	DiscountPercent float64  `json:"discount_percent,omitempty"`
	Seller          string   `json:"seller,omitempty"`
	Marketplace     bool     `json:"marketplace"`
	URL             string   `json:"url"`
	SKUs            []SKU    `json:"skus"`
}

// SKU is one buyable size of a model. StoresWithStock is the list of
// physical store ids the page reports as holding it. The site gives no
// quantities: a store is either in the list or not.
type SKU struct {
	SkuID           string   `json:"sku_id"`
	Size            string   `json:"size"`
	Price           float64  `json:"price"`
	AvailableOnline bool     `json:"available_online"`
	HomeDelivery    bool     `json:"home_delivery"`
	StorePickup     bool     `json:"store_pickup"`
	StoresWithStock []string `json:"stores_with_stock"`
	EAN             string   `json:"ean,omitempty"`
}

type wireItemGroup struct {
	ID      string `json:"id"`
	Reviews *struct {
		Count   int     `json:"count"`
		Average float64 `json:"average"`
	} `json:"reviews"`
	SkuGroups []struct {
		ModelID            string `json:"modelId"`
		IsSelected         bool   `json:"isSelected"`
		VarianceAttributes struct {
			Colors []struct {
				Name string `json:"name"`
			} `json:"colors"`
		} `json:"varianceAttributes"`
		SkuGroupConditions []struct {
			Seller *struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"seller"`
			TypeTargets []priceTarget `json:"typeTargets"`
		} `json:"skuGroupConditions"`
		Skus     []wireSku `json:"skus"`
		ModelURL string    `json:"modelUrl"`
	} `json:"skuGroups"`
	URL string `json:"url"`
}

type priceTarget struct {
	TypeTarget string `json:"typeTarget"`
	money
}

type wireSku struct {
	SkuID     string `json:"skuId"`
	Title     string `json:"title"`
	SizeLabel string `json:"sizeLabel"`
	Catchline string `json:"catchline"`
	Brand     *struct {
		Name string `json:"name"`
	} `json:"brand"`
	MainImage *struct {
		URL string `json:"url"`
	} `json:"mainImage"`
	Offers []struct {
		FixedPrices []struct {
			TypeTargets []priceTarget `json:"typeTargets"`
		} `json:"fixedPrices"`
	} `json:"offers"`
	Gtins                          []string        `json:"gtins"`
	StoresWithStock                json.RawMessage `json:"storesWithStock"`
	IsAvailable                    bool            `json:"isAvailable"`
	IsStorePickupAvailable         bool            `json:"isStorePickupAvailable"`
	IsHomeOrStoreDeliveryAvailable bool            `json:"isHomeOrStoreDeliveryAvailable"`
	URL                            string          `json:"url"`
}

// pick prefers the online ("ecommerce") price over the country-wide one.
func pick(ts []priceTarget) *priceTarget {
	for i := range ts {
		if ts[i].TypeTarget == "ecommerce" {
			return &ts[i]
		}
	}
	if len(ts) > 0 {
		return &ts[0]
	}
	return nil
}

// productRefRE pulls the group id and optional model id out of a product
// URL: /es/p/<slug>/<group>/<variance>m<model>.
var productRefRE = regexp.MustCompile(`/p/[^/]+/([A-Za-z0-9]+)(?:/[a-z0-9]*?m([0-9]+))?`)

// ProductPath turns what the user typed into a product page path. A full
// URL is used as given; "303295" or "303295/8513471" (group/model) become a
// short URL the site redirects to the canonical one. A bare group id needs a
// model id: the site has no group-only route.
func ProductPath(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && u.IsAbs() {
		if !productRefRE.MatchString(u.Path) {
			return "", Usage("%q is not a decathlon product url (they look like /es/p/<name>/<id>/...m<model>)", ref)
		}
		return u.String(), nil
	}
	group, model, _ := strings.Cut(ref, "/")
	model = strings.TrimPrefix(model, "m")
	if group == "" || model == "" || !alnum(group) || !digits(model) {
		return "", Usage("give a product url, or GROUP/MODEL ids as printed by search (id and model_id), e.g. 303295/8513471")
	}
	return "/es/p/_/" + group + "/m" + model, nil
}

func alnum(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return s != ""
}

func digits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Product fetches one product page.
func (c *Client) Product(ctx context.Context, ref string) (*ProductDetail, error) {
	path, err := ProductPath(ref)
	if err != nil {
		return nil, err
	}
	page, err := c.Fetch(ctx, path, 0)
	if err != nil {
		return nil, err
	}
	return ParseProduct(page)
}

// ParseProduct decodes a product page. Recommendation and bundle blocks on
// the same page carry item groups too, with their own stock; the product
// itself is the group whose id is in the page URL.
func ParseProduct(p *Page) (*ProductDetail, error) {
	u, err := url.Parse(p.URL)
	if err != nil {
		return nil, err
	}
	m := productRefRE.FindStringSubmatch(u.Path)
	if m == nil {
		// Unknown products redirect to, or render as, a generic page.
		return nil, notFound("decathlon has no such product (the page served was %s)", u.Path)
	}
	group := m[1]
	f, err := ParseFlight(p.Body)
	if err != nil {
		return nil, err
	}
	hits := f.Find(func(o map[string]any) bool {
		ig, ok := o["itemGroup"].(map[string]any)
		return ok && ig["id"] == group && has(ig, "skuGroups")
	})
	if len(hits) == 0 {
		if len(f.Find(func(o map[string]any) bool { return has(o, "skuGroups") })) == 0 {
			return nil, notFound("decathlon has no product %s", group)
		}
		return nil, apiChanged("product page has no item group %s", group)
	}
	var ig wireItemGroup
	if err := decodeInto(hits[0]["itemGroup"].(map[string]any), &ig); err != nil {
		return nil, apiChanged("product item group changed shape: %v", err)
	}
	d := &ProductDetail{ID: ig.ID, URL: p.URL, Models: []Model{}}
	if ig.Reviews != nil {
		d.Rating, d.Reviews = ig.Reviews.Average, ig.Reviews.Count
	}
	for _, b := range f.Find(func(o map[string]any) bool { return has(o, "breadcrumbs") }) {
		if list, ok := b["breadcrumbs"].([]any); ok {
			for _, it := range list {
				if m, ok := it.(map[string]any); ok {
					if name, ok := m["name"].(string); ok {
						d.Breadcrumbs = append(d.Breadcrumbs, name)
					}
				}
			}
			break
		}
	}
	for _, g := range ig.SkuGroups {
		md := Model{ModelID: g.ModelID, Selected: g.IsSelected, URL: absURL(p.URL, g.ModelURL), SKUs: []SKU{}}
		for _, c := range g.VarianceAttributes.Colors {
			md.Colors = append(md.Colors, c.Name)
		}
		if len(g.SkuGroupConditions) > 0 {
			cond := g.SkuGroupConditions[0]
			if t := pick(cond.TypeTargets); t != nil {
				mm := t.Currencies.Main
				md.Price, md.Currency, md.ReferencePrice, md.DiscountPercent = mm.ValueWithTaxes, mm.Currency, mm.ReferenceValueWithTaxes, mm.DiscountPercentage
			}
			if cond.Seller != nil {
				md.Seller = cond.Seller.Name
				md.Marketplace = cond.Seller.Type != "" && cond.Seller.Type != "FIRST_PARTY"
			}
		}
		for _, s := range g.Skus {
			if d.Title == "" || g.IsSelected {
				d.Title = strings.TrimSpace(s.Title)
				d.Description = s.Catchline
				if s.Brand != nil {
					d.Brand = s.Brand.Name
				}
				if s.MainImage != nil {
					d.Image = s.MainImage.URL
				}
			}
			sku := SKU{SkuID: s.SkuID, Size: s.SizeLabel, AvailableOnline: s.IsAvailable, HomeDelivery: s.IsHomeOrStoreDeliveryAvailable, StorePickup: s.IsStorePickupAvailable, StoresWithStock: []string{}}
			_ = json.Unmarshal(s.StoresWithStock, &sku.StoresWithStock)
			if sku.StoresWithStock == nil {
				sku.StoresWithStock = []string{}
			}
			if len(s.Offers) > 0 && len(s.Offers[0].FixedPrices) > 0 {
				if t := pick(s.Offers[0].FixedPrices[0].TypeTargets); t != nil {
					sku.Price = t.Currencies.Main.ValueWithTaxes
				}
			}
			if sku.Price == 0 {
				sku.Price = md.Price
			}
			if len(s.Gtins) > 0 {
				sku.EAN = s.Gtins[0]
			}
			md.SKUs = append(md.SKUs, sku)
		}
		d.Models = append(d.Models, md)
	}
	return d, nil
}
