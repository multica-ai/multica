package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueTeamCmd = &cobra.Command{
	Use:   "team",
	Short: "Coordinate multiple agents on one issue",
}

var issueTeamCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a team-runner issue (backlog, no assignee, policy embedded)",
	Long: `Create an issue pre-configured for the team runner.

The issue is created with status=backlog and no assignee so that no run is
triggered immediately. The team policy (lead/implementer/reviewer) is embedded
in the description. Use 'multica issue team run <id>' to start the workflow.`,
	RunE: runIssueTeamCreate,
}

var issueTeamRunCmd = &cobra.Command{
	Use:   "run <issue-id>",
	Short: "Run a lead, implementer, reviewer flow on one issue",
	Long: `Run a multi-agent lead/implementer/reviewer flow on an existing issue.

The issue must NOT have an existing assignee, because an assignee would trigger
a parasitic initial run when the issue was created. If the issue already has an
assignee, use --detach-assignee to remove it first, or pass --allow-assigned to
proceed with a warning.`,
	Args: exactArgs(1),
	RunE: runIssueTeamRun,
}

type issueTeamPolicy struct {
	Lead        string
	Implementer string
	Reviewer    string
	Until       string
}

type issueTeamAgent struct {
	Name string
	ID   string
}

type issueTeamRunOptions struct {
	IssueID        string
	PolicyText     string
	Wait           bool
	DryRun         bool
	MaxRounds      int
	PollInterval   time.Duration
	Timeout        time.Duration
	AllowAssigned  bool
	DetachAssignee bool
}

type issueTeamResolved struct {
	Policy      issueTeamPolicy
	Lead        issueTeamAgent
	Implementer issueTeamAgent
	Reviewer    issueTeamAgent
}

func init() {
	issueCmd.AddCommand(issueTeamCmd)
	issueTeamCmd.AddCommand(issueTeamCreateCmd)
	issueTeamCmd.AddCommand(issueTeamRunCmd)

	issueTeamCreateCmd.Flags().String("title", "", "Issue title (required)")
	issueTeamCreateCmd.Flags().String("policy", "", "Team policy, e.g.: lead=planner, implementer=builder, reviewer=reviewer (required)")
	issueTeamCreateCmd.Flags().String("parent", "", "Parent issue ID")
	issueTeamCreateCmd.Flags().String("priority", "medium", "Issue priority")
	issueTeamCreateCmd.Flags().String("output", "json", "Output format: json or table")

	issueTeamRunCmd.Flags().String("policy", "", "Inline policy, for example: lead=planner, implementer=builder, reviewer=reviewer")
	issueTeamRunCmd.Flags().Bool("wait", true, "Wait for each role and continue review rounds")
	issueTeamRunCmd.Flags().Bool("dry-run", false, "Resolve policy and print planned actions without posting comments")
	issueTeamRunCmd.Flags().Int("max-rounds", 2, "Maximum implementer and reviewer rounds")
	issueTeamRunCmd.Flags().Duration("poll-interval", 15*time.Second, "Polling interval while waiting")
	issueTeamRunCmd.Flags().Duration("timeout", 45*time.Minute, "Overall team run timeout")
	issueTeamRunCmd.Flags().Bool("allow-assigned", false, "Proceed even if the issue already has an assignee (prints a warning)")
	issueTeamRunCmd.Flags().Bool("detach-assignee", false, "Remove the existing assignee before starting the team run")
}

func runIssueTeamCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	policyText, _ := cmd.Flags().GetString("policy")
	if strings.TrimSpace(policyText) == "" {
		return fmt.Errorf("--policy is required (e.g. lead=planner, implementer=builder, reviewer=reviewer)")
	}
	policy, err := parseIssueTeamPolicy(policyText)
	if err != nil {
		return fmt.Errorf("invalid --policy: %w", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"title":       title,
		"status":      "backlog",
		"description": teamPolicyBlock(policy, ""),
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		body["priority"] = v
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		body["parent_issue_id"] = v
	}
	// Deliberately omit assignee so no initial run is triggered.

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// teamPolicyBlock formats a policy as an embeddable description block.
// If extra is non-empty it is appended after the policy block.
func teamPolicyBlock(policy issueTeamPolicy, extra string) string {
	var b strings.Builder
	b.WriteString("multica-team:\n")
	fmt.Fprintf(&b, "lead=%s\n", policy.Lead)
	fmt.Fprintf(&b, "implementer=%s\n", policy.Implementer)
	fmt.Fprintf(&b, "reviewer=%s\n", policy.Reviewer)
	b.WriteString("continue until reviewer approves\n")
	if strings.TrimSpace(extra) != "" {
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(extra))
		b.WriteString("\n")
	}
	return b.String()
}

