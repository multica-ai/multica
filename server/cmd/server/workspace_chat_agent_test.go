package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspaceDefaultChatAgentThroughRouter(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	for _, tc := range []struct {
		role string
		want int
	}{{"owner", 200}, {"admin", 200}, {"member", 403}, {"outsider", 404}} {
		t.Run(tc.role, func(t *testing.T) {
			wsID := fx.Insert(t, "workspace", testutil.Cols{"name": "Chat defaults role", "slug": uuid.NewString()})
			scoped := testutil.New(testPool, wsID, testUserID)
			if tc.role != "outsider" {
				scoped.Insert(t, "member", testutil.Cols{"workspace_id": wsID, "user_id": testUserID, "role": tc.role})
			}
			agentID := scoped.Agent(t, "Default", scoped.Runtime(t, "Runtime"))
			resp := authRequest(t, "PATCH", "/api/workspaces/"+wsID, map[string]any{"default_chat_agent_id": agentID, "name": "Changed"})
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("got %d want %d: %s", resp.StatusCode, tc.want, b)
			}
			var set bool
			scoped.QueryRow(t, `SELECT settings ? 'default_chat_agent_id' FROM workspace WHERE id=$1`, wsID).Scan(&set)
			if set != (tc.want == 200) {
				t.Fatal("role authorization failed to protect persistence")
			}
			if tc.want == 200 {
				// The header still points to the original fixture workspace. The URL
				// workspace must decide candidate scope, never the ambient header.
				foreign := fx.Agent(t, "Foreign", fx.Runtime(t, "Foreign runtime"))
				resp = authRequest(t, "PATCH", "/api/workspaces/"+wsID, map[string]any{"default_chat_agent_id": foreign})
				b, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 400 || bytes.Contains(b, []byte(foreign)) {
					t.Fatalf("cross-workspace default accepted/leaked: %d %s", resp.StatusCode, b)
				}
			}
		})
	}
}

func TestWorkspaceDefaultChatAgentPrivacyThroughHTTPAndWebSocket(t *testing.T) {
	ctx := context.Background()
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	wsID := fx.Insert(t, "workspace", testutil.Cols{"name": "Privacy defaults", "slug": uuid.NewString()})
	fx = testutil.New(testPool, wsID, testUserID)
	fx.Insert(t, "member", testutil.Cols{"workspace_id": wsID, "user_id": testUserID, "role": "owner"})
	agentID := fx.Agent(t, "Secret default", fx.Runtime(t, "Default runtime"))
	member := fx.Insert(t, "user", testutil.Cols{"name": "Viewer", "email": uuid.NewString() + "@example.test"})
	fx.Insert(t, "member", testutil.Cols{"workspace_id": wsID, "user_id": member, "role": "member"})
	token, err := generateTestJWT(member, "viewer@example.test", "Viewer")
	if err != nil {
		t.Fatal(err)
	}
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?workspace_id=" + wsID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteJSON(map[string]any{"type": "auth", "payload": map[string]string{"token": token}}); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ack, err := conn.ReadMessage()
	if err != nil || !bytes.Contains(ack, []byte("auth_ack")) {
		t.Fatalf("auth: %s %v", ack, err)
	}
	for _, restricted := range []bool{false, true} {
		t.Run(map[bool]string{false: "private", true: "member-restricted"}[restricted], func(t *testing.T) {
			if restricted {
				fx.Exec(t, `UPDATE agent SET permission_mode='public_to' WHERE id=$1`, agentID)
				fx.Insert(t, "agent_invocation_target", testutil.Cols{"agent_id": agentID, "target_type": "member", "target_id": testUserID})
			}
			resp := authRequest(t, "PATCH", "/api/workspaces/"+wsID, map[string]any{"default_chat_agent_id": agentID, "settings": map[string]any{"keep": true}})
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || !bytes.Contains(body, []byte(agentID)) {
				t.Fatalf("authorized update: %d %s", resp.StatusCode, body)
			}
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			for {
				_, frame, err := conn.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(frame, []byte(agentID)) {
					t.Fatalf("full websocket frame leaked default ID: %s", frame)
				}
				var event struct {
					Type string `json:"type"`
				}
				if err = json.Unmarshal(frame, &event); err != nil {
					t.Fatal(err)
				}
				if event.Type == "workspace:updated" {
					if !bytes.Contains(frame, []byte("keep")) {
						t.Fatal("broadcast lost settings")
					}
					break
				}
			}
			for _, path := range []string{"/api/workspaces", "/api/workspaces/" + wsID} {
				req, _ := http.NewRequestWithContext(ctx, "GET", testServer.URL+path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(response.Body)
				response.Body.Close()
				if response.StatusCode != 200 || bytes.Contains(body, []byte(agentID)) {
					t.Fatalf("full HTTP response leaked ID: %d %s", response.StatusCode, body)
				}
			}
		})
	}
}
