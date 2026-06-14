package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
	"github.com/multica-ai/multica/server/internal/cli"
)

const ailTuningIssueEnv = "MULTICA_AIL_TUNING_ISSUE_ID"

var newAilAPIClient = newAPIClient

var ailCmd = &cobra.Command{
	Use:   "ail",
	Short: "Agent improvement loop operations",
}

var ailStage2Cmd = &cobra.Command{
	Use:   "stage2",
	Short: "Run Stage 2 capture against a Stage 1 events file and write the index + summary",
	RunE:  runAilStage2,
}

var ailStage3Cmd = &cobra.Command{
	Use:   "stage3",
	Short: "Run Stage 3 analysis against a Stage 2 index and write digest + signatures",
	RunE:  runAilStage3,
}

var ailRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run Stage 2 capture then Stage 3 analysis in one workflow (Option A)",
	RunE:  runAilRun,
}

var ailReplayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Run Stage 7 one-off replay and evaluation against a Stage 2 index",
	RunE:  runAilReplay,
}

var ailStage6Cmd = &cobra.Command{
	Use:   "stage6",
	Short: "Generate a Stage 6 prospect dettool candidate scaffold and manifest entry",
	RunE:  runAilStage6,
}

var ailStage8Cmd = &cobra.Command{
	Use:   "stage8",
	Short: "Write Stage 8 promotion diagnostics and 30-day re-evaluation manifest",
	RunE:  runAilStage8,
}

