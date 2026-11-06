package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Export workspace token usage",
}

var usageExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export exact workspace token usage as JSON",
	Args:  cobra.NoArgs,
	RunE:  runUsageExport,
}

type usageExportItem struct {
	Day                      string `json:"day,omitempty"`
	AgentID                  string `json:"agent_id,omitempty"`
	AgentName                string `json:"agent_name,omitempty"`
	Provider                 string `json:"provider,omitempty"`
	Model                    string `json:"model,omitempty"`
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadTokens          int64  `json:"cache_read_tokens"`
	CacheWriteTokens         int64  `json:"cache_write_tokens"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
}

type usageExportPage struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Timezone   string            `json:"timezone"`
	GroupBy    []string          `json:"group_by"`
	Items      []usageExportItem `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

func init() {
	usageCmd.AddCommand(usageExportCmd)
	f := usageExportCmd.Flags()
	f.String("from", "", "Interval start (RFC3339 or YYYY-MM-DD; required)")
	f.String("to", "", "Exclusive interval end (RFC3339 or YYYY-MM-DD; required)")
	f.String("timezone", "UTC", "IANA timezone used for date-only bounds and day grouping")
	f.StringArray("agent", nil, "Agent ID or exact name to include (repeatable)")
	f.String("runtime", "", "Only include usage from this runtime UUID")
	f.String("project", "", "Only include usage from this project UUID")
	f.String("group-by", "agent,provider,model,day", "Comma-separated dimensions: agent,provider,model,day")
	f.String("output", "json", "Output format (json)")
}

func runUsageExport(cmd *cobra.Command, _ []string) error {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	if from == "" || to == "" {
		return fmt.Errorf("--from and --to are required")
	}
	output, _ := cmd.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("--output must be json")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("from", from)
	q.Set("to", to)
	tz, _ := cmd.Flags().GetString("timezone")
	q.Set("timezone", tz)
	groupBy, _ := cmd.Flags().GetString("group-by")
	q.Set("group_by", groupBy)
	agents, _ := cmd.Flags().GetStringArray("agent")
	for _, agent := range agents {
		q.Add("agent", agent)
	}
	if runtimeID, _ := cmd.Flags().GetString("runtime"); runtimeID != "" {
		q.Set("runtime_id", runtimeID)
	}
	if projectID, _ := cmd.Flags().GetString("project"); projectID != "" {
		q.Set("project_id", projectID)
	}
	q.Set("page_size", "1000")

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	result := usageExportPage{Items: []usageExportItem{}}
	for {
		var page usageExportPage
		if err := client.GetJSON(ctx, "/api/usage/export?"+q.Encode(), &page); err != nil {
			return fmt.Errorf("export usage: %w", err)
		}
		if result.From == "" {
			result.From, result.To = page.From, page.To
			result.Timezone, result.GroupBy = page.Timezone, page.GroupBy
		} else if result.From != page.From || result.To != page.To || result.Timezone != page.Timezone {
			return fmt.Errorf("export usage: pagination metadata changed")
		}
		result.Items = append(result.Items, page.Items...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		q.Set("cursor", *page.NextCursor)
	}
	result.NextCursor = nil
	return cli.PrintJSON(os.Stdout, result)
}
