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

func TestTriggerRestart_BrewLinuxCellarDeleted(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
	})

	prefix := filepath.Join(t.TempDir(), "home", "linuxbrew", ".linuxbrew")
	deletedCellarPath := filepath.Join(prefix, "Cellar", "multica", "0.2.9", "bin", "multica")
	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return prefix }

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	want := filepath.Join(prefix, "bin", "multica")
	if got := d.RestartBinary(); got != want {
		t.Fatalf("restart binary = %q, want %q", got, want)
	}
	if got := d.RestartBinary(); got == deletedCellarPath {
		t.Fatalf("restart binary used deleted Cellar path %q", got)
	}
}

// When `brew --prefix` is unavailable but the executable path is under a
// known Cellar root, triggerRestart must recover the prefix from the
// known-prefix list and target <prefix>/bin/multica.
func TestTriggerRestart_BrewPrefixUnavailable_FallsBackToKnownPrefix(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	originalMatchKnownBrewPrefix := matchKnownBrewPrefix
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
		matchKnownBrewPrefix = originalMatchKnownBrewPrefix
	})

	const knownPrefix = "/home/linuxbrew/.linuxbrew"
	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return "" }
	matchKnownBrewPrefix = func(string) string { return knownPrefix }

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	want := filepath.Join(knownPrefix, "bin", "multica")
	if got := d.RestartBinary(); got != want {
		t.Fatalf("restart binary = %q, want %q", got, want)
	}
}

// When `brew --prefix` is unavailable AND the executable is not under any
// known Cellar root, triggerRestart logs a warning and keeps the executable
// path (no fabricated <prefix>/bin/multica path).
func TestTriggerRestart_BrewPrefixUnavailable_NoKnownPrefix_KeepsExecutable(t *testing.T) {
	originalIsBrewInstall := isBrewInstall
	originalGetBrewPrefix := getBrewPrefix
	originalMatchKnownBrewPrefix := matchKnownBrewPrefix
	t.Cleanup(func() {
		isBrewInstall = originalIsBrewInstall
		getBrewPrefix = originalGetBrewPrefix
		matchKnownBrewPrefix = originalMatchKnownBrewPrefix
	})

	isBrewInstall = func() bool { return true }
	getBrewPrefix = func() string { return "" }
	matchKnownBrewPrefix = func(string) string { return "" }

	d := &Daemon{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.triggerRestart()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if got := d.RestartBinary(); got != exe {
		t.Fatalf("restart binary = %q, want unchanged executable %q", got, exe)
	}
}

func TestNewTaskSlotSemaphoreReturnsStableSlotIndexes(t *testing.T) {
	t.Parallel()

	sem := newTaskSlotSemaphore(4)
	seen := make(map[int]bool)
	for i := 0; i < 4; i++ {
		select {
		case slot := <-sem:
			if slot < 0 || slot > 3 {
				t.Fatalf("slot out of range: %d", slot)
			}
			if seen[slot] {
				t.Fatalf("duplicate slot: %d", slot)
			}
			seen[slot] = true
		default:
			t.Fatalf("expected slot %d to be available", i)
		}
	}

	select {
	case slot := <-sem:
		t.Fatalf("expected semaphore to be empty, got slot %d", slot)
	default:
	}

	sem <- 2
	select {
	case slot := <-sem:
		if slot != 2 {
			t.Fatalf("expected released slot 2, got %d", slot)
		}
	default:
		t.Fatal("expected released slot to be available")
	}
}

func TestProviderNeedsInlineSystemPrompt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		want     bool
	}{
		{provider: "openclaw", want: true},
		// Hermes ACP starts in the task cwd and loads AGENTS.md / .agent_context
		// directly. Inlining the full runtime brief duplicates that context and
		// can trip upstream provider safety filters on otherwise harmless tasks.
		{provider: "hermes", want: false},
		{provider: "kiro", want: true},
		{provider: "kimi", want: true},
		{provider: "codex", want: false},
		{provider: "claude", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			if got := providerNeedsInlineSystemPrompt(tc.provider); got != tc.want {
				t.Fatalf("providerNeedsInlineSystemPrompt(%q) = %v, want %v", tc.provider, got, tc.want)
			}
		})
	}
}

