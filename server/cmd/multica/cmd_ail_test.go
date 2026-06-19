package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
	"github.com/multica-ai/multica/server/internal/cli"
)

func newAilStage2TestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stage2"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("events-path", "", "")
	cmd.Flags().String("output-dir", "", "")
	cmd.Flags().String("emit-categories", "", "")
	cmd.Flags().Int("window-hours", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func writeTestAilEvents(t *testing.T, path string, events []ail.Stage2Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

func setTestFlag(t *testing.T, cmd *cobra.Command, name string, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set flag %s: %v", name, err)
	}
}

func TestRunAilStage2WritesOutputFilesAndJSONStdout(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-1 * time.Minute).Format(time.RFC3339Nano), EventType: "attempt_event", TaskID: "t3", AgentID: "a3", Status: "running"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "stage2_index.jsonl")); err != nil {
		t.Fatalf("stage2_index.jsonl not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "stage2_summary.json")); err != nil {
		t.Fatalf("stage2_summary.json not created: %v", err)
	}

	var result ail.Stage2Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.TotalWindow == 0 {
		t.Fatalf("total_window_events = 0, want > 0")
	}
}

func TestRunAilStage2UsesConfigFileForInputAndEmitCategories(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	configPath := filepath.Join(tmp, "stage2.json")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)
	config := `{"stage1":{"events_path":` + strconv.Quote(eventsPath) + `,"emit_categories":["failure_event"]}}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("config", configPath)
	_ = cmd.Flags().Set("output-dir", outputDir)

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 with config: %v", err)
	}

	var result ail.Stage2Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.TotalInput != 2 {
		t.Fatalf("total_input_events = %d, want 2", result.TotalInput)
	}
	if result.TotalWindow != 1 {
		t.Fatalf("total_window_events = %d, want 1", result.TotalWindow)
	}
	if result.ByEventType["failure_event"] != 1 {
		t.Fatalf("failure_event count = %d, want 1", result.ByEventType["failure_event"])
	}
	if _, ok := result.ByEventType["agent_event"]; ok {
		t.Fatalf("agent_event should be filtered by config emit categories: %#v", result.ByEventType)
	}
}

func TestRunAilStage2FlagsOverrideConfigAndWindowHours(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	configPath := filepath.Join(tmp, "stage2.json")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-2 * time.Hour).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "old", AgentID: "a1", Status: "failed", FailureReason: "old_error"},
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "attempt_event", TaskID: "recent", AgentID: "a2", Status: "running"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "recent-failure", AgentID: "a3", Status: "failed", FailureReason: "recent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)
	config := `{"stage1":{"events_path":` + strconv.Quote(filepath.Join(tmp, "missing.jsonl")) + `,"emit_categories":["failure_event"]}}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("config", configPath)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("emit-categories", "attempt_event")
	_ = cmd.Flags().Set("window-hours", "1")

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 with overrides: %v", err)
	}

	var result ail.Stage2Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.WindowDuration != "1h0m0s" {
		t.Fatalf("window_duration = %q, want 1h0m0s", result.WindowDuration)
	}
	if result.TotalInput != 3 {
		t.Fatalf("total_input_events = %d, want 3", result.TotalInput)
	}
	if result.TotalWindow != 1 {
		t.Fatalf("total_window_events = %d, want 1", result.TotalWindow)
	}
	if result.ByEventType["attempt_event"] != 1 {
		t.Fatalf("attempt_event count = %d, want 1", result.ByEventType["attempt_event"])
	}
	if _, ok := result.ByEventType["failure_event"]; ok {
		t.Fatalf("failure_event should be filtered by emit-categories flag: %#v", result.ByEventType)
	}
}

func TestRunAilStage2TableOutputNoPainBuckets(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 table no buckets: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "total_window") {
		t.Fatalf("table output missing summary line, got: %q", out)
	}
	if !strings.Contains(out, "No pain buckets") {
		t.Fatalf("table output should say no pain buckets, got: %q", out)
	}
}

