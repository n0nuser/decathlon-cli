package cli

import (
	"embed"
	"io/fs"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/output"
)

//go:embed skills/*.md
var skillFiles embed.FS

func (a *App) skillsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "skills", Short: "Embedded usage notes for agents"}
	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "List the notes", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.Printer.Print(skillNames()) },
	}, &cobra.Command{
		Use: "get NAME", Short: "Print one note as markdown", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := skillFiles.ReadFile("skills/" + args[0] + ".md")
			if err != nil {
				return output.Usagef("no skill %q; available: %s", args[0], strings.Join(skillNames(), ", "))
			}
			_, err = a.Stdout.Write(raw)
			return err
		},
	})
	return cmd
}

func skillNames() []string {
	entries, _ := fs.ReadDir(skillFiles, "skills")
	names := []string{}
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	slices.Sort(names)
	return names
}
