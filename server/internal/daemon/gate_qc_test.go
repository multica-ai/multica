package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestQuickCreateWriteThroughGCFreshPrepareAndResumeGates(t *testing.T) {
	const (
		workspaceID = "019f59d9-a6aa-7a53-b173-1eccc4b4c876"
		sourceID    = "019f59d9-a6aa-7a53-b173-1eccc4b4c877"
		freshID     = "019f59d9-a6aa-7a53-b173-1eccc4b4c878"
		issueID     = "019f59d9-a6aa-7a53-b173-1eccc4b4c879"
		sessionID   = "019f59d9-a6aa-7a53-b173-1eccc4b4c880"
		agentID     = "agent-qc-gc"
	)
	scope := "qc_" + sourceID
	workspacesRoot := filepath.Join(t.TempDir(), "workspaces")
	sharedHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", sharedHome)
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		t.Fatalf("create workspaces root: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/tasks/"+sourceID+"/gc-check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
	})
	d := newGCTestDaemon(t, mux)
	d.cfg.WorkspacesRoot = workspacesRoot

	sourceTask := execenv.TaskContextForEnv{
		AgentID:           agentID,
		SessionStoreScope: scope,
		QuickCreatePrompt: "create the issue",
	}
	source, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    workspaceID,
		WorkspaceSlug:  "Quick Create GC",
		TaskID:         sourceID,
		AgentName:      "Codex",
		Provider:       "codex",
		Task:           sourceTask,
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare source environment: %v", err)
	}
	t.Cleanup(func() { _ = source.Cleanup(true) })

	storeDir := execenv.CodexSessionStorePath("", sourceTask)
	if storeDir == "" {
		t.Fatal("expected a scoped Codex session store")
	}
	rolloutRel := filepath.Join("2026", "09", "02", "rollout-2026-09-02T00-00-00-"+sessionID+".jsonl")
	rollout := filepath.Join(source.CodexHome, "sessions", rolloutRel)
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatalf("create rollout directory: %v", err)
	}
	if err := os.WriteFile(rollout, []byte("source rollout"), 0o644); err != nil {
		t.Fatalf("write source rollout: %v", err)
	}
	if !execenv.CodexStoreRolloutPresent(storeDir, sessionID) {
		t.Fatal("source rollout was not written through to the shared store")
	}
	if err := execenv.WriteGCMeta(source.RootDir, execenv.GCMeta{
		Kind:        execenv.GCKindQuickCreate,
		TaskID:      sourceID,
		WorkspaceID: workspaceID,
	}, slog.Default()); err != nil {
		t.Fatalf("write source GC metadata: %v", err)
	}

	// Release the Prepare lock so the real GC path can reserve and remove the
	// completed quick-create root.
	source.ReleaseLock()
	d.runGC(context.Background())
	if _, err := os.Stat(source.RootDir); !os.IsNotExist(err) {
		t.Fatalf("GC should remove completed source root, stat error = %v", err)
	}
	if !execenv.CodexStoreRolloutPresent(storeDir, sessionID) {
		t.Fatal("GC should preserve the shared rollout store")
	}

	freshTask := execenv.TaskContextForEnv{
		AgentID:           agentID,
		IssueID:           issueID,
		SessionStoreScope: scope,
	}
	fresh, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot:  workspacesRoot,
		WorkspaceID:     workspaceID,
		WorkspaceSlug:   "Quick Create GC",
		TaskID:          freshID,
		IssueIdentifier: "MUL-5764",
		AgentName:       "Codex",
		Provider:        "codex",
		Task:            freshTask,
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare fresh issue environment: %v", err)
	}
	t.Cleanup(func() { _ = fresh.Cleanup(true) })

	task := Task{
		AgentID:           agentID,
		IssueID:           issueID,
		PriorSessionID:    sessionID,
		PriorWorkDir:      source.WorkDir,
		SessionStoreScope: scope,
	}
	taskCtx := freshTask
	taskCtx.PriorSessionResumed = true
	if !gateResumeToReachableSession(&task, &taskCtx, "codex", fresh.WorkDir, "", false, slog.Default()) {
		t.Fatal("first resume gate should accept the preserved quick-create rollout")
	}
	gateCodexResumeToRolloutPresence(&task, &taskCtx, "codex", fresh.CodexHome, slog.Default())
	if task.PriorSessionID != sessionID || !taskCtx.PriorSessionResumed {
		t.Fatalf("both resume gates should preserve session %q, task=%q resumed=%t", sessionID, task.PriorSessionID, taskCtx.PriorSessionResumed)
	}
}

