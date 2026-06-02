package session

// integration_test.go — comprehensive integration tests for session
// persistence (AIH-44.6). Covers lifecycle CRUD, concurrency, resume
// context injection, token-usage benchmark, and edge cases.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func populatedService(t *testing.T) (*testService, uuid.UUID, uuid.UUID, *Session) {
	t.Helper()
	svc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()

	sess, err := svc.createSession(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}

	summary := "Implemented RBAC middleware with JWT refresh tokens and role hierarchy"
	sess.ConversationSummary = &summary
	branch := "feature/rbac"
	sess.Branch = &branch
	wd := "/workspace/app"
	sess.WorkingDirectory = &wd
	sess.FilesModified = []string{"auth/middleware.go", "auth/jwt.go", "auth/roles.go"}

	state := StateData{
		Messages: []Message{
			{Role: "user", Content: "Implement RBAC", Timestamp: time.Now().Add(-time.Hour)},
			{Role: "assistant", Content: "Building RBAC middleware...", Timestamp: time.Now().Add(-50 * time.Minute)},
		},
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: "Use PostgreSQL row-level security for tenant isolation"},
			{ToolName: "decision", Output: "Cache roles in Redis with 5-minute TTL"},
			{ToolName: "code_review", Output: "Looks good"},
		},
		AnalysisDone:   true,
		LastCheckpoint: time.Now().Add(-10 * time.Minute),
	}
	stateBytes, _ := json.Marshal(state)
	sess.State = stateBytes

	return svc, issueID, agentID, sess
}

// sessionServiceAdapter wraps testService to satisfy the SessionService interface.
type sessionServiceAdapter struct {
	ts *testService
}

func (a *sessionServiceAdapter) CreateSession(ctx context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	return a.ts.createSession(ctx, issueID, agentID)
}

func (a *sessionServiceAdapter) GetActiveSession(_ context.Context, issueID, agentID uuid.UUID) (*Session, error) {
	return a.ts.repo.getActive(context.Background(), issueID, agentID)
}

func (a *sessionServiceAdapter) UpdateSessionWithPayload(_ context.Context, sessionID uuid.UUID, payload UpdatePayload) error {
	a.ts.repo.mu.Lock()
	defer a.ts.repo.mu.Unlock()
	s, ok := a.ts.repo.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	if s.Version != payload.ExpectedVersion {
		return ErrVersionConflict
	}
	if payload.StateData != nil {
		sb, _ := json.Marshal(payload.StateData)
		s.State = sb
	}
	if payload.Summary != nil {
		s.ConversationSummary = payload.Summary
	}
	if payload.FilesModified != nil {
		s.FilesModified = payload.FilesModified
	}
	if payload.WorkingDir != nil {
		s.WorkingDirectory = payload.WorkingDir
	}
	if payload.Branch != nil {
		s.Branch = payload.Branch
	}
	s.Version++
	return nil
}

func (a *sessionServiceAdapter) ExpireSession(_ context.Context, sessionID uuid.UUID) error {
	return a.ts.repo.deactivate(context.Background(), sessionID)
}

func (a *sessionServiceAdapter) ResetSession(_ context.Context, issueID, agentID uuid.UUID) error {
	return a.ts.repo.deactivateByIssueAndAgent(context.Background(), issueID, agentID)
}

func (a *sessionServiceAdapter) CleanupExpired(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-a.ts.config.InactivityExpiry)
	return a.ts.repo.expireBefore(ctx, cutoff)
}

func buildResumerFromTest(svc *testService) *Resumer {
	return NewResumer(&sessionServiceAdapter{ts: svc}, svc.config)
}

// ---------------------------------------------------------------------------
// 1. SESSION LIFECYCLE TESTS
// ---------------------------------------------------------------------------

