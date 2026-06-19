package ail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeStage7IndexFile(t *testing.T, path string, events []Stage2Event) {
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

func stage7FixtureEvents() []Stage2Event {
	return []Stage2Event{
		{
			TS:             "2026-01-15T08:00:00Z",
			EventType:      "failure_event",
			WorkspaceID:    "ws1",
			AgentID:        "agent1",
			IssueID:        "issue1",
			TaskID:         "task1",
			Status:         "failed",
			Attempt:        1,
			MaxAttempts:    3,
			RetryCount:     0,
			FailureReason:  "agent_error",
			ErrorSignature: "E_TIMEOUT",
			LoopSignature:  "install_loop",
		},
		{
			TS:             "2026-01-15T09:00:00Z",
			EventType:      "failure_event",
			WorkspaceID:    "ws1",
			AgentID:        "agent2",
			IssueID:        "issue2",
			TaskID:         "task2",
			Status:         "failed",
			Attempt:        2,
			MaxAttempts:    3,
			RetryCount:     1,
			FailureReason:  "runtime_offline",
			ErrorSignature: "E_RUNTIME",
			LoopSignature:  "runtime_loop",
		},
		{
			TS:            "2026-01-15T10:00:00Z",
			EventType:     "agent_event",
			WorkspaceID:   "ws1",
			AgentID:       "agent1",
			IssueID:       "issue3",
			TaskID:        "task3",
			Status:        "completed",
			Attempt:       1,
			MaxAttempts:   3,
			RetryCount:    0,
			LoopSignature: "done_loop",
		},
	}
}

func TestRunStage7ReplayProducesByteIdenticalDecisionGivenSameFilters(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage7")
	events := stage7FixtureEvents()
	writeStage7IndexFile(t, indexPath, []Stage2Event{events[2], events[0], events[1]})

	cfg := Stage7ReplayConfig{
		IndexPath:      indexPath,
		OutputDir:      outputDir,
		IssueIDs:       []string{"issue1"},
		AgentIDs:       []string{"agent1"},
		TimeStart:      "2026-01-15T07:00:00Z",
		TimeEnd:        "2026-01-15T09:00:00Z",
		FailureReasons: []string{"agent_error"},
		LoopSignatures: []string{"install_loop"},
		ToolArgs:       map[string]string{"candidate": "detect_timeout"},
		EnvKeys:        []string{"AIL_TEST_ENV"},
		GitRevision:    "abc123",
		LookupEnv:      func(key string) string { return "env-" + key },
	}

	first, err := RunStage7Replay(cfg)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	firstBytes, err := os.ReadFile(filepath.Join(outputDir, defaultStage7DecisionFile))
	if err != nil {
		t.Fatalf("read first decision: %v", err)
	}

	second, err := RunStage7Replay(cfg)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(outputDir, defaultStage7DecisionFile))
	if err != nil {
		t.Fatalf("read second decision: %v", err)
	}

	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("decision payload should be byte-identical:\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
	if first.ReplayID != second.ReplayID {
		t.Fatalf("replay_id mismatch: %s vs %s", first.ReplayID, second.ReplayID)
	}
	if first.EventCount != 1 {
		t.Fatalf("event_count = %d, want 1", first.EventCount)
	}
	if first.Events[0].Event.IssueID != "issue1" {
		t.Fatalf("selected issue = %q, want issue1", first.Events[0].Event.IssueID)
	}
	if first.DeterminismProfile.InputChecksum == "" {
		t.Fatal("input checksum should be populated")
	}
	if first.DeterminismProfile.Env["AIL_TEST_ENV"] != "env-AIL_TEST_ENV" {
		t.Fatalf("env profile = %#v", first.DeterminismProfile.Env)
	}
}

func TestRunStage7ReplayFiltersByStableEventIDAndComputesMetrics(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	outputDir := filepath.Join(tmp, "stage7")
	events := stage7FixtureEvents()
	writeStage7IndexFile(t, indexPath, events)

	eventID := Stage7EventID(events[1])
	secondEventID := Stage7EventID(events[0])
	evalPath := filepath.Join(tmp, "eval.jsonl")
	evalLines := strings.Join([]string{
		`{"event_id":"` + eventID + `","success_on_retry_before":false,"success_on_retry_after":true,"failed_retries_before":3,"failed_retries_after":1,"actionable":true,"invocation_cost":0.25}`,
		`{"event_id":"` + secondEventID + `","success_on_retry_before":true,"success_on_retry_after":true,"failed_retries_before":1,"failed_retries_after":1,"actionable":false,"invocation_cost":0.75}`,
		`{"event_id":"not-selected","success_on_retry_before":true,"success_on_retry_after":false,"failed_retries_before":1,"failed_retries_after":4,"actionable":false,"invocation_cost":99}`,
		`not-json`,
	}, "\n")
	if err := os.WriteFile(evalPath, []byte(evalLines), 0o644); err != nil {
		t.Fatalf("write eval: %v", err)
	}

	result, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath:             indexPath,
		OutputDir:             outputDir,
		EventIDs:              []string{eventID, secondEventID},
		EvaluationResultsPath: evalPath,
	})
	if err != nil {
		t.Fatalf("RunStage7Replay: %v", err)
	}

	if result.EventCount != 2 {
		t.Fatalf("event_count = %d, want 2", result.EventCount)
	}
	if result.Events[1].EventID != eventID {
		t.Fatalf("second event_id = %q, want %q", result.Events[1].EventID, eventID)
	}
	if result.Metrics.SuccessOnRetryDelta != 0.5 {
		t.Fatalf("success_on_retry_delta = %v, want 0.5", result.Metrics.SuccessOnRetryDelta)
	}
	if result.Metrics.RetryReduction != 2 {
		t.Fatalf("retry_reduction = %d, want 2", result.Metrics.RetryReduction)
	}
	if result.Metrics.Precision != 0.5 {
		t.Fatalf("precision = %v, want 0.5", result.Metrics.Precision)
	}
	if result.Metrics.InvocationCost != 1.0 {
		t.Fatalf("invocation_cost = %v, want 1.0", result.Metrics.InvocationCost)
	}
	if result.Metrics.EvaluationCount != 2 {
		t.Fatalf("evaluation_count = %d, want 2", result.Metrics.EvaluationCount)
	}
}