func init() {
	ailCmd.AddCommand(ailStage2Cmd)
	ailCmd.AddCommand(ailStage3Cmd)
	ailCmd.AddCommand(ailRunCmd)
	ailCmd.AddCommand(ailStage6Cmd)
	ailCmd.AddCommand(ailReplayCmd)
	ailCmd.AddCommand(ailStage8Cmd)

	ailStage2Cmd.Flags().String("config", "", "Path to optional Stage 2 config JSON file (contains stage1.events_path, stage1.emit_categories)")
	ailStage2Cmd.Flags().String("events-path", "", "Path to Stage 1 events JSONL file (overrides config and default)")
	ailStage2Cmd.Flags().String("output-dir", "", "Directory for stage2_index.jsonl and stage2_summary.json output (overrides default)")
	ailStage2Cmd.Flags().String("emit-categories", "", "Comma-separated event types to include (default: agent_event,attempt_event,failure_event)")
	ailStage2Cmd.Flags().Int("window-hours", 0, "Capture window in hours (default: 24)")
	ailStage2Cmd.Flags().String("output", "json", "Output format: json or table")

	ailStage3Cmd.Flags().String("index-path", "", "Path to Stage 2 index JSONL file (default: <stage2 output-dir>/stage2_index.jsonl)")
	ailStage3Cmd.Flags().String("output-dir", "", "Directory for Stage 3 output files (default: ~/diagnostics/stage3)")
	ailStage3Cmd.Flags().Int("window-hours", 0, "Analysis window in hours (default: 24)")
	ailStage3Cmd.Flags().Int("min-signature-count", 0, "Minimum event count for a repeat signature to be a candidate (default: 3)")
	ailStage3Cmd.Flags().Int("min-unique-tasks", 0, "Minimum unique task count for a repeat signature to be a candidate (default: 2)")
	ailStage3Cmd.Flags().String("output", "json", "Output format: json or table")

	ailRunCmd.Flags().String("config", "", "Path to optional Stage 2 config JSON file")
	ailRunCmd.Flags().String("events-path", "", "Path to Stage 1 events JSONL file")
	ailRunCmd.Flags().String("stage2-output-dir", "", "Directory for Stage 2 output files (default: ~/diagnostics/stage2)")
	ailRunCmd.Flags().String("stage3-output-dir", "", "Directory for Stage 3 output files (default: ~/diagnostics/stage3)")
	ailRunCmd.Flags().String("emit-categories", "", "Comma-separated event types to include (default: agent_event,attempt_event,failure_event)")
	ailRunCmd.Flags().Int("window-hours", 0, "Capture/analysis window in hours (default: 24)")
	ailRunCmd.Flags().Int("min-signature-count", 0, "Minimum event count for a repeat signature to be a candidate (default: 3)")
	ailRunCmd.Flags().Int("min-unique-tasks", 0, "Minimum unique task count for a repeat signature to be a candidate (default: 2)")
	ailRunCmd.Flags().String("stage5-output-dir", "", "Directory for Stage 5 digest and watermark output (default: ~/diagnostics/stage5)")
	ailRunCmd.Flags().String("digest-issue", "", "Issue ID to receive the Stage 5 human-readable digest (fallback: MULTICA_AIL_TUNING_ISSUE_ID)")
	ailRunCmd.Flags().String("output", "json", "Output format: json or table")

	ailReplayCmd.Flags().String("index-path", "", "Path to Stage 2 index JSONL file (default: <stage2 output-dir>/stage2_index.jsonl)")
	ailReplayCmd.Flags().String("output-dir", "", "Directory for Stage 7 output files (default: ~/diagnostics/stage7)")
	ailReplayCmd.Flags().StringArray("event-ids", nil, "Event IDs to replay; repeat or comma-separate values")
	ailReplayCmd.Flags().StringArray("issue-ids", nil, "Issue IDs to replay; repeat or comma-separate values")
	ailReplayCmd.Flags().StringArray("agent-ids", nil, "Agent IDs to replay; repeat or comma-separate values")
	ailReplayCmd.Flags().String("time-start", "", "Inclusive UTC replay start time (RFC3339)")
	ailReplayCmd.Flags().String("time-end", "", "Exclusive UTC replay end time (RFC3339)")
	ailReplayCmd.Flags().StringArray("failure-reasons", nil, "Failure reasons to replay; repeat or comma-separate values")
	ailReplayCmd.Flags().StringArray("loop-signatures", nil, "Loop signatures to replay; repeat or comma-separate values")
	ailReplayCmd.Flags().StringArray("tool-args", nil, "Deterministic profile tool arg as key=value; repeat for multiple values")
	ailReplayCmd.Flags().StringArray("env-keys", nil, "Environment keys to snapshot into the deterministic profile")
	ailReplayCmd.Flags().String("git-revision", "", "Git revision to record in the deterministic profile")
	ailReplayCmd.Flags().String("evaluation-results-path", "", "Optional evaluation results JSONL for metrics")
	ailReplayCmd.Flags().String("output", "json", "Output format: json or table")

	ailStage6Cmd.Flags().String("stage3-digest", "", "Path to stage3_digest.json; requires --tool")
	ailStage6Cmd.Flags().String("candidate-json", "", "Path to a Stage 5 recommended tool contract JSON")
	ailStage6Cmd.Flags().String("tool", "", "Suggested tool name to generate or select from --stage3-digest")
	ailStage6Cmd.Flags().String("prospect-dir", "", "Directory for generated prospect files (default: dettools/prospect)")
	ailStage6Cmd.Flags().String("manifest", "", "Path to prospect manifest JSON (default: <prospect-dir>/manifest.json)")
	ailStage6Cmd.Flags().String("human-approve-ref", "", "Required human approval reference for the candidate")
	ailStage6Cmd.Flags().String("owner", "", "Required owner responsible for the candidate")
	ailStage6Cmd.Flags().String("output", "json", "Output format: json or table")

	ailStage8Cmd.Flags().String("promotion-log", "", "Path to diagnostics/stage8-promotion.jsonl")
	ailStage8Cmd.Flags().String("index-path", "", "Path to Stage 2 index JSONL file")
	ailStage8Cmd.Flags().String("diagnostics-dir", "", "Directory for Stage 8 diagnostics bundle")
	ailStage8Cmd.Flags().String("candidate-decision-input", "", "Optional JSON decision payload to embed in candidate-decision.json")
	ailStage8Cmd.Flags().String("tool", "", "Promoted deterministic tool name")
	ailStage8Cmd.Flags().String("approve-ref", "", "Human approval reference")
	ailStage8Cmd.Flags().String("promoted-at", "", "Promotion timestamp (RFC3339); defaults to latest matching promotion log entry")
	ailStage8Cmd.Flags().Int("comparison-window-hours", 0, "Pre/post promotion comparison window in hours (default: 720)")
	ailStage8Cmd.Flags().Int("reevaluate-days", 0, "Days until re-evaluation is due (default: 30)")
	ailStage8Cmd.Flags().String("output", "json", "Output format: json or table")
}