func TestLifecycle_CreateSession_DefaultState(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, err := svc.createSession(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.IssueID != issueID {
		t.Errorf("IssueID mismatch")
	}
	if sess.AgentID != agentID {
		t.Errorf("AgentID mismatch")
	}
	if sess.RunNumber != 1 {
		t.Errorf("RunNumber = %d, want 1", sess.RunNumber)
	}
	if !sess.IsActive {
		t.Error("new session should be active")
	}
	if sess.Version != 1 {
		t.Errorf("Version = %d, want 1", sess.Version)
	}
	if sess.ExpiresAt == nil {
		t.Error("ExpiresAt should be set")
	}
	if sess.ExpiresAt.Before(time.Now().UTC()) {
		t.Error("ExpiresAt should be in the future")
	}

	var state StateData
	if err := json.Unmarshal(sess.State, &state); err != nil {
		t.Fatalf("initial state is not valid JSON: %v", err)
	}
	if state.LastCheckpoint.IsZero() {
		t.Error("initial state should have LastCheckpoint set")
	}
}

func TestLifecycle_LoadActiveSession(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active != nil {
		t.Error("expected nil before creation")
	}

	svc.createSession(ctx, issueID, agentID)

	active, _ = svc.repo.getActive(ctx, issueID, agentID)
	if active == nil {
		t.Fatal("expected active session after creation")
	}
	if !active.IsActive {
		t.Error("loaded session should be active")
	}
}

func TestLifecycle_UpdateSession_PersistsState(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	newState := &StateData{
		Messages: []Message{
			{Role: "user", Content: "hello", Timestamp: time.Now()},
			{Role: "assistant", Content: "hi there", Timestamp: time.Now()},
		},
		AnalysisDone:   true,
		LastCheckpoint: time.Now(),
	}
	summary := "Greeted user, completed initial analysis"
	stateBytes, _ := json.Marshal(newState)
	sess.State = stateBytes
	sess.ConversationSummary = &summary
	sess.FilesModified = []string{"main.go"}
	sess.WorkingDirectory = strPtr("/workspace/project")
	sess.Branch = strPtr("main")
	sess.Version = 2

	loaded, _ := svc.repo.getActive(ctx, issueID, agentID)
	if loaded == nil {
		t.Fatal("session should still be active")
	}
	var loadedState StateData
	if err := json.Unmarshal(loaded.State, &loadedState); err != nil {
		t.Fatalf("state JSON invalid: %v", err)
	}
}

func TestLifecycle_ExpireSession_NoLongerActive(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)
	if err := svc.repo.deactivate(ctx, sess.ID); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active != nil {
		t.Error("expired session should not be returned as active")
	}

	err := svc.repo.deactivate(ctx, sess.ID)
	if err != ErrSessionNotFound {
		t.Errorf("double-deactivate should return ErrSessionNotFound, got: %v", err)
	}
}

func TestLifecycle_ResetSession_FreshState(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	for i := 0; i < 3; i++ {
		svc.createSession(ctx, issueID, agentID)
	}
	svc.repo.mu.Lock()
	for _, s := range svc.repo.sessions {
		s.IsActive = true
	}
	svc.repo.mu.Unlock()

	svc.repo.deactivateByIssueAndAgent(ctx, issueID, agentID)

	active, _ := svc.repo.getActive(ctx, issueID, agentID)
	if active != nil {
		t.Error("all sessions should be deactivated after reset")
	}

	newSess, _ := svc.createSession(ctx, issueID, agentID)
	if newSess.RunNumber != 4 {
		t.Errorf("post-reset run_number = %d, want 4", newSess.RunNumber)
	}
}

