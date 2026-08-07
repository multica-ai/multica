package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const (
	followupDispositionAutoContinue  = "auto-continue"
	followupDispositionNeedsApproval = "needs-approval"
	followupDispositionNeedsInfo     = "needs-info"
	followupDispositionDone          = "done"
)

var (
	validFollowupKinds = map[string]bool{
		"worker_task":    true,
		"approval":       true,
		"review_gate":    true,
		"implementation": true,
		"research":       true,
		"cleanup":        true,
	}
	validFollowupDispositions = map[string]bool{
		followupDispositionAutoContinue:  true,
		followupDispositionNeedsApproval: true,
		followupDispositionNeedsInfo:     true,
		followupDispositionDone:          true,
	}
	validFollowupRiskLevels = map[string]bool{
		"low": true, "medium": true, "high": true,
	}
	escapeLiteralRegexp = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)
	agentMemberMention  = regexp.MustCompile(`mention://(agent|member)/[0-9a-fA-F-]+`)
)

var issueFollowupCmd = &cobra.Command{
	Use:   "followup",
	Short: "Create and list structured follow-up issues",
}

var issueFollowupCreateCmd = &cobra.Command{
	Use:   "create <parent>",
	Short: "Create or return a structured follow-up child issue",
	Args:  exactArgs(1),
	RunE:  runIssueFollowupCreate,
}

var issueFollowupListCmd = &cobra.Command{
	Use:   "list <issue>",
	Short: "List structured follow-ups for a parent issue",
	Args:  exactArgs(1),
	RunE:  runIssueFollowupList,
}

func init() {
	issueFollowupCmd.AddCommand(issueFollowupCreateCmd)
	issueFollowupCmd.AddCommand(issueFollowupListCmd)
	issueCmd.AddCommand(issueFollowupCmd)

	issueFollowupCreateCmd.Flags().String("title", "", "Follow-up issue title (required)")
	issueFollowupCreateCmd.Flags().String("kind", "", "Follow-up kind: worker_task, approval, review_gate, implementation, research, cleanup (required)")
	issueFollowupCreateCmd.Flags().String("disposition", "", "Follow-up disposition: auto-continue, needs-approval, needs-info, done (required)")
	issueFollowupCreateCmd.Flags().String("recommended-worker", "none", "Recommended worker: Claude, Codex, Gemini, Copilot, Hermes, or none")
	issueFollowupCreateCmd.Flags().String("risk-level", "medium", "Risk level: low, medium, or high")
	issueFollowupCreateCmd.Flags().String("approval-ask", "", "One-line approval question (required for needs-approval)")
	issueFollowupCreateCmd.Flags().String("info-ask", "", "One-line information request (required for needs-info)")
	issueFollowupCreateCmd.Flags().String("dedupe-key", "", "Stable ASCII dedupe key")
	issueFollowupCreateCmd.Flags().String("done-condition", "", "Done condition markdown")
	issueFollowupCreateCmd.Flags().String("description", "", "Additional issue description markdown")
	issueFollowupCreateCmd.Flags().Bool("description-stdin", false, "Read additional issue description from stdin")
	issueFollowupCreateCmd.Flags().String("description-file", "", "Read additional issue description from a UTF-8 file")
	issueFollowupCreateCmd.Flags().String("linked-pr-url", "", "Linked GitHub PR URL")
	issueFollowupCreateCmd.Flags().String("linked-comment-id", "", "Source comment UUID")
	issueFollowupCreateCmd.Flags().StringSlice("label", nil, "Label ID or UUID prefix to attach (repeatable)")
	issueFollowupCreateCmd.Flags().String("assignee", "", "Assignee name (member, agent, or squad; fuzzy match)")
	issueFollowupCreateCmd.Flags().String("assignee-id", "", "Assignee UUID — member, agent, or squad")
	issueFollowupCreateCmd.Flags().String("status", "", "Issue status override")
	issueFollowupCreateCmd.Flags().String("parent-comment", "", "Parent summary comment override")
	issueFollowupCreateCmd.Flags().Bool("no-parent-comment", false, "Do not add a parent summary comment")
	issueFollowupCreateCmd.Flags().Bool("allow-mention", false, "Allow agent/member mentions in --parent-comment")
	issueFollowupCreateCmd.Flags().Bool("plan-first", false, "Add Execution Mode: Plan-first. to the child description")
	issueFollowupCreateCmd.Flags().String("output", "json", "Output format: table or json")
	issueFollowupCreateCmd.Flags().Bool("quiet", false, "Suppress table output on success")

	issueFollowupListCmd.Flags().StringSlice("disposition", nil, "Filter by disposition (repeatable or comma-separated)")
	issueFollowupListCmd.Flags().StringSlice("lifecycle", nil, "Filter by lifecycle (repeatable or comma-separated)")
	issueFollowupListCmd.Flags().Bool("include-done", false, "Include completed follow-ups")
	issueFollowupListCmd.Flags().Bool("include-cancelled", false, "Include cancelled issues")
	issueFollowupListCmd.Flags().Bool("recursive", false, "Reserved for future recursive listing")
	issueFollowupListCmd.Flags().String("output", "table", "Output format: table or json")
	issueFollowupListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	issueFollowupListCmd.Flags().Int("limit", 100, "Maximum number of follow-ups to return")
}

