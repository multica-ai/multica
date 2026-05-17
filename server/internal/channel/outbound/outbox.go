package outbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	channelmetrics "github.com/multica-ai/multica/server/internal/channel/metrics"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

const (
	OutboxAggregationWindow = 60 * time.Second
	OutboxBatchSize         = 100
	OutboxTickInterval      = 10 * time.Second
)

type NotificationEnqueueRequest struct {
	Provider              string
	ConnectionID          string
	EventKind             string
	TargetUserID          pgtype.UUID
	TargetExternalUserID  string
	TargetType            string
	TargetChatID          string
	MentionExternalUserID string
	Title                 string
	Body                  string
	WorkspaceID           pgtype.UUID
	IssueID               pgtype.UUID
	IssueIdentifier       string
	IssueTitle            string
	InboxItemID           pgtype.UUID
	ActorType             string
	ActorID               pgtype.UUID
	SourceCommentID       pgtype.UUID
	Replyable             bool
}

type NotificationEnqueuer interface {
	EnqueueNotification(ctx context.Context, req NotificationEnqueueRequest) error
}

type OutboxNotification struct {
	ID                    pgtype.UUID
	Provider              string
	ConnectionID          string
	EventKind             string
	TargetUserID          pgtype.UUID
	TargetExternalUserID  string
	TargetType            string
	TargetChatID          string
	MentionExternalUserID string
	Title                 string
	Body                  string
	WorkspaceID           pgtype.UUID
	IssueID               pgtype.UUID
	IssueIdentifier       string
	IssueTitle            string
	InboxItemID           pgtype.UUID
	ActorType             string
	ActorID               pgtype.UUID
	SourceCommentID       pgtype.UUID
	Replyable             bool
	Attempts              int32
	MaxAttempts           int32
}

type outboxDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type DBNotificationStore struct {
	db     outboxDB
	window time.Duration
}

func NewDBNotificationStore(db outboxDB) *DBNotificationStore {
	return &DBNotificationStore{db: db, window: OutboxAggregationWindow}
}

func (s *DBNotificationStore) EnqueueNotification(ctx context.Context, req NotificationEnqueueRequest) error {
	targetType := strings.TrimSpace(req.TargetType)
	if targetType == "" {
		targetType = string(port.OutboundTargetUser)
	}
	if strings.TrimSpace(req.Provider) == "" || strings.TrimSpace(req.ConnectionID) == "" ||
		strings.TrimSpace(req.EventKind) == "" ||
		!req.TargetUserID.Valid {
		return errors.New("outbox: invalid notification enqueue request")
	}
	if targetType == string(port.OutboundTargetChat) {
		if strings.TrimSpace(req.TargetChatID) == "" {
			return errors.New("outbox: invalid chat notification enqueue request")
		}
	} else if strings.TrimSpace(req.TargetExternalUserID) == "" {
		return errors.New("outbox: invalid user notification enqueue request")
	}
	window := s.window
	if window <= 0 {
		window = OutboxAggregationWindow
	}
	const q = `
INSERT INTO channel_outbound_notification (
    provider, connection_id, event_kind, target_user_id, target_external_user_id,
    target_type, target_chat_id, mention_external_user_id,
    title, body, workspace_id, issue_id, issue_identifier, issue_title, inbox_item_id,
    actor_type, actor_id, source_comment_id, replyable, aggregation_due_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
          $16, $17, $18, $19, now() + $20::interval)
`
	_, err := s.db.Exec(ctx, q,
		req.Provider,
		req.ConnectionID,
		req.EventKind,
		req.TargetUserID,
		nullableString(req.TargetExternalUserID),
		targetType,
		strings.TrimSpace(req.TargetChatID),
		strings.TrimSpace(req.MentionExternalUserID),
		req.Title,
		req.Body,
		nullableUUID(req.WorkspaceID),
		nullableUUID(req.IssueID),
		strings.TrimSpace(req.IssueIdentifier),
		strings.TrimSpace(req.IssueTitle),
		nullableUUID(req.InboxItemID),
		normalizeActorType(req.ActorType),
		nullableUUID(req.ActorID),
		nullableUUID(req.SourceCommentID),
		req.Replyable,
		pgInterval(window),
	)
	return err
}

