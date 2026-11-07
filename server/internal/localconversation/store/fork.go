package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// ForkCopy creates a conversation from another's history.
type ForkCopy struct {
	SourceID string
	ID       string
	Title    string
	// ThroughTurnID ends the copy after that turn; empty copies everything.
	ThroughTurnID string
	// ProviderSessionID is the provider's forked native session.
	ProviderSessionID string
	// Receipt makes the fork request idempotent; it commits with the copy.
	Receipt   *CommandReceipt
	CreatedAt time.Time
}

// ForkConversation copies a conversation's settled history into a new
// conversation in one transaction. Turn and message ids are prefixed with
// the new conversation id so they stay unique; events are renumbered from
// one. The copy is idle, unpinned and unarchived, with no queue.
func (s *Store) ForkConversation(ctx context.Context, f ForkCopy) (int64, error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	src, err := q.GetConversation(ctx, f.SourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: conversation %s", ErrNotFound, f.SourceID)
	}
	if err != nil {
		return 0, err
	}
	maxIndex, boundarySeq := src.TurnCount-1, src.LastSeq
	if f.ThroughTurnID != "" {
		turn, err := q.GetTurn(ctx, f.ThroughTurnID)
		if err != nil || turn.ConversationID != src.ID {
			return 0, fmt.Errorf("%w: turn %s", ErrNotFound, f.ThroughTurnID)
		}
		maxIndex = turn.TurnIndex
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM event WHERE conversation_id = ? AND turn_id = ?`, src.ID, turn.ID).Scan(&boundarySeq); err != nil {
			return 0, err
		}
	}
	scopeSeq, err := q.BumpScopeSeq(ctx, src.ScopeID)
	if err != nil {
		return 0, err
	}
	now := formatTime(f.CreatedAt)
	err = q.InsertConversation(ctx, sqlitedb.InsertConversationParams{
		ID: f.ID, ScopeID: src.ScopeID, Provider: src.Provider, ProviderSessionID: f.ProviderSessionID,
		Title: f.Title, TitleMode: "manual", Cwd: src.Cwd, Model: src.Model, ReasoningEffort: src.ReasoningEffort,
		PermissionMode: src.PermissionMode, ProjectID: src.ProjectID, ForkedFromID: src.ID, ForkedFromTurnID: f.ThroughTurnID,
		SummarySeq: scopeSeq, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return 0, err
	}
	prefix := f.ID + "~"
	retained := `SELECT id FROM turn WHERE conversation_id = ? AND turn_index <= ?`
	steps := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO turn (id, conversation_id, turn_index, request_id, provider_turn_id, status, error_kind, error, reconciled, last_turn_seq, started_at, completed_at)
			SELECT ? || id, ?, turn_index, '', provider_turn_id, status, error_kind, error, reconciled, last_turn_seq, started_at, completed_at
			FROM turn WHERE conversation_id = ? AND turn_index <= ?`, []any{prefix, f.ID, src.ID, maxIndex}},
		{`INSERT INTO message (id, conversation_id, turn_id, role, content, attachment_ids, steered, failed, created_at)
			SELECT ? || id, ?, ? || turn_id, role, content, attachment_ids, steered, failed, created_at
			FROM message WHERE conversation_id = ? AND turn_id IN (` + retained + `)`, []any{prefix, f.ID, prefix, src.ID, src.ID, maxIndex}},
		{`INSERT INTO event (conversation_id, seq, turn_id, turn_seq, item_id, type, final, payload, created_at)
			SELECT ?, ROW_NUMBER() OVER (ORDER BY seq), CASE WHEN turn_id IS NULL THEN NULL ELSE ? || turn_id END, turn_seq, item_id, type, final, payload, created_at
			FROM event WHERE conversation_id = ? AND seq <= ? AND (turn_id IS NULL OR turn_id IN (` + retained + `))`, []any{f.ID, prefix, src.ID, boundarySeq, src.ID, maxIndex}},
		{`UPDATE conversation SET
			last_seq = (SELECT COUNT(*) FROM event WHERE conversation_id = ?1),
			turn_count = (SELECT COUNT(*) FROM turn WHERE conversation_id = ?1),
			last_message_preview = ?2
			WHERE id = ?1`, []any{f.ID, src.LastMessagePreview}},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return 0, err
		}
	}
	if f.Receipt != nil {
		if err := insertReceipt(ctx, q, *f.Receipt); err != nil {
			return 0, err
		}
	}
	if err := q.EstablishCompletionBaseline(ctx, f.ID); err != nil {
		return 0, err
	}
	return scopeSeq, tx.Commit()
}
