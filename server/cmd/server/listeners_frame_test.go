package main

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestRegisterListeners_FrameContainsActorType asserts that every WS frame
// produced by registerListeners includes the top-level "actor_type" field.
// This is a regression guard for the bug where agent operations appeared as
// human in the web UI because the broadcast frame lacked actor_type.
func TestRegisterListeners_FrameContainsActorType(t *testing.T) {
	cases := []struct {
		name      string
		event     events.Event
		checkUser bool // true = check SendToUser, false = check BroadcastToWorkspace
		userID    string
	}{
		{
			name: "workspace broadcast carries actor_type",
			event: events.Event{
				Type:        protocol.EventIssueCreated,
				WorkspaceID: "ws-1",
				ActorID:     "agent-abc",
				ActorType:   "agent",
				Payload:     map[string]any{"id": "issue-1"},
			},
		},
		{
			name: "personal event (inbox:new) carries actor_type",
			event: events.Event{
				Type:        protocol.EventInboxNew,
				WorkspaceID: "ws-1",
				ActorID:     "member-xyz",
				ActorType:   "member",
				Payload: map[string]any{
					"item": map[string]any{"recipient_id": "user-1"},
				},
			},
			checkUser: true,
			userID:    "user-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(tc.event)

			var raw []byte
			if tc.checkUser {
				if len(fb.userCalls) == 0 {
					t.Fatal("expected SendToUser call, got none")
				}
				raw = fb.userCalls[0].msg
			} else {
				if len(fb.workspaceCalls) == 0 {
					t.Fatal("expected BroadcastToWorkspace call, got none")
				}
				raw = fb.workspaceCalls[0].msg
			}

			var frame map[string]any
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatalf("failed to unmarshal frame: %v", err)
			}

			actorType, ok := frame["actor_type"]
			if !ok {
				t.Fatal("frame missing actor_type field")
			}
			if actorType != tc.event.ActorType {
				t.Fatalf("actor_type = %q, want %q", actorType, tc.event.ActorType)
			}
		})
	}
}
