package ail

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultStage8DiagnosticsDir      = "diagnostics"
	defaultStage8StageSummaryFile    = "stage-summary.jsonl"
	defaultStage8CandidateDecision   = "candidate-decision.json"
	defaultStage8RerunManifest       = "rerun-manifest.json"
	defaultStage8PromotionLog        = "stage8-promotion.jsonl"
	defaultStage8ComparisonWindow    = 30 * 24 * time.Hour
	defaultStage8ReevaluateAfterDays = 30
	stage8TimerStatusScheduled       = "scheduled"
	stage8TimerStatusDue             = "due"
)

// Stage8Config controls Stage 8 diagnostic bundle generation.
type Stage8Config struct {
	PromotionLogPath           string
	IndexPath                  string
	DiagnosticsDir             string
	CandidateDecisionInputPath string
	ToolName                   string
	ApproveRef                 string
	PromotedAt                 string
	ComparisonWindow           time.Duration
	ReevaluateAfterDays        int
	Now                        func() time.Time
}

// Stage8Result records the files written by the Stage 8 diagnostics bundle.
type Stage8Result struct {
	GeneratedAt           string                    `json:"generated_at"`
	ToolName              string                    `json:"tool_name"`
	ApproveRef            string                    `json:"approve_ref,omitempty"`
	PromotedAt            string                    `json:"promoted_at"`
	Comparison            Stage8TelemetryComparison `json:"telemetry_comparison"`
	StageSummaryPath      string                    `json:"stage_summary_path"`
	CandidateDecisionPath string                    `json:"candidate_decision_path"`
	RerunManifestPath     string                    `json:"rerun_manifest_path"`
	PromotionLogPath      string                    `json:"promotion_log_path"`
}

// Stage8TelemetryComparison compares pre/post-promotion baseline metrics.
type Stage8TelemetryComparison struct {
	ToolName         string               `json:"tool_name"`
	WindowDuration   string               `json:"window_duration"`
	PrePromotion     Stage8TelemetrySlice `json:"pre_promotion"`
	PostPromotion    Stage8TelemetrySlice `json:"post_promotion"`
	MetricDeltas     Stage8TelemetryDelta `json:"metric_deltas"`
	SkippedLineCount int                  `json:"skipped_line_count"`
}

// Stage8TelemetrySlice summarizes a single side of the promotion comparison window.
type Stage8TelemetrySlice struct {
	WindowStart         string  `json:"window_start"`
	WindowEnd           string  `json:"window_end"`
	EventCount          int     `json:"event_count"`
	ToolEventCount      int     `json:"tool_event_count"`
	ToolFailureCount    int     `json:"tool_failure_count"`
	RetryAfterToolCount int     `json:"retry_after_tool_count"`
	RetryAfterToolTotal int     `json:"retry_after_tool_total"`
	DettoolHitRate      float64 `json:"dettool.hit_rate"`
	ToolFailRate        float64 `json:"tool_fail_rate"`
	RetryRatioAfterTool float64 `json:"retry_ratio_after_tool"`
}

// Stage8TelemetryDelta records post-minus-pre changes for the tracked metrics.
type Stage8TelemetryDelta struct {
	DettoolHitRate      float64 `json:"dettool.hit_rate"`
	ToolFailRate        float64 `json:"tool_fail_rate"`
	RetryRatioAfterTool float64 `json:"retry_ratio_after_tool"`
}

// Stage8CandidateDecision records the promotion decision and telemetry evidence.
type Stage8CandidateDecision struct {
	ToolName       string                    `json:"tool_name"`
	ApproveRef     string                    `json:"approve_ref,omitempty"`
	PromotedAt     string                    `json:"promoted_at"`
	Decision       string                    `json:"decision"`
	GeneratedAt    string                    `json:"generated_at"`
	Comparison     Stage8TelemetryComparison `json:"telemetry_comparison"`
	SourceDecision any                       `json:"source_decision,omitempty"`
}