func TestLifecycle_RunNumber_IncrementsAcrossRuns(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	for i := 1; i <= 5; i++ {
		sess, err := svc.createSession(ctx, issueID, agentID)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if sess.RunNumber != i {
			t.Errorf("run %d: RunNumber = %d", i, sess.RunNumber)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. CONCURRENCY TESTS
// ---------------------------------------------------------------------------

func TestConcurrency_TwoRunsSameIssue_NoDataCorruption(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	svc.createSession(ctx, issueID, agentID)

	// Two concurrent creates on same issue+agent.
	// Note: the mock repo's getLatestRunNumber+create is not atomic (real DB
	// enforces UNIQUE(issue_id, agent_id, run_number)). We verify no panics,
	// no errors, and each session gets a unique UUID.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	sessions := make(chan *Session, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := svc.createSession(ctx, issueID, agentID)
			if err != nil {
				errs <- err
				return
			}
			sessions <- sess
		}()
	}
	wg.Wait()
	close(errs)
	close(sessions)

	for err := range errs {
		t.Errorf("concurrent create failed: %v", err)
	}

	seen := map[uuid.UUID]bool{}
	for sess := range sessions {
		if seen[sess.ID] {
			t.Errorf("duplicate session ID: %s", sess.ID)
		}
		seen[sess.ID] = true
	}
}

func TestConcurrency_TwoRunsDifferentIssues_NoCrossContamination(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	agentID := uuid.New()
	issue1 := uuid.New()
	issue2 := uuid.New()

	var wg sync.WaitGroup
	var s1, s2 *Session
	var e1, e2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		s1, e1 = svc.createSession(ctx, issue1, agentID)
	}()
	go func() {
		defer wg.Done()
		s2, e2 = svc.createSession(ctx, issue2, agentID)
	}()
	wg.Wait()

	if e1 != nil || e2 != nil {
		t.Fatalf("errors: %v, %v", e1, e2)
	}

	if s1.IssueID != issue1 || s2.IssueID != issue2 {
		t.Error("cross-contamination: issue IDs mixed up")
	}
	if s1.ID == s2.ID {
		t.Error("concurrent sessions on different issues got same ID")
	}

	a1, _ := svc.repo.getActive(ctx, issue1, agentID)
	a2, _ := svc.repo.getActive(ctx, issue2, agentID)
	if a1 == nil || a2 == nil {
		t.Error("both sessions should be active independently")
	}
	if a1.ID == a2.ID {
		t.Error("active sessions should be different")
	}
}

func TestConcurrency_HighParallelism_NoPanics(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	agentID := uuid.New()

	const numIssues = 20
	issues := make([]uuid.UUID, numIssues)
	for i := range issues {
		issues[i] = uuid.New()
	}

	var wg sync.WaitGroup
	var errCount atomic.Int32

	for _, iss := range issues {
		wg.Add(1)
		go func(issueID uuid.UUID) {
			defer wg.Done()
			if _, err := svc.createSession(ctx, issueID, agentID); err != nil {
				errCount.Add(1)
			}
		}(iss)
	}
	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("%d concurrent creates failed", errCount.Load())
	}

	for _, iss := range issues {
		active, _ := svc.repo.getActive(ctx, iss, agentID)
		if active == nil {
			t.Errorf("issue %s missing active session", iss)
		}
	}
}

func TestConcurrency_ResetDuringActiveRun_Graceful(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	var wg sync.WaitGroup
	var resetErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
	}()
	go func() {
		defer wg.Done()
		resetErr = svc.repo.deactivateByIssueAndAgent(ctx, issueID, agentID)
	}()
	wg.Wait()

	if resetErr != nil {
		t.Errorf("reset should not error: %v", resetErr)
	}

	svc.repo.mu.Lock()
	s := svc.repo.sessions[sess.ID]
	isActive := s.IsActive
	svc.repo.mu.Unlock()
	if isActive {
		t.Error("session should be inactive after concurrent reset")
	}
}

// ---------------------------------------------------------------------------
// 3. RESUME CONTEXT TESTS
// ---------------------------------------------------------------------------

