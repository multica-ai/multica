package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Codex app-server keeps the developer instructions a thread started with:
// thread/resume rebuilds the model context from the rollout, so a different
// developerInstructions value on resume never reaches the model
// (openai/codex#19045; reproduced against codex 0.169.0 in MUL-9445). The
// daemon therefore records, per thread, a digest of the instructions the
// thread actually received, and only resumes a thread whose recorded digest
// matches the brief it would send now.
//
// The record lives at the root of the per-(agent, issue) session store, next
// to the YYYY/MM/DD rollout tree Codex owns. A dot-prefixed file is neither a
// date directory nor a rollout-*.jsonl, so Codex's session listing and state
// backfill ignore it, and it shares the store's lifetime (GC removes it with
// the store).
const codexDeveloperInstructionsPrefix = ".multica-developer-instructions-"

func codexDeveloperInstructionsRecordPath(store, sessionID string) string {
	id := sanitizePathSegment(sessionID)
	if store == "" || id == "" {
		return ""
	}
	return filepath.Join(store, codexDeveloperInstructionsPrefix+id+".sha256")
}

func codexDeveloperInstructionsDigest(instructions string) string {
	sum := sha256.Sum256([]byte(instructions))
	return hex.EncodeToString(sum[:])
}

// RecordCodexDeveloperInstructions stores the digest of the developer
// instructions sessionID was started with, so a later run can tell whether
// resuming it would leave the model on an outdated brief.
func RecordCodexDeveloperInstructions(store, sessionID, instructions string) error {
	path := codexDeveloperInstructionsRecordPath(store, sessionID)
	if path == "" {
		return errors.New("execenv: no Codex session store to record developer instructions in")
	}
	if err := os.MkdirAll(store, 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(codexDeveloperInstructionsDigest(instructions)+"\n"), 0o644)
}

// CodexDeveloperInstructionsMatch reports whether sessionID is recorded as
// having started with exactly these developer instructions. A missing or
// unreadable record — including every thread started before this channel
// existed, whose brief came from AGENTS.md — is a mismatch: resuming it would
// silently keep whatever instructions it had.
func CodexDeveloperInstructionsMatch(store, sessionID, instructions string) bool {
	path := codexDeveloperInstructionsRecordPath(store, sessionID)
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == codexDeveloperInstructionsDigest(instructions)
}
