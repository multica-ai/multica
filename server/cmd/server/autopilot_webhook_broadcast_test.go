package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestAutopilotWebhookTokenIsNotBroadcastToTheWorkspace is the end-to-end proof
// for the redaction unit-tested in internal/handler: through the real router,
// the real event bus and a real WebSocket client, a member who is refused the
// webhook token by GetAutopilot must not receive it from the fanout either.
//
// Holding that token fires the autopilot through the public ingress route, so a
// leak here is a permission bypass that no write gate can see. The leak predates
// MUL-7108 and is closed with it, since it is the same bypass.
func TestAutopilotWebhookTokenIsNotBroadcastToTheWorkspace(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)

	email := fmt.Sprintf("autopilot-broadcast-reader-%d@multica.test", time.Now().UnixNano())
	reader := fx.User(t, "Autopilot Broadcast Reader", email)
	fx.Member(t, testWorkspaceID, reader, "member")
	readerToken, err := generateTestJWT(reader, email, "Autopilot Broadcast Reader")
	if err != nil {
		t.Fatalf("reader jwt: %v", err)
	}

	var agentID string
	fx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)
	autopilotID := fx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "webhook broadcast redaction",
		"assignee_id": agentID, "assignee_type": "agent", "execution_mode": "run_only",
		"status": "active", "created_by_type": "member", "created_by_id": testUserID,
	})
	triggerID := fx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": autopilotID, "kind": "webhook", "enabled": true,
		"provider": "generic", "webhook_token": fmt.Sprintf("tok_%d", time.Now().UnixNano()),
		"created_by_type": "member", "created_by_id": testUserID,
	})

	// The premise: this member is refused the secret on the read path.
	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, nil)
	if err != nil {
		t.Fatalf("build detail request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+readerToken)
	detailResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read detail: %v", err)
	}
	if detailResp.StatusCode != http.StatusOK {
		detailResp.Body.Close()
		t.Fatalf("detail status = %d, want 200", detailResp.StatusCode)
	}
	var detail struct {
		Autopilot handler.AutopilotResponse          `json:"autopilot"`
		Triggers  []handler.AutopilotTriggerResponse `json:"triggers"`
	}
	readJSON(t, detailResp, &detail)
	if detail.Autopilot.CanWrite == nil || *detail.Autopilot.CanWrite {
		t.Fatalf("fixture is wrong: the reader must be a non-writer, got can_write=%v", detail.Autopilot.CanWrite)
	}
	if len(detail.Triggers) != 1 || detail.Triggers[0].WebhookToken != nil {
		t.Fatal("fixture is wrong: the read path must already redact the token for this member")
	}

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?workspace_id=" + testWorkspaceID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "auth", "payload": map[string]string{"token": readerToken}}); err != nil {
		t.Fatalf("websocket auth: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ack, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(ack), "auth_ack") {
		t.Fatalf("websocket auth_ack: %s %v", ack, err)
	}
	// Same registration barrier the other WebSocket integration test uses: the
	// Hub adds the client to the room on its own goroutine.
	time.Sleep(100 * time.Millisecond)

	rotateResp := authRequest(t, "POST", "/api/autopilots/"+autopilotID+"/triggers/"+triggerID+"/rotate-webhook-token?workspace_id="+testWorkspaceID, nil)
	if rotateResp.StatusCode != http.StatusOK {
		rotateResp.Body.Close()
		t.Fatalf("rotate status = %d, want 200", rotateResp.StatusCode)
	}
	var rotated handler.AutopilotTriggerResponse
	readJSON(t, rotateResp, &rotated)
	// The writer's own response must still carry the live token — otherwise the
	// assertion below would pass on an endpoint that stopped issuing one.
	if rotated.WebhookToken == nil || *rotated.WebhookToken == "" {
		t.Fatal("rotate returned no token to the authorized caller")
	}

	var event struct {
		Type    string `json:"type"`
		Payload struct {
			AutopilotID string                           `json:"autopilot_id"`
			Trigger     handler.AutopilotTriggerResponse `json:"trigger"`
		} `json:"payload"`
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn.SetReadDeadline(deadline)
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no autopilot:updated event reached the workspace room: %v", err)
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("parse websocket frame: %v", err)
		}
		if event.Type == "autopilot:updated" && event.Payload.AutopilotID == autopilotID {
			break
		}
	}

	if event.Payload.Trigger.WebhookToken != nil || event.Payload.Trigger.WebhookPath != nil || event.Payload.Trigger.WebhookURL != nil {
		// Presence, not value: a failure message is not a place to print a
		// live credential.
		t.Fatalf("a non-writer received the webhook credential over the workspace websocket: token=%t path=%t url=%t",
			event.Payload.Trigger.WebhookToken != nil,
			event.Payload.Trigger.WebhookPath != nil,
			event.Payload.Trigger.WebhookURL != nil)
	}
	// The event must still arrive and identify the trigger: clients react by
	// refetching through the authenticated endpoint, which is what makes
	// carrying no secret cost nothing.
	if event.Payload.Trigger.ID != triggerID {
		t.Errorf("broadcast trigger id = %q, want %q — clients cannot tell what to refetch", event.Payload.Trigger.ID, triggerID)
	}
}
