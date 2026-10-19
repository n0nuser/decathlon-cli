package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/output"
	"github.com/n0nuser/decathlon-cli/internal/sink"
	"github.com/n0nuser/decathlon-cli/internal/store"
	"github.com/n0nuser/decathlon-cli/internal/watch"
)

const (
	defaultInterval = 30 * time.Minute
	minInterval     = 5 * time.Minute // each check is real page loads on a real shop
)

func (a *App) watchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch products and searches for price and stock changes",
		Long: `Watch a product (price, online availability, per-store stock) or a search
(new products, price changes). Watches live in a local database. Each check
prints the events it found and sends them to the watch's sinks.

Event types: ` + strings.Join(watch.Types, ", ") + `.

Examples:
  decathlon watch add product 303295/8513471 --store 169 --name tent --notify phone
  decathlon watch add search "hornillo gas" --max-price 30 --name stoves
  decathlon watch check --format jsonl | jq -r 'select(.type=="store.in_stock") | .store.name'
  decathlon watch run --interval 30m`,
	}
	add := &cobra.Command{Use: "add", Short: "Create a watch"}
	add.AddCommand(a.watchAddSearchCmd(), a.watchAddProductCmd())
	cmd.AddCommand(add, a.watchListCmd(), a.watchRemoveCmd(), a.watchCheckCmd(), a.watchRunCmd(), a.watchEventsCmd())
	return cmd
}

type watchAddFlags struct {
	name        string
	notify      []string
	interval    time.Duration
	emitInitial bool
}

func (f *watchAddFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.name, "name", "", "watch name (letters, digits, - and _), required")
	cmd.Flags().StringArrayVar(&f.notify, "notify", nil, "sink from config to deliver events to, repeatable")
	cmd.Flags().DurationVar(&f.interval, "interval", 0, "how often `watch check` considers it due (default: config watch.interval or 30m, minimum 5m)")
	cmd.Flags().BoolVar(&f.emitInitial, "emit-initial", false, "report what exists now as events, instead of a silent baseline")
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (a *App) interval(flag time.Duration) (time.Duration, error) {
	iv := flag
	if iv == 0 && a.Cfg.Watch.Interval != "" {
		d, err := time.ParseDuration(a.Cfg.Watch.Interval)
		if err != nil {
			return 0, output.Usagef("config watch.interval: %v", err)
		}
		iv = d
	}
	if iv == 0 {
		iv = defaultInterval
	}
	if iv < minInterval {
		return 0, output.Usagef("interval %s is below the %s minimum", iv, minInterval)
	}
	return iv, nil
}

func (a *App) addWatch(ctx context.Context, kind string, target any, f watchAddFlags) error {
	if !nameRE.MatchString(f.name) {
		return output.Usagef("--name is required: letters, digits, - and _, up to 64")
	}
	for _, s := range f.notify {
		sc, ok := a.Cfg.Sinks[s]
		if !ok {
			return output.Usagef("no sink %q in config (see `decathlon sink list`)", s)
		}
		if err := sink.Validate(s, sc); err != nil {
			return output.Usagef("%v", err)
		}
	}
	iv, err := a.interval(f.interval)
	if err != nil {
		return err
	}
	st, err := a.Store()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(target)
	w := store.Watch{Name: f.name, Kind: kind, Target: raw, Sinks: f.notify, Interval: store.Duration(iv)}
	if w.Sinks == nil {
		w.Sinks = []string{}
	}
	// Check before saving, so a target that does not resolve (unknown
	// product, bad filter) is refused instead of stored broken.
	res, err := watch.Check(ctx, a.Client, st, w, f.emitInitial)
	if err != nil {
		return err
	}
	if err := st.AddWatch(ctx, &w); err != nil {
		if errors.Is(err, store.ErrExists) {
			return output.Usagef("a watch named %q exists; remove it first or pick another name", f.name)
		}
		return err
	}
	a.deliver(ctx, w, res.Events)
	if err := st.CommitCheck(ctx, w, res.Upserts, res.Removed, res.Events); err != nil {
		return err
	}
	return a.Printer.Print(map[string]any{"watch": w, "events": eventList(res.Events)})
}

