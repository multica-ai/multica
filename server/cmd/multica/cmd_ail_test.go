package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/ail"
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