// Stage8RerunManifest makes the 30-day re-evaluation timer observable.
type Stage8RerunManifest struct {
	ToolName              string `json:"tool_name"`
	ApproveRef            string `json:"approve_ref,omitempty"`
	PromotedAt            string `json:"promoted_at"`
	ReevaluateAfterDays   int    `json:"reevaluate_after_days"`
	NextReevaluationAt    string `json:"next_reevaluation_at"`
	TimerStatus           string `json:"timer_status"`
	GeneratedAt           string `json:"generated_at"`
	StageSummaryPath      string `json:"stage_summary_path"`
	CandidateDecisionPath string `json:"candidate_decision_path"`
	PromotionLogPath      string `json:"promotion_log_path"`
}

type stage8PromotionEntry struct {
	TS           string `json:"ts"`
	ToolName     string `json:"tool_name"`
	ApproveRef   string `json:"approve_ref"`
	CommitSHA    string `json:"commit_sha"`
	Imported     bool   `json:"imported"`
	PromotedTool string `json:"promoted_tool"`
}

type stage8SummaryLine struct {
	Event       string                    `json:"event"`
	GeneratedAt string                    `json:"generated_at"`
	ToolName    string                    `json:"tool_name"`
	ApproveRef  string                    `json:"approve_ref,omitempty"`
	PromotedAt  string                    `json:"promoted_at"`
	Comparison  Stage8TelemetryComparison `json:"telemetry_comparison"`
}

// RunStage8Diagnostics writes the Stage 8 diagnostics bundle for a promoted deterministic tool.
func RunStage8Diagnostics(cfg Stage8Config) (Stage8Result, error) {
	cfg = normalizeStage8Config(cfg)
	now := cfg.Now().UTC()
	promotion, err := resolveStage8Promotion(cfg)
	if err != nil {
		return Stage8Result{}, err
	}
	if cfg.ToolName == "" {
		cfg.ToolName = promotion.ToolName
	}
	if cfg.ApproveRef == "" {
		cfg.ApproveRef = promotion.ApproveRef
	}
	if cfg.PromotedAt == "" {
		cfg.PromotedAt = promotion.TS
	}

	promotedAt, err := time.Parse(time.RFC3339Nano, cfg.PromotedAt)
	if err != nil {
		return Stage8Result{}, fmt.Errorf("parse promoted-at: %w", err)
	}
	promotedAt = promotedAt.UTC()

	events, skipped, err := readStage8Events(cfg.IndexPath)
	if err != nil {
		return Stage8Result{}, err
	}

	comparison := buildStage8Comparison(events, skipped, cfg.ToolName, promotedAt, cfg.ComparisonWindow)
	generatedAt := now.Format(time.RFC3339Nano)
	result := Stage8Result{
		GeneratedAt:           generatedAt,
		ToolName:              cfg.ToolName,
		ApproveRef:            cfg.ApproveRef,
		PromotedAt:            promotedAt.Format(time.RFC3339Nano),
		Comparison:            comparison,
		StageSummaryPath:      filepath.Join(cfg.DiagnosticsDir, defaultStage8StageSummaryFile),
		CandidateDecisionPath: filepath.Join(cfg.DiagnosticsDir, defaultStage8CandidateDecision),
		RerunManifestPath:     filepath.Join(cfg.DiagnosticsDir, defaultStage8RerunManifest),
		PromotionLogPath:      cfg.PromotionLogPath,
	}

	sourceDecision, err := readStage8SourceDecision(cfg.CandidateDecisionInputPath)
	if err != nil {
		return result, err
	}
	decision := Stage8CandidateDecision{
		ToolName:       cfg.ToolName,
		ApproveRef:     cfg.ApproveRef,
		PromotedAt:     result.PromotedAt,
		Decision:       "promoted",
		GeneratedAt:    generatedAt,
		Comparison:     comparison,
		SourceDecision: sourceDecision,
	}
	manifest := buildStage8RerunManifest(result, cfg, promotedAt, now)
	summary := stage8SummaryLine{
		Event:       "stage8_summary",
		GeneratedAt: generatedAt,
		ToolName:    cfg.ToolName,
		ApproveRef:  cfg.ApproveRef,
		PromotedAt:  result.PromotedAt,
		Comparison:  comparison,
	}

	if err := writeStage8SummaryJSONL(result.StageSummaryPath, summary); err != nil {
		return result, err
	}
	if err := writeJSON(result.CandidateDecisionPath, decision); err != nil {
		return result, err
	}
	if err := writeJSON(result.RerunManifestPath, manifest); err != nil {
		return result, err
	}
	return result, nil
}

