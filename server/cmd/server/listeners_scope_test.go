package main

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
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

func TestRegisterListeners_ChatSessionCreatedGoesOnlyToCreator(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventChatSessionCreated, WorkspaceID: "ws-1",
		ActorType: "member", ActorID: "creator-1", ChatSessionID: "chat-1",
		Payload: protocol.ChatSessionCreatedPayload{
			WorkspaceID: "ws-1", ChatSessionID: "chat-1", CreatorID: "creator-1",
			Title: "private opening title",
		},
	})

	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("private Chat create reached workspace fanout: %+v", fb.workspaceCalls)
	}
	if len(fb.userCalls) != 1 || fb.userCalls[0].userID != "creator-1" {
		t.Fatalf("creator fanout = %+v, want creator-1 once", fb.userCalls)
	}
	var frame struct {
		Payload protocol.ChatSessionCreatedPayload `json:"payload"`
	}
	if err := json.Unmarshal(fb.userCalls[0].msg, &frame); err != nil {
		t.Fatalf("decode creator frame: %v", err)
	}
	if frame.Payload.WorkspaceID != "ws-1" || frame.Payload.ChatSessionID != "chat-1" {
		t.Fatalf("creator payload = %+v", frame.Payload)
	}
}

func TestRegisterListeners_ChatSessionTitleUpdateGoesOnlyToCreator(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventChatSessionUpdated, WorkspaceID: "ws-1",
		ActorType: "member", ActorID: "creator-1", ChatSessionID: "chat-1",
		Payload: protocol.ChatSessionUpdatedPayload{
			ChatSessionID: "chat-1", Title: "private derived title",
		},
	})

	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("private Chat title reached workspace fanout: %+v", fb.workspaceCalls)
	}
	if len(fb.userCalls) != 1 || fb.userCalls[0].userID != "creator-1" {
		t.Fatalf("creator fanout = %+v, want creator-1 once", fb.userCalls)
	}
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

// An invitation conclusion must reach the invitee BOTH ways: the workspace
// broadcast keeps admin pending lists fresh, and a targeted send reaches the
// invitee's own clients — which are usually bound to a different workspace
// room (or no room at all, #8432), so the broadcast alone never arrives.
// The targeted frame passes excludeWorkspace so clients already in the room
// don't get the event twice.
func TestRegisterListeners_InvitationConcludedReachesActorAndWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
	}{
		{"invitation:accepted", protocol.EventInvitationAccepted},
		{"invitation:declined", protocol.EventInvitationDeclined},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(events.Event{
				Type: tc.eventType, WorkspaceID: "ws-1",
				ActorType: "member", ActorID: "invitee-1",
				Payload: map[string]any{"invitation_id": "inv-1"},
			})

			if len(fb.workspaceCalls) != 1 || fb.workspaceCalls[0].workspaceID != "ws-1" {
				t.Fatalf("workspace fanout = %+v, want exactly ws-1 (admin lists depend on it)", fb.workspaceCalls)
			}
			if len(fb.userCalls) != 1 {
				t.Fatalf("targeted sends = %+v, want exactly one", fb.userCalls)
			}
			send := fb.userCalls[0]
			if send.userID != "invitee-1" {
				t.Fatalf("targeted recipient = %q, want the acting invitee invitee-1", send.userID)
			}
			if len(send.exclude) != 1 || send.exclude[0] != "ws-1" {
				t.Fatalf("excludeWorkspace = %v, want [ws-1] to dedupe the room broadcast", send.exclude)
			}
			var frame struct {
				Type    string `json:"type"`
				ActorID string `json:"actor_id"`
				Payload struct {
					InvitationID string `json:"invitation_id"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(send.msg, &frame); err != nil {
				t.Fatalf("decode targeted frame: %v", err)
			}
			if frame.Type != tc.eventType || frame.ActorID != "invitee-1" || frame.Payload.InvitationID != "inv-1" {
				t.Fatalf("targeted frame = type %q actor %q payload %+v", frame.Type, frame.ActorID, frame.Payload)
			}
		})
	}
}

// Without an acting invitee there is nobody to target; the workspace
// broadcast must still go out.
func TestRegisterListeners_InvitationConcludedWithoutActorSkipsTargetedSend(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventInvitationDeclined, WorkspaceID: "ws-1",
		ActorType: "member",
		Payload:   map[string]any{"invitation_id": "inv-1"},
	})

	if len(fb.workspaceCalls) != 1 {
		t.Fatalf("workspace fanout = %+v, want exactly one", fb.workspaceCalls)
	}
	if len(fb.userCalls) != 0 {
		t.Fatalf("targeted sends = %+v, want none without an actor", fb.userCalls)
	}
}

// invitation:revoked keeps its invitee_user_id routing: its actor is the
// revoking admin, so targeting the actor would refresh the wrong user's list.
func TestRegisterListeners_InvitationRevokedStillTargetsInviteeNotActor(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventInvitationRevoked, WorkspaceID: "ws-1",
		ActorType: "member", ActorID: "admin-1",
		Payload: map[string]any{"invitee_user_id": inviteePtr("invitee-2")},
	})

	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("revoked reached workspace fanout: %+v (still a personal event)", fb.workspaceCalls)
	}
	if len(fb.userCalls) != 1 || fb.userCalls[0].userID != "invitee-2" {
		t.Fatalf("revoked fanout = %+v, want invitee-2 only", fb.userCalls)
	}
}

func inviteePtr(s string) *string { return &s }
