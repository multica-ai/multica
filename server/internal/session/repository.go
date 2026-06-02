package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionNotFound is returned when a session query returns no rows.
var ErrSessionNotFound = errors.New("session not found")

// ErrVersionConflict is returned when an optimistic lock fails.
var ErrVersionConflict = errors.New("version conflict: session was modified concurrently")

// Repository provides DB access for agent sessions.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new Repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new session.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	query := `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, conversation_summary,
			working_directory, branch, files_modified, is_active, created_at, last_active_at, expires_at, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := r.pool.Exec(ctx, query,
		s.ID, s.IssueID, s.AgentID, s.RunNumber, s.State,
		s.ConversationSummary, s.WorkingDirectory, s.Branch,
		s.FilesModified, s.IsActive, s.CreatedAt, s.LastActiveAt,
		s.ExpiresAt, s.Version,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("duplicate session: issue_id=%s agent_id=%s run_number=%d: %w",
				s.IssueID, s.AgentID, s.RunNumber, err)
		}
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetActiveSession returns the active session for a given issue+agent combination.
func (r *Repository) GetActiveSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state, conversation_summary,
			working_directory, branch, files_modified, is_active, created_at,
			last_active_at, expires_at, version
		FROM agent_sessions
		WHERE issue_id = $1 AND agent_id = $2 AND is_active = true
		ORDER BY run_number DESC
		LIMIT 1
	`
	return r.scanSession(ctx, query, issueID, agentID)
}

// GetByID retrieves a session by its primary key.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state, conversation_summary,
			working_directory, branch, files_modified, is_active, created_at,
			last_active_at, expires_at, version
		FROM agent_sessions
		WHERE id = $1
	`
	return r.scanSession(ctx, query, id)
}

// UpdateState performs an optimistic-locked update of session state.
func (r *Repository) UpdateState(ctx context.Context, id uuid.UUID, state json.RawMessage, expectedVersion int) (int, error) {
	query := `
		UPDATE agent_sessions
		SET state = $1, version = version + 1
		WHERE id = $2 AND version = $3
		RETURNING version
	`
	var newVersion int
	err := r.pool.QueryRow(ctx, query, state, id, expectedVersion).Scan(&newVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrVersionConflict
		}
		return 0, fmt.Errorf("update session state: %w", err)
	}
	return newVersion, nil
}

// Deactivate sets is_active=false for all sessions of the given issue+agent.
func (r *Repository) Deactivate(ctx context.Context, issueID, agentID uuid.UUID) error {
	query := `
		UPDATE agent_sessions
		SET is_active = false
		WHERE issue_id = $1 AND agent_id = $2 AND is_active = true
	`
	_, err := r.pool.Exec(ctx, query, issueID, agentID)
	if err != nil {
		return fmt.Errorf("deactivate sessions: %w", err)
	}
	return nil
}

// CleanupExpired deletes sessions past their expiry time.
func (r *Repository) CleanupExpired(ctx context.Context, before time.Time) (int64, error) {
	query := `
		DELETE FROM agent_sessions
		WHERE expires_at IS NOT NULL AND expires_at < $1
	`
	tag, err := r.pool.Exec(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// NextRunNumber returns the next run_number for an issue+agent pair.
func (r *Repository) NextRunNumber(ctx context.Context, issueID, agentID uuid.UUID) (int, error) {
	query := `
		SELECT COALESCE(MAX(run_number), 0) + 1
		FROM agent_sessions
		WHERE issue_id = $1 AND agent_id = $2
	`
	var next int
	err := r.pool.QueryRow(ctx, query, issueID, agentID).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("get next run number: %w", err)
	}
	return next, nil
}

// ListByIssue returns all sessions for a given issue, ordered by run_number descending.
func (r *Repository) ListByIssue(ctx context.Context, issueID uuid.UUID) ([]*Session, error) {
	query := `
		SELECT id, issue_id, agent_id, run_number, state, conversation_summary,
			working_directory, branch, files_modified, is_active, created_at,
			last_active_at, expires_at, version
		FROM agent_sessions
		WHERE issue_id = $1
		ORDER BY run_number DESC
	`
	rows, err := r.pool.Query(ctx, query, issueID)
	if err != nil {
		return nil, fmt.Errorf("list sessions by issue: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// scanSession scans a single session row.
func (r *Repository) scanSession(ctx context.Context, query string, args ...interface{}) (*Session, error) {
	row := r.pool.QueryRow(ctx, query, args...)
	s, err := scanRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanRow(row scanner) (*Session, error) {
	s := &Session{}
	err := row.Scan(
		&s.ID, &s.IssueID, &s.AgentID, &s.RunNumber, &s.State,
		&s.ConversationSummary, &s.WorkingDirectory, &s.Branch,
		&s.FilesModified, &s.IsActive, &s.CreatedAt,
		&s.LastActiveAt, &s.ExpiresAt, &s.Version,
	)
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return s, nil
}