// NewStage8ConfigFromArgs builds a Stage8Config from CLI arguments, applying defaults for unset values.
func NewStage8ConfigFromArgs(promotionLog, indexPath, diagnosticsDir, candidateDecisionInput, toolName, approveRef, promotedAt string, comparisonWindowHours, reevaluateDays int) Stage8Config {
	cfg := Stage8Config{
		PromotionLogPath:           promotionLog,
		IndexPath:                  indexPath,
		DiagnosticsDir:             diagnosticsDir,
		CandidateDecisionInputPath: candidateDecisionInput,
		ToolName:                   strings.TrimSpace(toolName),
		ApproveRef:                 strings.TrimSpace(approveRef),
		PromotedAt:                 strings.TrimSpace(promotedAt),
	}
	if comparisonWindowHours > 0 {
		cfg.ComparisonWindow = time.Duration(comparisonWindowHours) * time.Hour
	}
	if reevaluateDays > 0 {
		cfg.ReevaluateAfterDays = reevaluateDays
	}
	return normalizeStage8Config(cfg)
}

func normalizeStage8Config(cfg Stage8Config) Stage8Config {
	if cfg.DiagnosticsDir == "" {
		cfg.DiagnosticsDir = resolveStage8DiagnosticsDir()
	}
	if cfg.PromotionLogPath == "" {
		cfg.PromotionLogPath = filepath.Join(cfg.DiagnosticsDir, defaultStage8PromotionLog)
	}
	if cfg.IndexPath == "" {
		cfg.IndexPath = filepath.Join(resolveStage2OutputDir(), defaultStage2OutputIndexFile)
	}
	if cfg.ComparisonWindow <= 0 {
		cfg.ComparisonWindow = defaultStage8ComparisonWindow
	}
	if cfg.ReevaluateAfterDays <= 0 {
		cfg.ReevaluateAfterDays = defaultStage8ReevaluateAfterDays
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.ToolName = strings.TrimSpace(cfg.ToolName)
	cfg.ApproveRef = strings.TrimSpace(cfg.ApproveRef)
	cfg.PromotedAt = strings.TrimSpace(cfg.PromotedAt)
	cfg.CandidateDecisionInputPath = strings.TrimSpace(cfg.CandidateDecisionInputPath)
	return cfg
}

func resolveStage8Promotion(cfg Stage8Config) (stage8PromotionEntry, error) {
	if cfg.PromotedAt != "" && cfg.ToolName != "" {
		return stage8PromotionEntry{TS: cfg.PromotedAt, ToolName: cfg.ToolName, ApproveRef: cfg.ApproveRef}, nil
	}
	entries, err := readStage8Promotions(cfg.PromotionLogPath)
	if err != nil {
		return stage8PromotionEntry{}, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if cfg.ToolName == "" || entry.ToolName == cfg.ToolName {
			return entry, nil
		}
	}
	if cfg.ToolName == "" {
		return stage8PromotionEntry{}, fmt.Errorf("promotion log has no usable entries: %s", cfg.PromotionLogPath)
	}
	return stage8PromotionEntry{}, fmt.Errorf("promotion log has no entry for tool %q: %s", cfg.ToolName, cfg.PromotionLogPath)
}

func readStage8Promotions(path string) ([]stage8PromotionEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read promotion log: %w", err)
	}
	defer f.Close()

	entries := make([]stage8PromotionEntry, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry stage8PromotionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entry.ToolName = strings.TrimSpace(entry.ToolName)
		entry.ApproveRef = strings.TrimSpace(entry.ApproveRef)
		entry.TS = strings.TrimSpace(entry.TS)
		if entry.ToolName == "" || entry.TS == "" {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].TS < entries[j].TS
	})
	return entries, nil
}

func readStage8Events(path string) ([]Stage2Event, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read stage2 index: %w", err)
	}
	defer f.Close()

	events := make([]Stage2Event, 0)
	skipped := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var evt Stage2Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			skipped++
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, evt.TS); err != nil {
			skipped++
			continue
		}
		events = append(events, evt)
	}
	if err := scanner.Err(); err != nil {
		return nil, skipped, err
	}
	sort.SliceStable(events, func(i, j int) bool {
		tI, _ := time.Parse(time.RFC3339Nano, events[i].TS)
		tJ, _ := time.Parse(time.RFC3339Nano, events[j].TS)
		if !tI.Equal(tJ) {
			return tI.Before(tJ)
		}
		return Stage7EventID(events[i]) < Stage7EventID(events[j])
	})
	return events, skipped, nil
}

