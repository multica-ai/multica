package daemon

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestSkillTraceRecorderDisabledDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	recorder := NewSkillTraceRecorder(Config{SkillTracePath: path})

	if recorder.Enabled() {
		t.Fatal("recorder should be disabled by default")
	}
	if err := recorder.Record([]SkillTraceEvent{{EventType: SkillTraceEventInvoked}}); err != nil {
		t.Fatalf("Record disabled: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled recorder created a file: %v", err)
	}
}

func TestSkillInvocationTrackerRecordsNativeModelSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	tracker := newSkillInvocationTracker(
		NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path}),
		skillTraceTaskFixture(""),
		[]SkillData{
			{ID: "skill-1", Source: "workspace", Name: "Review Helper", Hash: "sha256:one"},
			{ID: "skill-2", Source: "workspace", Name: "Another Skill", Hash: "sha256:two"},
		},
		skillTraceMeta{Provider: "claude", MachineID: "daemon-1", DeviceName: "laptop"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	msg := agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "Skill",
		CallID: "call-1",
		Input:  map[string]any{"skill": "review-helper", "args": "another-skill"},
	}
	tracker.Observe(msg)
	tracker.Observe(msg) // repeated provider progress for one call is not usage twice

	events := readSkillTraceEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
	event := events[0]
	if event.EventType != SkillTraceEventInvoked || event.SkillID != "skill-1" || event.SkillName != "Review Helper" {
		t.Fatalf("unexpected invocation identity: %+v", event)
	}
	if event.Trigger != SkillTraceTriggerModel || event.ObservedVia != SkillTraceObservedNativeTool || event.ToolCallID != "call-1" {
		t.Fatalf("unexpected invocation evidence: %+v", event)
	}
	if event.TaskID != "task-1" || event.WorkspaceID != "workspace-1" || event.Provider != "claude" || event.MachineID != "daemon-1" {
		t.Fatalf("task/runtime metadata missing: %+v", event)
	}
	if event.EmployeeID != "member-1" || event.EmployeeName != "Jane" || event.EmployeeType != "member" {
		t.Fatalf("initiator attribution missing: %+v", event)
	}
}

func TestSkillInvocationTrackerRecordsActualSkillFileRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	task := skillTraceTaskFixture("")
	task.TriggerCommentContent = "[/Platform](slash://skill/skill-1)"
	tracker := newSkillInvocationTracker(
		NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path}),
		task,
		[]SkillData{{ID: "skill-1", Source: "builtin", Name: "Multica Platform", Hash: "sha256:platform"}},
		skillTraceMeta{Provider: "codex", DaemonProfile: "desktop"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	tracker.Observe(agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "functions.exec",
		CallID: "call-read",
		Input: map[string]any{
			"source": `await tools.exec_command({cmd: "sed -n '1,220p' /task/codex-home/skills/multica-platform/SKILL.md"})`,
		},
	})

	events := readSkillTraceEvents(t, path)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
	event := events[0]
	if event.Trigger != SkillTraceTriggerExplicit || event.ObservedVia != SkillTraceObservedFileRead {
		t.Fatalf("unexpected explicit read evidence: %+v", event)
	}
	if event.SkillID != "skill-1" || event.SkillHash != "sha256:platform" || event.Provider != "codex" {
		t.Fatalf("unexpected skill/runtime fields: %+v", event)
	}
}

func TestSkillInvocationTrackerDoesNotTreatInventoryOrMentionsAsUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	tracker := newSkillInvocationTracker(
		NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path}),
		skillTraceTaskFixture("review-helper is available"),
		[]SkillData{{ID: "skill-1", Source: "workspace", Name: "Review Helper"}},
		skillTraceMeta{Provider: "codex"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	// Constructing the tracker represents task preparation/mounting and must
	// not create the old false-positive skill_loaded inventory rows.
	tracker.Observe(agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "exec_command",
		CallID: "call-mention",
		Input:  map[string]any{"command": "echo review-helper"},
	})
	tracker.Observe(agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "patch_apply",
		CallID: "call-edit",
		Input:  map[string]any{"path": "/task/codex-home/skills/review-helper/SKILL.md"},
	})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("non-invocation signals created trace output: %v", err)
	}
}

func TestSkillInvocationTrackerMatchesDistinctMountedSkillSlugs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skill-invocations.jsonl")
	tracker := newSkillInvocationTracker(
		NewSkillTraceRecorder(Config{SkillTraceEnabled: true, SkillTracePath: path}),
		skillTraceTaskFixture(""),
		[]SkillData{
			{ID: "skill-1", Name: "A B"},
			{ID: "skill-2", Name: "A-B"},
		},
		skillTraceMeta{Provider: "codex"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	tracker.Observe(agent.Message{
		Type:   agent.MessageToolUse,
		Tool:   "exec_command",
		CallID: "call-second",
		Input:  map[string]any{"command": "cat /task/codex-home/skills/a-b-multica/SKILL.md"},
	})

	events := readSkillTraceEvents(t, path)
	if len(events) != 1 || events[0].SkillID != "skill-2" {
		t.Fatalf("resolved slug mapped to wrong skill: %+v", events)
	}
}

func skillTraceTaskFixture(chatMessage string) Task {
	return Task{
		ID:            "task-1",
		AgentID:       "agent-1",
		RuntimeID:     "runtime-1",
		IssueID:       "issue-1",
		WorkspaceID:   "workspace-1",
		ChatMessage:   chatMessage,
		InitiatorType: "member",
		InitiatorID:   "member-1",
		InitiatorName: "Jane",
		Agent: &AgentData{
			ID:   "agent-1",
			Name: "Builder",
		},
	}
}

func readSkillTraceEvents(t *testing.T, path string) []SkillTraceEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open skill trace: %v", err)
	}
	defer f.Close()

	var events []SkillTraceEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event SkillTraceEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan skill trace: %v", err)
	}
	return events
}
