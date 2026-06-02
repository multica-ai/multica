package session

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- Mock Repository ---

type mockRepository struct {
	mu           sync.Mutex
	sessions     map[uuid.UUID]*Session
	latestRunNum map[string]int
}

func newMockRepo() *mockRepository {
	return &mockRepository{
		sessions:     make(map[uuid.UUID]*Session),
		latestRunNum: make(map[string]int),
	}
}

func key(issueID, agentID uuid.UUID) string {
	return issueID.String() + ":" + agentID.String()
}

func (m *mockRepository) create(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	k := key(s.IssueID, s.AgentID)
	if s.RunNumber > m.latestRunNum[k] {
		m.latestRunNum[k] = s.RunNumber
	}
	return nil
}

func (m *mockRepository) getActive(_ context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *Session
	for _, s := range m.sessions {
		if s.IssueID == issueID && s.AgentID == agentID && s.IsActive {
			if best == nil || s.RunNumber > best.RunNumber {
				best = s
			}
		}
	}
	return best, nil
}

func (m *mockRepository) deactivate(_ context.Context, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok || !s.IsActive {
		return ErrSessionNotFound
	}
	s.IsActive = false
	return nil
}

func (m *mockRepository) deactivateByIssueAndAgent(_ context.Context, issueID, agentID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.IssueID == issueID && s.AgentID == agentID && s.IsActive {
			s.IsActive = false
		}
	}
	return nil
}

func (m *mockRepository) expireBefore(_ context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, s := range m.sessions {
		if s.IsActive && s.LastActiveAt.Before(cutoff) {
			s.IsActive = false
			count++
		}
	}
	return count, nil
}

func (m *mockRepository) getLatestRunNumber(_ context.Context, issueID, agentID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latestRunNum[key(issueID, agentID)], nil
}

// --- Test Service ---

type testService struct {
	repo   *mockRepository
	config Config
}

func newTestService(cfg Config) *testService {
	return &testService{
		repo:   newMockRepo(),
		config: cfg,
	}
}

func (ts *testService) createSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	latestRun, err := ts.repo.getLatestRunNumber(ctx, issueID, agentID)
	if err != nil {
		return nil, err
	}
	initialState := StateData{LastCheckpoint: time.Now().UTC()}
	stateBytes, _ := json.Marshal(initialState)
	now := time.Now().UTC()
	expiresAt := now.Add(ts.config.InactivityExpiry)

	sess := &Session{
		ID:           uuid.New(),
		IssueID:      issueID,
		AgentID:      agentID,
		RunNumber:    latestRun + 1,
		State:        stateBytes,
		IsActive:     true,
		LastActiveAt: now,
		ExpiresAt:    &expiresAt,
		Version:      1,
	}
	return sess, ts.repo.create(ctx, sess)
}

// --- Tests ---

func TestCreateSession_IncrementsRunNumber(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	for i, tt := range []struct {
		name     string
		expected int
	}{
		{"first run", 1},
		{"second run", 2},
		{"third run", 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sess, err := svc.createSession(ctx, issueID, agentID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if sess.RunNumber != tt.expected {
				t.Errorf("run_number = %d, want %d", sess.RunNumber, tt.expected)
			}
			if !sess.IsActive {
				t.Error("new session should be active")
			}
			if sess.Version != 1 {
				t.Errorf("version = %d, want 1", sess.Version)
			}
			if i == 0 {
				if sess.IssueID != issueID || sess.AgentID != agentID {
					t.Error("issue/agent ID mismatch")
				}
			}
		})
	}
}

func TestGetActiveSession_ReturnsNilWhenNone(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	sess, _ := svc.repo.getActive(ctx, uuid.New(), uuid.New())
	if sess != nil {
		t.Error("expected nil")
	}
}

func TestGetActiveSession_ReturnsLatestActive(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	s1, _ := svc.createSession(ctx, issueID, agentID)
	svc.createSession(ctx, issueID, agentID)
	_ = svc.repo.deactivate(ctx, s1.ID)

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active == nil || active.RunNumber != 2 {
		t.Errorf("expected run_number 2, got %v", active)
	}
}

func TestExpireSession(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)
	_ = svc.repo.deactivate(ctx, sess.ID)

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active != nil {
		t.Error("expired session should not be active")
	}
}

func TestResetSession_DeactivatesAll(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	svc.createSession(ctx, issueID, agentID)
	svc.createSession(ctx, issueID, agentID)

	// Re-activate all for test purposes.
	svc.repo.mu.Lock()
	for _, s := range svc.repo.sessions {
		s.IsActive = true
	}
	svc.repo.mu.Unlock()

	_ = svc.repo.deactivateByIssueAndAgent(ctx, issueID, agentID)

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active != nil {
		t.Error("all sessions should be deactivated after reset")
	}
}

func TestCleanupExpired(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(Config{
		InactivityExpiry:          1 * time.Hour,
		MaxMessagesBeforeCompress: 100,
	})
	issueID := uuid.New()
	agentID := uuid.New()

	s1, _ := svc.createSession(ctx, issueID, agentID)
	svc.repo.mu.Lock()
	s1.LastActiveAt = time.Now().UTC().Add(-2 * time.Hour)
	svc.repo.mu.Unlock()

	svc.createSession(ctx, issueID, agentID)

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	count, _ := svc.repo.expireBefore(ctx, cutoff)
	if count != 1 {
		t.Errorf("expired count = %d, want 1", count)
	}
}

func TestCompressMessages(t *testing.T) {
	svc := &service{config: Config{
		InactivityExpiry:          7 * 24 * time.Hour,
		MaxMessagesBeforeCompress: 5,
	}}
	data := &StateData{}
	for i := 0; i < 10; i++ {
		data.Messages = append(data.Messages, Message{
			Role:      "user",
			Content:   "test",
			Timestamp: time.Now().UTC(),
		})
	}
	svc.compressMessages(data)
	if len(data.Messages) != 5 {
		t.Errorf("after compression = %d, want 5", len(data.Messages))
	}
}

func TestCompressMessages_NoOpBelowThreshold(t *testing.T) {
	svc := &service{config: Config{
		InactivityExpiry:          7 * 24 * time.Hour,
		MaxMessagesBeforeCompress: 100,
	}}
	data := &StateData{}
	for i := 0; i < 50; i++ {
		data.Messages = append(data.Messages, Message{
			Role:      "user",
			Content:   "test",
			Timestamp: time.Now().UTC(),
		})
	}
	svc.compressMessages(data)
	if len(data.Messages) != 50 {
		t.Errorf("should not compress, got %d", len(data.Messages))
	}
}

func TestConcurrentAccess_DifferentIssues(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	agentID := uuid.New()
	issue1 := uuid.New()
	issue2 := uuid.New()

	done := make(chan error, 2)
	go func() {
		_, err := svc.createSession(ctx, issue1, agentID)
		done <- err
	}()
	go func() {
		_, err := svc.createSession(ctx, issue2, agentID)
		done <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent create failed: %v", err)
		}
	}
	s1, _ := svc.repo.getActive(ctx, issue1, agentID)
	s2, _ := svc.repo.getActive(ctx, issue2, agentID)
	if s1 == nil || s2 == nil {
		t.Error("both sessions should exist independently")
	}
}
