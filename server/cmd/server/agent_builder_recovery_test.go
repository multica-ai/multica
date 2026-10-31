package main

import (
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http"
	"testing"
)

// This wire-level regression compiles against the old server too: that server
// cannot discover the hidden empty session, and responds 404 to the first GET.
func TestEmptyBuilderRecoveryThroughRouter(t *testing.T) {
	f := testutil.New(testPool, testWorkspaceID, testUserID)
	p := f.Insert(t, "runtime_profile", testutil.Cols{"workspace_id": testWorkspaceID, "display_name": "Recovery router", "protocol_family": "codex", "command_name": "router-recovery", "created_by": testUserID})
	r := f.Insert(t, "agent_runtime", testutil.Cols{"workspace_id": testWorkspaceID, "name": "Router host", "runtime_mode": "local", "provider": "codex", "status": "online", "owner_id": testUserID, "profile_id": p})
	create := authRequest(t, "POST", "/api/agent-builder/sessions", map[string]any{"runtime_id": r})
	if create.StatusCode != 201 {
		t.Fatalf("create status=%d", create.StatusCode)
	}
	var created struct {
		SessionID      string `json:"session_id"`
		BuilderAgentID string `json:"builder_agent_id"`
	}
	readJSON(t, create, &created)
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM agent_builder_draft WHERE chat_session_id=$1`, created.SessionID)
		testPool.Exec(t.Context(), `DELETE FROM agent WHERE id=$1`, created.BuilderAgentID)
	})
	path := "/api/agent-builder/sessions/empty?profile_id=" + p
	res := authRequest(t, "GET", path, nil)
	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		t.Fatalf("empty-session discovery status=%d, want 200", res.StatusCode)
	}
	var page struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Snapshot  string `json:"snapshot"`
		} `json:"sessions"`
		NextCursor string `json:"next_cursor"`
	}
	readJSON(t, res, &page)
	if len(page.Sessions) != 1 || page.Sessions[0].SessionID != created.SessionID || page.NextCursor != "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	row := page.Sessions[0]
	// A successful autosave after discovery must defeat stale conditional delete.
	saved := authRequest(t, "PUT", "/api/agent-builder/sessions/"+row.SessionID+"/draft", map[string]any{"draft": map[string]any{"name": "Keep me"}})
	saved.Body.Close()
	if saved.StatusCode != 204 {
		t.Fatal(saved.StatusCode)
	}
	discarded := authRequest(t, "POST", "/api/agent-builder/sessions/"+row.SessionID+"/discard-empty", map[string]any{"profile_id": p, "snapshot": row.Snapshot})
	discarded.Body.Close()
	if discarded.StatusCode != 409 {
		t.Fatalf("stale discard=%d, want409", discarded.StatusCode)
	}
	var draft []byte
	f.QueryRow(t, `SELECT draft FROM agent_builder_draft WHERE chat_session_id=$1`, row.SessionID).Scan(&draft)
	var config map[string]any
	if err := json.Unmarshal(draft, &config); err != nil || config["name"] != "Keep me" {
		t.Fatal("saved draft lost", string(draft), err)
	}
}