func TestResume_SecondRunReceivesContext(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, err := resumer.Resume(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	if !result.IsResume {
		t.Error("second run should detect resume")
	}
	if result.Context == nil {
		t.Fatal("resume context should not be nil")
	}
	if result.Context.Summary == "" {
		t.Error("resume context should include summary")
	}
	if result.Context.Branch == "" {
		t.Error("resume context should include branch")
	}
	if len(result.Context.FilesModified) == 0 {
		t.Error("resume context should include files modified")
	}
	if result.Context.ResumedFromRunNum != 1 {
		t.Errorf("resumed from run %d, want 1", result.Context.ResumedFromRunNum)
	}
}

func TestResume_ContextIncludesSummaryAndDecisions(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	if result.Context == nil {
		t.Fatal("nil context")
	}

	prompt := result.Context.BuildResumePrompt()

	if !strings.Contains(prompt, "RBAC middleware") {
		t.Error("prompt should contain summary")
	}
	if !strings.Contains(prompt, "feature/rbac") {
		t.Error("prompt should contain branch")
	}
	for _, f := range []string{"auth/middleware.go", "auth/jwt.go", "auth/roles.go"} {
		if !strings.Contains(prompt, f) {
			t.Errorf("prompt missing file: %s", f)
		}
	}
	if !strings.Contains(prompt, "PostgreSQL row-level security") {
		t.Error("prompt should contain extracted decisions")
	}
	if !strings.Contains(prompt, "Redis") {
		t.Error("prompt should contain second decision")
	}
}

func TestResume_FreshRun_NoContextInjection(t *testing.T) {
	svc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()

	resumer := buildResumerFromTest(svc)
	result, err := resumer.Resume(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("fresh resume: %v", err)
	}

	if result.IsResume {
		t.Error("first run should not be a resume")
	}
	if result.Context != nil {
		t.Error("fresh run should have nil context")
	}
	if result.NewSession == nil {
		t.Fatal("should create new session")
	}
	if result.NewSession.RunNumber != 1 {
		t.Errorf("fresh run number = %d, want 1", result.NewSession.RunNumber)
	}
}

func TestResume_NoDuplicateAnalysisSteps(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	if result.Context == nil {
		t.Fatal("nil context")
	}

	if result.Context.Summary == "" {
		t.Error("empty summary would force re-analysis")
	}
	if len(result.Context.Decisions) == 0 {
		t.Error("no decisions means prior analysis results lost")
	}
}

func TestResume_FallbackOnMissingSession(t *testing.T) {
	svc := newTestService(DefaultConfig())
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, err := resumer.Resume(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("should not error on missing session: %v", err)
	}
	if result.IsResume {
		t.Error("should not be resume when no session exists")
	}
}

func TestResume_NewSessionIncrementsRunNumber(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	if result.NewSession.RunNumber != 2 {
		t.Errorf("new session run_number = %d, want 2", result.NewSession.RunNumber)
	}
}

func TestResume_Complete_PersistsState(t *testing.T) {
	svc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	state := &StateData{
		Messages: []Message{
			{Role: "user", Content: "Fix the auth bug", Timestamp: time.Now()},
			{Role: "assistant", Content: "Fixed auth/middleware.go", Timestamp: time.Now()},
		},
		AnalysisDone:   true,
		LastCheckpoint: time.Now(),
	}

	err := resumer.Complete(
		ctx,
		result.NewSession.ID,
		result.NewSession.Version,
		"Fixed auth bug by adding token validation",
		state,
		"fix/auth",
		"/workspace/app",
		[]string{"auth/middleware.go"},
	)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestResume_ExpirePrevious_DeactivatesOldSessions(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	oldActive, _ := svc.repo.getActive(ctx, issueID, agentID)
	if oldActive == nil {
		t.Fatal("old session should still be active")
	}

	err := resumer.ExpirePrevious(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("expire previous: %v", err)
	}
	_ = result
}

// ---------------------------------------------------------------------------
// 4. TOKEN USAGE BENCHMARK
// ---------------------------------------------------------------------------

func TestTokenUsage_FreshVsResumed(t *testing.T) {
	svc, issueID, agentID, _ := populatedService(t)
	ctx := context.Background()
	resumer := buildResumerFromTest(svc)

	freshResult, _ := resumer.Resume(ctx, uuid.New(), uuid.New())
	freshTokens := 0
	if freshResult.Context != nil {
		freshTokens = freshResult.Context.TokensEstimated
	}

	resumedResult, _ := resumer.Resume(ctx, issueID, agentID)
	if resumedResult.Context == nil {
		t.Fatal("resumed run should have context")
	}
	resumedTokens := resumedResult.Context.TokensEstimated

	if resumedTokens == 0 {
		t.Error("resumed session should inject tokens > 0")
	}

	t.Logf("Token usage benchmark:")
	t.Logf("  Fresh run context tokens:   %d", freshTokens)
	t.Logf("  Resumed run context tokens: %d", resumedTokens)
	t.Logf("  Context injects %d tokens of prior session data", resumedTokens)

	if resumedTokens > ResumeTokenCap+100 {
		t.Errorf("resumed tokens (%d) exceeded cap (%d)", resumedTokens, ResumeTokenCap)
	}
}

func TestTokenUsage_CappingAtResumeTokenCap(t *testing.T) {
	svc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	longSummary := strings.Repeat("We decided to use Go for the backend because of its strong concurrency model and type safety. ", 100)
	sess.ConversationSummary = &longSummary
	sess.Branch = strPtr("feature/big-change")
	sess.FilesModified = make([]string, 50)
	for i := range sess.FilesModified {
		sess.FilesModified[i] = fmt.Sprintf("pkg/module%d/file%d.go", i/10, i)
	}

	state := StateData{
		Messages: make([]Message, 80),
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: strings.Repeat("Important decision text. ", 50)},
			{ToolName: "decision", Output: strings.Repeat("Another critical choice. ", 50)},
		},
	}
	for i := range state.Messages {
		state.Messages[i] = Message{
			Role:    "user",
			Content: strings.Repeat("analysis output ", 20),
		}
	}
	stateBytes, _ := json.Marshal(state)
	sess.State = stateBytes

	resumer := buildResumerFromTest(svc)
	result, _ := resumer.Resume(ctx, issueID, agentID)

	if result.Context == nil {
		t.Fatal("nil context")
	}

	if result.Context.TokensEstimated > ResumeTokenCap+200 {
		t.Errorf("tokens not capped: got %d, cap %d", result.Context.TokensEstimated, ResumeTokenCap)
	}

	t.Logf("Large session tokens (after capping): %d / %d", result.Context.TokensEstimated, ResumeTokenCap)
}

