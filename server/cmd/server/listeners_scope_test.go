package main

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
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
		ActorType: "member", ActorID: "creator-1", ChatSessionID: "chat-1", ChatSessionOwnerID: "creator-1",
		Payload: protocol.ChatSessionCreatedPayload{
			WorkspaceID: "ws-1", ChatSessionID: "chat-1", CreatorID: "creator-1",
			Title: "private opening title",
		},
	})

	msg := onlyOwnerWorkspaceFrame(t, fb, "ws-1", "creator-1")
	var frame struct {
		Payload protocol.ChatSessionCreatedPayload `json:"payload"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
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
		ActorType: "member", ActorID: "creator-1", ChatSessionID: "chat-1", ChatSessionOwnerID: "creator-1",
		Payload: protocol.ChatSessionUpdatedPayload{
			ChatSessionID: "chat-1", Title: "private derived title",
		},
	})

	onlyOwnerWorkspaceFrame(t, fb, "ws-1", "creator-1")
}

// onlyOwnerWorkspaceFrame asserts that the one delivery went to ownerID's
// connections bound to workspaceID, and returns its frame. The user scope
// would also reach the owner's connections to other workspaces, which are not
// re-authorized against workspaceID.
func onlyOwnerWorkspaceFrame(t *testing.T, fb *fakeBroadcaster, workspaceID, ownerID string) []byte {
	t.Helper()
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.workspaceCalls) != 0 || len(fb.userCalls) != 0 || fb.broadcastCalled != 0 {
		t.Fatalf("private content left the owner's workspace connections: workspace=%+v user=%+v broadcast=%d",
			fb.workspaceCalls, fb.userCalls, fb.broadcastCalled)
	}
	want := realtime.WorkspaceMemberScopeID(workspaceID, ownerID)
	if len(fb.scopeCalls) != 1 || fb.scopeCalls[0].scopeType != realtime.ScopeWorkspaceMember || fb.scopeCalls[0].scopeID != want {
		t.Fatalf("scope fanout = %+v, want %s/%s once", fb.scopeCalls, realtime.ScopeWorkspaceMember, want)
	}
	return fb.scopeCalls[0].msg
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

// TestRegisterListeners_TaskEventsGoToWorkspace pins the must-fix #1 contract
// from the PR #1429 review: until the WS client supports scope-subscribe and
// reconnect-replay, task events MUST keep going through workspace fanout.
// Routing them via BroadcastToScope("task"|"chat", ...) with no client-side
// subscriber would silently drop them. Chat-task lifecycle events stay here
// too: every member's agent presence depends on them.
func TestRegisterListeners_TaskEventsGoToWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		taskID    string
		chatID    string
	}{
		{"issue task:message", protocol.EventTaskMessage, "task-1", ""},
		{"task:progress", protocol.EventTaskProgress, "task-2", ""},
		{"chat task:queued", protocol.EventTaskQueued, "task-3", "chat-1"},
		{"chat task:dispatch", protocol.EventTaskDispatch, "task-3", ""},
		{"chat task:completed", protocol.EventTaskCompleted, "task-3", "chat-1"},
		{"chat task:failed", protocol.EventTaskFailed, "task-3", "chat-1"},
		{"chat task:cancelled", protocol.EventTaskCancelled, "task-3", "chat-1"},
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

// chatPrivateCases is every event carrying chat session content: all chat:*
// events, plus the transcript stream of a chat task.
var chatPrivateCases = []struct {
	eventType string
	taskID    string
}{
	{protocol.EventChatMessage, ""},
	{protocol.EventChatDone, "task-1"},
	{protocol.EventChatQuickActions, "task-1"},
	{protocol.EventChatCancelFinalized, "task-1"},
	{protocol.EventChatSessionCreated, ""},
	{protocol.EventChatSessionUpdated, ""},
	{protocol.EventChatSessionRead, ""},
	{protocol.EventChatSessionDeleted, ""},
	{protocol.EventTaskMessage, "task-1"},
}

func TestRegisterListeners_ChatContentGoesOnlyToSessionOwner(t *testing.T) {
	for _, tc := range chatPrivateCases {
		t.Run(tc.eventType, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(events.Event{
				Type: tc.eventType, WorkspaceID: "ws-1", ActorType: "system",
				TaskID: tc.taskID, ChatSessionID: "chat-1", ChatSessionOwnerID: "owner-1",
				Payload: map[string]any{"content": "private chat content"},
			})

			msg := onlyOwnerWorkspaceFrame(t, fb, "ws-1", "owner-1")
			var frame struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(msg, &frame); err != nil {
				t.Fatalf("decode owner frame: %v", err)
			}
			if frame.Type != tc.eventType || frame.Payload["content"] != "private chat content" {
				t.Fatalf("owner frame = %+v", frame)
			}
		})
	}
}

// A producer that cannot name the owner and workspace must not fall back to a
// wider fanout: the owner refetches on the next lifecycle event, a leak is
// permanent.
func TestRegisterListeners_ChatContentWithoutOwnerIsDropped(t *testing.T) {
	for _, tc := range chatPrivateCases {
		for _, missing := range []struct{ name, workspaceID, ownerID string }{
			{"no owner", "ws-1", ""},
			{"no workspace", "", "owner-1"},
		} {
			t.Run(tc.eventType+"/"+missing.name, func(t *testing.T) {
				bus := events.New()
				fb := &fakeBroadcaster{}
				registerListeners(bus, fb)

				bus.Publish(events.Event{
					Type: tc.eventType, WorkspaceID: missing.workspaceID, ActorType: "system",
					TaskID: tc.taskID, ChatSessionID: "chat-1", ChatSessionOwnerID: missing.ownerID,
					Payload: map[string]any{"content": "private chat content"},
				})

				if len(fb.workspaceCalls) != 0 || len(fb.userCalls) != 0 || len(fb.scopeCalls) != 0 || fb.broadcastCalled != 0 {
					t.Fatalf("unroutable chat event was delivered: workspace=%+v user=%+v scope=%+v broadcast=%d",
						fb.workspaceCalls, fb.userCalls, fb.scopeCalls, fb.broadcastCalled)
				}
			})
		}
	}
}

// IM outbounds (Slack, Lark, ...) subscribe to chat:done and task:message on
// the bus in-process. Narrowing the WebSocket fanout must not narrow them.
func TestRegisterListeners_ChatRoutingKeepsInProcessListeners(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	var got []events.Event
	for _, eventType := range []string{protocol.EventChatDone, protocol.EventTaskMessage} {
		bus.Subscribe(eventType, func(e events.Event) { got = append(got, e) })
	}
	registerListeners(bus, fb)

	done := protocol.ChatDonePayload{ChatSessionID: "chat-1", TaskID: "task-1", Content: "assistant reply"}
	message := protocol.TaskMessagePayload{TaskID: "task-1", Seq: 1, Type: "text", Content: "streamed text"}
	for _, e := range []events.Event{
		{Type: protocol.EventChatDone, WorkspaceID: "ws-1", ChatSessionID: "chat-1", ChatSessionOwnerID: "owner-1", Payload: done},
		{Type: protocol.EventTaskMessage, WorkspaceID: "ws-1", TaskID: "task-1", ChatSessionID: "chat-1", ChatSessionOwnerID: "owner-1", Payload: message},
		// Ownerless events are dropped from the WebSocket only.
		{Type: protocol.EventChatDone, WorkspaceID: "ws-1", ChatSessionID: "chat-1", Payload: done},
	} {
		bus.Publish(e)
	}

	if len(got) != 3 {
		t.Fatalf("in-process listener saw %d events, want 3", len(got))
	}
	if p, ok := got[0].Payload.(protocol.ChatDonePayload); !ok || p.Content != "assistant reply" {
		t.Fatalf("chat:done payload = %#v, want the full reply", got[0].Payload)
	}
	if p, ok := got[1].Payload.(protocol.TaskMessagePayload); !ok || p.Content != "streamed text" {
		t.Fatalf("task:message payload = %#v, want the full message", got[1].Payload)
	}
	if p, ok := got[2].Payload.(protocol.ChatDonePayload); !ok || p.Content != "assistant reply" {
		t.Fatalf("ownerless chat:done payload = %#v, want the full reply", got[2].Payload)
	}
}

func TestRegisterListeners_MemberPrivateEventsGoOnlyToThatMember(t *testing.T) {
	for _, eventType := range []string{protocol.EventIssueViewPreferenceChanged, protocol.EventIssueViewChanged} {
		t.Run(eventType, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(events.Event{
				Type: eventType, WorkspaceID: "ws-1", ActorType: "member", ActorID: "owner-1",
				MemberPrivateUserID: "owner-1",
				Payload:             map[string]any{"view_id": "view-1"},
			})

			msg := onlyOwnerWorkspaceFrame(t, fb, "ws-1", "owner-1")
			var frame struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(msg, &frame); err != nil || frame.Type != eventType {
				t.Fatalf("member frame type = %q (%v), want %q", frame.Type, err, eventType)
			}
		})
	}
}

// Preferences have no audience but their owner: without one, nobody hears it.
func TestRegisterListeners_PreferenceEventWithoutMemberIsDropped(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventIssueViewPreferenceChanged, WorkspaceID: "ws-1", ActorType: "member", ActorID: "owner-1",
		Payload: map[string]any{"scope_type": "sidebar_projects"},
	})

	if len(fb.workspaceCalls) != 0 || len(fb.userCalls) != 0 || len(fb.scopeCalls) != 0 || fb.broadcastCalled != 0 {
		t.Fatalf("preference event without a member was delivered: workspace=%+v user=%+v scope=%+v broadcast=%d",
			fb.workspaceCalls, fb.userCalls, fb.scopeCalls, fb.broadcastCalled)
	}
}

// A shared view's change is news to the whole workspace.
func TestRegisterListeners_SharedIssueViewChangeGoesToWorkspace(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	bus.Publish(events.Event{
		Type: protocol.EventIssueViewChanged, WorkspaceID: "ws-1", ActorType: "member", ActorID: "owner-1",
		Payload: map[string]any{"view_id": "view-1", "action": "updated"},
	})

	if len(fb.scopeCalls) != 0 || len(fb.workspaceCalls) != 1 || fb.workspaceCalls[0].workspaceID != "ws-1" {
		t.Fatalf("shared view change fanout: scope=%+v workspace=%+v, want ws-1 once", fb.scopeCalls, fb.workspaceCalls)
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
