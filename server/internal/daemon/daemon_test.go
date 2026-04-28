package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/repocache"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func createDaemonTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup failed: %s: %v", out, err)
		}
	}
	return dir
}

func TestNormalizeServerBaseURL(t *testing.T) {
	t.Parallel()

	got, err := NormalizeServerBaseURL("ws://localhost:8080/ws")
	if err != nil {
		t.Fatalf("NormalizeServerBaseURL returned error: %v", err)
	}
	if got != "http://localhost:8080" {
		t.Fatalf("expected http://localhost:8080, got %s", got)
	}
}

func TestBuildPromptContainsIssueID(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	prompt := BuildPrompt(Task{
		IssueID: issueID,
		Agent: &AgentData{
			Name: "Local Codex",
			Skills: []SkillData{
				{Name: "Concise", Content: "Be concise."},
			},
		},
	})

	// Prompt should contain the issue ID and CLI hint.
	for _, want := range []string{
		issueID,
		"multica issue get",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Skills should NOT be inlined in the prompt (they're in runtime config).
	for _, absent := range []string{"## Agent Skills", "Be concise."} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q (skills are in runtime config)", absent)
		}
	}
}

func TestBuildPromptNoIssueDetails(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID: "test-id",
		Agent:   &AgentData{Name: "Test"},
	})

	// Prompt should not contain issue title/description (agent fetches via CLI).
	for _, absent := range []string{"**Issue:**", "**Summary:**"} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("prompt should NOT contain %q — agent fetches details via CLI", absent)
		}
	}
}

func TestBuildPromptAutopilotRunOnly(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		AutopilotRunID:       "run-1",
		AutopilotID:          "autopilot-1",
		AutopilotTitle:       "Daily dependency check",
		AutopilotDescription: "Check dependencies and report outdated packages.",
		AutopilotSource:      "manual",
	})

	for _, want := range []string{
		"run-only mode",
		"Autopilot run ID: run-1",
		"Daily dependency check",
		"Check dependencies and report outdated packages.",
		"multica autopilot get autopilot-1 --output json",
		"Do not run `multica issue get`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("autopilot prompt missing %q\n---\n%s", want, prompt)
		}
	}

	if strings.Contains(prompt, "Your assigned issue ID is:") {
		t.Fatalf("autopilot prompt should not use issue assignment template\n---\n%s", prompt)
	}
}

func TestBuildPromptCommentTriggered(t *testing.T) {
	t.Parallel()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	commentID := "c1c2c3c4-d5d6-7890-abcd-ef1234567890"
	commentContent := "请把报告翻译成英文"

	prompt := BuildPrompt(Task{
		IssueID:               issueID,
		TriggerCommentID:      commentID,
		TriggerCommentContent: commentContent,
		Agent:                 &AgentData{Name: "Test"},
	})

	// Prompt should contain the comment content, the trigger comment id, and
	// the full reply command with --parent. Re-emitting --parent on every turn
	// is what prevents resumed sessions from reusing the previous turn's
	// --parent UUID.
	for _, want := range []string{
		issueID,
		commentContent,
		"Focus on THIS comment",
		commentID,
		"multica issue comment add " + issueID + " --parent " + commentID,
		"do NOT reuse --parent values from previous turns",
		// Silence-as-valid-exit for agent-to-agent loops depends on the
		// reply command being framed conditionally rather than as a hard
		// requirement. Guard the phrasing so the conflict with the new
		// workflow (MUL-1323) doesn't come back.
		"If you decide to reply",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// Should still contain CLI hint for fetching issue context.
	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint for issue context")
	}
}

// TestBuildPromptCommentTriggeredByAgent covers the agent-to-agent mention
// loop signal injected into the per-turn prompt (MUL-1323 / GH#1576). When
// the triggering comment was posted by another agent, the prompt must name
// the author, warn against sign-off @mentions, and point at silence as a
// valid exit.
func TestBuildPromptCommentTriggeredByAgent(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "thanks, looks good!",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Atlas",
		Agent:                 &AgentData{Name: "Test"},
	})

	for _, want := range []string{
		"Another agent (Atlas)",
		"do not @mention the other agent as a sign-off",
		"Silence is the preferred way",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
}

