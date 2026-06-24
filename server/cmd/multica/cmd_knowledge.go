package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var knowledgeCmd = &cobra.Command{
	Use:   "knowledge",
	Short: "Search workspace knowledge",
}

var knowledgeSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search reviewed workspace knowledge",
	Args:  exactArgs(1),
	RunE:  runKnowledgeSearch,
}

func init() {
	knowledgeCmd.AddCommand(knowledgeSearchCmd)

	knowledgeSearchCmd.Flags().String("issue", "", "Issue ID or identifier used to add task context")
	knowledgeSearchCmd.Flags().Int32("limit", 5, "Maximum number of knowledge items to return")
	knowledgeSearchCmd.Flags().String("output", "table", "Output format: table or json")
}

func runKnowledgeSearch(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt32("limit")
	if limit <= 0 {
		return fmt.Errorf("--limit must be greater than 0")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"query": args[0],
		"limit": limit,
	}
	if issue, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issue) != "" {
		body["issue_id"] = strings.TrimSpace(issue)
	}

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	path := "/api/knowledge/search"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("search knowledge: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(cmd.OutOrStdout(), result)
	}

	headers := []string{"ID", "TITLE", "SCORE", "CONFIDENCE", "STATUS", "SCOPE", "SOURCE", "SUMMARY"}
	rows := make([][]string, 0)
	for _, raw := range anySlice(result["results"]) {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item, _ := row["item"].(map[string]any)
		sourceSummary, _ := row["source_summary"].(map[string]any)
		rows = append(rows, []string{
			strVal(item, "id"),
			strVal(item, "title"),
			fmt.Sprintf("%.2f", numVal(row, "final_score")),
			strVal(item, "confidence_status"),
			strVal(item, "lifecycle_status"),
			knowledgeSearchScope(item),
			knowledgeSearchSource(sourceSummary),
			knowledgeSearchSummary(item),
		})
	}
	cli.PrintTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

func anySlice(v any) []any {
	switch values := v.(type) {
	case []any:
		return values
	default:
		return nil
	}
}

func knowledgeSearchScope(item map[string]any) string {
	if value := strings.TrimSpace(strVal(item, "applicability")); value != "" {
		return truncateTableValue(value, 80)
	}
	labels := anySlice(item["domain_labels"])
	if len(labels) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		if value := strings.TrimSpace(fmt.Sprintf("%v", label)); value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return truncateTableValue(strings.Join(parts, ","), 80)
}

func knowledgeSearchSource(summary map[string]any) string {
	if title := strings.TrimSpace(strVal(summary, "primary_source_title")); title != "" {
		return truncateTableValue(title, 80)
	}
	sourceType := strings.TrimSpace(strVal(summary, "primary_source_type"))
	sourceID := strings.TrimSpace(strVal(summary, "primary_source_id"))
	switch {
	case sourceType != "" && sourceID != "":
		return sourceType + ":" + displayID(sourceID, false)
	case sourceType != "":
		return sourceType
	default:
		return "-"
	}
}

func knowledgeSearchSummary(item map[string]any) string {
	for _, key := range []string{"problem_pattern", "recommended_practice", "trigger_conditions", "diagnostic_steps"} {
		if value := strings.TrimSpace(strVal(item, key)); value != "" {
			return truncateTableValue(value, 120)
		}
	}
	return "-"
}

func truncateTableValue(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}
