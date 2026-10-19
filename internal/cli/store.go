package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

func jsonOf(v any) ([]byte, error) { return json.Marshal(v) }

// parseNear reads "lat,lng". Empty falls back to config location.
func (a *App) parseNear(s string) (lat, lng float64, ok bool, err error) {
	if s == "" {
		if l := a.Cfg.Location; l != nil {
			return l.Lat, l.Lng, true, nil
		}
		return 0, 0, false, nil
	}
	la, ln, found := strings.Cut(s, ",")
	if found {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(la), 64)
		lng, err2 := strconv.ParseFloat(strings.TrimSpace(ln), 64)
		if err1 == nil && err2 == nil && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180 {
			return lat, lng, true, nil
		}
	}
	return 0, 0, false, output.Usagef("--near takes LAT,LNG, e.g. 40.4168,-3.7038")
}

type storeList []api.Store

func (l storeList) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, s := range l {
		dist := ""
		if s.DistanceKm > 0 {
			dist = fmt.Sprintf("%.1f km", s.DistanceKm)
		}
		rows = append(rows, []string{s.ID, output.Truncate(s.Name, 30), output.Truncate(s.City, 22), output.Truncate(s.Province, 16), dist})
	}
	output.Table(w, color, []string{"ID", "STORE", "CITY", "PROVINCE", "DISTANCE"}, rows)
}

type storeView struct{ *api.Store }

func (v storeView) Pretty(w io.Writer, color bool) {
	s := v.Store
	fmt.Fprintln(w, output.Bold(output.Clean(fmt.Sprintf("%s (store %s)", s.Name, s.ID)), color))
	fmt.Fprintln(w, output.Clean(strings.TrimSpace(s.Street+", "+s.PostalCode+" "+s.City)))
	if s.Phone != "" {
		fmt.Fprintln(w, output.Clean(s.Phone))
	}
	fmt.Fprintln(w, output.Dim(fmt.Sprintf("%.5f,%.5f", s.Lat, s.Lng), color))
	rows := [][]string{}
	for _, h := range s.Hours {
		t := "closed"
		if h.State == "open" {
			t = h.Open + "–" + h.Close
		}
		rows = append(rows, []string{h.Day, t})
	}
	output.Table(w, color, nil, rows)
}

type stockView struct{ *api.StockReport }

func (v stockView) JSONLines() []any {
	out := make([]any, len(v.StockReport.Lines))
	for i, l := range v.StockReport.Lines {
		out[i] = l
	}
	return out
}

func (v stockView) Pretty(w io.Writer, color bool) {
	fmt.Fprintln(w, output.Bold(output.Clean(v.Title), color))
	if len(v.Lines) == 0 {
		fmt.Fprintln(w, "no store holds it")
		return
	}
	rows := [][]string{}
	for _, l := range v.Lines {
		in := "out"
		if l.InStock {
			in = output.Green("in stock", color)
		}
		dist := ""
		if l.Distance > 0 {
			dist = fmt.Sprintf("%.1f km", l.Distance)
		}
		rows = append(rows, []string{l.StoreID, output.Truncate(l.StoreName, 28), output.Truncate(l.City, 20), dist, l.Size, output.Truncate(l.Color, 22), in})
	}
	output.Table(w, color, []string{"ID", "STORE", "CITY", "DISTANCE", "SIZE", "COLOUR", "STOCK"}, rows)
	fmt.Fprintln(w, output.Dim("the site reports presence only, never quantities", color))
}

