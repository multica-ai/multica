package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type chatMembershipFunc func(context.Context, string, string) (bool, error)

func (f chatMembershipFunc) IsMember(ctx context.Context, userID, workspaceID string) (bool, error) {
	return f(ctx, userID, workspaceID)
}

func chatAlertEvent() events.Event {
	return events.Event{
		Type: protocol.EventChatDone, WorkspaceID: "source", ChatSessionOwnerID: "owner", ChatSessionID: "session",
		Payload: protocol.ChatDonePayload{
			ChatSessionID: "session", MessageID: "message", Content: "\n" + strings.Repeat("回", 250),
			Notification: &protocol.ChatReplyNotification{WorkspaceID: "source", Title: "Private chat"},
		},
	}
}

func TestChatReplyAlertChecksSourceMembership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		member bool
		err    error
	}{
		{"member", true, nil}, {"removed", false, nil}, {"lookup failure", false, errors.New("unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)
			registerChatReplyNotifications(bus, fb, chatMembershipFunc(func(ctx context.Context, userID, workspaceID string) (bool, error) {
				if userID != "owner" || workspaceID != "source" {
					t.Fatalf("membership checked against %s/%s", workspaceID, userID)
				}
				if _, bounded := ctx.Deadline(); !bounded {
					t.Fatal("membership lookup must have a deadline")
				}
				return tc.member, tc.err
			}))
			bus.Publish(chatAlertEvent())
			if len(fb.scopeCalls) != 1 || fb.scopeCalls[0].scopeID != realtime.WorkspaceMemberScopeID("source", "owner") {
				t.Fatalf("full chat content changed scope: %+v", fb.scopeCalls)
			}
			if !tc.member || tc.err != nil {
				if len(fb.userCalls) != 0 {
					t.Fatal("alert delivered without source membership")
				}
				return
			}
			if len(fb.userCalls) != 1 || fb.userCalls[0].userID != "owner" || len(fb.userCalls[0].exclude) != 0 {
				t.Fatalf("alert must reach only the owner's connections in every workspace: %+v", fb.userCalls)
			}
			var frame struct {
				Type    string                                `json:"type"`
				Payload protocol.ChatReplyNotificationPayload `json:"payload"`
			}
			if err := json.Unmarshal(fb.userCalls[0].msg, &frame); err != nil {
				t.Fatal(err)
			}
			if frame.Type != protocol.EventChatReplyNotification || frame.Payload.WorkspaceID != "source" ||
				frame.Payload.ChatSessionID != "session" || frame.Payload.MessageID != "message" ||
				frame.Payload.Body != strings.Repeat("回", 200)+"…" {
				t.Fatalf("incorrect alert projection: %+v", frame)
			}
		})
	}
}

func TestChatReplyAlertInvalidContextIsDropped(t *testing.T) {
	for _, name := range []string{"owner", "workspace", "message", "context", "mismatch", "session"} {
		t.Run(name, func(t *testing.T) {
			e := chatAlertEvent()
			p := e.Payload.(protocol.ChatDonePayload)
			switch name {
			case "owner":
				e.ChatSessionOwnerID = ""
			case "workspace":
				e.WorkspaceID = ""
			case "message":
				p.MessageID = ""
			case "context":
				p.Notification = nil
			case "mismatch":
				p.Notification.WorkspaceID = "other"
			case "session":
				p.ChatSessionID = "other"
			}
			e.Payload = p
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerChatReplyNotifications(bus, fb, chatMembershipFunc(func(context.Context, string, string) (bool, error) {
				t.Fatal("invalid context reached membership lookup")
				return true, nil
			}))
			bus.Publish(e)
			if len(fb.userCalls) != 0 {
				t.Fatal("invalid alert delivered")
			}
		})
	}
}

func TestChatReplyAlertReachesOtherWorkspaceConnection(t *testing.T) {
	fx := workspaceFixture(t)
	source := fx.Workspace(t, "Chat source", "chat-alert-source")
	other := fx.Workspace(t, "Chat destination", "chat-alert-destination")
	fx.Member(t, source, testUserID, "owner")
	fx.Member(t, other, testUserID, "owner")
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)
	registerChatReplyNotifications(bus, hub, &membershipChecker{queries: db.New(testPool)})
	router, _ := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, _ := realtimeMembershipConn(t, server, testUserID, other)
	defer conn.Close()
	e := chatAlertEvent()
	e.WorkspaceID, e.ChatSessionOwnerID = source, testUserID
	p := e.Payload.(protocol.ChatDonePayload)
	p.Notification.WorkspaceID = source
	e.Payload = p
	bus.Publish(e)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Type    string                                `json:"type"`
		Payload protocol.ChatReplyNotificationPayload `json:"payload"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != protocol.EventChatReplyNotification || frame.Payload.WorkspaceID != source {
		t.Fatalf("other workspace must receive a bounded alert, not chat content: %s", raw)
	}
	// Leaving the source must stop alerts without disconnecting this user's
	// still-authorized connection to the other workspace.
	fx.Exec(t, "DELETE FROM member WHERE workspace_id = $1 AND user_id = $2", source, testUserID)
	bus.Publish(e)
	hub.SendToUser(testUserID, []byte(`{"type":"test:alert_barrier"}`))
	_, raw, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &frame); err != nil || frame.Type != "test:alert_barrier" {
		t.Fatalf("removed source member received another alert: %s (%v)", raw, err)
	}
}
