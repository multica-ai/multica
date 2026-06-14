package ail

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// goldenIndexLines holds pre-sorted Stage 2 index JSONL lines used by golden determinism tests.
// Timestamps are sorted ascending as Stage 2 would produce.
const goldenIndexLines = `` +
	`{"ts":"2026-01-15T06:00:00Z","event_type":"failure_event","workspace_id":"ws1","agent_id":"agent3","issue_id":"iss4","task_id":"task4","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"runtime_offline"}` + "\n" +
	`{"ts":"2026-01-15T07:00:00Z","event_type":"failure_event","workspace_id":"ws1","agent_id":"agent4","issue_id":"iss5","task_id":"task5","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"runtime_offline"}` + "\n" +
	`{"ts":"2026-01-15T08:00:00Z","event_type":"failure_event","workspace_id":"ws1","agent_id":"agent1","issue_id":"iss1","task_id":"task1","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"agent_error","error_signature":"E_TIMEOUT"}` + "\n" +
	`{"ts":"2026-01-15T09:00:00Z","event_type":"failure_event","workspace_id":"ws1","agent_id":"agent1","issue_id":"iss2","task_id":"task2","status":"failed","attempt":2,"max_attempts":3,"retry_count":1,"failure_reason":"agent_error","error_signature":"E_TIMEOUT"}` + "\n" +
	`{"ts":"2026-01-15T10:00:00Z","event_type":"failure_event","workspace_id":"ws1","agent_id":"agent2","issue_id":"iss3","task_id":"task3","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"agent_error","error_signature":"E_TIMEOUT"}` + "\n"

var fixedClock2026 = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

func writeGoldenIndexFile(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "stage2_index.jsonl")
	if err := os.WriteFile(path, []byte(goldenIndexLines), 0o644); err != nil {
		t.Fatalf("write golden index: %v", err)
	}
	return path
}

func TestRunStage3AnalyzeGoldenDeterminism(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}

	got, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "stage3", "digest_golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file updated: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file (run with UPDATE_GOLDEN=1 to create it): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRunStage3AnalyzeMissingIndexReturnsError(t *testing.T) {
	tmp := t.TempDir()
	cfg := Stage3Config{
		IndexPath:      filepath.Join(tmp, "nonexistent.jsonl"),
		OutputDir:      filepath.Join(tmp, "out"),
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	_, err := RunStage3Analyze(cfg)
	if err == nil {
		t.Fatal("expected error for missing index, got nil")
	}
	if !strings.Contains(err.Error(), "read stage2 index") {
		t.Fatalf("error should mention 'read stage2 index', got: %v", err)
	}
}

func TestRunStage3AnalyzeWatermarkIdempotency(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}

	first, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Mutate the clock so the second run would produce a different AnalyzedAt if recomputing.
	laterClock := fixedClock2026.Add(time.Hour)
	cfg.Now = func() time.Time { return laterClock }

	second, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if first.AnalyzedAt != second.AnalyzedAt {
		t.Fatalf("idempotent re-run should return cached result with same analyzed_at: first=%q second=%q", first.AnalyzedAt, second.AnalyzedAt)
	}
	if first.TotalEvents != second.TotalEvents {
		t.Fatalf("total_window_events mismatch: %d vs %d", first.TotalEvents, second.TotalEvents)
	}
}

func TestRunStage3AnalyzeWatermarkDifferentWindowRecomputes(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	first, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Change the window — same index but different window forces recompute.
	cfg.WindowDuration = 48 * time.Hour
	laterClock := fixedClock2026.Add(time.Hour)
	cfg.Now = func() time.Time { return laterClock }

	second, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("second run with different window: %v", err)
	}

	if first.AnalyzedAt == second.AnalyzedAt {
		t.Fatal("different window should force recompute, but analyzed_at was identical")
	}
	if second.WindowDuration != "48h0m0s" {
		t.Fatalf("second run window_duration = %q, want 48h0m0s", second.WindowDuration)
	}
}

