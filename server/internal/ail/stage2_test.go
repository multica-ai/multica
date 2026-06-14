package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunStage2CaptureWritesIndexAndSummary(t *testing.T) {
	testNow := time.Now().UTC().Truncate(time.Second)
	t.Run("capture, sort, and summarize", func(t *testing.T) {
		tmp := t.TempDir()
		inputPath := filepath.Join(tmp, "stage1-events.jsonl")
		outputDir := filepath.Join(tmp, "diagnostics", "stage2")

		events := []Stage2Event{
			{
				TS:            testNow.Add(-10 * time.Minute).Format(time.RFC3339Nano),
				EventType:     "attempt_event",
				WorkspaceID:   "ws-1",
				AgentID:       "agent-1",
				IssueID:       "iss-1",
				TaskID:        "task-1",
				Status:        "running",
				Attempt:       1,
				MaxAttempts:   3,
				FailureReason: "agent_error",
			},
			{
				TS:            testNow.Add(-5 * time.Minute).Format(time.RFC3339Nano),
				EventType:     "failure_event",
				WorkspaceID:   "ws-1",
				AgentID:       "agent-1",
				IssueID:       "iss-1",
				TaskID:        "task-1",
				Status:        "failed",
				Attempt:       1,
				MaxAttempts:   3,
				FailureReason: "agent_error",
			},
			{
				TS:            testNow.Add(-4 * time.Minute).Format(time.RFC3339Nano),
				EventType:     "failure_event",
				WorkspaceID:   "ws-1",
				AgentID:       "agent-2",
				IssueID:       "iss-2",
				TaskID:        "task-2",
				Status:        "failed",
				Attempt:       1,
				MaxAttempts:   3,
				FailureReason: "runtime_offline",
			},
			{
				TS:            testNow.Add(-70 * time.Hour).Format(time.RFC3339Nano),
				EventType:     "failure_event",
				WorkspaceID:   "ws-old",
				AgentID:       "agent-old",
				IssueID:       "iss-old",
				TaskID:        "task-old",
				Status:        "failed",
				Attempt:       1,
				MaxAttempts:   3,
				FailureReason: "stale",
			},
		}

		f, err := os.Create(inputPath)
		if err != nil {
			t.Fatalf("create input file: %v", err)
		}
		enc := json.NewEncoder(f)
		for _, evt := range events {
			if err := enc.Encode(evt); err != nil {
				t.Fatalf("encode event: %v", err)
			}
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close input file: %v", err)
		}

		cfg := Stage2Config{
			InputPath:      inputPath,
			OutputDir:      outputDir,
			WindowDuration: 24 * time.Hour,
			TopN:           10,
		}
		result, err := RunStage2Capture(cfg)
		if err != nil {
			t.Fatalf("run capture: %v", err)
		}

		if result.TotalWindow != 3 { // one stale row filtered by window
			t.Fatalf("total_window = %d, want 3", result.TotalWindow)
		}
		if result.TotalInput != 4 {
			t.Fatalf("total_input = %d, want 4", result.TotalInput)
		}
		if result.ByEventType["failure_event"] != 2 {
			t.Fatalf("failure_event count = %d, want 2", result.ByEventType["failure_event"])
		}
		if result.UniqueTasks != 2 {
			t.Fatalf("unique tasks = %d, want 2", result.UniqueTasks)
		}
		if len(result.TopPainBuckets) != 2 {
			t.Fatalf("top buckets len = %d, want 2", len(result.TopPainBuckets))
		}
		if result.TopPainBuckets[0].FailureReason == "" {
			t.Fatalf("top bucket missing failure reason")
		}

		indexPath := filepath.Join(outputDir, "stage2_index.jsonl")
		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatalf("read index file: %v", err)
		}
		indexLines := strings.Count(string(indexData), "\n")
		if indexLines != 3 {
			t.Fatalf("index lines = %d, want 3", indexLines)
		}

		summaryPath := filepath.Join(outputDir, "stage2_summary.json")
		if _, err := os.Stat(summaryPath); err != nil {
			t.Fatalf("summary file should exist: %v", err)
		}
	})
}

