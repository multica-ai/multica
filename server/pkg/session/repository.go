package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionNotFound is returned when no matching session exists.
var ErrSessionNotFound = errors.New("session not found")

// ErrVersionConflict is returned when an optimistic concurrency check fails.
var ErrVersionConflict = errors.New("session version conflict")

// Repository handles persistence for agent sessions.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new session repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new session row and returns it.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	query := `
		INSERT INTO agent_sessions (
			id, issue_id, agent_id, run_number, state,
			conversation_summary, working_directory, branch,
			files_modified, is_active, expires_at, version
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11, $12
		)
		RETURNING created_at, last_active_at`

	err := r.pool.QueryRow(ctx, query,
		s.ID, s.IssueID, s.AgentID, s.RunNumber, s.State,
		s.ConversationSummary, s.WorkingDirectory, s.Branch,
		s.FilesModified, s.IsActive, s.ExpiresAt, s.Version,
	).Scan(&s.CreatedAt, &s.LastActiveAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetActiveSession returns the currently active session for an issue+agent pair.
// Uses SELECT ... FOR UPDATE to prevent concurrent modification.
func (r *Repository) GetActiveSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	return r.getActiveSession(ctx, issueID, agentID, true)
}

// GetActiveSessionNoLock is the read-only variant without FOR UPDATE.
func (r *Repository) GetActiveSessionNoLock(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	return r.getActiveSession(ctx, issueID, agentID, false)
}

func (r *Repository) getActiveSession(ctx context.Context, issueID, agentID uuid.UUID, forUpdate bool) (*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state,
			conversation_summary, working_directory, branch,
			files_modified, is_active, created_at, last_active_at,
			expires_at, version
		FROM agent_sessions
		WHERE issue_id = $1 AND agent_id = $2 AND is_active = true
		ORDER BY run_number DESC
		LIMIT 1`
	if forUpdate {
		query += "\n\t\tFOR UPDATE"
	}

	s := &Session{}
	err := r.pool.QueryRow(ctx, query, issueID, agentID).Scan(
		&s.ID, &s.IssueID, &s.AgentID, &s.RunNumber, &s.State,
		&s.ConversationSummary, &s.WorkingDirectory, &s.Branch,
		&s.FilesModified, &s.IsActive, &s.CreatedAt, &s.LastActiveAt,
		&s.ExpiresAt, &s.Version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active session: %w", err)
	}
	return s, nil
}

// UpdateState atomically updates the session state with optimistic concurrency control.
func (r *Repository) UpdateState(ctx context.Context, sessionID uuid.UUID, state json.RawMessage, summary *string, filesModified []string, workingDir *string, branch *string, expectedVersion int) error {
	query := `
		UPDATE agent_sessions
		SET state = COALESCE($2, state),
			conversation_summary = $3,
			files_modified = $4,
			working_directory = $5,
			branch = $6,
			last_active_at = now(),
			version = version + 1
		WHERE id = $1 AND version = $7`

	tag, err := r.pool.Exec(ctx, query,
		sessionID, state, summary, filesModified, workingDir, branch, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("update session state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionConflict
	}
	return nil
}

// Deactivate sets is_active = false for the given session.
func (r *Repository) Deactivate(ctx context.Context, sessionID uuid.UUID) error {
	query := `
		UPDATE agent_sessions
		SET is_active = false, last_active_at = now()
		WHERE id = $1 AND is_active = true`

	tag, err := r.pool.Exec(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("deactivate session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// DeactivateByIssueAndAgent deactivates all active sessions for a given issue+agent pair.
func (r *Repository) DeactivateByIssueAndAgent(ctx context.Context, issueID, agentID uuid.UUID) error {
	query := `
		UPDATE agent_sessions
		SET is_active = false, last_active_at = now()
		WHERE issue_id = $1 AND agent_id = $2 AND is_active = true`

	_, err := r.pool.Exec(ctx, query, issueID, agentID)
	if err != nil {
		return fmt.Errorf("deactivate sessions by issue/agent: %w", err)
	}
	return nil
}

// ExpireBefore marks all active sessions with last_active_at before the cutoff as inactive.
func (r *Repository) ExpireBefore(ctx context.Context, cutoff time.Time) ([]uuid.UUID, error) {
	query := `
		UPDATE agent_sessions
		SET is_active = false, expires_at = now()
		WHERE is_active = true AND last_active_at < $1
		RETURNING id`

	rows, err := r.pool.Query(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("expire sessions: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan expired id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetLatestRunNumber returns the highest run_number for a given issue+agent pair.
func (r *Repository) GetLatestRunNumber(ctx context.Context, issueID, agentID uuid.UUID) (int, error) {
	query := `
		SELECT COALESCE(MAX(run_number), 0)::int
		FROM agent_sessions
		WHERE issue_id = $1 AND agent_id = $2`

	var runNumber int
	err := r.pool.QueryRow(ctx, query, issueID, agentID).Scan(&runNumber)
	if err != nil {
		return 0, fmt.Errorf("get latest run number: %w", err)
	}
	return runNumber, nil
}

// pgtype helpers for NULL-safe scanning.
func toUUIDPtr(v pgtype.UUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	u := uuid.UUID(v.Bytes)
	return &u
}

func toTimePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}

func toStringPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

// ListByIssue returns all sessions for a given issue, ordered by agent_id, run_number DESC.
func (r *Repository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state,
			conversation_summary, working_directory, branch,
			files_modified, is_active, created_at, last_active_at,
			expires_at, version
		FROM agent_sessions
		WHERE issue_id = $1
		ORDER BY agent_id, run_number DESC`

	rows, err := r.pool.Query(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("list sessions by issue: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		if err := rows.Scan(
			&s.ID, &s.IssueID, &s.AgentID, &s.RunNumber, &s.State,
			&s.ConversationSummary, &s.WorkingDirectory, &s.Branch,
			&s.FilesModified, &s.IsActive, &s.CreatedAt, &s.LastActiveAt,
			&s.ExpiresAt, &s.Version,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// GetByID returns a single session by its primary key.
func (r *Repository) GetByID(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state,
			conversation_summary, working_directory, branch,
			files_modified, is_active, created_at, last_active_at,
			expires_at, version
		FROM agent_sessions
		WHERE id = $1`

	s := &Session{}
	err := r.pool.QueryRow(ctx, query, sessionID).Scan(
		&s.ID, &s.IssueID, &s.AgentID, &s.RunNumber, &s.State,
		&s.ConversationSummary, &s.WorkingDirectory, &s.Branch,
		&s.FilesModified, &s.IsActive, &s.CreatedAt, &s.LastActiveAt,
		&s.ExpiresAt, &s.Version,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("get session by id: %w", err)
	}
	return s, nil
}
