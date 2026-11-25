package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Monthly app tool call limit (MUL-10047).
//
// Cloud sends the calls a workspace may send to the app provider in a period
// through the "app_tool_calls" gate. Every call the gateway is about to send
// to Composio is counted in app_tool_call_quota_period with one conditional
// upsert, before it runs, so a call that then fails upstream still counts, as
// the provider bills it. Under enforce the increment only happens below the
// limit, so concurrent calls never take a workspace past it. off, which is
// also the self-hosted behavior, touches no table.

// AppToolCallQuotaNoticeType is the Inbox type of the limit notice.
const AppToolCallQuotaNoticeType = "app_tool_calls_quota_exceeded"

// AppToolCallQuota checks the app_tool_calls gate. A zero value with no
// Entitlements admits every call without reading the database.
type AppToolCallQuota struct {
	Queries      *db.Queries
	Entitlements entitlement.Provider
	// TxStarter writes the limit notice; without it no notice is sent.
	TxStarter TxStarter
	Bus       *events.Bus
	now       func() time.Time
}

var _ connectedapps.CallQuota = AppToolCallQuota{}

type appToolCallPolicy struct {
	action        entitlement.Action
	limit         int64
	periodStart   time.Time
	periodEnd     time.Time
	resetAt       time.Time
	notifications *entitlement.NotificationPolicy
}

func appToolCallPolicyFrom(decision entitlement.Decision) (appToolCallPolicy, bool) {
	gate := decision.Gate
	if (gate.Action != entitlement.ActionObserve && gate.Action != entitlement.ActionEnforce) ||
		gate.Limit == nil || *gate.Limit < 0 || gate.PeriodStart == nil || gate.PeriodEnd == nil ||
		gate.ResetAt == nil || !gate.PeriodStart.Before(*gate.PeriodEnd) {
		return appToolCallPolicy{}, false
	}
	return appToolCallPolicy{
		action:        gate.Action,
		limit:         int64(*gate.Limit),
		periodStart:   gate.PeriodStart.UTC(),
		periodEnd:     gate.PeriodEnd.UTC(),
		resetAt:       gate.ResetAt.UTC(),
		notifications: gate.Notifications,
	}, true
}

// sameBudget reports whether two policies count the same calls against the
// same limit, so re-counting under the other would decide the same way.
func (p appToolCallPolicy) sameBudget(other appToolCallPolicy) bool {
	return p.action == other.action && p.limit == other.limit &&
		p.periodStart.Equal(other.periodStart) && p.periodEnd.Equal(other.periodEnd)
}

// policy is the workspace's current app call policy. One cached before its
// period ended (the first minute of a month) is re-read, so neither a call
// nor Billing lands on the month that is over; if Cloud still sends the old
// period, no limit applies for now.
func (q AppToolCallQuota) policy(ctx context.Context, workspaceID pgtype.UUID) (appToolCallPolicy, bool) {
	if q.Entitlements == nil || q.Queries == nil || !workspaceID.Valid {
		return appToolCallPolicy{}, false
	}
	policy, ok := appToolCallPolicyFrom(q.Entitlements.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateAppToolCalls))
	if ok && !q.clock().Before(policy.periodEnd) {
		if policy, ok = q.refreshedPolicy(ctx, workspaceID); ok && !q.clock().Before(policy.periodEnd) {
			return appToolCallPolicy{}, false
		}
	}
	return policy, ok
}

// refreshedPolicy reads Cloud's current policy, skipping the reuse window
// where the provider has one, as the autopilot quota does before a refusal:
// a seat bought or a month started moments ago must not be judged on a policy
// fetched just before.
func (q AppToolCallQuota) refreshedPolicy(ctx context.Context, workspaceID pgtype.UUID) (appToolCallPolicy, bool) {
	workspace := uuid.UUID(workspaceID.Bytes)
	var decision entitlement.Decision
	switch provider := q.Entitlements.(type) {
	case entitlement.FreshRefresher:
		decision = provider.RefreshGateNow(ctx, workspace, entitlement.GateAppToolCalls)
	case entitlement.Refresher:
		decision = provider.RefreshGate(ctx, workspace, entitlement.GateAppToolCalls)
	default:
		decision = q.Entitlements.Gate(ctx, workspace, entitlement.GateAppToolCalls)
	}
	return appToolCallPolicyFrom(decision)
}

func (q AppToolCallQuota) clock() time.Time {
	if q.now != nil {
		return q.now()
	}
	return time.Now()
}