func (s *DBNotificationStore) ClaimDue(ctx context.Context, limit int32, readyConnectionIDs []string) ([]OutboxNotification, error) {
	if readyConnectionIDs != nil && len(readyConnectionIDs) == 0 {
		return nil, nil
	}
	if readyConnectionIDs != nil {
		const q = `
UPDATE channel_outbound_notification SET
    status = 'processing',
    next_attempt_at = now() + interval '5 minutes',
    updated_at = now()
WHERE id IN (
    SELECT id FROM channel_outbound_notification
    WHERE status = 'pending'
      AND aggregation_due_at <= now()
      AND next_attempt_at <= now()
      AND connection_id = ANY($2::text[])
    ORDER BY aggregation_due_at ASC, created_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, provider, connection_id, event_kind, target_user_id,
          COALESCE(target_external_user_id, '') AS target_external_user_id,
          target_type, target_chat_id, mention_external_user_id,
          title, body, workspace_id, issue_id, issue_identifier, issue_title, inbox_item_id,
          actor_type, actor_id, source_comment_id, replyable, attempts, max_attempts
`
		return s.queryNotifications(ctx, q, limit, readyConnectionIDs)
	}
	const q = `
UPDATE channel_outbound_notification SET
    status = 'processing',
    next_attempt_at = now() + interval '5 minutes',
    updated_at = now()
WHERE id IN (
    SELECT id FROM channel_outbound_notification
    WHERE status = 'pending'
      AND aggregation_due_at <= now()
      AND next_attempt_at <= now()
    ORDER BY aggregation_due_at ASC, created_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, provider, connection_id, event_kind, target_user_id,
          COALESCE(target_external_user_id, '') AS target_external_user_id,
          target_type, target_chat_id, mention_external_user_id,
          title, body, workspace_id, issue_id, issue_identifier, issue_title, inbox_item_id,
          actor_type, actor_id, source_comment_id, replyable, attempts, max_attempts
`
	return s.queryNotifications(ctx, q, limit)
}

func (s *DBNotificationStore) ReclaimStaleProcessing(ctx context.Context, limit int32, staleAfter time.Duration, readyConnectionIDs []string) ([]OutboxNotification, error) {
	if readyConnectionIDs != nil && len(readyConnectionIDs) == 0 {
		return nil, nil
	}
	if readyConnectionIDs != nil {
		const q = `
UPDATE channel_outbound_notification SET
    status = 'processing',
    next_attempt_at = now() + interval '5 minutes',
    updated_at = now()
WHERE id IN (
    SELECT id FROM channel_outbound_notification
    WHERE status = 'processing'
      AND updated_at < now() - $2::interval
      AND connection_id = ANY($3::text[])
    ORDER BY updated_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, provider, connection_id, event_kind, target_user_id,
          COALESCE(target_external_user_id, '') AS target_external_user_id,
          target_type, target_chat_id, mention_external_user_id,
          title, body, workspace_id, issue_id, issue_identifier, issue_title, inbox_item_id,
          actor_type, actor_id, source_comment_id, replyable, attempts, max_attempts
`
		return s.queryNotifications(ctx, q, limit, pgInterval(staleAfter), readyConnectionIDs)
	}
	const q = `
UPDATE channel_outbound_notification SET
    status = 'processing',
    next_attempt_at = now() + interval '5 minutes',
    updated_at = now()
WHERE id IN (
    SELECT id FROM channel_outbound_notification
    WHERE status = 'processing'
      AND updated_at < now() - $2::interval
    ORDER BY updated_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, provider, connection_id, event_kind, target_user_id,
          COALESCE(target_external_user_id, '') AS target_external_user_id,
          target_type, target_chat_id, mention_external_user_id,
          title, body, workspace_id, issue_id, issue_identifier, issue_title, inbox_item_id,
          actor_type, actor_id, source_comment_id, replyable, attempts, max_attempts
`
	return s.queryNotifications(ctx, q, limit, pgInterval(staleAfter))
}