// TestComposeOpenclawIncludeRoots — the Elon must-fix regression: the
// daemon must grant OpenClaw permission to follow the wrapper's $include
// link from envRoot into the user's active config dir, while preserving
// any roots the user already configured in their shell env so their own
// cross-directory layouts keep working.
func TestComposeOpenclawIncludeRoots(t *testing.T) {
	t.Parallel()

	sep := string(os.PathListSeparator)
	cases := []struct {
		name    string
		add     string
		user    string
		want    string
		wantSet bool
	}{
		{
			// Fresh install — preparer emits no $include, so daemon
			// shouldn't touch OPENCLAW_INCLUDE_ROOTS at all.
			name:    "fresh_install_no_root_to_grant",
			add:     "",
			user:    "/some/user/dir",
			wantSet: false,
		},
		{
			// User has no existing value — output is just the granted dir.
			name:    "no_user_value",
			add:     "/home/alice/.openclaw",
			user:    "",
			want:    "/home/alice/.openclaw",
			wantSet: true,
		},
		{
			// User has their own include roots — daemon must prepend
			// granted dir AND preserve user's entries verbatim.
			name:    "preserves_user_value",
			add:     "/home/alice/.openclaw",
			user:    "/etc/openclaw" + sep + "/opt/openclaw/shared",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw" + sep + "/opt/openclaw/shared",
			wantSet: true,
		},
		{
			// User's value already contains the granted dir — daemon
			// must dedupe rather than emit a redundant entry that would
			// trip OpenClaw confused-deputy heuristics.
			name:    "dedupes_when_user_already_grants_same_dir",
			add:     "/home/alice/.openclaw",
			user:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			wantSet: true,
		},
		{
			// Stray empty segments from a malformed user env are skipped.
			name:    "skips_empty_segments_in_user_value",
			add:     "/home/alice/.openclaw",
			user:    "" + sep + "/etc/openclaw" + sep + "",
			want:    "/home/alice/.openclaw" + sep + "/etc/openclaw",
			wantSet: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := composeOpenclawIncludeRoots(tc.add, tc.user)
			if ok != tc.wantSet {
				t.Fatalf("ok = %v, want %v (got = %q)", ok, tc.wantSet, got)
			}
			if got != tc.want {
				t.Errorf("got = %q, want %q", got, tc.want)
			}
		})
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
	}, "claude")

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
	}, "claude")

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
	}, "claude")

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
	}, "claude")

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
	}, "claude")

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
	}, "claude")

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
	}, "claude")

	if !strings.Contains(prompt, "multica issue get") {
		t.Fatal("prompt missing CLI hint")
	}
}

