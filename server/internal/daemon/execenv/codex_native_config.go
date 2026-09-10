package execenv

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const codexNativeAgentsManifest = ".multica-inherited-agents.json"

// syncCodexNativeConfig refreshes host-authoritative instructions and native
// roles on both prepare and reuse. Only roles recorded in the manifest may be
// removed when the host stops providing them; unrelated task roles survive.
func syncCodexNativeConfig(codexHome, sharedHome string) error {
	root, err := openVerifiedCodexHomeRoot(codexHome, "native configuration")
	if err != nil {
		return err
	}
	defer root.Close()
	sharedInfo, err := os.Stat(sharedHome)
	if err != nil {
		if _, statErr := os.Lstat(sharedHome); !os.IsNotExist(err) || !os.IsNotExist(statErr) {
			return fmt.Errorf("stat shared codex home: %w", err)
		}
	}
	if err == nil {
		taskInfo, err := root.Stat(".")
		if err != nil {
			return fmt.Errorf("stat task codex home: %w", err)
		}
		if os.SameFile(sharedInfo, taskInfo) {
			return fmt.Errorf("shared and task codex homes must be separate directories")
		}
	}

	// Read every source before changing any destination. A read failure must not
	// look like removal of a host file and silently erase the last good copy.
	sources := make(map[string][]byte)
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md"} {
		src := filepath.Join(sharedHome, name)
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("stat global instructions %s: %w", name, err)
		}
		data, err := readCodexNativeSource(src)
		if err != nil {
			return err
		}
		sources[name] = data
	}
	entries, err := os.ReadDir(filepath.Join(sharedHome, "agents"))
	if err != nil {
		if _, statErr := os.Lstat(filepath.Join(sharedHome, "agents")); !os.IsNotExist(err) || !os.IsNotExist(statErr) {
			return fmt.Errorf("read shared native agents: %w", err)
		}
	}
	var current []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		name := entry.Name()
		if !validCodexNativeAgentName(name) {
			return fmt.Errorf("invalid shared native agent filename %q", name)
		}
		data, err := readCodexNativeSource(filepath.Join(sharedHome, "agents", name))
		if err != nil {
			return err
		}
		sources[filepath.Join("agents", name)] = data
		current = append(current, name)
	}

	var previous []string
	data, err := root.ReadFile(codexNativeAgentsManifest)
	if err == nil {
		if err := json.Unmarshal(data, &previous); err != nil {
			return fmt.Errorf("read inherited native agents manifest: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read inherited native agents manifest: %w", err)
	}
	for _, name := range previous {
		if !validCodexNativeAgentName(name) {
			return fmt.Errorf("invalid inherited native agent filename %q", name)
		}
	}

	// Record the union before modifying role files so a failed refresh remains
	// tracked and can be retried, including roles copied for the first time.
	tracked := append(slices.Clone(previous), current...)
	slices.Sort(tracked)
	tracked = slices.Compact(tracked)
	if len(tracked) > 0 {
		if err := writeCodexNativeManifest(root, tracked); err != nil {
			return err
		}
	}
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md"} {
		if data, ok := sources[name]; ok {
			if err := replaceCodexNativeFile(root, name, data); err != nil {
				return err
			}
		} else if err := root.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale global instructions %s: %w", name, err)
		}
	}
	if len(current) > 0 {
		if err := root.MkdirAll("agents", 0o755); err != nil {
			return fmt.Errorf("create native agents directory: %w", err)
		}
	}
	for _, name := range current {
		path := filepath.Join("agents", name)
		if err := replaceCodexNativeFile(root, path, sources[path]); err != nil {
			return err
		}
	}
	for _, name := range previous {
		if !slices.Contains(current, name) {
			if err := root.Remove(filepath.Join("agents", name)); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove stale native agent %s: %w", name, err)
			}
		}
	}
	if len(tracked) > 0 {
		return writeCodexNativeManifest(root, current)
	}
	return nil
}

func validCodexNativeAgentName(name string) bool {
	return filepath.IsLocal(name) && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\") && strings.HasSuffix(name, ".toml")
}

func readCodexNativeSource(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat native configuration %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("native configuration %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read native configuration %s: %w", path, err)
	}
	return data, nil
}

func writeCodexNativeManifest(root *os.Root, names []string) error {
	data, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encode inherited native agents manifest: %w", err)
	}
	return replaceCodexNativeFile(root, codexNativeAgentsManifest, data)
}

// Publish complete files by rename, replacing links themselves. All operations
// use the verified root so a reused task cannot redirect writes outside it.
// Only the daemon user needs access; native configuration can contain secrets.
func replaceCodexNativeFile(root *os.Root, name string, data []byte) error {
	temp := filepath.Join(filepath.Dir(name), ".multica-native-"+rand.Text())
	out, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create native configuration %s: %w", name, err)
	}
	defer out.Close()
	defer root.Remove(temp)
	if _, err := out.Write(data); err != nil {
		return fmt.Errorf("write native configuration %s: %w", name, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close native configuration %s: %w", name, err)
	}
	if err := root.Rename(temp, name); err != nil {
		return fmt.Errorf("replace native configuration %s: %w", name, err)
	}
	return nil
}
