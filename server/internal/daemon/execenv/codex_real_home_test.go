package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

// Codex tasks keep the daemon user's real HOME on every platform. Earlier
// daemons redirected HOME/XDG into a seeded per-task `home/` directory under
// the env root so the Linux workspace-write sandbox could write to `~`; Linux
// now runs danger-full-access and that mechanism is gone (MUL-5578 / #6218).
//
// These tests pin the two halves that are easy to regress: the per-task home
// must not come back, and the task-scoped CODEX_HOME — a separate, still-live
// mechanism — must not be confused with it.

// legacyTaskHomeDirName is the directory older daemons created under the env
// root to hold the redirected HOME. Nothing writes it anymore; it is named here
// only so the tests can assert its absence.
const legacyTaskHomeDirName = "home"

func TestPrepareCodexKeepsRealHomeAndScopesOnlyCodexHome(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-codex-real-home",
		TaskID:         "b1c2d3e4-f5a6-7890-bcde-f01234567890",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "real-home-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := os.Lstat(filepath.Join(env.RootDir, legacyTaskHomeDirName)); !os.IsNotExist(err) {
		t.Errorf("per-task HOME directory must not be created under the env root (err=%v)", err)
	}

	// The task-scoped CODEX_HOME is a different mechanism and stays: it is
	// managed Codex state (config, sessions, skills, MCP), not the Unix HOME.
	if want := filepath.Join(env.RootDir, codexHomeDirName); env.CodexHome != want {
		t.Errorf("CodexHome = %q, want %q", env.CodexHome, want)
	}
	if _, err := os.Stat(env.CodexHome); err != nil {
		t.Errorf("task-scoped CODEX_HOME should exist: %v", err)
	}
}

func TestPrepareCodexLocalDirectoryChatKeepsRolloutAcrossTaskIDs(t *testing.T) {
	// Given: a direct chat runs against the same local directory in two tasks.
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	workspacesRoot := t.TempDir()
	localWorkDir := t.TempDir()
	task := TaskContextForEnv{
		AgentID:       "agent-chat-resume",
		ChatSessionID: "chat-session-resume",
	}

	first, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-chat-resume",
		TaskID:         "11111111-1111-1111-1111-111111111111",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		LocalWorkDir:   localWorkDir,
		Task:           task,
	}, testLogger())
	if err != nil {
		t.Fatalf("first Prepare failed: %v", err)
	}
	sessionID := "019fe3de-56a2-7792-8913-ee53664dfa0e"
	seedRolloutAt(t, filepath.Join(first.CodexHome, "sessions", "2026", "08", "09", "rollout-2026-08-09T09-13-47-"+sessionID+".jsonl"), 32)
	if err := first.Cleanup(true); err != nil {
		t.Fatalf("cleanup first task: %v", err)
	}

	// When: the follow-up task prepares a fresh task-scoped CODEX_HOME.
	second, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-chat-resume",
		TaskID:         "22222222-2222-2222-2222-222222222222",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		LocalWorkDir:   localWorkDir,
		Task:           task,
	}, testLogger())
	if err != nil {
		t.Fatalf("second Prepare failed: %v", err)
	}
	defer second.Cleanup(true)

	// Then: the prior rollout is visible, so the daemon can resume the chat.
	if !CodexResumeRolloutPresent(second.CodexHome, sessionID) {
		t.Fatal("follow-up direct chat cannot see the prior rollout; context would be dropped")
	}
}

func TestCodexSessionStoreKeyUsesChatSessionIDWhenIssueAbsent(t *testing.T) {
	// Given: two direct chats belong to the same agent and have no issue ID.
	const agentID = "agent-chat-key"
	const chatA = "chat-session-a"
	const chatB = "chat-session-b"
	taskA := TaskContextForEnv{AgentID: agentID, ChatSessionID: chatA}

	// When: their persistent Codex session-store keys are built.
	keyA := codexSessionStoreKey("", taskA)
	keyAAgain := codexSessionStoreKey("", taskA)
	keyB := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, ChatSessionID: chatB})

	// Then: the key is stable for one chat and isolated from another chat.
	if keyA == "" {
		t.Fatal("direct chat must have a persistent Codex session-store key")
	}
	if keyA != keyAAgain {
		t.Fatalf("same chat produced unstable keys: %q and %q", keyA, keyAAgain)
	}
	if keyA == keyB {
		t.Fatalf("different chats share one Codex session-store key: %q", keyA)
	}
}

// TestReuseCodexToleratesLegacyTaskHome covers an env root prepared by an older
// daemon: its leftover `home/` directory (including the credential symlinks it
// seeded) must neither break reuse nor be adopted as a HOME again.
func TestReuseCodexToleratesLegacyTaskHome(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-codex-legacy-home",
		TaskID:         "c1d2e3f4-a5b6-7890-cdef-012345678901",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "legacy-home-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// Simulate what an older daemon left behind.
	legacyHome := filepath.Join(env.RootDir, legacyTaskHomeDirName)
	if err := os.MkdirAll(filepath.Join(legacyHome, ".config"), 0o700); err != nil {
		t.Fatalf("seed legacy task home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyHome, ".npmrc"), []byte("//registry:_authToken=x\n"), 0o600); err != nil {
		t.Fatalf("seed legacy .npmrc: %v", err)
	}

	reused := Reuse(ReuseParams{
		WorkDir:  env.WorkDir,
		Provider: "codex",
		Task:     TaskContextForEnv{IssueID: "legacy-home-test"},
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil on an env root carrying a legacy task home")
	}
	if reused.CodexHome == "" {
		t.Error("expected CodexHome to be restored on reuse")
	}
}