func TestReadStage2EventsSkipsMalformedLinesAndInvalidTimestamps(t *testing.T) {
	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "bad.jsonl")
	if err := os.WriteFile(inputPath, []byte("{\"not\": \"json\\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	cfg := Stage2Config{InputPath: inputPath, WindowDuration: 24 * time.Hour}
	_, skipped, err := readStage2Events(cfg)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestReadStage2EventsResolvesEmitCategoriesAndWindow(t *testing.T) {
	testNow := time.Now().UTC().Truncate(time.Second)
	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "events.jsonl")

	oldLine := "{\"ts\": \"" + testNow.Add(-48*time.Hour).Format(time.RFC3339Nano) + "\", \"event_type\": \"agent_event\"}\n"
	keepLine := "{\"ts\": \"" + testNow.Add(-30*time.Minute).Format(time.RFC3339Nano) + "\", \"event_type\": \"agent_event\"}\n"
	if err := os.WriteFile(inputPath, []byte(oldLine+keepLine), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	cfg := Stage2Config{InputPath: inputPath, WindowDuration: 2 * time.Hour, EmitCategories: ParseEmitCategories("failure_event")}
	payload, skipped, err := readStage2Events(cfg)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}
	if payload.totalInput != 2 {
		t.Fatalf("totalInput = %d, want 2", payload.totalInput)
	}
	if got := len(payload.events); got != 0 {
		t.Fatalf("events kept = %d, want 0", got)
	}

	cfg.EmitCategories = ParseEmitCategories("agent_event")
	cfg.WindowDuration = 1 * time.Hour
	payload, skipped, err = readStage2Events(cfg)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}
	if payload.totalInput != 2 {
		t.Fatalf("totalInput = %d, want 2", payload.totalInput)
	}
	if got := len(payload.events); got != 1 {
		t.Fatalf("events kept = %d, want 1", got)
	}
}

func TestParseEmitCategories(t *testing.T) {
	cats := ParseEmitCategories("agent_event, attempt_event ; failure_event\n loop_signal")
	if len(cats) != 4 {
		t.Fatalf("got %d categories, want 4", len(cats))
	}
	if _, ok := cats["loop_signal"]; !ok {
		t.Fatalf("expected loop_signal in parsed categories")
	}
}

func TestNewStage2ConfigFromArgsLoadsConfigAndArgs(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "ail-config.json")
	configJSON := `{"stage1":{"events_path":"` + filepath.Join(tmp, "from-config.jsonl") + `","emit_categories":["failure_event","attempt_event"]}}`
	if err := os.WriteFile(cfgPath, []byte(configJSON), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewStage2ConfigFromArgs(cfgPath, "", "", "", 0)
	if err != nil {
		t.Fatalf("new config: %v", err)
	}
	if cfg.InputPath != filepath.Join(tmp, "from-config.jsonl") {
		t.Fatalf("input path = %q", cfg.InputPath)
	}
	if _, ok := cfg.EmitCategories["failure_event"]; !ok {
		t.Fatalf("expected config categories to include failure_event")
	}

	cfg, err = NewStage2ConfigFromArgs("", filepath.Join(tmp, "override.jsonl"), "", "agent_event", 12)
	if err != nil {
		t.Fatalf("new config override: %v", err)
	}
	if cfg.InputPath != filepath.Join(tmp, "override.jsonl") {
		t.Fatalf("override input path = %q", cfg.InputPath)
	}
	if cfg.WindowDuration != 12*time.Hour {
		t.Fatalf("window = %v, want 12h", cfg.WindowDuration)
	}
	if _, ok := cfg.EmitCategories["agent_event"]; !ok || len(cfg.EmitCategories) != 1 {
		t.Fatalf("expected only agent_event override")
	}
}
