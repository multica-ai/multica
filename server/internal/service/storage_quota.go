package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/util"
)

// Attachment storage limit (MUL-9489).
//
// Cloud sends the bytes a workspace may hold through the "storage_bytes"
// gate. Usage is not kept in a counter: it is the sum of the workspace's
// attachment rows, read on demand from a covering index. Cascade deletes are
// therefore reflected at once and nothing needs a backfill. The sum runs only
// while the gate is observe or enforce; off, which is also the self-hosted
// behavior, reads nothing.
//
// The check is a soft limit. It takes no lock, so uploads that run at the
// same moment the workspace crosses its limit all pass: the overshoot is at
// most what was in flight then. Reads, downloads, thumbnails and deletes are
// never checked, so a full workspace keeps every attachment it has.

// DefaultStorageSumTimeout bounds the usage sum on an upload. A sum that runs
// longer is abandoned and the upload admitted, so a slow query cannot hold
// uploads or exhaust the pool before failing open.
const DefaultStorageSumTimeout = 300 * time.Millisecond

// Storage quota sources and results are the bounded metric labels.
const (
	StorageSourceUpload   = "upload"
	StorageSourceSubIssue = "sub_issue"
	StorageSourceChannel  = "channel"
	StorageSourceUsage    = "usage"

	storageSumOK      = "ok"
	storageSumTimeout = "timeout"
	storageSumError   = "error"
)

// StorageQuotaMetrics accepts only bounded labels. Workspace identifiers and
// byte counts must never be metric labels.
type StorageQuotaMetrics interface {
	RecordStorageQuotaSum(source, result string, durationSeconds float64)
	RecordStorageQuotaDecision(action, source, result string)
}

// StorageUsageQuerier sums a workspace's attachment bytes.
type StorageUsageQuerier interface {
	SumWorkspaceAttachmentBytes(ctx context.Context, workspaceID pgtype.UUID) (int64, error)
}

// StorageLimitReachedError is returned when an enforced storage limit refuses
// new bytes. Used is what the workspace held when it was refused.
type StorageLimitReachedError struct {
	Used  int64
	Limit int64
}

func (e *StorageLimitReachedError) Error() string {
	return "workspace has reached its attachment storage limit"
}

// StorageQuota checks the storage_bytes gate. A zero value with no
// Entitlements admits everything without reading the database.
type StorageQuota struct {
	Sums         StorageUsageQuerier
	Entitlements entitlement.Provider
	Overages     EntitlementOverages
	Metrics      StorageQuotaMetrics
	// SumTimeout defaults to DefaultStorageSumTimeout.
	SumTimeout time.Duration
	now        func() time.Time
}

// StorageUsage is the storage limit as clients render it. Limit is set only
// while a limit applies; Used also needs the sum to have been read in time;
// Reached is set only under enforce, because observe never refuses an upload.
type StorageUsage struct {
	Action  entitlement.Action
	Used    *int64
	Limit   *int64
	Reached *bool
}

// Admit decides whether bytes more may be stored in the workspace. Under
// enforce it refuses with *StorageLimitReachedError when the workspace would
// go past its limit, after re-reading Cloud's policy so a workspace that has
// just upgraded is not refused on a cached one. Under observe it admits and
// records the attempt. Every failure to decide admits.
func (q StorageQuota) Admit(ctx context.Context, workspaceID pgtype.UUID, bytes int64, source string) error {
	decision, ok := q.gate(ctx, workspaceID)
	if !ok {
		return nil
	}
	used, ok := q.sum(ctx, workspaceID, source)
	if !ok {
		q.recordDecision(decision.Gate.Action, source, "fail_open")
		return nil
	}
	attempted := used + max(bytes, 0)
	if attempted <= gateLimit(decision) {
		q.recordDecision(decision.Gate.Action, source, "admitted")
		return nil
	}
	if decision.Gate.Action == entitlement.ActionEnforce {
		if refresher, isRefresher := q.Entitlements.(entitlement.Refresher); isRefresher {
			decision = refresher.RefreshGate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateStorageBytes)
			if !storageGateActive(decision) || attempted <= gateLimit(decision) {
				q.recordDecision(decision.Gate.Action, source, "admitted")
				return nil
			}
		}
	}
	q.recordOverage(ctx, workspaceID, decision, attempted)
	if decision.Gate.Action == entitlement.ActionEnforce {
		q.recordDecision(decision.Gate.Action, source, "rejected")
		return &StorageLimitReachedError{Used: used, Limit: gateLimit(decision)}
	}
	q.recordDecision(decision.Gate.Action, source, "observed")
	return nil
}

// RecordStored is for bytes already written without a check: files that
// arrive through a messaging channel are never refused, because the sender
// would lose them with no way to see why. It records the workspace as over
// its limit, as an admitted attempt, when the stored bytes put it there.
func (q StorageQuota) RecordStored(ctx context.Context, workspaceID pgtype.UUID, source string) {
	decision, ok := q.gate(ctx, workspaceID)
	if !ok {
		return
	}
	used, ok := q.sum(ctx, workspaceID, source)
	if !ok || used <= gateLimit(decision) {
		return
	}
	// The bytes were admitted whatever the action, so they count as observed.
	decision.Gate.Action = entitlement.ActionObserve
	q.recordOverage(ctx, workspaceID, decision, used)
	q.recordDecision(entitlement.ActionObserve, source, "observed")
}