type followupCreateOptions struct {
	ParentRef         resolvedID
	Title             string
	Kind              string
	Disposition       string
	RecommendedWorker string
	RiskLevel         string
	ApprovalAsk       string
	InfoAsk           string
	DedupeKey         string
	DoneCondition     string
	Description       string
	LinkedPRURL       string
	LinkedCommentID   string
	Labels            []string
	Status            string
	ParentComment     string
	NoParentComment   bool
	AllowMention      bool
	PlanFirst         bool
}

type followupCreateResult struct {
	Issue    map[string]any `json:"issue"`
	Deduped  bool           `json:"deduped"`
	Metadata map[string]any `json:"metadata"`
	Comment  map[string]any `json:"comment,omitempty"`
}

type followupListResult struct {
	Parent map[string]any              `json:"parent"`
	Groups map[string][]map[string]any `json:"groups"`
	Counts map[string]int              `json:"counts"`
}

func runIssueFollowupCreate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	parentRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve parent issue: %w", err)
	}
	opts, err := parseFollowupCreateOptions(cmd, parentRef)
	if err != nil {
		return err
	}
	if err := validateFollowupTextInputs(opts); err != nil {
		return err
	}

	if opts.DedupeKey != "" {
		existing, err := findActiveFollowupByDedupe(ctx, client, parentRef.ID, opts.DedupeKey)
		if err != nil {
			return fmt.Errorf("dedupe lookup: %w", err)
		}
		if existing != nil {
			result := followupCreateResult{
				Issue:    existing,
				Deduped:  true,
				Metadata: metadataMap(existing),
			}
			return printFollowupCreateResult(cmd, result)
		}
	} else {
		fmt.Fprintln(os.Stderr, "warning: --dedupe-key not set; duplicate follow-ups may be created.")
	}

	child, err := createFollowupIssue(ctx, client, cmd, opts)
	if err != nil {
		return err
	}
	metadata := buildFollowupMetadata(opts)
	if err := setFollowupMetadata(ctx, client, strVal(child, "id"), metadata); err != nil {
		_ = markFollowupMetadataFailure(ctx, client, strVal(child, "id"))
		return fmt.Errorf("set follow-up metadata: %w", err)
	}
	if err := attachFollowupLabels(ctx, client, strVal(child, "id"), opts.Labels); err != nil {
		return err
	}

	var comment map[string]any
	if !opts.NoParentComment {
		content := opts.ParentComment
		if content == "" {
			content = defaultFollowupParentComment(opts, child)
		}
		if err := validateParentCommentMentions(content, opts.AllowMention); err != nil {
			return err
		}
		created, err := addFollowupParentComment(ctx, client, parentRef.ID, content)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: add parent follow-up comment failed: %v\n", err)
		} else {
			comment = created
		}
	}

	result := followupCreateResult{
		Issue:    child,
		Deduped:  false,
		Metadata: metadata,
		Comment:  comment,
	}
	return printFollowupCreateResult(cmd, result)
}

