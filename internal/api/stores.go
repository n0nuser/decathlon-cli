package api

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Store is one physical Decathlon store from the store locator.
type Store struct {
	ID         string  `json:"id"` // the id product pages list in stores_with_stock
	Name       string  `json:"name"`
	Street     string  `json:"street,omitempty"`
	City       string  `json:"city,omitempty"`
	PostalCode string  `json:"postal_code,omitempty"`
	Region     string  `json:"region,omitempty"`
	Province   string  `json:"province,omitempty"`
	Phone      string  `json:"phone,omitempty"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	Hours      []Hours `json:"hours,omitempty"`
	DistanceKm float64 `json:"distance_km,omitempty"`
}

type Hours struct {
	Day   string `json:"day"`
	State string `json:"state"`
	Open  string `json:"open,omitempty"`
	Close string `json:"close,omitempty"`
}

type wireStore struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Coordinates struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"coordinates"`
	Address struct {
		Street     string `json:"street"`
		City       string `json:"city"`
		PostalCode string `json:"postalCode"`
		Region     string `json:"administrativeAreaLevel1"`
		Province   string `json:"administrativeAreaLevel2"`
	} `json:"address"`
	PhoneNumber  string `json:"phoneNumber"`
	WorkingHours []struct {
		State     string `json:"state"`
		Day       string `json:"day"`
		OpenHour  string `json:"openHour"`
		CloseHour string `json:"closeHour"`
	} `json:"workingHours"`
	PublishOnEcommerce bool `json:"publishOnEcommerce"`
}

// StoreDirectoryTTL is how long the store list is cached; stores open and
// close on a scale of months.
const StoreDirectoryTTL = 24 * time.Hour

var dayOrder = map[string]int{"MONDAY": 0, "TUESDAY": 1, "WEDNESDAY": 2, "THURSDAY": 3, "FRIDAY": 4, "SATURDAY": 5, "SUNDAY": 6}

// Stores returns every store in the store locator, sorted by id.
func (c *Client) Stores(ctx context.Context) ([]Store, error) {
	page, err := c.Fetch(ctx, "/es/store-locator", StoreDirectoryTTL)
	if err != nil {
		return nil, err
	}
	return ParseStores(page)
}

func ParseStores(p *Page) ([]Store, error) {
	f, err := ParseFlight(p.Body)
	if err != nil {
		return nil, err
	}
	hits := f.Find(func(m map[string]any) bool { return has(m, "thirdsId", "coordinates", "address", "name", "id") })
	if len(hits) == 0 {
		return nil, apiChanged("store locator lists no stores")
	}
	seen := map[string]bool{}
	var out []Store
	for _, h := range hits {
		var w wireStore
		if err := decodeInto(h, &w); err != nil {
			return nil, apiChanged("store entry changed shape: %v", err)
		}
		if seen[w.ID] {
			continue
		}
		seen[w.ID] = true
		s := Store{ID: w.ID, Name: w.Name, Street: w.Address.Street, City: w.Address.City, PostalCode: w.Address.PostalCode, Region: w.Address.Region, Province: w.Address.Province, Phone: w.PhoneNumber, Lat: w.Coordinates.Lat, Lng: w.Coordinates.Lng}
		for _, wh := range w.WorkingHours {
			s.Hours = append(s.Hours, Hours{Day: strings.ToLower(wh.Day), State: strings.ToLower(wh.State), Open: wh.OpenHour, Close: wh.CloseHour})
		}
		sort.Slice(s.Hours, func(i, j int) bool {
			return dayOrder[strings.ToUpper(s.Hours[i].Day)] < dayOrder[strings.ToUpper(s.Hours[j].Day)]
		})
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return idLess(out[i].ID, out[j].ID) })
	return out, nil
}

func idLess(a, b string) bool {
	x, errA := strconv.Atoi(a)
	y, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return x < y
	}
	return a < b
}

