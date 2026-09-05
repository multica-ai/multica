package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidCodexSessionStoreScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		scope string
		valid bool
	}{
		{"valid qc", "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874", true},
		{"qc prefix only", "qc_", false},
		{"qc non-uuid", "qc_not-a-uuid", false},
		{"qc uppercase uuid", "qc_019F59D9-A6AA-7A53-B173-1ECCC4B4C874", false},
		{"qc compact uuid", "qc_019f59d9a6aa7a53b1731eccc4b4c874", false},
		{"empty", "", false},
		{"path separator", "qc_bad/scope", false},
		{"punctuation", "qc_bad:scope", false},
		{"overlong", strings.Repeat("q", 256), false},
		{"normal issue", "019f59d9-a6aa-7a53-b173-1eccc4b4c873", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidCodexSessionStoreScope(tc.scope); got != tc.valid {
				t.Fatalf("ValidCodexSessionStoreScope(%q)=%v want %v", tc.scope, got, tc.valid)
			}
		})
	}
}

func TestCodexSessionStoreKey_QuickCreateScope(t *testing.T) {
	t.Parallel()
	const (
		agentID = "agent-qc"
		qcScope = "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874"
		issueID = "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	)
	keyQC := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: qcScope, IssueID: issueID})
	keyIssue := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, IssueID: issueID})
	if keyQC == keyIssue {
		t.Fatalf("qc scope should override issue, got same key %q", keyQC)
	}
	if filepath.Base(keyQC) != qcScope {
		t.Fatalf("qc key base should be qc scope, got %q", keyQC)
	}
	// chat fallback
	chatID := "019f59d9-a6aa-7a53-b173-1eccc4b4c875"
	keyChat := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, ChatSessionID: chatID})
	if !strings.HasPrefix(filepath.Base(keyChat), "chat_") {
		t.Fatalf("chat key should have chat_ prefix, got %q", keyChat)
	}
	// Every malformed qc scope falls back to the stable issue/chat key rather
	// than overriding it with a potentially colliding directory name.
	for _, malformed := range []string{
		"qc_",
		"qc_not-a-uuid",
		"qc_bad/scope",
		"qc_019F59D9-A6AA-7A53-B173-1ECCC4B4C874",
		"qc_019f59d9a6aa7a53b1731eccc4b4c874",
	} {
		keyFallback := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: malformed, IssueID: issueID})
		if keyFallback != keyIssue {
			t.Fatalf("malformed qc scope %q should fall back to issue, got %q vs %q", malformed, keyFallback, keyIssue)
		}
		chatFallback := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: malformed, ChatSessionID: chatID})
		if chatFallback != keyChat {
			t.Fatalf("malformed qc scope %q should fall back to chat, got %q vs %q", malformed, chatFallback, keyChat)
		}
	}
	// qc should override even chat when valid
	keyQCChat := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: qcScope, ChatSessionID: chatID})
	if filepath.Base(keyQCChat) != qcScope {
		t.Fatalf("qc should override chat, got %q", keyQCChat)
	}
}

func TestPrepareCodexSessionsDir_QuickCreateHandoffAcrossIssueBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	const (
		agentID = "agent-quick-create"
		issueID = "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
		scope   = "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874"
		session = "019f59d9-a6aa-7a53-b173-1eccc4b4c875"
	)
	sourceKey := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: scope})
	issueKey := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: scope, IssueID: issueID})
	if sourceKey != issueKey {
		t.Fatalf("quick-create handoff keys differ: source=%q issue=%q", sourceKey, issueKey)
	}
	sourceHome := filepath.Join(root, "source-task", "codex-home")
	if err := os.MkdirAll(sourceHome, 0o755); err != nil {
		t.Fatalf("mkdir source home: %v", err)
	}
	if err := prepareCodexSessionsDir(sourceHome, sharedHome, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: sourceKey, SessionStoreScope: scope}, testLogger()); err != nil {
		t.Fatalf("prepare source home: %v", err)
	}
	seedRolloutAt(t, filepath.Join(sourceHome, "sessions", "2026", "08", "05", "rollout-2026-08-05T00-00-00-"+session+".jsonl"), 32)

	issueHome := filepath.Join(root, "issue-task", "codex-home")
	if err := os.MkdirAll(issueHome, 0o755); err != nil {
		t.Fatalf("mkdir issue home: %v", err)
	}
	if err := prepareCodexSessionsDir(issueHome, sharedHome, CodexHomeOptions{IsLocalDirectory: true, SessionStoreKey: issueKey, SessionStoreScope: scope, ResumeSessionID: session}, testLogger()); err != nil {
		t.Fatalf("prepare issue home: %v", err)
	}
	if !CodexResumeRolloutPresent(issueHome, session) {
		t.Fatal("first issue task cannot see quick-create source rollout")
	}
}