// TestBuildPromptCommentTriggeredByMember guards against the agent-loop warning
// leaking into human-authored triggers — a human asking a question should not
// be pre-discouraged from getting a reply.
func TestBuildPromptCommentTriggeredByMember(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "can you translate this?",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Alice",
		Agent:                 &AgentData{Name: "Test"},
	})

	if !strings.Contains(prompt, "A user just left a new comment") {
		t.Fatalf("member-triggered prompt should label the author as a user\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "Another agent") {
		t.Fatalf("member-triggered prompt should not claim the author was another agent")
	}
	// Must NOT use the old "You MUST respond" language — that conflicts with
	// the agent-to-agent silence-as-valid-exit workflow. Even on human-authored
	// triggers, the reply command is framed conditionally for a single
	// consistent rule across turn types.
	if strings.Contains(prompt, "MUST respond") {
		t.Fatalf("prompt should not contain unconditional \"MUST respond\" language\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "If you decide to reply") {
		t.Fatalf("prompt should frame the reply command conditionally\n---\n%s", prompt)
	}
}

func TestBuildPromptCommentTriggeredNoContent(t *testing.T) {
	t.Parallel()

	// When TriggerCommentID is set but content is empty (e.g. fetch failed),
	// it should still use the comment prompt path.
	prompt := BuildPrompt(Task{
		IssueID:          "test-id",
		TriggerCommentID: "comment-id",
		Agent:            &AgentData{Name: "Test"},
	})

	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint")
	}
}

