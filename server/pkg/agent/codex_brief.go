package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// The daemon hands Codex the Multica runtime brief as the app-server's
// developerInstructions (GitHub #11480). Codex records it in the thread only on
// thread/start: thread/resume ignores the field, so a brief that changed since
// the thread began (edited agent instructions, a new skill, a squad roster
// change) would never reach a resumed thread.
//
// To keep resumed threads current, the backend remembers which brief each
// thread last received and, when a resume finds a different one, prepends the
// new brief to the turn once. The record sits in the sessions store next to the
// thread's rollout, so it travels with whatever makes the thread resumable.
// Codex only treats `rollout-*` files there as sessions, so it ignores it.

// codexBriefRecordDir is the directory under the Codex sessions root that
// holds one file per thread, named after the thread ID, containing the SHA-256
// of the brief that thread last received.
const codexBriefRecordDir = "multica-brief"

// codexBriefRecordPath returns where the brief record for threadID lives, or
// "" when no sessions root can be resolved or the ID is not a plain name.
func codexBriefRecordPath(codexHome, threadID string) string {
	if threadID == "" || threadID != filepath.Base(threadID) || strings.HasPrefix(threadID, ".") {
		return ""
	}
	// The daemon always pins CODEX_HOME; use its sessions dir even before
	// Codex has created it, rather than falling back to the ambient home.
	root := ""
	if codexHome = strings.TrimSpace(codexHome); codexHome != "" {
		root = filepath.Join(codexHome, "sessions")
	} else {
		root = codexSessionRoot("")
	}
	if root == "" {
		return ""
	}
	return filepath.Join(root, codexBriefRecordDir, threadID)
}

// codexThreadHasBrief reports whether threadID is recorded as already holding
// the brief with the given hash. A missing or unreadable record means "not
// known", which makes the caller resend the brief: a duplicate copy costs
// tokens, a missing one costs the workflow rules.
func codexThreadHasBrief(codexHome, threadID, briefHash string) bool {
	path := codexBriefRecordPath(codexHome, threadID)
	if path == "" || briefHash == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == briefHash
}

// recordCodexThreadBrief stores briefHash as the brief threadID now holds.
func recordCodexThreadBrief(codexHome, threadID, briefHash string) error {
	path := codexBriefRecordPath(codexHome, threadID)
	if path == "" || briefHash == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(briefHash+"\n"), 0o644)
}

// codexBriefRefresh wraps a brief that replaces the one a resumed thread was
// started with, so the model knows which copy is current.
func codexBriefRefresh(brief string) string {
	return "<multica-runtime-brief-update>\n" +
		"Your Multica runtime instructions changed since this conversation started. " +
		"The version below replaces every earlier copy; follow it from now on.\n\n" +
		strings.TrimRight(brief, "\n") +
		"\n</multica-runtime-brief-update>\n\n"
}
