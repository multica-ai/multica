package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage agent runtimes",
}

var runtimeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agent runtimes",
	RunE:  runRuntimeList,
}

func init() {
	runtimeCmd.AddCommand(runtimeListCmd)

	runtimeListCmd.Flags().String("output", "table", "Output format: table or json")
}

func runRuntimeList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var runtimes []map[string]any
	if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runtimes)
	}

	headers := []string{"ID", "NAME", "PROVIDER", "STATUS", "DEVICE"}
	rows := make([][]string, 0, len(runtimes))
	for _, r := range runtimes {
		rows = append(rows, []string{
			strVal(r, "id"),
			strVal(r, "name"),
			strVal(r, "provider"),
			strVal(r, "status"),
			strVal(r, "device_info"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
