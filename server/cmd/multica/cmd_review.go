package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Read scorecards and publish reviews (the People & Performance reviewer)",
	Long: `Scorecards are the figures the company keeps about each agent, squad and the
company, for the last completed week and month. A review judges one subject for
one period from those figures and cites them. Only the workspace's reviewer agent
publishes reviews; owners and admins read them.

  multica review scorecards --period month --json
  multica review publish --file review.json

A subject with too few runs has an "insufficient" scorecard and is never reviewed.`,
}

var reviewScorecardsCmd = &cobra.Command{
	Use:   "scorecards",
	Short: "List scorecards you may read",
	Long: `List the stored scorecards, newest period first.

  multica review scorecards [--subject-type agent|squad|company] [--subject-id <id>] [--period week|month]`,
	Args: cobra.NoArgs,
	RunE: runReviewScorecards,
}

var reviewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List reviews you may read",
	Args:  cobra.NoArgs,
	RunE:  runReviewList,
}

var reviewPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a review and file its change recommendations",
	Long: `Publish one review from a JSON file.

  {
    "subject_type": "agent",
    "subject_id": "<agent id>",
    "period": "month",
    "period_start": "2026-10-01",
    "rating": "below",
    "narrative": "What the figures show, and what you saw in the issues.",
    "cited_metrics": ["success_rate", "first_pass_rate"],
    "recommendations": [
      {"type": "instruction_change", "rationale": "...",
       "payload": {"agent_id": "<agent id>", "instructions": "...", "diff_summary": "...", "rationale": "..."}},
      {"type": "observe", "rationale": "Nothing to change yet."}
    ]
  }

The server copies each cited figure from the stored scorecard into the review.
A change recommendation becomes an organisation change proposal for the boss.
The payload is that change's fields; its type is set from the recommendation.
A change the attention limits refuse is kept on the review with its reason.`,
	Args: cobra.NoArgs,
	RunE: runReviewPublish,
}

func runReviewScorecards(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	q := url.Values{}
	for _, f := range []string{"subject-type", "subject-id", "period"} {
		v, _ := cmd.Flags().GetString(f)
		if v != "" {
			q.Set(strings.ReplaceAll(f, "-", "_"), v)
		}
	}
	var result struct {
		Scorecards []map[string]any `json:"scorecards"`
	}
	if err := client.GetJSON(ctx, "/api/scorecards?"+q.Encode(), &result); err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SUBJECT\tID\tPERIOD\tSTATUS\tSAMPLE\tSUCCESS")
	for _, sc := range result.Scorecards {
		success := "-"
		if m, ok := sc["metrics"].(map[string]any); ok {
			if rate, ok := m["success_rate"].(map[string]any); ok && rate["value"] != nil {
				success = fmt.Sprintf("%.0f%%", rate["value"].(float64)*100)
			}
		}
		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%s\n", sc["subject_type"], sc["subject_id"], sc["period_start"], sc["status"], sc["sample_size"], success)
	}
	return w.Flush()
}

func runReviewList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/reviews", &result); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runReviewPublish(cmd *cobra.Command, _ []string) error {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return fmt.Errorf("--file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read review file: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("review file is not a JSON object: %w", err)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result struct {
		Review struct {
			ID              string `json:"id"`
			Rating          string `json:"rating"`
			Recommendations []struct {
				Type       string `json:"type"`
				ProposalID string `json:"proposal_id"`
				Refused    string `json:"refused"`
			} `json:"recommendations"`
		} `json:"review"`
	}
	if err := client.PostJSON(ctx, "/api/reviews", body, &result); err != nil {
		return err
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Review published: %s (%s)\n", result.Review.ID, result.Review.Rating)
	for _, rec := range result.Review.Recommendations {
		switch {
		case rec.ProposalID != "":
			fmt.Printf("  %s: proposal %s\n", rec.Type, rec.ProposalID)
		case rec.Refused != "":
			fmt.Printf("  %s: not filed: %s\n", rec.Type, rec.Refused)
		default:
			fmt.Printf("  %s: noted, no change proposed\n", rec.Type)
		}
	}
	return nil
}

func init() {
	reviewScorecardsCmd.Flags().String("subject-type", "", "agent, squad or company")
	reviewScorecardsCmd.Flags().String("subject-id", "", "The subject's id")
	reviewScorecardsCmd.Flags().String("period", "", "week or month")
	reviewScorecardsCmd.Flags().String("output", "table", "Output format: table or json")
	reviewListCmd.Flags().String("output", "json", "Output format: json")
	reviewPublishCmd.Flags().String("file", "", "Path to the review JSON — required")
	reviewPublishCmd.Flags().String("output", "table", "Output format: table or json")

	reviewCmd.AddCommand(reviewScorecardsCmd)
	reviewCmd.AddCommand(reviewListCmd)
	reviewCmd.AddCommand(reviewPublishCmd)
}