// TestBuildPromptSquadLeaderNoActionProhibition verifies that when a squad
// leader is triggered by another agent's comment, the per-turn prompt
// explicitly forbids posting a comment whose only purpose is to announce
// no_action or "exiting silently". This is the fix for MUL-2168.
func TestBuildPromptSquadLeaderNoActionProhibition(t *testing.T) {
	t.Parallel()

	prompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "Progress update: tests passing.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Worker",
		Agent: &AgentData{
			Name:         "Leader",
			Instructions: "You lead the team.\n\n## Squad Operating Protocol\n\nYou are the LEADER.",
		},
	}, "claude")

	for _, want := range []string{
		"Squad leader no_action rule",
		"DO NOT post any comment",
		"multica squad activity",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("squad leader prompt missing %q\n---\n%s", want, prompt)
		}
	}

	// Non-squad-leader agent should NOT get the squad leader rule.
	nonLeaderPrompt := BuildPrompt(Task{
		IssueID:               "issue-1",
		TriggerCommentID:      "comment-1",
		TriggerCommentContent: "Progress update: tests passing.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "Worker",
		Agent: &AgentData{
			Name:         "Regular",
			Instructions: "You are a regular agent.",
		},
	}, "claude")

	if strings.Contains(nonLeaderPrompt, "Squad leader no_action rule") {
		t.Fatalf("non-squad-leader prompt should NOT contain squad leader rule\n---\n%s", nonLeaderPrompt)
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

func TestIsTaskNotFoundError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 with task not found body",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/tasks/abc/messages",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"task not found"}`,
			},
			want: true,
		},
		{
			name: "404 with mixed-case body still matches",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"Task Not Found"}`,
			},
			want: true,
		},
		{
			name: "500 with same body is not task-not-found",
			err: &requestError{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"task not found"}`,
			},
			want: false,
		},
		{
			name: "404 with workspace-not-found body is not task-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"workspace not found"}`,
			},
			want: false,
		},
		{
			name: "non-requestError",
			err:  errors.New("network down"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isTaskNotFoundError(tc.err); got != tc.want {
				t.Fatalf("isTaskNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsRuntimeNotFoundError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "404 with runtime not found body from heartbeat",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/heartbeat",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"runtime not found"}`,
			},
			want: true,
		},
		{
			name: "404 with runtime not found body from claim",
			err: &requestError{
				Method:     http.MethodPost,
				Path:       "/api/daemon/runtimes/abc/tasks/claim",
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"runtime not found"}`,
			},
			want: true,
		},
		{
			name: "mixed-case body still matches",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"Runtime Not Found"}`,
			},
			want: true,
		},
		{
			name: "500 with same body must NOT be treated as runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error":"runtime not found"}`,
			},
			want: false,
		},
		{
			name: "404 with task-not-found body is not runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"task not found"}`,
			},
			want: false,
		},
		{
			name: "404 with workspace-not-found body is not runtime-not-found",
			err: &requestError{
				StatusCode: http.StatusNotFound,
				Body:       `{"error":"workspace not found"}`,
			},
			want: false,
		},
		{
			name: "non-requestError",
			err:  errors.New("network down"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isRuntimeNotFoundError(tc.err); got != tc.want {
				t.Fatalf("isRuntimeNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestShouldInterruptAgent(t *testing.T) {
	t.Parallel()

	notFound := &requestError{
		StatusCode: http.StatusNotFound,
		Body:       `{"error":"task not found"}`,
	}
	transient := &requestError{
		StatusCode: http.StatusBadGateway,
		Body:       `<html>...</html>`,
	}

	cases := []struct {
		name   string
		status string
		err    error
		want   bool
	}{
		{name: "status cancelled", status: "cancelled", err: nil, want: true},
		{name: "task deleted (404)", status: "", err: notFound, want: true},
		{name: "running normally", status: "running", err: nil, want: false},
		{name: "transient 5xx is not a cancel signal", status: "", err: transient, want: false},
		{name: "no information yet", status: "", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldInterruptAgent(tc.status, tc.err); got != tc.want {
				t.Fatalf("shouldInterruptAgent(%q, %v) = %v, want %v", tc.status, tc.err, got, tc.want)
			}
		})
	}
}

// TestWatchTaskCancellation_TaskDeleted reproduces the zombie-task bug:
// when the server deletes a task while it is running (issue removed,
// agent reassigned, etc.), GetTaskStatus starts returning 404. Before the
// fix the daemon kept polling and never interrupted the running agent —
// codex would keep emitting tool calls for minutes against a dead task.
//
// After the fix, watchTaskCancellation must close its channel within a
// few poll intervals so the caller can cancel the agent context.
func TestWatchTaskCancellation_TaskDeleted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"task not found"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-deleted", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
		// Expected: the watcher detected the 404 and signalled cancellation.
	case <-time.After(2 * time.Second):
		t.Fatal("watchTaskCancellation did not signal cancellation when task was deleted (404)")
	}
}

// TestWatchTaskCancellation_StatusCancelled keeps the existing behaviour
// (server transitions task status to "cancelled") working alongside the
// new 404 path.
func TestWatchTaskCancellation_StatusCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"cancelled"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-cancelled", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTaskCancellation did not signal cancellation when status=cancelled")
	}
}

