package cli

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/n0nuser/decathlon-cli/internal/api"
	"github.com/n0nuser/decathlon-cli/internal/output"
)

type productView struct{ *api.ProductDetail }

func (v productView) Pretty(w io.Writer, color bool) {
	fmt.Fprintln(w, output.Bold(output.Clean(v.Title), color))
	line := v.Brand
	if v.Reviews > 0 {
		line += fmt.Sprintf(" · %.1f/5 (%d reviews)", v.Rating, v.Reviews)
	}
	fmt.Fprintln(w, output.Clean(line))
	fmt.Fprintln(w, output.Dim(v.URL, color))
	if v.Description != "" {
		fmt.Fprintln(w, "\n"+output.Clean(v.Description))
	}
	fmt.Fprintln(w)
	rows := [][]string{}
	for _, m := range v.Models {
		price := output.Money(m.Price, m.Currency)
		if m.ReferencePrice > m.Price {
			price += " " + output.Dim("was "+output.Money(m.ReferencePrice, m.Currency), color)
		}
		sel := ""
		if m.Selected {
			sel = "*"
		}
		for _, s := range m.SKUs {
			online := "no"
			if s.AvailableOnline {
				online = "yes"
			}
			rows = append(rows, []string{sel + m.Ref, output.Truncate(strings.Join(m.Colors, "/"), 28), s.Size, price, online, fmt.Sprint(len(s.StoresWithStock))})
		}
	}
	output.Table(w, color, []string{"REF", "COLOUR", "SIZE", "PRICE", "ONLINE", "STORES"}, rows)
	fmt.Fprintln(w, output.Dim("STORES counts stores holding the size; see `decathlon store stock` for which.", color))
}

func (a *App) productCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "product",
		Aliases: []string{"item"},
		Short:   "Read a product page: models, sizes, prices and stock",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show PRODUCT",
		Short: "Show every colour and size of a product, with online and store availability",
		Long: `Show a product. PRODUCT is a product URL, or GROUP/MODEL as printed in the
ID column of 'search' (the product group id and one colour's model id).

Examples:
  decathlon product show 303295/8513471 --format pretty
  decathlon product show https://www.decathlon.es/es/p/tienda-de-campana-2-personas-quechua-mh100/303295/c251c98c74m8513471`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.Client.Product(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.Printer.Print(productView{d})
		},
	}, &cobra.Command{
		Use:   "open PRODUCT",
		Short: "Open the product page in the browser",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := api.ProductPath(args[0])
			if err != nil {
				return err
			}
			return openBrowser(a.Client.Abs(path))
		},
	})
	return cmd
}

func openBrowser(u string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", u)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	return c.Start()
}
