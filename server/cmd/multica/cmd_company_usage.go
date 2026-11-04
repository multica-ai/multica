package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var companyUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show the runs and tokens a request and its child issues have spent",
	Long: `Show what a request cost: the agent runs and tokens spent on the request
issue and on its direct children. Read-only.

Pass the request issue with --parent <issue-id>. Cost is reported in USD ticks as
the server stores it; --output json prints the server response unchanged.`,
	Args: cobra.NoArgs,
	RunE: runCompanyUsage,
}

func init() {
	companyUsageCmd.Flags().String("parent", "", "The request issue (id or identifier)")
	companyUsageCmd.Flags().String("output", "table", "Output format: table or json")
	_ = companyUsageCmd.MarkFlagRequired("parent")
	companyCmd.AddCommand(companyUsageCmd)
}

// requestUsageView mirrors the JSON of GET /api/company/requests/{id}/usage.
type requestUsageView struct {
	RequestID        string `json:"request_id"`
	RunCount         int64  `json:"run_count"`
	MeteredRunCount  int64  `json:"metered_run_count"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	CostUsdTicks     int64  `json:"cost_usd_ticks"`
}

func runCompanyUsage(cmd *cobra.Command, _ []string) error {
	parent, _ := cmd.Flags().GetString("parent")
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return fmt.Errorf("--parent is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var raw json.RawMessage
	if err := client.GetJSON(ctx, "/api/company/requests/"+parent+"/usage", &raw); err != nil {
		return fmt.Errorf("read request usage: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, raw)
	}
	var view requestUsageView
	if err := json.Unmarshal(raw, &view); err != nil {
		return fmt.Errorf("read request usage: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "RUNS\tMETERED\tINPUT\tOUTPUT\tCACHE READ\tCACHE WRITE\tCOST (USD TICKS)")
	fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
		view.RunCount, view.MeteredRunCount, view.InputTokens, view.OutputTokens,
		view.CacheReadTokens, view.CacheWriteTokens, view.CostUsdTicks)
	return w.Flush()
}
