package traceupload

import (
	"encoding/json"
	"testing"
)

// CP1: a closed task produces a sidecar with all context fields populated and
// serialized into the trace_meta line exactly as the registry reader expects.
func TestSidecarMetaLineAllFields(t *testing.T) {
	s := Sidecar{
		Runtime:          "claude",
		TaskID:           "task-1",
		SessionID:        "sess-1",
		IssueID:          "issue-1",
		AgentID:          "agent-1",
		AgentName:        "Mia",
		WorkspaceID:      "ws-1",
		ProjectID:        "proj-1",
		AutopilotID:      "auto-1",
		ParentTaskID:     "parent-1",
		CreatedByType:    "member",
		Surface:          "issue",
		TriggerUserID:    "user-1",
		TriggerUserName:  "Jesper",
		IssueTitle:       "FIR-2763: Token-spend observability",
		ParentIssueTitle: "FIR-2759: Vurder vores trace-system",
		ProjectName:      "Agent platform",
	}
	line, err := s.MetaLine()
	if err != nil {
		t.Fatalf("MetaLine: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("unmarshal meta line: %v", err)
	}
	want := map[string]string{
		"type":               "trace_meta",
		"runtime":            "claude",
		"task_id":            "task-1",
		"session_id":         "sess-1",
		"issue_id":           "issue-1",
		"agent_id":           "agent-1",
		"agent_name":         "Mia",
		"workspace_id":       "ws-1",
		"project_id":         "proj-1",
		"autopilot_id":       "auto-1",
		"parent_task_id":     "parent-1",
		"created_by_type":    "member",
		"surface":            "issue",
		"trigger_user_id":    "user-1",
		"trigger_user_name":  "Jesper",
		"issue_title":        "FIR-2763: Token-spend observability",
		"parent_issue_title": "FIR-2759: Vurder vores trace-system",
		"project_name":       "Agent platform",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("meta[%q] = %v, want %q", k, got[k], v)
		}
	}
}

// Empty optional fields are omitted so the registry reads them as null.
func TestSidecarMetaLineOmitsEmpty(t *testing.T) {
	s := Sidecar{Runtime: "codex", TaskID: "t", SessionID: "s"}
	line, err := s.MetaLine()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(line, &got)
	for _, k := range []string{"issue_id", "agent_id", "project_id", "autopilot_id", "parent_task_id", "created_by_type", "surface", "trigger_user_id", "trigger_user_name", "issue_title", "parent_issue_title", "project_name"} {
		if _, present := got[k]; present {
			t.Errorf("expected %q omitted, but present", k)
		}
	}
	// Required fields always present.
	for _, k := range []string{"type", "runtime", "task_id", "session_id"} {
		if _, present := got[k]; !present {
			t.Errorf("expected %q present", k)
		}
	}
}

func TestSidecarValid(t *testing.T) {
	cases := []struct {
		name string
		s    Sidecar
		want bool
	}{
		{"ok claude", Sidecar{Runtime: "claude", TaskID: "t", SessionID: "s"}, true},
		{"ok codex", Sidecar{Runtime: "codex", TaskID: "t", SessionID: "s"}, true},
		{"no task", Sidecar{Runtime: "claude", SessionID: "s"}, false},
		{"no session", Sidecar{Runtime: "claude", TaskID: "t"}, false},
		{"bad runtime", Sidecar{Runtime: "gemini", TaskID: "t", SessionID: "s"}, false},
	}
	for _, c := range cases {
		if got := c.s.Valid(); got != c.want {
			t.Errorf("%s: Valid()=%v want %v", c.name, got, c.want)
		}
	}
}

func TestSidecarKey(t *testing.T) {
	s := Sidecar{TaskID: "t1", SessionID: "s1"}
	if s.Key() != "t1|s1" {
		t.Errorf("Key()=%q", s.Key())
	}
}
