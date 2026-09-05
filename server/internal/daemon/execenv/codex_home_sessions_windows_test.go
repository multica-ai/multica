//go:build windows

package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A real Windows junction is reported by Lstat as an irregular directory,
// not a symlink. This keeps the QC session-store path on the SameFile branch
// that protects junctions from being mistaken for task-owned directories.
func TestPrepareCodexSessionsDir_QuickCreateJunction(t *testing.T) {
	sharedHome := t.TempDir()
	codexHome := t.TempDir()
	scope := "qc_019f59d9-a6aa-7b8c-9d0e-1f2a3b4c5d6e"
	key := codexSessionStoreKey("", TaskContextForEnv{
		AgentID:           "agent-qc-junction",
		SessionStoreScope: scope,
	})
	storeDir := codexSessionStoreDir(sharedHome, key)
	sessionID := "019f59d9-a6aa-7b8c-9d0e-1f2a3b4c5d6e"
	seedRolloutAt(t, filepath.Join(storeDir, "2026", "09", "02", "rollout-2026-09-02T00-00-00-"+sessionID+".jsonl"), 8)

	sessions := filepath.Join(codexHome, "sessions")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", sessions, storeDir).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %s: %v", out, err)
	}
	fi, err := os.Lstat(sessions)
	if err != nil {
		t.Fatalf("lstat junction: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a junction, got a symlink (mode %v)", fi.Mode())
	}
	if fi.Mode()&os.ModeIrregular == 0 {
		t.Fatalf("expected an irregular junction entry, got mode %v", fi.Mode())
	}
	if !isSameCodexStoreLink(sessions, storeDir) {
		t.Fatal("junction was not recognized as the existing QC session store")
	}

	if err := prepareCodexSessionsDir(codexHome, sharedHome, CodexHomeOptions{
		ResumeSessionID:   sessionID,
		SessionStoreKey:   key,
		SessionStoreScope: scope,
	}, testLogger()); err != nil {
		t.Fatalf("prepareCodexSessionsDir: %v", err)
	}
	assertSessionsLinkedToStore(t, sessions, storeDir)
	if !CodexResumeRolloutPresent(codexHome, sessionID) {
		t.Fatalf("resume rollout %q not visible through the QC junction", sessionID)
	}
}
