package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ── squad capability ──────────────────────────────────────────────────────────

var squadCapabilityCmd = &cobra.Command{
	Use:   "capability",
	Short: "Work with squad capabilities",
	Long: `Declare what a squad can do so other squads can discover it.

Capability is stored as a JSON object with three fields:
  domains     — broad areas of expertise (e.g. "strategic_decision")
  keywords    — specific skills or topics (e.g. "决策分析,加权评分")
  description — short natural-language summary

Use 'multica squad route <query>' to find the right squad for a task.`,
}

// ── capability set ────────────────────────────────────────────────────────────

var squadCapabilitySetCmd = &cobra.Command{
	Use:   "set <squad-id>",
	Short: "Set or update a squad's capability",
	Args:  exactArgs(1),
	RunE:  runSquadCapabilitySet,
}

func runSquadCapabilitySet(cmd *cobra.Command, args []string) error {
	domains, _ := cmd.Flags().GetString("domains")
	keywords, _ := cmd.Flags().GetString("keywords")
	description, _ := cmd.Flags().GetString("description")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	domainList := splitCSV(domains)
	keywordList := splitCSV(keywords)

	body := map[string]any{
		"domains":     domainList,
		"keywords":    keywordList,
		"description": description,
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/squads/"+args[0]+"/capability", body, &result); err != nil {
		return fmt.Errorf("set capability: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stderr, "Capability set for squad %s.\n", args[0])
	return nil
}

// ── capability get ────────────────────────────────────────────────────────────

var squadCapabilityGetCmd = &cobra.Command{
	Use:   "get <squad-id>",
	Short: "Get a squad's capability",
	Args:  exactArgs(1),
	RunE:  runSquadCapabilityGet,
}

func runSquadCapabilityGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/squads/"+args[0]+"/capability", &result); err != nil {
		return fmt.Errorf("get capability: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Squad:    %s\n", strVal(result, "name"))
	cap, _ := result["capability"].(map[string]any)
	if cap == nil {
		fmt.Println("Capability: (not set)")
		return nil
	}
	if desc := strVal(cap, "description"); desc != "" {
		fmt.Printf("Description: %s\n", desc)
	}
	if domains := cap["domains"]; domains != nil {
		fmt.Printf("Domains:  %v\n", domains)
	}
	if keywords := cap["keywords"]; keywords != nil {
		fmt.Printf("Keywords: %v\n", keywords)
	}
	return nil
}

// ── capability list ───────────────────────────────────────────────────────────

var squadCapabilityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List capabilities of all squads in the workspace",
	Args:  cobra.NoArgs,
	RunE:  runSquadCapabilityList,
}

func runSquadCapabilityList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var results []map[string]any
	if err := client.GetJSON(ctx, "/api/squads/capabilities", &results); err != nil {
		return fmt.Errorf("list capabilities: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, results)
	}

	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "No squads found.")
		return nil
	}

	for _, r := range results {
		cap, _ := r["capability"].(map[string]any)
		hasCap := cap != nil && len(cap) > 0
		marker := " "
		if !hasCap {
			marker = "!"
		}
		fmt.Printf("%s %-30s %s\n", marker, strVal(r, "name"), capSummary(cap))
	}
	return nil
}

func capSummary(cap map[string]any) string {
	if cap == nil || len(cap) == 0 {
		return "(not declared)"
	}
	parts := []string{}
	if desc := strVal(cap, "description"); desc != "" {
		parts = append(parts, desc)
	}
	if kws, ok := cap["keywords"].([]any); ok && len(kws) > 0 {
		strs := make([]string, len(kws))
		for i, v := range kws {
			strs[i] = fmt.Sprint(v)
		}
		parts = append(parts, strings.Join(strs, ", "))
	}
	return strings.Join(parts, " | ")
}

// ── capability delete ─────────────────────────────────────────────────────────

var squadCapabilityDeleteCmd = &cobra.Command{
	Use:   "delete <squad-id>",
	Short: "Clear a squad's capability (reset to empty)",
	Args:  exactArgs(1),
	RunE:  runSquadCapabilityDelete,
}

func runSquadCapabilityDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/squads/"+args[0]+"/capability"); err != nil {
		return fmt.Errorf("delete capability: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"squad_id": args[0], "cleared": true})
	}
	fmt.Fprintf(os.Stderr, "Capability cleared for squad %s.\n", args[0])
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	// capability set
	squadCapabilitySetCmd.Flags().String("domains", "", "Comma-separated domains (e.g. \"strategic_decision,tech_architecture\")")
	squadCapabilitySetCmd.Flags().String("keywords", "", "Comma-separated keywords (e.g. \"决策分析,加权评分\")")
	squadCapabilitySetCmd.Flags().String("description", "", "Short natural-language description")
	squadCapabilitySetCmd.Flags().String("output", "json", "Output format: table or json")

	// capability get
	squadCapabilityGetCmd.Flags().String("output", "table", "Output format: table or json")

	// capability list
	squadCapabilityListCmd.Flags().String("output", "table", "Output format: table or json")

	// capability delete
	squadCapabilityDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	squadCapabilityCmd.AddCommand(squadCapabilitySetCmd)
	squadCapabilityCmd.AddCommand(squadCapabilityGetCmd)
	squadCapabilityCmd.AddCommand(squadCapabilityListCmd)
	squadCapabilityCmd.AddCommand(squadCapabilityDeleteCmd)
}
