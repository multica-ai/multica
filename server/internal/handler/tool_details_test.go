package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// restrictedTranscript seeds a run of an agent owned by a plain member, with
// the agent's tool details restricted.
func restrictedTranscript(t *testing.T, label string) (taskID, agentOwner string) {
	t.Helper()
	taskID, _ = seedTranscript(t, label)
	agentOwner = createTestUserAndMember(t, "member")
	dbfx.Exec(t, `UPDATE agent SET owner_id = $1, tool_details_visibility = 'restricted'
		WHERE id = (SELECT agent_id FROM agent_task_queue WHERE id = $2)`, agentOwner, taskID)
	// Runs before the user is deleted, which the agent would otherwise block.
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE owner_id = $2`, testUserID, agentOwner)
	})
	return taskID, agentOwner
}

func readTranscript(t *testing.T, taskID, userID string, headers map[string]string) map[int]protocol.TaskMessagePayload {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/messages", nil), userID), "taskId", taskID)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	rec := httptest.NewRecorder()
	testHandler.ListTaskMessagesByUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read transcript = %d %s", rec.Code, rec.Body.String())
	}
	var msgs []protocol.TaskMessagePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &msgs); err != nil {
		t.Fatal(err)
	}
	bySeq := map[int]protocol.TaskMessagePayload{}
	for _, m := range msgs {
		bySeq[m.Seq] = m
	}
	return bySeq
}

func TestTaskMessagesHideToolDetailsOutsideTheAgentsAudience(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID, agentOwner := restrictedTranscript(t, "tool-details-read")
	admin := createTestUserAndMember(t, "admin")
	member := createTestUserAndMember(t, "member")

	for _, tc := range []struct {
		name    string
		userID  string
		headers map[string]string
		full    bool
	}{
		{name: "agent owner", userID: agentOwner, full: true},
		{name: "workspace owner", userID: testUserID, full: true},
		{name: "workspace admin", userID: admin, full: true},
		{name: "plain member", userID: member},
		{name: "an agent acting for the workspace owner", userID: testUserID,
			headers: map[string]string{"X-Actor-Source": "task_token", "X-Agent-ID": "00000000-0000-0000-0000-000000000001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := readTranscript(t, taskID, tc.userID, tc.headers)
			tool := msgs[2]
			if tool.Tool != "fs_read" || tool.Type != "tool_use" {
				t.Fatalf("tool call lost its identity: %+v", tool)
			}
			if tc.full {
				if tool.DetailsHidden || tool.Input["path"] != "/etc/hosts" || tool.Output == "" {
					t.Errorf("reader got %+v, want the whole tool call", tool)
				}
			} else if !tool.DetailsHidden || tool.Input != nil || tool.Output != "" {
				t.Errorf("non-reader got %+v, want the tool call without input or output", tool)
			}
			if msgs[1].Content != "planning the run" || msgs[3].Content != "done" {
				t.Errorf("reasoning or prose was hidden: thinking=%q text=%q", msgs[1].Content, msgs[3].Content)
			}
		})
	}

	dbfx.Exec(t, `UPDATE agent SET tool_details_visibility = 'everyone'
		WHERE id = (SELECT agent_id FROM agent_task_queue WHERE id = $1)`, taskID)
	if tool := readTranscript(t, taskID, member, nil)[2]; tool.DetailsHidden || tool.Input == nil {
		t.Errorf("an unrestricted agent hid its tool call from a member: %+v", tool)
	}
}

func TestReportTaskMessagesMarksRestrictedToolDetails(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID, agentOwner := restrictedTranscript(t, "tool-details-event")
	admin := createTestUserAndMember(t, "admin")
	member := createTestUserAndMember(t, "member")

	var mu sync.Mutex
	var got []events.Event
	testHandler.Bus.Subscribe(protocol.EventTaskMessage, func(e events.Event) {
		if e.TaskID == taskID {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
	})
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 10, "type": "tool_use", "tool": "Bash", "input": map[string]any{"command": "env"}},
	})).Want(http.StatusOK)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("published %d events, want 1", len(got))
	}
	e := got[0]
	if !e.ToolDetailsRestricted {
		t.Fatal("event from a restricted agent is not marked restricted")
	}
	for _, reader := range []string{agentOwner, testUserID, admin} {
		if !e.ToolDetailReaders[reader] {
			t.Errorf("reader %s missing from %v", reader, e.ToolDetailReaders)
		}
	}
	if e.ToolDetailReaders[member] {
		t.Errorf("plain member %s listed as a reader", member)
	}
	if p := e.Payload.(protocol.TaskMessagePayload); p.Input["command"] != "env" {
		t.Errorf("the event must carry the whole call for readers, got %+v", p)
	}
}

func updateToolDetails(t *testing.T, agentID, userID string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodPut, "/api/agents/"+agentID, body), userID), "id", agentID)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	rec := httptest.NewRecorder()
	testHandler.UpdateAgent(rec, req)
	return rec
}

func TestUpdateAgentToolDetailsVisibility(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentOwner := createTestUserAndMember(t, "member")
	agentID := dbfx.Agent(t, "tool-details-settings agent", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": agentOwner, "custom_env": testutil.Raw("'{}'::jsonb"), "custom_args": testutil.Raw("'[]'::jsonb"),
	})
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE agent SET owner_id = $1 WHERE owner_id = $2`, testUserID, agentOwner)
	})
	visibility := func() string {
		a, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
		if err != nil {
			t.Fatal(err)
		}
		return a.ToolDetailsVisibility
	}
	if v := visibility(); v != "everyone" {
		t.Fatalf("new agent tool_details_visibility = %q, want everyone", v)
	}

	rec := updateToolDetails(t, agentID, agentOwner, map[string]any{"tool_details_visibility": "restricted"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner update = %d %s", rec.Code, rec.Body.String())
	}
	var resp AgentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ToolDetailsVisibility != "restricted" || visibility() != "restricted" {
		t.Fatalf("response=%q stored=%q, want restricted", resp.ToolDetailsVisibility, visibility())
	}

	admin := createTestUserAndMember(t, "admin")
	if rec := updateToolDetails(t, agentID, admin, map[string]any{"tool_details_visibility": "everyone"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin update = %d %s", rec.Code, rec.Body.String())
	}

	for _, tc := range []struct {
		name    string
		userID  string
		value   string
		headers map[string]string
		want    int
	}{
		{name: "unknown value", userID: agentOwner, value: "owner_only", want: http.StatusBadRequest},
		{name: "plain member", userID: createTestUserAndMember(t, "member"), value: "restricted", want: http.StatusForbidden},
		{name: "agent credential", userID: agentOwner, value: "restricted", want: http.StatusForbidden,
			headers: map[string]string{"X-Actor-Source": "task_token", "X-Agent-ID": agentID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := updateToolDetails(t, agentID, tc.userID, map[string]any{"tool_details_visibility": tc.value}, tc.headers); rec.Code != tc.want {
				t.Fatalf("update = %d %s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if v := visibility(); v != "everyone" {
				t.Fatalf("refused update changed the setting to %q", v)
			}
		})
	}
}
