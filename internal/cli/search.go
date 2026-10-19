package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

// searchFlags are shared by `search` and `watch add search`.
type searchFlags struct {
	category string
	brands   []string
	store    string
	delivery string
	minPrice float64
	maxPrice float64
	sort     string
	filters  []string
}

func (a *App) bindSearchFlags(cmd *cobra.Command, f *searchFlags) *pflag.FlagSet {
	fs := pflag.NewFlagSet("search", pflag.ContinueOnError)
	fs.StringVar(&f.category, "category", "", "list a category instead of searching keywords (path from `decathlon category list`)")
	fs.StringArrayVar(&f.brands, "brand", nil, "brand, repeatable (any brand the page lists)")
	fs.StringVar(&f.store, "store", "", "only products in stock at this store (id or name; category listings only)")
	fs.StringVar(&f.delivery, "delivery", "", "only products delivered within "+strings.Join(api.DeliveryNames(), " or ")+" (category listings only)")
	fs.Float64Var(&f.minPrice, "min-price", 0, "minimum price in euros")
	fs.Float64Var(&f.maxPrice, "max-price", 0, "maximum price in euros")
	fs.StringVar(&f.sort, "sort", "", strings.Join(api.SortNames(), ", ")+" (newest: categories only)")
	fs.StringArrayVar(&f.filters, "filter", nil, "any other filter the page offers, as key=value, repeatable (see `decathlon search filters`)")
	cmd.Flags().AddFlagSet(fs)
	_ = cmd.RegisterFlagCompletionFunc("sort", cobra.FixedCompletions(api.SortNames(), cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("delivery", cobra.FixedCompletions(api.DeliveryNames(), cobra.ShellCompDirectiveNoFileComp))
	return fs
}

func (f searchFlags) params(keywords []string) (api.SearchParams, error) {
	p := api.SearchParams{Query: strings.TrimSpace(strings.Join(keywords, " ")), Category: f.category, MinPrice: f.minPrice, MaxPrice: f.maxPrice, Sort: f.sort}
	if p.Query == "" && p.Category == "" {
		return p, output.Usagef("give keywords or --category. See `decathlon search --help`")
	}
	if f.minPrice < 0 || f.maxPrice < 0 {
		return p, output.Usagef("prices must be positive")
	}
	if f.sort != "" {
		if _, ok := api.SortID(f.sort); !ok {
			return p, output.Usagef("--sort must be one of %s", strings.Join(api.SortNames(), ", "))
		}
	}
	for _, b := range f.brands {
		p.Filters = append(p.Filters, api.FilterArg{Key: "brand", Value: b})
	}
	if f.store != "" {
		p.Filters = append(p.Filters, api.FilterArg{Key: "store", Value: f.store})
	}
	if f.delivery != "" {
		if _, ok := api.DeliveryID(f.delivery); !ok {
			return p, output.Usagef("--delivery must be 24h or 48h")
		}
		p.Filters = append(p.Filters, api.FilterArg{Key: "delivery", Value: f.delivery})
	}
	for _, kv := range f.filters {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return p, output.Usagef("--filter takes key=value, got %q", kv)
		}
		p.Filters = append(p.Filters, api.FilterArg{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
	}
	return p, nil
}

type searchView struct {
	*api.SearchResult
}

func (v searchView) JSONLines() []any {
	out := make([]any, len(v.Products))
	for i, p := range v.Products {
		out[i] = p
	}
	return out
}

func (v searchView) Pretty(w io.Writer, color bool) {
	title := v.Query
	if v.Category != nil {
		title = v.Category.Name
	}
	head := fmt.Sprintf("%d products · %s", v.Total, title)
	if v.Pages > 1 {
		head += fmt.Sprintf(" · page %d of %d", v.Page, v.Pages)
	}
	if len(v.Applied) > 0 {
		var fs []string
		for _, a := range v.Applied {
			fs = append(fs, a.Facet+"="+a.Label)
		}
		head += " · " + strings.Join(fs, ", ")
	}
	fmt.Fprintln(w, output.Bold(output.Clean(head), color))
	rows := make([][]string, 0, len(v.Products))
	for _, p := range v.Products {
		price := output.Money(p.Price, p.Currency)
		if p.ReferencePrice > p.Price {
			price += " " + output.Dim("(-"+fmt.Sprintf("%.0f", 100-p.Price/p.ReferencePrice*100)+"%)", color)
		}
		rating := ""
		if p.Reviews > 0 {
			rating = fmt.Sprintf("%.1f (%d)", p.Rating, p.Reviews)
		}
		mark := ""
		if p.Marketplace {
			mark = "mkt"
		}
		rows = append(rows, []string{p.Ref, price, output.Truncate(p.Brand, 14), output.Truncate(p.Title, 56), rating, mark})
	}
	output.Table(w, color, []string{"REF", "PRICE", "BRAND", "TITLE", "RATING", ""}, rows)
	if len(v.Suggestions) > 0 && v.Total == 0 {
		fmt.Fprintln(w, output.Dim("did you mean: "+strings.Join(v.Suggestions, ", "), color))
	}
	if v.Page < v.Pages {
		fmt.Fprintln(w, output.Dim(fmt.Sprintf("more: --page %d", v.Page+1), color))
	}
}

func (a *App) searchCmd() *cobra.Command {
	var f searchFlags
	var page, pages, limit int
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Search products, or list a category, with the website's filters",
		Long: `Search products by keywords, or list a category with --category.

Every filter the website shows works: the common ones have flags, the rest go
through --filter key=value, with keys and values as listed by
'decathlon search filters'. Keyword searches that match a category (such as
"tienda campaña") are redirected by the site to that category, which also
offers the store and delivery filters.

Pass the REF column (ref in JSON) to 'product show', 'store stock' and
'watch add product'.

Examples:
  decathlon search "tienda campaña" --format pretty
  decathlon search "hornillo gas" --brand campingaz --max-price 40 --sort price_asc
  decathlon search --category deportes/camping/tiendas-campana --store 169 --delivery 48h
  decathlon search --category deportes/camping/tiendas-campana --filter "Nº de plazas=2 plazas"
  decathlon search "botas montaña" --pages 3 --format jsonl | jq -r .url`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := f.params(args)
			if err != nil {
				return err
			}
			if pages < 1 {
				return output.Usagef("--pages must be at least 1")
			}
			if page < 1 {
				return output.Usagef("--page is a 1-based page number and cannot be below 1")
			}
			if limit < 0 {
				return output.Usagef("--limit cannot be negative")
			}
			p.Page = page
			var res *api.SearchResult
			for i := 0; i < pages; i++ {
				r, err := a.Client.Search(cmd.Context(), p)
				if err != nil {
					return err
				}
				if res == nil {
					res = r
				} else {
					res.Products = append(res.Products, r.Products...)
					res.Page = r.Page
				}
				if r.Page >= r.Pages || (limit > 0 && len(res.Products) >= limit) {
					break
				}
				p.Page++
			}
			if limit > 0 && len(res.Products) > limit {
				res.Products = res.Products[:limit]
			}
			return a.Printer.Print(searchView{res})
		},
	}
	a.bindSearchFlags(cmd, &f)
	cmd.Flags().IntVar(&page, "page", 1, "result page to start from (1-based, 40 products each)")
	cmd.Flags().IntVar(&pages, "pages", 1, "how many pages to fetch")
	cmd.Flags().IntVar(&limit, "limit", 0, "stop after this many products (0: whole pages)")
	cmd.AddCommand(a.searchFiltersCmd())
	return cmd
}

type facetsView []api.Facet

func (v facetsView) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, f := range v {
		vals := ""
		if f.Kind == "span" {
			vals = fmt.Sprintf("%s – %s (--min-price/--max-price)", output.Money(f.Min, "EUR"), output.Money(f.Max, "EUR"))
		} else {
			var vs []string
			for _, x := range f.Values {
				vs = append(vs, fmt.Sprintf("%s (%d)", x.Label, x.Count))
			}
			vals = output.Truncate(strings.Join(vs, ", "), 90)
		}
		rows = append(rows, []string{f.ID, f.Label, vals})
	}
	output.Table(w, color, []string{"KEY", "LABEL", "VALUES"}, rows)
}

func (a *App) searchFiltersCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "filters [keywords...]",
		Short: "List the filters and values a search or category offers",
		Long: `List the filters a search or category page offers. KEY (or LABEL) and any
value's label go into --filter key=value. Keyword searches and category
listings offer different filters; only categories have store and delivery.

Examples:
  decathlon search filters "hornillo gas" --format pretty
  decathlon search filters --category deportes/camping/tiendas-campana --format pretty`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := api.SearchParams{Query: strings.TrimSpace(strings.Join(args, " ")), Category: category}
			r, err := a.Client.Filters(cmd.Context(), p)
			if err != nil {
				return err
			}
			return a.Printer.Print(facetsView(r.Facets))
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "category path")
	return cmd
}
