package cli

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/output"
	"github.com/n0nuser/decathlon-cli/internal/sink"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

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
			for _, name := range slices.Sorted(maps.Keys(a.Cfg.Sinks)) {
				s := a.Cfg.Sinks[name]
				r := sinkRow{Name: name, Type: s.Type, Target: sink.Target(s)}
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
			p, _ := json.Marshal(store.ProductRef{ID: "303295", ModelID: "8513471", Title: "Sample product (sink test)", Price: 24.99, Currency: "EUR", URL: a.Client.Abs("/es/p/_/303295/m8513471")})
			st, _ := json.Marshal(store.StoreRef{ID: "169", Name: "Pamplona"})
			e := store.Event{Type: store.StoreInStock, Watch: "sink-test", At: time.Now().UTC(), Product: p, Store: st}
			if err := sink.Deliver(cmd.Context(), args[0], s, e); err != nil {
				return err
			}
			return a.Printer.Print(map[string]any{"sink": args[0], "sent": true})
		},
	})
	return cmd
}
