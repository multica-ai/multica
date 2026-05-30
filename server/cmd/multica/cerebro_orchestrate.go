// CEREBRO-PATCH(cerebro-orchestration): FIR-2564 cerebro-only file — orchestration CLI.
//
// `multica orchestrate` is the handle on the orchestration engine
// (server/internal/cerebro/orchestration). It reads a plan file describing
// tasks and their "waits-on" edges, then:
//
//	validate <plan>  — check the plan is runnable (no cycles, no missing deps)
//	waves    <plan>  — show which tasks can run together, wave by wave
//	run      <plan>  — execute the plan locally and report the outcome
//
// `run` executes against a local simulator by default (--dry-run, the default):
// it exercises the real scheduler — waves, concurrency, the single-writer lock,
// retries and downstream blocking — without dispatching real agents. Wiring the
// executor to live agent dispatch / sub-issue promotion is the server-side step
// and is gated behind a deploy; the engine and this command are the foundation.
//
// Label gate. When `run` is given `--issue <id>`, the CLI refuses to schedule
// anything unless that issue carries the `orchestrate` label. This is the
// early-rollout safety guarantee: the engine is reachable to anyone with the
// CLI, but it stays inert on every issue until a human explicitly opts in.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cerebro/orchestration"
	"github.com/multica-ai/multica/server/internal/cli"
)

// orchestrateLabel is the opt-in label every issue must carry before
// `orchestrate run --issue <id>` will execute on it. The label gate is the
// safety guarantee Jesper asked for during early rollout: the engine never
// runs against an issue that hasn't been explicitly tagged, so an accidental
// or auto-generated invocation has nothing to do until a human attaches the
// label.
const orchestrateLabel = "orchestrate"

var orchestrateCmd = &cobra.Command{
	Use:   "orchestrate",
	Short: "Run dependency-aware task plans (cerebro orchestration layer)",
	Long: "Orchestrate a plan of tasks that depend on each other: compute which " +
		"tasks can run concurrently (waves), reject impossible plans, and run " +
		"ready tasks in parallel while letting only one writing task mutate at a time.",
}

var orchestrateValidateCmd = &cobra.Command{
	Use:   "validate <plan-file>",
	Short: "Validate a plan (cycles, missing/self dependencies, shape)",
	Args:  exactArgs(1),
	RunE:  runOrchestrateValidate,
}

var orchestrateWavesCmd = &cobra.Command{
	Use:   "waves <plan-file>",
	Short: "Show the execution waves (what can run together)",
	Args:  exactArgs(1),
	RunE:  runOrchestrateWaves,
}

var orchestrateRunCmd = &cobra.Command{
	Use:   "run <plan-file>",
	Short: "Execute a plan (local simulation by default)",
	Args:  exactArgs(1),
	RunE:  runOrchestrateRun,
}

func init() {
	orchestrateValidateCmd.Flags().String("output", "table", "Output format: table|json")
	orchestrateWavesCmd.Flags().String("output", "table", "Output format: table|json")
	orchestrateRunCmd.Flags().String("output", "table", "Output format: table|json")
	orchestrateRunCmd.Flags().Int("concurrency", 0, "Override plan concurrency (1-8)")
	orchestrateRunCmd.Flags().Bool("dry-run", true, "Simulate node execution instead of dispatching real work")
	orchestrateRunCmd.Flags().String("issue", "", "Issue id or key the plan belongs to; requires the `"+orchestrateLabel+"` label on the issue")

	orchestrateCmd.AddCommand(orchestrateValidateCmd)
	orchestrateCmd.AddCommand(orchestrateWavesCmd)
	orchestrateCmd.AddCommand(orchestrateRunCmd)
}

func loadPlan(path string) (orchestration.Plan, error) {
	var plan orchestration.Plan
	raw, err := os.ReadFile(path)
	if err != nil {
		return plan, fmt.Errorf("read plan: %w", err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return plan, fmt.Errorf("parse plan JSON: %w", err)
	}
	return plan, nil
}

func runOrchestrateValidate(cmd *cobra.Command, args []string) error {
	plan, err := loadPlan(args[0])
	if err != nil {
		return err
	}
	issues := orchestration.ValidatePlan(plan)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"ok": len(issues) == 0, "issues": issues})
	}
	if len(issues) == 0 {
		fmt.Fprintf(os.Stdout, "OK — plan is valid (%d nodes)\n", len(plan.Nodes))
		return nil
	}
	fmt.Fprintf(os.Stdout, "INVALID — %d problem(s):\n", len(issues))
	for _, is := range issues {
		fmt.Fprintf(os.Stdout, "  • [%s] %s\n", is.Code, is.Message)
	}
	return errSilent // non-zero exit without duplicate error print
}

