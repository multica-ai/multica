package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica squad directory` lists the squads a squad may ask for work (spec 3.5):
// what each takes on, how long it takes to answer, and its open requests.
var squadDirectoryCmd = &cobra.Command{
	Use:   "directory",
	Short: "List squads with their mission, intake, response time and open requests",
	Args:  cobra.NoArgs,
	RunE:  runSquadDirectory,
}

func init() {
	squadDirectoryCmd.Flags().String("output", "table", "Output format: table or json")
}

func runSquadDirectory(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var out struct {
		Squads []map[string]any `json:"squads"`
	}
	if err := client.GetJSON(ctx, "/api/squads/directory", &out); err != nil {
		return fmt.Errorf("squad directory: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, out)
	}
	if len(out.Squads) == 0 {
		fmt.Fprintln(os.Stderr, "No squads found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tFLOOR\tRESPONDS IN\tOPEN REQUESTS\tINTAKE")
	for _, s := range out.Squads {
		hours, _ := s["response_hours"].(float64)
		fmt.Fprintf(w, "%s\t%s\t%s\t%sh\t%s\t%s\n",
			strVal(s, "id"), strVal(s, "name"), strVal(s, "floor_name"),
			strconv.Itoa(int(hours)), fmt.Sprint(s["open_requests"]), strVal(s, "intake"))
	}
	return w.Flush()
}