func TestIsWorkspaceNotFoundError(t *testing.T) {
	t.Parallel()

	err := &requestError{
		Method:     http.MethodPost,
		Path:       "/api/daemon/register",
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"workspace not found"}`,
	}
	if !isWorkspaceNotFoundError(err) {
		t.Fatal("expected workspace not found error to be recognized")
	}

	if isWorkspaceNotFoundError(&requestError{StatusCode: http.StatusInternalServerError, Body: `{"error":"workspace not found"}`}) {
		t.Fatal("did not expect 500 to be treated as workspace not found")
	}
}

func TestMergeUsage(t *testing.T) {
	t.Parallel()

	a := map[string]agent.TokenUsage{
		"model-a": {InputTokens: 10, OutputTokens: 5},
	}
	b := map[string]agent.TokenUsage{
		"model-a": {InputTokens: 20, OutputTokens: 10, CacheReadTokens: 3},
		"model-b": {InputTokens: 100},
	}
	merged := mergeUsage(a, b)

	if got := merged["model-a"]; got.InputTokens != 30 || got.OutputTokens != 15 || got.CacheReadTokens != 3 {
		t.Fatalf("model-a: expected {30,15,3,0}, got %+v", got)
	}
	if got := merged["model-b"]; got.InputTokens != 100 {
		t.Fatalf("model-b: expected InputTokens=100, got %+v", got)
	}

	if got := mergeUsage(nil, b); len(got) != 2 {
		t.Fatal("mergeUsage(nil, b) should return b")
	}
	if got := mergeUsage(a, nil); len(got) != 1 {
		t.Fatal("mergeUsage(a, nil) should return a")
	}
}

// fakeBackend is a test double for agent.Backend that returns preconfigured
// results. Each call to Execute pops the next entry from the results slice.
type fakeBackend struct {
	calls   []agent.ExecOptions
	results []agent.Result
	errors  []error
	idx     atomic.Int32
}

func (b *fakeBackend) Execute(_ context.Context, _ string, opts agent.ExecOptions) (*agent.Session, error) {
	i := int(b.idx.Add(1)) - 1
	b.calls = append(b.calls, opts)
	if i < len(b.errors) && b.errors[i] != nil {
		return nil, b.errors[i]
	}
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result, 1)
	close(msgCh)
	resCh <- b.results[i]
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return &Daemon{
		client: NewClient(srv.URL),
		logger: slog.Default(),
	}
}

func newRepoReadyTestDaemon(t *testing.T, handler http.HandlerFunc) *Daemon {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Daemon{
		client:       NewClient(srv.URL),
		repoCache:    repocache.New(t.TempDir(), slog.Default()),
		logger:       slog.Default(),
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
	}
}

func TestExecuteAndDrain_ResumeFailureFallback(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)
	ctx := context.Background()
	taskLog := slog.Default()

	fb := &fakeBackend{
		results: []agent.Result{
			{Status: "failed", Error: "session not found", Usage: map[string]agent.TokenUsage{
				"m1": {InputTokens: 5},
			}},
			{Status: "completed", Output: "done", SessionID: "new-sess", Usage: map[string]agent.TokenUsage{
				"m1": {InputTokens: 10, OutputTokens: 20},
			}},
		},
	}

	// First attempt: resume fails (no SessionID in result).
	opts := agent.ExecOptions{ResumeSessionID: "stale-id"}
	result, _, err := d.executeAndDrain(ctx, fb, "prompt", opts, taskLog, "task-1")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if result.Status != "failed" || result.SessionID != "" {
		t.Fatalf("expected failed result with empty SessionID, got %+v", result)
	}

	// Simulate the retry logic from runTask.
	if result.Status == "failed" && result.SessionID == "" {
		firstUsage := result.Usage
		opts.ResumeSessionID = ""
		retryResult, _, retryErr := d.executeAndDrain(ctx, fb, "prompt", opts, taskLog, "task-1")
		if retryErr != nil {
			t.Fatalf("retry error: %v", retryErr)
		}
		result = retryResult
		result.Usage = mergeUsage(firstUsage, result.Usage)
	}

	if result.Status != "completed" || result.Output != "done" {
		t.Fatalf("expected completed result, got %+v", result)
	}
	if result.SessionID != "new-sess" {
		t.Fatalf("expected new-sess, got %s", result.SessionID)
	}
	// Usage should be merged.
	if u := result.Usage["m1"]; u.InputTokens != 15 || u.OutputTokens != 20 {
		t.Fatalf("expected merged usage {15,20}, got %+v", u)
	}
	// Second call should NOT have ResumeSessionID.
	if fb.calls[1].ResumeSessionID != "" {
		t.Fatal("retry should not have ResumeSessionID")
	}
}

func TestExecuteAndDrain_NoRetryWhenSessionEstablished(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	fb := &fakeBackend{
		results: []agent.Result{
			{Status: "failed", Error: "model error", SessionID: "valid-sess"},
		},
	}

	opts := agent.ExecOptions{ResumeSessionID: "some-id"}
	result, _, err := d.executeAndDrain(context.Background(), fb, "p", opts, slog.Default(), "t")
	if err != nil {
		t.Fatal(err)
	}

	// SessionID is set → session was established → should NOT retry.
	shouldRetry := result.Status == "failed" && result.SessionID == ""
	if shouldRetry {
		t.Fatal("should not retry when SessionID is present")
	}
	if int(fb.idx.Load()) != 1 {
		t.Fatalf("expected 1 call, got %d", fb.idx.Load())
	}
}

func TestExecuteAndDrain_CodexInactivityReportsToolResultTranscript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'` + "\n" +
		`read line` + "\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-drain"}}}'` + "\n" +
		`read line` + "\n" +
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thr-drain","turn":{"id":"turn-drain"}}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"item/started","params":{"threadId":"thr-drain","item":{"type":"commandExecution","id":"cmd-1","command":"git status"}}}'` + "\n" +
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-drain","item":{"type":"commandExecution","id":"cmd-1","aggregatedOutput":"clean"}}}'` + "\n" +
		`sleep 5` + "\n"
	if err := os.WriteFile(fakePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	if err := os.Chmod(fakePath, 0o755); err != nil {
		t.Fatalf("chmod fake codex: %v", err)
	}

	var mu sync.Mutex
	var reported []TaskMessageData
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/task-stale/messages" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Messages []TaskMessageData `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode task messages: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		reported = append(reported, body.Messages...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	backend, err := agent.New("codex", agent.Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new codex backend: %v", err)
	}
	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	result, tools, err := d.executeAndDrain(context.Background(), backend, "prompt", agent.ExecOptions{
		Timeout:                   5 * time.Second,
		SemanticInactivityTimeout: 100 * time.Millisecond,
	}, slog.Default(), "task-stale")
	if err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	if result.Status != "timeout" {
		t.Fatalf("expected timeout, got status=%q error=%q", result.Status, result.Error)
	}
	if tools != 1 {
		t.Fatalf("expected one tool use, got %d", tools)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		var gotToolUse, gotToolResult bool
		for _, msg := range reported {
			if msg.Seq == 1 && msg.Type == "tool_use" && msg.Tool == "exec_command" {
				gotToolUse = true
			}
			if msg.Seq == 2 && msg.Type == "tool_result" && msg.Tool == "exec_command" && msg.Output == "clean" {
				gotToolResult = true
			}
		}
		mu.Unlock()
		if gotToolUse && gotToolResult {
			return
		}
		if time.Now().After(deadline) {
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("expected tool_use seq=1 and tool_result seq=2 in transcript, got %+v", reported)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// blockingBackend returns a Session whose Result channel is never written to,
// so executeAndDrain can only exit via the drainCtx.Done() path.
type blockingBackend struct{}

func (blockingBackend) Execute(_ context.Context, _ string, _ agent.ExecOptions) (*agent.Session, error) {
	msgCh := make(chan agent.Message)
	resCh := make(chan agent.Result)
	close(msgCh)
	return &agent.Session{Messages: msgCh, Result: resCh}, nil
}

func TestExecuteAndDrain_ContextCancelled_ReportsCancelled(t *testing.T) {
	t.Parallel()

	d := newTestDaemon(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, _, err := d.executeAndDrain(ctx, blockingBackend{}, "p", agent.ExecOptions{}, slog.Default(), "t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "cancelled" {
		t.Fatalf("expected status=cancelled when parent ctx is cancelled, got %q (err=%q)", result.Status, result.Error)
	}
}

func TestEnsureRepoReadyFastPathDoesNotRefresh(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		http.Error(w, "unexpected refresh", http.StatusInternalServerError)
	})
	if err := d.repoCache.Sync("ws-1", []repocache.RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("seed repo cache: %v", err)
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "v1", []RepoData{{URL: sourceRepo}})

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("expected no refresh calls, got %d", got)
	}
}