func (s *DBNotificationStore) queryNotifications(ctx context.Context, q string, args ...any) ([]OutboxNotification, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OutboxNotification
	for rows.Next() {
		var n OutboxNotification
		if err := rows.Scan(
			&n.ID,
			&n.Provider,
			&n.ConnectionID,
			&n.EventKind,
			&n.TargetUserID,
			&n.TargetExternalUserID,
			&n.TargetType,
			&n.TargetChatID,
			&n.MentionExternalUserID,
			&n.Title,
			&n.Body,
			&n.WorkspaceID,
			&n.IssueID,
			&n.IssueIdentifier,
			&n.IssueTitle,
			&n.InboxItemID,
			&n.ActorType,
			&n.ActorID,
			&n.SourceCommentID,
			&n.Replyable,
			&n.Attempts,
			&n.MaxAttempts,
		); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *DBNotificationStore) MarkSent(ctx context.Context, ids []pgtype.UUID) error {
	const q = `
UPDATE channel_outbound_notification SET
    status = 'sent',
    updated_at = now(),
    last_error = NULL
WHERE id = $1
`
	for _, id := range ids {
		if _, err := s.db.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *DBNotificationStore) ScheduleRetry(ctx context.Context, ids []pgtype.UUID, lastError string, backoff time.Duration) error {
	const q = `
UPDATE channel_outbound_notification SET
    status = 'pending',
    attempts = attempts + 1,
    next_attempt_at = now() + $2::interval,
    updated_at = now(),
    last_error = $3
WHERE id = $1
`
	for _, id := range ids {
		if _, err := s.db.Exec(ctx, q, id, pgInterval(backoff), truncateError(lastError)); err != nil {
			return err
		}
	}
	return nil
}

func (s *DBNotificationStore) MarkDead(ctx context.Context, ids []pgtype.UUID, lastError string) error {
	const q = `
UPDATE channel_outbound_notification SET
    status = 'dead',
    updated_at = now(),
    last_error = $2
WHERE id = $1
`
	for _, id := range ids {
		if _, err := s.db.Exec(ctx, q, id, truncateError(lastError)); err != nil {
			return err
		}
	}
	return nil
}

func (s *DBNotificationStore) Cleanup(ctx context.Context) error {
	const q = `
DELETE FROM channel_outbound_notification
WHERE status IN ('sent', 'dead')
  AND updated_at < now() - interval '7 days'
`
	_, err := s.db.Exec(ctx, q)
	return err
}

type OutboxWorker struct {
	store            NotificationStore
	sender           RetrySender
	recorder         SentNotificationRecorder
	active           func() bool
	readyConnections func() []string
}

type NotificationStore interface {
	ClaimDue(ctx context.Context, limit int32, readyConnectionIDs []string) ([]OutboxNotification, error)
	ReclaimStaleProcessing(ctx context.Context, limit int32, staleAfter time.Duration, readyConnectionIDs []string) ([]OutboxNotification, error)
	MarkSent(ctx context.Context, ids []pgtype.UUID) error
	ScheduleRetry(ctx context.Context, ids []pgtype.UUID, lastError string, backoff time.Duration) error
	MarkDead(ctx context.Context, ids []pgtype.UUID, lastError string) error
	Cleanup(ctx context.Context) error
}

func NewOutboxWorker(store NotificationStore, sender RetrySender) *OutboxWorker {
	return &OutboxWorker{store: store, sender: sender}
}

type SentNotificationRecorder interface {
	RecordSentNotification(ctx context.Context, group notificationGroup, payload RetryPayload, result port.SendResult) error
}

func (w *OutboxWorker) SetMessageRecorder(recorder SentNotificationRecorder) {
	w.recorder = recorder
}

func (w *OutboxWorker) SetActiveFunc(active func() bool) {
	w.active = active
}

// SetReadyConnectionsFunc limits claims to connection ids that currently have
// a live adapter. A nil function means the worker is connection-unscoped, which
// is kept for older tests and standalone uses.
func (w *OutboxWorker) SetReadyConnectionsFunc(readyConnections func() []string) {
	w.readyConnections = readyConnections
}

func (w *OutboxWorker) readyConnectionIDs() []string {
	if w.active != nil && !w.active() {
		return []string{}
	}
	if w.readyConnections == nil {
		return nil
	}
	return normalizeConnectionIDs(w.readyConnections())
}

func (w *OutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(OutboxTickInterval)
	defer ticker.Stop()
	cleanupTicker := time.NewTicker(CleanupTickInterval)
	defer cleanupTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		case <-cleanupTicker.C:
			if err := w.store.Cleanup(ctx); err != nil {
				slog.Error("outbox worker: cleanup failed", "error", err)
			}
		}
	}
}

func (w *OutboxWorker) processBatch(ctx context.Context) {
	readyConnectionIDs := w.readyConnectionIDs()
	if readyConnectionIDs != nil && len(readyConnectionIDs) == 0 {
		return
	}

	reclaimed, err := w.store.ReclaimStaleProcessing(ctx, OutboxBatchSize, 5*time.Minute, readyConnectionIDs)
	if err != nil {
		channelmetrics.M.RecordOutboundOutbox("unknown", "reclaim_error", 1)
		slog.Error("outbox worker: reclaim stale processing failed", "error", err)
	}

	rows, err := w.store.ClaimDue(ctx, OutboxBatchSize, readyConnectionIDs)
	if err != nil {
		channelmetrics.M.RecordOutboundOutbox("unknown", "claim_error", 1)
		slog.Error("outbox worker: claim failed", "error", err)
		rows = nil
	}
	groups := groupNotifications(append(reclaimed, rows...))
	for _, g := range groups {
		w.processGroup(ctx, g)
	}
}

func normalizeConnectionIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