func parseFollowupCreateOptions(cmd *cobra.Command, parentRef resolvedID) (followupCreateOptions, error) {
	title, _ := cmd.Flags().GetString("title")
	kind, _ := cmd.Flags().GetString("kind")
	disposition, _ := cmd.Flags().GetString("disposition")
	riskLevel, _ := cmd.Flags().GetString("risk-level")
	desc, _, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return followupCreateOptions{}, err
	}
	opts := followupCreateOptions{
		ParentRef:         parentRef,
		Title:             title,
		Kind:              kind,
		Disposition:       disposition,
		RecommendedWorker: mustGetStringFlag(cmd, "recommended-worker"),
		RiskLevel:         riskLevel,
		ApprovalAsk:       mustGetStringFlag(cmd, "approval-ask"),
		InfoAsk:           mustGetStringFlag(cmd, "info-ask"),
		DedupeKey:         mustGetStringFlag(cmd, "dedupe-key"),
		DoneCondition:     mustGetStringFlag(cmd, "done-condition"),
		Description:       desc,
		LinkedPRURL:       mustGetStringFlag(cmd, "linked-pr-url"),
		LinkedCommentID:   mustGetStringFlag(cmd, "linked-comment-id"),
		Labels:            mustGetStringSliceFlag(cmd, "label"),
		Status:            mustGetStringFlag(cmd, "status"),
		ParentComment:     mustGetStringFlag(cmd, "parent-comment"),
		NoParentComment:   mustGetBoolFlag(cmd, "no-parent-comment"),
		AllowMention:      mustGetBoolFlag(cmd, "allow-mention"),
		PlanFirst:         mustGetBoolFlag(cmd, "plan-first"),
	}
	if opts.Title == "" {
		return opts, fmt.Errorf("--title is required")
	}
	if !validFollowupKinds[opts.Kind] {
		return opts, fmt.Errorf("--kind must be one of: worker_task, approval, review_gate, implementation, research, cleanup")
	}
	if !validFollowupDispositions[opts.Disposition] {
		return opts, fmt.Errorf("--disposition must be one of: auto-continue, needs-approval, needs-info, done")
	}
	if !validFollowupRiskLevels[opts.RiskLevel] {
		return opts, fmt.Errorf("--risk-level must be one of: low, medium, high")
	}
	if opts.Disposition == followupDispositionNeedsApproval && opts.ApprovalAsk == "" {
		return opts, fmt.Errorf("--approval-ask is required when --disposition=needs-approval")
	}
	if opts.Disposition == followupDispositionNeedsInfo && opts.InfoAsk == "" {
		return opts, fmt.Errorf("--info-ask is required when --disposition=needs-info")
	}
	if opts.Status == "" {
		opts.Status = defaultFollowupStatus(opts.Disposition)
	}
	return opts, nil
}

func validateFollowupTextInputs(opts followupCreateOptions) error {
	values := map[string]string{
		"title":          opts.Title,
		"approval-ask":   opts.ApprovalAsk,
		"info-ask":       opts.InfoAsk,
		"dedupe-key":     opts.DedupeKey,
		"done-condition": opts.DoneCondition,
		"description":    opts.Description,
		"parent-comment": opts.ParentComment,
	}
	for name, value := range values {
		if value == "" {
			continue
		}
		if !utf8.ValidString(value) {
			return fmt.Errorf("--%s must be valid UTF-8", name)
		}
		if escapeLiteralRegexp.MatchString(value) {
			return fmt.Errorf("escape literal '\\uXXXX' detected in --%s; pass raw UTF-8 instead", name)
		}
	}
	if opts.ParentComment != "" {
		return validateParentCommentMentions(opts.ParentComment, opts.AllowMention)
	}
	return nil
}

func validateParentCommentMentions(content string, allow bool) error {
	if allow {
		return nil
	}
	if agentMemberMention.MatchString(content) {
		return fmt.Errorf("agent/member mentions are blocked in follow-up parent comments; pass --allow-mention only when you intentionally want to notify someone")
	}
	return nil
}