func TestEnsureRepoReadyTrimsURL(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		http.Error(w, "unexpected refresh", http.StatusInternalServerError)
	})
	if err := d.repoCache.Sync("ws-1", []repocache.RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("seed repo cache: %v", err)
	}
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "v1", []RepoData{{URL: sourceRepo}})

	// URL with trailing whitespace should still hit the fast path.
	if err := d.ensureRepoReady(context.Background(), "ws-1", "  "+sourceRepo+"  "); err != nil {
		t.Fatalf("ensureRepoReady with padded URL: %v", err)
	}
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("expected no refresh calls for trimmed URL, got %d", got)
	}
}

func TestEnsureRepoReadyRefreshesOnMiss(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo, Description: "repo"}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil)

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 refresh call, got %d", got)
	}
	if d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		t.Fatal("expected repo to be cached after refresh")
	}
}

func TestEnsureRepoReadyReturnsNotConfigured(t *testing.T) {
	t.Parallel()

	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v1",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil)

	err := d.ensureRepoReady(context.Background(), "ws-1", "git@example.com:team/api.git")
	if !errors.Is(err, ErrRepoNotConfigured) {
		t.Fatalf("expected ErrRepoNotConfigured, got %v", err)
	}
}

func TestEnsureRepoReadyReportsSyncFailure(t *testing.T) {
	t.Parallel()

	missingRepo := filepath.Join(t.TempDir(), "missing-repo")
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: missingRepo, Description: "missing"}},
			ReposVersion: "v1",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil)

	err := d.ensureRepoReady(context.Background(), "ws-1", missingRepo)
	if err == nil || !strings.Contains(err.Error(), "repo is configured but not synced:") {
		t.Fatalf("expected sync failure error, got %v", err)
	}
	if got := d.workspaceLastRepoSyncErr("ws-1"); got == "" {
		t.Fatal("expected lastRepoSyncErr to be recorded")
	}
}

func TestEnsureRepoReadyConcurrentMissRefreshesOnce(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/daemon/workspaces/ws-1/repos" {
			http.NotFound(w, r)
			return
		}
		refreshCalls.Add(1)
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{{URL: sourceRepo, Description: "repo"}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil)

	const concurrency = 8
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- d.ensureRepoReady(context.Background(), "ws-1", sourceRepo)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("ensureRepoReady returned error: %v", err)
		}
	}
	// All 8 goroutines race on a cold miss; the per-workspace mutex
	// must serialize them so the server is only called once.
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", got)
	}
}

func TestShellArgsFromEnv(t *testing.T) {
	t.Setenv("MULTICA_CLAUDE_ARGS", `--max-turns 60 --append-system-prompt "multi word"`)
	got, err := shellArgsFromEnv("MULTICA_CLAUDE_ARGS")
	if err != nil {
		t.Fatalf("shellArgsFromEnv: %v", err)
	}
	want := []string{"--max-turns", "60", "--append-system-prompt", "multi word"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestShellArgsFromEnvEmptyIsNil(t *testing.T) {
	t.Setenv("MULTICA_CODEX_ARGS", "   ")
	got, err := shellArgsFromEnv("MULTICA_CODEX_ARGS")
	if err != nil {
		t.Fatalf("shellArgsFromEnv: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty env, got %#v", got)
	}
}

func TestDefaultArgsForProvider(t *testing.T) {
	cfg := Config{ClaudeArgs: []string{"--max-turns", "60"}, CodexArgs: []string{"--sandbox", "workspace-write"}}
	if got := defaultArgsForProvider(cfg, "claude"); strings.Join(got, " ") != "--max-turns 60" {
		t.Fatalf("unexpected claude args: %#v", got)
	}
	if got := defaultArgsForProvider(cfg, "codex"); strings.Join(got, " ") != "--sandbox workspace-write" {
		t.Fatalf("unexpected codex args: %#v", got)
	}
	if got := defaultArgsForProvider(cfg, "gemini"); got != nil {
		t.Fatalf("expected nil for unsupported provider, got %#v", got)
	}
}
