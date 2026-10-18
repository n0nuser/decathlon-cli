package api

import (
	"context"
	"strings"
	"time"
)

// Category is one node of the storefront's navigation menu.
type Category struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Path     string     `json:"path"` // pass to --category, e.g. deportes/camping/tiendas-campana
	Children []Category `json:"children,omitempty"`
}

const categoryTTL = 24 * time.Hour

type wireMenuNode struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	URL                 string         `json:"url"`
	Children            []wireMenuNode `json:"children"`
	PrimaryCategories   []wireMenuNode `json:"primaryCategories"`
	SecondaryCategories []wireMenuNode `json:"secondaryCategories"`
	ThematicCategories  []wireMenuNode `json:"thematicCategories"`
}

func (n wireMenuNode) category() Category {
	c := Category{ID: n.ID, Name: strings.TrimSpace(n.Name), Path: strings.TrimPrefix(strings.Trim(n.URL, "/"), "es/")}
	for _, group := range [][]wireMenuNode{n.PrimaryCategories, n.SecondaryCategories, n.ThematicCategories, n.Children} {
		for _, ch := range group {
			if ch.URL == "" || !strings.HasPrefix(ch.URL, "/es/") {
				continue // external links and campaign tiles
			}
			c.Children = append(c.Children, ch.category())
		}
	}
	return c
}

// Categories returns the navigation menu tree (cached for a day).
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	page, err := c.Fetch(ctx, "/es/", categoryTTL)
	if err != nil {
		return nil, err
	}
	return ParseCategories(page)
}

func ParseCategories(p *Page) ([]Category, error) {
	f, err := ParseFlight(p.Body)
	if err != nil {
		return nil, err
	}
	hits := f.Find(func(m map[string]any) bool {
		mi, ok := m["menuItem"].(map[string]any)
		return ok && has(mi, "name", "url")
	})
	if len(hits) == 0 {
		return nil, apiChanged("page has no navigation menu")
	}
	seen := map[string]bool{}
	var out []Category
	for _, h := range hits {
		var n wireMenuNode
		if err := decodeInto(h["menuItem"].(map[string]any), &n); err != nil {
			return nil, apiChanged("menu item changed shape: %v", err)
		}
		if seen[n.ID] || !strings.HasPrefix(n.URL, "/es/") {
			continue
		}
		seen[n.ID] = true
		out = append(out, n.category())
	}
	return out, nil
}