func (a *App) storeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "store", Short: "Physical stores and which of them hold a product"}

	var near, city string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List stores, optionally nearest first",
		Long: `List Decathlon's stores in Spain from the store locator (cached for a day).
--near sorts by distance from a point; config 'location' is the default.

Examples:
  decathlon store list --format pretty
  decathlon store list --near 40.4168,-3.7038 --limit 5 --format pretty
  decathlon store list --city madrid`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stores, err := a.Client.Stores(cmd.Context())
			if err != nil {
				return err
			}
			lat, lng, ok, err := a.parseNear(near)
			if err != nil {
				return err
			}
			if ok {
				stores = api.Near(stores, lat, lng)
			}
			if city != "" {
				var keep []api.Store
				for _, s := range stores {
					if strings.Contains(strings.ToLower(s.City+" "+s.Province+" "+s.Name), strings.ToLower(city)) {
						keep = append(keep, s)
					}
				}
				stores = keep
			}
			if limit > 0 && len(stores) > limit {
				stores = stores[:limit]
			}
			if stores == nil {
				stores = []api.Store{}
			}
			return a.Printer.Print(storeList(stores))
		},
	}
	list.Flags().StringVar(&near, "near", "", "sort by distance from LAT,LNG")
	list.Flags().StringVar(&city, "city", "", "only stores whose city, province or name contains this")
	list.Flags().IntVar(&limit, "limit", 0, "show at most this many")

	show := &cobra.Command{
		Use:   "show STORE",
		Short: "Show one store's address, phone and opening hours",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stores, err := a.Client.Stores(cmd.Context())
			if err != nil {
				return err
			}
			s, err := api.FindStore(stores, args[0])
			if err != nil {
				return err
			}
			return a.Printer.Print(storeView{s})
		},
	}

	var sf stockFlags
	stock := &cobra.Command{
		Use:   "stock PRODUCT",
		Short: "Show which stores hold each size of a product",
		Long: `Show which physical stores hold a product, per colour and size. PRODUCT is
a product URL or GROUP/MODEL from 'search'.

Without --store, every store that holds a size is listed. With --store
(repeatable, id or name; config 'stores' is the default), only those stores
are checked; --all also prints the sizes they do not hold. The site says
whether a store holds a size, never how many.

Examples:
  decathlon store stock 303295/8513471 --near 40.4168,-3.7038 --limit 10 --format pretty
  decathlon store stock 336529/8668913 --size 41 --store 169 --store Badalona --all
  decathlon store stock 303295/8513471 --format jsonl | jq -r 'select(.in_stock) | .store_name'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := a.stock(cmd, args[0], sf)
			if err != nil {
				return err
			}
			return a.Printer.Print(stockView{rep})
		},
	}
	sf.bind(stock)
	cmd.AddCommand(list, show, stock)
	return cmd
}

type stockFlags struct {
	stores []string
	model  string
	size   string
	near   string
	all    bool
	limit  int
}

func (f *stockFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.stores, "store", nil, "only these stores (id or name), repeatable; default: config stores")
	cmd.Flags().StringVar(&f.model, "model", "", "only this model (colour) id; default: every model")
	cmd.Flags().StringVar(&f.size, "size", "", "only this size, as the site labels it (\"42\", \"M\", \"Talla única\")")
	cmd.Flags().StringVar(&f.near, "near", "", "sort stores by distance from LAT,LNG (default: config location)")
	cmd.Flags().BoolVar(&f.all, "all", false, "with --store, also list sizes the stores do not hold")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "show at most this many lines")
}

// resolveStores turns ids or names into store ids.
func (a *App) resolveStores(stores []api.Store, refs []string) ([]string, error) {
	var ids []string
	for _, r := range refs {
		s, err := api.FindStore(stores, r)
		if err != nil {
			return nil, err
		}
		ids = append(ids, s.ID)
	}
	return ids, nil
}

func (a *App) stock(cmd *cobra.Command, ref string, f stockFlags) (*api.StockReport, error) {
	ctx := cmd.Context()
	stores, err := a.Client.Stores(ctx)
	if err != nil {
		return nil, err
	}
	refs := f.stores
	if len(refs) == 0 {
		refs = a.Cfg.Stores
	}
	ids, err := a.resolveStores(stores, refs)
	if err != nil {
		return nil, err
	}
	lat, lng, ok, err := a.parseNear(f.near)
	if err != nil {
		return nil, err
	}
	if ok {
		stores = api.Near(stores, lat, lng)
	}
	_, model, err := api.ParseRef(ref)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(ref, "http") {
		model = "" // a URL checks every colour; GROUP/MODEL narrows to one
	}
	if f.model != "" {
		model = strings.TrimPrefix(f.model, "m")
	}
	d, err := a.Client.Product(ctx, ref)
	if err != nil {
		return nil, err
	}
	rep, err := api.Stock(d, stores, api.StockQuery{ModelID: model, Size: f.size, Stores: ids, All: f.all})
	if err != nil {
		return nil, err
	}
	if f.limit > 0 && len(rep.Lines) > f.limit {
		rep.Lines = rep.Lines[:f.limit]
	}
	return rep, nil
}