// Admit counts one call that is about to be sent to the provider and returns
// the start of the period it was counted in; zero when it was not counted.
// Under enforce it refuses a call past the limit, after re-reading Cloud's
// policy so a workspace that has just upgraded or added seats is not refused
// on a cached one. Under observe it admits and records the overage. Every
// failure to decide admits without counting.
func (q AppToolCallQuota) Admit(ctx context.Context, workspaceID pgtype.UUID) (time.Time, *connectedapps.CallQuotaExceeded) {
	policy, ok := q.policy(ctx, workspaceID)
	if !ok {
		return time.Time{}, nil
	}
	used, admitted, err := q.increment(ctx, workspaceID, policy)
	if err != nil {
		return time.Time{}, nil
	}
	if !admitted {
		fresh, active := q.refreshedPolicy(ctx, workspaceID)
		if !active {
			return time.Time{}, nil
		}
		if !fresh.sameBudget(policy) {
			policy = fresh
			if used, admitted, err = q.increment(ctx, workspaceID, policy); err != nil {
				return time.Time{}, nil
			}
		}
	}
	if admitted {
		if used > policy.limit {
			q.recordOverage(ctx, workspaceID, policy, used)
		} else {
			EntitlementOverages{Queries: q.Queries}.SyncLimit(
				ctx, workspaceID, entitlement.GateAppToolCalls, policy.periodStart, policy.periodEnd, policy.limit,
			)
		}
		return policy.periodStart, nil
	}
	used = q.used(ctx, workspaceID, policy)
	q.recordOverage(ctx, workspaceID, policy, used+1)
	q.deliverRejectionNotice(ctx, workspaceID, policy, used)
	return time.Time{}, &connectedapps.CallQuotaExceeded{Used: used, Limit: policy.limit, ResetAt: policy.resetAt}
}

// increment counts the call. admitted=false is a refusal under enforce; an
// error admits.
func (q AppToolCallQuota) increment(ctx context.Context, workspaceID pgtype.UUID, policy appToolCallPolicy) (int64, bool, error) {
	used, err := q.Queries.IncrementAppToolCallQuota(ctx, db.IncrementAppToolCallQuotaParams{
		WorkspaceID: workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: policy.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: policy.periodEnd, Valid: true},
		Enforce:     policy.action == entitlement.ActionEnforce,
		LimitValue:  policy.limit,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		slog.WarnContext(ctx, "app tool call quota increment failed; admitting",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
		return 0, false, err
	}
	return used, true, nil
}

// used is the period's count, for a refusal's message. It falls back to the
// limit, which a refused workspace has reached.
func (q AppToolCallQuota) used(ctx context.Context, workspaceID pgtype.UUID, policy appToolCallPolicy) int64 {
	used, err := q.Queries.GetAppToolCallQuotaUsed(ctx, db.GetAppToolCallQuotaUsedParams{
		WorkspaceID: workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: policy.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: policy.periodEnd, Valid: true},
	})
	if err != nil {
		return policy.limit
	}
	return used
}

func (q AppToolCallQuota) recordOverage(ctx context.Context, workspaceID pgtype.UUID, policy appToolCallPolicy, attempted int64) {
	EntitlementOverages{Queries: q.Queries}.Record(ctx, LimitExceededAttempt{
		WorkspaceID:    workspaceID,
		Gate:           entitlement.GateAppToolCalls,
		Action:         policy.action,
		PeriodStart:    policy.periodStart,
		PeriodEnd:      policy.periodEnd,
		Limit:          policy.limit,
		AttemptedUsage: attempted,
	})
}

