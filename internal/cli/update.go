package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
	"github.com/n0nuser/decathlon-cli/internal/updater"
)

func (a *App) updateCmd() *cobra.Command {
	var yes, check bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update decathlon to the latest stable release",
		Long: `Check GitHub for the latest stable release and replace this binary with it,
after verifying the archive against the release's checksums.txt. --check only
reports. Development builds and package-manager installs are not replaced.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !updater.Stable(a.Version) {
				return output.Usagef("this is a development build (%s); install a release to use update", a.Version)
			}
			r, err := updater.Latest(cmd.Context(), updater.Client())
			if err != nil {
				return &api.Error{Kind: api.KindNetwork, Msg: "checking for updates: " + err.Error()}
			}
			avail := updater.Newer(r.Tag, a.Version)
			if check || !avail {
				return a.Printer.Print(map[string]any{"current_version": a.Version, "latest_version": strings.TrimPrefix(r.Tag, "v"), "update_available": avail})
			}
			if !yes {
				if !a.Interactive {
					return output.Usagef("%s is available; run `decathlon update --yes`", r.Tag)
				}
				if !a.ask(fmt.Sprintf("decathlon %s is available (you have %s). Update? [Y/n] ", strings.TrimPrefix(r.Tag, "v"), a.Version)) {
					return nil
				}
			}
			return a.install(cmd.Context(), r)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "update without asking")
	cmd.Flags().BoolVar(&check, "check", false, "only report whether an update exists")
	return cmd
}

func (a *App) ask(prompt string) bool {
	fmt.Fprint(a.Stderr, prompt)
	line, _ := bufio.NewReader(a.Stdin).ReadString('\n')
	s := strings.ToLower(strings.TrimSpace(line))
	return s == "" || s == "y" || s == "yes"
}

func (a *App) install(ctx context.Context, r updater.Release) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	if pm := updater.ManagedBy(exe); pm != "" {
		return output.Usagef("this copy is managed by %s; update it there", pm)
	}
	if err := updater.Install(ctx, updater.Client(), r, exe); err != nil {
		return err
	}
	return a.Printer.Print(map[string]any{"updated_to": strings.TrimPrefix(r.Tag, "v"), "path": exe})
}

// maybeUpdate offers an update at most once a day, only to people at a
// terminal, never inside scripts, MCP, watches or `update` itself.
func (a *App) maybeUpdate(ctx context.Context, cmd *cobra.Command) {
	if cmd == nil || !a.Interactive || os.Getenv("DECATHLON_NO_UPDATE_CHECK") != "" || !updater.Stable(a.Version) || a.Paths.StateDir == "" {
		return
	}
	for _, skip := range []string{"update", "watch", "mcp", "completion", "help"} {
		if underCommand(cmd, skip) || cmd.Name() == skip {
			return
		}
	}
	stamp := filepath.Join(a.Paths.StateDir, "update-check")
	if fi, err := os.Stat(stamp); err == nil && time.Since(fi.ModTime()) < 24*time.Hour {
		return
	}
	_ = os.MkdirAll(a.Paths.StateDir, 0o700)
	_ = os.WriteFile(stamp, nil, 0o600)
	_ = os.Chtimes(stamp, time.Now(), time.Now())
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := updater.Latest(ctx, updater.Client())
	if err != nil || !updater.Newer(r.Tag, a.Version) {
		return
	}
	if a.ask(fmt.Sprintf("decathlon %s is available (you have %s). Update? [Y/n] ", strings.TrimPrefix(r.Tag, "v"), a.Version)) {
		if err := a.install(context.Background(), r); err != nil {
			fmt.Fprintln(a.Stderr, "decathlon: update failed:", err)
		}
	}
}
