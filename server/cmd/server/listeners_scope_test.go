package main

import (
	"context"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeBroadcaster records every fanout call so tests can assert which scope a
// given event landed on.
type fakeBroadcaster struct {
	mu              sync.Mutex
	scopeCalls      []scopeCall
	workspaceCalls  []workspaceCall
	userCalls       []userCall
	broadcastCalled int
}

type scopeCall struct {
	scopeType, scopeID string
	msg                []byte
}
type workspaceCall struct {
	workspaceID string
	msg         []byte
}
type userCall struct {
	userID  string
	msg     []byte
	exclude []string
}

func (f *fakeBroadcaster) BroadcastToScope(scopeType, scopeID string, message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopeCalls = append(f.scopeCalls, scopeCall{scopeType, scopeID, message})
}
func (f *fakeBroadcaster) BroadcastToWorkspace(workspaceID string, message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaceCalls = append(f.workspaceCalls, workspaceCall{workspaceID, message})
}
func (f *fakeBroadcaster) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userCalls = append(f.userCalls, userCall{userID, message, excludeWorkspace})
}
func (f *fakeBroadcaster) Broadcast(message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcastCalled++
}

// TestRegisterListeners_TaskChatGoToWorkspace pins the must-fix #1 contract
// from the PR #1429 review: until the WS client supports scope-subscribe and
// reconnect-replay, high-frequency task/chat events MUST keep going through
// workspace fanout. Routing them via BroadcastToScope("task"|"chat", ...)
// with no client-side subscriber would silently drop every chat / task
// message and break the live timeline + chat unread badges.
func TestRegisterListeners_TaskChatGoToWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		taskID    string
		chatID    string
	}{
		{"task:message with TaskID", protocol.EventTaskMessage, "task-1", ""},
		{"task:progress with TaskID", protocol.EventTaskProgress, "task-2", ""},
		{"chat:message with ChatSessionID", protocol.EventChatMessage, "", "chat-1"},
		{"chat:done with ChatSessionID", protocol.EventChatDone, "", "chat-2"},
		{"chat:session_read with ChatSessionID", protocol.EventChatSessionRead, "", "chat-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(events.Event{
				Type:          tc.eventType,
				WorkspaceID:   "ws-1",
				TaskID:        tc.taskID,
				ChatSessionID: tc.chatID,
				Payload:       map[string]any{"hello": "world"},
			})

			if len(fb.scopeCalls) != 0 {
				t.Fatalf("expected no BroadcastToScope calls (must-fix #1: keep workspace fanout until client lands), got %+v", fb.scopeCalls)
			}
			if len(fb.workspaceCalls) != 1 {
				t.Fatalf("expected exactly 1 BroadcastToWorkspace call, got %d", len(fb.workspaceCalls))
			}
			if fb.workspaceCalls[0].workspaceID != "ws-1" {
				t.Fatalf("expected workspace ws-1, got %q", fb.workspaceCalls[0].workspaceID)
			}
		})
	}
}

func TestRegisterListeners_PrivateSpaceUsesMemberAudience(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var spaceID, spaceMemberID, outsiderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace_space (workspace_id, name, key, visibility, created_by)
		VALUES ($1, 'Realtime Private', 'RTPRIV', 'private', $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&spaceID); err != nil {
		t.Fatalf("create private space: %v", err)
	}
	for email, target := range map[string]*string{
		"realtime-private-member@multica.test":   &spaceMemberID,
		"realtime-private-outsider@multica.test": &outsiderID,
	} {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO "user" (name, email) VALUES ($1, $1) RETURNING id
		`, email).Scan(target); err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
		`, testWorkspaceID, *target); err != nil {
			t.Fatalf("add workspace member %s: %v", email, err)
		}
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role, sort_order)
		VALUES ($1, $2, $3, 'member', 1)
	`, testWorkspaceID, spaceID, spaceMemberID); err != nil {
		t.Fatalf("add private space member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace_space WHERE id = $1`, spaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id IN ($2, $3)`, testWorkspaceID, spaceMemberID, outsiderID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id IN ($1, $2)`, spaceMemberID, outsiderID)
	})

	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, db.New(testPool))
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     spaceMemberID,
		Payload:     map[string]any{"space_id": spaceID},
	})

	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("private event must not use workspace broadcast: %+v", fb.workspaceCalls)
	}
	delivered := make(map[string]bool, len(fb.userCalls))
	for _, call := range fb.userCalls {
		delivered[call.userID] = true
	}
	if !delivered[testUserID] || !delivered[spaceMemberID] {
		t.Fatalf("private audience missing admin/member: %+v", delivered)
	}
	if delivered[outsiderID] {
		t.Fatalf("private event leaked to non-member: %+v", delivered)
	}
}
