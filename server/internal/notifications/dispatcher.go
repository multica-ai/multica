package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type NotificationMessage struct {
	InboxItemID       string
	WorkspaceID       string
	RecipientUserID   string
	Type              string
	Severity          string
	Title             string
	Body              string
	IssueID           string
	IssueIdentifier   string
	IssueStatus       string
	ActorType         string
	ActorID           string
	URL               string
	RecipientExternal string
}

type NotificationChannel interface {
	Name() string
	Send(ctx context.Context, msg NotificationMessage) error
}

type Dispatcher struct {
	queries  *db.Queries
	cfg      Config
	channels map[string]NotificationChannel
}

func NewDispatcher(queries *db.Queries, cfg Config, channels ...NotificationChannel) *Dispatcher {
	byName := make(map[string]NotificationChannel, len(channels))
	for _, channel := range channels {
		if channel == nil || strings.TrimSpace(channel.Name()) == "" {
			continue
		}
		byName[channel.Name()] = channel
	}
	return &Dispatcher{queries: queries, cfg: cfg, channels: byName}
}

func (d *Dispatcher) Enabled() bool {
	return d != nil && d.cfg.Enabled && len(d.channels) > 0
}

func (d *Dispatcher) RegisterInboxListener(bus *events.Bus) {
	if !d.Enabled() {
		return
	}
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		d.enqueueInboxEvent(e)
	})
}

func (d *Dispatcher) RunWorker(ctx context.Context) {
	if !d.Enabled() {
		return
	}
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	d.processBatch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.processBatch(ctx)
		}
	}
}

func (d *Dispatcher) enqueueInboxEvent(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	if stringField(item, "recipient_type") != "member" {
		return
	}
	notifType := stringField(item, "type")
	if !deliverableType(notifType) {
		return
	}

	inboxItemID, err := util.ParseUUID(stringField(item, "id"))
	if err != nil {
		slog.Warn("notification delivery skipped invalid inbox item id", "error", err)
		return
	}
	workspaceID, err := util.ParseUUID(stringField(item, "workspace_id"))
	if err != nil {
		slog.Warn("notification delivery skipped invalid workspace id", "error", err)
		return
	}
	recipientID, err := util.ParseUUID(stringField(item, "recipient_id"))
	if err != nil {
		slog.Warn("notification delivery skipped invalid recipient id", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for channelName := range d.channels {
		muted, err := d.channelMuted(ctx, workspaceID, recipientID, notifType, channelName)
		if err != nil {
			slog.Warn("failed to load notification channel preference", "channel", channelName, "error", err)
		}
		if muted {
			continue
		}
		dedupeKey := fmt.Sprintf("%s:%s:%s", channelName, stringField(item, "id"), stringField(item, "recipient_id"))
		if err := d.queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
			InboxItemID:     inboxItemID,
			WorkspaceID:     workspaceID,
			RecipientUserID: recipientID,
			Channel:         channelName,
			DedupeKey:       dedupeKey,
		}); err != nil {
			slog.Warn("failed to enqueue notification delivery", "channel", channelName, "error", err)
		}
	}
}

