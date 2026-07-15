// Package piharness installs the Firtal-owned Pi extension into an isolated
// Multica task worktree and locks Pi to that extension for the duration of the run.
package piharness

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

//go:embed multica-harness.ts
var extension []byte

const extensionName = "multica-harness.ts"

// Prepare writes the versioned extension into task-owned state. The file is
// private because future harness revisions may contain workspace-specific data.
func Prepare(workdir string) (string, error) {
	if strings.TrimSpace(workdir) == "" {
		return "", errors.New("workdir is required")
	}
	dir := filepath.Join(workdir, ".multica", "pi-harness")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, extensionName)
	if err := os.WriteFile(path, extension, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// PrepareConnections validates the provider-neutral mcpServers document and
// writes it beside the extension with credential-safe permissions.
func PrepareConnections(workdir string, raw []byte) (string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = []byte(`{"mcpServers":{}}`)
	}
	var document struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", err
	}
	for name, server := range document.MCPServers {
		var object map[string]any
		if name == "" || json.Unmarshal(server, &object) != nil || object == nil {
			return "", errors.New("mcpServers entries must be named objects")
		}
	}
	dir := filepath.Join(workdir, ".multica", "pi-harness")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ManagedArgs removes user arguments that could replace, supplement, or hide
// the managed tool registry, then installs exactly the Firtal extension.
func ManagedArgs(args []string, extensionPath string) ([]string, error) {
	if strings.TrimSpace(extensionPath) == "" {
		return nil, errors.New("managed extension path is required")
	}
	filtered := make([]string, 0, len(args)+3)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		switch name {
		case "-e", "--extension", "--tools":
			if arg == name && i+1 < len(args) {
				i++
			}
		case "--no-extensions", "--no-tools":
		default:
			filtered = append(filtered, arg)
		}
	}
	return append(filtered, "--no-extensions", "-e", extensionPath), nil
}