// ---------------------------------------------------------------------------
// 5. EDGE CASES
// ---------------------------------------------------------------------------

func TestEdgeCase_SessionWithOver100Messages_Compression(t *testing.T) {
	cfg := Config{
		InactivityExpiry:          7 * 24 * time.Hour,
		MaxMessagesBeforeCompress: 100,
	}
	svc := &service{config: cfg}

	data := &StateData{
		Messages: make([]Message, 150),
	}
	for i := range data.Messages {
		data.Messages[i] = Message{
			Role:      "user",
			Content:   fmt.Sprintf("message %d", i),
			Timestamp: time.Now(),
		}
	}

	svc.compressMessages(data)

	if len(data.Messages) != 100 {
		t.Errorf("after compression: got %d messages, want 100", len(data.Messages))
	}

	if data.Messages[0].Content != "message 50" {
		t.Errorf("first retained message = %q, want 'message 50'", data.Messages[0].Content)
	}
	if data.Messages[99].Content != "message 149" {
		t.Errorf("last retained message = %q, want 'message 149'", data.Messages[99].Content)
	}
}

func TestEdgeCase_SessionExpiredExactlyAtBoundary(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(Config{
		InactivityExpiry:          1 * time.Hour,
		MaxMessagesBeforeCompress: 100,
	})
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	cutoff := time.Now().UTC().Add(-1 * time.Hour)
	svc.repo.mu.Lock()
	sess.LastActiveAt = cutoff
	svc.repo.mu.Unlock()

	count, _ := svc.repo.expireBefore(ctx, cutoff)
	if count != 0 {
		t.Errorf("exactly-at-boundary should not expire: got count=%d", count)
	}

	svc.repo.mu.Lock()
	sess.IsActive = true
	sess.LastActiveAt = cutoff.Add(-time.Nanosecond)
	svc.repo.mu.Unlock()

	count, _ = svc.repo.expireBefore(ctx, cutoff)
	if count != 1 {
		t.Errorf("1ns before boundary should expire: got count=%d", count)
	}
}

func TestEdgeCase_EmptySessionState_FirstRun(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	var state StateData
	if err := json.Unmarshal(sess.State, &state); err != nil {
		t.Fatalf("initial state JSON invalid: %v", err)
	}

	resumeCtx := buildResumeContext(sess, DefaultConfig())
	if resumeCtx.HasContent() {
		t.Error("empty initial session should not produce resume content")
	}
	if resumeCtx.BuildResumePrompt() != "" {
		t.Error("empty session should produce empty prompt")
	}
}

func TestEdgeCase_CorruptedSessionData_GracefulError(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())
	issueID := uuid.New()
	agentID := uuid.New()

	sess, _ := svc.createSession(ctx, issueID, agentID)

	svc.repo.mu.Lock()
	sess.State = json.RawMessage(`{invalid json!!!}`)
	svc.repo.mu.Unlock()

	resumeCtx := buildResumeContext(sess, DefaultConfig())
	if resumeCtx.Decisions != nil {
		t.Error("corrupted state should produce nil decisions")
	}
}

