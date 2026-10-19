package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/mcp"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

func (a *App) mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the read commands to an agent over MCP (stdio)",
		Long: `Serve search, product, store, category and watch tools over the Model
Context Protocol on stdin/stdout. Each tool runs the matching command, so its
output is the command's JSON. Register it with 'decathlon mcp install'.

Tools: ` + toolNames() + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := &mcp.Server{Version: a.Version, Run: a.runInProcess}
			return s.Serve(cmd.Context(), a.Stdin, a.Stdout)
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:       "install HARNESS",
		Short:     "Register `decathlon mcp` with claude-code, codex or cursor",
		Args:      cobra.ExactArgs(1),
		ValidArgs: mcp.Harnesses,
		RunE: func(cmd *cobra.Command, args []string) error {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			res, err := mcp.Install(args[0], exe)
			if err != nil {
				return output.Usagef("%v", err)
			}
			return a.Printer.Print(res)
		},
	})
	return cmd
}

func toolNames() string {
	var n []string
	for _, t := range mcp.Tools {
		n = append(n, t.Name)
	}
	return strings.Join(n, ", ")
}

// runInProcess runs one CLI invocation with JSON output and JSON errors,
// re-reading config each time so a long-lived server sees edits.
func (a *App) runInProcess(ctx context.Context, argv []string) mcp.Result {
	var out, errb bytes.Buffer
	args := append([]string{"--format", "json", "--error-format", "json", "--no-input"}, argv...)
	if a.flagDebug {
		args = append([]string{"--debug"}, args...)
	}
	sub := &App{Version: a.Version, Stdin: bytes.NewReader(nil), Stdout: &out, Stderr: &errb}
	root := sub.rootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errb)
	cmd, err := root.ExecuteContextC(ctx)
	if sub.store != nil {
		sub.store.Close()
	}
	r := mcp.Result{Stdout: out.String()}
	if err != nil {
		path := "decathlon"
		if cmd != nil {
			path = cmd.CommandPath()
		}
		var env bytes.Buffer
		output.PrintError(&env, err, output.ErrJSON, a.Version, path)
		r.ErrorJSON = strings.TrimSpace(env.String())
	}
	return r
}