func runOrchestrateWaves(cmd *cobra.Command, args []string) error {
	plan, err := loadPlan(args[0])
	if err != nil {
		return err
	}
	if issues := orchestration.ValidatePlan(plan); len(issues) > 0 {
		return fmt.Errorf("plan is invalid: %s (run `orchestrate validate` for details)", issues[0].Message)
	}
	waves := orchestration.ComputeWaves(plan.Nodes)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, waves)
	}
	for _, w := range waves {
		fmt.Fprintf(os.Stdout, "%s (can run together):\n", w.Label)
		for _, id := range w.NodeIDs {
			fmt.Fprintf(os.Stdout, "  - %s\n", id)
		}
	}
	return nil
}

func runOrchestrateRun(cmd *cobra.Command, args []string) error {
	plan, err := loadPlan(args[0])
	if err != nil {
		return err
	}
	if issues := orchestration.ValidatePlan(plan); len(issues) > 0 {
		return fmt.Errorf("plan is invalid: %s (run `orchestrate validate` for details)", issues[0].Message)
	}
	if c, _ := cmd.Flags().GetInt("concurrency"); c > 0 {
		plan.Concurrency = c
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if !dryRun {
		return fmt.Errorf("live execution is not available in this build: real agent dispatch is the gated server-side step; re-run with --dry-run")
	}
	if issueRef, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issueRef) != "" {
		if err := requireOrchestrateLabel(cmd, issueRef); err != nil {
			return err
		}
	}

	output, _ := cmd.Flags().GetString("output")
	verbose := output != "json"
	exec := func(ctx context.Context, node orchestration.Node, deps []orchestration.DepResult) orchestration.NodeResult {
		if verbose {
			fmt.Fprintf(os.Stdout, "  ▶ running %-16s%s\n", node.ID, writeTag(node.Write))
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			return orchestration.NodeResult{Success: false, Error: "cancelled"}
		}
		return orchestration.NodeResult{Success: true, Summary: "simulated ok"}
	}

	if verbose {
		fmt.Fprintf(os.Stdout, "Running %q — %d nodes, concurrency %d (dry-run)\n",
			plan.Title, len(plan.Nodes), planConcurrency(plan))
	}
	run := orchestration.ExecutePlan(context.Background(), plan, exec)

	if output == "json" {
		return cli.PrintJSON(os.Stdout, run)
	}
	fmt.Fprintf(os.Stdout, "\nResult: %s\n", run.Status)
	for _, n := range run.Nodes {
		line := fmt.Sprintf("  %-9s %-16s", n.Status, n.ID)
		if n.AttemptCount > 1 {
			line += fmt.Sprintf(" (%d attempts)", n.AttemptCount)
		}
		if n.Error != "" {
			line += "  — " + n.Error
		}
		fmt.Fprintln(os.Stdout, line)
	}
	if run.Status != orchestration.StatusSuccess {
		return errSilent
	}
	return nil
}

func writeTag(write bool) string {
	if write {
		return "[write — exclusive]"
	}
	return ""
}

func planConcurrency(plan orchestration.Plan) int {
	if plan.Concurrency >= 1 && plan.Concurrency <= 8 {
		return plan.Concurrency
	}
	return 2
}

// requireOrchestrateLabel enforces the early-rollout safety gate: an issue
// must carry the `orchestrate` label before `orchestrate run --issue <id>`
// will touch it. The check runs before any scheduling work — if the label is
// missing we fail fast with an actionable message.
func requireOrchestrateLabel(cmd *cobra.Command, issueRef string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	issue, err := resolveIssueRef(ctx, client, issueRef)
	if err != nil {
		return fmt.Errorf("resolve issue %q: %w", issueRef, err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issue.ID+"/labels", &result); err != nil {
		return fmt.Errorf("look up labels on %s: %w", issue.Display, err)
	}
	labelsRaw, _ := result["labels"].([]any)
	for _, raw := range labelsRaw {
		lbl, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := lbl["name"].(string)
		if strings.EqualFold(strings.TrimSpace(name), orchestrateLabel) {
			return nil
		}
	}
	return fmt.Errorf(
		"issue %s is missing the %q label — orchestration only runs on opted-in issues. "+
			"Attach the label first: multica issue label add %s %s",
		issue.Display, orchestrateLabel, issue.Display, orchestrateLabel,
	)
}