func (d *Dispatcher) processBatch(ctx context.Context) {
	channels := make([]string, 0, len(d.channels))
	for name := range d.channels {
		channels = append(channels, name)
	}
	if len(channels) == 0 {
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	rows, err := d.queries.ClaimPendingNotificationDeliveries(queryCtx, db.ClaimPendingNotificationDeliveriesParams{
		Channels:    channels,
		MaxAttempts: d.cfg.MaxAttempts,
		Limit:       d.cfg.BatchSize,
	})
	cancel()
	if err != nil {
		slog.Warn("failed to claim notification deliveries", "error", err)
		return
	}
	for _, delivery := range rows {
		d.processDelivery(ctx, delivery)
	}
}

func (d *Dispatcher) processDelivery(ctx context.Context, delivery db.NotificationDelivery) {
	channel := d.channels[delivery.Channel]
	if channel == nil {
		d.markFailed(ctx, delivery.ID, "notification channel is not registered")
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	row, err := d.queries.GetNotificationDeliveryMessage(queryCtx, db.GetNotificationDeliveryMessageParams{
		Provider: channel.Name(),
		ID:       delivery.ID,
	})
	cancel()
	if err != nil {
		d.retryOrFail(ctx, delivery, err)
		return
	}
	if !row.RecipientOpenID.Valid || strings.TrimSpace(row.RecipientOpenID.String) == "" {
		d.markFailed(ctx, delivery.ID, "recipient has no Lark open_id")
		return
	}

	msg := d.messageFromRow(row)
	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	err = channel.Send(sendCtx, msg)
	sendCancel()
	if err != nil {
		d.retryOrFail(ctx, delivery, err)
		return
	}

	updateCtx, updateCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := d.queries.MarkNotificationDeliverySent(updateCtx, delivery.ID); err != nil {
		slog.Warn("failed to mark notification delivery sent", "delivery_id", util.UUIDToString(delivery.ID), "error", err)
	}
	updateCancel()
}

func (d *Dispatcher) messageFromRow(row db.GetNotificationDeliveryMessageRow) NotificationMessage {
	issueID := util.UUIDToString(row.IssueID)
	identifier := ""
	if row.IssueNumber.Valid {
		identifier = row.IssuePrefix + "-" + strconv.Itoa(int(row.IssueNumber.Int32))
	}
	return NotificationMessage{
		InboxItemID:       util.UUIDToString(row.InboxItemID),
		WorkspaceID:       util.UUIDToString(row.WorkspaceID),
		RecipientUserID:   util.UUIDToString(row.RecipientUserID),
		Type:              row.Type,
		Severity:          row.Severity,
		Title:             row.Title,
		Body:              textValue(row.Body),
		IssueID:           issueID,
		IssueIdentifier:   identifier,
		IssueStatus:       textValue(row.IssueStatus),
		ActorType:         textValue(row.ActorType),
		ActorID:           util.UUIDToString(row.ActorID),
		URL:               d.issueURL(row.WorkspaceSlug, issueID),
		RecipientExternal: strings.TrimSpace(row.RecipientOpenID.String),
	}
}

func (d *Dispatcher) issueURL(workspaceSlug, issueID string) string {
	if workspaceSlug == "" || issueID == "" {
		return d.cfg.AppURL
	}
	return d.cfg.AppURL + "/" + url.PathEscape(workspaceSlug) + "/issues/" + url.PathEscape(issueID)
}

func (d *Dispatcher) retryOrFail(ctx context.Context, delivery db.NotificationDelivery, err error) {
	if delivery.RetryCount+1 >= d.cfg.MaxAttempts {
		d.markFailed(ctx, delivery.ID, err.Error())
		return
	}
	delay := d.backoff(delivery.RetryCount)
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if updateErr := d.queries.MarkNotificationDeliveryPendingAfterFailure(updateCtx, db.MarkNotificationDeliveryPendingAfterFailureParams{
		ID:           delivery.ID,
		LastError:    err.Error(),
		DelaySeconds: int32(delay.Seconds()),
	}); updateErr != nil {
		slog.Warn("failed to reschedule notification delivery", "delivery_id", util.UUIDToString(delivery.ID), "error", updateErr)
	}
	cancel()
}

func (d *Dispatcher) markFailed(ctx context.Context, id pgtype.UUID, msg string) {
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := d.queries.MarkNotificationDeliveryFailed(updateCtx, db.MarkNotificationDeliveryFailedParams{
		ID:        id,
		LastError: msg,
	}); err != nil {
		slog.Warn("failed to mark notification delivery failed", "delivery_id", util.UUIDToString(id), "error", err)
	}
	cancel()
}

func (d *Dispatcher) backoff(retryCount int32) time.Duration {
	delay := d.cfg.InitialBackoff
	for i := int32(0); i < retryCount; i++ {
		delay *= 2
		if delay >= d.cfg.MaxBackoff {
			return d.cfg.MaxBackoff
		}
	}
	if delay > d.cfg.MaxBackoff {
		return d.cfg.MaxBackoff
	}
	return delay
}

func (d *Dispatcher) channelMuted(ctx context.Context, workspaceID, userID pgtype.UUID, notifType, channel string) (bool, error) {
	group := notifTypeToGroup[notifType]
	if group == "" {
		return false, nil
	}
	pref, err := d.queries.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return false, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(pref.Preferences, &raw); err != nil {
		return false, nil
	}
	value, ok := raw[group]
	if !ok {
		return false, nil
	}
	var legacy string
	if err := json.Unmarshal(value, &legacy); err == nil {
		return legacy == "muted", nil
	}
	var byChannel map[string]string
	if err := json.Unmarshal(value, &byChannel); err != nil {
		return false, nil
	}
	return byChannel[channel] == "muted", nil
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

var highValueTypes = map[string]bool{
	"issue_assigned":  true,
	"mentioned":       true,
	"new_comment":     true,
	"task_failed":     true,
	"agent_completed": true,
	"task_completed":  true,
}

func deliverableType(notifType string) bool {
	return highValueTypes[notifType]
}

var notifTypeToGroup = map[string]string{
	"issue_assigned":   "assignments",
	"unassigned":       "assignments",
	"assignee_changed": "assignments",
	"status_changed":   "status_changes",
	"new_comment":      "comments",
	"mentioned":        "comments",
	"priority_changed": "updates",
	"due_date_changed": "updates",
	"task_completed":   "agent_activity",
	"task_failed":      "agent_activity",
	"agent_blocked":    "agent_activity",
	"agent_completed":  "agent_activity",
}