func (a *App) watchAddSearchCmd() *cobra.Command {
	var sf searchFlags
	var wf watchAddFlags
	var pages int
	cmd := &cobra.Command{
		Use:   "search [keywords...]",
		Short: "Watch a search or category for new products and price changes",
		Long: `Watch a search. It takes the same keywords and filters as 'search', and
reports product.new and product.price_changed across the first --pages pages.

Examples:
  decathlon watch add search "hornillo gas" --max-price 30 --name stoves
  decathlon watch add search --category deportes/camping/tiendas-campana --store 169 --name tents-pamplona`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := sf.params(args)
			if err != nil {
				return err
			}
			if pages < 1 || pages > 5 {
				return output.Usagef("--pages must be 1 to 5")
			}
			return a.addWatch(cmd.Context(), store.KindSearch, watch.SearchTarget{Params: p, Pages: pages}, wf)
		},
	}
	a.bindSearchFlags(cmd, &sf)
	cmd.Flags().IntVar(&pages, "pages", 1, "result pages to watch (1-5)")
	wf.bind(cmd)
	return cmd
}

func (a *App) watchAddProductCmd() *cobra.Command {
	var wf watchAddFlags
	var stores []string
	var model, size string
	cmd := &cobra.Command{
		Use:   "product PRODUCT",
		Short: "Watch a product's price, online availability and store stock",
		Long: `Watch a product. PRODUCT is a URL or GROUP/MODEL (GROUP/MODEL narrows it to
that colour). Reports product.price_changed, sku.new, sku.back_online,
sku.sold_out_online, store.in_stock, store.out_of_stock and product.removed.

--store limits store events to those stores (config 'stores' is the
default); with neither, every store's changes are reported.

Examples:
  decathlon watch add product 303295/8513471 --store 169 --name tent
  decathlon watch add product 336529/8668913 --size 42 --store Badalona --name boots --notify phone`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, refModel, err := api.ParseRef(args[0])
			if err != nil {
				return err
			}
			if strings.HasPrefix(args[0], "http") {
				refModel = ""
			}
			t := watch.ProductTarget{Ref: args[0], Model: strings.TrimPrefix(model, "m"), Size: size}
			if t.Model == "" {
				t.Model = refModel
			}
			refs := stores
			if len(refs) == 0 {
				refs = a.Cfg.Stores
			}
			if len(refs) > 0 {
				all, err := a.Client.Stores(cmd.Context())
				if err != nil {
					return err
				}
				if t.Stores, err = a.resolveStores(all, refs); err != nil {
					return err
				}
			}
			return a.addWatch(cmd.Context(), store.KindProduct, t, wf)
		},
	}
	cmd.Flags().StringArrayVar(&stores, "store", nil, "stores whose stock to report (id or name), repeatable")
	cmd.Flags().StringVar(&model, "model", "", "only this model (colour) id")
	cmd.Flags().StringVar(&size, "size", "", "only this size")
	wf.bind(cmd)
	return cmd
}

type watchList []store.Watch

func (l watchList) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, x := range l {
		last := "never"
		if !x.LastCheckAt.IsZero() {
			last = x.LastCheckAt.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{x.Name, x.Kind, output.Truncate(describe(x), 60), x.Interval.String(), strings.Join(x.Sinks, ","), last})
	}
	output.Table(w, color, []string{"NAME", "KIND", "TARGET", "EVERY", "SINKS", "LAST CHECK"}, rows)
}

func describe(w store.Watch) string {
	switch w.Kind {
	case store.KindSearch:
		var t watch.SearchTarget
		_ = json.Unmarshal(w.Target, &t)
		s := t.Params.Query
		if t.Params.Category != "" {
			s = "category " + t.Params.Category
		}
		for _, f := range t.Params.Filters {
			s += " " + f.Key + "=" + f.Value
		}
		return s
	case store.KindProduct:
		var t watch.ProductTarget
		_ = json.Unmarshal(w.Target, &t)
		s := t.Ref
		if t.Size != "" {
			s += " size " + t.Size
		}
		if len(t.Stores) > 0 {
			s += " stores " + strings.Join(t.Stores, ",")
		}
		return s
	}
	return string(w.Target)
}

func (a *App) watchListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List watches", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.Store()
			if err != nil {
				return err
			}
			ws, err := st.Watches(cmd.Context())
			if err != nil {
				return err
			}
			return a.Printer.Print(watchList(ws))
		},
	}
}

