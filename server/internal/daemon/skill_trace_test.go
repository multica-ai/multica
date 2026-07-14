package daemon

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillTraceRecorderDisabledDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	rec := NewSkillTraceRecorder(Config{SkillTracePath: path})

	if rec.Enabled() {
		t.Fatal("recorder should be disabled unless SkillTraceEnabled is true")
	}
	if err := rec.Record([]SkillTraceEvent{{EventType: SkillTraceEventLoaded, SkillID: "skill-1"}}); err != nil {
		t.Fatalf("Record on disabled recorder should be a no-op, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled recorder should not create %s, stat err=%v", path, err)
	}
}

func TestSkillTraceRecorderAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces", "skill-invocations.jsonl")
	rec := NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path})

	events := []SkillTraceEvent{
		{EventType: SkillTraceEventLoaded, WorkspaceID: "ws-1", TaskID: "task-1", SkillID: "skill-1", SkillName: "deploy", SkillSource: "workspace", Trigger: SkillTraceTriggerImplicit},
		{EventType: SkillTraceEventInvoked, WorkspaceID: "ws-1", TaskID: "task-1", SkillID: "skill-1", SkillName: "deploy", SkillSource: "workspace", Trigger: SkillTraceTriggerExplicit},
	}
	if err := rec.Record(events); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := readSkillTraceEvents(t, path)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].TS.IsZero() || got[1].TS.IsZero() {
		t.Fatalf("events should be timestamped: %+v", got)
	}
	if got[0].EventType != SkillTraceEventLoaded || got[0].Trigger != SkillTraceTriggerImplicit {
		t.Fatalf("first event = %+v, want implicit loaded event", got[0])
	}
	if got[1].EventType != SkillTraceEventInvoked || got[1].Trigger != SkillTraceTriggerExplicit {
		t.Fatalf("second event = %+v, want explicit invocation event", got[1])
	}
}

func TestBuildSkillTraceEventsSeparatesLoadedAndExplicitInvocations(t *testing.T) {
	task := Task{
		ID:            "task-1",
		WorkspaceID:   "ws-1",
		IssueID:       "issue-1",
		RuntimeID:     "rt-1",
		ChatSessionID: "chat-1",
		ChatMessage:   "[/Deploy](slash://skill/skill-1) and again [/Deploy](slash://skill/skill-1) plus [/Unknown](slash://skill/missing)",
		InitiatorType: "member",
		InitiatorID:   "member-1",
		InitiatorName: "Jane Doe",
		Agent: &AgentData{
			ID:   "agent-1",
			Name: "Builder",
		},
	}
	skills := []SkillData{
		{ID: "skill-1", Source: "workspace", Name: "deploy", Hash: "sha256:one"},
		{ID: "skill-2", Source: "builtin", Name: "multica-working-on-issues", Hash: "sha256:two"},
	}
	meta := skillTraceMeta{
		Provider:         "codex",
		MachineID:        "daemon-1",
		DeviceName:       "laptop",
		DaemonProfile:    "default",
		RuntimeProfileID: "profile-1",
	}

	events := buildSkillTraceEvents(task, skills, meta)
	if len(events) != 3 {
		t.Fatalf("got %d events, want two loaded events and one explicit invocation: %+v", len(events), events)
	}

	if events[0].EventType != SkillTraceEventLoaded || events[0].SkillID != "skill-1" || events[0].Trigger != SkillTraceTriggerImplicit {
		t.Fatalf("event[0] = %+v, want implicit loaded skill-1", events[0])
	}
	if events[1].EventType != SkillTraceEventLoaded || events[1].SkillID != "skill-2" || events[1].Trigger != SkillTraceTriggerImplicit {
		t.Fatalf("event[1] = %+v, want implicit loaded skill-2", events[1])
	}
	if events[2].EventType != SkillTraceEventInvoked || events[2].SkillID != "skill-1" || events[2].Trigger != SkillTraceTriggerExplicit {
		t.Fatalf("event[2] = %+v, want explicit invocation for skill-1", events[2])
	}
	if events[2].EmployeeID != "member-1" || events[2].EmployeeName != "Jane Doe" || events[2].EmployeeType != "member" {
		t.Fatalf("employee fields = (%q,%q,%q), want member initiator", events[2].EmployeeID, events[2].EmployeeName, events[2].EmployeeType)
	}
	if events[2].AgentID != "agent-1" || events[2].AgentName != "Builder" || events[2].RuntimeProfileID != "profile-1" {
		t.Fatalf("agent/runtime fields not propagated: %+v", events[2])
	}
}

func TestDaemonRecordSkillTraceWritesRuntimeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	d := &Daemon{
		cfg: Config{
			DaemonID:   "daemon-1",
			DeviceName: "laptop",
			Profile:    "default",
		},
		skillTrace: NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path}),
		runtimeIndex: map[string]Runtime{
			"rt-1": {ID: "rt-1", Provider: "codex", ProfileID: "profile-1"},
		},
	}
	task := Task{
		ID:          "task-1",
		WorkspaceID: "ws-1",
		RuntimeID:   "rt-1",
		ChatMessage: "[/Deploy](slash://skill/skill-1)",
		Agent: &AgentData{
			ID:   "agent-1",
			Name: "Builder",
		},
	}
	skills := []SkillData{{ID: "skill-1", Source: "workspace", Name: "deploy", Hash: "sha256:one"}}

	d.recordSkillTrace(task, skills, "codex", slog.New(slog.NewTextHandler(io.Discard, nil)))

	got := readSkillTraceEvents(t, path)
	if len(got) != 2 {
		t.Fatalf("got %d events, want loaded and explicit invocation events: %+v", len(got), got)
	}
	for _, ev := range got {
		if ev.Provider != "codex" || ev.MachineID != "daemon-1" || ev.DeviceName != "laptop" || ev.DaemonProfile != "default" {
			t.Fatalf("daemon metadata not propagated: %+v", ev)
		}
		if ev.RuntimeProfileID != "profile-1" {
			t.Fatalf("runtime profile id = %q, want profile-1 in %+v", ev.RuntimeProfileID, ev)
		}
	}
}

func readSkillTraceEvents(t *testing.T, path string) []SkillTraceEvent {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trace file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var got []SkillTraceEvent
	for scanner.Scan() {
		var ev SkillTraceEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("decode line %q: %v", scanner.Text(), err)
		}
		got = append(got, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trace file: %v", err)
	}
	return got
}
