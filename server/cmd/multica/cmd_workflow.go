package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage workflow runs",
}

var workflowPlanCmd = &cobra.Command{
	Use:   "plan <run-id>",
	Short: "Submit an execution plan for an orchestrated workflow run",
	Long: `Submit a dynamic execution plan for a workflow run in "planning" status.
The plan is a JSON array of steps. Each step must have a "type" field.

Agent steps:    {"type":"agent","agent_id":"...","prompt":"..."}
Review steps:   {"type":"review","reviewer_type":"agent","review_agent_id":"...","review_prompt":"..."}
Approval steps: {"type":"approval","reviewer_ids":["..."]}`,
	Args: exactArgs(1),
	RunE: runWorkflowPlan,
}

var workflowReviewCmd = &cobra.Command{
	Use:   "review <step-run-id>",
	Short: "Submit a review decision for a workflow step",
	Long: `Submit a review decision for a workflow review step.
Valid decisions: approve, reject, stop, redirect.

For redirect, you must also specify --redirect-to with the target step index.`,
	Args: exactArgs(1),
	RunE: runWorkflowReview,
}

func init() {
	workflowCmd.AddCommand(workflowPlanCmd)
	workflowCmd.AddCommand(workflowReviewCmd)

	// workflow plan
	workflowPlanCmd.Flags().String("steps", "", "Steps as JSON array (required)")
	workflowPlanCmd.Flags().String("steps-file", "", "Read steps from a JSON file")

	// workflow review
	workflowReviewCmd.Flags().String("decision", "", "Decision: approve, reject, stop, redirect (required)")
	workflowReviewCmd.Flags().String("comment", "", "Optional comment")
	workflowReviewCmd.Flags().Int("redirect-to", -1, "Target step index for redirect decision")
}

func runWorkflowPlan(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	runID := args[0]
	stepsStr, _ := cmd.Flags().GetString("steps")
	stepsFile, _ := cmd.Flags().GetString("steps-file")

	var stepsRaw json.RawMessage

	if stepsFile != "" {
		data, err := os.ReadFile(stepsFile)
		if err != nil {
			return fmt.Errorf("read steps file: %w", err)
		}
		stepsRaw = data
	} else if stepsStr != "" {
		stepsRaw = json.RawMessage(stepsStr)
	} else {
		return fmt.Errorf("either --steps or --steps-file is required")
	}

	// Validate JSON
	var steps []any
	if err := json.Unmarshal(stepsRaw, &steps); err != nil {
		return fmt.Errorf("invalid steps JSON: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]any{"steps": steps}
	var resp map[string]any
	path := fmt.Sprintf("/api/workflow-runs/%s/plan", runID)
	if err := client.PostJSON(ctx, path, body, &resp); err != nil {
		return fmt.Errorf("submit plan: %w", err)
	}

	return cli.PrintJSON(os.Stdout, resp)
}

func runWorkflowReview(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	stepRunID := args[0]
	decision, _ := cmd.Flags().GetString("decision")
	comment, _ := cmd.Flags().GetString("comment")
	redirectTo, _ := cmd.Flags().GetInt("redirect-to")

	if decision == "" {
		return fmt.Errorf("--decision is required (approve, reject, stop, redirect)")
	}

	// Normalize shorthand
	switch decision {
	case "approve":
		decision = "approved"
	case "reject":
		decision = "rejected"
	case "stop":
		decision = "stopped"
	}

	body := map[string]any{
		"decision": decision,
		"comment":  comment,
	}
	if decision == "redirect" {
		if redirectTo < 0 {
			return fmt.Errorf("--redirect-to is required for redirect decision")
		}
		body["redirect_to_step"] = redirectTo
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp map[string]any
	path := fmt.Sprintf("/api/workflow-step-runs/%s/review", stepRunID)
	if err := client.PostJSON(ctx, path, body, &resp); err != nil {
		return fmt.Errorf("submit review: %w", err)
	}

	return cli.PrintJSON(os.Stdout, resp)
}
