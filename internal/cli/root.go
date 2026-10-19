// Package cli is the cobra command tree. Each noun has its own file;
// handlers parse flags, call internal/api (or the watch store), and print.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/config"
	"github.com/n0nuser/decathlon-cli/internal/output"
	"github.com/n0nuser/decathlon-cli/internal/store"
)

// App holds what one invocation shares between commands.
type App struct {
	Version string
	Paths   config.Paths
	Cfg     config.Config
	Client  *api.Client
	Printer output.Printer

	Stdout, Stderr io.Writer
	Stdin          io.Reader
	// Interactive: stdin, stdout and stderr are TTYs and --no-input is off.
	Interactive bool

	flagFormat      string
	flagNoColor     bool
	flagErrorFormat string
	flagNoInput     bool
	flagDebug       bool

	store *store.Store
}

// Execute runs one invocation and returns its exit code.
func Execute(version string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &App{Version: version, Stdin: stdin, Stdout: stdout, Stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, err := root.ExecuteContextC(ctx)
	if a.store != nil {
		a.store.Close()
	}
	if err == nil {
		a.maybeUpdate(ctx, cmd)
		return 0
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		fmt.Fprintln(stderr, "decathlon: interrupted")
		return 130
	}
	path := "decathlon"
	if cmd != nil {
		path = cmd.CommandPath()
	}
	ef := output.ErrorFormat(a.flagErrorFormat)
	if v := os.Getenv("DECATHLON_ERROR_FORMAT"); v != "" && !root.PersistentFlags().Changed("error-format") {
		ef = output.ErrorFormat(v)
	}
	return output.PrintError(stderr, err, ef, version, path)
}

// CommandTree is the command tree without running it, for cmd/docsgen.
func CommandTree(version string) *cobra.Command {
	return (&App{Version: version}).rootCmd()
}

func (a *App) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "decathlon",
		Short: "Search decathlon.es, check store stock and watch prices from the terminal",
		Long: `Search decathlon.es with every filter the website has, read product pages,
see which physical stores hold each size, and watch products or searches for
price and stock changes. Read-only: no cart, no account, no checkout.

Output is JSON on stdout by default; --format pretty is for humans.
Diagnostics go to stderr. Requests need a local FlareSolverr (see doctor).

Examples:
  decathlon search "tienda campaña" --format pretty
  decathlon search --category deportes/camping/tiendas-campana --store 169 --max-price 60
  decathlon product show 303295/8513471
  decathlon store stock 303295/8513471 --near 40.42,-3.70 --format pretty
  decathlon watch add product 303295/8513471 --store 169 --name tent

Agent usage: decathlon skills get decathlon-usage`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       a.Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.setup(cmd)
		},
	}
	root.SetVersionTemplate("decathlon {{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return output.Usagef("%s. See `%s --help`", err, cmd.CommandPath())
	})
	pf := root.PersistentFlags()
	pf.StringVar(&a.flagFormat, "format", "json", "stdout format: json, jsonl, pretty, toon")
	pf.BoolVar(&a.flagNoColor, "no-color", false, "no colour in pretty output (also NO_COLOR)")
	pf.StringVar(&a.flagErrorFormat, "error-format", "text", "stderr error format: text or json (env DECATHLON_ERROR_FORMAT)")
	pf.BoolVar(&a.flagNoInput, "no-input", false, "never prompt; fail when input is missing")
	pf.BoolVar(&a.flagDebug, "debug", false, "log each request to stderr")
	_ = root.RegisterFlagCompletionFunc("format", cobra.FixedCompletions(output.Formats, cobra.ShellCompDirectiveNoFileComp))
	_ = root.RegisterFlagCompletionFunc("error-format", cobra.FixedCompletions([]string{"text", "json"}, cobra.ShellCompDirectiveNoFileComp))

	root.AddCommand(
		a.searchCmd(), a.productCmd(), a.categoryCmd(), a.storeCmd(),
		a.watchCmd(), a.sinkCmd(),
		a.configCmd(), a.doctorCmd(), a.updateCmd(), a.mcpCmd(), a.skillsCmd(),
	)
	return root
}

// setup reads config and builds the client. No network.
func (a *App) setup(cmd *cobra.Command) error {
	if cmd.Name() == "help" || underCommand(cmd, "completion") {
		return nil
	}
	format, err := output.ParseFormat(a.flagFormat)
	if err != nil {
		return err
	}
	if a.flagErrorFormat != "text" && a.flagErrorFormat != "json" {
		return output.Usagef("--error-format must be text or json")
	}
	out, _ := a.Stdout.(*os.File)
	in, _ := a.Stdin.(*os.File)
	errf, _ := a.Stderr.(*os.File)
	a.Interactive = !a.flagNoInput && output.IsTerminal(in) && output.IsTerminal(out) && output.IsTerminal(errf)
	a.Printer = output.Printer{W: a.Stdout, Format: format, Color: output.ColorEnabled(a.flagNoColor, out)}

	a.Paths = config.DefaultPaths()
	if a.Cfg, err = config.Load(a.Paths.ConfigFile); err != nil && !underCommand(cmd, "config") {
		return err
	}
	c := api.New()
	c.SolverURL = a.Cfg.SolverURL()
	c.SessionFile = a.Paths.SessionFile
	c.CacheDir = a.Paths.CacheDir
	if v := os.Getenv("DECATHLON_BASE_URL"); v != "" {
		c.BaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("DECATHLON_REQUEST_GAP"); v != "" {
		gap, err := time.ParseDuration(v)
		if err != nil {
			return output.Usagef("DECATHLON_REQUEST_GAP: %v", err)
		}
		c.Gap = gap
	}
	if a.flagDebug {
		c.Debug = a.Stderr
	}
	c.Notice = func(s string) { fmt.Fprintln(a.Stderr, "decathlon: "+s) }
	a.Client = c
	return nil
}

func underCommand(cmd *cobra.Command, name string) bool {
	for c := cmd; c != nil && c.HasParent(); c = c.Parent() {
		if c.Name() == name && !c.Parent().HasParent() {
			return true
		}
	}
	return false
}

// Store opens the watch database on first use.
func (a *App) Store() (*store.Store, error) {
	if a.store == nil {
		st, err := store.Open(a.Paths.StateDB)
		if err != nil {
			return nil, err
		}
		a.store = st
	}
	return a.store, nil
}

// confirm asks on a TTY; elsewhere --yes is required.
func (a *App) confirm(yes bool, prompt string) error {
	if yes {
		return nil
	}
	if !a.Interactive {
		return output.Usagef("%s Pass --yes to confirm", prompt)
	}
	fmt.Fprintf(a.Stderr, "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	if s := strings.ToLower(strings.TrimSpace(line)); s != "y" && s != "yes" {
		return output.Usagef("cancelled")
	}
	return nil
}
