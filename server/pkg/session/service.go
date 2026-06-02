package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxCreateRetries bounds the TOCTOU-retry loop in CreateSession so a
// pathological contention scenario cannot spin forever.
const maxCreateRetries = 5

// SessionService defines the contract for session lifecycle management.
type SessionService interface {
	// CreateSession creates a new session for the given issue+agent, incrementing run_number.
	CreateSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error)

	// GetActiveSession returns the currently active session for an issue+agent, or nil if none.
	GetActiveSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error)

	// UpdateSessionWithPayload persists state changes atomically with optimistic concurrency.
	UpdateSessionWithPayload(ctx context.Context, sessionID uuid.UUID, payload UpdatePayload) error

	// ExpireSession marks a specific session as inactive/expired.
	ExpireSession(ctx context.Context, sessionID uuid.UUID) error

	// ResetSession deactivates the current session so the next run starts fresh.
	ResetSession(ctx context.Context, issueID, agentID uuid.UUID) error

	// CleanupExpired marks all sessions older than the configured inactivity period as expired.
	CleanupExpired(ctx context.Context) (int, error)

	// ListSessions returns all sessions for a given issue, ordered by agent_id, run_number DESC.
	ListSessions(ctx context.Context, issueID uuid.UUID) ([]*Session, error)

	// GetSession returns a single session by ID.
	GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error)
}

// UpdatePayload holds the data to persist when updating a session.
type UpdatePayload struct {
	StateData       *StateData
	Summary         *string
	FilesModified   []string
	WorkingDir      *string
	Branch          *string
	ExpectedVersion int
}

// service implements SessionService.
type service struct {
	repo   *Repository
	config Config
	logger *slog.Logger
}

// NewService creates a new session lifecycle service.
func NewService(pool *pgxpool.Pool, cfg Config, logger *slog.Logger) SessionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &service{
		repo:   NewRepository(pool),
		config: cfg,
		logger: logger,
	}
}

// CreateSession creates a new session, incrementing run_number from the latest.
// Uses a retry loop to handle the TOCTOU race between GetLatestRunNumber and
// the INSERT when two concurrent requests read the same max run_number.
func (s *service) CreateSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	initialState := StateData{LastCheckpoint: time.Now().UTC()}
	stateBytes, err := json.Marshal(initialState)
	if err != nil {
		return nil, fmt.Errorf("marshal initial state: %w", err)
	}

	for attempt := 0; attempt < maxCreateRetries; attempt++ {
		latestRun, err := s.repo.GetLatestRunNumber(ctx, issueID, agentID)
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}

		now := time.Now().UTC()
		expiresAt := now.Add(s.config.InactivityExpiry)

		sess := &Session{
			ID:        uuid.New(),
			IssueID:   issueID,
			AgentID:   agentID,
			RunNumber: latestRun + 1,
			State:     stateBytes,
			IsActive:  true,
			ExpiresAt: &expiresAt,
			Version:   1,
		}

		if err := s.repo.Create(ctx, sess); err != nil {
			if isUniqueViolation(err) {
				s.logger.Debug("session create race, retrying",
					"issue_id", issueID,
					"agent_id", agentID,
					"run_number", sess.RunNumber,
					"attempt", attempt+1,
				)
				continue
			}
			return nil, err
		}

		s.logger.Info("session created",
			"session_id", sess.ID,
			"issue_id", issueID,
			"agent_id", agentID,
			"run_number", sess.RunNumber,
		)
		return sess, nil
	}
	return nil, fmt.Errorf("create session: exceeded %d retries due to run_number contention", maxCreateRetries)
}

// GetActiveSession returns the active session for an issue+agent, or nil.
func (s *service) GetActiveSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	sess, err := s.repo.GetActiveSessionNoLock(ctx, issueID, agentID)
	if err != nil {
		return nil, fmt.Errorf("get active session: %w", err)
	}
	return sess, nil
}

// UpdateSessionWithPayload persists full state data with version checking and compression.
// When StateData is nil the existing DB state is preserved (no clobber).
func (s *service) UpdateSessionWithPayload(ctx context.Context, sessionID uuid.UUID, payload UpdatePayload) error {
	var stateBytes json.RawMessage
	if payload.StateData != nil {
		s.compressMessages(payload.StateData)
		b, err := json.Marshal(payload.StateData)
		if err != nil {
			return fmt.Errorf("marshal state data: %w", err)
		}
		stateBytes = b
	}

	if err := s.repo.UpdateState(ctx, sessionID, stateBytes, payload.Summary, payload.FilesModified, payload.WorkingDir, payload.Branch, payload.ExpectedVersion); err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	s.logger.Info("session updated",
		"session_id", sessionID,
		"version", payload.ExpectedVersion+1,
	)
	return nil
}

// ExpireSession marks a specific session as expired.
func (s *service) ExpireSession(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.repo.Deactivate(ctx, sessionID); err != nil {
		return fmt.Errorf("expire session: %w", err)
	}
	s.logger.Info("session expired", "session_id", sessionID)
	return nil
}

// ResetSession deactivates the current active session for an issue+agent.
func (s *service) ResetSession(ctx context.Context, issueID, agentID uuid.UUID) error {
	if err := s.repo.DeactivateByIssueAndAgent(ctx, issueID, agentID); err != nil {
		return fmt.Errorf("reset session: %w", err)
	}
	s.logger.Info("session reset",
		"issue_id", issueID,
		"agent_id", agentID,
	)
	return nil
}

// CleanupExpired marks all sessions inactive if they haven't been active within the configured period.
func (s *service) CleanupExpired(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-s.config.InactivityExpiry)
	ids, err := s.repo.ExpireBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired: %w", err)
	}
	if len(ids) > 0 {
		s.logger.Info("expired sessions cleaned up", "count", len(ids), "cutoff", cutoff)
	}
	return len(ids), nil
}

// ListSessions returns all sessions for a given issue.
func (s *service) ListSessions(ctx context.Context, issueID uuid.UUID) ([]*Session, error) {
	return s.repo.ListByIssue(ctx, issueID)
}

// GetSession returns a single session by ID.
func (s *service) GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	return s.repo.GetByID(ctx, sessionID)
}

// compressMessages summarizes older messages when count exceeds threshold.
// Discarded messages are condensed into a single summary message so the
// caller's intent (documented as "auto-summarize") is honoured rather than
// silently dropping history.
func (s *service) compressMessages(data *StateData) {
	if len(data.Messages) <= s.config.MaxMessagesBeforeCompress {
		return
	}

	// Keep threshold-1 original messages + 1 summary = exactly threshold total.
	splitIdx := len(data.Messages) - (s.config.MaxMessagesBeforeCompress - 1)
	discarded := data.Messages[:splitIdx]

	// Build a brief summary from discarded messages so history is not lost.
	var roles []string
	for _, m := range discarded {
		roles = append(roles, m.Role)
	}
	summary := Message{
		Role:      "system",
		Content:   fmt.Sprintf("[compressed %d messages: %s]", len(discarded), strings.Join(roles, ",")),
		Timestamp: discarded[len(discarded)-1].Timestamp,
	}

	data.Messages = append([]Message{summary}, data.Messages[splitIdx:]...)
}

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
