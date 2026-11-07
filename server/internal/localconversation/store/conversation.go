package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// ErrNotFound reports a missing conversation or turn.
var ErrNotFound = errors.New("local conversation store: not found")

// Scope identifies one API origin, account and workspace.
type Scope struct {
	APIOrigin   string
	UserID      string
	WorkspaceID string
}

// EnsureScope returns the id of scope, creating it on first use.
func (s *Store) EnsureScope(ctx context.Context, scope Scope) (int64, error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	id, err := q.GetScopeID(ctx, sqlitedb.GetScopeIDParams{ApiOrigin: scope.APIOrigin, UserID: scope.UserID, WorkspaceID: scope.WorkspaceID})
	if errors.Is(err, sql.ErrNoRows) {
		id, err = q.InsertScope(ctx, sqlitedb.InsertScopeParams{ApiOrigin: scope.APIOrigin, UserID: scope.UserID, WorkspaceID: scope.WorkspaceID})
	}
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// NewConversation describes a conversation to create.
type NewConversation struct {
	ID                string
	ScopeID           int64
	Provider          string
	ProviderSessionID string
	Title             string
	// TitleMode is "auto" (generated titles may replace it) or "manual".
	TitleMode       string
	Cwd             string
	Model           *string
	ReasoningEffort *string
	PermissionMode  string
	ProjectID       *string
	SideSourceID    string
	// Side is the side conversation's source context, as JSON.
	Side                  string
	ForkedFromID          string
	ForkedFromTurnID      string
	ImportedFromSessionID string
	// Receipt, when set, commits with the conversation.
	Receipt   *CommandReceipt
	CreatedAt time.Time
}

// CreateConversation inserts a conversation and publishes it in the scope's
// summary stream. It returns the new scope sequence.
func (s *Store) CreateConversation(ctx context.Context, c NewConversation) (int64, error) {
	return s.ImportConversation(ctx, c, nil)
}

// ImportConversation creates a conversation with history in one
// transaction: the conversation, then each batch in order. It returns the
// scope sequence of the creation.
func (s *Store) ImportConversation(ctx context.Context, c NewConversation, history []Batch) (int64, error) {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	scopeSeq, err := insertConversation(ctx, q, c)
	if err != nil {
		return 0, err
	}
	for _, b := range history {
		b.ConversationID = c.ID
		if _, err := applyBatch(ctx, q, b); err != nil {
			return 0, err
		}
	}
	if err := q.EstablishCompletionBaseline(ctx, c.ID); err != nil {
		return 0, err
	}
	return scopeSeq, tx.Commit()
}

func insertConversation(ctx context.Context, q *sqlitedb.Queries, c NewConversation) (int64, error) {
	scopeSeq, err := q.BumpScopeSeq(ctx, c.ScopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	titleMode := c.TitleMode
	if titleMode == "" {
		titleMode = "auto"
	}
	permission := c.PermissionMode
	if permission == "" {
		permission = "default"
	}
	now := formatTime(c.CreatedAt)
	err = q.InsertConversation(ctx, sqlitedb.InsertConversationParams{
		ID:                    c.ID,
		ScopeID:               c.ScopeID,
		Provider:              c.Provider,
		ProviderSessionID:     c.ProviderSessionID,
		Title:                 c.Title,
		TitleMode:             titleMode,
		Cwd:                   c.Cwd,
		Model:                 nullString(c.Model),
		ReasoningEffort:       nullString(c.ReasoningEffort),
		PermissionMode:        permission,
		ProjectID:             nullString(c.ProjectID),
		SideSourceID:          c.SideSourceID,
		Side:                  c.Side,
		ForkedFromID:          c.ForkedFromID,
		ForkedFromTurnID:      c.ForkedFromTurnID,
		ImportedFromSessionID: c.ImportedFromSessionID,
		SummarySeq:            scopeSeq,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
	if err != nil {
		return 0, err
	}
	if c.Receipt != nil {
		if err := insertReceipt(ctx, q, *c.Receipt); err != nil {
			return 0, err
		}
	}
	return scopeSeq, nil
}

func nullString(v *string) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

// ScopeByID returns a scope's identity.
func (s *Store) ScopeByID(ctx context.Context, id int64) (Scope, error) {
	row, err := sqlitedb.New(s.read).GetScope(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Scope{}, ErrNotFound
	}
	return Scope{APIOrigin: row.ApiOrigin, UserID: row.UserID, WorkspaceID: row.WorkspaceID}, err
}

// ImportedConversation returns the scope's conversation that already
// holds a native session of provider, by its own or its source session.
func (s *Store) ImportedConversation(ctx context.Context, scopeID int64, provider, sessionID string) (sqlitedb.Conversation, error) {
	c, err := sqlitedb.New(s.read).FindImportedConversation(ctx, sqlitedb.FindImportedConversationParams{ScopeID: scopeID, Provider: provider, SessionID: sessionID})
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}
