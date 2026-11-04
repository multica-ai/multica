package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica metric` lists metrics and reports readings (spec 4.1). A reading is a
// manual report at a scope: company, floor:<id>, squad:<id> or initiative:<id>.
// The server decides who may report and checks the scope.
var metricCmd = &cobra.Command{
	Use:   "metric",
	Short: "List metrics and report readings",
}

var metricListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the workspace's metrics, built-in ones included",
	Args:  cobra.NoArgs,
	RunE:  runMetricList,
}

var metricReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Report one reading of a manual metric",
	Long: "Report one reading of a metric at a scope. The first report of a key creates a manual metric.\n" +
		"Built-in metrics are computed by the company and refuse reports.",
	Example: "  multica metric report --key mrr --scope initiative:<id> --value 1200 --note \"March close\"",
	Args:    cobra.NoArgs,
	RunE:    runMetricReport,
}

func init() {
	metricListCmd.Flags().String("output", "table", "Output format: table or json")
	metricReportCmd.Flags().String("key", "", "Metric key, for example mrr or runs.count — required")
	metricReportCmd.Flags().String("scope", "company", "Scope: company, floor:<id>, squad:<id> or initiative:<id>")
	metricReportCmd.Flags().Float64("value", 0, "The value observed — required")
	metricReportCmd.Flags().String("note", "", "A note for the reading")
	metricReportCmd.Flags().String("observed-at", "", "When the value was observed, RFC 3339 (default now)")
	metricReportCmd.Flags().String("output", "table", "Output format: table or json")
	_ = metricReportCmd.MarkFlagRequired("key")
	_ = metricReportCmd.MarkFlagRequired("value")

	metricCmd.AddCommand(metricListCmd)
	metricCmd.AddCommand(metricReportCmd)
}

// parseMetricScope reads --scope: "company", or a scope type and an object id
// separated by a colon. The id is checked by the server.
func parseMetricScope(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "company" || raw == "" {
		return "company", "", nil
	}
	scopeType, id, ok := strings.Cut(raw, ":")
	if !ok || strings.TrimSpace(id) == "" {
		return "", "", fmt.Errorf("--scope must be company, or floor:<id>, squad:<id> or initiative:<id>")
	}
	switch scopeType {
	case "floor", "squad", "initiative":
		return scopeType, strings.TrimSpace(id), nil
	}
	return "", "", fmt.Errorf("--scope type must be floor, squad or initiative, not %q", scopeType)
}

func runMetricList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var out struct {
		Metrics []map[string]any `json:"metrics"`
	}
	if err := client.GetJSON(ctx, "/api/metrics", &out); err != nil {
		return fmt.Errorf("list metrics: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, out)
	}
	if len(out.Metrics) == 0 {
		fmt.Fprintln(os.Stderr, "No metrics found.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tNAME\tUNIT\tSOURCE\tDIRECTION")
	for _, m := range out.Metrics {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", strVal(m, "key"), strVal(m, "name"), strVal(m, "unit"), strVal(m, "source"), strVal(m, "direction"))
	}
	return w.Flush()
}

func runMetricReport(cmd *cobra.Command, _ []string) error {
	key, _ := cmd.Flags().GetString("key")
	scopeRaw, _ := cmd.Flags().GetString("scope")
	value, _ := cmd.Flags().GetFloat64("value")
	note, _ := cmd.Flags().GetString("note")
	observed, _ := cmd.Flags().GetString("observed-at")
	scopeType, scopeID, err := parseMetricScope(scopeRaw)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("--key is required")
	}
	body := map[string]any{"scope_type": scopeType, "value": value}
	if scopeID != "" {
		body["scope_id"] = scopeID
	}
	if note != "" {
		body["note"] = note
	}
	if observed != "" {
		if _, err := time.Parse(time.RFC3339, observed); err != nil {
			return fmt.Errorf("--observed-at must be an RFC 3339 time: %w", err)
		}
		body["observed_at"] = observed
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/metrics/"+url.PathEscape(key)+"/readings", body, &result); err != nil {
		return fmt.Errorf("report reading: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	reading, _ := result["reading"].(map[string]any)
	fmt.Printf("Reported %s = %s at %s (%s)\n", key, strconv.FormatFloat(value, 'f', -1, 64), scopeRaw, strVal(reading, "observed_at"))
	return nil
}