type notificationGroup struct {
	provider      string
	connectionID  string
	eventKind     string
	target        port.OutboundTarget
	mentionUserID string
	targetUserID  pgtype.UUID
	items         []OutboxNotification
}

func groupNotifications(rows []OutboxNotification) []notificationGroup {
	byKey := map[string]*notificationGroup{}
	for _, n := range rows {
		target := notificationTarget(n)
		if target.ID == "" {
			continue
		}
		key := n.ConnectionID + "\x00" + n.EventKind + "\x00" + string(target.Type) + "\x00" + target.ID + "\x00" + uuidStr(n.TargetUserID)
		g := byKey[key]
		if g == nil {
			g = &notificationGroup{
				provider:      n.Provider,
				connectionID:  n.ConnectionID,
				eventKind:     n.EventKind,
				target:        target,
				mentionUserID: strings.TrimSpace(n.MentionExternalUserID),
				targetUserID:  n.TargetUserID,
			}
			byKey[key] = g
		}
		g.items = append(g.items, n)
	}
	out := make([]notificationGroup, 0, len(byKey))
	for _, g := range byKey {
		out = append(out, *g)
	}
	return out
}

func notificationTarget(n OutboxNotification) port.OutboundTarget {
	switch n.TargetType {
	case string(port.OutboundTargetChat):
		return port.TargetChat(strings.TrimSpace(n.TargetChatID))
	default:
		return port.TargetUser(strings.TrimSpace(n.TargetExternalUserID))
	}
}

func (w *OutboxWorker) processGroup(ctx context.Context, g notificationGroup) {
	if len(g.items) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(g.items))
	for _, item := range g.items {
		ids = append(ids, item.ID)
	}
	payload := RetryPayload{
		Title:    fmt.Sprintf("Multica 有 %d 条新通知", len(g.items)),
		Body:     buildOutboxBody(g.items),
		Mentions: mentionList(g.mentionUserID),
	}
	if len(g.items) == 1 {
		payload.Title = g.items[0].Title
		payload.Body = g.items[0].Body
	}
	result, err := w.sender.SendCard(ctx, g.connectionID, g.target, payload)
	if err == nil {
		if w.recorder != nil {
			if recordErr := w.recorder.RecordSentNotification(ctx, g, payload, result); recordErr != nil {
				slog.Error("outbox worker: record sent notification failed", "error", recordErr)
			}
		}
		channelmetrics.M.RecordOutboundOutbox(g.provider, "sent", len(g.items))
		if markErr := w.store.MarkSent(ctx, ids); markErr != nil {
			slog.Error("outbox worker: mark sent failed", "error", markErr)
		}
		return
	}

	var retryIDs, deadIDs []pgtype.UUID
	for _, item := range g.items {
		if !IsRetryable(err) || item.Attempts >= item.MaxAttempts {
			deadIDs = append(deadIDs, item.ID)
		} else {
			retryIDs = append(retryIDs, item.ID)
		}
	}

	if len(retryIDs) > 0 {
		for _, id := range retryIDs {
			backoff := backoffForAttempt(int(itemAttemptsByID(g.items, id)))
			if retryErr := w.store.ScheduleRetry(ctx, []pgtype.UUID{id}, err.Error(), backoff); retryErr != nil {
				slog.Error("outbox worker: schedule retry failed", "error", retryErr)
			}
		}
		channelmetrics.M.RecordOutboundOutbox(g.provider, "scheduled", len(retryIDs))
	}
	if len(deadIDs) > 0 {
		if deadErr := w.store.MarkDead(ctx, deadIDs, err.Error()); deadErr != nil {
			slog.Error("outbox worker: mark dead failed", "error", deadErr)
		}
		channelmetrics.M.RecordOutboundOutbox(g.provider, "dead", len(deadIDs))
	}
}

func itemAttemptsByID(items []OutboxNotification, id pgtype.UUID) int32 {
	for _, item := range items {
		if item.ID == id {
			return item.Attempts
		}
	}
	return 0
}

func buildOutboxBody(items []OutboxNotification) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteByte('\n')
		}
		_, _ = fmt.Fprintf(&b, "[%d] %s", i+1, item.Title)
		if item.Body != "" {
			b.WriteString(": ")
			b.WriteString(item.Body)
		}
	}
	return b.String()
}

func truncateError(s string) string {
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}

func nullableUUID(id pgtype.UUID) any {
	if !id.Valid {
		return nil
	}
	return id
}

func nullableString(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func normalizeActorType(s string) string {
	switch strings.TrimSpace(s) {
	case "member", "agent", "system":
		return strings.TrimSpace(s)
	default:
		return ""
	}
}
