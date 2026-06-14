package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeStage8IndexFile(t *testing.T, path string, events []Stage2Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create index file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

func stage8FixtureEvents() []Stage2Event {
	return []Stage2Event{
		{TS: "2026-01-15T10:00:00Z", EventType: "agent_event", TaskID: "pre-plain", AgentID: "agent-1", Status: "completed"},
		{TS: "2026-01-15T11:00:00Z", EventType: "failure_event", TaskID: "pre-tool-failed", AgentID: "agent-1", Status: "failed", FailureReason: "tool_error", RetryCount: 1, DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T12:00:00Z", EventType: "agent_event", TaskID: "post-tool-ok", AgentID: "agent-2", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T13:00:00Z", EventType: "attempt_event", TaskID: "post-tool-ok-2", AgentID: "agent-2", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
	}
}

func TestRunStage8DiagnosticsWritesBundleAndMatchesGoldens(t *testing.T) {
	tmp := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
	indexPath := "stage2_index.jsonl"
	diagnosticsDir := "diagnostics"
	promotionLogPath := filepath.Join(diagnosticsDir, defaultStage8PromotionLog)
	if err := os.MkdirAll(diagnosticsDir, 0o755); err != nil {
		t.Fatalf("mkdir diagnostics: %v", err)
	}
	writeStage8IndexFile(t, indexPath, stage8FixtureEvents())
	promotionLog := `{"ts":"2026-01-15T12:00:00Z","event":"stage8_promotion","tool_name":"detect_timeout","approve_ref":"PER-14","commit_sha":"abc123","imported":true}` + "\n"
	if err := os.WriteFile(promotionLogPath, []byte(promotionLog), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}

	result, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath:    promotionLogPath,
		IndexPath:           indexPath,
		DiagnosticsDir:      diagnosticsDir,
		ToolName:            "detect_timeout",
		ComparisonWindow:    24 * time.Hour,
		ReevaluateAfterDays: 30,
		Now:                 func() time.Time { return time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("RunStage8Diagnostics: %v", err)
	}

	if result.ToolName != "detect_timeout" {
		t.Fatalf("tool_name = %q, want detect_timeout", result.ToolName)
	}
	for _, path := range []string{result.StageSummaryPath, result.CandidateDecisionPath, result.RerunManifestPath, result.PromotionLogPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected artifact %q to exist: %v", path, err)
		}
	}

	assertFileEqualsGolden(t, result.StageSummaryPath, filepath.Join(originalWD, "testdata/stage8/stage_summary_golden.jsonl"))
	assertFileEqualsGolden(t, result.CandidateDecisionPath, filepath.Join(originalWD, "testdata/stage8/candidate_decision_golden.json"))
	assertFileEqualsGolden(t, result.RerunManifestPath, filepath.Join(originalWD, "testdata/stage8/rerun_manifest_golden.json"))
}

func TestRunStage8DiagnosticsEmbedsSourceDecisionAndMarksDueTimer(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	promotionLogPath := filepath.Join(tmp, "diagnostics", defaultStage8PromotionLog)
	decisionPath := filepath.Join(tmp, "stage7_decision.json")
	writeStage8IndexFile(t, indexPath, stage8FixtureEvents())
	if err := os.MkdirAll(filepath.Dir(promotionLogPath), 0o755); err != nil {
		t.Fatalf("mkdir diagnostics: %v", err)
	}
	if err := os.WriteFile(promotionLogPath, []byte(`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}
	if err := os.WriteFile(decisionPath, []byte(`{"decision":"ready_for_candidate","replay_id":"r1"}`), 0o644); err != nil {
		t.Fatalf("write decision input: %v", err)
	}

	result, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath:           promotionLogPath,
		IndexPath:                  indexPath,
		DiagnosticsDir:             filepath.Dir(promotionLogPath),
		CandidateDecisionInputPath: decisionPath,
		ToolName:                   "detect_timeout",
		ComparisonWindow:           24 * time.Hour,
		ReevaluateAfterDays:        1,
		Now:                        func() time.Time { return time.Date(2026, 1, 17, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("RunStage8Diagnostics: %v", err)
	}

	decisionBytes, err := os.ReadFile(result.CandidateDecisionPath)
	if err != nil {
		t.Fatalf("read candidate decision: %v", err)
	}
	if !strings.Contains(string(decisionBytes), `"source_decision"`) {
		t.Fatalf("candidate decision should embed source decision, got:\n%s", decisionBytes)
	}

	var manifest Stage8RerunManifest
	manifestBytes, err := os.ReadFile(result.RerunManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.TimerStatus != stage8TimerStatusDue {
		t.Fatalf("timer_status = %q, want due", manifest.TimerStatus)
	}
}

func TestRunStage8DiagnosticsInfersLatestPromotionEntry(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	promotionLogPath := filepath.Join(tmp, "diagnostics", defaultStage8PromotionLog)
	writeStage8IndexFile(t, indexPath, stage8FixtureEvents())
	if err := os.MkdirAll(filepath.Dir(promotionLogPath), 0o755); err != nil {
		t.Fatalf("mkdir diagnostics: %v", err)
	}
	logLines := strings.Join([]string{
		`{"ts":"2026-01-14T12:00:00Z","tool_name":"old_tool","approve_ref":"OLD"}`,
		`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout","approve_ref":"PER-14"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(promotionLogPath, []byte(logLines), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}

	result, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        indexPath,
		DiagnosticsDir:   filepath.Dir(promotionLogPath),
		Now:              func() time.Time { return time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("RunStage8Diagnostics: %v", err)
	}

	if result.ToolName != "detect_timeout" {
		t.Fatalf("tool_name = %q, want latest promotion tool", result.ToolName)
	}
	if result.ApproveRef != "PER-14" {
		t.Fatalf("approve_ref = %q, want PER-14", result.ApproveRef)
	}
}

func TestRunStage8DiagnosticsAllowsExplicitPromotionWithoutLog(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	diagnosticsDir := filepath.Join(tmp, "diagnostics")
	writeStage8IndexFile(t, indexPath, stage8FixtureEvents())

	result, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath:    filepath.Join(tmp, "missing-promotion.jsonl"),
		IndexPath:           indexPath,
		DiagnosticsDir:      diagnosticsDir,
		ToolName:            " detect_timeout ",
		ApproveRef:          " PER-14 ",
		PromotedAt:          "2026-01-15T12:00:00Z",
		ComparisonWindow:    24 * time.Hour,
		ReevaluateAfterDays: 30,
		Now:                 func() time.Time { return time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("RunStage8Diagnostics explicit promotion: %v", err)
	}

	if result.ToolName != "detect_timeout" {
		t.Fatalf("tool_name = %q, want trimmed detect_timeout", result.ToolName)
	}
	if result.ApproveRef != "PER-14" {
		t.Fatalf("approve_ref = %q, want trimmed PER-14", result.ApproveRef)
	}
	if _, err := os.Stat(result.StageSummaryPath); err != nil {
		t.Fatalf("stage summary not written: %v", err)
	}
}

func TestBuildStage8ComparisonUsesHalfOpenWindowsAndExactTool(t *testing.T) {
	promotedAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	events := []Stage2Event{
		{TS: "2026-01-15T10:00:00Z", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T10:59:59Z", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T11:00:00Z", Status: "completed", DettoolsUsed: []string{" detect_timeout "}},
		{TS: "2026-01-15T11:30:00Z", Status: "completed", DettoolsUsed: []string{"other_tool"}},
		{TS: "2026-01-15T12:00:00Z", Status: "failed", FailureReason: "tool_error", RetryCount: 2, DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T12:30:00Z", Status: "completed", DettoolsUsed: []string{"other_tool"}},
		{TS: "2026-01-15T12:59:59Z", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T13:00:00Z", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
	}

	comparison := buildStage8Comparison(events, 3, "detect_timeout", promotedAt, time.Hour)

	if comparison.SkippedLineCount != 3 {
		t.Fatalf("skipped_line_count = %d, want 3", comparison.SkippedLineCount)
	}
	if comparison.PrePromotion.EventCount != 2 {
		t.Fatalf("pre event count = %d, want 2", comparison.PrePromotion.EventCount)
	}
	if comparison.PrePromotion.ToolEventCount != 1 {
		t.Fatalf("pre tool event count = %d, want 1", comparison.PrePromotion.ToolEventCount)
	}
	if comparison.PrePromotion.DettoolHitRate != 0.5 {
		t.Fatalf("pre hit rate = %v, want 0.5", comparison.PrePromotion.DettoolHitRate)
	}
	if comparison.PostPromotion.EventCount != 3 {
		t.Fatalf("post event count = %d, want 3", comparison.PostPromotion.EventCount)
	}
	if comparison.PostPromotion.ToolEventCount != 2 {
		t.Fatalf("post tool event count = %d, want 2", comparison.PostPromotion.ToolEventCount)
	}
	if comparison.PostPromotion.ToolFailRate != 0.5 {
		t.Fatalf("post tool fail rate = %v, want 0.5", comparison.PostPromotion.ToolFailRate)
	}
	if comparison.PostPromotion.RetryRatioAfterTool != 0.5 {
		t.Fatalf("post retry ratio = %v, want 0.5", comparison.PostPromotion.RetryRatioAfterTool)
	}
}

func TestBuildStage8ComparisonHandlesEmptyWindowsWithoutNaN(t *testing.T) {
	promotedAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	comparison := buildStage8Comparison(nil, 0, "detect_timeout", promotedAt, time.Hour)

	if comparison.PrePromotion.DettoolHitRate != 0 || comparison.PostPromotion.DettoolHitRate != 0 {
		t.Fatalf("empty hit rates should be zero: %#v", comparison)
	}
	if comparison.PrePromotion.ToolFailRate != 0 || comparison.PostPromotion.ToolFailRate != 0 {
		t.Fatalf("empty fail rates should be zero: %#v", comparison)
	}
	if comparison.PrePromotion.RetryRatioAfterTool != 0 || comparison.PostPromotion.RetryRatioAfterTool != 0 {
		t.Fatalf("empty retry ratios should be zero: %#v", comparison)
	}
}

func TestRunStage8DiagnosticsReturnsErrorsForMissingInputs(t *testing.T) {
	tmp := t.TempDir()
	_, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: filepath.Join(tmp, "missing-promotion.jsonl"),
		IndexPath:        filepath.Join(tmp, "missing-index.jsonl"),
		ToolName:         "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected missing promotion log error, got nil")
	}
	if !strings.Contains(err.Error(), "read promotion log") {
		t.Fatalf("error should mention promotion log, got: %v", err)
	}

	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: filepath.Join(tmp, "missing-promotion.jsonl"),
		IndexPath:        filepath.Join(tmp, "missing-index.jsonl"),
		ToolName:         "detect_timeout",
		PromotedAt:       "bad-time",
	})
	if err == nil {
		t.Fatal("expected bad promoted-at error, got nil")
	}
	if !strings.Contains(err.Error(), "parse promoted-at") {
		t.Fatalf("error should mention promoted-at, got: %v", err)
	}

	promotionLogPath := filepath.Join(tmp, "stage8-promotion.jsonl")
	if err := os.WriteFile(promotionLogPath, []byte(`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}
	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        filepath.Join(tmp, "missing-index.jsonl"),
		ToolName:         "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected missing index error, got nil")
	}
	if !strings.Contains(err.Error(), "read stage2 index") {
		t.Fatalf("error should mention stage2 index, got: %v", err)
	}
}

func TestRunStage8DiagnosticsReturnsErrorsForBadFiles(t *testing.T) {
	tmp := t.TempDir()
	promotionLogPath := filepath.Join(tmp, "stage8-promotion.jsonl")
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	decisionPath := filepath.Join(tmp, "decision.json")
	if err := os.WriteFile(promotionLogPath, []byte(`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}
	if err := os.WriteFile(indexPath, []byte(`{"ts":"2026-01-15T12:00:00Z","dettools_used":["detect_timeout"]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(decisionPath, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	_, err := RunStage8Diagnostics(Stage8Config{
		PromotionLogPath:           promotionLogPath,
		IndexPath:                  indexPath,
		DiagnosticsDir:             filepath.Join(tmp, "diagnostics"),
		CandidateDecisionInputPath: decisionPath,
		ToolName:                   "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected bad source decision error, got nil")
	}
	if !strings.Contains(err.Error(), "parse candidate decision input") {
		t.Fatalf("error should mention candidate decision input, got: %v", err)
	}

	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath:           promotionLogPath,
		IndexPath:                  indexPath,
		DiagnosticsDir:             filepath.Join(tmp, "diagnostics"),
		CandidateDecisionInputPath: filepath.Join(tmp, "missing-decision.json"),
		ToolName:                   "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected missing source decision error, got nil")
	}
	if !strings.Contains(err.Error(), "read candidate decision input") {
		t.Fatalf("error should mention candidate decision input read, got: %v", err)
	}

	blockingFile := filepath.Join(tmp, "blocking")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        indexPath,
		DiagnosticsDir:   filepath.Join(blockingFile, "diagnostics"),
		ToolName:         "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected artifact write error, got nil")
	}

	decisionDir := filepath.Join(tmp, "decision-write", defaultStage8CandidateDecision)
	if err := os.MkdirAll(decisionDir, 0o755); err != nil {
		t.Fatalf("mkdir decision dir: %v", err)
	}
	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        indexPath,
		DiagnosticsDir:   filepath.Dir(decisionDir),
		ToolName:         "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected candidate decision write error, got nil")
	}

	manifestDir := filepath.Join(tmp, "manifest-write", defaultStage8RerunManifest)
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        indexPath,
		DiagnosticsDir:   filepath.Dir(manifestDir),
		ToolName:         "detect_timeout",
	})
	if err == nil {
		t.Fatal("expected rerun manifest write error, got nil")
	}
}

func TestReadStage8EventsSkipsMalformedRowsAndReportsScannerError(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := readStage8Events(filepath.Join(tmp, "missing.jsonl")); err == nil {
		t.Fatal("expected missing index open error, got nil")
	}

	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	lines := strings.Join([]string{
		"",
		"not-json",
		`{"ts":"not-a-time"}`,
		`{"ts":"2026-01-15T12:00:00Z","dettools_used":["detect_timeout"]}`,
		`{"ts":"2026-01-15T12:00:00Z","dettools_used":["other_tool"]}`,
	}, "\n")
	if err := os.WriteFile(indexPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	events, skipped, err := readStage8Events(indexPath)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if skipped != 2 {
		t.Fatalf("skipped = %d, want 2", skipped)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}

	longPath := filepath.Join(tmp, "long.jsonl")
	if err := os.WriteFile(longPath, []byte(strings.Repeat("x", 70*1024)), 0o644); err != nil {
		t.Fatalf("write long index: %v", err)
	}
	_, _, err = readStage8Events(longPath)
	if err == nil {
		t.Fatal("expected scanner error for long line, got nil")
	}
}

func TestReadStage8PromotionsSkipsBadRowsAndReportsErrors(t *testing.T) {
	tmp := t.TempDir()
	promotionLogPath := filepath.Join(tmp, "stage8-promotion.jsonl")
	lines := strings.Join([]string{
		"",
		"not-json",
		`{"ts":"","tool_name":"missing_ts"}`,
		`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout","approve_ref":"PER-14"}`,
	}, "\n")
	if err := os.WriteFile(promotionLogPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}

	entries, err := readStage8Promotions(promotionLogPath)
	if err != nil {
		t.Fatalf("read promotions: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}

	longPath := filepath.Join(tmp, "long-promotion.jsonl")
	if err := os.WriteFile(longPath, []byte(strings.Repeat("x", 70*1024)), 0o644); err != nil {
		t.Fatalf("write long promotion log: %v", err)
	}
	if _, err := readStage8Promotions(longPath); err == nil {
		t.Fatal("expected scanner error for long promotion log line, got nil")
	}

	emptyPath := filepath.Join(tmp, "empty.jsonl")
	if err := os.WriteFile(emptyPath, []byte("not-json\n"), 0o644); err != nil {
		t.Fatalf("write empty promotion log: %v", err)
	}
	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: emptyPath,
		IndexPath:        filepath.Join(tmp, "missing-index.jsonl"),
	})
	if err == nil {
		t.Fatal("expected no usable promotions error, got nil")
	}
	if !strings.Contains(err.Error(), "no usable entries") {
		t.Fatalf("error should mention no usable entries, got: %v", err)
	}

	_, err = RunStage8Diagnostics(Stage8Config{
		PromotionLogPath: promotionLogPath,
		IndexPath:        filepath.Join(tmp, "missing-index.jsonl"),
		ToolName:         "missing_tool",
	})
	if err == nil {
		t.Fatal("expected missing tool promotion error, got nil")
	}
	if !strings.Contains(err.Error(), "no entry for tool") {
		t.Fatalf("error should mention missing tool entry, got: %v", err)
	}
}

func TestStage8EventUsesToolHandlesEmptyAndMissingToolNames(t *testing.T) {
	evt := Stage2Event{DettoolsUsed: []string{" detect_timeout "}}
	if !stage8EventUsesTool(evt, "") {
		t.Fatal("empty requested tool should match any dettool usage")
	}
	if !stage8EventUsesTool(evt, "detect_timeout") {
		t.Fatal("trimmed dettool name should match")
	}
	if stage8EventUsesTool(evt, "other_tool") {
		t.Fatal("unexpected dettool match")
	}
	if stage8EventUsesTool(Stage2Event{}, "") {
		t.Fatal("empty requested tool should not match when no dettools were used")
	}
}

func TestNewStage8ConfigFromArgsUsesDefaultsAndFlags(t *testing.T) {
	cfg := NewStage8ConfigFromArgs("promo.jsonl", "index.jsonl", "diag", "decision.json", " detect_timeout ", " PER-14 ", "2026-01-15T12:00:00Z", 2, 7)
	if cfg.PromotionLogPath != "promo.jsonl" || cfg.IndexPath != "index.jsonl" || cfg.DiagnosticsDir != "diag" {
		t.Fatalf("paths not preserved: %#v", cfg)
	}
	if cfg.ToolName != "detect_timeout" || cfg.ApproveRef != "PER-14" {
		t.Fatalf("strings not trimmed: %#v", cfg)
	}
	if cfg.ComparisonWindow != 2*time.Hour || cfg.ReevaluateAfterDays != 7 {
		t.Fatalf("numeric flags not applied: %#v", cfg)
	}
}

func TestNormalizeStage8ConfigUsesDefaults(t *testing.T) {
	t.Setenv("HOME", "/tmp/stage8-home")
	cfg := normalizeStage8Config(Stage8Config{})
	if !strings.HasSuffix(cfg.DiagnosticsDir, filepath.Join("diagnostics")) {
		t.Fatalf("diagnostics dir = %q, want diagnostics suffix", cfg.DiagnosticsDir)
	}
	if !strings.HasSuffix(cfg.PromotionLogPath, filepath.Join("diagnostics", defaultStage8PromotionLog)) {
		t.Fatalf("promotion log = %q", cfg.PromotionLogPath)
	}
	if !strings.HasSuffix(cfg.IndexPath, filepath.Join("diagnostics", "stage2", "stage2_index.jsonl")) {
		t.Fatalf("index path = %q", cfg.IndexPath)
	}
	if cfg.ComparisonWindow != defaultStage8ComparisonWindow {
		t.Fatalf("comparison window = %s, want default", cfg.ComparisonWindow)
	}
	if cfg.ReevaluateAfterDays != defaultStage8ReevaluateAfterDays {
		t.Fatalf("reevaluate days = %d, want default", cfg.ReevaluateAfterDays)
	}
	if cfg.Now == nil {
		t.Fatal("Now default should be populated")
	}
}

func TestResolveStage8DiagnosticsDirWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := resolveStage8DiagnosticsDir(); got != filepath.Join(".", defaultStage8DiagnosticsDir) {
		t.Fatalf("diagnostics dir = %q, want relative default", got)
	}
}

func assertFileEqualsGolden(t *testing.T, gotPath, goldenPath string) {
	t.Helper()
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("read got file %s: %v", gotPath, err)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s does not match %s\ngot:\n%s\nwant:\n%s", gotPath, goldenPath, got, want)
	}
}