// TestWatchTaskCancellation_RunningTaskNotInterrupted ensures the watcher
// does NOT trigger on transient errors or while the task is still running.
func TestWatchTaskCancellation_RunningTaskNotInterrupted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cancelled := d.watchTaskCancellation(ctx, "task-running", 10*time.Millisecond, slog.Default())

	select {
	case <-cancelled:
		t.Fatal("watchTaskCancellation should not signal cancellation while task is running")
	case <-time.After(150 * time.Millisecond):
	}
	if calls.Load() < 5 {
		t.Fatalf("expected the watcher to poll at least 5 times in 150ms, got %d", calls.Load())
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
	d := &Daemon{
		client:       NewClient(srv.URL),
		repoCache:    repocache.New(t.TempDir(), slog.Default()),
		logger:       slog.Default(),
		workspaces:   make(map[string]*workspaceState),
		runtimeIndex: make(map[string]Runtime),
	}
	// Drain background syncs (started by registerTaskRepos) before the
	// t.TempDir cache root is cleaned up, otherwise an in-flight clone/fetch
	// races against the deletion and the test fails with a misleading
	// "directory not empty" cleanup error.
	t.Cleanup(d.waitBackgroundSyncs)
	return d
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
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "v1", []RepoData{{URL: sourceRepo}}, nil)

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
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "v1", []RepoData{{URL: sourceRepo}}, nil)

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
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

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

// A project github_repo URL that the workspace itself does not bind must still
// be allowed for `multica repo checkout` after registerTaskRepos runs. Without
// this, the new project-repos-override-workspace-repos behavior would surface
// repos in the meta-skill that the agent then can't actually clone.
func TestRegisterTaskReposAllowsProjectOnlyURL(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	var refreshCalls atomic.Int32
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		// If the workspace endpoint is hit it returns an empty list — the
		// project-only URL must NOT depend on this for allowlist membership.
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v1",
		})
	})
	// Workspace has zero workspace-bound repos; the project resource gives us
	// the only repo URL the agent should be able to check out.
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

	d.registerTaskRepos("ws-1", []RepoData{{URL: sourceRepo}})

	// The async clone goroutine in registerTaskRepos may not have finished;
	// poll briefly until the cache is populated so the test isn't racy.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d.repoCache.Lookup("ws-1", sourceRepo) != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		t.Fatalf("expected repo to be cached after registerTaskRepos, but Lookup returned empty")
	}

	if !d.workspaceRepoAllowed("ws-1", sourceRepo) {
		t.Fatal("expected project repo to pass workspaceRepoAllowed")
	}

	if err := d.ensureRepoReady(context.Background(), "ws-1", sourceRepo); err != nil {
		t.Fatalf("ensureRepoReady: %v", err)
	}
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("expected zero workspace-repos refreshes (URL came from project), got %d", got)
	}
}

// Confirms that a workspace refresh wiping allowedRepoURLs does not also wipe
// task-scoped URLs (project repos). Without the separate taskRepoURLs map a
// concurrent refresh would silently revoke project-only URLs and the next
// checkout would fail.
func TestRegisterTaskReposSurvivesWorkspaceRefresh(t *testing.T) {
	t.Parallel()

	sourceRepo := createDaemonTestRepo(t)
	d := newRepoReadyTestDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID:  "ws-1",
			Repos:        []RepoData{},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)
	d.registerTaskRepos("ws-1", []RepoData{{URL: sourceRepo}})

	// Wait for the registration to populate the cache.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.repoCache.Lookup("ws-1", sourceRepo) == "" {
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := d.refreshWorkspaceRepos(context.Background(), "ws-1"); err != nil {
		t.Fatalf("refreshWorkspaceRepos: %v", err)
	}

	if !d.workspaceRepoAllowed("ws-1", sourceRepo) {
		t.Fatal("project repo URL was wiped by workspace refresh")
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
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

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
			Repos:        []RepoData{{URL: missingRepo}},
			ReposVersion: "v1",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

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
			Repos:        []RepoData{{URL: sourceRepo}},
			ReposVersion: "v2",
		})
	})
	d.workspaces["ws-1"] = newWorkspaceState("ws-1", nil, "", nil, nil)

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

// reportTaskResultRecorder captures which terminal endpoint
// (.../complete or .../fail) reportTaskResult hits and the body it
// posts, so the tests can assert the disposition (success vs fail)
// independently of the rest of handleTask.
type reportTaskResultRecorder struct {
	mu      sync.Mutex
	path    string
	method  string
	payload map[string]any
}

