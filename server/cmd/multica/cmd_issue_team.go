package main

import (
	"context"
	"encoding/json"
	"errors"
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
		if _, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, "implementer", resolved.Implementer, implementerPrompt(1, "", nil)); err != nil {
			return err
		}
		if _, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, "reviewer", resolved.Reviewer, reviewerPrompt(1)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Triggered lead, implementer, and reviewer on issue %s\n", opts.IssueID)
		return nil
	}

	// Load existing state for idempotent resume after crash.
	state, err := loadTeamRunState(ctx, client, opts.IssueID)
	if err != nil {
		return err
	}
	if state == nil {
		state = &teamRunState{}
	}

	if !state.LeadDone {
		if _, err := postAndWaitForRole(ctx, client, opts, "lead", resolved.Lead, leadPrompt()); err != nil {
			return blockOnRoleFailure(ctx, client, opts.IssueID, "lead", err)
		}
		state.LeadDone = true
		if err := saveTeamRunState(ctx, client, opts.IssueID, state); err != nil {
			return err
		}
	}

	for round := 1; round <= opts.MaxRounds; round++ {
		if state.ImplementerDoneRound < round {
			if _, err := postAndWaitForRole(ctx, client, opts, "implementer", resolved.Implementer, implementerPrompt(round, state.ReviewerFeedback, state.ReviewerChanges)); err != nil {
				return blockOnRoleFailure(ctx, client, opts.IssueID, "implementer", err)
			}
			state.ImplementerDoneRound = round
			if err := saveTeamRunState(ctx, client, opts.IssueID, state); err != nil {
				return err
			}
		}

		if state.ReviewerDoneRound < round {
			comment, err := postAndWaitForRole(ctx, client, opts, "reviewer", resolved.Reviewer, reviewerPrompt(round))
			if err != nil {
				return blockOnRoleFailure(ctx, client, opts.IssueID, "reviewer", err)
			}
			state.ReviewerFeedback = strVal(comment, "content")
			verdict, verdictOK := parseReviewerVerdict(state.ReviewerFeedback)
			if verdictOK {
				state.ReviewerChanges = verdict.RequiredChanges
			} else {
				state.ReviewerChanges = nil
			}
			state.ReviewerDoneRound = round
			if err := saveTeamRunState(ctx, client, opts.IssueID, state); err != nil {
				return err
			}
		}

		if reviewerApproved(state.ReviewerFeedback) {
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
	roleComment, err := postIssueTeamRoleComment(ctx, client, opts.IssueID, role, agent, prompt)
	if err != nil {
		return nil, err
	}
	triggerCommentID := strVal(roleComment, "id")
	triggerCreatedAt := time.Now().UTC()
	if createdAt, err := time.Parse(time.RFC3339, strVal(roleComment, "created_at")); err == nil {
		triggerCreatedAt = createdAt
	}

	if err := waitForRunComplete(ctx, client, opts.IssueID, triggerCommentID, agent.ID, opts.PollInterval); err != nil {
		return nil, fmt.Errorf("wait for %s run: %w", role, err)
	}

	comment, err := fetchAgentCommentSince(ctx, client, opts.IssueID, agent.ID, triggerCreatedAt)
	if err != nil {
		return nil, fmt.Errorf("fetch %s response: %w", role, err)
	}
	if comment == nil {
		return nil, fmt.Errorf("no response comment from %s after run completed", role)
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

// agentRunFailedError is returned by waitForRunComplete when the agent run
// for a role transitions to "failed" status. The runner uses this to
// distinguish fast-detected agent crashes from ordinary context timeouts.
type agentRunFailedError struct {
	Reason string
}

func (e *agentRunFailedError) Error() string {
	if e.Reason == "" {
		return "agent run failed"
	}
	return "agent run failed: " + e.Reason
}

// waitForRunComplete polls /task-runs until the task triggered by
// triggerCommentID and owned by agentID reaches a terminal status.
// It returns *agentRunFailedError immediately when the run fails.
// It does not touch the comments endpoint, making it efficient for busy issues.
func waitForRunComplete(ctx context.Context, client *cli.APIClient, issueID, triggerCommentID, agentID string, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		var runs []map[string]any
		if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/task-runs", &runs); err != nil {
			return fmt.Errorf("list task runs: %w", err)
		}
		for _, run := range runs {
			if strVal(run, "trigger_comment_id") != triggerCommentID {
				continue
			}
			if strVal(run, "agent_id") != agentID {
				continue
			}
			switch strVal(run, "status") {
			case "completed":
				return nil
			case "failed", "cancelled":
				reason := strVal(run, "failure_reason")
				if reason == "" {
					reason = strVal(run, "error")
				}
				return &agentRunFailedError{Reason: reason}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// fetchAgentCommentSince retrieves the latest comment from agentID posted after
// the given time, using the ?since= query param to avoid scanning old comments.
func fetchAgentCommentSince(ctx context.Context, client *cli.APIClient, issueID, agentID string, after time.Time) (map[string]any, error) {
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments?since=" + url.QueryEscape(after.Format(time.RFC3339))
	var comments []map[string]any
	if err := client.GetJSON(ctx, path, &comments); err != nil {
		return nil, fmt.Errorf("list comments since: %w", err)
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
		if latest == nil || createdAt.After(latestAt) {
			latest = comment
			latestAt = createdAt
		}
	}
	return latest, nil
}

// blockOnRoleFailure checks whether err is an *agentRunFailedError. If it is,
// it sets the issue to blocked and posts a comment explaining which role failed
// and why, so the human can investigate without hunting through run logs.
// It always returns the original error unchanged.
func blockOnRoleFailure(ctx context.Context, client *cli.APIClient, issueID, role string, err error) error {
	var runErr *agentRunFailedError
	if !errors.As(err, &runErr) {
		return err
	}
	_ = updateIssueStatus(ctx, client, issueID, "blocked")
	content := fmt.Sprintf("Role `%s` agent run failed: %s", role, runErr.Reason)
	var result map[string]any
	_ = client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", map[string]any{"content": content}, &result)
	return err
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

func implementerPrompt(round int, feedback string, requiredChanges []string) string {
	if strings.TrimSpace(feedback) == "" {
		return fmt.Sprintf("Please implement the current plan for this issue. This is round %d. Post a concise delivery note here when complete.", round)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Please address the reviewer feedback below for round %d, then post a concise delivery note here.\n\nReviewer feedback:\n%s", round, feedback)
	if len(requiredChanges) > 0 {
		b.WriteString("\n\nRequired changes:\n")
		for _, c := range requiredChanges {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	return b.String()
}

type reviewerVerdict struct {
	Verdict         string   `json:"verdict"`
	Summary         string   `json:"summary"`
	RequiredChanges []string `json:"required_changes"`
}

var jsonFenceRe = regexp.MustCompile("(?s)```(?:json)?[ \t]*\n([^`]+)```")

func parseReviewerVerdict(text string) (reviewerVerdict, bool) {
	// 1. Try ```json ... ``` fenced blocks
	if matches := jsonFenceRe.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		for _, m := range matches {
			var v reviewerVerdict
			if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &v); err == nil {
				if v.Verdict == "approved" || v.Verdict == "changes_requested" {
					return v, true
				}
			}
		}
	}

	// 2. Try the whole text as raw JSON
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		var v reviewerVerdict
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			if v.Verdict == "approved" || v.Verdict == "changes_requested" {
				return v, true
			}
		}
	}

	return reviewerVerdict{}, false
}

func reviewerApproved(text string) bool {
	v, ok := parseReviewerVerdict(text)
	return ok && v.Verdict == "approved"
}

func reviewerPrompt(round int) string {
	return fmt.Sprintf("Please review the latest work for round %d.\n\nRespond with a JSON verdict in a fenced code block:\n\n```json\n{\n  \"verdict\": \"approved\",\n  \"summary\": \"brief summary\",\n  \"required_changes\": []\n}\n```\n\nUse `\"verdict\": \"approved\"` if the work is acceptable. Use `\"verdict\": \"changes_requested\"` and list each required change in `required_changes` otherwise.", round)
}

// --- durable team-run state (KOR-896) ---

const teamStateMarkerPrefix = "<!-- multica-team-state: "
const teamStateMarkerSuffix = " -->"

// teamRunState tracks the progress of a waiting team run so it can be resumed
// after the CLI process is killed. Persisted as an invisible HTML comment on
// the issue.
type teamRunState struct {
	LeadDone             bool     `json:"lead_done"`
	ImplementerDoneRound int      `json:"implementer_done_round"` // 0 = not started; N = completed through round N
	ReviewerDoneRound    int      `json:"reviewer_done_round"`    // 0 = not started; N = completed through round N
	ReviewerFeedback     string   `json:"reviewer_feedback"`
	ReviewerChanges      []string `json:"reviewer_changes,omitempty"`
	StateCommentID       string   `json:"state_comment_id"` // ID of the persisted state comment
}

// parseTeamRunState extracts runner state from a comment body that contains the
// <!-- multica-team-state: {...} --> marker. Returns nil (no error) when the
// marker is absent or when the JSON is corrupt -- both cases mean start fresh.
func parseTeamRunState(text string) (*teamRunState, error) {
	start := strings.Index(text, teamStateMarkerPrefix)
	if start < 0 {
		return nil, nil
	}
	jsonStart := start + len(teamStateMarkerPrefix)
	end := strings.Index(text[jsonStart:], teamStateMarkerSuffix)
	if end < 0 {
		return nil, nil
	}
	jsonStr := strings.TrimSpace(text[jsonStart : jsonStart+end])
	var state teamRunState
	if err := json.Unmarshal([]byte(jsonStr), &state); err != nil {
		// Corrupt JSON: treat as no state so the runner starts fresh.
		return nil, nil
	}
	return &state, nil
}

// loadTeamRunState scans the issue's comments for a state marker and returns
// the decoded state. Returns nil when no valid state is found (fresh run).
func loadTeamRunState(ctx context.Context, client *cli.APIClient, issueID string) (*teamRunState, error) {
	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", &comments); err != nil {
		return nil, fmt.Errorf("load team state: %w", err)
	}
	for _, comment := range comments {
		content := strVal(comment, "content")
		if !strings.Contains(content, teamStateMarkerPrefix) {
			continue
		}
		state, _ := parseTeamRunState(content)
		if state != nil {
			// Prefer the actual comment ID over what's encoded in the JSON,
			// in case an earlier run saved a stale ID.
			state.StateCommentID = strVal(comment, "id")
			return state, nil
		}
	}
	return nil, nil
}

// saveTeamRunState deletes the previous state comment (best-effort) and posts
// a new one with the updated state JSON.
func saveTeamRunState(ctx context.Context, client *cli.APIClient, issueID string, state *teamRunState) error {
	if state.StateCommentID != "" {
		_ = client.DeleteJSON(ctx, "/api/comments/"+url.PathEscape(state.StateCommentID))
		state.StateCommentID = ""
	}
	jsonBytes, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal team state: %w", err)
	}
	content := teamStateMarkerPrefix + string(jsonBytes) + teamStateMarkerSuffix
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/comments", map[string]any{"content": content}, &result); err != nil {
		return fmt.Errorf("save team state: %w", err)
	}
	state.StateCommentID = strVal(result, "id")
	return nil
}
