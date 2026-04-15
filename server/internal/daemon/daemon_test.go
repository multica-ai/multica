package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

	// Prompt should contain the comment content directly.
	for _, want := range []string{
		issueID,
		commentContent,
		"comment that triggered this task",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}

	// Should still contain CLI hint for fetching issue context.
	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint for issue context")
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

func TestResolveTaskModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		runtimeConfig   any
		providerDefault string
		want            string
	}{
		{
			name:            "no runtime config uses provider default",
			runtimeConfig:   nil,
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
		{
			name: "absent model uses provider default and ignores extra keys",
			runtimeConfig: map[string]any{
				"temperature": 0.2,
				"notes":       "preserved elsewhere",
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
		{
			name: "string model overrides provider default",
			runtimeConfig: map[string]any{
				"model": "gpt-5.3-codex-spark",
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.3-codex-spark",
		},
		{
			name: "whitespace around model is trimmed",
			runtimeConfig: map[string]any{
				"model": "  gpt-5.3-codex-spark  ",
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.3-codex-spark",
		},
		{
			name: "empty model uses provider default",
			runtimeConfig: map[string]any{
				"model": "",
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
		{
			name: "whitespace model uses provider default",
			runtimeConfig: map[string]any{
				"model": " \t\n ",
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
		{
			name: "non-string model uses provider default",
			runtimeConfig: map[string]any{
				"model": 123,
			},
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
		{
			name:            "raw json runtime config model overrides provider default",
			runtimeConfig:   json.RawMessage(`{"model":"gpt-5.3-codex-spark","keep":"value"}`),
			providerDefault: "gpt-5.4",
			want:            "gpt-5.3-codex-spark",
		},
		{
			name:            "invalid json uses provider default",
			runtimeConfig:   json.RawMessage(`{"model":`),
			providerDefault: "gpt-5.4",
			want:            "gpt-5.4",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveTaskModel(tt.providerDefault, tt.runtimeConfig); got != tt.want {
				t.Fatalf("resolveTaskModel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskModelDoesNotMutateRuntimeConfig(t *testing.T) {
	t.Parallel()

	runtimeConfig := map[string]any{
		"model": "gpt-5.3-codex-spark",
		"keep":  "value",
	}
	if got := resolveTaskModel("gpt-5.4", runtimeConfig); got != "gpt-5.3-codex-spark" {
		t.Fatalf("resolveTaskModel() = %q, want gpt-5.3-codex-spark", got)
	}
	if got := runtimeConfig["keep"]; got != "value" {
		t.Fatalf("runtime config extra key changed: got %v", got)
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

func TestRunTaskPassesResolvedModelToExecOptions(t *testing.T) {
	oldNewAgentBackend := newAgentBackend
	t.Cleanup(func() {
		newAgentBackend = oldNewAgentBackend
	})

	tests := []struct {
		name          string
		runtimeConfig any
		wantModel     string
	}{
		{
			name:      "no runtime config passes provider default",
			wantModel: "gpt-5.4",
		},
		{
			name: "runtime config model override is passed",
			runtimeConfig: map[string]any{
				"model":       "gpt-5.3-codex-spark",
				"temperature": 0.2,
			},
			wantModel: "gpt-5.3-codex-spark",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeBackend{
				results: []agent.Result{{
					Status: "completed",
					Output: "done",
				}},
			}
			newAgentBackend = func(agentType string, cfg agent.Config) (agent.Backend, error) {
				if agentType != "claude" {
					t.Fatalf("agent type = %q, want claude", agentType)
				}
				return backend, nil
			}

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			d := New(Config{
				ServerBaseURL:  "http://example.test",
				Agents:         map[string]AgentEntry{"claude": {Path: "claude", Model: "gpt-5.4"}},
				WorkspacesRoot: t.TempDir(),
				AgentTimeout:   time.Second,
			}, logger)

			_, err := d.runTask(context.Background(), Task{
				ID:          "task-00000001",
				AgentID:     "agent-1",
				IssueID:     "issue-1",
				WorkspaceID: "workspace-1",
				Agent: &AgentData{
					ID:            "agent-1",
					Name:          "Test Agent",
					Instructions:  "Test instructions",
					RuntimeConfig: tt.runtimeConfig,
				},
			}, "claude", logger)
			if err != nil {
				t.Fatalf("runTask() error = %v", err)
			}
			if backend.calls[0].Model != tt.wantModel {
				t.Fatalf("ExecOptions.Model = %q, want %q", backend.calls[0].Model, tt.wantModel)
			}
		})
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
