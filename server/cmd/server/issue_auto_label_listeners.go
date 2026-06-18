package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func registerIssueAutoLabelListeners(bus *events.Bus, svc *service.IssueAutoLabelService) {
	if bus == nil || svc == nil {
		return
	}
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		if e.ActorType != "member" {
			return
		}
		issueID := issueIDFromCreatedPayload(e.Payload)
		if issueID == "" {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := svc.AutoLabelCreatedIssue(ctx, issueID); err != nil {
				slog.Warn("issue auto-label: failed",
					"workspace_id", e.WorkspaceID,
					"issue_id", issueID,
					"error", err,
				)
			}
		}()
	})
}

func issueIDFromCreatedPayload(payload any) string {
	m, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if issueID, ok := m["issue_id"].(string); ok {
		return issueID
	}
	issue, ok := extractIssueFields(m["issue"])
	if !ok {
		return ""
	}
	return issue.ID
}
