package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const autopilotQuotaFirstRejectionNoticeKey = "rejection_first"

type autopilotQuotaNoticeFacts struct {
	WorkspaceID pgtype.UUID
	AutopilotID pgtype.UUID
	Source      string
	Used        int64
	Reserved    int64
	Total       int64
	Limit       int64
	ResetAt     time.Time
}

func (s *AutopilotService) deliverAutopilotQuotaRejectionNotice(
	ctx context.Context,
	policy autopilotQuotaPolicy,
	workspaceID pgtype.UUID,
	source string,
	params db.CreateAutopilotRunParams,
) {
	if policy.action != entitlement.ActionEnforce || policy.notifications == nil ||
		policy.notifications.OnRejection != entitlement.NotificationFirstRejectionPerPeriod {
		return
	}
	items, err := s.createAutopilotQuotaRejectionNotice(ctx, policy, workspaceID, source, params)
	if err != nil {
		slog.Warn("autopilot quota rejection notice delivery failed; quota rejection remains committed",
			"workspace_id", util.UUIDToString(workspaceID),
			"autopilot_id", util.UUIDToString(params.AutopilotID),
			"source", source,
			"error", err,
		)
		return
	}
	s.publishAutopilotQuotaInboxItems(items)
}

func (s *AutopilotService) createAutopilotQuotaRejectionNotice(
	ctx context.Context,
	policy autopilotQuotaPolicy,
	workspaceID pgtype.UUID,
	source string,
	params db.CreateAutopilotRunParams,
) ([]db.InboxItem, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin quota rejection notice: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)
	period, err := q.EnsureAutopilotQuotaPeriod(ctx, db.EnsureAutopilotQuotaPeriodParams{
		WorkspaceID: workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: policy.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: policy.periodEnd, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("lock quota period for rejection notice: %w", err)
	}
	if period.RejectionNotifiedAt.Valid {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit already-delivered quota rejection notice: %w", err)
		}
		return nil, nil
	}

	items, err := s.createAutopilotQuotaInboxItems(ctx, q, autopilotQuotaNoticeFacts{
		WorkspaceID: period.WorkspaceID,
		AutopilotID: params.AutopilotID,
		Source:      source,
		Used:        period.UsedCount,
		Reserved:    period.ReservedCount,
		Total:       period.UsedCount + period.ReservedCount,
		Limit:       policy.limit,
		ResetAt:     policy.resetAt,
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		// Keep the marker unset so a later rejection can notify a newly restored
		// workspace owner or admin instead of silencing the entire period.
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit quota rejection notice without recipients: %w", err)
		}
		return nil, nil
	}
	if _, err := q.MarkAutopilotQuotaRejectionNotified(ctx, db.MarkAutopilotQuotaRejectionNotifiedParams{
		WorkspaceID: period.WorkspaceID,
		PeriodStart: period.PeriodStart,
		PeriodEnd:   period.PeriodEnd,
	}); err != nil {
		return nil, fmt.Errorf("mark autopilot quota rejection notified: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit quota rejection notice: %w", err)
	}
	return items, nil
}

func (s *AutopilotService) createAutopilotQuotaInboxItems(
	ctx context.Context,
	q *db.Queries,
	facts autopilotQuotaNoticeFacts,
) ([]db.InboxItem, error) {
	autopilotTitle := ""
	autopilot, err := q.GetAutopilot(ctx, facts.AutopilotID)
	if err == nil {
		autopilotTitle = autopilot.Title
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load autopilot for quota notice: %w", err)
	}
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, q, facts.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace billing notification recipients: %w", err)
	}
	resetAt := facts.ResetAt.UTC().Format("January 2, 2006 at 15:04 UTC")

	body := fmt.Sprintf(
		"This workspace has reached its limit of %d autopilot runs for the current period. Further autopilot runs will not start until the allowance resets at %s.",
		facts.Limit, resetAt,
	)
	details, err := json.Marshal(map[string]string{
		"notice_kind":     "rejected",
		"notice_key":      autopilotQuotaFirstRejectionNoticeKey,
		"used":            strconv.FormatInt(facts.Used, 10),
		"reserved":        strconv.FormatInt(facts.Reserved, 10),
		"total":           strconv.FormatInt(facts.Total, 10),
		"limit":           strconv.FormatInt(facts.Limit, 10),
		"reset_at":        facts.ResetAt.UTC().Format(time.RFC3339),
		"source":          facts.Source,
		"autopilot_id":    util.UUIDToString(facts.AutopilotID),
		"autopilot_title": autopilotTitle,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal autopilot quota inbox details: %w", err)
	}

	items := make([]db.InboxItem, 0, len(recipients))
	for _, recipient := range recipients {
		item, err := q.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: facts.WorkspaceID,
			RecipientType: recipient.Type, RecipientID: recipient.ID,
			Type: "autopilot_quota_exceeded", Severity: "attention", IssueID: pgtype.UUID{},
			Title: "Autopilot run limit reached", Body: pgtype.Text{String: body, Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, ActorID: pgtype.UUID{},
			Details: details,
		})
		if err != nil {
			return nil, fmt.Errorf("create autopilot quota inbox item: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *AutopilotService) publishAutopilotQuotaInboxItems(items []db.InboxItem) {
	if s.Bus == nil {
		return
	}
	for _, item := range items {
		s.Bus.Publish(events.Event{
			Type: protocol.EventInboxNew, WorkspaceID: util.UUIDToString(item.WorkspaceID),
			ActorType: "system",
			Payload: map[string]any{"item": map[string]any{
				"id": util.UUIDToString(item.ID), "workspace_id": util.UUIDToString(item.WorkspaceID),
				"recipient_type": item.RecipientType, "recipient_id": util.UUIDToString(item.RecipientID),
				"type": item.Type, "severity": item.Severity, "issue_id": nil,
				"issue_status": nil, "issue_priority": nil,
				"title": item.Title, "body": util.TextToPtr(item.Body), "read": item.Read,
				"archived": item.Archived, "created_at": util.TimestampToString(item.CreatedAt),
				"actor_type": util.TextToPtr(item.ActorType), "actor_id": nil,
				"details": json.RawMessage(item.Details),
			}},
		})
	}
}
