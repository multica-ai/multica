package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type chatPrivacyFrame struct {
	Type string `json:"type"`
	raw  string
}

// readChatPrivacyFrames reads conn until a frame of type stop arrives and
// returns every frame before it.
func readChatPrivacyFrames(t *testing.T, conn *websocket.Conn, stop string) []chatPrivacyFrame {
	t.Helper()
	var frames []chatPrivacyFrame
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %s: %v (got %d frames)", stop, err, len(frames))
		}
		var f chatPrivacyFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("decode frame %s: %v", raw, err)
		}
		if f.Type == stop {
			return frames
		}
		f.raw = string(raw)
		frames = append(frames, f)
	}
}

func chatPrivacyRequest(t *testing.T, server *httptest.Server, token, method, path string, body any, out any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		var e map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&e)
		t.Fatalf("%s %s: status %d %v", method, path, resp.StatusCode, e)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
}

// A member's chat with an agent is private to that member. Another member of
// the same workspace, connected to the same workspace room, must not receive
// the user turn, the agent's streamed transcript, or the final reply — while
// the owner still receives all three, and every member still sees the task
// lifecycle that drives agent presence.
func TestChatRealtimeIsPrivateToSessionOwner(t *testing.T) {
	ctx := context.Background()
	fx := workspaceFixture(t)
	bystanderID := fx.User(t, "Chat bystander", "chat-privacy-bystander@example.test")
	fx.Member(t, testWorkspaceID, bystanderID, "member")
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Integration Test Agent'`,
		testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}

	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)
	router, h := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	owner, ownerToken := realtimeMembershipConn(t, server, testUserID, testWorkspaceID)
	bystander, _ := realtimeMembershipConn(t, server, bystanderID, testWorkspaceID)

	const userTurn = "private-user-turn-7f3a"
	const transcript = "private-transcript-9c1d"
	const reply = "private-reply-4b8e"
	const followUp = "private-follow-up-2e6a"

	var session struct {
		ID string `json:"id"`
	}
	chatPrivacyRequest(t, server, ownerToken, http.MethodPost, "/api/chat/sessions",
		map[string]any{"agent_id": agentID}, &session)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, session.ID)
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, session.ID)
	})
	var sent struct {
		TaskID string `json:"task_id"`
	}
	chatPrivacyRequest(t, server, ownerToken, http.MethodPost, "/api/chat/sessions/"+session.ID+"/messages",
		map[string]any{"content": userTurn}, &sent)

	// The daemon claims and runs the task, streams a transcript row, then
	// completes with the reply — through the production producers.
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now() WHERE id = $1`,
		sent.TaskID); err != nil {
		t.Fatalf("start task: %v", err)
	}
	report := testutil.JSONRequest(http.MethodPost, "/api/daemon/tasks/"+sent.TaskID+"/messages",
		map[string]any{"messages": []any{map[string]any{"seq": 1, "type": "text", "content": transcript}}})
	report = testutil.WithURLParams(report, "taskId", sent.TaskID)
	report.Header.Set("X-User-ID", testUserID)
	testutil.Call(t, h.ReportTaskMessages, report).Want(http.StatusOK)
	result, _ := json.Marshal(map[string]string{"output": reply})
	completed, err := h.TaskService.CompleteTask(ctx, util.MustParseUUID(sent.TaskID), result, "", "", "", false, "", "")
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if err := h.TaskService.SupplementChatQuickActions(ctx, *completed,
		`[{"label":"`+followUp+`","prompt":"`+followUp+`"}]`, false); err != nil {
		t.Fatalf("supplement quick actions: %v", err)
	}

	// A workspace event published last marks the end of what each socket got.
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: testWorkspaceID, Payload: map[string]string{}})
	ownerFrames := readChatPrivacyFrames(t, owner, protocol.EventIssueUpdated)
	bystanderFrames := readChatPrivacyFrames(t, bystander, protocol.EventIssueUpdated)

	ownerGot := map[string]string{}
	for _, f := range ownerFrames {
		ownerGot[f.Type] += f.raw
	}
	for typ, secret := range map[string]string{
		protocol.EventChatMessage:      userTurn,
		protocol.EventTaskMessage:      transcript,
		protocol.EventChatDone:         reply,
		protocol.EventChatQuickActions: followUp,
	} {
		if !strings.Contains(ownerGot[typ], secret) {
			t.Errorf("owner did not receive %s carrying %q; got %v", typ, secret, ownerGot)
		}
	}

	var bystanderTypes []string
	for _, f := range bystanderFrames {
		bystanderTypes = append(bystanderTypes, f.Type)
		if strings.HasPrefix(f.Type, "chat:") || f.Type == protocol.EventTaskMessage {
			t.Errorf("bystander received %s: %s", f.Type, f.raw)
		}
		for _, secret := range []string{userTurn, transcript, reply, followUp} {
			if strings.Contains(f.raw, secret) {
				t.Errorf("bystander frame %s leaked %q: %s", f.Type, secret, f.raw)
			}
		}
	}
	hasCompleted := false
	for _, typ := range bystanderTypes {
		hasCompleted = hasCompleted || typ == protocol.EventTaskCompleted
	}
	if !hasCompleted {
		t.Errorf("bystander lost task:completed, which drives agent presence; got %v", bystanderTypes)
	}
}

// Removal from a workspace must stop that workspace's chat content even while
// the creator stays connected to another workspace. That connection is
// authorized against its own workspace only, so user-scope delivery would
// carry the agent's transcript and reply past the removal.
func TestChatRealtimeStopsAtWorkspaceRemoval(t *testing.T) {
	ctx := context.Background()
	fx := workspaceFixture(t)
	creatorID := fx.User(t, "Removed chat creator", "chat-removed-creator@example.test")
	memberID := fx.Member(t, testWorkspaceID, creatorID, "member")
	otherWS := fx.Workspace(t, "Still accessible workspace", "chat-removal-other-workspace")
	fx.Member(t, otherWS, creatorID, "owner")
	var runtimeID string
	fx.QueryRow(t, `SELECT runtime_id FROM agent WHERE workspace_id=$1 AND name='Integration Test Agent'`, testWorkspaceID).Scan(&runtimeID)
	agentID := fx.Agent(t, "Workspace shared agent", runtimeID, testutil.Cols{
		"visibility": "workspace", "permission_mode": "public_to",
	})
	fx.Insert(t, "agent_invocation_target", testutil.Cols{
		"agent_id": agentID, "target_type": "workspace", "target_id": testWorkspaceID,
	})

	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)
	router, h := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	other, creatorToken := realtimeMembershipConn(t, server, creatorID, otherWS)

	var session struct {
		ID string `json:"id"`
	}
	chatPrivacyRequest(t, server, creatorToken, http.MethodPost, "/api/chat/sessions",
		map[string]any{"agent_id": agentID}, &session)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id=$1`, session.ID)
		testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id=$1`, session.ID)
	})
	var sent struct {
		TaskID string `json:"task_id"`
	}
	chatPrivacyRequest(t, server, creatorToken, http.MethodPost, "/api/chat/sessions/"+session.ID+"/messages",
		map[string]any{"content": "read workspace documents"}, &sent)
	fx.Exec(t, `UPDATE agent_task_queue SET status='running', dispatched_at=now(), started_at=now() WHERE id=$1`, sent.TaskID)
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: otherWS, Payload: map[string]string{}})
	readChatPrivacyFrames(t, other, protocol.EventIssueUpdated)

	// Use the actual removal API. The shared agent/runtime belongs to the
	// workspace owner, so this member's removal does not cancel the task.
	chatPrivacyRequest(t, server, testToken, http.MethodDelete,
		"/api/workspaces/"+testWorkspaceID+"/members/"+memberID, nil, nil)
	if fx.Count(t, `SELECT count(*) FROM member WHERE id=$1`, memberID) != 0 {
		t.Fatal("membership was not removed")
	}
	if fx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id=$1 AND status='running'`, sent.TaskID) != 1 {
		t.Fatal("shared-agent task unexpectedly stopped")
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/tasks/"+sent.TaskID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+creatorToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed member's REST read: got %d, want 403 or 404", resp.StatusCode)
	}

	const secret = "workspace-secret-after-membership-was-revoked"
	report := testutil.JSONRequest(http.MethodPost, "/api/daemon/tasks/"+sent.TaskID+"/messages",
		map[string]any{"messages": []any{map[string]any{"seq": 1, "type": "text", "content": secret}}})
	report = testutil.WithURLParams(report, "taskId", sent.TaskID)
	report.Header.Set("X-User-ID", testUserID)
	testutil.Call(t, h.ReportTaskMessages, report).Want(http.StatusOK)
	result, _ := json.Marshal(map[string]string{"output": secret})
	if _, err := h.TaskService.CompleteTask(ctx, util.MustParseUUID(sent.TaskID), result, "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: protocol.EventIssueUpdated, WorkspaceID: otherWS, Payload: map[string]string{}})
	for _, frame := range readChatPrivacyFrames(t, other, protocol.EventIssueUpdated) {
		if strings.Contains(frame.raw, secret) {
			t.Errorf("removed member received old-workspace content via other-workspace socket: %s", frame.raw)
		}
	}
}
