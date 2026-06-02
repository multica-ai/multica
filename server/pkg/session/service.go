package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
func (s *service) CreateSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	latestRun, err := s.repo.GetLatestRunNumber(ctx, issueID, agentID)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	initialState := StateData{LastCheckpoint: time.Now().UTC()}
	stateBytes, err := json.Marshal(initialState)
	if err != nil {
		return nil, fmt.Errorf("marshal initial state: %w", err)
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

// GetActiveSession returns the active session for an issue+agent, or nil.
func (s *service) GetActiveSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	sess, err := s.repo.GetActiveSessionNoLock(ctx, issueID, agentID)
	if err != nil {
		return nil, fmt.Errorf("get active session: %w", err)
	}
	return sess, nil
}

// UpdateSessionWithPayload persists full state data with version checking and compression.
func (s *service) UpdateSessionWithPayload(ctx context.Context, sessionID uuid.UUID, payload UpdatePayload) error {
	if payload.StateData != nil {
		s.compressMessages(payload.StateData)
	}

	stateBytes, err := json.Marshal(payload.StateData)
	if err != nil {
		return fmt.Errorf("marshal state data: %w", err)
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

// compressMessages summarizes older messages when count exceeds threshold.
// Keeps the last N messages intact, discards the rest (caller can store a summary).
func (s *service) compressMessages(data *StateData) {
	if len(data.Messages) <= s.config.MaxMessagesBeforeCompress {
		return
	}

	splitIdx := len(data.Messages) - s.config.MaxMessagesBeforeCompress
	data.Messages = data.Messages[splitIdx:]
}