func buildStage8Comparison(events []Stage2Event, skipped int, toolName string, promotedAt time.Time, window time.Duration) Stage8TelemetryComparison {
	preStart := promotedAt.Add(-window)
	postEnd := promotedAt.Add(window)
	pre := buildStage8Slice(events, toolName, preStart, promotedAt)
	post := buildStage8Slice(events, toolName, promotedAt, postEnd)
	return Stage8TelemetryComparison{
		ToolName:       toolName,
		WindowDuration: window.String(),
		PrePromotion:   pre,
		PostPromotion:  post,
		MetricDeltas: Stage8TelemetryDelta{
			DettoolHitRate:      post.DettoolHitRate - pre.DettoolHitRate,
			ToolFailRate:        post.ToolFailRate - pre.ToolFailRate,
			RetryRatioAfterTool: post.RetryRatioAfterTool - pre.RetryRatioAfterTool,
		},
		SkippedLineCount: skipped,
	}
}

func buildStage8Slice(events []Stage2Event, toolName string, start, end time.Time) Stage8TelemetrySlice {
	slice := Stage8TelemetrySlice{
		WindowStart: start.UTC().Format(time.RFC3339Nano),
		WindowEnd:   end.UTC().Format(time.RFC3339Nano),
	}
	for _, evt := range events {
		when, _ := time.Parse(time.RFC3339Nano, evt.TS)
		if when.Before(start) || !when.Before(end) {
			continue
		}
		slice.EventCount++
		if !stage8EventUsesTool(evt, toolName) {
			continue
		}
		slice.ToolEventCount++
		if stage8EventFailed(evt) {
			slice.ToolFailureCount++
		}
		if evt.RetryCount > 0 {
			slice.RetryAfterToolCount++
			slice.RetryAfterToolTotal += int(evt.RetryCount)
		}
	}
	if slice.EventCount > 0 {
		slice.DettoolHitRate = float64(slice.ToolEventCount) / float64(slice.EventCount)
	}
	if slice.ToolEventCount > 0 {
		slice.ToolFailRate = float64(slice.ToolFailureCount) / float64(slice.ToolEventCount)
		slice.RetryRatioAfterTool = float64(slice.RetryAfterToolCount) / float64(slice.ToolEventCount)
	}
	return slice
}

func stage8EventUsesTool(evt Stage2Event, toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return len(evt.DettoolsUsed) > 0
	}
	for _, used := range evt.DettoolsUsed {
		if strings.TrimSpace(used) == toolName {
			return true
		}
	}
	return false
}

func stage8EventFailed(evt Stage2Event) bool {
	return strings.EqualFold(strings.TrimSpace(evt.Status), "failed") || strings.TrimSpace(evt.FailureReason) != ""
}

func readStage8SourceDecision(path string) (any, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read candidate decision input: %w", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse candidate decision input: %w", err)
	}
	return out, nil
}

func buildStage8RerunManifest(result Stage8Result, cfg Stage8Config, promotedAt, now time.Time) Stage8RerunManifest {
	next := promotedAt.AddDate(0, 0, cfg.ReevaluateAfterDays).UTC()
	status := stage8TimerStatusScheduled
	if !now.Before(next) {
		status = stage8TimerStatusDue
	}
	return Stage8RerunManifest{
		ToolName:              result.ToolName,
		ApproveRef:            result.ApproveRef,
		PromotedAt:            result.PromotedAt,
		ReevaluateAfterDays:   cfg.ReevaluateAfterDays,
		NextReevaluationAt:    next.Format(time.RFC3339Nano),
		TimerStatus:           status,
		GeneratedAt:           result.GeneratedAt,
		StageSummaryPath:      result.StageSummaryPath,
		CandidateDecisionPath: result.CandidateDecisionPath,
		PromotionLogPath:      result.PromotionLogPath,
	}
}

func writeStage8SummaryJSONL(path string, summary stage8SummaryLine) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(summary)
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func resolveStage8DiagnosticsDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, defaultStage8DiagnosticsDir)
	}
	return filepath.Join(".", defaultStage8DiagnosticsDir)
}
