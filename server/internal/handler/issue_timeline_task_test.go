package handler

import "testing"

func TestIssueTimelineTaskResponse(t *testing.T) {
	full := AgentTaskResponse{
		ID: "task", AgentID: "agent", Status: "completed", HandoffNote: "private handoff",
		Result:              map[string]any{"comment": "inline reply", "output": "large provider output", "session_id": "session"},
		DeliveredCommentIDs: []string{"input"},
		Usage:               []TaskUsageData{{Provider: "codex"}},
	}
	index := issueTimelineTaskResponse(full)
	if index.ID != full.ID || index.Status != full.Status || index.AgentID != full.AgentID {
		t.Fatal("timeline view must preserve run identity and status")
	}
	result, ok := index.Result.(map[string]any)
	if !ok || len(result) != 1 || result["comment"] != "inline reply" {
		t.Fatalf("inline reply must remain available, got %#v", index.Result)
	}
	if index.HandoffNote != "" || len(index.DeliveredCommentIDs) != 1 || len(index.Usage) != 1 {
		t.Fatal("omit handoff instructions, preserve delivery and usage")
	}
	if full.HandoffNote == "" || len(full.Result.(map[string]any)) != 3 {
		t.Fatal("projection must not mutate full history")
	}
	full.Result = map[string]any{"output": "provider-only output"}
	if issueTimelineTaskResponse(full).Result != nil {
		t.Fatal("provider-only output must not enter the timeline index")
	}
}
