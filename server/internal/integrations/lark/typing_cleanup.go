package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type typingLedgerQueries interface {
	RegisterChannelTypingReaction(context.Context, db.RegisterChannelTypingReactionParams) (db.ChannelTypingReaction, error)
	FinishChannelTypingReactionAdd(context.Context, db.FinishChannelTypingReactionAddParams) (db.ChannelTypingReaction, error)
	AcknowledgeChannelTypingReactionCleanup(context.Context, db.AcknowledgeChannelTypingReactionCleanupParams) error
	ClaimChannelTypingReactionCleanup(context.Context, pgtype.UUID) ([]db.ChannelTypingReaction, error)
	SettleChannelTypingInputs(context.Context, db.SettleChannelTypingInputsParams) error
	SkipChannelTypingReactionAdd(context.Context, pgtype.UUID) error
	PruneChannelTypingReactionCleanup(context.Context) error
	ExpireChannelTypingReactionCleanup(context.Context) ([]db.ExpireChannelTypingReactionCleanupRow, error)
	ListRequestedChannelTypingReactions(context.Context, []pgtype.UUID) ([]pgtype.UUID, error)
}

// Settle persists the failed flush's input boundary before removing reactions.
// It covers inputs whose detached Add has not even registered yet.
func (m *TypingIndicatorManager) Settle(ctx context.Context, sessionID pgtype.UUID, scope engine.TypingSettlement) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), typingCleanupTimeout)
	defer cancel()
	if err := m.queries.SettleChannelTypingInputs(ctx, db.SettleChannelTypingInputsParams{
		ThroughMessageID: scope.ThroughMessageID, ChatSessionID: sessionID,
		WorkspaceID: scope.WorkspaceID, InstallationID: scope.InstallationID, ContextRevision: pgtype.Int8{Int64: scope.ContextRevision, Valid: true},
	}); err != nil {
		m.log.Warn("lark typing: persist taskless settlement", "err", err)
		return
	}
	m.Reconcile(ctx, sessionID)
}

// Reconcile claims all terminal inputs, not just the task's delivery trigger.
// A null session selects due work across all workspaces for the system worker.
// The ledger survives source deletion; eligibility is based on committed rows,
// so rolled-back cancellation/deletion never grants cleanup ownership.
func (m *TypingIndicatorManager) Reconcile(ctx context.Context, sessionID pgtype.UUID) map[string]bool {
	defer m.pruneLocal(ctx)
	covered := make(map[string]bool)
	rows, err := m.queries.ClaimChannelTypingReactionCleanup(ctx, sessionID)
	if err != nil {
		m.log.Warn("lark typing: claim cleanup", "err", err)
		return covered
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		} // Claimed but unfinished work remains due after its lease.
		covered[uuidString(row.InstallationID)+"/"+row.ChannelMessageID] = true
		m.mu.Lock()
		key := uuidString(row.ChatSessionID)
		for _, state := range append([]*TypingIndicatorState(nil), m.states[key]...) {
			if state.LedgerID == row.ID {
				state.ended = true
				m.removeState(key, state)
			}
		}
		m.mu.Unlock()
		callCtx, cancel := context.WithTimeout(ctx, typingCleanupTimeout)
		m.cleanupReaction(callCtx, row)
		cancel()
	}
	return covered
}

func (m *TypingIndicatorManager) cleanupReaction(ctx context.Context, row db.ChannelTypingReaction) {
	var snapshot Installation
	if err := json.Unmarshal(row.InstallationSnapshot, &snapshot); err != nil {
		m.log.Warn("lark typing: read cleanup snapshot", "err", err)
		return
	}
	creds := m.credentialsForInstallation(ctx, uuidString(row.ChatSessionID), row.InstallationID, snapshot)
	if creds == nil {
		return
	}
	var err error
	if row.ReactionID != "" {
		err = m.client.DeleteMessageReaction(ctx, DeleteReactionParams{InstallationID: *creds, MessageID: row.ChannelMessageID, ReactionID: row.ReactionID})
		// A different replica may already have deleted this exact reaction. Prove
		// absence rather than retrying forever on the API's already-missing error.
		if err != nil {
			if lister, ok := m.client.(ReactionLister); ok {
				reactions, listErr := lister.ListMessageReactions(ctx, ListMessageReactionsParams{InstallationID: *creds, MessageID: row.ChannelMessageID, EmojiType: typingEmoji})
				if listErr == nil {
					found := false
					for _, r := range reactions {
						if r.ReactionID == row.ReactionID {
							found = true
						}
					}
					if !found {
						err = nil
					}
				}
			}
		}
	} else {
		err = m.sweepForCleanup(ctx, *creds, row.ChannelMessageID)
	}
	if err != nil {
		m.log.Warn("lark typing: cleanup retained for retry", "cleanup_id", uuidString(row.ID), "attempt", row.Attempts, "err", err)
		return
	}
	if err = m.queries.AcknowledgeChannelTypingReactionCleanup(ctx, db.AcknowledgeChannelTypingReactionCleanupParams{ID: row.ID, ReactionID: row.ReactionID}); err != nil {
		m.log.Warn("lark typing: cleanup acknowledgement failed; retry retained", "cleanup_id", uuidString(row.ID), "err", err)
	}
}