func runIssueTeamRun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	policyText, _ := cmd.Flags().GetString("policy")
	wait, _ := cmd.Flags().GetBool("wait")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	maxRounds, _ := cmd.Flags().GetInt("max-rounds")
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	allowAssigned, _ := cmd.Flags().GetBool("allow-assigned")
	detachAssignee, _ := cmd.Flags().GetBool("detach-assignee")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	opts := issueTeamRunOptions{
		IssueID:        args[0],
		PolicyText:     policyText,
		Wait:           wait,
		DryRun:         dryRun,
		MaxRounds:      maxRounds,
		PollInterval:   pollInterval,
		Timeout:        timeout,
		AllowAssigned:  allowAssigned,
		DetachAssignee: detachAssignee,
	}
	return runIssueTeam(ctx, client, opts, os.Stdout)
}

func runIssueTeam(ctx context.Context, client *cli.APIClient, opts issueTeamRunOptions, out io.Writer) error {
	if opts.IssueID == "" {
		return fmt.Errorf("issue id is required")
	}
	if opts.MaxRounds < 1 {
		return fmt.Errorf("--max-rounds must be at least 1")
	}
	if opts.PollInterval <= 0 {
		return fmt.Errorf("--poll-interval must be positive")
	}

	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(opts.IssueID), &issue); err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	// Guardrail: an existing assignee causes a parasitic initial run.
	if assigneeID := strVal(issue, "assignee_id"); assigneeID != "" {
		switch {
		case opts.DetachAssignee:
			var patched map[string]any
			if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(opts.IssueID),
				map[string]any{"assignee_id": nil, "assignee_type": nil}, &patched); err != nil {
				return fmt.Errorf("detach assignee: %w", err)
			}
			fmt.Fprintf(out, "Detached assignee from issue %s before team run.\n", opts.IssueID)
		case opts.AllowAssigned:
			fmt.Fprintf(out, "Warning: issue %s already has an assignee (%s). This may have triggered a parasitic run. Proceeding because --allow-assigned is set.\n", opts.IssueID, assigneeID)
		default:
			return fmt.Errorf(
				"issue %s already has an assignee (%s): creating a team-runner issue with an assignee triggers a parasitic initial run.\n"+
					"Use --detach-assignee to remove the assignee first, or --allow-assigned to proceed with a warning.\n"+
					"To avoid this in future, create team issues with 'multica issue team create' which forces status=backlog and no assignee.",
				opts.IssueID, assigneeID,
			)
		}
	}

	policy, err := issueTeamPolicyForIssue(ctx, client, opts.PolicyText, issue, opts.IssueID)
	if err != nil {
		return err
	}

	resolved, err := resolveIssueTeam(ctx, client, policy)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return printIssueTeamDryRun(out, opts.IssueID, resolved)
	}

	if err := updateIssueStatus(ctx, client, opts.IssueID, "in_progress"); err != nil {
		return err
	}

	if !opts.Wait {
		if _, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, "lead", resolved.Lead, leadPrompt()); err != nil {
			return err
		}
		if _, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, "implementer", resolved.Implementer, implementerPrompt(1, "")); err != nil {
			return err
		}
		if _, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, "reviewer", resolved.Reviewer, reviewerPrompt(1)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Triggered lead, implementer, and reviewer on issue %s\n", opts.IssueID)
		return nil
	}

	if _, err := postAndWaitForRole(ctx, client, opts, "lead", resolved.Lead, leadPrompt()); err != nil {
		return err
	}

	var reviewerFeedback string
	for round := 1; round <= opts.MaxRounds; round++ {
		if _, err := postAndWaitForRole(ctx, client, opts, "implementer", resolved.Implementer, implementerPrompt(round, reviewerFeedback)); err != nil {
			return err
		}

		comment, err := postAndWaitForRole(ctx, client, opts, "reviewer", resolved.Reviewer, reviewerPrompt(round))
		if err != nil {
			return err
		}
		reviewerFeedback = strVal(comment, "content")
		if reviewerApproved(reviewerFeedback) {
			if err := updateIssueStatus(ctx, client, opts.IssueID, "in_review"); err != nil {
				return err
			}
			fmt.Fprintf(out, "Reviewer approved issue %s after %d round(s)\n", opts.IssueID, round)
			return nil
		}
	}

	if err := updateIssueStatus(ctx, client, opts.IssueID, "blocked"); err != nil {
		return err
	}
	return fmt.Errorf("reviewer did not approve within %d round(s)", opts.MaxRounds)
}

