package daemon

import "testing"

func TestBuildMulticaTaskMyMirEvent(t *testing.T) {
	t.Parallel()
	task := Task{
		ID:                    "task-1234567890",
		AgentID:               "agent-fallback",
		RuntimeID:             "runtime-1",
		IssueID:               "issue-1",
		WorkspaceID:           "workspace-1",
		ProjectID:             "project-1",
		ProjectTitle:          "Project One",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "please do the thing\nwith context",
		Agent: &AgentData{
			ID:   "agent-1",
			Name: "Hermes",
		},
	}
	result := TaskResult{
		Status:        "completed",
		Comment:       "done\nwith proof",
		SessionID:     "session-1",
		WorkDir:       "/tmp/work",
		FailureReason: "",
	}

	event := buildMulticaTaskMyMirEvent(task, "hermes", result)
	if event.Provider != "hermes" || event.TaskID != task.ID || event.Status != "completed" {
		t.Fatalf("unexpected core event fields: %#v", event)
	}
	if event.AgentID != "agent-1" || event.AgentName != "Hermes" {
		t.Fatalf("agent fields not resolved from task.Agent: %#v", event)
	}
	if event.SessionID != "session-1" || event.WorkDir != "/tmp/work" {
		t.Fatalf("result fields missing: %#v", event)
	}
	if event.TriggerCommentContent != "please do the thing with context" {
		t.Fatalf("trigger comment not log-normalized: %q", event.TriggerCommentContent)
	}
	if event.Comment != "done with proof" {
		t.Fatalf("comment not log-normalized: %q", event.Comment)
	}
}