func TestRunAilStage2TableOutputWithPainBucketsTruncatedToThree(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	outputDir := filepath.Join(tmp, "out")

	// 4 distinct failure reasons exercises the top-3 truncation branch
	reasons := []string{"reason_a", "reason_b", "reason_c", "reason_d"}
	events := make([]ail.Stage2Event, 0, len(reasons))
	for i, r := range reasons {
		events = append(events, ail.Stage2Event{
			TS:            now.Add(-time.Duration(i+1) * time.Minute).Format(time.RFC3339Nano),
			EventType:     "failure_event",
			TaskID:        "t" + strconv.Itoa(i),
			AgentID:       "a1",
			Status:        "failed",
			FailureReason: r,
		})
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilStage2TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage2(cmd, nil); err != nil {
		t.Fatalf("runAilStage2 table with buckets: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "RANK") {
		t.Fatalf("table output missing RANK header, got: %q", out)
	}
}

func TestRunAilStage2ErrorFromInvalidConfigFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(configPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	cmd := newAilStage2TestCmd()
	_ = cmd.Flags().Set("config", configPath)

	if err := runAilStage2(cmd, nil); err == nil {
		t.Fatal("expected error from invalid config, got nil")
	}
}

func TestRunAilStage2ErrorMissingEventsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newAilStage2TestCmd()

	if err := runAilStage2(cmd, nil); err == nil {
		t.Fatal("expected error from missing events file, got nil")
	}
}

// --- Stage 3 CLI tests ---

func newAilStage3TestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stage3"}
	cmd.Flags().String("index-path", "", "")
	cmd.Flags().String("output-dir", "", "")
	cmd.Flags().Int("window-hours", 0, "")
	cmd.Flags().Int("min-signature-count", 0, "")
	cmd.Flags().Int("min-unique-tasks", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newAilRunTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("events-path", "", "")
	cmd.Flags().String("stage2-output-dir", "", "")
	cmd.Flags().String("stage3-output-dir", "", "")
	cmd.Flags().String("emit-categories", "", "")
	cmd.Flags().Int("window-hours", 0, "")
	cmd.Flags().Int("min-signature-count", 0, "")
	cmd.Flags().Int("min-unique-tasks", 0, "")
	cmd.Flags().String("stage5-output-dir", "", "")
	cmd.Flags().String("digest-issue", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newAilReplayTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "replay"}
	cmd.Flags().String("index-path", "", "")
	cmd.Flags().String("output-dir", "", "")
	cmd.Flags().StringArray("event-ids", nil, "")
	cmd.Flags().StringArray("issue-ids", nil, "")
	cmd.Flags().StringArray("agent-ids", nil, "")
	cmd.Flags().String("time-start", "", "")
	cmd.Flags().String("time-end", "", "")
	cmd.Flags().StringArray("failure-reasons", nil, "")
	cmd.Flags().StringArray("loop-signatures", nil, "")
	cmd.Flags().StringArray("tool-args", nil, "")
	cmd.Flags().StringArray("env-keys", nil, "")
	cmd.Flags().String("git-revision", "", "")
	cmd.Flags().String("evaluation-results-path", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newAilStage6TestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stage6"}
	cmd.Flags().String("stage3-digest", "", "")
	cmd.Flags().String("candidate-json", "", "")
	cmd.Flags().String("tool", "", "")
	cmd.Flags().String("prospect-dir", "", "")
	cmd.Flags().String("manifest", "", "")
	cmd.Flags().String("human-approve-ref", "", "")
	cmd.Flags().String("owner", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newAilStage8TestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "stage8"}
	cmd.Flags().String("promotion-log", "", "")
	cmd.Flags().String("index-path", "", "")
	cmd.Flags().String("diagnostics-dir", "", "")
	cmd.Flags().String("candidate-decision-input", "", "")
	cmd.Flags().String("tool", "", "")
	cmd.Flags().String("approve-ref", "", "")
	cmd.Flags().String("promoted-at", "", "")
	cmd.Flags().Int("comparison-window-hours", 0, "")
	cmd.Flags().Int("reevaluate-days", 0, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func writeTestAilIndex(t *testing.T, path string, events []ail.Stage2Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create index file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode index event: %v", err)
		}
	}
}

func TestRunAilStage3WritesOutputFilesAndJSONStdout(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage3out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t3", AgentID: "a2", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilIndex(t, indexPath, events)

	cmd := newAilStage3TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("index-path", indexPath)
	_ = cmd.Flags().Set("output-dir", outputDir)

	if err := runAilStage3(cmd, nil); err != nil {
		t.Fatalf("runAilStage3: %v", err)
	}

	for _, name := range []string{"stage3_digest.json", "stage3_signatures.jsonl", "stage3_watermark.json"} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("%s not created: %v", name, err)
		}
	}

	var result ail.Stage3Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.TotalEvents == 0 {
		t.Fatal("total_window_events = 0, want > 0")
	}
}