func runAilStage2(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	eventsPath, _ := cmd.Flags().GetString("events-path")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	emitCats, _ := cmd.Flags().GetString("emit-categories")
	windowHours, _ := cmd.Flags().GetInt("window-hours")

	cfg, err := ail.NewStage2ConfigFromArgs(configPath, eventsPath, outputDir, emitCats, windowHours)
	if err != nil {
		return err
	}

	result, err := ail.RunStage2Capture(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage2Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runAilStage3(cmd *cobra.Command, _ []string) error {
	indexPath, _ := cmd.Flags().GetString("index-path")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	windowHours, _ := cmd.Flags().GetInt("window-hours")
	minSigCount, _ := cmd.Flags().GetInt("min-signature-count")
	minUniqueTasks, _ := cmd.Flags().GetInt("min-unique-tasks")

	cfg := ail.NewStage3ConfigFromArgs(indexPath, outputDir, windowHours, minSigCount, minUniqueTasks)

	result, err := ail.RunStage3Analyze(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage3Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runAilRun(cmd *cobra.Command, _ []string) error {
	configPath, _ := cmd.Flags().GetString("config")
	eventsPath, _ := cmd.Flags().GetString("events-path")
	stage2OutputDir, _ := cmd.Flags().GetString("stage2-output-dir")
	emitCats, _ := cmd.Flags().GetString("emit-categories")
	windowHours, _ := cmd.Flags().GetInt("window-hours")

	s2cfg, err := ail.NewStage2ConfigFromArgs(configPath, eventsPath, stage2OutputDir, emitCats, windowHours)
	if err != nil {
		return err
	}

	s2result, err := ail.RunStage2Capture(s2cfg)
	if err != nil {
		return fmt.Errorf("stage2: %w", err)
	}

	stage3OutputDir, _ := cmd.Flags().GetString("stage3-output-dir")
	minSigCount, _ := cmd.Flags().GetInt("min-signature-count")
	minUniqueTasks, _ := cmd.Flags().GetInt("min-unique-tasks")

	s3cfg := ail.NewStage3ConfigFromArgs(s2cfg.IndexFilePath(), stage3OutputDir, windowHours, minSigCount, minUniqueTasks)

	s3result, err := ail.RunStage3Analyze(s3cfg)
	if err != nil {
		return fmt.Errorf("stage3: %w", err)
	}

	stage5OutputDir, _ := cmd.Flags().GetString("stage5-output-dir")
	s5digest, err := ail.RunStage5Digest(ail.Stage5Config{OutputDir: stage5OutputDir}, s3result)
	if err != nil {
		return fmt.Errorf("stage5: %w", err)
	}

	digestIssue, _ := cmd.Flags().GetString("digest-issue")
	if strings.TrimSpace(digestIssue) == "" {
		digestIssue = os.Getenv(ailTuningIssueEnv)
	}
	digestIssue = strings.TrimSpace(digestIssue)
	digestPosted := false
	if digestIssue != "" {
		posted, postErr := postAilStage5Digest(cmd, digestIssue, s5digest)
		if postErr != nil {
			return fmt.Errorf("stage5 digest post: %w", postErr)
		}
		digestPosted = posted
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilRunTable(cmd, s2result, s3result, s5digest, digestPosted)
		return nil
	}
	combined := struct {
		Stage2            ail.Stage2Result `json:"stage2"`
		Stage3            ail.Stage3Result `json:"stage3"`
		Stage5            ail.Stage5Digest `json:"stage5"`
		Stage5DigestPost  bool             `json:"stage5_digest_posted"`
		Stage5DigestIssue string           `json:"stage5_digest_issue,omitempty"`
	}{Stage2: s2result, Stage3: s3result, Stage5: s5digest, Stage5DigestPost: digestPosted, Stage5DigestIssue: digestIssue}
	return cli.PrintJSON(cmd.OutOrStdout(), combined)
}

func runAilReplay(cmd *cobra.Command, _ []string) error {
	indexPath, _ := cmd.Flags().GetString("index-path")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	eventIDs, _ := cmd.Flags().GetStringArray("event-ids")
	issueIDs, _ := cmd.Flags().GetStringArray("issue-ids")
	agentIDs, _ := cmd.Flags().GetStringArray("agent-ids")
	timeStart, _ := cmd.Flags().GetString("time-start")
	timeEnd, _ := cmd.Flags().GetString("time-end")
	failureReasons, _ := cmd.Flags().GetStringArray("failure-reasons")
	loopSignatures, _ := cmd.Flags().GetStringArray("loop-signatures")
	toolArgPairs, _ := cmd.Flags().GetStringArray("tool-args")
	envKeys, _ := cmd.Flags().GetStringArray("env-keys")
	gitRevision, _ := cmd.Flags().GetString("git-revision")
	evaluationResultsPath, _ := cmd.Flags().GetString("evaluation-results-path")

	toolArgs, err := parseAilReplayToolArgs(toolArgPairs)
	if err != nil {
		return err
	}

	cfg := ail.Stage7ReplayConfig{
		IndexPath:             indexPath,
		OutputDir:             outputDir,
		EventIDs:              eventIDs,
		IssueIDs:              issueIDs,
		AgentIDs:              agentIDs,
		TimeStart:             timeStart,
		TimeEnd:               timeEnd,
		FailureReasons:        failureReasons,
		LoopSignatures:        loopSignatures,
		ToolArgs:              toolArgs,
		EnvKeys:               envKeys,
		GitRevision:           gitRevision,
		EvaluationResultsPath: evaluationResultsPath,
	}

	result, err := ail.RunStage7Replay(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilReplayTable(cmd, cfg, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runAilStage6(cmd *cobra.Command, _ []string) error {
	stage3Digest, _ := cmd.Flags().GetString("stage3-digest")
	candidateJSON, _ := cmd.Flags().GetString("candidate-json")
	toolName, _ := cmd.Flags().GetString("tool")
	prospectDir, _ := cmd.Flags().GetString("prospect-dir")
	manifestPath, _ := cmd.Flags().GetString("manifest")
	humanApproveRef, _ := cmd.Flags().GetString("human-approve-ref")
	owner, _ := cmd.Flags().GetString("owner")

	cfg := ail.NewStage6ConfigFromArgs(stage3Digest, candidateJSON, toolName, prospectDir, manifestPath, humanApproveRef, owner)
	result, err := ail.RunStage6Generate(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage6Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func runAilStage8(cmd *cobra.Command, _ []string) error {
	promotionLog, _ := cmd.Flags().GetString("promotion-log")
	indexPath, _ := cmd.Flags().GetString("index-path")
	diagnosticsDir, _ := cmd.Flags().GetString("diagnostics-dir")
	candidateDecisionInput, _ := cmd.Flags().GetString("candidate-decision-input")
	toolName, _ := cmd.Flags().GetString("tool")
	approveRef, _ := cmd.Flags().GetString("approve-ref")
	promotedAt, _ := cmd.Flags().GetString("promoted-at")
	comparisonWindowHours, _ := cmd.Flags().GetInt("comparison-window-hours")
	reevaluateDays, _ := cmd.Flags().GetInt("reevaluate-days")

	cfg := ail.NewStage8ConfigFromArgs(promotionLog, indexPath, diagnosticsDir, candidateDecisionInput, toolName, approveRef, promotedAt, comparisonWindowHours, reevaluateDays)
	result, err := ail.RunStage8Diagnostics(cfg)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAilStage8Table(cmd, result)
		return nil
	}
	return cli.PrintJSON(cmd.OutOrStdout(), result)
}

func postAilStage5Digest(cmd *cobra.Command, issueID string, digest ail.Stage5Digest) (bool, error) {
	client, err := newAilAPIClient(cmd)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cli.APITimeout())
	defer cancel()

	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+issueID+"/comments", &comments); err != nil {
		return false, fmt.Errorf("list comments: %w", err)
	}
	for _, comment := range comments {
		if content, ok := comment["content"].(string); ok && strings.Contains(content, digest.Marker) {
			return false, nil
		}
	}

	body := map[string]any{"content": ail.RenderStage5Comment(digest)}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &result); err != nil {
		return false, fmt.Errorf("add comment: %w", err)
	}
	return true, nil
}

func parseAilReplayToolArgs(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	toolArgs := map[string]string{}
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key, val, ok := strings.Cut(part, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				return nil, fmt.Errorf("tool-args entries must use key=value, got %q", part)
			}
			toolArgs[key] = strings.TrimSpace(val)
		}
	}
	if len(toolArgs) == 0 {
		return nil, nil
	}
	return toolArgs, nil
}

func printAilStage2Table(cmd *cobra.Command, result ail.Stage2Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "generated_at: %s  window: %s  total_window: %d  unique_tasks: %d  unique_agents: %d\n",
		result.GeneratedAt, result.WindowDuration, result.TotalWindow, result.UniqueTasks, result.UniqueAgents)
	top := result.TopPainBuckets
	if len(top) > 3 {
		top = top[:3]
	}
	if len(top) == 0 {
		fmt.Fprintf(w, "No pain buckets in window.\n")
		return
	}
	headers := []string{"RANK", "KEY", "COUNT", "TASKS"}
	rows := make([][]string, 0, len(top))
	for i, b := range top {
		rows = append(rows, []string{strconv.Itoa(i + 1), b.Key, strconv.Itoa(b.Count), strconv.Itoa(b.TaskCount)})
	}
	cli.PrintTable(w, headers, rows)
}

func printAilStage3Table(cmd *cobra.Command, result ail.Stage3Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "analyzed_at: %s  window: %s  total_window: %d  signatures: %d  candidates: %d\n",
		result.AnalyzedAt, result.WindowDuration, result.TotalEvents, len(result.RepeatSignatures), len(result.CandidateDettools))
	top := result.TopPainBuckets
	if len(top) > 3 {
		top = top[:3]
	}
	if len(top) == 0 {
		fmt.Fprintf(w, "No pain buckets in window.\n")
		return
	}
	headers := []string{"RANK", "KEY", "COUNT", "TASKS", "AGENTS"}
	rows := make([][]string, 0, len(top))
	for i, b := range top {
		rows = append(rows, []string{strconv.Itoa(i + 1), b.Key, strconv.Itoa(b.Count), strconv.Itoa(b.UniqueTasks), strconv.Itoa(b.UniqueAgents)})
	}
	cli.PrintTable(w, headers, rows)
}

func printAilRunTable(cmd *cobra.Command, s2 ail.Stage2Result, s3 ail.Stage3Result, s5 ail.Stage5Digest, stage5Posted bool) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "stage2: window=%s total_window=%d unique_tasks=%d\n",
		s2.WindowDuration, s2.TotalWindow, s2.UniqueTasks)
	fmt.Fprintf(w, "stage3: analyzed_at=%s total_events=%d candidates=%d\n",
		s3.AnalyzedAt, s3.TotalEvents, len(s3.CandidateDettools))
	fmt.Fprintf(w, "stage5: marker=%s digest_posted=%t alerts=%d\n",
		s5.Marker, stage5Posted, len(s5.Alerts))
}

