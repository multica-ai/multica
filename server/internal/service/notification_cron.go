package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// RedisLocker is a minimal interface for acquiring distributed locks.
// Implementations should use Redis SET NX EX.
type RedisLocker interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key string) error
}

// BlockedTimeoutChecker periodically scans for issues that have been blocked
// for longer than the threshold and have not yet been notified (idempotent via
// issue metadata key `last_blocked_notify_at`).
//
// Design (OXY-583 实现注意事项 #3):
//   - Uses Go time.Ticker, not pg_cron
//   - Redis distributed lock (SET NX EX 300) for multi-instance coordination
//   - Threshold: 4 hours; cooldown: 24 hours per issue
//   - Falls back to single-instance mode when Redis is unavailable
type BlockedTimeoutChecker struct {
	DB        DBTX
	Bus       *events.Bus
	Locker    RedisLocker // nil when Redis unavailable (single-instance mode)
	Interval  time.Duration
	Threshold time.Duration
	Cooldown  time.Duration

	stopCh chan struct{}
	doneCh chan struct{}
}

// DBTX is the minimal database interface for the blocked timeout checker.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

const (
	defaultBlockedCheckInterval = 5 * time.Minute
	defaultBlockedThreshold     = 4 * time.Hour
	defaultBlockedCooldown      = 24 * time.Hour
	blockedTimeoutLockKey       = "cron:blocked_timeout_check:lock"
	blockedTimeoutLockTTL       = 300 * time.Second
	blockedMetadataNotifyKey    = "last_blocked_notify_at"
)

// NewBlockedTimeoutChecker creates a blocked timeout checker with sensible defaults.
func NewBlockedTimeoutChecker(db DBTX, bus *events.Bus, locker RedisLocker) *BlockedTimeoutChecker {
	return &BlockedTimeoutChecker{
		DB:        db,
		Bus:       bus,
		Locker:    locker,
		Interval:  defaultBlockedCheckInterval,
		Threshold: defaultBlockedThreshold,
		Cooldown:  defaultBlockedCooldown,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the periodic check loop in a background goroutine.
func (c *BlockedTimeoutChecker) Start(ctx context.Context) {
	go c.run(ctx)
}

// Stop signals the checker to stop and waits for the goroutine to exit.
func (c *BlockedTimeoutChecker) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *BlockedTimeoutChecker) run(ctx context.Context) {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()

	// Run once immediately at startup.
	c.check(ctx)

	for {
		select {
		case <-ticker.C:
			c.check(ctx)
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *BlockedTimeoutChecker) check(ctx context.Context) {
	// Try to acquire the distributed lock.
	if c.Locker != nil {
		acquired, err := c.Locker.TryLock(ctx, blockedTimeoutLockKey, blockedTimeoutLockTTL)
		if err != nil {
			slog.Warn("blocked timeout check: failed to acquire Redis lock, falling back to single-instance mode",
				"error", err)
		} else if !acquired {
			slog.Debug("blocked timeout check: another instance is running, skipping")
			return
		}
		defer func() {
			if err := c.Locker.Unlock(ctx, blockedTimeoutLockKey); err != nil {
				slog.Warn("blocked timeout check: failed to release Redis lock", "error", err)
			}
		}()
	}

	cutoff := time.Now().UTC().Add(-c.Threshold)

	// Query all blocked issues updated before the cutoff.
	rows, err := c.DB.Query(ctx,
		`SELECT id, workspace_id, title, metadata, updated_at
		 FROM issue
		 WHERE status = 'blocked'
		   AND updated_at < $1
		   AND deleted_at IS NULL
		 ORDER BY updated_at ASC
		 LIMIT 500`,
		cutoff,
	)
	if err != nil {
		slog.Warn("blocked timeout check: query failed", "error", err)
		return
	}
	defer rows.Close()

	type blockedRow struct {
		ID          pgtype.UUID
		WorkspaceID pgtype.UUID
		Title       string
		Metadata    []byte
		UpdatedAt   pgtype.Timestamptz
	}

	var issues []blockedRow
	for rows.Next() {
		var r blockedRow
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.Title, &r.Metadata, &r.UpdatedAt); err != nil {
			slog.Warn("blocked timeout check: scan failed", "error", err)
			continue
		}
		issues = append(issues, r)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("blocked timeout check: rows error", "error", err)
		return
	}

	for _, issue := range issues {
		// Check cooldown via metadata.
		meta := parseMetadataMap(issue.Metadata)
		if lastNotifyStr, ok := meta[blockedMetadataNotifyKey].(string); ok {
			if lastNotify, err := time.Parse(time.RFC3339, lastNotifyStr); err == nil {
				if time.Since(lastNotify) < c.Cooldown {
					continue
				}
			}
		}

		issueID := util.UUIDToString(issue.ID)
		workspaceID := util.UUIDToString(issue.WorkspaceID)

		// Update metadata to record this notification (atomic set via JSONB merge).
		meta[blockedMetadataNotifyKey] = time.Now().UTC().Format(time.RFC3339)
		newMeta, err := json.Marshal(meta)
		if err != nil {
			slog.Warn("blocked timeout check: failed to marshal metadata", "issue_id", issueID, "error", err)
			continue
		}

		_, err = c.DB.Exec(ctx,
			`UPDATE issue
			 SET metadata = metadata || $1::jsonb,
			     updated_at = now()
			 WHERE id = $2`,
			newMeta, issue.ID,
		)
		if err != nil {
			slog.Warn("blocked timeout check: failed to update issue metadata", "issue_id", issueID, "error", err)
			continue
		}

		// Extract blocked_reason and waiting_on from metadata.
		blockedReason, _ := meta["blocked_reason"].(string)
		waitingOn, _ := meta["waiting_on"].(string)

		// Emit the notification event.
		c.Bus.Publish(events.Event{
			Type:        protocol.EventNotificationBlockedTimeout,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			ActorID:     "",
			Payload: map[string]any{
				"issue_id":       issueID,
				"issue_title":    issue.Title,
				"blocked_reason": blockedReason,
				"waiting_on":     waitingOn,
			},
		})

		slog.Info("blocked timeout notification emitted",
			"issue_id", issueID,
			"issue_title", issue.Title,
			"workspace_id", workspaceID)
	}
}

func parseMetadataMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}
