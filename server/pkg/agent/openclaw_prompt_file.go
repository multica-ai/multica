package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenClaw added agent --message-file in 2026.6.11. Older supported versions
// retain their existing transport; Windows command lines cannot carry full tasks.
func prepareOpenclawPromptFile(args []string, platform, version string) ([]string, func(), error) {
	noop := func() {}
	if platform != "windows" || compareOpenclawVersion(version, "2026.6.11") < 0 {
		return args, noop, nil
	}
	if len(args) < 2 || args[len(args)-2] != "--message" {
		return nil, noop, fmt.Errorf("openclaw prompt transport: missing daemon prompt")
	}
	dir, err := os.MkdirTemp("", "multica-openclaw-prompt-")
	if err != nil {
		return nil, noop, fmt.Errorf("openclaw prompt file: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte(args[len(args)-1]), 0o600); err != nil {
		cleanup()
		return nil, noop, fmt.Errorf("openclaw prompt file: %w", err)
	}
	result := append([]string(nil), args...)
	result[len(result)-2] = "--message-file"
	result[len(result)-1] = path
	return result, cleanup, nil
}