func (a *App) watchRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "remove NAME", Short: "Delete a watch and its event history", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.Store()
			if err != nil {
				return err
			}
			if _, err := st.Watch(cmd.Context(), args[0]); err != nil {
				return &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no watch %q", args[0])}
			}
			if err := a.confirm(yes, fmt.Sprintf("Delete watch %q and its events?", args[0])); err != nil {
				return err
			}
			if err := st.RemoveWatch(cmd.Context(), args[0]); err != nil {
				return err
			}
			return a.Printer.Print(map[string]string{"removed": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return cmd
}

type eventList []store.Event

func (l eventList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]store.Event(l))
}

func (l eventList) Pretty(w io.Writer, color bool) {
	if len(l) == 0 {
		fmt.Fprintln(w, output.Dim("no changes", color))
		return
	}
	rows := [][]string{}
	for _, e := range l {
		title, body, _ := sink.Text(e)
		rows = append(rows, []string{e.At.Local().Format("01-02 15:04"), e.Watch, e.Type, output.Truncate(title, 70), output.Truncate(body, 30)})
	}
	output.Table(w, color, []string{"AT", "WATCH", "TYPE", "WHAT", ""}, rows)
}

// deliver sends events to each watch sink; failures are notices, not errors.
func (a *App) deliver(ctx context.Context, w store.Watch, evs []store.Event) {
	for _, name := range w.Sinks {
		sc, ok := a.Cfg.Sinks[name]
		if !ok {
			fmt.Fprintf(a.Stderr, "decathlon: watch %s: sink %q is not in config any more\n", w.Name, name)
			continue
		}
		for _, e := range evs {
			if err := sink.Deliver(ctx, name, sc, e); err != nil {
				fmt.Fprintf(a.Stderr, "decathlon: watch %s: sink %s: %v\n", w.Name, name, err)
			}
		}
	}
}

// runChecks checks each watch in turn. Events are printed and delivered
// before state is committed, so a crash re-reports rather than loses. A
// failing watch is reported and the others still run; the command fails at
// the end if any did.
func (a *App) runChecks(ctx context.Context, st *store.Store, ws []store.Watch) ([]store.Event, error) {
	var all []store.Event
	var failed []string
	var lastErr error
	for _, w := range ws {
		res, err := watch.Check(ctx, a.Client, st, w, false)
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			fmt.Fprintf(a.Stderr, "decathlon: watch %s: %v\n", w.Name, err)
			failed = append(failed, w.Name)
			lastErr = err
			continue
		}
		a.deliver(ctx, w, res.Events)
		if err := st.CommitCheck(ctx, w, res.Upserts, res.Removed, res.Events); err != nil {
			return all, err
		}
		all = append(all, res.Events...)
	}
	if len(failed) > 0 {
		var ae *api.Error
		if len(failed) == len(ws) && errors.As(lastErr, &ae) {
			return all, lastErr
		}
		return all, fmt.Errorf("%d of %d watches failed: %s", len(failed), len(ws), strings.Join(failed, ", "))
	}
	return all, nil
}

func (a *App) selectWatches(ctx context.Context, st *store.Store, names []string, force bool) ([]store.Watch, error) {
	ws, err := st.Watches(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) > 0 {
		var out []store.Watch
		for _, n := range names {
			found := false
			for _, w := range ws {
				if w.Name == n {
					out, found = append(out, w), true
				}
			}
			if !found {
				return nil, &api.Error{Kind: api.KindNotFound, Msg: fmt.Sprintf("no watch %q", n)}
			}
		}
		return out, nil
	}
	if force {
		return ws, nil
	}
	var due []store.Watch
	now := time.Now()
	for _, w := range ws {
		if w.LastCheckAt.IsZero() || now.Sub(w.LastCheckAt) >= time.Duration(w.Interval)-time.Minute {
			due = append(due, w)
		}
	}
	return due, nil
}