func TestGateResumeToReachableSession_QuickCreateBypass(t *testing.T) {
	// Fresh Issue env: prior workdir != env workdir, but qc store has rollout
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	t.Setenv("CODEX_HOME", sharedHome)
	const (
		agentID = "agent-qc-gate"
		scope   = "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874"
		session = "019f59d9-a6aa-7a53-b173-1eccc4b4c875"
	)
	// Seed store directly via exported path helper
	storeDir := execenv.CodexSessionStorePath("", execenv.TaskContextForEnv{AgentID: agentID, SessionStoreScope: scope})
	// CodexSessionStorePath resolves via CODEX_HOME, but we have set CODEX_HOME to sharedHome
	// For determinism, also ensure directory exists via same logic as codexSessionStoreDir
	if storeDir == "" {
		// fallback manual for default profile
		storeDir = filepath.Join(sharedHome, "multica-sessions", "default", agentID, scope)
	}
	if err := os.MkdirAll(filepath.Join(storeDir, "2026", "08", "05"), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(storeDir, "2026", "08", "05", "rollout-2026-08-05T00-00-00-"+session+".jsonl")
	if err := os.WriteFile(seed, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure it's regular file
	if fi, err := os.Lstat(seed); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("seed not regular: %v %v", fi, err)
	}
	task := Task{
		AgentID:           agentID,
		PriorSessionID:    session,
		PriorWorkDir:      "/tmp/prior-not-exist", // different from env
		SessionStoreScope: scope,
	}
	taskCtx := execenv.TaskContextForEnv{PriorSessionResumed: true, AgentID: agentID, SessionStoreScope: scope}
	envDir := filepath.Join(root, "fresh-env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// sessionHomeReachable false, workdir mismatch -> normally would drop, but qc store has rollout so should keep
	reachable := gateResumeToReachableSession(&task, &taskCtx, "codex", envDir, "", false, slog.Default())
	if !reachable {
		t.Fatal("qc store rollout should make session reachable across fresh workdir")
	}
	if task.PriorSessionID != session {
		t.Fatalf("qc gate should not drop PriorSessionID, got %q", task.PriorSessionID)
	}
	// Negative: without rollout, should drop
	task2 := Task{
		AgentID:           agentID,
		PriorSessionID:    "missing-session",
		PriorWorkDir:      "/tmp/prior-not-exist",
		SessionStoreScope: scope,
	}
	taskCtx2 := execenv.TaskContextForEnv{PriorSessionResumed: true, AgentID: agentID, SessionStoreScope: scope}
	reachable2 := gateResumeToReachableSession(&task2, &taskCtx2, "codex", envDir, "", false, slog.Default())
	if reachable2 {
		t.Fatal("missing rollout should not be reachable")
	}
	if task2.PriorSessionID != "" {
		t.Fatalf("should drop missing session, got %q", task2.PriorSessionID)
	}
	// Negative: malformed scope should not bypass (falls back to workdir check)
	task3 := Task{
		AgentID:           agentID,
		PriorSessionID:    session,
		PriorWorkDir:      "/tmp/prior-not-exist",
		SessionStoreScope: "qc_bad/scope",
	}
	taskCtx3 := execenv.TaskContextForEnv{PriorSessionResumed: true, AgentID: agentID, SessionStoreScope: "qc_bad/scope"}
	reachable3 := gateResumeToReachableSession(&task3, &taskCtx3, "codex", envDir, "", false, slog.Default())
	if reachable3 {
		t.Fatal("malformed qc scope should not bypass workdir check")
	}
	// Negative: different agent should not see
	task4 := Task{
		AgentID:           "other-agent",
		PriorSessionID:    session,
		PriorWorkDir:      "/tmp/prior-not-exist",
		SessionStoreScope: scope,
	}
	taskCtx4 := execenv.TaskContextForEnv{PriorSessionResumed: true, AgentID: "other-agent", SessionStoreScope: scope}
	reachable4 := gateResumeToReachableSession(&task4, &taskCtx4, "codex", envDir, "", false, slog.Default())
	if reachable4 {
		t.Fatal("other agent should not see qc store")
	}
	// Negative: profile isolation
	task5 := Task{
		AgentID:           agentID,
		PriorSessionID:    session,
		PriorWorkDir:      "/tmp/prior-not-exist",
		SessionStoreScope: scope,
	}
	taskCtx5 := execenv.TaskContextForEnv{PriorSessionResumed: true, AgentID: agentID, SessionStoreScope: scope}
	reachable5 := gateResumeToReachableSession(&task5, &taskCtx5, "codex", envDir, "other-profile", false, slog.Default())
	if reachable5 {
		t.Fatal("other profile should not see store")
	}
}
