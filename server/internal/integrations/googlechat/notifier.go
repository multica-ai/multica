package googlechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	spawn   func(func())
}

// NewNotifier builds an EventInboxNew subscriber.
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
		spawn:   func(run func()) { go run() },
	}
}

// Register subscribes without adding work to the synchronous Publish path.
func (n *Notifier) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, func(event events.Event) {
		n.spawn(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			n.deliver(ctx, event)
		})
	})
}

func (n *Notifier) deliver(ctx context.Context, event events.Event) {
	item, ok := eventItem(event.Payload)
	if !ok || stringValue(item["recipient_type"]) != "member" {
		return
	}
	recipientIDText := stringValue(item["recipient_id"])
	workspaceIDText := stringValue(item["workspace_id"])
	if workspaceIDText == "" {
		workspaceIDText = event.WorkspaceID
	}
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

	notification := NotificationItem{
		Title: stringValue(item["title"]),
		Body:  stringValue(item["body"]),
	}
	if issueIDText := stringValue(item["issue_id"]); issueIDText != "" {
		if issueID, parseErr := util.ParseUUID(issueIDText); parseErr == nil && issueID.Valid {
			if issue, issueErr := n.queries.GetIssue(ctx, issueID); issueErr == nil && issue.WorkspaceID == workspaceID {
				notification.IssueIdentifier = fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)
				notification.IssueURL = channel.IssueWebLink(
					n.appURL,
					workspace.Slug,
					notification.IssueIdentifier,
				)
			}
		}
	}

	if err := n.sender.Send(
		ctx,
		webhookURL,
		FormatNotifications(workspace.Name, []NotificationItem{notification}),
	); err != nil {
		n.logger.WarnContext(ctx, "google chat: notification delivery failed",
			"workspace_id", workspaceIDText,
			"recipient_id", recipientIDText,
			"space_id", SpaceID(webhookURL),
			"error", err)
	}
}

func eventItem(payload any) (map[string]any, bool) {
	value, ok := payload.(map[string]any)
	if !ok {
		return nil, false
	}
	item, ok := value["item"].(map[string]any)
	return item, ok
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed != nil {
			return *typed
		}
	}
	return ""
}
