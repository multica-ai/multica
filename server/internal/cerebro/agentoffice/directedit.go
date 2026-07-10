package agentoffice

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// RecordDirectEdit snapshots the agent's live composite context into the
// append-only version history after a write that bypassed the change-request
// flow (today: the generic PUT /api/agents/{id} UpdateAgent endpoint). Without
// this, a direct edit mutates the agent while agent.context_version and the
// history keep pointing at the pre-edit state — exactly the untracked drift
// Agent Office exists to close (FIR-1775 Phase 2).
//
// The caller invokes it AFTER its own writes have committed; this method
// re-reads the live row inside its own transaction so the snapshot reflects
// the final state. When the live composite already matches the latest
// recorded version (the edit touched no versioned field, e.g. a rename),
// nothing is recorded and recorded=false is returned.
//
// Two concurrent direct edits can race to the same bumped version number; the
// UNIQUE(agent_id, version) constraint fails the loser, whose edit is then
// captured by the next successful record (the snapshot is always composed
// from the live row, never from the request). Callers treat errors as
// best-effort: the direct edit itself has already happened.
func (s *Service) RecordDirectEdit(ctx context.Context, agentID, editorID pgtype.UUID, description string) (version string, recorded bool, err error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Cerebro.WithTx(tx)

	agent, err := qtx.GetAgentContext(ctx, agentID)
	if err != nil {
		return "", false, fmt.Errorf("failed to load agent: %w", err)
	}
	skillIDs, err := qtx.ListAgentSkillIDsForContext(ctx, agentID)
	if err != nil {
		return "", false, fmt.Errorf("failed to load skill bindings: %w", err)
	}
	snapshot := EncodeSnapshot(ComposeCurrentSnapshot(agent, skillIDs))

	// Base the bump on whichever is ahead: the agent's version pointer or the
	// newest history row. They can disagree if the pointer ever drifted; taking
	// the max keeps new versions monotonic and the UNIQUE constraint satisfied.
	base := agent.ContextVersion
	latest, err := qtx.GetLatestAgentContextVersion(ctx, agentID)
	switch {
	case err == nil:
		if bytes.Equal(EncodeSnapshot(DecodeSnapshot(latest.Snapshot)), snapshot) {
			// Live state is already captured — the edit changed no versioned field.
			return agent.ContextVersion, false, nil
		}
		if SemverGT(latest.Version, base) {
			base = latest.Version
		}
	case errors.Is(err, pgx.ErrNoRows):
		// No history yet (agent created after the 9100 backfill) — first
		// direct edit seeds the history from the current pointer.
	default:
		return "", false, fmt.Errorf("failed to load latest version: %w", err)
	}

	newVersion := BumpPatch(base)
	if _, err := qtx.BumpAgentContextVersion(ctx, cerebrodb.BumpAgentContextVersionParams{
		ID:             agentID,
		ContextVersion: newVersion,
	}); err != nil {
		return "", false, fmt.Errorf("failed to bump version pointer: %w", err)
	}
	if _, err := qtx.CreateAgentContextVersion(ctx, cerebrodb.CreateAgentContextVersionParams{
		AgentID:     agentID,
		Version:     newVersion,
		Snapshot:    snapshot,
		Description: description,
		CreatedBy:   editorID,
	}); err != nil {
		return "", false, fmt.Errorf("failed to snapshot version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("failed to commit: %w", err)
	}
	return newVersion, true, nil
}