func TestPrepareCodexSessionsDir_ManagedQuickCreateFreshWriteThrough(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	const (
		agentID = "agent-managed-qc"
		scope   = "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874"
		session = "019f59d9-a6aa-7a53-b173-1eccc4b4c875"
	)
	key := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: scope})
	// Fresh managed: sessions does not exist yet, should link to durable store
	codexHome := filepath.Join(root, "fresh-managed", "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex home: %v", err)
	}
	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{SessionStoreKey: key, SessionStoreScope: scope}, testLogger()); err != nil {
		t.Fatalf("prepare fresh managed: %v", err)
	}
	// Should be linked, not local dir
	sessions := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("sessions missing: %v", err)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Fatalf("fresh managed qc should link to store, got mode %v", fi.Mode())
	}
	// Seed rollout through link and verify store has it as regular file
	seedRolloutAt(t, filepath.Join(sessions, "2026", "08", "05", "rollout-2026-08-05T00-00-00-"+session+".jsonl"), 32)
	storeDir := codexSessionStoreDir(sharedHome, key)
	if !CodexStoreRolloutPresent(storeDir, session) {
		t.Fatal("store should have rollout as regular file")
	}
	// Fresh managed without qc should be local dir
	codexHome2 := filepath.Join(root, "fresh-managed2", "codex-home")
	if err := os.MkdirAll(codexHome2, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	key2 := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, IssueID: "019f59d9-a6aa-7a53-b173-1eccc4b4c873"})
	if err := prepareCodexSessionsDir(codexHome2, sharedHome, CodexHomeOptions{SessionStoreKey: key2, SessionStoreScope: ""}, testLogger()); err != nil {
		t.Fatalf("prepare second: %v", err)
	}
	fi2, _ := os.Lstat(filepath.Join(codexHome2, "sessions"))
	if fi2.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		t.Fatal("fresh managed without qc should be local dir, not link")
	}
}

func TestCodexStoreHasRollout(t *testing.T) {
	root := t.TempDir()
	sharedHome := filepath.Join(root, "shared")
	t.Setenv("CODEX_HOME", sharedHome)
	const (
		agentID = "agent-store-check"
		scope   = "qc_019f59d9-a6aa-7a53-b173-1eccc4b4c874"
		session = "019f59d9-a6aa-7a53-b173-1eccc4b4c875"
	)
	key := codexSessionStoreKey("", TaskContextForEnv{AgentID: agentID, SessionStoreScope: scope})
	storeDir := codexSessionStoreDir(sharedHome, key)
	seedRolloutAt(t, filepath.Join(storeDir, "2026", "08", "05", "rollout-2026-08-05T00-00-00-"+session+".jsonl"), 16)
	if !CodexStoreHasRollout("", agentID, scope, session) {
		t.Fatal("store should have rollout")
	}
	if CodexStoreHasRollout("", agentID, scope, "missing") {
		t.Fatal("missing session should not be reported present")
	}
	if CodexStoreHasRollout("", agentID, "malformed/scope", session) {
		t.Fatal("malformed scope should not be considered")
	}
}
