package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCreateChatSessionWorkingDirectoryPersistence(t *testing.T) {
	if testHandler == nil || dbfx == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Chat directory host", testutil.Cols{"runtime_mode": "local", "daemon_id": "chat-directory-host", "metadata": testutil.Raw(`'{"capabilities":["chat-working-directory-v1"]}'::jsonb`)})
	agentID := dbfx.Agent(t, "Chat directory agent", runtimeID)
	projectID := dbfx.Project(t, "Chat directory project")
	selected := &protocol.ChatWorkingDirectory{DaemonID: "chat-directory-host", Path: "/tmp/项目 with spaces"}
	req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{"agent_id": agentID, "project_id": projectID, "working_directory": selected}))
	var created ChatSessionResponse
	testutil.Call(t, testHandler.CreateChatSession, req).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id=$1`, created.ID) })
	stored, err := testHandler.Queries.GetChatSession(context.Background(), parseUUID(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	var persisted protocol.ChatWorkingDirectory
	if err := json.Unmarshal(stored.WorkingDirectory, &persisted); err != nil || persisted != *selected {
		t.Fatalf("persisted=%+v, %v", persisted, err)
	}
	if created.WorkingDirectory == nil || *created.WorkingDirectory != *selected {
		t.Fatalf("created directory = %+v", created.WorkingDirectory)
	}
	if created.ProjectID == nil || *created.ProjectID != projectID {
		t.Fatalf("directory selection lost attached project: %+v", created.ProjectID)
	}
	var detail ChatSessionResponse
	detailReq := withURLParam(withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/chat/sessions/"+created.ID, nil)), "sessionId", created.ID)
	testutil.Call(t, testHandler.GetChatSession, detailReq).Want(http.StatusOK).JSON(&detail)
	if detail.WorkingDirectory == nil || *detail.WorkingDirectory != *selected {
		t.Fatalf("detail directory = %+v", detail.WorkingDirectory)
	}
	if stored.WorkDir.Valid {
		t.Fatal("requested directory must not overwrite historical work_dir")
	}
	for _, status := range []string{"", "all"} {
		var rows []ChatSessionResponse
		list := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/chat/sessions?status="+status, nil))
		testutil.Call(t, testHandler.ListChatSessions, list).Want(http.StatusOK).JSON(&rows)
		found := false
		for _, row := range rows {
			if row.ID == created.ID {
				found = true
				if row.WorkingDirectory == nil || *row.WorkingDirectory != *selected {
					t.Fatalf("listed directory=%+v", row.WorkingDirectory)
				}
			}
		}
		if !found {
			t.Fatal("created chat missing from list")
		}
	}
	agent, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	rt, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": created.ID})
		dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": created.ID, "role": "user", "content": "Use the selected directory", "task_id": taskID})
		claimReq := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/claim", nil), "runtimeId", runtimeID)
		claimReq.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityChatWorkingDirectoryV1)
		var claimed struct {
			Task *AgentTaskResponse `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, claimReq).Want(http.StatusOK).JSON(&claimed)
		if claimed.Task == nil || claimed.Task.ID != taskID || claimed.Task.WorkingDirectory == nil || *claimed.Task.WorkingDirectory != *selected {
			t.Fatalf("claim directory response=%+v", claimed.Task)
		}
		dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, taskID)
	}
	rt.DaemonID.String = "moved-host"
	if _, err := chatWorkingDirectoryForClaim(stored, agent, rt, true); err == nil || !strings.Contains(err.Error(), "machine") {
		t.Fatalf("moved runtime error=%v", err)
	}
}

func TestCreateChatSessionWorkingDirectoryRejections(t *testing.T) {
	if testHandler == nil || dbfx == nil {
		t.Skip("database not available")
	}
	otherOwnerID := dbfx.User(t, "Other chat directory owner", "chat-directory-other-owner@example.com")
	runtimeID := dbfx.Runtime(t, "Chat directory validation host", testutil.Cols{"provider": "openclaw", "runtime_mode": "local", "daemon_id": "validation-host", "metadata": testutil.Raw(`'{"capabilities":["chat-working-directory-v1"]}'::jsonb`)})
	agentID := dbfx.Agent(t, "Chat directory validation agent", runtimeID)
	cases := []struct {
		name                                         string
		directory                                    any
		runtimeMode, status, metadata, config, owner string
		want                                         int
	}{
		{name: "relative path", directory: map[string]any{"daemon_id": "validation-host", "path": "relative"}, want: 400},
		{name: "wrong machine", directory: map[string]any{"daemon_id": "other-host", "path": "/tmp"}, want: 400},
		{name: "cloud", runtimeMode: "cloud", want: 400},
		{name: "offline", status: "offline", want: 400},
		{name: "unsupported", metadata: `{}`, want: 400},
		{name: "gateway", config: `{"mode":"gateway"}`, want: 400},
		{name: "non owner", owner: otherOwnerID, want: 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, status, metadata, config, owner := "local", "online", `{"capabilities":["chat-working-directory-v1"]}`, `{}`, testUserID
			if tc.runtimeMode != "" {
				mode = tc.runtimeMode
			}
			if tc.status != "" {
				status = tc.status
			}
			if tc.metadata != "" {
				metadata = tc.metadata
			}
			if tc.config != "" {
				config = tc.config
			}
			if tc.owner != "" {
				owner = tc.owner
			}
			dbfx.Exec(t, `UPDATE agent_runtime SET runtime_mode=$2,status=$3,metadata=$4::jsonb,owner_id=$5 WHERE id=$1`, runtimeID, mode, status, metadata, owner)
			dbfx.Exec(t, `UPDATE agent SET runtime_config=$2::jsonb WHERE id=$1`, agentID, config)
			directory := tc.directory
			if directory == nil {
				directory = map[string]any{"daemon_id": "validation-host", "path": "/tmp/项目"}
			}
			req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", map[string]any{"agent_id": agentID, "working_directory": directory}))
			testutil.Call(t, testHandler.CreateChatSession, req).Want(tc.want)
		})
	}
}

func TestCreateChatSessionWorkingDirectoryDefault(t *testing.T) {
	if testHandler == nil || dbfx == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Default directory cloud runtime")
	agentID := dbfx.Agent(t, "Default directory agent", runtimeID)
	for _, explicitNull := range []bool{false, true} {
		body := map[string]any{"agent_id": agentID}
		if explicitNull {
			body["working_directory"] = nil
		}
		var created ChatSessionResponse
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", body))
		testutil.Call(t, testHandler.CreateChatSession, req).Want(http.StatusCreated).JSON(&created)
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id=$1`, created.ID) })
		if created.WorkingDirectory != nil {
			t.Fatalf("default = %+v", created.WorkingDirectory)
		}
		stored, err := testHandler.Queries.GetChatSession(context.Background(), parseUUID(created.ID))
		if err != nil {
			t.Fatal(err)
		}
		if len(stored.WorkingDirectory) > 0 {
			t.Fatalf("stored default = %s", stored.WorkingDirectory)
		}
	}
}
