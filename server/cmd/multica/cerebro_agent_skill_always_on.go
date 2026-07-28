package main

// FIR-3805 — `multica agent skills always-on <agent-id>`: mark which of an
// agent's bound skills have their FULL text pasted into the agent's
// instructions on every run, instead of being listed as a one-line,
// load-on-demand entry.
//
// The set is replaced wholesale, the same shape as `agent skills set`, so the
// command is idempotent and "clear them all" is expressible (`--skill-ids ''`).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var agentSkillsAlwaysOnCmd = &cobra.Command{
	Use:   "always-on <agent-id>",
	Short: "Set which of an agent's skills are always on (full text injected into its instructions every run)",
	Long: `Set which of an agent's bound skills are "always on".

An always-on skill has its full text pasted into the agent's instructions on
every run, so rules that must hold for every response (writing style,
check-before-you-send gates) apply without the agent choosing to open the skill
first. A skill that is not always on keeps today's behaviour: one line in the
brief, loaded on demand.

The list replaces the agent's whole always-on set. Every id must already be
bound to the agent — bind it with 'multica agent skills add' first.

Examples:
  # Make two skills always on
  multica agent skills always-on <agent-id> --skill-ids <id-a>,<id-b>

  # Clear the always-on set (skills stay bound, just no longer always on)
  multica agent skills always-on <agent-id> --skill-ids ''`,
	Args: exactArgs(1),
	RunE: runAgentSkillsAlwaysOn,
}

func init() {
	agentSkillsCmd.AddCommand(agentSkillsAlwaysOnCmd)
	// StringSlice, not String: cleanSkillIDsFlag reads this with GetStringSlice,
	// the same as `agent skills set` and `agent skills add`. A plain String flag
	// makes that read return nothing, and the command then silently sends an
	// empty set (FIR-3881).
	agentSkillsAlwaysOnCmd.Flags().StringSlice("skill-ids", nil, "Comma-separated skill IDs to mark always-on (replaces the current set; use --skill-ids '' to clear)")
	agentSkillsAlwaysOnCmd.Flags().String("output", "table", "Output format: table or json")
}

// cerebroAlwaysOnCell renders the always_on field of one agent-skill row for
// the `agent skills list` table. Missing or non-boolean reads as "no" — an
// older server that does not send the field must not blank the column.
func cerebroAlwaysOnCell(skill map[string]any) string {
	if v, ok := skill["always_on"].(bool); ok && v {
		return "yes"
	}
	return "no"
}

func runAgentSkillsAlwaysOn(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	if !cmd.Flags().Changed("skill-ids") {
		return fmt.Errorf("--skill-ids is required (comma-separated skill IDs; use --skill-ids '' to clear the always-on set)")
	}
	ids := cleanSkillIDsFlag(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"always_on_skill_ids": ids}
	var result json.RawMessage
	if err := client.PutJSON(ctx, "/api/agents/"+args[0]+"/skills/always-on", body, &result); err != nil {
		return fmt.Errorf("set always-on skills: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	rows := make([][]string, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, []string{args[0], id, "yes"})
	}
	if len(rows) == 0 {
		rows = append(rows, []string{args[0], "(none)", "no"})
	}
	cli.PrintTable(os.Stdout, []string{"AGENT_ID", "SKILL_ID", "ALWAYS_ON"}, rows)
	return nil
}