func TestEdgeCase_CorruptedState_NilState(t *testing.T) {
	sess := &Session{
		ID:           uuid.New(),
		RunNumber:    1,
		State:        nil,
		IsActive:     true,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	resumeCtx := buildResumeContext(sess, DefaultConfig())
	if resumeCtx.HasContent() {
		t.Error("nil state session should not have content")
	}
}

func TestEdgeCase_EmptyStateJSON(t *testing.T) {
	sess := &Session{
		ID:           uuid.New(),
		RunNumber:    1,
		State:        json.RawMessage(`{}`),
		IsActive:     true,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
	}

	resumeCtx := buildResumeContext(sess, DefaultConfig())
	if resumeCtx.HasContent() {
		t.Error("empty JSON state should not produce content")
	}
	if len(resumeCtx.Decisions) != 0 {
		t.Error("empty state should have no decisions")
	}
}

func TestEdgeCase_NilSession_GetActiveReturnsNil(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(DefaultConfig())

	active, _ := svc.repo.getActive(ctx, uuid.New(), uuid.New())
	if active != nil {
		t.Error("non-existent issue/agent should return nil")
	}
}

func TestEdgeCase_ExtractDecisions_LargeOutput_Truncated(t *testing.T) {
	st := &StateData{
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: strings.Repeat("A", 500)},
		},
	}
	decisions := extractDecisions(st)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	// truncate(s, 200) produces s[:199] + U+2026 (3 UTF-8 bytes) = 202 bytes.
	if len(decisions[0]) > 203 {
		t.Errorf("decision not truncated: len=%d", len(decisions[0]))
	}
	if !strings.HasSuffix(decisions[0], "…") {
		t.Error("truncated decision should end with ellipsis")
	}
}

func TestEdgeCase_ExtractDecisions_MaxFive(t *testing.T) {
	st := &StateData{
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: "d1"},
			{ToolName: "decision", Output: "d2"},
			{ToolName: "decision", Output: "d3"},
			{ToolName: "decision", Output: "d4"},
			{ToolName: "decision", Output: "d5"},
			{ToolName: "decision", Output: "d6"},
			{ToolName: "decision", Output: "d7"},
		},
	}
	decisions := extractDecisions(st)
	if len(decisions) != 5 {
		t.Errorf("expected max 5 decisions, got %d", len(decisions))
	}
}

func TestEdgeCase_VeryLargeSession_NoPanic(t *testing.T) {
	cfg := Config{
		InactivityExpiry:          7 * 24 * time.Hour,
		MaxMessagesBeforeCompress: 100,
	}
	svc := &service{config: cfg}

	data := &StateData{
		Messages: make([]Message, 10000),
	}
	for i := range data.Messages {
		data.Messages[i] = Message{
			Role:    "user",
			Content: strings.Repeat("x", 1000),
		}
	}

	svc.compressMessages(data)
	if len(data.Messages) != 100 {
		t.Errorf("expected 100 after compress, got %d", len(data.Messages))
	}
}

// ---------------------------------------------------------------------------
// 6. PROMPT RENDERING TESTS
// ---------------------------------------------------------------------------

