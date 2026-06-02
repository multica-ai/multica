package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewSession(t *testing.T) {
	issueID := uuid.New()
	agentID := uuid.New()
	state := json.RawMessage(`{"messages": [], "tool_results": []}`)

	s := NewSession(issueID, agentID, state)

	if s.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
	if s.IssueID != issueID {
		t.Errorf("IssueID = %s, want %s", s.IssueID, issueID)
	}
	if s.AgentID != agentID {
		t.Errorf("AgentID = %s, want %s", s.AgentID, agentID)
	}
	if s.RunNumber != 1 {
		t.Errorf("RunNumber = %d, want 1", s.RunNumber)
	}
	if !s.IsActive {
		t.Error("expected IsActive = true")
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if len(s.FilesModified) != 0 {
		t.Errorf("FilesModified = %v, want empty", s.FilesModified)
	}
}

func TestNewSession_NilState(t *testing.T) {
	s := NewSession(uuid.New(), uuid.New(), nil)
	if string(s.State) != `{}` {
		t.Errorf("State = %s, want {}", s.State)
	}
}

func TestSession_Expired(t *testing.T) {
	s := &Session{}

	// No expiry set → not expired
	if s.Expired() {
		t.Error("expected not expired when ExpiresAt is nil")
	}

	// Future expiry → not expired
	future := time.Now().Add(time.Hour)
	s.ExpiresAt = &future
	if s.Expired() {
		t.Error("expected not expired with future ExpiresAt")
	}

	// Past expiry → expired
	past := time.Now().Add(-time.Hour)
	s.ExpiresAt = &past
	if !s.Expired() {
		t.Error("expected expired with past ExpiresAt")
	}
}

func TestSession_SetExpiry(t *testing.T) {
	s := &Session{}
	s.SetExpiry(7 * 24 * time.Hour)

	if s.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set")
	}
	expected := time.Now().Add(7 * 24 * time.Hour)
	diff := s.ExpiresAt.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("ExpiresAt = %v, want ~%v", s.ExpiresAt, expected)
	}
}
