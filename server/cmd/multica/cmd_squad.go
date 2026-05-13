package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var squadCmd = &cobra.Command{
	Use:   "squad",
	Short: "Work with squads",
}

// ── List ────────────────────────────────────────────────────────────────────

var squadListCmd = &cobra.Command{
	Use:   "list",
	Short: "List squads in the workspace",
	Args:  cobra.NoArgs,
	RunE:  runSquadList,
}

func runSquadList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var squads []map[string]any
	if err := client.GetJSON(ctx, "/api/squads", &squads); err != nil {
		return fmt.Errorf("list squads: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, squads)
	}

	if len(squads) == 0 {
		fmt.Fprintln(os.Stderr, "No squads found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tLEADER ID\tMEMBERS")
	for _, s := range squads {
		fmt.Fprintf(w, "%s\t%s\t%s\t-\n",
			strVal(s, "id"), strVal(s, "name"), strVal(s, "leader_id"))
	}
	return w.Flush()
}

// ── Get ─────────────────────────────────────────────────────────────────────

var squadGetCmd = &cobra.Command{
	Use:   "get <squad-id>",
	Short: "Get squad details",
	Args:  exactArgs(1),
	RunE:  runSquadGet,
}

func runSquadGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var squad map[string]any
	if err := client.GetJSON(ctx, "/api/squads/"+args[0], &squad); err != nil {
		return fmt.Errorf("get squad: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, squad)
	}

	fmt.Printf("ID:           %s\n", strVal(squad, "id"))
	fmt.Printf("Name:         %s\n", strVal(squad, "name"))
	fmt.Printf("Description:  %s\n", strVal(squad, "description"))
	fmt.Printf("Leader ID:    %s\n", strVal(squad, "leader_id"))
	fmt.Printf("Created:      %s\n", strVal(squad, "created_at"))
	if inst := strVal(squad, "instructions"); inst != "" {
		fmt.Printf("Instructions: %s\n", inst)
	}
	return nil
}

// ── Members ─────────────────────────────────────────────────────────────────

var squadMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Work with squad members",
}

var squadMemberListCmd = &cobra.Command{
	Use:   "list <squad-id>",
	Short: "List members of a squad",
	Args:  exactArgs(1),
	RunE:  runSquadMemberList,
}

func runSquadMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/squads/"+args[0]+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	if len(members) == 0 {
		fmt.Fprintln(os.Stderr, "No members found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "MEMBER ID\tTYPE\tROLE")
	for _, m := range members {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			strVal(m, "member_id"), strVal(m, "member_type"), strVal(m, "role"))
	}
	return w.Flush()
}

// ── Activity ────────────────────────────────────────────────────────────────

var squadActivityCmd = &cobra.Command{
	Use:   "activity <issue-id> <outcome>",
	Short: "Record a squad leader evaluation on an issue",
	Long: `Record the squad leader's evaluation decision for an issue.

Outcome must be one of:
  action     — leader delegated or took action
  no_action  — leader evaluated and decided no action needed
  failed     — leader encountered an error

This command is intended to be called by squad leader agents after each
trigger to record their decision in the issue timeline.`,
	Args: exactArgs(2),
	RunE: runSquadActivity,
}

func runSquadActivity(cmd *cobra.Command, args []string) error {
	issueID := args[0]
	outcome := args[1]

	if outcome != "action" && outcome != "no_action" && outcome != "failed" {
		return fmt.Errorf("invalid outcome %q; valid values: action, no_action, failed", outcome)
	}

	reason, _ := cmd.Flags().GetString("reason")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, issueID)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{
		"outcome": outcome,
		"reason":  reason,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueRef.ID+"/squad-evaluated", body, &result); err != nil {
		return fmt.Errorf("record evaluation: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Squad evaluation recorded: %s (issue %s)\n", outcome, issueRef.Display)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ── Init ────────────────────────────────────────────────────────────────────

func init() {
	squadListCmd.Flags().String("output", "table", "Output format: table or json")
	squadGetCmd.Flags().String("output", "table", "Output format: table or json")
	squadMemberListCmd.Flags().String("output", "table", "Output format: table or json")
	squadActivityCmd.Flags().String("reason", "", "Short explanation of the decision")
	squadActivityCmd.Flags().String("output", "table", "Output format: table or json")

	squadMemberCmd.AddCommand(squadMemberListCmd)

	squadCmd.AddCommand(squadListCmd)
	squadCmd.AddCommand(squadGetCmd)
	squadCmd.AddCommand(squadMemberCmd)
	squadCmd.AddCommand(squadActivityCmd)
}