func TestRunStage7ReplayNormalizesEquivalentFiltersForByteStableReplay(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	events := stage7FixtureEvents()
	writeStage7IndexFile(t, indexPath, []Stage2Event{events[1], events[0], events[2]})
	firstOutputDir := filepath.Join(tmp, "first")
	secondOutputDir := filepath.Join(tmp, "second")
	firstID := Stage7EventID(events[0])
	secondID := Stage7EventID(events[1])

	base := Stage7ReplayConfig{
		IndexPath:   indexPath,
		ToolArgs:    map[string]string{" candidate ": " detect_timeout ", "mode": " replay ", " ": "ignored"},
		GitRevision: " abc123 ",
		LookupEnv:   func(key string) string { return "env-" + key },
	}
	firstCfg := base
	firstCfg.OutputDir = firstOutputDir
	firstCfg.EventIDs = []string{secondID + "," + firstID, firstID}
	firstCfg.IssueIDs = []string{"issue2, issue1", "issue1"}
	firstCfg.AgentIDs = []string{"agent2", "agent1, agent2"}
	firstCfg.FailureReasons = []string{"runtime_offline, agent_error"}
	firstCfg.LoopSignatures = []string{"runtime_loop", "install_loop"}
	firstCfg.EnvKeys = []string{"AIL_B,AIL_A", "AIL_A"}

	secondCfg := base
	secondCfg.OutputDir = secondOutputDir
	secondCfg.EventIDs = []string{firstID, secondID}
	secondCfg.IssueIDs = []string{"issue1", "issue2"}
	secondCfg.AgentIDs = []string{"agent1", "agent2"}
	secondCfg.FailureReasons = []string{"agent_error", "runtime_offline"}
	secondCfg.LoopSignatures = []string{"install_loop", "runtime_loop"}
	secondCfg.EnvKeys = []string{"AIL_A", "AIL_B"}

	first, err := RunStage7Replay(firstCfg)
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	second, err := RunStage7Replay(secondCfg)
	if err != nil {
		t.Fatalf("second replay: %v", err)
	}
	firstBytes, err := os.ReadFile(filepath.Join(firstOutputDir, defaultStage7DecisionFile))
	if err != nil {
		t.Fatalf("read first decision: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondOutputDir, defaultStage7DecisionFile))
	if err != nil {
		t.Fatalf("read second decision: %v", err)
	}

	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("equivalent replay configs should produce byte-identical decisions:\nfirst:\n%s\nsecond:\n%s", firstBytes, secondBytes)
	}
	if first.ReplayID != second.ReplayID {
		t.Fatalf("replay_id mismatch: %s vs %s", first.ReplayID, second.ReplayID)
	}
	wantEventIDs := []string{firstID, secondID}
	sort.Strings(wantEventIDs)
	if got := first.Filters.EventIDs; len(got) != 2 || got[0] != wantEventIDs[0] || got[1] != wantEventIDs[1] {
		t.Fatalf("normalized event IDs = %#v, want %#v", got, wantEventIDs)
	}
	if _, ok := first.DeterminismProfile.ToolArgs[" "]; ok {
		t.Fatalf("blank tool arg key should be removed: %#v", first.DeterminismProfile.ToolArgs)
	}
	if first.DeterminismProfile.GitRevision != "abc123" {
		t.Fatalf("git revision = %q, want abc123", first.DeterminismProfile.GitRevision)
	}
}

func TestRunStage7ReplayTimeStartInclusiveAndTimeEndExclusive(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	events := []Stage2Event{
		{TS: "2026-01-15T07:59:59Z", EventType: "failure_event", IssueID: "before", AgentID: "agent-1", FailureReason: "agent_error"},
		{TS: "2026-01-15T08:00:00Z", EventType: "failure_event", IssueID: "start", AgentID: "agent-1", FailureReason: "agent_error"},
		{TS: "2026-01-15T08:30:00Z", EventType: "failure_event", IssueID: "middle", AgentID: "agent-1", FailureReason: "agent_error"},
		{TS: "2026-01-15T09:00:00Z", EventType: "failure_event", IssueID: "end", AgentID: "agent-1", FailureReason: "agent_error"},
	}
	writeStage7IndexFile(t, indexPath, events)

	result, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: indexPath,
		OutputDir: filepath.Join(tmp, "stage7"),
		TimeStart: "2026-01-15T08:00:00Z",
		TimeEnd:   "2026-01-15T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("RunStage7Replay: %v", err)
	}

	if result.EventCount != 2 {
		t.Fatalf("event_count = %d, want 2", result.EventCount)
	}
	gotIssues := []string{result.Events[0].Event.IssueID, result.Events[1].Event.IssueID}
	if gotIssues[0] != "start" || gotIssues[1] != "middle" {
		t.Fatalf("selected issues = %#v, want start and middle", gotIssues)
	}
}

