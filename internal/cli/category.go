package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

type categoryTree struct {
	cats  []api.Category
	depth int
}

func (t categoryTree) MarshalJSON() ([]byte, error) { return jsonOf(t.cats) }

func (t categoryTree) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	var walk func(cs []api.Category, d int)
	walk = func(cs []api.Category, d int) {
		for _, c := range cs {
			rows = append(rows, []string{strings.Repeat("  ", d) + c.Name, output.Dim(c.Path, color)})
			if d+1 < t.depth {
				walk(c.Children, d+1)
			}
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
	Count int    `json:"count"`
}

type categoryView struct {
	Category      *api.CategoryRef `json:"category"`
	Total         int              `json:"total"`
	URL           string           `json:"url"`
	Subcategories []subcategory    `json:"subcategories"`
}

func (v categoryView) Pretty(w io.Writer, color bool) {
	if v.Category != nil {
		fmt.Fprintln(w, output.Bold(output.Clean(fmt.Sprintf("%s · %d products", v.Category.Name, v.Total)), color))
	}
	rows := [][]string{}
	for _, s := range v.Subcategories {
		rows = append(rows, []string{s.Name, fmt.Sprint(s.Count), output.Dim(s.Path, color)})
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
			return a.Printer.Print(categoryTree{prune(cats, depth), depth})
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
