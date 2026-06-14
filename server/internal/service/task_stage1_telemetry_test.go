package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestEmitTaskLifecycleEventWritesJSONL(t *testing.T) {
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "true")
	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", "")
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "agent_improvement_loop_stage1.jsonl")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", logPath)

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{
		ID:          util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
		Status:      "running",
		Attempt:     1,
		MaxAttempts: 3,
		Error:       pgtype.Text{String: "", Valid: false},
		Result:      []byte("{\"output\":\"ok\"}"),
	}

	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected stage1 log file to exist: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one event line, got %d", len(lines))
	}

	var got Stage1LifecycleEvent
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("expected valid json event: %v", err)
	}
	if got.EventType != "attempt_event" {
		t.Fatalf("expected event_type attempt_event, got %q", got.EventType)
	}
	if got.TaskID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected task id: %q", got.TaskID)
	}
	if got.Status != "running" {
		t.Fatalf("unexpected status: %q", got.Status)
	}
	if got.RunDurationMs < 0 {
		t.Fatalf("expected non-negative run_duration_ms, got %d", got.RunDurationMs)
	}
}

func TestStage1TelemetryCanBeDisabled(t *testing.T) {
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "false")
	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", "")
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "should-not-exist.jsonl")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", logPath)

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{
		ID:     util.MustParseUUID("22222222-2222-2222-2222-222222222222"),
		Status: "running",
	}

	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected no stage1 event file when telemetry disabled, got: %v", err)
	}
}

func TestStage1TelemetryHonorsEmitCategoryFilter(t *testing.T) {
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "true")
	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", "")
	t.Setenv("MULTICA_AIL_STAGE1_EMIT_CATEGORIES", "failure_event")
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "stage1-filtered.jsonl")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", logPath)

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{
		ID:     util.MustParseUUID("33333333-3333-3333-3333-333333333333"),
		Status: "running",
		Error:  pgtype.Text{String: "boom", Valid: true},
	}

	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("expected filtered-out event types to skip emission, got path to exist")
	}

	svc.emitTaskLifecycleEvent(context.Background(), "failure_event", "failed", task, "runtime_offline", "timeout")

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected stage1 log file to exist after filtered-in event: %v", err)
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		t.Fatalf("expected event content to be written")
	}

	var got Stage1LifecycleEvent
	if err := json.Unmarshal([]byte(trimmed), &got); err != nil {
		t.Fatalf("expected valid json event: %v", err)
	}
	if got.EventType != "failure_event" {
		t.Fatalf("expected failure_event, got %q", got.EventType)
	}
}

func TestStage1TelemetryReadsConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "agent-improvement-loop-config.json")
	eventsPath := filepath.Join(tmpDir, "from_config.jsonl")

	if err := os.WriteFile(cfgPath, []byte("{\"stage1\": {\"enabled\": true, \"events_path\": \""+eventsPath+"\", \"emit_categories\": [\"attempt_event\"]}}"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", cfgPath)
	t.Setenv("MULTICA_AIL_STAGE1_EMIT_CATEGORIES", "")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", "")
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "")

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{ID: util.MustParseUUID("44444444-4444-4444-4444-444444444444"), Status: "running"}

	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")
	svc.emitTaskLifecycleEvent(context.Background(), "failure_event", "failed", task, "runtime_offline", "")

	content, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("expected config event file to exist: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Fatalf("config emit categories should filter to one event, got %d", len(lines))
	}
	var evt Stage1LifecycleEvent
	if err := json.Unmarshal([]byte(lines[0]), &evt); err != nil {
		t.Fatalf("expected valid json event: %v", err)
	}
	if evt.EventType != "attempt_event" {
		t.Fatalf("expected attempt_event, got %q", evt.EventType)
	}
}

func TestStage1TelemetryConfigJSONMalformedFallsBackToDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(cfgPath, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", cfgPath)
	t.Setenv("MULTICA_AIL_STAGE1_EMIT_CATEGORIES", "")
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "")
	logPath := filepath.Join(tmpDir, "events.jsonl")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", logPath)

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{ID: util.MustParseUUID("55555555-5555-5555-5555-555555555555"), Status: "running"}
	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")

	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected fallback path write after malformed config: %v", err)
	}
}

func TestStage1TelemetryInvalidPathFallsBackToNoop(t *testing.T) {
	tmpDir := t.TempDir()
	badParent := filepath.Join(tmpDir, "block")
	if err := os.WriteFile(badParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(badParent, "events.jsonl")

	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "true")
	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", "")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", badPath)
	t.Setenv("MULTICA_AIL_STAGE1_EMIT_CATEGORIES", "")

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{ID: util.MustParseUUID("66666666-6666-6666-6666-666666666666"), Status: "running"}
	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")

	if _, err := os.Stat(badPath); err == nil {
		t.Fatalf("expected no stage1 event file when path directory is invalid")
	}
}

func TestStage1TelemetryEventOrderingAndIdempotence(t *testing.T) {
	t.Setenv("MULTICA_AIL_STAGE1_ENABLED", "true")
	t.Setenv("MULTICA_AIL_STAGE1_CONFIG", "")
	logPath := filepath.Join(t.TempDir(), "ordering.jsonl")
	t.Setenv("MULTICA_AIL_STAGE1_EVENTS_PATH", logPath)

	svc := &TaskService{Stage1Telemetry: NewStage1EventSinkFromEnv()}
	task := db.AgentTaskQueue{ID: util.MustParseUUID("77777777-7777-7777-7777-777777777777"), Status: "running"}

	for i := 0; i < 10; i++ {
		svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected stage1 log file to exist: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 emitted events, got %d", len(lines))
	}

	prevTs := time.Time{}
	for i, line := range lines {
		var evt Stage1LifecycleEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("invalid json at line %d: %v", i, err)
		}
		ts, err := time.Parse(time.RFC3339Nano, evt.TS)
		if err != nil {
			t.Fatalf("bad timestamp at line %d: %v", i, err)
		}
		if !prevTs.IsZero() && ts.Before(prevTs) {
			t.Fatalf("event timestamps not ordered: %v before %v at line %d", ts, prevTs, i)
		}
		prevTs = ts
	}
}

func TestEmitTaskLifecycleEventSafeWhenNilTelemetry(t *testing.T) {
	svc := &TaskService{}
	task := db.AgentTaskQueue{ID: util.MustParseUUID("88888888-8888-8888-8888-888888888888"), Status: "running"}
	svc.emitTaskLifecycleEvent(context.Background(), "attempt_event", "running", task, "", "")
}