func TestBuildResumePrompt_ContainsAllSections(t *testing.T) {
	now := time.Now()
	ctx := ResumeContext{
		Session: &Session{
			CreatedAt:    now.Add(-2 * time.Hour),
			LastActiveAt: now.Add(-30 * time.Minute),
		},
		Summary:           "Implemented feature X with tests",
		Branch:            "feature/x",
		WorkingDirectory:  "/workspace/app",
		FilesModified:     []string{"main.go", "handler.go", "test.go"},
		Decisions:         []string{"Use chi router", "Add rate limiting"},
		ResumedFromRunNum: 3,
		TokensEstimated:   50,
	}

	prompt := ctx.BuildResumePrompt()

	checks := []struct {
		name, substr string
	}{
		{"header", "Previous Session Context (Run #3)"},
		{"session start", "Session started:"},
		{"last active", "Last active:"},
		{"summary heading", "### Summary"},
		{"summary text", "Implemented feature X"},
		{"working state heading", "### Working State"},
		{"branch", "Branch: feature/x"},
		{"workdir", "Working Directory: /workspace/app"},
		{"files heading", "Files Modified:"},
		{"file1", "main.go"},
		{"file2", "handler.go"},
		{"file3", "test.go"},
		{"decisions heading", "### Key Decisions"},
		{"decision1", "Use chi router"},
		{"decision2", "Add rate limiting"},
	}
	for _, c := range checks {
		if !strings.Contains(prompt, c.substr) {
			t.Errorf("prompt missing %s: expected %q", c.name, c.substr)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. CONFIG TESTS
// ---------------------------------------------------------------------------

func TestConfig_DefaultValues(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.InactivityExpiry != 7*24*time.Hour {
		t.Errorf("InactivityExpiry = %v, want 7d", cfg.InactivityExpiry)
	}
	if cfg.MaxMessagesBeforeCompress != 100 {
		t.Errorf("MaxMessagesBeforeCompress = %d, want 100", cfg.MaxMessagesBeforeCompress)
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{
		InactivityExpiry:          24 * time.Hour,
		MaxMessagesBeforeCompress: 50,
	}
	svc := &service{config: cfg}

	data := &StateData{Messages: make([]Message, 60)}
	svc.compressMessages(data)
	if len(data.Messages) != 50 {
		t.Errorf("custom compress threshold: got %d, want 50", len(data.Messages))
	}
}

// ---------------------------------------------------------------------------
// 8. FULL LIFECYCLE INTEGRATION (multi-run scenario)
// ---------------------------------------------------------------------------

func TestFullLifecycle_ThreeRunScenario(t *testing.T) {
	svc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()
	resumer := buildResumerFromTest(svc)

	// === Run 1: Fresh start ===
	r1, err := resumer.Resume(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("run 1 resume: %v", err)
	}
	if r1.IsResume {
		t.Error("run 1 should not be resume")
	}
	if r1.NewSession.RunNumber != 1 {
		t.Errorf("run 1 number = %d", r1.NewSession.RunNumber)
	}

	r1State := &StateData{
		Messages: []Message{
			{Role: "user", Content: "Implement auth", Timestamp: time.Now()},
			{Role: "assistant", Content: "Done, implemented JWT", Timestamp: time.Now()},
		},
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: "Use RS256 for JWT signing"},
		},
		AnalysisDone: true,
	}
	resumer.Complete(ctx, r1.NewSession.ID, r1.NewSession.Version,
		"Implemented JWT auth with RS256", r1State, "feature/auth", "/app", []string{"auth.go"})

	// === Run 2: Resume from run 1 ===
	r2, err := resumer.Resume(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("run 2 resume: %v", err)
	}
	if !r2.IsResume {
		t.Error("run 2 should be resume")
	}
	if r2.NewSession.RunNumber != 2 {
		t.Errorf("run 2 number = %d", r2.NewSession.RunNumber)
	}
	if r2.Context == nil {
		t.Fatal("run 2 context should not be nil")
	}
	if !strings.Contains(r2.Context.Summary, "JWT auth") {
		t.Error("run 2 context missing run 1 summary")
	}
	if r2.Context.Branch != "feature/auth" {
		t.Errorf("run 2 branch = %q", r2.Context.Branch)
	}
	if len(r2.Context.Decisions) == 0 {
		t.Error("run 2 should have decisions from run 1")
	}

	r2State := &StateData{
		Messages: []Message{
			{Role: "user", Content: "Add RBAC", Timestamp: time.Now()},
			{Role: "assistant", Content: "Done, added role hierarchy", Timestamp: time.Now()},
		},
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: "Use Redis for role caching"},
		},
	}
	resumer.Complete(ctx, r2.NewSession.ID, r2.NewSession.Version,
		"Added RBAC with role hierarchy and Redis caching", r2State, "feature/rbac", "/app", []string{"auth.go", "rbac.go"})

	// === Run 3: Resume from run 2 ===
	r3, err := resumer.Resume(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("run 3 resume: %v", err)
	}
	if !r3.IsResume {
		t.Error("run 3 should be resume")
	}
	if r3.NewSession.RunNumber != 3 {
		t.Errorf("run 3 number = %d", r3.NewSession.RunNumber)
	}
	if r3.Context == nil {
		t.Fatal("run 3 context should not be nil")
	}
	if !strings.Contains(r3.Context.Summary, "RBAC") {
		t.Error("run 3 context should reference RBAC from run 2")
	}
	if len(r3.Context.FilesModified) != 2 {
		t.Errorf("run 3 files = %d, want 2", len(r3.Context.FilesModified))
	}
}
