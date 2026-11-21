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
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
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
	if policy.action != entitlement.ActionEnforce || policy.notifications == nil {
		return
	}
	switch policy.notifications.OnRejection {
	case entitlement.NotificationFirstRejectionPerPeriod, entitlement.NotificationFirstRejectionPerLimit:
	default:
		return
	}
	if _, err := s.createAutopilotQuotaRejectionNotice(ctx, policy, workspaceID, source, params); err != nil {
		slog.Warn("autopilot quota rejection notice delivery failed; quota rejection remains committed",
			"workspace_id", util.UUIDToString(workspaceID),
			"autopilot_id", util.UUIDToString(params.AutopilotID),
			"source", source,
			"error", err,
		)
	}
}

// autopilotQuotaRejectionAlreadyNotified reports whether the period's marker
// already covers a rejection at limit. Per period, any notice covers the rest
// of the period. Per limit, a notice covers only the limit it was sent at, so
// a limit raised by an add-on re-arms it. A notice from before the limit was
// recorded counts as covering the period: it cannot be told apart, and a
// duplicate notice is worse than a missed re-arm in that one period.
func autopilotQuotaRejectionAlreadyNotified(period db.AutopilotQuotaPeriod, mode string, limit int64) bool {
	if !period.RejectionNotifiedAt.Valid {
		return false
	}
	if mode != entitlement.NotificationFirstRejectionPerLimit || !period.RejectionNotifiedLimit.Valid {
		return true
	}
	return period.RejectionNotifiedLimit.Int64 == limit
}

// createAutopilotQuotaRejectionNotice writes the first rejection notice of the
// period (or of the limit, per the notification policy) and, once that
// commits, announces each row. The period row lock serialises concurrent
// rejections, so one limit is notified once.
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
	mode := entitlement.NotificationFirstRejectionPerPeriod
	if policy.notifications != nil {
		mode = policy.notifications.OnRejection
	}
	if autopilotQuotaRejectionAlreadyNotified(period, mode, policy.limit) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit already-delivered quota rejection notice: %w", err)
		}
		return nil, nil
	}

	var pending inbox.Deferred
	items, err := s.createAutopilotQuotaInboxItems(ctx, q, &pending, autopilotQuotaNoticeFacts{
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
		WorkspaceID:   period.WorkspaceID,
		PeriodStart:   period.PeriodStart,
		PeriodEnd:     period.PeriodEnd,
		NotifiedLimit: pgtype.Int8{Int64: policy.limit, Valid: true},
	}); err != nil {
		return nil, fmt.Errorf("mark autopilot quota rejection notified: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit quota rejection notice: %w", err)
	}
	pending.Flush(s.Bus)
	return items, nil
}

func (s *AutopilotService) createAutopilotQuotaInboxItems(
	ctx context.Context,
	q *db.Queries,
	pending *inbox.Deferred,
	facts autopilotQuotaNoticeFacts,
) ([]db.InboxItem, error) {
	autopilotTitle := ""
	autopilot, err := q.GetAutopilot(ctx, facts.AutopilotID)
	autopilotFound := err == nil
	if autopilotFound {
		autopilotTitle = autopilot.Title
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load autopilot for quota notice: %w", err)
	}
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, q, facts.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace billing notification recipients: %w", err)
	}
	// The person responsible for the rejected autopilot hears about it too,
	// even without billing access: its member creator, or the owner of the
	// agent that created it. A creator who left the workspace is skipped, and
	// one who already manages billing is not notified twice.
	if autopilotFound {
		creator, ok, err := ResolveAutopilotNotificationRecipient(ctx, q, autopilot)
		if err != nil {
			return nil, fmt.Errorf("resolve autopilot creator for quota notice: %w", err)
		}
		if ok && !containsAutopilotNotificationRecipient(recipients, creator) {
			recipients = append(recipients, creator)
		}
	}
	resetAt := facts.ResetAt.UTC().Format("January 2, 2006 at 15:04 UTC")

	body := fmt.Sprintf(
		"This workspace has reached its limit of %d autopilot runs for the current period. Further autopilot runs will not start until the allowance resets at %s, or until the workspace upgrades its plan or buys more runs.",
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
		item, inserted, err := inbox.Deliver(ctx, q, pending, inbox.Notification{Params: db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: facts.WorkspaceID,
			RecipientType: recipient.Type, RecipientID: recipient.ID,
			Type: "autopilot_quota_exceeded", Severity: "attention", IssueID: pgtype.UUID{},
			Title: "Autopilot run limit reached", Body: pgtype.Text{String: body, Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, ActorID: pgtype.UUID{},
			Details: details,
		}})
		if err != nil {
			return nil, fmt.Errorf("create autopilot quota inbox item: %w", err)
		}
		if !inserted {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func containsAutopilotNotificationRecipient(recipients []AutopilotNotificationRecipient, candidate AutopilotNotificationRecipient) bool {
	for _, recipient := range recipients {
		if recipient.Type == candidate.Type && recipient.ID == candidate.ID {
			return true
		}
	}
	return false
}
