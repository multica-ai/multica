package main

// CEREBRO-PATCH(analytics-cli): FIR-2996 canonical analytics query CLI.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	cerebroanalytics "github.com/multica-ai/multica/server/internal/cerebro/analytics"
	"github.com/multica-ai/multica/server/internal/cli"
)

var analyticsCmd = &cobra.Command{Use: "analytics", Short: "Query workspace analytics"}
var analyticsCatalogCmd = &cobra.Command{Use: "catalog", Short: "List analytics metrics and dimensions", Args: cobra.NoArgs, RunE: runAnalyticsCatalog}
var analyticsQueryCmd = &cobra.Command{Use: "query", Short: "Run a canonical analytics query from JSON", Args: cobra.NoArgs, RunE: runAnalyticsQuery}
var analyticsBackfillCmd = &cobra.Command{Use: "backfill", Short: "Project a page of historical runs into analytics", Args: cobra.NoArgs, RunE: runAnalyticsBackfill}

func init() {
	analyticsCmd.AddCommand(analyticsCatalogCmd, analyticsQueryCmd, analyticsBackfillCmd)
	analyticsQueryCmd.Flags().String("file", "", "Read query JSON from a file instead of stdin")
	analyticsBackfillCmd.Flags().String("cursor", "", "Resume after this run ID")
	analyticsBackfillCmd.Flags().Int("limit", 250, "Runs to project in this page (1-1000)")
}

func runAnalyticsBackfill(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	cursor, _ := cmd.Flags().GetString("cursor")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 1000 {
		return fmt.Errorf("--limit must be between 1 and 1000")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/analytics/backfill", map[string]any{"cursor": cursor, "limit": limit}, &result); err != nil {
		return fmt.Errorf("backfill analytics: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runAnalyticsCatalog(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	var catalog cerebroanalytics.Catalog
	if err := client.GetJSON(ctx, "/api/analytics/catalog", &catalog); err != nil {
		return fmt.Errorf("get analytics catalog: %w", err)
	}
	return cli.PrintJSON(os.Stdout, catalog)
}

func runAnalyticsQuery(cmd *cobra.Command, _ []string) error {
	input := io.Reader(os.Stdin)
	file, _ := cmd.Flags().GetString("file")
	if file != "" {
		opened, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("open analytics query: %w", err)
		}
		defer opened.Close()
		input = opened
	}
	query, err := loadAnalyticsQuery(input)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	var result cerebroanalytics.QueryResult
	if err := client.PostJSON(ctx, "/api/analytics/query", query, &result); err != nil {
		return fmt.Errorf("query analytics: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func loadAnalyticsQuery(input io.Reader) (cerebroanalytics.Query, error) {
	var query cerebroanalytics.Query
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&query); err != nil {
		return query, fmt.Errorf("decode analytics query: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return query, fmt.Errorf("decode analytics query: expected one JSON object")
	}
	return query, nil
}