func TestBuildStage7MetricsCapturesRetryRegression(t *testing.T) {
	tmp := t.TempDir()
	evalPath := filepath.Join(tmp, "eval.jsonl")
	lines := strings.Join([]string{
		`{"event_id":"selected-1","success_on_retry_before":true,"success_on_retry_after":false,"failed_retries_before":1,"failed_retries_after":3,"actionable":false,"invocation_cost":0.10}`,
		`{"event_id":"selected-2","success_on_retry_before":false,"success_on_retry_after":false,"failed_retries_before":2,"failed_retries_after":5,"actionable":true,"invocation_cost":0.20}`,
	}, "\n")
	if err := os.WriteFile(evalPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("write eval: %v", err)
	}

	metrics, err := buildStage7Metrics(evalPath, []Stage7ReplayEvent{{EventID: "selected-1"}, {EventID: "selected-2"}})
	if err != nil {
		t.Fatalf("build metrics: %v", err)
	}

	if metrics.SuccessOnRetryDelta != -0.5 {
		t.Fatalf("success_on_retry_delta = %v, want -0.5", metrics.SuccessOnRetryDelta)
	}
	if metrics.RetryReduction != -5 {
		t.Fatalf("retry_reduction = %d, want -5", metrics.RetryReduction)
	}
	if metrics.Precision != 0.5 {
		t.Fatalf("precision = %v, want 0.5", metrics.Precision)
	}
	if metrics.InvocationCost < 0.299 || metrics.InvocationCost > 0.301 {
		t.Fatalf("invocation_cost = %v, want about 0.3", metrics.InvocationCost)
	}
}