func TestRunStage3AnalyzeWatermarkMatchesButDigestMissing(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	_, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Delete the digest so the watermark matches but cached read fails.
	if err := os.Remove(filepath.Join(outputDir, defaultStage3DigestFile)); err != nil {
		t.Fatalf("remove digest: %v", err)
	}

	laterClock := fixedClock2026.Add(time.Hour)
	cfg.Now = func() time.Time { return laterClock }

	second, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("second run with missing digest: %v", err)
	}
	// Recomputed: analyzed_at reflects the later clock.
	if second.AnalyzedAt == fixedClock2026.UTC().Format(time.RFC3339Nano) {
		t.Fatal("digest was missing, should have recomputed with later clock")
	}
}

func TestRunStage3AnalyzeWatermarkInvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a corrupt watermark — analyzer should ignore it and recompute.
	if err := os.WriteFile(filepath.Join(outputDir, defaultStage3WatermarkFile), []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write watermark: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze with invalid watermark: %v", err)
	}
	if result.TotalEvents != 5 {
		t.Fatalf("total_window_events = %d, want 5", result.TotalEvents)
	}
}

func TestRunStage3AnalyzeWritesAllThreeArtifacts(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	if _, err := RunStage3Analyze(cfg); err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}

	for _, name := range []string{defaultStage3DigestFile, defaultStage3SignaturesFile, defaultStage3WatermarkFile} {
		if _, err := os.Stat(filepath.Join(outputDir, name)); err != nil {
			t.Fatalf("artifact %q should exist: %v", name, err)
		}
	}

	wmBytes, err := os.ReadFile(filepath.Join(outputDir, defaultStage3WatermarkFile))
	if err != nil {
		t.Fatalf("read watermark: %v", err)
	}
	var wm Stage3Watermark
	if err := json.Unmarshal(wmBytes, &wm); err != nil {
		t.Fatalf("parse watermark: %v", err)
	}
	if wm.IndexSHA256 == "" {
		t.Fatal("watermark index_sha256 should be set")
	}
	if wm.WindowDuration != "24h0m0s" {
		t.Fatalf("watermark window_duration = %q, want 24h0m0s", wm.WindowDuration)
	}
}