func (m *TypingIndicatorManager) sweepForCleanup(ctx context.Context, creds InstallationCredentials, messageID string) error {
	if creds.AppID == "" {
		return fmt.Errorf("reaction owner App ID unavailable")
	}
	lister, ok := m.client.(ReactionLister)
	if !ok {
		return fmt.Errorf("reaction listing unavailable")
	}
	reactions, err := lister.ListMessageReactions(ctx, ListMessageReactionsParams{InstallationID: creds, MessageID: messageID, EmojiType: typingEmoji})
	if err != nil {
		return err
	}
	for _, r := range reactions {
		if r.OperatorType != "app" || r.OperatorID != creds.AppID || r.EmojiType != typingEmoji {
			continue
		}
		err = errors.Join(err, m.client.DeleteMessageReaction(ctx, DeleteReactionParams{InstallationID: creds, MessageID: messageID, ReactionID: r.ReactionID}))
	}
	return err
}

// Run retries durable cleanup after transient errors, lost bus events and
// process restarts. Claims use a 30s-1h capped backoff and SKIP LOCKED; each
// pass is bounded. Expiration and GC have a separate budget, so slow remote
// calls cannot indefinitely retain credential snapshots or terminal records.
func (m *TypingIndicatorManager) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		m.maintainCleanup(ctx)
		passCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		m.Reconcile(passCtx, pgtype.UUID{})
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *TypingIndicatorManager) maintainCleanup(ctx context.Context) {
	expireCtx, cancel := context.WithTimeout(ctx, typingCleanupTimeout)
	rows, err := m.queries.ExpireChannelTypingReactionCleanup(expireCtx)
	cancel()
	if err != nil && ctx.Err() == nil {
		m.log.Error("lark typing: credential expiry maintenance failed", "err", err)
	}
	counts := make(map[pgtype.UUID]int)
	for _, row := range rows {
		counts[row.WorkspaceID]++
	}
	for workspaceID, count := range counts {
		m.log.Warn("lark typing: cleanup abandoned at retention deadline; credentials erased", "workspace_id", uuidString(workspaceID), "count", count)
	}
	pruneCtx, pruneCancel := context.WithTimeout(ctx, typingCleanupTimeout)
	defer pruneCancel()
	if err := m.queries.PruneChannelTypingReactionCleanup(pruneCtx); err != nil && ctx.Err() == nil {
		m.log.Warn("lark typing: prune terminal cleanup", "err", err)
	}
}

// A different replica can acknowledge cleanup without touching our local map.
// Evict those stale views even when no remote retry is left to claim.
func (m *TypingIndicatorManager) pruneLocal(ctx context.Context) {
	m.mu.RLock()
	var ids []pgtype.UUID
	for _, states := range m.states {
		for _, state := range states {
			ids = append(ids, state.LedgerID)
		}
	}
	m.mu.RUnlock()
	if len(ids) == 0 {
		return
	}
	ended, err := m.queries.ListRequestedChannelTypingReactions(ctx, ids)
	if err != nil {
		m.log.Warn("lark typing: refresh local cleanup state", "err", err)
		return
	}
	closed := make(map[pgtype.UUID]bool, len(ended))
	for _, id := range ended {
		closed[id] = true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, states := range m.states {
		for _, state := range append([]*TypingIndicatorState(nil), states...) {
			if closed[state.LedgerID] {
				state.ended = true
				m.removeState(key, state)
			}
		}
	}
}