func TestRunStage7ReplayUsesDefaultsAndDecisionPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	indexPath := filepath.Join(tmp, "diagnostics", "stage2", "stage2_index.jsonl")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatalf("mkdir index dir: %v", err)
	}
	writeStage7IndexFile(t, indexPath, stage7FixtureEvents())

	result, err := RunStage7Replay(Stage7ReplayConfig{})
	if err != nil {
		t.Fatalf("RunStage7Replay with defaults: %v", err)
	}

	if result.EventCount != 3 {
		t.Fatalf("event_count = %d, want 3", result.EventCount)
	}
	wantPath := filepath.Join(tmp, "diagnostics", "stage7", defaultStage7DecisionFile)
	if got := (Stage7ReplayConfig{}).Stage7DecisionPath(); got != wantPath {
		t.Fatalf("decision path = %q, want %q", got, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("default decision file should exist: %v", err)
	}
}

func TestRunStage7ReplaySkipsMalformedIndexRowsAndInvalidEventTimes(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	valid := Stage2Event{TS: "2026-01-15T08:00:00Z", EventType: "failure_event", IssueID: "issue-1", AgentID: "agent-1", Status: "failed", FailureReason: "agent_error"}
	invalidTime := Stage2Event{TS: "not-a-time", EventType: "failure_event", IssueID: "issue-2", AgentID: "agent-2", Status: "failed", FailureReason: "agent_error"}
	validBytes, err := json.Marshal(valid)
	if err != nil {
		t.Fatalf("marshal valid: %v", err)
	}
	invalidBytes, err := json.Marshal(invalidTime)
	if err != nil {
		t.Fatalf("marshal invalid time: %v", err)
	}
	data := strings.Join([]string{"", "not-json", string(invalidBytes), string(validBytes)}, "\n")
	if err := os.WriteFile(indexPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	result, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: indexPath,
		OutputDir: filepath.Join(tmp, "stage7"),
		TimeStart: "2026-01-15T07:00:00Z",
	})
	if err != nil {
		t.Fatalf("RunStage7Replay: %v", err)
	}
	if result.EventCount != 1 {
		t.Fatalf("event_count = %d, want 1", result.EventCount)
	}
	if result.Events[0].Event.IssueID != "issue-1" {
		t.Fatalf("selected issue = %q, want issue-1", result.Events[0].Event.IssueID)
	}
}