func (r *reportTaskResultRecorder) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var payload map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		r.mu.Lock()
		r.path = req.URL.Path
		r.method = req.Method
		r.payload = payload
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
}

func TestReportTaskResult_CompletedHitsCompleteEndpoint(t *testing.T) {
	t.Parallel()

	rec := &reportTaskResultRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-1", TaskResult{
		Status:     "completed",
		Comment:    "all good",
		BranchName: "agent/foo",
		SessionID:  "ses-1",
		WorkDir:    "/tmp/foo",
	}, slog.Default())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.path != "/api/daemon/tasks/task-1/complete" {
		t.Fatalf("expected /complete endpoint, got %s", rec.path)
	}
	if rec.payload["output"] != "all good" {
		t.Errorf("output: got %v", rec.payload["output"])
	}
	if rec.payload["branch_name"] != "agent/foo" {
		t.Errorf("branch_name: got %v", rec.payload["branch_name"])
	}
	if rec.payload["session_id"] != "ses-1" {
		t.Errorf("session_id: got %v", rec.payload["session_id"])
	}
}

// Pins the GitHub multica#1952 fail-closed behaviour: a task whose
// agent run never produced a real result (blocked, cancelled, or any
// future status we forget to enumerate) MUST go through FailTask, so
// the UI never shows a green "Completed" badge for a run that didn't
// actually do anything (e.g. provider 429 / out-of-credit).
func TestReportTaskResult_NonCompletedHitsFailEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		status            string
		failureReasonIn   string
		wantFailureReason string
	}{
		{
			name:              "blocked with explicit reason preserves it",
			status:            "blocked",
			failureReasonIn:   "iteration_limit",
			wantFailureReason: "iteration_limit",
		},
		{
			name:              "blocked without reason defaults to agent_error",
			status:            "blocked",
			failureReasonIn:   "",
			wantFailureReason: "agent_error",
		},
		{
			name:              "cancelled defaults to cancelled reason",
			status:            "cancelled",
			failureReasonIn:   "",
			wantFailureReason: "cancelled",
		},
		{
			name:              "unknown status fails closed",
			status:            "weird_new_status",
			failureReasonIn:   "",
			wantFailureReason: "agent_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &reportTaskResultRecorder{}
			srv := httptest.NewServer(rec.handler(t))
			t.Cleanup(srv.Close)

			d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
			d.reportTaskResult(context.Background(), "task-x", TaskResult{
				Status:        tc.status,
				Comment:       "rate limit reached",
				SessionID:     "ses-x",
				WorkDir:       "/tmp/x",
				FailureReason: tc.failureReasonIn,
			}, slog.Default())

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.path != "/api/daemon/tasks/task-x/fail" {
				t.Fatalf("expected /fail endpoint for status=%q, got %s", tc.status, rec.path)
			}
			if rec.payload["error"] != "rate limit reached" {
				t.Errorf("error body: got %v", rec.payload["error"])
			}
			if got := rec.payload["failure_reason"]; got != tc.wantFailureReason {
				t.Errorf("failure_reason: got %v, want %q", got, tc.wantFailureReason)
			}
			if rec.payload["session_id"] != "ses-x" {
				t.Errorf("session_id should be forwarded on failure paths so chat resume keeps working, got %v", rec.payload["session_id"])
			}
		})
	}
}

