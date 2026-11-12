package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issueStatusNotifier is the slice of lark.IssueCallbackNotifier the listener
// drives; tests substitute a recorder.
type issueStatusNotifier interface {
	NotifyStatusChanged(ctx context.Context, issueID pgtype.UUID) error
}

const issueCallbackNotifyTimeout = 10 * time.Second

// registerIssueCallbackListener reports issue status changes to the chats that
// registered a callback for the issue. Bus delivery is synchronous and the
// notice is a platform round-trip, so it runs off the publishing request.
func registerIssueCallbackListener(bus *events.Bus, notifier issueStatusNotifier, async func(func())) {
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if changed, _ := payload["status_changed"].(bool); !changed {
			return
		}
		id, _, ok := issueIDAndStatusFromPayload(payload["issue"])
		if !ok {
			return
		}
		issueID, err := util.ParseUUID(id)
		if err != nil {
			return
		}
		async(func() {
			ctx, cancel := context.WithTimeout(context.Background(), issueCallbackNotifyTimeout)
			defer cancel()
			if err := notifier.NotifyStatusChanged(ctx, issueID); err != nil {
				slog.Warn("issue callback: status notice failed", "issue_id", id, "error", err)
			}
		})
	})
}
