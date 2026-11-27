package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type stubAudience struct {
	recipients []string
	private    bool
}

func (s stubAudience) Audience(context.Context, events.Event) ([]string, bool, error) {
	return s.recipients, s.private, nil
}
func (stubAudience) IssueRefReaders(context.Context, events.Event) (map[string]map[string]bool, error) {
	return nil, nil
}
func (stubAudience) Members(context.Context, string) ([]string, error) { return nil, nil }

const toolDetailsWS = "ws-1"

func restrictedToolEvent(chatOwner string) events.Event {
	e := events.Event{
		Type:        protocol.EventTaskMessage,
		WorkspaceID: toolDetailsWS,
		ActorType:   "system",
		TaskID:      "task-1",
		Payload: protocol.TaskMessagePayload{
			TaskID: "task-1", Seq: 2, Type: "tool_use", Tool: "Bash",
			Input: map[string]any{"command": "cat ~/.ssh/config"},
		},
		ToolDetailsRestricted: true,
		ToolDetailReaders:     map[string]bool{"owner": true, "admin": true},
	}
	if chatOwner != "" {
		e.ChatSessionID = "chat-1"
		e.ChatSessionOwnerID = chatOwner
	}
	return e
}

// frameDetails reads what a frame says about the tool call it carries.
func frameDetails(t *testing.T, frame []byte) (hidden bool, input map[string]any) {
	t.Helper()
	var msg struct {
		Payload protocol.TaskMessagePayload `json:"payload"`
	}
	if err := json.Unmarshal(frame, &msg); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	if msg.Payload.Tool != "Bash" || msg.Payload.Seq != 2 {
		t.Fatalf("frame lost the tool call itself: %+v", msg.Payload)
	}
	return msg.Payload.DetailsHidden, msg.Payload.Input
}

func memberFrames(fb *fakeBroadcaster) map[string][]byte {
	out := map[string][]byte{}
	for _, c := range fb.scopeCalls {
		if c.scopeType == realtime.ScopeWorkspaceMember {
			out[c.scopeID] = c.msg
		}
	}
	return out
}

func wantFull(t *testing.T, who string, frame []byte) {
	t.Helper()
	if frame == nil {
		t.Fatalf("%s got no frame", who)
	}
	if hidden, input := frameDetails(t, frame); hidden || input["command"] == nil {
		t.Errorf("%s may read the tool call but got hidden=%v input=%v", who, hidden, input)
	}
}

func wantStripped(t *testing.T, who string, frame []byte) {
	t.Helper()
	if frame == nil {
		t.Fatalf("%s got no frame", who)
	}
	if hidden, input := frameDetails(t, frame); !hidden || input != nil {
		t.Errorf("%s may not read the tool call but got hidden=%v input=%v", who, hidden, input)
	}
}

func TestRestrictedToolCallReachesTheWorkspaceWithoutItsDetails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		audience []privateProjectAudience
	}{
		{name: "no project audience"},
		{name: "a run outside private projects", audience: []privateProjectAudience{stubAudience{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb, tc.audience...)

			bus.Publish(restrictedToolEvent(""))

			if len(fb.workspaceCalls) != 1 {
				t.Fatalf("workspace frames = %d, want 1", len(fb.workspaceCalls))
			}
			wantStripped(t, "the workspace", fb.workspaceCalls[0].msg)
			frames := memberFrames(fb)
			if len(frames) != 2 {
				t.Fatalf("member frames = %d, want one per reader", len(frames))
			}
			wantFull(t, "the agent owner", frames[realtime.WorkspaceMemberScopeID(toolDetailsWS, "owner")])
			wantFull(t, "an admin", frames[realtime.WorkspaceMemberScopeID(toolDetailsWS, "admin")])
		})
	}
}

// sendOrder records which kind of frame went out in what order.
type sendOrder struct {
	*fakeBroadcaster
	sent []string
}

func (s *sendOrder) BroadcastToScope(scopeType, scopeID string, message []byte) {
	s.sent = append(s.sent, scopeType)
	s.fakeBroadcaster.BroadcastToScope(scopeType, scopeID, message)
}

func (s *sendOrder) BroadcastToWorkspace(workspaceID string, message []byte) {
	s.sent = append(s.sent, "workspace")
	s.fakeBroadcaster.BroadcastToWorkspace(workspaceID, message)
}

// Clients built before details_hidden keep the first copy of a seq, so a
// reader must get the full call before the stripped one.
func TestRestrictedToolCallReachesReadersBeforeTheWorkspace(t *testing.T) {
	bus := events.New()
	b := &sendOrder{fakeBroadcaster: &fakeBroadcaster{}}
	registerListeners(bus, b)

	bus.Publish(restrictedToolEvent(""))

	want := []string{realtime.ScopeWorkspaceMember, realtime.ScopeWorkspaceMember, "workspace"}
	if len(b.sent) != len(want) {
		t.Fatalf("sent %v, want %v", b.sent, want)
	}
	for i := range want {
		if b.sent[i] != want[i] {
			t.Fatalf("sent %v, want %v", b.sent, want)
		}
	}
}

func TestRestrictedToolCallInAPrivateProjectGoesToEachMemberOnce(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb, stubAudience{recipients: []string{"owner", "member"}, private: true})

	bus.Publish(restrictedToolEvent(""))

	if len(fb.workspaceCalls) != 0 {
		t.Fatalf("a private project's run reached the workspace %d time(s)", len(fb.workspaceCalls))
	}
	frames := memberFrames(fb)
	if len(fb.scopeCalls) != 2 {
		t.Fatalf("member frames = %d, want 2 (admin is not in the project)", len(fb.scopeCalls))
	}
	wantFull(t, "the agent owner", frames[realtime.WorkspaceMemberScopeID(toolDetailsWS, "owner")])
	wantStripped(t, "a project member", frames[realtime.WorkspaceMemberScopeID(toolDetailsWS, "member")])
}

func TestRestrictedToolCallInAChatGoesToItsCreatorAsTheyMayReadIt(t *testing.T) {
	for _, tc := range []struct {
		creator string
		full    bool
	}{{"member", false}, {"admin", true}} {
		t.Run(tc.creator, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb, stubAudience{})

			bus.Publish(restrictedToolEvent(tc.creator))

			if len(fb.workspaceCalls) != 0 || len(fb.scopeCalls) != 1 {
				t.Fatalf("chat frame went to workspace=%d members=%d, want only its creator",
					len(fb.workspaceCalls), len(fb.scopeCalls))
			}
			frame := memberFrames(fb)[realtime.WorkspaceMemberScopeID(toolDetailsWS, tc.creator)]
			if tc.full {
				wantFull(t, "the chat creator", frame)
			} else {
				wantStripped(t, "the chat creator", frame)
			}
		})
	}
}

func TestRestrictedAgentsProseTakesTheOrdinaryRoute(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)

	e := restrictedToolEvent("")
	e.Payload = protocol.TaskMessagePayload{TaskID: "task-1", Seq: 3, Type: "thinking", Content: "reading the config"}
	bus.Publish(e)

	if len(fb.workspaceCalls) != 1 || len(fb.scopeCalls) != 0 {
		t.Fatalf("workspace=%d members=%d, want one workspace frame", len(fb.workspaceCalls), len(fb.scopeCalls))
	}
	var msg struct {
		Payload protocol.TaskMessagePayload `json:"payload"`
	}
	if err := json.Unmarshal(fb.workspaceCalls[0].msg, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Payload.Content != "reading the config" || msg.Payload.DetailsHidden {
		t.Errorf("reasoning was changed: %+v", msg.Payload)
	}
}