func TestRunStage3AnalyzeNoFailureEvents(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	lines := `{"ts":"` + now.Add(-5*time.Minute).Format(time.RFC3339Nano) + `","event_type":"agent_event","workspace_id":"ws1","agent_id":"a1","task_id":"t1","status":"completed","attempt":1,"max_attempts":3,"retry_count":0}` + "\n"
	if err := os.WriteFile(indexPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if result.TotalEvents != 1 {
		t.Fatalf("total_window_events = %d, want 1", result.TotalEvents)
	}
	if len(result.TopPainBuckets) != 0 {
		t.Fatalf("top_pain_buckets should be empty, got %d", len(result.TopPainBuckets))
	}
	if len(result.RepeatSignatures) != 0 {
		t.Fatalf("repeat_signatures should be empty, got %d", len(result.RepeatSignatures))
	}
	if len(result.CandidateDettools) != 0 {
		t.Fatalf("candidate_dettools should be empty, got %d", len(result.CandidateDettools))
	}
	if len(result.ByFailureReason) != 0 {
		t.Fatalf("by_failure_reason should be empty, got %v", result.ByFailureReason)
	}
}

func TestRunStage3AnalyzeEventsOutsideWindowAreFiltered(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	oldLine := `{"ts":"` + now.Add(-48*time.Hour).Format(time.RFC3339Nano) + `","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"t1","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"stale_error"}` + "\n"
	recentLine := `{"ts":"` + now.Add(-5*time.Minute).Format(time.RFC3339Nano) + `","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"t2","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"recent_error"}` + "\n"
	if err := os.WriteFile(indexPath, []byte(oldLine+recentLine), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if result.TotalEvents != 1 {
		t.Fatalf("total_window_events = %d, want 1 (stale filtered)", result.TotalEvents)
	}
	if _, ok := result.ByFailureReason["stale_error"]; ok {
		t.Fatal("stale_error should be filtered by window")
	}
	if _, ok := result.ByFailureReason["recent_error"]; !ok {
		t.Fatal("recent_error should be in result")
	}
}

func TestRunStage3AnalyzeSkipsMalformedAndInvalidTimestampLines(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	validLine := `{"ts":"` + fixedClock2026.Add(-time.Minute).Format(time.RFC3339Nano) + `","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"valid","status":"failed","failure_reason":"agent_error"}`
	invalidTimestampLine := `{"ts":"not-a-time","event_type":"failure_event","workspace_id":"ws1","agent_id":"a2","task_id":"invalid-ts","status":"failed","failure_reason":"runtime_offline"}`
	data := strings.Join([]string{
		"not-json",
		invalidTimestampLine,
		validLine,
		"",
	}, "\n")
	if err := os.WriteFile(indexPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	result, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if result.TotalEvents != 1 {
		t.Fatalf("total_window_events = %d, want only the single valid line", result.TotalEvents)
	}
	if result.ByFailureReason["agent_error"] != 1 {
		t.Fatalf("agent_error count = %d, want 1", result.ByFailureReason["agent_error"])
	}
	if _, ok := result.ByFailureReason["runtime_offline"]; ok {
		t.Fatalf("invalid timestamp event should be skipped: %#v", result.ByFailureReason)
	}
}

func TestRunStage3AnalyzeRepeatSignatureClustering(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	var lines []string
	for i := 0; i < 4; i++ {
		line := `{"ts":"` + now.Add(-time.Duration(i+1)*time.Minute).Format(time.RFC3339Nano) +
			`","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"t` +
			string(rune('0'+i)) + `","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"agent_error","error_signature":"E_CONN","loop_signature":"install_loop"}`
		lines = append(lines, line)
	}
	if err := os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    3,
		MinUniqueTasks: 2,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if len(result.RepeatSignatures) != 1 {
		t.Fatalf("repeat_signatures len = %d, want 1", len(result.RepeatSignatures))
	}
	sig := result.RepeatSignatures[0]
	if sig.Key != "agent_error::E_CONN::install_loop" {
		t.Fatalf("signature key = %q, want agent_error::E_CONN::install_loop", sig.Key)
	}
	if sig.Count != 4 {
		t.Fatalf("count = %d, want 4", sig.Count)
	}
	if sig.FirstSeen == "" || sig.LastSeen == "" {
		t.Fatal("first_seen / last_seen should be set")
	}
	if sig.FirstSeen > sig.LastSeen {
		t.Fatalf("first_seen %q should be <= last_seen %q", sig.FirstSeen, sig.LastSeen)
	}
	if len(result.CandidateDettools) != 1 {
		t.Fatalf("candidate_dettools len = %d, want 1", len(result.CandidateDettools))
	}
	cand := result.CandidateDettools[0]
	if cand.SuggestedName != "detect_agent_error_e_conn_install_loop" {
		t.Fatalf("suggested_name = %q", cand.SuggestedName)
	}
}

func TestRunStage3AnalyzeKeepsEarliestExampleAndRawRefForCluster(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	events := []Stage2Event{
		{
			TS:             fixedClock2026.Add(-3 * time.Minute).Format(time.RFC3339Nano),
			EventType:      "failure_event",
			AgentID:        "a1",
			TaskID:         "task-first",
			Status:         "failed",
			FailureReason:  "agent_error",
			ErrorSignature: "E_TIMEOUT",
			LoopSignature:  "retry_loop",
			RawRef:         "comment:first",
		},
		{
			TS:             fixedClock2026.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			EventType:      "failure_event",
			AgentID:        "a2",
			TaskID:         "task-second",
			Status:         "failed",
			FailureReason:  "agent_error",
			ErrorSignature: "E_TIMEOUT",
			LoopSignature:  "retry_loop",
			RawRef:         "comment:second",
		},
	}
	writeStage3IndexEvents(t, indexPath, events)

	result, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    2,
		MinUniqueTasks: 2,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if len(result.RepeatSignatures) != 1 {
		t.Fatalf("repeat_signatures len = %d, want 1", len(result.RepeatSignatures))
	}
	sig := result.RepeatSignatures[0]
	if sig.ExampleTaskID != "task-first" {
		t.Fatalf("example_task_id = %q, want task-first", sig.ExampleTaskID)
	}
	if sig.ExampleRawRef != "comment:first" {
		t.Fatalf("example_raw_ref = %q, want comment:first", sig.ExampleRawRef)
	}
	if sig.UniqueAgents != 2 {
		t.Fatalf("unique_agents = %d, want 2", sig.UniqueAgents)
	}
}

func TestRunStage3AnalyzeCandidateDetoolRanking(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	var lines []string
	// 5 events for "high_signal" signature across 5 unique tasks
	for i := 0; i < 5; i++ {
		line := `{"ts":"` + now.Add(-time.Duration(i+1)*time.Minute).Format(time.RFC3339Nano) +
			`","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"high` +
			string(rune('0'+i)) + `","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"high_signal"}`
		lines = append(lines, line)
	}
	// 3 events for "low_signal" signature across 3 unique tasks
	for i := 0; i < 3; i++ {
		line := `{"ts":"` + now.Add(-time.Duration(i+10)*time.Minute).Format(time.RFC3339Nano) +
			`","event_type":"failure_event","workspace_id":"ws1","agent_id":"a2","task_id":"low` +
			string(rune('0'+i)) + `","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"low_signal"}`
		lines = append(lines, line)
	}
	if err := os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    3,
		MinUniqueTasks: 2,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if len(result.CandidateDettools) != 2 {
		t.Fatalf("candidate_dettools len = %d, want 2", len(result.CandidateDettools))
	}
	// high_signal (count=5, unique_tasks=5) > low_signal (count=3, unique_tasks=3) by gain
	if result.CandidateDettools[0].SuggestedName != "detect_high_signal" {
		t.Fatalf("first candidate = %q, want detect_high_signal", result.CandidateDettools[0].SuggestedName)
	}
	if result.CandidateDettools[0].ExpectedDeterminismGain <= result.CandidateDettools[1].ExpectedDeterminismGain {
		t.Fatal("first candidate should have higher gain than second")
	}
}

func TestRunStage3AnalyzeCandidateTieBreaksBySuggestedName(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	var events []Stage2Event
	for _, reason := range []string{"zeta_error", "alpha_error"} {
		for i := 0; i < 3; i++ {
			events = append(events, Stage2Event{
				TS:            fixedClock2026.Add(-time.Duration(len(events)+1) * time.Minute).Format(time.RFC3339Nano),
				EventType:     "failure_event",
				AgentID:       "agent-" + reason,
				TaskID:        reason + string(rune('0'+i)),
				Status:        "failed",
				FailureReason: reason,
			})
		}
	}
	writeStage3IndexEvents(t, indexPath, events)

	result, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    3,
		MinUniqueTasks: 3,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if len(result.CandidateDettools) != 2 {
		t.Fatalf("candidate_dettools len = %d, want 2", len(result.CandidateDettools))
	}
	if result.CandidateDettools[0].SuggestedName != "detect_alpha_error" {
		t.Fatalf("first tied candidate = %q, want detect_alpha_error", result.CandidateDettools[0].SuggestedName)
	}
	if result.CandidateDettools[1].SuggestedName != "detect_zeta_error" {
		t.Fatalf("second tied candidate = %q, want detect_zeta_error", result.CandidateDettools[1].SuggestedName)
	}
}

func TestRunStage3AnalyzeCandidateReadyForCandidate(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	var lines []string
	for i := 0; i < 10; i++ {
		line := `{"ts":"` + now.Add(-time.Duration(i+1)*time.Minute).Format(time.RFC3339Nano) +
			`","event_type":"failure_event","workspace_id":"ws1","agent_id":"a` + string(rune('0'+i%3)) + `","task_id":"t` +
			string(rune('0'+i)) + `","status":"failed","attempt":1,"max_attempts":3,"retry_count":0,"failure_reason":"persistent_error"}`
		lines = append(lines, line)
	}
	if err := os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    3,
		MinUniqueTasks: 2,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	if len(result.CandidateDettools) != 1 {
		t.Fatalf("candidate_dettools len = %d, want 1", len(result.CandidateDettools))
	}
	if result.CandidateDettools[0].DecisionHint != "ready_for_candidate" {
		t.Fatalf("decision_hint = %q, want ready_for_candidate (count=10, unique_tasks>=3)", result.CandidateDettools[0].DecisionHint)
	}
}

func TestRunStage3AnalyzeCandidateDefer(t *testing.T) {
	now := time.Now().UTC()
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")

	// 3 events for same agent and same task — count >= minSigCount but uniqueTasks < minUniqueTasks
	var lines []string
	for i := 0; i < 3; i++ {
		line := `{"ts":"` + now.Add(-time.Duration(i+1)*time.Minute).Format(time.RFC3339Nano) +
			`","event_type":"failure_event","workspace_id":"ws1","agent_id":"a1","task_id":"t1","status":"failed","attempt":` +
			string(rune('1'+i)) + `,"max_attempts":3,"retry_count":0,"failure_reason":"retry_error"}`
		lines = append(lines, line)
	}
	if err := os.WriteFile(indexPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(tmp, "stage3"),
		WindowDuration: 24 * time.Hour,
		MinSigCount:    3,
		MinUniqueTasks: 2,
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}
	// count=3 >= minSigCount=3 but uniqueTasks=1 < minUniqueTasks=2 → not a candidate
	if len(result.CandidateDettools) != 0 {
		t.Fatalf("candidate_dettools should be empty, got %d", len(result.CandidateDettools))
	}
}

func TestRunStage3AnalyzeReturnsErrorWhenOutputDirCannotBeCreated(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	notDir := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(notDir, []byte("file"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	_, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      filepath.Join(notDir, "stage3"),
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err == nil {
		t.Fatal("expected error when output dir cannot be created, got nil")
	}
}

func TestRunStage3AnalyzeReturnsErrorWhenDigestPathIsDirectory(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")
	if err := os.MkdirAll(filepath.Join(outputDir, defaultStage3DigestFile), 0o755); err != nil {
		t.Fatalf("mkdir digest path: %v", err)
	}

	_, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err == nil {
		t.Fatal("expected error when digest path is a directory, got nil")
	}
}

func TestRunStage3AnalyzeReturnsErrorWhenSignaturesPathIsDirectory(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")
	if err := os.MkdirAll(filepath.Join(outputDir, defaultStage3SignaturesFile), 0o755); err != nil {
		t.Fatalf("mkdir signatures path: %v", err)
	}

	_, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err == nil {
		t.Fatal("expected error when signatures path is a directory, got nil")
	}
}

func TestRunStage3AnalyzeReturnsErrorWhenWatermarkPathIsDirectory(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")
	if err := os.MkdirAll(filepath.Join(outputDir, defaultStage3WatermarkFile), 0o755); err != nil {
		t.Fatalf("mkdir watermark path: %v", err)
	}

	_, err := RunStage3Analyze(Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	})
	if err == nil {
		t.Fatal("expected error when watermark path is a directory, got nil")
	}
}

func TestStage3DecisionHint(t *testing.T) {
	cases := []struct {
		count        int
		uniqueTasks  int
		expectedHint string
	}{
		{10, 3, "ready_for_candidate"},
		{15, 5, "ready_for_candidate"},
		{5, 2, "ready_for_review"},
		{3, 3, "ready_for_review"},
		{9, 2, "ready_for_review"},
		{2, 1, "defer"},
		{1, 1, "defer"},
		{4, 1, "defer"},
	}
	for _, c := range cases {
		got := stage3DecisionHint(c.count, c.uniqueTasks)
		if got != c.expectedHint {
			t.Errorf("stage3DecisionHint(%d, %d) = %q, want %q", c.count, c.uniqueTasks, got, c.expectedHint)
		}
	}
}

func TestStage3SignatureKey(t *testing.T) {
	cases := []struct {
		fr, es, ls string
		want       string
	}{
		{"agent_error", "E_TIMEOUT", "install_loop", "agent_error::E_TIMEOUT::install_loop"},
		{"agent_error", "E_TIMEOUT", "", "agent_error::E_TIMEOUT"},
		{"agent_error", "", "install_loop", "agent_error::install_loop"},
		{"agent_error", "", "", "agent_error"},
		{"", "E_TIMEOUT", "", "E_TIMEOUT"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		got := stage3SignatureKey(c.fr, c.es, c.ls)
		if got != c.want {
			t.Errorf("stage3SignatureKey(%q, %q, %q) = %q, want %q", c.fr, c.es, c.ls, got, c.want)
		}
	}
}

func TestStage3ToSnakeCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"agent_error::E_TIMEOUT", "detect_agent_error_e_timeout"},
		{"runtime_offline", "detect_runtime_offline"},
		{"A::B::C", "detect_a_b_c"},
		{"UPPER_CASE", "detect_upper_case"},
		{"mixed123", "detect_mixed123"},
		{"::leading", "detect_leading"},
		{"trailing::", "detect_trailing"},
	}
	for _, c := range cases {
		got := stage3ToSnakeCase(c.input)
		if got != c.want {
			t.Errorf("stage3ToSnakeCase(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestNewStage3ConfigFromArgs(t *testing.T) {
	tmp := t.TempDir()

	cfg := NewStage3ConfigFromArgs("", "", 0, 0, 0)
	if cfg.WindowDuration != defaultStage2Window {
		t.Fatalf("default window = %v, want %v", cfg.WindowDuration, defaultStage2Window)
	}
	if cfg.MinSigCount != defaultStage3MinSignatureCount {
		t.Fatalf("default MinSigCount = %d", cfg.MinSigCount)
	}
	if cfg.MinUniqueTasks != defaultStage3MinUniqueTasks {
		t.Fatalf("default MinUniqueTasks = %d", cfg.MinUniqueTasks)
	}
	if cfg.Now == nil {
		t.Fatal("Now should default to time.Now")
	}

	cfg = NewStage3ConfigFromArgs(
		filepath.Join(tmp, "my_index.jsonl"),
		filepath.Join(tmp, "my_out"),
		12, 5, 3,
	)
	if cfg.IndexPath != filepath.Join(tmp, "my_index.jsonl") {
		t.Fatalf("IndexPath = %q", cfg.IndexPath)
	}
	if cfg.OutputDir != filepath.Join(tmp, "my_out") {
		t.Fatalf("OutputDir = %q", cfg.OutputDir)
	}
	if cfg.WindowDuration != 12*time.Hour {
		t.Fatalf("WindowDuration = %v, want 12h", cfg.WindowDuration)
	}
	if cfg.MinSigCount != 5 {
		t.Fatalf("MinSigCount = %d, want 5", cfg.MinSigCount)
	}
	if cfg.MinUniqueTasks != 3 {
		t.Fatalf("MinUniqueTasks = %d, want 3", cfg.MinUniqueTasks)
	}
}

func TestResolveStage3OutputDirWithNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	dir := resolveStage3OutputDir()
	if !strings.HasSuffix(dir, defaultStage3OutputDir) {
		t.Fatalf("resolveStage3OutputDir with no HOME = %q, should end with %q", dir, defaultStage3OutputDir)
	}
}

func writeStage3IndexEvents(t *testing.T, path string, events []Stage2Event) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			t.Fatalf("encode index event: %v", err)
		}
	}
}

func TestStage3SignaturesJSONLContent(t *testing.T) {
	tmp := t.TempDir()
	indexPath := writeGoldenIndexFile(t, tmp)
	outputDir := filepath.Join(tmp, "stage3")

	cfg := Stage3Config{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		WindowDuration: 24 * time.Hour,
		Now:            func() time.Time { return fixedClock2026 },
	}
	result, err := RunStage3Analyze(cfg)
	if err != nil {
		t.Fatalf("RunStage3Analyze: %v", err)
	}

	sigsData, err := os.ReadFile(filepath.Join(outputDir, defaultStage3SignaturesFile))
	if err != nil {
		t.Fatalf("read signatures: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(sigsData)), "\n")
	if len(lines) != len(result.RepeatSignatures) {
		t.Fatalf("signatures.jsonl lines = %d, want %d", len(lines), len(result.RepeatSignatures))
	}
	for i, line := range lines {
		var sig Stage3Signature
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			t.Fatalf("parse signature line %d: %v", i, err)
		}
		if sig.Key != result.RepeatSignatures[i].Key {
			t.Fatalf("signature[%d].key = %q, want %q", i, sig.Key, result.RepeatSignatures[i].Key)
		}
	}
}
