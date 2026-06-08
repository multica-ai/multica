package main

// TECH-3077: skill metadata CLI extensions — category/domain/tag/status/data_domain
// filtering on `multica skill list`, and the `multica skill audit` subcommand.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// flags attached to skillListCmd.
var (
	skillListCategory   string
	skillListDomain     string
	skillListTag        string
	skillListStatus     string
	skillListDataDomain string
)

var skillAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "List skills with missing or incomplete metadata (category/domain/status)",
	RunE:  runSkillAudit,
}

func init() {
	skillListCmd.Flags().StringVar(&skillListCategory, "category", "", "Filter by category (e.g. \"Data & Analyse\")")
	skillListCmd.Flags().StringVar(&skillListDomain, "domain", "", "Filter by domain (e.g. data, deploy, governance)")
	skillListCmd.Flags().StringVar(&skillListTag, "tag", "", "Filter by tag")
	skillListCmd.Flags().StringVar(&skillListStatus, "status", "", "Filter by status (stable, beta, deprecated)")
	skillListCmd.Flags().StringVar(&skillListDataDomain, "data-domain", "", "Filter by data domain (e.g. firtal-dwh)")

	skillCmd.AddCommand(skillAuditCmd)
}

// skillListHasMetadataFilter returns true when any metadata filter flag is set.
func skillListHasMetadataFilter() bool {
	return skillListCategory != "" || skillListDomain != "" || skillListTag != "" ||
		skillListStatus != "" || skillListDataDomain != ""
}

// buildSkillListMetadataQuery builds query params for the filtered list endpoint.
func buildSkillListMetadataQuery() string {
	var parts []string
	addParam := func(k, v string) {
		if v != "" {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	addParam("category", skillListCategory)
	addParam("domain", skillListDomain)
	addParam("tag", skillListTag)
	addParam("status", skillListStatus)
	addParam("data_domain", skillListDataDomain)
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

func runSkillAudit(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var rows []struct {
		ID             string   `json:"id"`
		Name           string   `json:"name"`
		CurrentVersion string   `json:"current_version"`
		Issues         []string `json:"issues"`
	}
	if err := client.GetJSON(ctx, "/api/skills/audit", &rows); err != nil {
		return fmt.Errorf("skill audit: %w", err)
	}

	outputFmt, _ := cmd.Flags().GetString("output")
	if outputFmt == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}

	if len(rows) == 0 {
		fmt.Println("All skills have complete metadata.")
		return nil
	}

	fmt.Printf("%-40s %-12s  %s\n", "NAME", "VERSION", "ISSUES")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range rows {
		fmt.Printf("%-40s %-12s  %s\n", r.Name, r.CurrentVersion, strings.Join(r.Issues, ", "))
	}
	fmt.Printf("\n%d skills need metadata updates.\n", len(rows))
	return nil
}

// ensure json import is used
var _ = json.Marshal
