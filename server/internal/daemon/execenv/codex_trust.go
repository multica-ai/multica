package execenv

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// TrustCodexWorkdir marks only the exact daemon-selected workdir trusted in the
// isolated per-task CODEX_HOME. It never touches the user's shared config.
func TrustCodexWorkdir(codexHome, workdir string) error {
	if codexHome == "" || workdir == "" {
		return fmt.Errorf("codex home and workdir are required")
	}
	cleanWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("resolve codex workdir: %w", err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read task codex config: %w", err)
	}
	config := make(map[string]any)
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &config); err != nil {
			return fmt.Errorf("parse task codex config: %w", err)
		}
	}
	// The task config is copied from the user's shared Codex home. Do not carry
	// any other trusted project into the isolated agent process: the daemon owns
	// this process's cwd and grants trust to that exact canonical path only.
	config["projects"] = map[string]any{
		filepath.Clean(cleanWorkdir): map[string]any{"trust_level": "trusted"},
	}
	updated, err := toml.Marshal(config)
	if err != nil {
		return fmt.Errorf("encode task codex config: %w", err)
	}
	if err := writeFileAtomic(configPath, updated, 0o600); err != nil {
		return fmt.Errorf("write task codex config: %w", err)
	}
	return nil
}