func findActiveFollowupByDedupe(ctx context.Context, client *cli.APIClient, parentID, dedupeKey string) (map[string]any, error) {
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	params.Set("limit", "50")
	filter, err := buildMetadataFilterQueryParam([]string{
		"source_issue_id=" + parentID,
		"followup_dedupe_key=" + dedupeKey,
	})
	if err != nil {
		return nil, err
	}
	params.Set("metadata", filter)
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
		return nil, err
	}
	issues, _ := result["issues"].([]any)
	for _, raw := range issues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status := strVal(issue, "status")
		if status == "done" || status == "cancelled" {
			continue
		}
		return issue, nil
	}
	return nil, nil
}

func createFollowupIssue(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, opts followupCreateOptions) (map[string]any, error) {
	body := map[string]any{
		"title":           opts.Title,
		"description":     renderFollowupDescription(opts),
		"parent_issue_id": opts.ParentRef.ID,
		"status":          opts.Status,
		"allow_duplicate": true,
	}
	aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		return nil, fmt.Errorf("create follow-up issue: %w", err)
	}
	return result, nil
}

func setFollowupMetadata(ctx context.Context, client *cli.APIClient, issueID string, metadata map[string]any) error {
	for key, value := range metadata {
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal metadata %s: %w", key, err)
		}
		body := map[string]any{"value": json.RawMessage(raw)}
		if err := client.PutJSON(ctx, "/api/issues/"+issueID+"/metadata/"+key, body, nil); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

func markFollowupMetadataFailure(ctx context.Context, client *cli.APIClient, issueID string) error {
	body := map[string]any{"status": "blocked"}
	return client.PutJSON(ctx, "/api/issues/"+issueID, body, nil)
}

func attachFollowupLabels(ctx context.Context, client *cli.APIClient, issueID string, labels []string) error {
	for _, label := range labels {
		labelRef, err := resolveLabelID(ctx, client, label)
		if err != nil {
			return fmt.Errorf("resolve label %q: %w", label, err)
		}
		body := map[string]any{"label_id": labelRef.ID}
		if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/labels", body, nil); err != nil {
			return fmt.Errorf("attach label %q: %w", label, err)
		}
	}
	return nil
}

func addFollowupParentComment(ctx context.Context, client *cli.APIClient, parentID, content string) (map[string]any, error) {
	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+parentID+"/comments", body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func buildFollowupMetadata(opts followupCreateOptions) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	metadata := map[string]any{
		"followup_disposition": opts.Disposition,
		"followup_lifecycle":   defaultFollowupLifecycle(opts.Disposition),
		"followup_kind":        opts.Kind,
		"recommended_worker":   opts.RecommendedWorker,
		"risk_level":           opts.RiskLevel,
		"source_issue_id":      opts.ParentRef.ID,
		"last_disposition_at":  now,
	}
	if opts.ApprovalAsk != "" {
		metadata["approval_ask"] = opts.ApprovalAsk
	}
	if opts.InfoAsk != "" {
		metadata["info_ask"] = opts.InfoAsk
	}
	if opts.LinkedCommentID != "" {
		metadata["source_comment_id"] = opts.LinkedCommentID
	}
	if opts.LinkedPRURL != "" {
		metadata["linked_pr_url"] = opts.LinkedPRURL
	}
	if opts.DedupeKey != "" {
		metadata["followup_dedupe_key"] = opts.DedupeKey
	}
	return metadata
}

func renderFollowupDescription(opts followupCreateOptions) string {
	doneCondition := opts.DoneCondition
	if doneCondition == "" {
		doneCondition = "- Follow-up result is posted back to the parent issue."
	}
	executionMode := "Direct."
	if opts.PlanFirst {
		executionMode = "Plan-first."
	}
	parts := []string{
		"## Context\nParent issue: [" + opts.ParentRef.Display + "](mention://issue/" + opts.ParentRef.ID + ")",
		"## Goal\n" + opts.Title,
		"## Done Condition\n" + doneCondition,
		"## Execution Mode\n" + executionMode,
		"## Safety\n- Do not use destructive commands such as `rm`, `rmdir`, or `git rm`.\n- Do not force push, rebase, rewrite history, or access credentials.\n- Stop for approval on product, security, payment, schema, API, or migration risk.",
		"## Return Format\n- 변경 요약\n- CLI 사용 예시\n- 테스트/검증 결과\n- PR 또는 diff 위치\n- 남은 리스크/후속 작업",
	}
	if opts.Description != "" {
		parts = append(parts, "## Additional Context\n"+opts.Description)
	}
	if opts.DedupeKey != "" {
		parts = append(parts, "<!-- followup-dedupe-key: "+opts.DedupeKey+" -->")
	}
	return strings.Join(parts, "\n\n")
}

