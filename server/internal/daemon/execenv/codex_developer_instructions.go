package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
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

// ComposeCodexDeveloperInstructions returns the developer instructions a
// Codex thread should start with when the brief travels over the app-server
// channel. Codex resolves `params.developer_instructions.or(config)`
// (codex-rs/core/src/config/mod.rs, rust-v0.169.0): a non-empty parameter
// replaces the user's configured developer_instructions instead of extending
// it. The task home's config.toml is the copy Codex will load, so its value is
// kept first and the brief follows. On error the brief is returned alone.
func ComposeCodexDeveloperInstructions(codexHome, brief string) (string, error) {
	if codexHome == "" {
		return brief, nil
	}
	path := filepath.Join(codexHome, "config.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return brief, nil
	}
	if err != nil {
		return brief, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		DeveloperInstructions string `toml:"developer_instructions"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return brief, fmt.Errorf("parse %s: %w", path, err)
	}
	configured := strings.TrimSpace(cfg.DeveloperInstructions)
	if configured == "" {
		return brief, nil
	}
	return configured + "\n\n" + brief, nil
}