// Usage reports the workspace's storage limit and use. Before reporting a
// full workspace it re-reads Cloud's policy, as Admit does, so a client is not
// offered an upgrade the workspace already has. A sum that fails or runs past
// its timeout still reports the limit, with Used and Reached nil, so a client
// can say usage is unavailable instead of hiding the limit.
func (q StorageQuota) Usage(ctx context.Context, workspaceID pgtype.UUID) StorageUsage {
	decision, ok := q.gate(ctx, workspaceID)
	if !ok {
		return StorageUsage{Action: entitlement.ActionOff}
	}
	used, err := q.usageSum(ctx, workspaceID)
	if err != nil {
		limit := gateLimit(decision)
		return StorageUsage{Action: decision.Gate.Action, Limit: &limit}
	}
	if decision.Gate.Action == entitlement.ActionEnforce && used >= gateLimit(decision) {
		if refresher, isRefresher := q.Entitlements.(entitlement.Refresher); isRefresher {
			decision = refresher.RefreshGate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateStorageBytes)
			if !storageGateActive(decision) {
				return StorageUsage{Action: entitlement.ActionOff}
			}
		}
	}
	limit := gateLimit(decision)
	out := StorageUsage{Action: decision.Gate.Action, Used: &used, Limit: &limit}
	if decision.Gate.Action == entitlement.ActionEnforce {
		reached := used >= limit
		out.Reached = &reached
	}
	return out
}

// usageSum is the billing page's read, bounded by the same timeout.
func (q StorageQuota) usageSum(ctx context.Context, workspaceID pgtype.UUID) (int64, error) {
	started := time.Now()
	used, result, err := q.timedSum(ctx, workspaceID)
	q.recordSum(StorageSourceUsage, result, time.Since(started).Seconds())
	return used, err
}

func (q StorageQuota) gate(ctx context.Context, workspaceID pgtype.UUID) (entitlement.Decision, bool) {
	if q.Entitlements == nil || q.Sums == nil || !workspaceID.Valid {
		return entitlement.Decision{}, false
	}
	decision := q.Entitlements.Gate(ctx, uuid.UUID(workspaceID.Bytes), entitlement.GateStorageBytes)
	return decision, storageGateActive(decision)
}

func storageGateActive(decision entitlement.Decision) bool {
	action := decision.Gate.Action
	return (action == entitlement.ActionObserve || action == entitlement.ActionEnforce) &&
		decision.Gate.Limit != nil && *decision.Gate.Limit >= 0
}

func gateLimit(decision entitlement.Decision) int64 { return int64(*decision.Gate.Limit) }

// sum reads the workspace's used bytes for an admission. ok=false means the
// sum timed out or failed and the caller must admit.
func (q StorageQuota) sum(ctx context.Context, workspaceID pgtype.UUID, source string) (int64, bool) {
	started := time.Now()
	used, result, err := q.timedSum(ctx, workspaceID)
	q.recordSum(source, result, time.Since(started).Seconds())
	if err != nil {
		slog.WarnContext(ctx, "storage usage sum failed; admitting",
			"workspace_id", util.UUIDToString(workspaceID), "source", source, "result", result, "error", err)
		return 0, false
	}
	return used, true
}

func (q StorageQuota) timedSum(ctx context.Context, workspaceID pgtype.UUID) (int64, string, error) {
	timeout := q.SumTimeout
	if timeout <= 0 {
		timeout = DefaultStorageSumTimeout
	}
	sumCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	used, err := q.Sums.SumWorkspaceAttachmentBytes(sumCtx, workspaceID)
	switch {
	case err == nil:
		return used, storageSumOK, nil
	case ctx.Err() == nil && errors.Is(sumCtx.Err(), context.DeadlineExceeded):
		return 0, storageSumTimeout, err
	default:
		return 0, storageSumError, err
	}
}

func (q StorageQuota) recordOverage(ctx context.Context, workspaceID pgtype.UUID, decision entitlement.Decision, attempted int64) {
	if q.Overages.Queries == nil {
		return
	}
	now := time.Now
	if q.now != nil {
		now = q.now
	}
	start, end := StorageLimitPeriod(now())
	q.Overages.Record(ctx, LimitExceededAttempt{
		WorkspaceID:    workspaceID,
		Gate:           entitlement.GateStorageBytes,
		Action:         decision.Gate.Action,
		PeriodStart:    start,
		PeriodEnd:      end,
		Limit:          gateLimit(decision),
		AttemptedUsage: attempted,
	})
}

// StorageLimitPeriod groups storage over-limit records by UTC calendar month.
// The gate has no period; the month only buckets the records, so how many
// workspaces reach the limit reads month by month like the other limits.
func StorageLimitPeriod(now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func (q StorageQuota) recordSum(source, result string, seconds float64) {
	if q.Metrics != nil {
		q.Metrics.RecordStorageQuotaSum(source, result, seconds)
	}
}

func (q StorageQuota) recordDecision(action entitlement.Action, source, result string) {
	if q.Metrics != nil {
		q.Metrics.RecordStorageQuotaDecision(string(action), source, result)
	}
}
