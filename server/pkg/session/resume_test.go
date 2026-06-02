package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildResumePrompt_FreshSession_NoBlock(t *testing.T) {
	ctx := ResumeContext{}
	if got := ctx.BuildResumePrompt(); got != "" {
		t.Errorf("expected empty prompt for no-content context, got %q", got)
	}
}

func TestBuildResumePrompt_WithSummary(t *testing.T) {
	now := time.Now().UTC()
	ctx := ResumeContext{
		Session: &Session{
			CreatedAt:    now.Add(-time.Hour),
			LastActiveAt: now.Add(-30 * time.Minute),
		},
		Summary:           "Fixed auth bug in login handler. Refactored middleware.",
		ResumedFromRunNum: 2,
	}
	prompt := ctx.BuildResumePrompt()

	if !strings.Contains(prompt, "Previous Session Context (Run #2)") {
		t.Error("missing run number header")
	}
	if !strings.Contains(prompt, "Summary") {
		t.Error("missing Summary heading")
	}
	if !strings.Contains(prompt, "Fixed auth bug") {
		t.Error("missing summary text")
	}
	if !strings.Contains(prompt, "Session started:") {
		t.Error("missing session timestamp")
	}
}

func TestBuildResumePrompt_WithWorkingState(t *testing.T) {
	ctx := ResumeContext{
		Summary:          "Previous work",
		Branch:           "feature/AIH-22",
		WorkingDirectory: "/workspace/repo",
		FilesModified:    []string{"src/main.go", "config.yaml"},
		ResumedFromRunNum: 1,
	}
	prompt := ctx.BuildResumePrompt()

	if !strings.Contains(prompt, "Branch: feature/AIH-22") {
		t.Error("missing branch info")
	}
	if !strings.Contains(prompt, "Working Directory: /workspace/repo") {
		t.Error("missing working directory")
	}
	if !strings.Contains(prompt, "src/main.go") {
		t.Error("missing modified file")
	}
}

func TestBuildResumePrompt_WithDecisions(t *testing.T) {
	ctx := ResumeContext{
		Summary:          "Work in progress",
		Decisions:        []string{"Use Postgres for persistence", "Skip Redis cache layer"},
		ResumedFromRunNum: 3,
	}
	prompt := ctx.BuildResumePrompt()

	if !strings.Contains(prompt, "Key Decisions") {
		t.Error("missing decisions section")
	}
	if !strings.Contains(prompt, "Use Postgres") {
		t.Error("missing decision text")
	}
}

func TestBuildResumePrompt_TokenCapping(t *testing.T) {
	longSummary := strings.Repeat("word ", 5000)
	state := StateData{
		Messages: []Message{{Role: "user", Content: longSummary, Timestamp: time.Now()}},
	}
	stateBytes, _ := json.Marshal(state)
	now := time.Now()
	sess := &Session{
		State:        stateBytes,
		IsActive:     true,
		CreatedAt:    now,
		LastActiveAt: now,
	}

	ctx := buildResumeContext(sess, DefaultConfig())
	if ctx.TokensEstimated > ResumeTokenCap+100 {
		t.Errorf("token count not capped: got %d, want ≤ %d", ctx.TokensEstimated, ResumeTokenCap)
	}
}

func TestBuildResumeContext_FreshSession_NoContent(t *testing.T) {
	sess := &Session{
		ID:           uuid.New(),
		IssueID:      uuid.New(),
		AgentID:      uuid.New(),
		RunNumber:    1,
		State:        json.RawMessage(`{}`),
		IsActive:     true,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		Version:      1,
	}
	resumeCtx := buildResumeContext(sess, DefaultConfig())
	if resumeCtx.HasContent() {
		t.Error("fresh session with empty state should have no content")
	}
}

func TestResumeResult_FreshRun_IsNotResume(t *testing.T) {
	res := &ResumeResult{
		NewSession: &Session{RunNumber: 1},
		IsResume:   false,
	}
	if res.IsResume {
		t.Error("fresh run should not be IsResume")
	}
	if res.Context != nil {
		t.Error("fresh run should have nil context")
	}
}

func TestExtractDecisions_ToolResults(t *testing.T) {
	st := &StateData{
		ToolResults: []ToolResult{
			{ToolName: "decision", Output: "Use GraphQL instead of REST"},
			{ToolName: "code_review", Output: "Looks good"},
			{ToolName: "decision_note", Output: "Deploy to staging first"},
		},
	}
	decisions := extractDecisions(st)
	if len(decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(decisions))
	}
}