// FindStore resolves an id (padded or not) or a name/city to one store.
func FindStore(stores []Store, ref string) (*Store, error) {
	if n, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil {
		id := strconv.Itoa(n)
		for i := range stores {
			if stores[i].ID == id {
				return &stores[i], nil
			}
		}
		return nil, notFound("no store with id %s (see `decathlon store list`)", ref)
	}
	var matches []*Store
	for i := range stores {
		if fold(stores[i].Name) == fold(ref) {
			return &stores[i], nil
		}
		if strings.Contains(fold(stores[i].Name), fold(ref)) || fold(stores[i].City) == fold(ref) {
			matches = append(matches, &stores[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, notFound("no store matches %q (see `decathlon store list`)", ref)
	case 1:
		return matches[0], nil
	}
	var names []string
	for _, m := range matches {
		names = append(names, m.ID+" "+m.Name)
	}
	return nil, Usage("%q matches several stores: %s. Use the id", ref, strings.Join(names, ", "))
}

// Near sorts stores by distance from a point and fills DistanceKm.
func Near(stores []Store, lat, lng float64) []Store {
	out := append([]Store(nil), stores...)
	for i := range out {
		out[i].DistanceKm = math.Round(haversine(lat, lng, out[i].Lat, out[i].Lng)*10) / 10
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DistanceKm < out[j].DistanceKm })
	return out
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dlat, dlng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dlng/2)*math.Sin(dlng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// StockLine is one SKU's availability in one store.
type StockLine struct {
	StoreID   string  `json:"store_id"`
	StoreName string  `json:"store_name"`
	City      string  `json:"city,omitempty"`
	ModelID   string  `json:"model_id"`
	Color     string  `json:"color,omitempty"`
	SkuID     string  `json:"sku_id"`
	Size      string  `json:"size"`
	InStock   bool    `json:"in_stock"`
	Distance  float64 `json:"distance_km,omitempty"`
}

// StockReport is a product's availability across stores. The site reports
// presence only, never quantities.
type StockReport struct {
	ProductID string      `json:"product_id"`
	Title     string      `json:"title"`
	URL       string      `json:"url"`
	Lines     []StockLine `json:"lines"`
	// UnknownStores are ids product pages listed that the store locator does
	// not know (a store closing, or a warehouse).
	UnknownStores []string `json:"unknown_stores,omitempty"`
}

// StockQuery narrows a stock report.
type StockQuery struct {
	ModelID string   // empty: every model
	Size    string   // empty: every size
	Stores  []string // ids; empty: every store holding stock
	All     bool     // include out-of-stock lines for the named stores
}

// Stock builds the per-store stock report for a product.
func Stock(d *ProductDetail, stores []Store, q StockQuery) (*StockReport, error) {
	byID := map[string]*Store{}
	for i := range stores {
		byID[stores[i].ID] = &stores[i]
	}
	rep := &StockReport{ProductID: d.ID, Title: d.Title, URL: d.URL, Lines: []StockLine{}}
	unknown := map[string]bool{}
	matchedModel, matchedSize := q.ModelID == "", q.Size == ""
	for _, m := range d.Models {
		if q.ModelID != "" && m.ModelID != q.ModelID {
			continue
		}
		matchedModel = true
		color := strings.Join(m.Colors, "/")
		for _, s := range m.SKUs {
			if q.Size != "" && fold(s.Size) != fold(q.Size) {
				continue
			}
			matchedSize = true
			held := map[string]bool{}
			for _, id := range s.StoresWithStock {
				held[id] = true
			}
			line := func(id string, in bool) StockLine {
				l := StockLine{StoreID: id, ModelID: m.ModelID, Color: color, SkuID: s.SkuID, Size: s.Size, InStock: in}
				if st := byID[id]; st != nil {
					l.StoreName, l.City, l.Distance = st.Name, st.City, st.DistanceKm
				}
				return l
			}
			if len(q.Stores) > 0 {
				for _, id := range q.Stores {
					if held[id] || q.All {
						rep.Lines = append(rep.Lines, line(id, held[id]))
					}
				}
				continue
			}
			for _, id := range s.StoresWithStock {
				if byID[id] == nil {
					unknown[id] = true
				}
				rep.Lines = append(rep.Lines, line(id, true))
			}
		}
	}
	if !matchedModel {
		return nil, notFound("product %s has no model %s", d.ID, q.ModelID)
	}
	if !matchedSize {
		return nil, notFound("product %s has no size %q", d.ID, q.Size)
	}
	for id := range unknown {
		rep.UnknownStores = append(rep.UnknownStores, id)
	}
	sort.Slice(rep.UnknownStores, func(i, j int) bool { return idLess(rep.UnknownStores[i], rep.UnknownStores[j]) })
	sort.SliceStable(rep.Lines, func(i, j int) bool {
		a, b := rep.Lines[i], rep.Lines[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		if a.StoreID != b.StoreID {
			return idLess(a.StoreID, b.StoreID)
		}
		return false
	})
	return rep, nil
}