func issueTeamPolicyForIssue(ctx context.Context, client *cli.APIClient, inline string, issue map[string]any, issueID string) (issueTeamPolicy, error) {
	if strings.TrimSpace(inline) != "" {
		return parseIssueTeamPolicy(inline)
	}

	description := strVal(issue, "description")
	if policy, err := parseIssueTeamPolicy(description); err == nil {
		return policy, nil
	}

	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", &comments); err != nil {
		return issueTeamPolicy{}, fmt.Errorf("parse policy from issue description failed and comments could not be loaded: %w", err)
	}

	var b strings.Builder
	b.WriteString(description)
	for _, comment := range comments {
		b.WriteString("\n")
		b.WriteString(strVal(comment, "content"))
	}
	return parseIssueTeamPolicy(b.String())
}

func parseIssueTeamPolicy(text string) (issueTeamPolicy, error) {
	var policy issueTeamPolicy
	normalized := strings.NewReplacer(",", "\n", ";", "\n", "\r\n", "\n").Replace(text)
	rolePattern := regexp.MustCompile(`(?i)(?:^|[\s,;])(lead|implementer|builder|reviewer|until|continue)\s*[:=]\s*([^,;\n]+)`)
	for _, match := range rolePattern.FindAllStringSubmatch(text, -1) {
		applyIssueTeamPolicyValue(&policy, match[1], match[2])
	}
	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "-"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "continue until reviewer approves") {
			policy.Until = "reviewer approves"
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}

		applyIssueTeamPolicyValue(&policy, key, value)
	}
	if policy.Until == "" {
		policy.Until = "reviewer approves"
	}
	if policy.Lead == "" {
		return issueTeamPolicy{}, fmt.Errorf("team policy missing lead")
	}
	if policy.Implementer == "" {
		return issueTeamPolicy{}, fmt.Errorf("team policy missing implementer")
	}
	if policy.Reviewer == "" {
		return issueTeamPolicy{}, fmt.Errorf("team policy missing reviewer")
	}
	return policy, nil
}

func applyIssueTeamPolicyValue(policy *issueTeamPolicy, key, value string) {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.Trim(strings.TrimSpace(value), "`")
	switch key {
	case "lead":
		policy.Lead = value
	case "implementer", "builder":
		policy.Implementer = value
	case "reviewer":
		policy.Reviewer = value
	case "until", "continue":
		policy.Until = strings.TrimPrefix(strings.ToLower(value), "until ")
	}
}

func resolveIssueTeam(ctx context.Context, client *cli.APIClient, policy issueTeamPolicy) (issueTeamResolved, error) {
	lead, err := resolveIssueTeamAgent(ctx, client, policy.Lead)
	if err != nil {
		return issueTeamResolved{}, fmt.Errorf("resolve lead: %w", err)
	}
	implementer, err := resolveIssueTeamAgent(ctx, client, policy.Implementer)
	if err != nil {
		return issueTeamResolved{}, fmt.Errorf("resolve implementer: %w", err)
	}
	reviewer, err := resolveIssueTeamAgent(ctx, client, policy.Reviewer)
	if err != nil {
		return issueTeamResolved{}, fmt.Errorf("resolve reviewer: %w", err)
	}
	return issueTeamResolved{
		Policy:      policy,
		Lead:        lead,
		Implementer: implementer,
		Reviewer:    reviewer,
	}, nil
}