func defaultFollowupStatus(disposition string) string {
	switch disposition {
	case followupDispositionAutoContinue:
		return "todo"
	case followupDispositionNeedsApproval, followupDispositionNeedsInfo:
		return "blocked"
	case followupDispositionDone:
		return "done"
	default:
		return "backlog"
	}
}

func defaultFollowupLifecycle(disposition string) string {
	switch disposition {
	case followupDispositionAutoContinue:
		return "routed"
	case followupDispositionNeedsApproval:
		return "waiting_approval"
	case followupDispositionNeedsInfo:
		return "blocked"
	case followupDispositionDone:
		return "completed"
	default:
		return "proposed"
	}
}

func defaultFollowupParentComment(opts followupCreateOptions, child map[string]any) string {
	prefix := map[string]string{
		followupDispositionAutoContinue:  "자동 진행 중:",
		followupDispositionNeedsApproval: "승인 필요:",
		followupDispositionNeedsInfo:     "막힘:",
		followupDispositionDone:          "추가 액션 없음:",
	}[opts.Disposition]
	key := issueDisplayKey(child)
	if key == "" {
		key = strVal(child, "id")
	}
	return fmt.Sprintf("%s [%s](mention://issue/%s) — %s", prefix, key, strVal(child, "id"), opts.Title)
}

