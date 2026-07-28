// Package opencodeharness installs the Firtal-owned OpenCode plugin that gates
// every tool call against the Multica tool policy.
//
// OpenCode exposes no provider-native hook file, so this plugin is its
// tool-policy adapter: OpenCode loads `.opencode/plugin/*.js` from the project
// directory and fires `tool.execute.before` ahead of every tool it runs.
// Throwing from that hook aborts the call.
package opencodeharness

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

//go:embed multica-tool-policy.js
var plugin []byte

// pluginName is the file OpenCode loads. The `multica-` prefix keeps it
// distinguishable from any plugin a user drops in the same directory.
const pluginName = "multica-tool-policy.js"

// pluginDir is fixed by OpenCode: it discovers plugins in <project>/.opencode/plugin.
var pluginDir = filepath.Join(".opencode", "plugin")

// Prepare writes the versioned plugin into the task worktree and returns its
// path. The file is rewritten on every run so a stale copy from an earlier
// harness revision can never survive in a reused workdir.
func Prepare(workdir string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		return "", errors.New("workdir is required")
	}
	dir := filepath.Join(workdir, pluginDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, pluginName)
	if err := os.WriteFile(path, plugin, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Installed reports whether the plugin this build ships is the one currently on
// disk. The daemon calls it after Prepare to refuse a spawn rather than start
// OpenCode with a plugin that failed to land — an unenforced OpenCode run is
// exactly what the gate exists to prevent.
func Installed(workdir string) bool {
	if strings.TrimSpace(workdir) == "" {
		return false
	}
	got, err := os.ReadFile(filepath.Join(workdir, pluginDir, pluginName))
	if err != nil {
		return false
	}
	return string(got) == string(plugin)
}