func printAilReplayTable(cmd *cobra.Command, cfg ail.Stage7ReplayConfig, result ail.Stage7ReplayDecision) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "replay_id: %s  events: %d  decision: %s\n",
		result.ReplayID, result.EventCount, cfg.Stage7DecisionPath())
	fmt.Fprintf(w, "metrics: success_on_retry_delta=%.4f retry_reduction=%d precision=%.4f invocation_cost=%.4f evaluation_count=%d\n",
		result.Metrics.SuccessOnRetryDelta,
		result.Metrics.RetryReduction,
		result.Metrics.Precision,
		result.Metrics.InvocationCost,
		result.Metrics.EvaluationCount)
}

func printAilStage6Table(cmd *cobra.Command, result ail.Stage6Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "stage6: tool=%s status=%s owner=%s approve_ref=%s\n",
		result.ToolName, result.ManifestEntry.Status, result.ManifestEntry.Owner, result.ManifestEntry.HumanApproveRef)
	headers := []string{"ARTIFACT", "PATH"}
	rows := [][]string{
		{"candidate", result.CandidatePath},
		{"test", result.TestPath},
		{"manifest", result.ManifestPath},
	}
	cli.PrintTable(w, headers, rows)
}

func printAilStage8Table(cmd *cobra.Command, result ail.Stage8Result) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "stage8: tool=%s promoted_at=%s summary=%s\n",
		result.ToolName, result.PromotedAt, result.StageSummaryPath)
	fmt.Fprintf(w, "metrics: dettool.hit_rate %.4f -> %.4f  tool_fail_rate %.4f -> %.4f  retry_ratio_after_tool %.4f -> %.4f\n",
		result.Comparison.PrePromotion.DettoolHitRate,
		result.Comparison.PostPromotion.DettoolHitRate,
		result.Comparison.PrePromotion.ToolFailRate,
		result.Comparison.PostPromotion.ToolFailRate,
		result.Comparison.PrePromotion.RetryRatioAfterTool,
		result.Comparison.PostPromotion.RetryRatioAfterTool)
}