func resolveIssueTeamAgent(ctx context.Context, client *cli.APIClient, name string) (issueTeamAgent, error) {
	kind, id, err := resolveAssignee(ctx, client, name)
	if err != nil {
		return issueTeamAgent{}, err
	}
	if kind != "agent" {
		return issueTeamAgent{}, fmt.Errorf("%q resolved to %s, expected agent", name, kind)
	}
	return issueTeamAgent{Name: name, ID: id}, nil
}

func printIssueTeamDryRun(out io.Writer, issueID string, resolved issueTeamResolved) error {
	plan := map[string]any{
		"issue_id":    issueID,
		"policy":      resolved.Policy,
		"lead":        resolved.Lead,
		"implementer": resolved.Implementer,
		"reviewer":    resolved.Reviewer,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(plan)
}

func postAndWaitForRole(ctx context.Context, client *cli.APIClient, opts issueTeamRunOptions, role string, agent issueTeamAgent, prompt string) (map[string]any, error) {
	startedAt := time.Now().UTC().Add(-2 * time.Second)
	roleComment, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, role, agent, prompt)
	if err != nil {
		return nil, err
	}
	if createdAt, err := time.Parse(time.RFC3339, strVal(roleComment, "created_at")); err == nil {
		startedAt = createdAt
	}
	comment, err := waitForAgentComment(ctx, client, opts.IssueID, agent.ID, startedAt, opts.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("wait for %s: %w", role, err)
	}
	return comment, nil
}

func postIssueTeamRoleComment(ctx context.Context, client *cli.APIClient, issueID, role string, agent issueTeamAgent, prompt string) (map[string]any, error) {
	content := fmt.Sprintf("Team role: %s\n\n%s %s", role, agentMention(agent), prompt)
	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", body, &result); err != nil {
		return nil, fmt.Errorf("post %s comment: %w", role, err)
	}
	return result, nil
}

func waitForAgentComment(ctx context.Context, client *cli.APIClient, issueID, agentID string, after time.Time, pollInterval time.Duration) (map[string]any, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		comment, err := latestAgentCommentSince(ctx, client, issueID, agentID, after)
		if err != nil {
			return nil, err
		}
		if comment != nil {
			return comment, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func latestAgentCommentSince(ctx context.Context, client *cli.APIClient, issueID, agentID string, after time.Time) (map[string]any, error) {
	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", &comments); err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}

	var latest map[string]any
	var latestAt time.Time
	for _, comment := range comments {
		if strVal(comment, "author_type") != "agent" || strVal(comment, "author_id") != agentID {
			continue
		}
		createdAt, err := time.Parse(time.RFC3339, strVal(comment, "created_at"))
		if err != nil {
			continue
		}
		if createdAt.Before(after) {
			continue
		}
		if latest == nil || createdAt.After(latestAt) {
			latest = comment
			latestAt = createdAt
		}
	}
	return latest, nil
}

func updateIssueStatus(ctx context.Context, client *cli.APIClient, issueID, status string) error {
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueID), map[string]any{"status": status}, &result); err != nil {
		return fmt.Errorf("set issue status %s: %w", status, err)
	}
	return nil
}

func agentMention(agent issueTeamAgent) string {
	name := strings.TrimSpace(agent.Name)
	if name == "" {
		name = agent.ID
	}
	return fmt.Sprintf("[@%s](mention://agent/%s)", name, agent.ID)
}

func leadPrompt() string {
	return "Please create a short plan for this issue. Keep the conversation and follow-up work on this same issue. Do not @mention other agents; the team runner will trigger each next role."
}

func implementerPrompt(round int, feedback string) string {
	if strings.TrimSpace(feedback) == "" {
		return fmt.Sprintf("Please implement the current plan for this issue. This is round %d. Post a concise delivery note here when complete.", round)
	}
	return fmt.Sprintf("Please address the reviewer feedback below for round %d, then post a concise delivery note here.\n\nReviewer feedback:\n%s", round, feedback)
}

func reviewerPrompt(round int) string {
	return fmt.Sprintf("Please review the latest work for round %d. Reply with `reviewer_approved` if acceptable. Otherwise list the required changes.", round)
}

func reviewerApproved(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(normalized, "not approved") || strings.Contains(normalized, "changes requested") || strings.Contains(normalized, "needs changes") {
		return false
	}
	return strings.Contains(normalized, "reviewer_approved") ||
		strings.Contains(normalized, "approved") ||
		strings.Contains(normalized, "approve")
}