func TestFilterStage7EventsCoversEverySkipBranch(t *testing.T) {
	events := []Stage7ReplayEvent{
		{EventID: "event-1", Event: Stage2Event{TS: "2026-01-15T08:00:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "install_loop"}},
		{EventID: "event-2", Event: Stage2Event{TS: "2026-01-15T09:00:00Z", IssueID: "issue-2", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "install_loop"}},
		{EventID: "event-3", Event: Stage2Event{TS: "2026-01-15T10:00:00Z", IssueID: "issue-1", AgentID: "agent-2", FailureReason: "agent_error", LoopSignature: "install_loop"}},
		{EventID: "event-4", Event: Stage2Event{TS: "2026-01-15T11:00:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "runtime_offline", LoopSignature: "install_loop"}},
		{EventID: "event-5", Event: Stage2Event{TS: "2026-01-15T12:00:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "runtime_loop"}},
		{EventID: "event-6", Event: Stage2Event{TS: "2026-01-15T06:00:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "install_loop"}},
		{EventID: "event-7", Event: Stage2Event{TS: "2026-01-15T13:00:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "install_loop"}},
		{EventID: "event-8", Event: Stage2Event{TS: "2026-01-15T08:30:00Z", IssueID: "issue-1", AgentID: "agent-1", FailureReason: "agent_error", LoopSignature: "install_loop"}},
	}

	selected, err := filterStage7Events(events, Stage7ReplayFilters{
		EventIDs:       []string{"event-1", "event-2", "event-3", "event-4", "event-5", "event-6", "event-7", "event-8"},
		IssueIDs:       []string{"issue-1"},
		AgentIDs:       []string{"agent-1"},
		TimeStart:      "2026-01-15T07:00:00Z",
		TimeEnd:        "2026-01-15T13:00:00Z",
		FailureReasons: []string{"agent_error"},
		LoopSignatures: []string{"install_loop"},
	})
	if err != nil {
		t.Fatalf("filter events: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected len = %d, want 2", len(selected))
	}
	if selected[0].EventID != "event-1" || selected[1].EventID != "event-8" {
		t.Fatalf("selected IDs = %#v", selected)
	}
}

func TestFilterStage7EventsReturnsErrorForBadEndTime(t *testing.T) {
	_, err := filterStage7Events(nil, Stage7ReplayFilters{TimeEnd: "not-a-time"})
	if err == nil {
		t.Fatal("expected bad time-end error, got nil")
	}
	if !strings.Contains(err.Error(), "parse time-end") {
		t.Fatalf("error should mention parse time-end, got: %v", err)
	}
}

func TestRunStage7ReplayReturnsErrorForMissingEvaluationResults(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	writeStage7IndexFile(t, indexPath, stage7FixtureEvents())

	_, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath:             indexPath,
		OutputDir:             filepath.Join(tmp, "stage7"),
		EvaluationResultsPath: filepath.Join(tmp, "missing-eval.jsonl"),
	})
	if err == nil {
		t.Fatal("expected missing evaluation results error, got nil")
	}
	if !strings.Contains(err.Error(), "read evaluation results") {
		t.Fatalf("error should mention read evaluation results, got: %v", err)
	}
}

func TestRunStage7ReplayReturnsErrorWhenDecisionCannotBeWritten(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	blockingFile := filepath.Join(tmp, "blocking")
	writeStage7IndexFile(t, indexPath, stage7FixtureEvents())
	if err := os.WriteFile(blockingFile, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	_, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: indexPath,
		OutputDir: filepath.Join(blockingFile, "stage7"),
	})
	if err == nil {
		t.Fatal("expected decision write error, got nil")
	}
}

func TestReadStage7ReplayEventsReturnsScannerErrorForLongLine(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	if err := os.WriteFile(indexPath, []byte(strings.Repeat("x", 70*1024)), 0o644); err != nil {
		t.Fatalf("write long line: %v", err)
	}

	_, err := readStage7ReplayEvents(Stage7ReplayConfig{IndexPath: indexPath})
	if err == nil {
		t.Fatal("expected scanner error for long line, got nil")
	}
}

func TestBuildStage7MetricsReturnsScannerErrorForLongLine(t *testing.T) {
	tmp := t.TempDir()
	evalPath := filepath.Join(tmp, "eval.jsonl")
	if err := os.WriteFile(evalPath, []byte(strings.Repeat("x", 70*1024)), 0o644); err != nil {
		t.Fatalf("write long eval line: %v", err)
	}

	_, err := buildStage7Metrics(evalPath, []Stage7ReplayEvent{{EventID: "selected"}})
	if err == nil {
		t.Fatal("expected scanner error for long evaluation line, got nil")
	}
}

func TestSortStage7EventsUsesEventIDTieBreaker(t *testing.T) {
	events := []Stage7ReplayEvent{
		{EventID: "b", Event: Stage2Event{TS: "not-a-time"}},
		{EventID: "a", Event: Stage2Event{TS: "not-a-time"}},
	}

	sortStage7Events(events)

	if events[0].EventID != "a" {
		t.Fatalf("first event ID = %q, want a", events[0].EventID)
	}
}

func TestBuildStage7MetricsReturnsZeroWhenNoRowsMatch(t *testing.T) {
	tmp := t.TempDir()
	evalPath := filepath.Join(tmp, "eval.jsonl")
	if err := os.WriteFile(evalPath, []byte("\n{\"event_id\":\"other\"}\n"), 0o644); err != nil {
		t.Fatalf("write eval: %v", err)
	}

	metrics, err := buildStage7Metrics(evalPath, []Stage7ReplayEvent{{EventID: "selected"}})
	if err != nil {
		t.Fatalf("build metrics: %v", err)
	}
	if metrics != (Stage7ReplayMetrics{}) {
		t.Fatalf("metrics = %#v, want zero", metrics)
	}
}

func TestNormalizeStage7MapReturnsNilForBlankKeys(t *testing.T) {
	if got := normalizeStage7Map(map[string]string{" ": "ignored"}); got != nil {
		t.Fatalf("normalize map = %#v, want nil", got)
	}
}

func TestResolveStage7OutputDirWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if got := resolveStage7OutputDir(); got != filepath.Join(".", defaultStage7OutputDir) {
		t.Fatalf("output dir = %q", got)
	}
}

func TestRunStage7ReplayHandlesMissingEvaluationResultsAsZeroMetrics(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	writeStage7IndexFile(t, indexPath, stage7FixtureEvents())

	result, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: indexPath,
		OutputDir: filepath.Join(tmp, "stage7"),
	})
	if err != nil {
		t.Fatalf("RunStage7Replay: %v", err)
	}

	if result.EventCount != 3 {
		t.Fatalf("event_count = %d, want 3", result.EventCount)
	}
	if result.Metrics != (Stage7ReplayMetrics{}) {
		t.Fatalf("metrics = %#v, want zero defaults", result.Metrics)
	}
}

func TestRunStage7ReplayReturnsErrorForBadTimeFilter(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "stage2_index.jsonl")
	writeStage7IndexFile(t, indexPath, stage7FixtureEvents())

	_, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: indexPath,
		OutputDir: filepath.Join(tmp, "stage7"),
		TimeStart: "not-a-time",
	})
	if err == nil {
		t.Fatal("expected bad time filter error, got nil")
	}
	if !strings.Contains(err.Error(), "parse time-start") {
		t.Fatalf("error should mention parse time-start, got: %v", err)
	}
}

func TestRunStage7ReplayReturnsErrorForMissingIndex(t *testing.T) {
	tmp := t.TempDir()
	_, err := RunStage7Replay(Stage7ReplayConfig{
		IndexPath: filepath.Join(tmp, "missing.jsonl"),
		OutputDir: filepath.Join(tmp, "stage7"),
	})
	if err == nil {
		t.Fatal("expected missing index error, got nil")
	}
	if !strings.Contains(err.Error(), "read stage2 index") {
		t.Fatalf("error should mention read stage2 index, got: %v", err)
	}
}