func TestRunAilStage3DeterministicStdout(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage3out")

	events := []ail.Stage2Event{
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "tx", AgentID: "a1", Status: "failed", FailureReason: "runtime_offline"},
	}
	writeTestAilIndex(t, indexPath, events)

	runOnce := func() string {
		cmd := newAilStage3TestCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		_ = cmd.Flags().Set("index-path", indexPath)
		_ = cmd.Flags().Set("output-dir", outputDir)
		if err := runAilStage3(cmd, nil); err != nil {
			t.Fatalf("runAilStage3: %v", err)
		}
		return buf.String()
	}

	first := runOnce()
	second := runOnce()
	if first != second {
		t.Fatalf("stdout not deterministic:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestRunAilStage3MissingIndexReturnsError(t *testing.T) {
	tmp := t.TempDir()
	cmd := newAilStage3TestCmd()
	_ = cmd.Flags().Set("index-path", filepath.Join(tmp, "missing.jsonl"))
	_ = cmd.Flags().Set("output-dir", filepath.Join(tmp, "out"))
	if err := runAilStage3(cmd, nil); err == nil {
		t.Fatal("expected error from missing index, got nil")
	}
}

func TestRunAilStage3TableOutputNoPainBuckets(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage3out")

	// Event without failure_reason produces no pain buckets.
	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
	}
	writeTestAilIndex(t, indexPath, events)

	cmd := newAilStage3TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("index-path", indexPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage3(cmd, nil); err != nil {
		t.Fatalf("runAilStage3 table: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "analyzed_at") {
		t.Fatalf("table output missing analyzed_at summary, got: %q", out)
	}
	if !strings.Contains(out, "No pain buckets") {
		t.Fatalf("table output should say no pain buckets, got: %q", out)
	}
}

func TestRunAilStage3TableOutputWithPainBuckets(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage3out")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "bucket_a"},
		{TS: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "bucket_b"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t3", AgentID: "a3", Status: "failed", FailureReason: "bucket_c"},
		{TS: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t4", AgentID: "a4", Status: "failed", FailureReason: "bucket_d"},
	}
	writeTestAilIndex(t, indexPath, events)

	cmd := newAilStage3TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("index-path", indexPath)
	_ = cmd.Flags().Set("output-dir", outputDir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilStage3(cmd, nil); err != nil {
		t.Fatalf("runAilStage3 table with buckets: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "RANK") {
		t.Fatalf("table output missing RANK header, got: %q", out)
	}
}

// --- run (Stage 2 + 3) CLI tests ---

func TestRunAilRunWritesAllArtifactsAndJSONStdout(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "agent_error"},
		{TS: now.Add(-3 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t3", AgentID: "a3", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)

	if err := runAilRun(cmd, nil); err != nil {
		t.Fatalf("runAilRun: %v", err)
	}

	for _, path := range []string{
		filepath.Join(stage2Dir, "stage2_index.jsonl"),
		filepath.Join(stage2Dir, "stage2_summary.json"),
		filepath.Join(stage3Dir, "stage3_digest.json"),
		filepath.Join(stage3Dir, "stage3_signatures.jsonl"),
		filepath.Join(stage3Dir, "stage3_watermark.json"),
		filepath.Join(stage5Dir, "stage5_digest.json"),
		filepath.Join(stage5Dir, "stage5_watermark.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("artifact %q not created: %v", path, err)
		}
	}

	var combined struct {
		Stage2 ail.Stage2Result `json:"stage2"`
		Stage3 ail.Stage3Result `json:"stage3"`
		Stage5 ail.Stage5Digest `json:"stage5"`
	}
	if err := json.Unmarshal(buf.Bytes(), &combined); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if combined.Stage2.TotalWindow == 0 {
		t.Fatal("stage2.total_window_events = 0")
	}
	if combined.Stage3.TotalEvents == 0 {
		t.Fatal("stage3.total_window_events = 0")
	}
	if combined.Stage5.SignalCount == 0 {
		t.Fatal("stage5.signal_count = 0")
	}

	// Watermark must be present in stage3 output.
	wmBytes, err := os.ReadFile(filepath.Join(stage3Dir, "stage3_watermark.json"))
	if err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	var wm ail.Stage3Watermark
	if err := json.Unmarshal(wmBytes, &wm); err != nil {
		t.Fatalf("parse watermark: %v", err)
	}
	if wm.IndexSHA256 == "" {
		t.Fatal("watermark index_sha256 not set")
	}
}

func TestRunAilRunPassesStage2SignatureFieldsAndStage3Thresholds(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")

	events := []ail.Stage2Event{
		{
			TS:             now.Add(-5 * time.Minute).Format(time.RFC3339Nano),
			EventType:      "failure_event",
			TaskID:         "task-1",
			AgentID:        "agent-1",
			Status:         "failed",
			FailureReason:  "agent_error",
			ErrorSignature: "E_PARSE",
			LoopSignature:  "setup_loop",
			RawRef:         "run:1",
		},
		{
			TS:             now.Add(-4 * time.Minute).Format(time.RFC3339Nano),
			EventType:      "failure_event",
			TaskID:         "task-2",
			AgentID:        "agent-2",
			Status:         "failed",
			FailureReason:  "agent_error",
			ErrorSignature: "E_PARSE",
			LoopSignature:  "setup_loop",
			RawRef:         "run:2",
		},
		{
			TS:            now.Add(-3 * time.Minute).Format(time.RFC3339Nano),
			EventType:     "failure_event",
			TaskID:        "task-ignored",
			AgentID:       "agent-3",
			Status:        "failed",
			FailureReason: "runtime_offline",
		},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
	_ = cmd.Flags().Set("min-signature-count", "2")
	_ = cmd.Flags().Set("min-unique-tasks", "2")

	if err := runAilRun(cmd, nil); err != nil {
		t.Fatalf("runAilRun: %v", err)
	}

	var combined struct {
		Stage2 ail.Stage2Result `json:"stage2"`
		Stage3 ail.Stage3Result `json:"stage3"`
	}
	if err := json.Unmarshal(buf.Bytes(), &combined); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if combined.Stage2.TotalWindow != 3 {
		t.Fatalf("stage2.total_window_events = %d, want 3", combined.Stage2.TotalWindow)
	}
	if len(combined.Stage3.CandidateDettools) != 1 {
		t.Fatalf("stage3.candidate_dettools len = %d, want 1", len(combined.Stage3.CandidateDettools))
	}
	cand := combined.Stage3.CandidateDettools[0]
	if cand.SourceSignatureKey != "agent_error::E_PARSE::setup_loop" {
		t.Fatalf("source_signature_key = %q, want agent_error::E_PARSE::setup_loop", cand.SourceSignatureKey)
	}
	if cand.SuggestedName != "detect_agent_error_e_parse_setup_loop" {
		t.Fatalf("suggested_name = %q, want detect_agent_error_e_parse_setup_loop", cand.SuggestedName)
	}

	signaturesBytes, err := os.ReadFile(filepath.Join(stage3Dir, "stage3_signatures.jsonl"))
	if err != nil {
		t.Fatalf("read stage3 signatures: %v", err)
	}
	if !strings.Contains(string(signaturesBytes), `"example_raw_ref":"run:1"`) {
		t.Fatalf("stage3 signatures should preserve raw_ref from Stage 2 index, got:\n%s", signaturesBytes)
	}
}

func TestRunAilRunTableOutput(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")

	events := []ail.Stage2Event{
		{TS: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), EventType: "agent_event", TaskID: "t1", AgentID: "a1", Status: "completed"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
	_ = cmd.Flags().Set("output", "table")

	if err := runAilRun(cmd, nil); err != nil {
		t.Fatalf("runAilRun table: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "stage2:") {
		t.Fatalf("table output missing stage2 summary line, got: %q", out)
	}
	if !strings.Contains(out, "stage3:") {
		t.Fatalf("table output missing stage3 summary line, got: %q", out)
	}
	if !strings.Contains(out, "stage5:") {
		t.Fatalf("table output missing stage5 summary line, got: %q", out)
	}
}

func TestRunAilRunErrorMissingEventsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newAilRunTestCmd()
	if err := runAilRun(cmd, nil); err == nil {
		t.Fatal("expected error from missing events file, got nil")
	}
}

func TestRunAilRunErrorFromInvalidConfigFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(configPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	cmd := newAilRunTestCmd()
	_ = cmd.Flags().Set("config", configPath)

	if err := runAilRun(cmd, nil); err == nil {
		t.Fatal("expected error from invalid config, got nil")
	}
}

func TestRunAilRunErrorWhenStage3AnalyzeFails(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "err"},
	}
	writeTestAilEvents(t, eventsPath, events)

	// Using a regular file as the parent of stage3-output-dir causes os.MkdirAll to fail inside RunStage3Analyze.
	blockingFile := filepath.Join(tmp, "blocking_file")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cmd := newAilRunTestCmd()
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", filepath.Join(blockingFile, "subdir"))

	err := runAilRun(cmd, nil)
	if err == nil {
		t.Fatal("expected error when stage3 output dir parent is a file, got nil")
	}
	if !strings.Contains(err.Error(), "stage3:") {
		t.Fatalf("expected error to contain \"stage3:\", got: %v", err)
	}
}

func TestRunAilRunErrorWhenStage5DigestFails(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	blockingFile := filepath.Join(tmp, "blocking_file")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "err"},
	}
	writeTestAilEvents(t, eventsPath, events)
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("create blocking file: %v", err)
	}

	cmd := newAilRunTestCmd()
	setTestFlag(t, cmd, "events-path", eventsPath)
	setTestFlag(t, cmd, "stage2-output-dir", stage2Dir)
	setTestFlag(t, cmd, "stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", filepath.Join(blockingFile, "subdir"))

	err := runAilRun(cmd, nil)
	if err == nil {
		t.Fatal("expected error when stage5 output dir parent is a file, got nil")
	}
	if !strings.Contains(err.Error(), "stage5:") {
		t.Fatalf("expected error to contain \"stage5:\", got: %v", err)
	}
}

// --- Stage 6 CLI tests ---

func TestRunAilStage6GeneratesCandidateManifestAndJSONStdout(t *testing.T) {
	tmp := t.TempDir()
	prospectDir := filepath.Join(tmp, "prospect")
	candidateJSON := filepath.Join(tmp, "candidate.json")
	contract := ail.Stage5ToolContract{
		Rank:               1,
		SuggestedName:      "detect_agent_error_e_parse",
		SourceSignatureKey: "agent_error::E_PARSE::setup_loop",
		GoSignature:        "func DetectAgentErrorEParse(ctx context.Context, input AgentErrorEParseInput) (AgentErrorEParseOutput, error)",
		ExampleInput: map[string]any{
			"failure_reason":  "agent_error",
			"error_signature": "E_PARSE",
			"loop_signature":  "setup_loop",
			"example_task_id": "task-1",
		},
		ExampleOutput: map[string]any{
			"decision":       "ready_for_candidate",
			"matched":        true,
			"source_cluster": "agent_error::E_PARSE::setup_loop",
		},
		DecisionHint: "ready_for_candidate",
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("marshal contract: %v", err)
	}
	if err := os.WriteFile(candidateJSON, raw, 0o644); err != nil {
		t.Fatalf("write candidate json: %v", err)
	}

	cmd := newAilStage6TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "candidate-json", candidateJSON)
	setTestFlag(t, cmd, "prospect-dir", prospectDir)
	setTestFlag(t, cmd, "human-approve-ref", "PER-12")
	setTestFlag(t, cmd, "owner", "platform")

	if err := runAilStage6(cmd, nil); err != nil {
		t.Fatalf("runAilStage6: %v", err)
	}

	var result ail.Stage6Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.ToolName != "detect_agent_error_e_parse" {
		t.Fatalf("tool_name = %q, want detect_agent_error_e_parse", result.ToolName)
	}
	for _, path := range []string{result.CandidatePath, result.TestPath, result.ManifestPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated path %q not created: %v", path, err)
		}
	}

	manifestBytes, err := os.ReadFile(filepath.Join(prospectDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest ail.Stage6Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if len(manifest.Items) != 1 {
		t.Fatalf("manifest items len = %d, want 1", len(manifest.Items))
	}
	item := manifest.Items[0]
	if item.Status != "candidate" || item.HumanApproveRef != "PER-12" || item.Owner != "platform" || item.SourceClusterID != "agent_error::E_PARSE::setup_loop" {
		t.Fatalf("manifest item not populated correctly: %#v", item)
	}

	goMod := []byte("module prospecttest\n\ngo 1.24.0\n")
	if err := os.WriteFile(filepath.Join(prospectDir, "go.mod"), goMod, 0o644); err != nil {
		t.Fatalf("write generated go.mod: %v", err)
	}
	testCmd := exec.Command("go", "test", ".")
	testCmd.Dir = prospectDir
	out, err := testCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated candidate go test failed: %v\n%s", err, out)
	}
}

func TestRunAilStage6GeneratesFromStage3DigestAndTableOutput(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	stage3Path := filepath.Join(tmp, "stage3_digest.json")
	prospectDir := filepath.Join(tmp, "prospect")
	stage3 := ail.Stage3Result{
		WindowDuration: "24h0m0s",
		TotalEvents:    3,
		RepeatSignatures: []ail.Stage3Signature{
			{Key: "runtime_offline::E_CONN::loop", FailureReason: "runtime_offline", ErrorSignature: "E_CONN", LoopSignature: "loop", Count: 3, UniqueTasks: 2, UniqueAgents: 1, ExampleTaskID: "task-9"},
		},
		CandidateDettools: []ail.Stage3CandidateDettool{
			{SuggestedName: "detect_runtime_offline_e_conn_loop", SourceSignatureKey: "runtime_offline::E_CONN::loop", ExpectedDeterminismGain: 0.8, DecisionHint: "ready_for_review"},
		},
		AnalyzedAt: now.Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(stage3)
	if err != nil {
		t.Fatalf("marshal stage3: %v", err)
	}
	if err := os.WriteFile(stage3Path, raw, 0o644); err != nil {
		t.Fatalf("write stage3 digest: %v", err)
	}

	cmd := newAilStage6TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "stage3-digest", stage3Path)
	setTestFlag(t, cmd, "tool", "detect_runtime_offline_e_conn_loop")
	setTestFlag(t, cmd, "prospect-dir", prospectDir)
	setTestFlag(t, cmd, "human-approve-ref", "review-comment-1")
	setTestFlag(t, cmd, "owner", "eval")
	setTestFlag(t, cmd, "output", "table")

	if err := runAilStage6(cmd, nil); err != nil {
		t.Fatalf("runAilStage6 from stage3: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"stage6:", "candidate", "manifest", "detect_runtime_offline_e_conn_loop"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRunAilStage6ReturnsErrorGivenMissingApprovalFields(t *testing.T) {
	tmp := t.TempDir()
	candidateJSON := filepath.Join(tmp, "candidate.json")
	if err := os.WriteFile(candidateJSON, []byte(`{"suggested_name":"detect_x","source_signature_key":"x","example_input":{"failure_reason":"x"}}`), 0o644); err != nil {
		t.Fatalf("write candidate json: %v", err)
	}

	cmd := newAilStage6TestCmd()
	setTestFlag(t, cmd, "candidate-json", candidateJSON)
	setTestFlag(t, cmd, "owner", "platform")

	err := runAilStage6(cmd, nil)
	if err == nil {
		t.Fatal("expected error from missing human approve ref, got nil")
	}
	if !strings.Contains(err.Error(), "human approve ref") {
		t.Fatalf("error = %v, want missing approval ref", err)
	}
}

func TestRunAilRunPostsStage5DigestOnce(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")
	postCount := 0
	var postedContent string
	comments := []map[string]any{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues/tune-1/comments" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(comments)
		case http.MethodPost:
			postCount++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode post body: %v", err)
			}
			postedContent, _ = body["content"].(string)
			comments = append(comments, map[string]any{"content": postedContent})
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-1"})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv(ailTuningIssueEnv, "")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error", ErrorSignature: "E_TIMEOUT"},
		{TS: now.Add(-4 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t2", AgentID: "a2", Status: "failed", FailureReason: "agent_error", ErrorSignature: "E_TIMEOUT"},
	}
	writeTestAilEvents(t, eventsPath, events)

	run := func() bool {
		cmd := newAilRunTestCmd()
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		_ = cmd.Flags().Set("events-path", eventsPath)
		_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
		_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
		setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
		setTestFlag(t, cmd, "digest-issue", "tune-1")
		_ = cmd.Flags().Set("min-signature-count", "2")
		_ = cmd.Flags().Set("min-unique-tasks", "2")
		if err := runAilRun(cmd, nil); err != nil {
			t.Fatalf("runAilRun: %v", err)
		}
		var result struct {
			Stage5DigestPost bool `json:"stage5_digest_posted"`
		}
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("decode stdout: %v\n%s", err, buf.String())
		}
		return result.Stage5DigestPost
	}

	if !run() {
		t.Fatal("first run should post digest")
	}
	if run() {
		t.Fatal("second run should skip duplicate digest")
	}
	if postCount != 1 {
		t.Fatalf("post count = %d, want 1", postCount)
	}
	if !strings.Contains(postedContent, "Agent Improvement Digest") {
		t.Fatalf("posted digest missing heading: %q", postedContent)
	}
	if !strings.Contains(postedContent, "Suggested tools") {
		t.Fatalf("posted digest missing suggested tools: %q", postedContent)
	}
}

func TestRunAilRunUsesDigestIssueEnvFallback(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")
	postCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues/env-tune/comments" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case http.MethodPost:
			postCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-1"})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv(ailTuningIssueEnv, "env-tune")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
	if err := runAilRun(cmd, nil); err != nil {
		t.Fatalf("runAilRun: %v", err)
	}
	if postCount != 1 {
		t.Fatalf("post count = %d, want 1", postCount)
	}
}

func TestRunAilRunDigestIssueFlagOverridesEnvFallback(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")
	var postedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		postedPath = r.URL.Path
		if r.URL.Path != "/api/issues/flag-tune/comments" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "comment-1"})
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv(ailTuningIssueEnv, "env-tune")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
	setTestFlag(t, cmd, "digest-issue", "flag-tune")
	if err := runAilRun(cmd, nil); err != nil {
		t.Fatalf("runAilRun: %v", err)
	}

	var result struct {
		Stage5DigestPost  bool   `json:"stage5_digest_posted"`
		Stage5DigestIssue string `json:"stage5_digest_issue"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, buf.String())
	}
	if !result.Stage5DigestPost {
		t.Fatal("stage5_digest_posted = false, want true")
	}
	if result.Stage5DigestIssue != "flag-tune" {
		t.Fatalf("stage5_digest_issue = %q, want flag-tune", result.Stage5DigestIssue)
	}
	if postedPath != "/api/issues/flag-tune/comments" {
		t.Fatalf("posted path = %q, want flag digest issue path", postedPath)
	}
}

func TestRunAilRunReturnsErrorWhenStage5DigestWriteFails(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	blockingFile := filepath.Join(tmp, "blocking-stage5")
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	_ = cmd.Flags().Set("events-path", eventsPath)
	_ = cmd.Flags().Set("stage2-output-dir", stage2Dir)
	_ = cmd.Flags().Set("stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", filepath.Join(blockingFile, "stage5"))

	err := runAilRun(cmd, nil)
	if err == nil {
		t.Fatal("expected error when stage5 output dir parent is a file, got nil")
	}
	if !strings.Contains(err.Error(), "stage5:") {
		t.Fatalf("expected error to contain \"stage5:\", got: %v", err)
	}
}

func TestPostAilStage5DigestReturnsListCommentsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/issues/tune-err/comments" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	_, err := postAilStage5Digest(newAilRunTestCmd(), "tune-err", ail.BuildStage5Digest(ail.Stage3Result{WindowDuration: "24h0m0s"}))

	if err == nil {
		t.Fatal("expected list comments error, got nil")
	}
	if !strings.Contains(err.Error(), "list comments") {
		t.Fatalf("error = %v, want list comments context", err)
	}
}

func TestPostAilStage5DigestReturnsClientConstructionError(t *testing.T) {
	original := newAilAPIClient
	t.Cleanup(func() { newAilAPIClient = original })
	newAilAPIClient = func(cmd *cobra.Command) (*cli.APIClient, error) {
		return nil, errors.New("missing client")
	}

	_, err := postAilStage5Digest(newAilRunTestCmd(), "tune-err", ail.BuildStage5Digest(ail.Stage3Result{WindowDuration: "24h0m0s"}))

	if err == nil {
		t.Fatal("expected client construction error, got nil")
	}
	if !strings.Contains(err.Error(), "missing client") {
		t.Fatalf("error = %v, want missing client", err)
	}
}

func TestPostAilStage5DigestReturnsAddCommentError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/issues/tune-err/comments" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case http.MethodPost:
			http.Error(w, "write failed", http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	posted, err := postAilStage5Digest(newAilRunTestCmd(), "tune-err", ail.BuildStage5Digest(ail.Stage3Result{WindowDuration: "24h0m0s"}))

	if err == nil {
		t.Fatal("expected add comment error, got nil")
	}
	if posted {
		t.Fatal("posted = true, want false")
	}
	if !strings.Contains(err.Error(), "add comment") {
		t.Fatalf("error = %v, want add comment context", err)
	}
}

func TestRunAilRunReturnsErrorWhenStage5DigestPostFails(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	eventsPath := filepath.Join(tmp, "events.jsonl")
	stage2Dir := filepath.Join(tmp, "stage2")
	stage3Dir := filepath.Join(tmp, "stage3")
	stage5Dir := filepath.Join(tmp, "diagnostics", "stage5")

	events := []ail.Stage2Event{
		{TS: now.Add(-5 * time.Minute).Format(time.RFC3339Nano), EventType: "failure_event", TaskID: "t1", AgentID: "a1", Status: "failed", FailureReason: "agent_error"},
	}
	writeTestAilEvents(t, eventsPath, events)

	cmd := newAilRunTestCmd()
	setTestFlag(t, cmd, "events-path", eventsPath)
	setTestFlag(t, cmd, "stage2-output-dir", stage2Dir)
	setTestFlag(t, cmd, "stage3-output-dir", stage3Dir)
	setTestFlag(t, cmd, "stage5-output-dir", stage5Dir)
	setTestFlag(t, cmd, "digest-issue", "tune-1")

	err := runAilRun(cmd, nil)

	if err == nil {
		t.Fatal("expected stage5 digest post error, got nil")
	}
	if !strings.Contains(err.Error(), "stage5 digest post") {
		t.Fatalf("error = %v, want stage5 digest post context", err)
	}
}

// --- replay (Stage 7) CLI tests ---

func TestRunAilReplayWritesDecisionAndJSONStdout(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage7")

	events := []ail.Stage2Event{
		{TS: "2026-01-15T08:00:00Z", EventType: "failure_event", TaskID: "task-1", IssueID: "issue-1", AgentID: "agent-1", Status: "failed", FailureReason: "agent_error", LoopSignature: "install_loop"},
		{TS: "2026-01-15T09:00:00Z", EventType: "failure_event", TaskID: "task-2", IssueID: "issue-2", AgentID: "agent-2", Status: "failed", FailureReason: "runtime_offline", LoopSignature: "runtime_loop"},
	}
	writeTestAilIndex(t, indexPath, events)

	selectedID := ail.Stage7EventID(events[0])
	evalPath := filepath.Join(tmp, "eval.jsonl")
	evalLine := `{"event_id":"` + selectedID + `","success_on_retry_before":false,"success_on_retry_after":true,"failed_retries_before":2,"failed_retries_after":0,"actionable":true,"invocation_cost":0.5}` + "\n"
	if err := os.WriteFile(evalPath, []byte(evalLine), 0o644); err != nil {
		t.Fatalf("write eval: %v", err)
	}

	t.Setenv("AIL_REPLAY_ENV", "env-value")
	cmd := newAilReplayTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "output-dir", outputDir)
	setTestFlag(t, cmd, "event-ids", selectedID)
	setTestFlag(t, cmd, "issue-ids", "issue-1")
	setTestFlag(t, cmd, "agent-ids", "agent-1")
	setTestFlag(t, cmd, "time-start", "2026-01-15T07:00:00Z")
	setTestFlag(t, cmd, "time-end", "2026-01-15T09:00:00Z")
	setTestFlag(t, cmd, "failure-reasons", "agent_error")
	setTestFlag(t, cmd, "loop-signatures", "install_loop")
	setTestFlag(t, cmd, "tool-args", "candidate=detect_timeout,mode=replay")
	setTestFlag(t, cmd, "env-keys", "AIL_REPLAY_ENV")
	setTestFlag(t, cmd, "git-revision", "abc123")
	setTestFlag(t, cmd, "evaluation-results-path", evalPath)

	if err := runAilReplay(cmd, nil); err != nil {
		t.Fatalf("runAilReplay: %v", err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "stage7_decision.json")); err != nil {
		t.Fatalf("stage7_decision.json not created: %v", err)
	}

	var result ail.Stage7ReplayDecision
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.EventCount != 1 {
		t.Fatalf("event_count = %d, want 1", result.EventCount)
	}
	if result.Events[0].EventID != selectedID {
		t.Fatalf("event_id = %q, want %q", result.Events[0].EventID, selectedID)
	}
	if result.DeterminismProfile.ToolArgs["candidate"] != "detect_timeout" {
		t.Fatalf("tool args = %#v", result.DeterminismProfile.ToolArgs)
	}
	if result.DeterminismProfile.Env["AIL_REPLAY_ENV"] != "env-value" {
		t.Fatalf("env profile = %#v", result.DeterminismProfile.Env)
	}
	if result.Metrics.RetryReduction != 2 {
		t.Fatalf("retry_reduction = %d, want 2", result.Metrics.RetryReduction)
	}
}

func TestRunAilReplayTableOutput(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage7")
	writeTestAilIndex(t, indexPath, []ail.Stage2Event{
		{TS: "2026-01-15T08:00:00Z", EventType: "agent_event", TaskID: "task-1", IssueID: "issue-1", AgentID: "agent-1", Status: "completed"},
	})

	cmd := newAilReplayTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "output-dir", outputDir)
	setTestFlag(t, cmd, "output", "table")

	if err := runAilReplay(cmd, nil); err != nil {
		t.Fatalf("runAilReplay table: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "replay_id:") {
		t.Fatalf("table output missing replay_id, got: %q", out)
	}
	if !strings.Contains(out, "metrics:") {
		t.Fatalf("table output missing metrics, got: %q", out)
	}
}

func TestRunAilReplayNormalizesCommaSeparatedAndRepeatedFlags(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage7")
	events := []ail.Stage2Event{
		{TS: "2026-01-15T08:00:00Z", EventType: "failure_event", TaskID: "task-1", IssueID: "issue-1", AgentID: "agent-1", Status: "failed", FailureReason: "agent_error", LoopSignature: "install_loop"},
		{TS: "2026-01-15T09:00:00Z", EventType: "failure_event", TaskID: "task-2", IssueID: "issue-2", AgentID: "agent-2", Status: "failed", FailureReason: "runtime_offline", LoopSignature: "runtime_loop"},
	}
	writeTestAilIndex(t, indexPath, []ail.Stage2Event{events[1], events[0]})

	t.Setenv("AIL_CLI_A", "value-a")
	t.Setenv("AIL_CLI_B", "value-b")
	cmd := newAilReplayTestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "output-dir", outputDir)
	setTestFlag(t, cmd, "issue-ids", " issue-2, issue-1 ")
	setTestFlag(t, cmd, "issue-ids", "issue-1")
	setTestFlag(t, cmd, "agent-ids", "agent-2,agent-1")
	setTestFlag(t, cmd, "failure-reasons", " runtime_offline,agent_error ")
	setTestFlag(t, cmd, "loop-signatures", "runtime_loop, install_loop")
	setTestFlag(t, cmd, "tool-args", " candidate = detect_timeout ,mode=replay ")
	setTestFlag(t, cmd, "tool-args", "mode=updated")
	setTestFlag(t, cmd, "env-keys", "AIL_CLI_B, AIL_CLI_A")
	setTestFlag(t, cmd, "env-keys", "AIL_CLI_A")

	if err := runAilReplay(cmd, nil); err != nil {
		t.Fatalf("runAilReplay: %v", err)
	}

	var result ail.Stage7ReplayDecision
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.EventCount != 2 {
		t.Fatalf("event_count = %d, want 2", result.EventCount)
	}
	if got := result.Filters.IssueIDs; len(got) != 2 || got[0] != "issue-1" || got[1] != "issue-2" {
		t.Fatalf("issue IDs = %#v, want normalized issue-1 issue-2", got)
	}
	if result.DeterminismProfile.ToolArgs["candidate"] != "detect_timeout" {
		t.Fatalf("candidate tool arg = %#v", result.DeterminismProfile.ToolArgs)
	}
	if result.DeterminismProfile.ToolArgs["mode"] != "updated" {
		t.Fatalf("mode tool arg = %#v, want updated", result.DeterminismProfile.ToolArgs)
	}
	if result.DeterminismProfile.Env["AIL_CLI_A"] != "value-a" || result.DeterminismProfile.Env["AIL_CLI_B"] != "value-b" {
		t.Fatalf("env profile = %#v", result.DeterminismProfile.Env)
	}
	if result.Events[0].Event.IssueID != "issue-1" || result.Events[1].Event.IssueID != "issue-2" {
		t.Fatalf("events should be sorted by timestamp, got %#v", result.Events)
	}
}

func TestRunAilReplayReturnsTimeEndParseError(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	writeTestAilIndex(t, indexPath, []ail.Stage2Event{
		{TS: "2026-01-15T08:00:00Z", EventType: "failure_event", TaskID: "task-1", IssueID: "issue-1", AgentID: "agent-1", Status: "failed"},
	})

	cmd := newAilReplayTestCmd()
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "time-end", "tomorrow")

	err := runAilReplay(cmd, nil)
	if err == nil {
		t.Fatal("expected time-end parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse time-end") {
		t.Fatalf("error should mention parse time-end, got: %v", err)
	}
}

func TestRunAilReplayInvalidToolArgsReturnsError(t *testing.T) {
	cmd := newAilReplayTestCmd()
	setTestFlag(t, cmd, "tool-args", "missing-equals")

	err := runAilReplay(cmd, nil)
	if err == nil {
		t.Fatal("expected invalid tool-args error, got nil")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Fatalf("error should mention key=value, got: %v", err)
	}
}

func TestRunAilReplayReturnsStage7Error(t *testing.T) {
	tmp := t.TempDir()
	cmd := newAilReplayTestCmd()
	setTestFlag(t, cmd, "index-path", filepath.Join(tmp, "missing.jsonl"))
	setTestFlag(t, cmd, "tool-args", " , ")

	err := runAilReplay(cmd, nil)
	if err == nil {
		t.Fatal("expected stage7 error, got nil")
	}
	if !strings.Contains(err.Error(), "read stage2 index") {
		t.Fatalf("error should mention read stage2 index, got: %v", err)
	}
}

// --- Stage 8 CLI tests ---

func TestRunAilStage8WritesDiagnosticsAndJSONStdout(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	diagnosticsDir := filepath.Join(tmp, "diagnostics")
	promotionLogPath := filepath.Join(diagnosticsDir, "stage8-promotion.jsonl")
	events := []ail.Stage2Event{
		{TS: "2026-01-15T11:00:00Z", EventType: "failure_event", TaskID: "task-1", Status: "failed", FailureReason: "tool_error", RetryCount: 1, DettoolsUsed: []string{"detect_timeout"}},
		{TS: "2026-01-15T12:00:00Z", EventType: "agent_event", TaskID: "task-2", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
	}
	writeTestAilIndex(t, indexPath, events)
	if err := os.MkdirAll(diagnosticsDir, 0o755); err != nil {
		t.Fatalf("mkdir diagnostics: %v", err)
	}
	if err := os.WriteFile(promotionLogPath, []byte(`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout","approve_ref":"PER-14"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}

	cmd := newAilStage8TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "promotion-log", promotionLogPath)
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "diagnostics-dir", diagnosticsDir)
	setTestFlag(t, cmd, "tool", "detect_timeout")
	setTestFlag(t, cmd, "comparison-window-hours", "24")

	if err := runAilStage8(cmd, nil); err != nil {
		t.Fatalf("runAilStage8: %v", err)
	}

	for _, name := range []string{"stage-summary.jsonl", "candidate-decision.json", "rerun-manifest.json", "stage8-promotion.jsonl"} {
		if _, err := os.Stat(filepath.Join(diagnosticsDir, name)); err != nil {
			t.Fatalf("%s not created: %v", name, err)
		}
	}

	var result ail.Stage8Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.ToolName != "detect_timeout" {
		t.Fatalf("tool_name = %q, want detect_timeout", result.ToolName)
	}
	if result.Comparison.PostPromotion.DettoolHitRate != 1 {
		t.Fatalf("post hit rate = %v, want 1", result.Comparison.PostPromotion.DettoolHitRate)
	}
}

