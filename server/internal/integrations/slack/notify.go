package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const deliveryTimeout = 5 * time.Second

// IssueEvent holds the minimal issue data needed to format a Slack notification.
// Callers should populate this from their own representation (e.g., handler.IssueResponse).
type IssueEvent struct {
	WorkspaceID string
	Identifier  string // e.g. "FRG-42"
	Title       string
	Status      string // new status
}

// NotifyStatusChange fires an asynchronous Slack notification when an issue
// status changes. It is a fire-and-forget call: the caller returns immediately.
// Panics inside the goroutine are recovered and logged so they cannot crash the
// notification listener.
//
// actorName is a best-effort display name (e.g. the member's full name, or the
// agent ID string) used in the Slack message body.
func NotifyStatusChange(
	ctx context.Context,
	queries *db.Queries,
	issue IssueEvent,
	prevStatus string,
	actorName string,
) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("slack: recovered from panic", "panic", r)
			}
		}()

		deliverCtx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
		defer cancel()

		wsUUID := util.MustParseUUID(issue.WorkspaceID)

		integration, err := queries.GetSlackIntegrationForWorkspace(deliverCtx, wsUUID)
		if err != nil {
			// No enabled integration is the common case; not an error.
			return
		}

		if !shouldFire(integration.TriggerStatuses, issue.Status) {
			return
		}

		workspace, err := queries.GetWorkspace(deliverCtx, wsUUID)
		if err != nil {
			slog.Error("slack: fetch workspace failed", "workspace_id", issue.WorkspaceID, "error", err)
			_ = queries.UpdateSlackIntegrationStatus(deliverCtx, db.UpdateSlackIntegrationStatusParams{
				WorkspaceID: wsUUID,
				LastError:   pgtype.Text{String: fmt.Sprintf("fetch workspace: %v", err), Valid: true},
			})
			return
		}

		issueURL := fmt.Sprintf("https://forge.asymbl.app/%s/issues/%s",
			workspace.Slug, issue.Identifier)

		body := BuildMessage(MessageParams{
			IssueKey:   issue.Identifier,
			IssueTitle: issue.Title,
			PrevStatus: prevStatus,
			NewStatus:  issue.Status,
			ActorName:  actorName,
			IssueURL:   issueURL,
		})

		if postErr := PostWebhook(deliverCtx, integration.WebhookUrl, body); postErr != nil {
			slog.Warn("slack: delivery failed", "workspace_id", issue.WorkspaceID, "error", postErr)
			_ = queries.UpdateSlackIntegrationStatus(deliverCtx, db.UpdateSlackIntegrationStatusParams{
				WorkspaceID: wsUUID,
				LastError:   pgtype.Text{String: postErr.Error(), Valid: true},
			})
			return
		}

		slog.Info("slack: notification delivered", "workspace_id", issue.WorkspaceID, "issue", issue.Identifier)
		_ = queries.UpdateSlackIntegrationStatus(deliverCtx, db.UpdateSlackIntegrationStatusParams{
			WorkspaceID: wsUUID,
			LastSentAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
	}()
}

// shouldFire returns true when the new status warrants a Slack notification.
// An empty or missing trigger set means "fire on any status change".
func shouldFire(triggerStatuses []byte, newStatus string) bool {
	if len(triggerStatuses) == 0 {
		return true
	}
	var triggers []string
	if err := json.Unmarshal(triggerStatuses, &triggers); err != nil || len(triggers) == 0 {
		return true
	}
	for _, s := range triggers {
		if s == newStatus {
			return true
		}
	}
	return false
}