func TestExtractDecisions_NilState(t *testing.T) {
	d := extractDecisions(nil)
	if d != nil {
		t.Error("nil state should return nil decisions")
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	c := ResumeContext{}
	if tokens := estimateTokens(c); tokens != 0 {
		t.Errorf("expected 0 tokens for empty context, got %d", tokens)
	}
}

func TestEstimateTokens_WithSummary(t *testing.T) {
	c := ResumeContext{Summary: "one two three four five"}
	tokens := estimateTokens(c)
	if tokens < 1 {
		t.Errorf("expected >0 tokens, got %d", tokens)
	}
}

func TestTruncateByTokens_ShortString(t *testing.T) {
	s := "short"
	if got := truncateByTokens(s, 100); got != s {
		t.Errorf("short string should not be truncated, got %q", got)
	}
}

func TestTruncateByTokens_LongString(t *testing.T) {
	s := strings.Repeat("word ", 2000)
	got := truncateByTokens(s, 100)
	if !strings.HasSuffix(got, "…") {
		t.Error("truncated string should end with ellipsis")
	}
	if len(strings.Fields(got)) > 80 {
		t.Errorf("truncated string still too long: %d words", len(strings.Fields(got)))
	}
}

func TestHasContent_OnlyBranch(t *testing.T) {
	c := ResumeContext{Branch: "main"}
	if !c.HasContent() {
		t.Error("branch-only context should have content")
	}
}

func TestHasContent_Empty(t *testing.T) {
	c := ResumeContext{}
	if c.HasContent() {
		t.Error("empty context should not have content")
	}
}

func TestStringHelpers(t *testing.T) {
	if ptrStr(nil) != "" {
		t.Error("ptrStr(nil) should return empty string")
	}
	s := "hello"
	if ptrStr(&s) != "hello" {
		t.Error("ptrStr should dereference")
	}
	if strPtr("") != nil {
		t.Error("strPtr(\"\") should return nil")
	}
	p := strPtr("x")
	if p == nil || *p != "x" {
		t.Error("strPtr(\"x\") should return non-nil pointer")
	}
}

func TestResumer_FullCycle_WithMock(t *testing.T) {
	// Simulate: first run creates session, second run builds context.
	// We test through the mock test service (same package).
	tsvc := newTestService(DefaultConfig())
	ctx := context.Background()
	issueID := uuid.New()
	agentID := uuid.New()

	// Simulate first run: create session and store some state.
	sess1, err := tsvc.createSession(ctx, issueID, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if sess1.RunNumber != 1 {
		t.Errorf("expected run 1, got %d", sess1.RunNumber)
	}

	// Simulate storing state after first run.
	sess1.ConversationSummary = strPtr("Implemented JWT auth middleware with refresh tokens")
	sess1.Branch = strPtr("feature/auth")
	sess1.WorkingDirectory = strPtr("/workspace/app")
	sess1.FilesModified = []string{"auth/middleware.go", "auth/jwt.go"}

	// Second run: active session found → should produce resume context.
	sess2, err := tsvc.createSession(ctx, issueID, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if sess2.RunNumber != 2 {
		t.Errorf("expected run 2, got %d", sess2.RunNumber)
	}

	active, _ := tsvc.repo.getActive(ctx, issueID, agentID)
	if active == nil {
		t.Fatal("expected active session")
	}

	// Build context from the first session (simulating what Resumer does).
	stateBytes, _ := json.Marshal(sess1.State)
	simSess := &Session{
		ConversationSummary: sess1.ConversationSummary,
		Branch:              sess1.Branch,
		WorkingDirectory:    sess1.WorkingDirectory,
		FilesModified:       sess1.FilesModified,
		RunNumber:           sess1.RunNumber,
		State:               stateBytes,
		CreatedAt:           time.Now().Add(-time.Hour),
		LastActiveAt:        time.Now(),
	}
	resumeCtx := buildResumeContext(simSess, DefaultConfig())
	if !resumeCtx.HasContent() {
		t.Error("session with summary should have content")
	}
	if resumeCtx.ResumedFromRunNum != 1 {
		t.Errorf("expected resume from run 1, got %d", resumeCtx.ResumedFromRunNum)
	}
	if len(resumeCtx.FilesModified) != 2 {
		t.Errorf("expected 2 files, got %d", len(resumeCtx.FilesModified))
	}

	prompt := resumeCtx.BuildResumePrompt()
	if !strings.Contains(prompt, "Run #1") {
		t.Error("prompt should reference run number")
	}
	if !strings.Contains(prompt, "JWT auth") {
		t.Error("prompt should reference summary")
	}
	if !strings.Contains(prompt, "feature/auth") {
		t.Error("prompt should reference branch")
	}
}

func TestTruncateHelper(t *testing.T) {
	if truncate("", 10) != "" {
		t.Error("empty string")
	}
	if truncate("abc", 10) != "abc" {
		t.Error("short string not preserved")
	}
	if truncate("abcdef", 4) != "abc…" {
		t.Error("truncation failed")
	}
}
