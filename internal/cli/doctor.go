package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/output"
	"github.com/n0nuser/decathlon-cli/internal/sink"
)

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	OK     bool    `json:"ok"`
	Checks []check `json:"checks"`
}

func (r doctorReport) Pretty(w io.Writer, color bool) {
	rows := [][]string{}
	for _, c := range r.Checks {
		mark := output.Green("ok", color)
		if !c.OK {
			mark = "FAIL"
		}
		rows = append(rows, []string{mark, c.Name, c.Detail})
	}
	output.Table(w, color, nil, rows)
}

func (a *App) doctorCmd() *cobra.Command {
	var mint bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check config, FlareSolverr, the session and that decathlon.es answers",
		Long: `Check that everything a request needs works: config parses, sinks are
valid, FlareSolverr answers, a session exists (or can be minted), and a
plain request with it reaches decathlon.es. --mint forces a new session.
Exit 1 if any check fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rep := doctorReport{OK: true}
			add := func(name string, err error, detail string) {
				c := check{Name: name, OK: err == nil, Detail: detail}
				if err != nil {
					c.Detail, rep.OK = err.Error(), false
				}
				rep.Checks = append(rep.Checks, c)
			}
			add("config", a.configErr, a.Paths.ConfigFile)
			for _, name := range slices.Sorted(maps.Keys(a.Cfg.Sinks)) {
				add("sink "+name, sink.Validate(name, a.Cfg.Sinks[name]), a.Cfg.Sinks[name].Type)
			}
			v, err := a.Client.SolverStatus(ctx)
			add("flaresolverr", err, fmt.Sprintf("%s (v%s)", a.Client.SolverURL, v))
			if mint {
				if err := a.Client.ForgetSession(); err != nil {
					add("session", err, "")
				}
			}
			if s, err := a.Client.StoredSession(); err == nil && !mint {
				add("session", nil, fmt.Sprintf("minted %s, %d cookies", s.MintedAt.Local().Format("2006-01-02 15:04"), len(s.Cookies)))
			} else if v != "" {
				err := a.Client.Mint(ctx)
				add("session", err, "minted a new one through flaresolverr")
			} else {
				add("session", errors.New("none stored, and flaresolverr is down so none can be minted"), "")
			}
			if rep.OK {
				_, err := a.Client.Fetch(ctx, "/es/", 0)
				add("decathlon.es", err, "home page fetched over net/http with the session")
			}
			if err := a.Printer.Print(rep); err != nil {
				return err
			}
			if !rep.OK {
				return errors.New("doctor found problems; see the checks above")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&mint, "mint", false, "discard the stored session and mint a new one")
	return cmd
}
