package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskCLIReadsOtherChatSessionsWithWorkspaceSelector(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_TOKEN", "mat_test_task")
	t.Setenv("MULTICA_AGENT_ID", "source-agent")
	t.Setenv("MULTICA_TASK_ID", "source-task")
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Workspace-ID") != "target-workspace" || r.Header.Get("Authorization") != "Bearer mat_test_task" {
			t.Errorf("wrong task request identity")
		}
		seen[r.URL.RequestURI()] = true
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "chat-1"}})
	}))
	defer srv.Close()
	cmd := newRuntimeDeleteTestCmd(srv.URL)
	_ = cmd.Flags().Set("workspace-id", "target-workspace")
	if err := runChatSessionsList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	cmd.Flags().Bool("all-members", true, "")
	cmd.Flags().String("query", "incident", "")
	cmd.Flags().Int("offset", 100, "")
	if err := runChatSessionsList(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if err := runChatSessionsGet(cmd, []string{"chat-1"}); err != nil {
		t.Fatal(err)
	}
	if err := runChatSessionsMessages(cmd, []string{"chat-1"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/chat/sessions?status=all", "/api/chat/superuser/sessions?q=incident&offset=100", "/api/chat/sessions/chat-1", "/api/chat/sessions/chat-1/messages"} {
		if !seen[path] {
			t.Errorf("missing request %s", path)
		}
	}
}
