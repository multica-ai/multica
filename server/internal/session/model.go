package session

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Session represents an agent run session for a specific issue.
type Session struct {
	ID                  uuid.UUID       `json:"id"`
	IssueID             uuid.UUID       `json:"issue_id"`
	AgentID             uuid.UUID       `json:"agent_id"`
	RunNumber           int             `json:"run_number"`
	State               json.RawMessage `json:"state"`
	ConversationSummary *string         `json:"conversation_summary,omitempty"`
	WorkingDirectory    *string         `json:"working_directory,omitempty"`
	Branch              *string         `json:"branch,omitempty"`
	FilesModified       []string        `json:"files_modified"`
	IsActive            bool            `json:"is_active"`
	CreatedAt           time.Time       `json:"created_at"`
	LastActiveAt        time.Time       `json:"last_active_at"`
	ExpiresAt           *time.Time      `json:"expires_at,omitempty"`
	Version             int             `json:"version"`
}

// NewSession creates a new session with defaults for a first-run on the given issue+agent.
func NewSession(issueID, agentID uuid.UUID, state json.RawMessage) *Session {
	if state == nil {
		state = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	return &Session{
		ID:            uuid.New(),
		IssueID:       issueID,
		AgentID:       agentID,
		RunNumber:     1,
		State:         state,
		FilesModified: []string{},
		IsActive:      true,
		CreatedAt:     now,
		LastActiveAt:  now,
		Version:       1,
	}
}

// Expired returns true if the session has an expiry time that has passed.
func (s *Session) Expired() bool {
	return s.ExpiresAt != nil && time.Now().UTC().After(*s.ExpiresAt)
}

// SetExpiry sets the expiry to the given duration from now.
func (s *Session) SetExpiry(d time.Duration) {
	t := time.Now().UTC().Add(d)
	s.ExpiresAt = &t
}