// TestHandleTask_ReportsUsageBeforeCancel verifies that ReportTaskUsage is called
// even when the server marks the task as cancelled during the post-run status
// check. Regression test for the ordering bug where the cancel check ran before
// usage was reported, silently discarding accumulated tokens.
func TestHandleTask_ReportsUsageBeforeCancel(t *testing.T) {
	t.Parallel()

	var callOrder []string
	var mu sync.Mutex
	recordCall := func(name string) {
		mu.Lock()
		callOrder = append(callOrder, name)
		mu.Unlock()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			recordCall("start")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			recordCall("usage")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			recordCall("status")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: time.Hour, // effectively disable poll-cancel path; we want the post-run status check
	}

	// Inject a fake runner that returns a result with usage tokens, bypassing
	// real agent process execution.
	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		return TaskResult{
			Status: "completed",
			Usage: []TaskUsageEntry{
				{Provider: "anthropic", Model: "claude-opus-4-6", InputTokens: 100, OutputTokens: 50},
			},
		}, nil
	})

	task := Task{
		ID:        "task-abc",
		RuntimeID: "rt-1",
		IssueID:   "issue-xyz",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	// usage must appear before status in the call order.
	usageIdx, statusIdx := -1, -1
	for i, name := range order {
		switch name {
		case "usage":
			usageIdx = i
		case "status":
			statusIdx = i
		}
	}

	if usageIdx == -1 {
		t.Fatal("ReportTaskUsage was never called — usage is lost for cancelled tasks")
	}
	if statusIdx == -1 {
		t.Fatal("GetTaskStatus was never called")
	}
	if usageIdx > statusIdx {
		t.Fatalf("usage was reported AFTER status check (order: %v) — regression", order)
	}
}

// TestHandleTask_ReportsUsageWhenCancelledByPoll verifies that ReportTaskUsage is
// called even when the task is cancelled mid-execution by the poll goroutine.
// Regression test for the cancelledByPoll early-return path that previously
// discarded accumulated usage before calling ReportTaskUsage.
func TestHandleTask_ReportsUsageWhenCancelledByPoll(t *testing.T) {
	t.Parallel()

	var callOrder []string
	var mu sync.Mutex
	recordCall := func(name string) {
		mu.Lock()
		callOrder = append(callOrder, name)
		mu.Unlock()
	}

	// statusCallCount lets the poll goroutine return "cancelled" on first call
	// while still handling later calls from the post-run status check.
	var statusCallCount atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			recordCall("usage")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			// First call is from the poll goroutine — return "cancelled" to
			// trigger runCancel() and close(cancelledByPoll).
			if statusCallCount.Add(1) == 1 {
				recordCall("poll-status")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"cancelled"}`))
			} else {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:         make(map[string]*workspaceState),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "claude"}},
		cancelPollInterval: 10 * time.Millisecond, // fire quickly so test is fast
	}

	// Inject a runner that blocks until runCtx is cancelled (simulating a real
	// agent being interrupted), then returns usage tokens as claude.go does.
	d.runner = taskRunnerFunc(func(runCtx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-runCtx.Done()
		return TaskResult{
			Status: "aborted",
			Usage: []TaskUsageEntry{
				{Provider: "anthropic", Model: "claude-opus-4-6", InputTokens: 200, OutputTokens: 80},
			},
		}, nil
	})

	task := Task{
		ID:        "task-poll",
		RuntimeID: "rt-1",
		IssueID:   "issue-poll",
		Agent:     &AgentData{Name: "test-agent"},
	}

	d.handleTask(context.Background(), task, 0)

	mu.Lock()
	order := make([]string, len(callOrder))
	copy(order, callOrder)
	mu.Unlock()

	// Verify the poll goroutine actually fired — without this assertion the test
	// could pass via the post-run GetTaskStatus check without ever taking the
	// cancelledByPoll path, making it a vacuous regression guard.
	pollStatusIdx := -1
	usageIdx := -1
	for i, name := range order {
		switch name {
		case "poll-status":
			pollStatusIdx = i
		case "usage":
			usageIdx = i
		}
	}
	if pollStatusIdx == -1 {
		t.Fatalf("poll goroutine never fired (order: %v) — cancelledByPoll path not exercised", order)
	}
	if usageIdx == -1 {
		t.Fatalf("ReportTaskUsage was never called on poll-cancelled path (order: %v) — tokens lost", order)
	}
	// poll-status must precede usage: poll fires → runCtx cancelled → runner unblocks → usage flushed.
	// If usage comes first, usage was reported before the runner was interrupted, which is impossible
	// given that the runner blocks on runCtx.Done().
	if usageIdx < pollStatusIdx {
		t.Fatalf("usage reported before poll-status (order: %v) — poll-status must come first", order)
	}
}