func printFollowupCreateResult(cmd *cobra.Command, result followupCreateResult) error {
	quiet, _ := cmd.Flags().GetBool("quiet")
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if quiet {
		return nil
	}
	headers := []string{"KEY", "STATUS", "DISPOSITION", "LIFECYCLE", "DEDUPED", "TITLE"}
	rows := [][]string{{
		issueDisplayKey(result.Issue),
		strVal(result.Issue, "status"),
		fmt.Sprintf("%v", result.Metadata["followup_disposition"]),
		fmt.Sprintf("%v", result.Metadata["followup_lifecycle"]),
		fmt.Sprintf("%v", result.Deduped),
		strVal(result.Issue, "title"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueFollowupList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	parentRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve parent issue: %w", err)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	issues, err := listFollowupIssues(ctx, client, parentRef.ID, limit)
	if err != nil {
		return err
	}
	dispositionFilter := stringSetFromCSVFlags(mustGetStringSliceFlag(cmd, "disposition"))
	lifecycleFilter := stringSetFromCSVFlags(mustGetStringSliceFlag(cmd, "lifecycle"))
	includeDone, _ := cmd.Flags().GetBool("include-done")
	includeCancelled, _ := cmd.Flags().GetBool("include-cancelled")
	groups := groupFollowups(issues, dispositionFilter, lifecycleFilter, includeDone, includeCancelled)
	result := followupListResult{
		Parent: map[string]any{"id": parentRef.ID, "key": parentRef.Display},
		Groups: groups,
		Counts: followupCounts(groups),
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printFollowupListTable(result, fullID)
	return nil
}

func listFollowupIssues(ctx context.Context, client *cli.APIClient, parentID string, limit int) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	params.Set("limit", fmt.Sprintf("%d", limit))
	filter, err := buildMetadataFilterQueryParam([]string{"source_issue_id=" + parentID})
	if err != nil {
		return nil, err
	}
	params.Set("metadata", filter)
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
		return nil, fmt.Errorf("list follow-up issues: %w", err)
	}
	raw, _ := result["issues"].([]any)
	issues := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if issue, ok := item.(map[string]any); ok {
			issues = append(issues, issue)
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		return followupSortKey(issues[i]) < followupSortKey(issues[j])
	})
	return issues, nil
}

func groupFollowups(issues []map[string]any, dispositionFilter, lifecycleFilter map[string]bool, includeDone, includeCancelled bool) map[string][]map[string]any {
	groups := map[string][]map[string]any{
		followupDispositionAutoContinue:  {},
		followupDispositionNeedsApproval: {},
		followupDispositionNeedsInfo:     {},
		followupDispositionDone:          {},
	}
	for _, issue := range issues {
		status := strVal(issue, "status")
		if status == "cancelled" && !includeCancelled {
			continue
		}
		metadata := metadataMap(issue)
		disposition := stringFromAny(metadata["followup_disposition"])
		lifecycle := stringFromAny(metadata["followup_lifecycle"])
		if disposition == "" {
			continue
		}
		if disposition == followupDispositionDone && !includeDone {
			continue
		}
		if len(dispositionFilter) > 0 && !dispositionFilter[disposition] {
			continue
		}
		if len(lifecycleFilter) > 0 && !lifecycleFilter[lifecycle] {
			continue
		}
		groups[disposition] = append(groups[disposition], issue)
	}
	return groups
}

func followupCounts(groups map[string][]map[string]any) map[string]int {
	counts := make(map[string]int, len(groups))
	for k, v := range groups {
		counts[k] = len(v)
	}
	return counts
}

func printFollowupListTable(result followupListResult, fullID bool) {
	fmt.Fprintf(os.Stdout, "Next Actions for %s\n\n", stringFromAny(result.Parent["key"]))
	headers := []string{"DISPOSITION", "COUNT", "KEY", "LIFECYCLE", "RISK", "TITLE"}
	if fullID {
		headers = []string{"DISPOSITION", "COUNT", "ID", "KEY", "LIFECYCLE", "RISK", "TITLE"}
	}
	rows := [][]string{}
	for _, disposition := range []string{followupDispositionAutoContinue, followupDispositionNeedsApproval, followupDispositionNeedsInfo, followupDispositionDone} {
		items := result.Groups[disposition]
		if len(items) == 0 {
			row := []string{disposition, "0", "", "", "", ""}
			if fullID {
				row = []string{disposition, "0", "", "", "", "", ""}
			}
			rows = append(rows, row)
			continue
		}
		for i, issue := range items {
			count := ""
			if i == 0 {
				count = fmt.Sprintf("%d", len(items))
			}
			metadata := metadataMap(issue)
			row := []string{
				disposition,
				count,
				issueDisplayKey(issue),
				stringFromAny(metadata["followup_lifecycle"]),
				stringFromAny(metadata["risk_level"]),
				strVal(issue, "title"),
			}
			if fullID {
				row = []string{
					disposition,
					count,
					strVal(issue, "id"),
					issueDisplayKey(issue),
					stringFromAny(metadata["followup_lifecycle"]),
					stringFromAny(metadata["risk_level"]),
					strVal(issue, "title"),
				}
			}
			rows = append(rows, row)
		}
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func metadataMap(issue map[string]any) map[string]any {
	metadata, _ := issue["metadata"].(map[string]any)
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func stringSetFromCSVFlags(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out[part] = true
			}
		}
	}
	return out
}

func followupSortKey(issue map[string]any) string {
	metadata := metadataMap(issue)
	lifecycle := stringFromAny(metadata["followup_lifecycle"])
	return fmt.Sprintf("%02d:%s", followupLifecycleRank(lifecycle), strVal(issue, "updated_at"))
}

func followupLifecycleRank(lifecycle string) int {
	switch lifecycle {
	case "waiting_approval":
		return 0
	case "blocked":
		return 1
	case "running":
		return 2
	case "routed":
		return 3
	case "in_review":
		return 4
	case "completed":
		return 5
	case "cancelled":
		return 6
	default:
		return 9
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func mustGetStringFlag(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func mustGetBoolFlag(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func mustGetStringSliceFlag(cmd *cobra.Command, name string) []string {
	v, _ := cmd.Flags().GetStringSlice(name)
	return v
}
