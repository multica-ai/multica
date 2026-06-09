package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeManifest describes a user-installed agent runtime extension.
// It mirrors the AionUi extension manifest pattern: a JSON file that
// declares an ACP-compatible CLI and its capabilities, discovered by
// scanning ~/.multica/runtimes/*/runtime.json at daemon startup.
type RuntimeManifest struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description,omitempty"`
	Provider    string                 `json:"provider"`
	Transport   string                 `json:"transport"` // "acp-stdio" or "stream-json"
	Command     RuntimeManifestCommand `json:"command"`
	Env         map[string]string      `json:"env,omitempty"`
}

// RuntimeManifestCommand describes how to launch the ACP-compatible CLI.
type RuntimeManifestCommand struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
}

// ToAgentEntry converts a runtime manifest into a daemon AgentEntry for
// registration. The transport field determines how the daemon spawns the
// CLI at task time.
func (m RuntimeManifest) ToAgentEntry() AgentEntry {
	transport := m.Transport
	if transport == "" {
		transport = "acp-stdio"
	}
	return AgentEntry{
		Path:       m.Command.Executable,
		Transport:  transport,
		ACPArgs:    m.Command.Args,
		IsExternal: true,
	}
}

// LoadRuntimeManifests scans rootDir (typically ~/.multica/runtimes/)
// for subdirectories containing a runtime.json file and returns the
// parsed manifests. Invalid or unparseable files are skipped with a
// logged warning; the caller decides how to handle an empty result.
func LoadRuntimeManifests(rootDir string) ([]RuntimeManifest, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime dir %s: %w", rootDir, err)
	}

	var manifests []RuntimeManifest
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(rootDir, entry.Name(), "runtime.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			// Log and skip — don't block daemon startup for a single bad extension.
			fmt.Fprintf(os.Stderr, "warning: failed to read runtime manifest %s: %v\n", manifestPath, err)
			continue
		}

		var m RuntimeManifest
		if err := json.Unmarshal(data, &m); err != nil {
			fmt.Fprintf(os.Stderr, "warning: invalid runtime manifest %s: %v\n", manifestPath, err)
			continue
		}

		// Validate required fields.
		var missing []string
		if m.ID == "" {
			missing = append(missing, "id")
		}
		if m.Name == "" {
			missing = append(missing, "name")
		}
		if m.Provider == "" {
			missing = append(missing, "provider")
		}
		if m.Transport == "" {
			missing = append(missing, "transport")
		}
		if m.Command.Executable == "" {
			missing = append(missing, "command.executable")
		}
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "warning: runtime manifest %s missing required fields: %s\n", manifestPath, strings.Join(missing, ", "))
			continue
		}

		manifests = append(manifests, m)
	}

	return manifests, nil
}

// DefaultRuntimesDir returns the default path for user-installed runtime
// extensions (~/.multica/runtimes). The directory is NOT created here;
// callers should create it if needed before scanning.
func DefaultRuntimesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".multica", "runtimes")
	}
	return filepath.Join(home, ".multica", "runtimes")
}
