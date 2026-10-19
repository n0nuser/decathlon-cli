package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

type categoryTree struct{ cats []api.Category }

func (t categoryTree) MarshalJSON() ([]byte, error) { return json.Marshal(t.cats) }

func (t categoryTree) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	var walk func(cs []api.Category, d int)
	walk = func(cs []api.Category, d int) {
		for _, c := range cs {
			rows = append(rows, []string{strings.Repeat("  ", d) + c.Name, output.Dim(c.Path, color)})
			walk(c.Children, d+1)
		}
	}
	walk(t.cats, 0)
	output.Table(w, color, []string{"CATEGORY", "PATH"}, rows)
}

func prune(cs []api.Category, depth int) []api.Category {
	out := make([]api.Category, len(cs))
	for i, c := range cs {
		out[i] = c
		if depth <= 1 {
			out[i].Children = nil
		} else {
			out[i].Children = prune(c.Children, depth-1)
		}
	}
	return out
}

type subcategory struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Count int    `json:"count,omitempty"` // unknown for menu-only sections
}

type categoryView struct {
	Category      *api.CategoryRef `json:"category"`
	Total         int              `json:"total"`
	URL           string           `json:"url"`
	Subcategories []subcategory    `json:"subcategories"`
}

func (v categoryView) Pretty(w io.Writer, color bool) {
	if v.Category != nil {
		head := v.Category.Name
		if v.Total > 0 {
			head += fmt.Sprintf(" · %d products", v.Total)
		}
		fmt.Fprintln(w, output.Bold(output.Clean(head), color))
	}
	rows := [][]string{}
	for _, s := range v.Subcategories {
		n := ""
		if s.Count > 0 {
			n = fmt.Sprint(s.Count)
		}
		rows = append(rows, []string{s.Name, n, output.Dim(s.Path, color)})
	}
	output.Table(w, color, []string{"SUBCATEGORY", "PRODUCTS", "PATH"}, rows)
}

func (a *App) categoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "category", Short: "Browse the category tree"}
	var depth int
	list := &cobra.Command{
		Use:   "list",
		Short: "List the navigation menu's categories with the paths --category takes",
		Long: `List the storefront's navigation menu. PATH is what --category takes in
'search' and 'watch add search'. The menu is cached for a day.

Examples:
  decathlon category list --format pretty
  decathlon category list --depth 3 --format jsonl | jq -r .path`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cats, err := a.Client.Categories(cmd.Context())
			if err != nil {
				return err
			}
			if depth < 1 {
				return output.Usagef("--depth must be at least 1")
			}
			return a.Printer.Print(categoryTree{prune(cats, depth)})
		},
	}
	list.Flags().IntVar(&depth, "depth", 2, "levels to show")
	show := &cobra.Command{
		Use:   "show PATH",
		Short: "Show a category's size and its subcategories",
		Long: `Show a category: how many products it lists and which subcategories it has,
with their product counts. Browse further with the subcategory paths, and
list products with 'decathlon search --category PATH'.

Examples:
  decathlon category show deportes/camping --format pretty`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.Client.Filters(cmd.Context(), api.SearchParams{Category: args[0]})
			if api.IsLandingPage(err) {
				return a.categoryFromMenu(cmd, args[0])
			}
			if err != nil {
				return err
			}
			v := categoryView{Category: r.Category, Total: r.Total, URL: r.URL, Subcategories: []subcategory{}}
			for _, f := range r.Facets {
				if f.ID != "categories" {
					continue
				}
				for _, val := range f.Values {
					v.Subcategories = append(v.Subcategories, subcategory{Name: val.Label, Path: val.Path(), Count: val.Count})
				}
			}
			return a.Printer.Print(v)
		},
	}
	cmd.AddCommand(list, show)
	return cmd
}

// categoryFromMenu answers `category show` for landing pages, which list no
// products: their subcategories come from the navigation menu instead.
func (a *App) categoryFromMenu(cmd *cobra.Command, path string) error {
	cats, err := a.Client.Categories(cmd.Context())
	if err != nil {
		return err
	}
	want := strings.TrimPrefix(strings.Trim(path, "/"), "es/")
	var find func(cs []api.Category) *api.Category
	find = func(cs []api.Category) *api.Category {
		for i := range cs {
			if cs[i].Path == want {
				return &cs[i]
			}
			if c := find(cs[i].Children); c != nil {
				return c
			}
		}
		return nil
	}
	c := find(cats)
	if c == nil {
		return &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("%s lists no products and is not in the menu; see `decathlon category list --depth 3`", want)}
	}
	v := categoryView{Category: &api.CategoryRef{ID: c.ID, Name: c.Name, Path: c.Path}, URL: a.Client.Abs("/es/" + c.Path), Subcategories: []subcategory{}}
	for _, ch := range c.Children {
		v.Subcategories = append(v.Subcategories, subcategory{Name: ch.Name, Path: ch.Path})
	}
	return a.Printer.Print(v)
}
