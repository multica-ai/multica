package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// OMP aliases --session to --resume. All session selection and persistence
// flags must remain daemon-owned, including on fresh runs using --session-dir.
var ompSessionArgs = map[string]blockedArgMode{
	"--session":     blockedOptionalValue,
	"--session-dir": blockedWithValue,
	"--no-session":  blockedStandalone,
	"--continue":    blockedStandalone,
	"-c":            blockedStandalone,
	"--resume":      blockedOptionalValue,
	"-r":            blockedOptionalValue,
	"--fork":        blockedWithValue,
	"--session-id":  blockedWithValue,
}

func buildOmpArgs(sessionPath, sessionDir string, opts ExecOptions, logger *slog.Logger) []string {
	opts.CustomArgs = filterCustomArgs(opts.CustomArgs, ompSessionArgs, logger)
	args := buildPiArgs(sessionPath, opts, logger)
	if sessionDir != "" {
		args = append(args, "--session-dir", sessionDir)
	}
	return args
}

// Each fresh execution owns a private directory. OMP chooses the filename;
// discover it after exit instead of depending on its timestamp/ID naming
// convention or moving a transcript away from its companion artifacts.
func findOmpSessionFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var path string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if info.Size() == 0 {
			continue
		}
		if path != "" {
			return "", fmt.Errorf("multiple transcripts in fresh session directory %q", dir)
		}
		path = filepath.Join(dir, entry.Name())
	}
	if path == "" {
		return "", fmt.Errorf("no persisted transcript in fresh session directory %q", dir)
	}
	return path, nil
}