func TestRunAilStage8TableOutput(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	diagnosticsDir := filepath.Join(tmp, "diagnostics")
	promotionLogPath := filepath.Join(diagnosticsDir, "stage8-promotion.jsonl")
	writeTestAilIndex(t, indexPath, []ail.Stage2Event{{TS: "2026-01-15T12:00:00Z", EventType: "agent_event", Status: "completed", DettoolsUsed: []string{"detect_timeout"}}})
	if err := os.MkdirAll(diagnosticsDir, 0o755); err != nil {
		t.Fatalf("mkdir diagnostics: %v", err)
	}
	if err := os.WriteFile(promotionLogPath, []byte(`{"ts":"2026-01-15T12:00:00Z","tool_name":"detect_timeout"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write promotion log: %v", err)
	}

	cmd := newAilStage8TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "promotion-log", promotionLogPath)
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "diagnostics-dir", diagnosticsDir)
	setTestFlag(t, cmd, "tool", "detect_timeout")
	setTestFlag(t, cmd, "output", "table")

	if err := runAilStage8(cmd, nil); err != nil {
		t.Fatalf("runAilStage8 table: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "stage8:") {
		t.Fatalf("table output missing stage8 summary, got: %q", out)
	}
	if !strings.Contains(out, "dettool.hit_rate") {
		t.Fatalf("table output missing metric line, got: %q", out)
	}
}

func TestRunAilStage8UsesExplicitPromotionFlagsWithoutPromotionLog(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	diagnosticsDir := filepath.Join(tmp, "diagnostics")
	events := []ail.Stage2Event{
		{TS: "2026-01-15T11:30:00Z", EventType: "agent_event", TaskID: "task-1", Status: "completed"},
		{TS: "2026-01-15T12:00:00Z", EventType: "agent_event", TaskID: "task-2", Status: "completed", DettoolsUsed: []string{"detect_timeout"}},
	}
	writeTestAilIndex(t, indexPath, events)

	cmd := newAilStage8TestCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	setTestFlag(t, cmd, "promotion-log", filepath.Join(tmp, "missing-promotion.jsonl"))
	setTestFlag(t, cmd, "index-path", indexPath)
	setTestFlag(t, cmd, "diagnostics-dir", diagnosticsDir)
	setTestFlag(t, cmd, "tool", " detect_timeout ")
	setTestFlag(t, cmd, "approve-ref", " PER-14 ")
	setTestFlag(t, cmd, "promoted-at", "2026-01-15T12:00:00Z")
	setTestFlag(t, cmd, "comparison-window-hours", "1")
	setTestFlag(t, cmd, "reevaluate-days", "15")

	if err := runAilStage8(cmd, nil); err != nil {
		t.Fatalf("runAilStage8 explicit promotion: %v", err)
	}

	var result ail.Stage8Result
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if result.ToolName != "detect_timeout" {
		t.Fatalf("tool_name = %q, want trimmed detect_timeout", result.ToolName)
	}
	if result.ApproveRef != "PER-14" {
		t.Fatalf("approve_ref = %q, want trimmed PER-14", result.ApproveRef)
	}

	var manifest ail.Stage8RerunManifest
	manifestBytes, err := os.ReadFile(filepath.Join(diagnosticsDir, "rerun-manifest.json"))
	if err != nil {
		t.Fatalf("read rerun manifest: %v", err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse rerun manifest: %v", err)
	}
	if manifest.ReevaluateAfterDays != 15 {
		t.Fatalf("reevaluate_after_days = %d, want 15", manifest.ReevaluateAfterDays)
	}
}

func TestRunAilStage8ReturnsStage8Error(t *testing.T) {
	tmp := t.TempDir()
	cmd := newAilStage8TestCmd()
	setTestFlag(t, cmd, "promotion-log", filepath.Join(tmp, "missing.jsonl"))
	setTestFlag(t, cmd, "index-path", filepath.Join(tmp, "missing-index.jsonl"))
	setTestFlag(t, cmd, "tool", "detect_timeout")

	err := runAilStage8(cmd, nil)
	if err == nil {
		t.Fatal("expected stage8 error, got nil")
	}
	if !strings.Contains(err.Error(), "read promotion log") {
		t.Fatalf("error should mention promotion log, got: %v", err)
	}
}

func TestStage8PromoteScriptAppendsDiagnosticsAndInvokesBundleGenerator(t *testing.T) {
	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repo")
	for _, dir := range []string{
		filepath.Join(repoRoot, "dettools", "prospect"),
		filepath.Join(repoRoot, "skills", "agent-improvement-loop"),
		filepath.Join(repoRoot, "diagnostics", "stage2"),
		filepath.Join(tmp, "bin"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, name := range []string{"analyzer.md", "evaluator.md", "SETUP.md"} {
		if err := os.WriteFile(filepath.Join(repoRoot, "skills", "agent-improvement-loop", name), []byte("# test\n"), 0o644); err != nil {
			t.Fatalf("write skill file %s: %v", name, err)
		}
	}
	candidatePath := filepath.Join(repoRoot, "dettools", "prospect", "detect_timeout_candidate.go")
	if err := os.WriteFile(candidatePath, []byte("package dettools\n"), 0o644); err != nil {
		t.Fatalf("write candidate: %v", err)
	}
	manifestPath := filepath.Join(repoRoot, "dettools", "prospect", "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(`{"items":[{"tool_name":"detect_timeout","tool_file":"detect_timeout_candidate.go","status":"candidate"}]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	indexPath := filepath.Join(repoRoot, "diagnostics", "stage2", "stage2_index.jsonl")
	if err := os.WriteFile(indexPath, []byte(`{"ts":"2026-01-15T12:00:00Z","dettools_used":["detect_timeout"]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write stage2 index: %v", err)
	}
	fakeMulticaPath := filepath.Join(tmp, "bin", "multica")
	callsPath := filepath.Join(tmp, "multica-calls.txt")
	fakeMultica := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(callsPath) + "\n"
	if err := os.WriteFile(fakeMulticaPath, []byte(fakeMultica), 0o755); err != nil {
		t.Fatalf("write fake multica: %v", err)
	}

	cmd := exec.Command("bash", "../../../scripts/stage8-promote.sh",
		"--tool", "detect_timeout",
		"--approve-ref", "PER-14",
		"--approved-by", "tester",
		"--commit", "abc123",
		"--skip-import",
		"--comparison-window-hours", "12",
		"--reevaluate-days", "45",
	)
	cmd.Env = append(os.Environ(),
		"MULTICA_REPO_ROOT="+repoRoot,
		"PATH="+filepath.Join(tmp, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("stage8-promote.sh failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "dettools", "detect_timeout.go")); err != nil {
		t.Fatalf("promoted tool not moved: %v", err)
	}
	diagBytes, err := os.ReadFile(filepath.Join(repoRoot, "diagnostics", "stage8-promotion.jsonl"))
	if err != nil {
		t.Fatalf("read diagnostics log: %v", err)
	}
	if !strings.Contains(string(diagBytes), `"tool_name": "detect_timeout"`) {
		t.Fatalf("diagnostics log missing promoted tool entry:\n%s", diagBytes)
	}
	callsBytes, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatalf("read fake multica calls: %v", err)
	}
	calls := string(callsBytes)
	for _, want := range []string{
		"ail stage8",
		"--promotion-log diagnostics/stage8-promotion.jsonl",
		"--index-path diagnostics/stage2/stage2_index.jsonl",
		"--comparison-window-hours 12",
		"--reevaluate-days 45",
		"--output table",
	} {
		if !strings.Contains(calls, want) {
			t.Fatalf("fake multica call missing %q:\n%s", want, calls)
		}
	}
}