// deliverRejectionNotice tells the workspace's owners and admins, once per
// period or once per limit as Cloud asks, that calls are being refused. It
// is best-effort: a failure is logged and never changes the refusal.
func (q AppToolCallQuota) deliverRejectionNotice(ctx context.Context, workspaceID pgtype.UUID, policy appToolCallPolicy, used int64) {
	if q.TxStarter == nil || policy.notifications == nil {
		return
	}
	mode := policy.notifications.OnRejection
	if mode != entitlement.NotificationFirstRejectionPerPeriod && mode != entitlement.NotificationFirstRejectionPerLimit {
		return
	}
	if err := q.createRejectionNotice(ctx, workspaceID, policy, used, mode == entitlement.NotificationFirstRejectionPerLimit); err != nil {
		slog.WarnContext(ctx, "app tool call quota notice delivery failed; the refusal stands",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
	}
}

func (q AppToolCallQuota) createRejectionNotice(ctx context.Context, workspaceID pgtype.UUID, policy appToolCallPolicy, used int64, perLimit bool) error {
	tx, err := q.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin app tool call quota notice: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := q.Queries.WithTx(tx)
	if _, err := qtx.ClaimAppToolCallQuotaRejectionNotice(ctx, db.ClaimAppToolCallQuotaRejectionNoticeParams{
		WorkspaceID: workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: policy.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: policy.periodEnd, Valid: true},
		LimitValue:  policy.limit,
		PerLimit:    perLimit,
	}); errors.Is(err, pgx.ErrNoRows) {
		return nil // already notified at this limit
	} else if err != nil {
		return fmt.Errorf("claim app tool call quota notice: %w", err)
	}
	recipients, err := ListWorkspaceManagerNotificationRecipients(ctx, qtx, workspaceID)
	if err != nil {
		return fmt.Errorf("list app tool call quota notice recipients: %w", err)
	}
	resetAt := policy.resetAt.UTC().Format("January 2, 2006 at 15:04 UTC")
	body := fmt.Sprintf(
		"This workspace has used its %d app calls for the current period. Agents cannot run app tools until the allowance resets at %s, or until the workspace upgrades its plan or adds seats.",
		policy.limit, resetAt,
	)
	details, err := json.Marshal(map[string]string{
		"used":     strconv.FormatInt(used, 10),
		"limit":    strconv.FormatInt(policy.limit, 10),
		"reset_at": policy.resetAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal app tool call quota notice details: %w", err)
	}
	var pending inbox.Deferred
	delivered := 0
	for _, recipient := range recipients {
		_, inserted, err := inbox.Deliver(ctx, qtx, &pending, inbox.Notification{Params: db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: workspaceID,
			RecipientType: recipient.Type, RecipientID: recipient.ID,
			Type: AppToolCallQuotaNoticeType, Severity: "attention", IssueID: pgtype.UUID{},
			Title: "App call limit reached", Body: pgtype.Text{String: body, Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, ActorID: pgtype.UUID{},
			Details: details,
		}})
		if err != nil {
			return fmt.Errorf("create app tool call quota inbox item: %w", err)
		}
		if inserted {
			delivered++
		}
	}
	if delivered == 0 {
		// Roll the claim back so a later refusal can reach a manager added since.
		return nil
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit app tool call quota notice: %w", err)
	}
	if q.Bus != nil {
		pending.Flush(q.Bus)
	}
	return nil
}

// AppToolCallUsage is the monthly app call limit as clients render it. Limit
// and the period are set only while a limit applies; Reached only under
// enforce, because observe never refuses a call.
type AppToolCallUsage struct {
	Action      entitlement.Action
	Used        *int64
	Limit       *int64
	Reached     *bool
	PeriodStart *time.Time
	PeriodEnd   *time.Time
	ResetAt     *time.Time
}

// Usage reports the workspace's app call limit and this period's count.
func (q AppToolCallQuota) Usage(ctx context.Context, workspaceID pgtype.UUID) (AppToolCallUsage, error) {
	policy, ok := q.policy(ctx, workspaceID)
	if !ok {
		return AppToolCallUsage{Action: entitlement.ActionOff}, nil
	}
	used, err := q.Queries.GetAppToolCallQuotaUsed(ctx, db.GetAppToolCallQuotaUsedParams{
		WorkspaceID: workspaceID,
		PeriodStart: pgtype.Timestamptz{Time: policy.periodStart, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: policy.periodEnd, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		used = 0
	} else if err != nil {
		return AppToolCallUsage{}, fmt.Errorf("load app tool call usage: %w", err)
	}
	out := AppToolCallUsage{
		Action: policy.action, Used: &used, Limit: &policy.limit,
		PeriodStart: &policy.periodStart, PeriodEnd: &policy.periodEnd, ResetAt: &policy.resetAt,
	}
	if policy.action == entitlement.ActionEnforce {
		reached := used >= policy.limit
		out.Reached = &reached
	}
	return out, nil
}

// AppToolCallMemberUsage is one person's calls in the period: who started the
// run or approved the call. A zero UserID is runs no person started.
type AppToolCallMemberUsage struct {
	UserID pgtype.UUID
	Calls  int64
}

// MemberUsage breaks the period's counted calls down by person, from the
// call log rows Admit counted in the period.
func (q AppToolCallQuota) MemberUsage(ctx context.Context, workspaceID pgtype.UUID, periodStart time.Time) ([]AppToolCallMemberUsage, error) {
	rows, err := q.Queries.ListAppToolCallUsageByMember(ctx, db.ListAppToolCallUsageByMemberParams{
		WorkspaceID:        workspaceID,
		MeteredPeriodStart: pgtype.Timestamptz{Time: periodStart.UTC(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list app tool call usage by member: %w", err)
	}
	out := make([]AppToolCallMemberUsage, 0, len(rows))
	for _, row := range rows {
		out = append(out, AppToolCallMemberUsage{UserID: row.TriggeredBy, Calls: row.Calls})
	}
	return out, nil
}
