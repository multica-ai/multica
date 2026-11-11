package googlechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/notifydebounce"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type notifierQueries interface {
	GetMemberByUserAndWorkspace(context.Context, db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	GetNotificationPreference(context.Context, db.GetNotificationPreferenceParams) (db.NotificationPreference, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	GetIssue(context.Context, pgtype.UUID) (db.Issue, error)
}

// Notifier pushes new member inbox items to a configured Google Chat space.
type Notifier struct {
	queries notifierQueries
	sender  Sender
	appURL  string
	logger  *slog.Logger
}

// NewNotifier builds the Google Chat channel for the shared inbox delivery buffer.
func NewNotifier(queries notifierQueries, sender Sender, appURL string, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	appURL = strings.TrimRight(strings.TrimSpace(appURL), "/")
	return &Notifier{
		queries: queries,
		sender:  sender,
		appURL:  appURL,
		logger:  logger,
	}
}

// DeliverInboxBatch sends one message for a recipient's surviving inbox items.
func (n *Notifier) DeliverInboxBatch(ctx context.Context, batch notifydebounce.Batch) {
	if batch.RecipientType != "member" || len(batch.Items) == 0 {
		return
	}
	recipientIDText := batch.RecipientID
	workspaceIDText := batch.WorkspaceID
	recipientID, err := util.ParseUUID(recipientIDText)
	if err != nil || !recipientID.Valid {
		return
	}
	workspaceID, err := util.ParseUUID(workspaceIDText)
	if err != nil || !workspaceID.Valid {
		return
	}

	if _, err := n.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      recipientID,
		WorkspaceID: workspaceID,
	}); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.logger.WarnContext(ctx, "google chat: member lookup failed",
				"workspace_id", workspaceIDText,
				"recipient_id", recipientIDText,
				"error", err)
		}
		return
	}

	preference, err := n.queries.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: workspaceID,
		UserID:      recipientID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.logger.WarnContext(ctx, "google chat: notification preference lookup failed",
				"workspace_id", workspaceIDText,
				"recipient_id", recipientIDText,
				"error", err)
		}
		return
	}
	var preferences map[string]string
	if err := json.Unmarshal(preference.Preferences, &preferences); err != nil {
		return
	}
	webhookURL := strings.TrimSpace(preferences["google_chat_webhook"])
	if webhookURL == "" || n.sender == nil {
		return
	}
	if _, err := ValidateWebhookURL(webhookURL); err != nil {
		n.logger.WarnContext(ctx, "google chat: stored webhook is invalid",
			"workspace_id", workspaceIDText,
			"recipient_id", recipientIDText)
		return
	}

	workspace, err := n.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		n.logger.WarnContext(ctx, "google chat: workspace lookup failed",
			"workspace_id", workspaceIDText,
			"recipient_id", recipientIDText,
			"error", err)
		return
	}

	notifications := make([]NotificationItem, 0, len(batch.Items))
	for _, item := range batch.Items {
		notification := NotificationItem{
			Title: item.Title,
		}
		if item.Body.Valid {
			notification.Body = item.Body.String
		}
		if item.IssueID.Valid {
			if issue, issueErr := n.queries.GetIssue(ctx, item.IssueID); issueErr == nil && issue.WorkspaceID == workspaceID {
				notification.IssueIdentifier = fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)
				notification.IssueURL = channel.IssueWebLink(
					n.appURL,
					workspace.Slug,
					notification.IssueIdentifier,
				)
			}
		}
		notifications = append(notifications, notification)
	}

	if err := n.sender.Send(
		ctx,
		webhookURL,
		FormatNotifications(workspace.Name, notifications),
	); err != nil {
		n.logger.WarnContext(ctx, "google chat: notification delivery failed",
			"workspace_id", workspaceIDText,
			"recipient_id", recipientIDText,
			"space_id", SpaceID(webhookURL),
			"error", err)
	}
}
