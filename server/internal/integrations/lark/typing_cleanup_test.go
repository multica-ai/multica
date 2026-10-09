package lark

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *fakeTypingQueries) RegisterChannelTypingReaction(_ context.Context, p db.RegisterChannelTypingReactionParams) (db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	if f.ledger == nil {
		f.ledger = make(map[pgtype.UUID]db.ChannelTypingReaction)
	}
	r := db.ChannelTypingReaction{ID: p.ID, WorkspaceID: p.WorkspaceID, ChatSessionID: p.ChatSessionID, ChatMessageID: p.ChatMessageID, InstallationID: p.InstallationID, ChannelMessageID: p.ChannelMessageID, InstallationSnapshot: p.InstallationSnapshot}
	f.ledger[p.ID] = r
	return r, nil
}
func (f *fakeTypingQueries) FinishChannelTypingReactionAdd(_ context.Context, p db.FinishChannelTypingReactionAddParams) (db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	r, ok := f.ledger[p.ID]
	if !ok {
		return r, pgx.ErrNoRows
	}
	r.ReactionID = p.ReactionID
	r.AddFinished = true
	r.CleanupRequired = r.CleanupRequired || p.CleanupRequired
	r.CleanedAt = pgtype.Timestamptz{}
	f.ledger[p.ID] = r
	return r, nil
}
func (f *fakeTypingQueries) AcknowledgeChannelTypingReactionCleanup(_ context.Context, p db.AcknowledgeChannelTypingReactionCleanupParams) error {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	r := f.ledger[p.ID]
	if r.AddFinished && r.ReactionID != "" && r.ReactionID == p.ReactionID {
		r.CleanedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		f.ledger[p.ID] = r
	}
	return nil
}

// Existing event unit fixtures supply terminal events directly rather than
// mutating DB tasks. Real eligibility is covered by the DB regression matrix.
func (f *fakeTypingQueries) ClaimChannelTypingReactionCleanup(_ context.Context, session pgtype.UUID) ([]db.ChannelTypingReaction, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	var rows []db.ChannelTypingReaction
	for id, r := range f.ledger {
		if !r.CleanedAt.Valid && (!session.Valid || r.ChatSessionID == session) {
			r.CleanupRequired = true
			f.ledger[id] = r
			rows = append(rows, r)
		}
	}
	return rows, nil
}
func (f *fakeTypingQueries) SettleChannelTypingInputs(context.Context, db.SettleChannelTypingInputsParams) error {
	return nil
}
func (f *fakeTypingQueries) SkipChannelTypingReactionAdd(context.Context, pgtype.UUID) error {
	return nil
}
func (f *fakeTypingQueries) PruneChannelTypingReactionCleanup(context.Context) error { return nil }

func (f *fakeTypingQueries) ListRequestedChannelTypingReactions(_ context.Context, ids []pgtype.UUID) ([]pgtype.UUID, error) {
	f.ledgerMu.Lock()
	defer f.ledgerMu.Unlock()
	var closed []pgtype.UUID
	for _, id := range ids {
		if f.ledger[id].CleanupRequired {
			closed = append(closed, id)
		}
	}
	return closed, nil
}