func (a *App) watchCheckCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "check [NAME...]",
		Short: "Check due watches once and print their events",
		Long: `Check the named watches, or every watch that is due (--all: every watch).
Prints the events (a JSON array; one per line with --format jsonl), sends
them to sinks, and stores the new state. Exit 0 when nothing changed.

Run it from cron or a systemd timer:
  */30 * * * * decathlon watch check --format jsonl >> ~/decathlon-events.jsonl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.Store()
			if err != nil {
				return err
			}
			ws, err := a.selectWatches(cmd.Context(), st, args, all)
			if err != nil {
				return err
			}
			evs, err := a.runChecks(cmd.Context(), st, ws)
			if perr := a.Printer.Print(eventList(evs)); perr != nil {
				return perr
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check every watch, due or not")
	return cmd
}

func (a *App) watchRunCmd() *cobra.Command {
	var every time.Duration
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Check due watches in a loop until interrupted",
		Long: `Run due checks in the foreground, waking every --interval (plus up to 10%
jitter). Events stream to stdout one per line. Stop with Ctrl-C.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if every < minInterval {
				return output.Usagef("--interval must be at least %s", minInterval)
			}
			st, err := a.Store()
			if err != nil {
				return err
			}
			p := a.Printer
			if p.Format == output.JSON {
				p.Format = output.JSONL
			}
			ctx := cmd.Context()
			for {
				ws, err := a.selectWatches(ctx, st, nil, false)
				if err != nil {
					return err
				}
				evs, err := a.runChecks(ctx, st, ws)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil {
					fmt.Fprintf(a.Stderr, "decathlon: %v\n", err)
				}
				for _, e := range evs {
					_ = p.Print(e)
				}
				wait := every + watch.Jitter(every, rand.Int64())
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
		},
	}
	cmd.Flags().DurationVar(&every, "interval", defaultInterval, "how often to wake up")
	return cmd
}

func (a *App) watchEventsCmd() *cobra.Command {
	var since time.Duration
	var limit int
	cmd := &cobra.Command{
		Use: "events [NAME]", Short: "Show stored events, newest first", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.Store()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			from := time.Time{}
			if since > 0 {
				from = time.Now().Add(-since)
			}
			evs, err := st.Events(cmd.Context(), name, from, limit)
			if err != nil {
				return err
			}
			return a.Printer.Print(eventList(evs))
		},
	}
	cmd.Flags().DurationVar(&since, "since", 0, "only events newer than this (e.g. 24h)")
	cmd.Flags().IntVar(&limit, "limit", 50, "at most this many (0: all)")
	return cmd
}

// sinks

type sinkRow struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Problem string `json:"problem,omitempty"`
}

type sinkList []sinkRow

func (l sinkList) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, s := range l {
		rows = append(rows, []string{s.Name, s.Type, output.Truncate(s.Target, 50), s.Problem})
	}
	output.Table(w, color, []string{"SINK", "TYPE", "TARGET", "PROBLEM"}, rows)
}

func (a *App) sinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sink",
		Short: "Notification targets declared in config",
		Long: `Sinks are declared in config.toml and named by watches with --notify:

  [sinks.phone]
  type = "ntfy"
  url = "https://ntfy.sh"
  topic = "my-decathlon-alerts"

  [sinks.discord]
  type = "webhook"
  url = "https://discord.com/api/webhooks/..."
  template = "discord"

  [sinks.script]
  type = "exec"
  command = ["/home/me/bin/on-decathlon-event"]   # event JSON on stdin`,
	}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List sinks and config problems", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			l := sinkList{}
			for _, name := range sortedNames(a.Cfg.Sinks) {
				s := a.Cfg.Sinks[name]
				r := sinkRow{Name: name, Type: s.Type, Target: s.URL}
				if s.Type == "ntfy" {
					r.Target = strings.TrimRight(s.URL, "/") + "/" + s.Topic
				}
				if s.Type == "exec" {
					r.Target = strings.Join(s.Command, " ")
				}
				if err := sink.Validate(name, s); err != nil {
					r.Problem = err.Error()
				}
				l = append(l, r)
			}
			return a.Printer.Print(l)
		},
	}, &cobra.Command{
		Use: "test NAME", Short: "Send a sample event to a sink", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, ok := a.Cfg.Sinks[args[0]]
			if !ok {
				return output.Usagef("no sink %q in config", args[0])
			}
			p, _ := json.Marshal(watch.ProductRef{ID: "303295", ModelID: "8513471", Title: "Sample product (sink test)", Price: 24.99, Currency: "EUR", URL: a.Client.Abs("/es/p/_/303295/m8513471")})
			st, _ := json.Marshal(watch.StoreRef{ID: "169", Name: "Pamplona"})
			e := store.Event{Type: watch.StoreInStock, Watch: "sink-test", At: time.Now().UTC(), Product: p, Store: st}
			if err := sink.Deliver(cmd.Context(), args[0], s, e); err != nil {
				return err
			}
			return a.Printer.Print(map[string]any{"sink": args[0], "sent": true})
		},
	})
	return cmd
}

func sortedNames(m map[string]config.SinkConfig) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
