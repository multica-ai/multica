package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const collabRevalidateTimeout = 5 * time.Second

// collabRevalidator is the part of *collab.Client the listeners use.
type collabRevalidator interface {
	Revalidate(ctx context.Context, req collab.RevalidateRequest) error
}

// registerCollabListeners makes live editors prove their access again when an
// event may have revoked it. A ticket only checks access when an editor
// joins; asking the affected connections for fresh tickets disconnects
// whoever the API no longer lets in, and the rest do not notice. Connections
// renew their tickets before they expire anyway, so a missed event delays
// a revocation by at most a ticket's lifetime.
func registerCollabListeners(bus *events.Bus, client collabRevalidator) {
	revalidate := func(req collab.RevalidateRequest) {
		if req.WorkspaceID == "" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), collabRevalidateTimeout)
			defer cancel()
			if err := client.Revalidate(ctx, req); err != nil {
				slog.Warn("collab revalidate failed", "workspace_id", req.WorkspaceID, "document", req.Document, "error", err)
			}
		}()
	}
	member := func(workspaceID, userID string) {
		if userID == "" {
			return
		}
		revalidate(collab.RevalidateRequest{WorkspaceID: workspaceID, Actor: &collab.Actor{Type: "member", ID: userID}})
	}

	// Removed from the workspace, or a role change such as becoming a guest,
	// who may not edit live at all.
	bus.Subscribe(protocol.EventMemberRemoved, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		userID, _ := payload["user_id"].(string)
		member(e.WorkspaceID, userID)
	})
	bus.Subscribe(protocol.EventMemberUpdated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		switch m := payload["member"].(type) {
		case handler.MemberWithUserResponse:
			member(e.WorkspaceID, m.UserID)
		case map[string]any:
			userID, _ := m["user_id"].(string)
			member(e.WorkspaceID, userID)
		}
	})

	// A project's visibility or members decide who sees its issues. Docs do
	// not depend on projects.
	for _, eventType := range []string{protocol.EventProjectUpdated, protocol.EventProjectMembersChanged, protocol.EventProjectDeleted} {
		bus.Subscribe(eventType, func(e events.Event) {
			revalidate(collab.RevalidateRequest{WorkspaceID: e.WorkspaceID, Type: "issue"})
		})
	}

	// An issue moved to another project may be hidden from some of its editors.
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, _ := e.Payload.(map[string]any)
		if changed, _ := payload["project_changed"].(bool); !changed {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		revalidate(collab.RevalidateRequest{WorkspaceID: e.WorkspaceID, Document: collab.IssueDocumentName(issue.ID)})
	})

	bus.Subscribe(protocol.EventWorkspaceDeleted, func(e events.Event) {
		revalidate(collab.RevalidateRequest{WorkspaceID: e.WorkspaceID})
	})
}
