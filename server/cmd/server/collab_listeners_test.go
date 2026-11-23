package main

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeRevalidator chan collab.RevalidateRequest

func (f fakeRevalidator) Revalidate(_ context.Context, req collab.RevalidateRequest) error {
	f <- req
	return nil
}

func TestCollabListenersRevalidateOnRevocation(t *testing.T) {
	bus := events.New()
	got := make(fakeRevalidator, 8)
	registerCollabListeners(bus, got)
	next := func() collab.RevalidateRequest {
		t.Helper()
		select {
		case req := <-got:
			return req
		case <-time.After(2 * time.Second):
			t.Fatal("no revalidation")
			return collab.RevalidateRequest{}
		}
	}
	none := func() {
		t.Helper()
		select {
		case req := <-got:
			t.Fatalf("unexpected revalidation: %+v", req)
		case <-time.After(50 * time.Millisecond):
		}
	}
	publish := func(eventType string, payload any) {
		bus.Publish(events.Event{Type: eventType, WorkspaceID: "ws-1", ActorType: "member", ActorID: "admin", Payload: payload})
	}

	publish(protocol.EventMemberRemoved, map[string]any{"user_id": "u-1", "workspace_id": "ws-1"})
	if req := next(); req.WorkspaceID != "ws-1" || req.Actor == nil || req.Actor.Type != "member" || req.Actor.ID != "u-1" {
		t.Fatalf("member removed: %+v", req)
	}

	publish(protocol.EventMemberUpdated, map[string]any{"member": handler.MemberWithUserResponse{UserID: "u-2", Role: "guest"}})
	if req := next(); req.Actor == nil || req.Actor.ID != "u-2" {
		t.Fatalf("member updated: %+v", req)
	}

	publish(protocol.EventProjectMembersChanged, map[string]any{"project_id": "p-1"})
	if req := next(); req.WorkspaceID != "ws-1" || req.Type != "issue" || req.Actor != nil {
		t.Fatalf("project members changed: %+v", req)
	}

	publish(protocol.EventIssueUpdated, map[string]any{"issue": handler.IssueResponse{ID: "i-1"}, "project_changed": false})
	none()
	publish(protocol.EventIssueUpdated, map[string]any{"issue": handler.IssueResponse{ID: "i-1"}, "project_changed": true})
	if req := next(); req.Document != collab.IssueDocumentName("i-1") {
		t.Fatalf("issue moved: %+v", req)
	}
}
