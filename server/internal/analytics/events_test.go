package analytics

import "testing"

func TestRuntimeReadyOmitsUnmeasuredDuration(t *testing.T) {
	ev := RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 0)
	if _, ok := ev.Properties["ready_duration_ms"]; ok {
		t.Fatalf("ready_duration_ms should be omitted until it is measured")
	}

	ev = RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 123)
	if got := ev.Properties["ready_duration_ms"]; got != int64(123) {
		t.Fatalf("ready_duration_ms = %v, want 123", got)
	}
}

func TestFailedEventsUseWillRetry(t *testing.T) {
	ctx := TaskContext{
		UserID:      "user-1",
		WorkspaceID: "workspace-1",
		AgentID:     "agent-1",
		TaskID:      "task-1",
		Source:      SourceManual,
	}
	taskEv := AgentTaskFailed(ctx, 10, "runtime_offline", "runtime", true)
	if got := taskEv.Properties["will_retry"]; got != true {
		t.Fatalf("task will_retry = %v, want true", got)
	}
	if _, ok := taskEv.Properties["recoverable"]; ok {
		t.Fatalf("task failure should not emit recoverable")
	}

	runEv := AutopilotRunFailed("user-1", "workspace-1", "autopilot-1", "run-1", "agent-1", "manual", "task failed", "task_error", false, 10)
	if got := runEv.Properties["will_retry"]; got != false {
		t.Fatalf("autopilot will_retry = %v, want false", got)
	}
	if _, ok := runEv.Properties["recoverable"]; ok {
		t.Fatalf("autopilot failure should not emit recoverable")
	}
}

func TestAgentTaskDispatchedUsesTaskCoreProperties(t *testing.T) {
	ctx := TaskContext{
		UserID:      "user-1",
		WorkspaceID: "workspace-1",
		AgentID:     "agent-1",
		TaskID:      "task-1",
		IssueID:     "issue-1",
		Source:      SourceManual,
		RuntimeMode: "local",
		Provider:    "codex",
	}
	ev := AgentTaskDispatched(ctx)

	if ev.Name != EventAgentTaskDispatched {
		t.Fatalf("event name = %q, want %q", ev.Name, EventAgentTaskDispatched)
	}
	if got := ev.WorkspaceID; got != "workspace-1" {
		t.Fatalf("workspace_id = %q, want workspace-1", got)
	}
	if got := ev.Properties["task_id"]; got != "task-1" {
		t.Fatalf("task_id = %v, want task-1", got)
	}
	if got := ev.Properties["runtime_mode"]; got != "local" {
		t.Fatalf("runtime_mode = %v, want local", got)
	}
}
