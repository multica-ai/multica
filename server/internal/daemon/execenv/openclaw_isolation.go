package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Isolated-local is opt-in: only config/env provider credentials are inherited.
// Never copy a shared agent database (credentials and conversation state coexist
// there on newer OpenClaw versions), or bypass OpenClaw's ownership lock.
func prepareOpenclawIsolation(cfg map[string]any, envRoot string, rows []any, source openclawAgentsSource, skills []string) error {
	if source != openclawAgentsSourceEntries {
		return fmt.Errorf("isolated-local requires OpenClaw agents.entries config; registry-only and legacy storage are unsupported")
	}
	state := filepath.Join(envRoot, "openclaw-state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(state)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("openclaw isolated state must be a private directory")
	}
	if err := os.Chmod(state, 0o700); err != nil {
		return err
	}
	agents, ok := cfg["agents"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing agents config")
	}
	entries, ok := agents["entries"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing agents.entries config")
	}
	for _, row := range rows {
		original, ok := row.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid agent entry")
		}
		id, ok := original[openclawEntriesKeyField].(string)
		if !ok || id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
			return fmt.Errorf("invalid isolated agent identifier")
		}
		entry, ok := entries[id].(map[string]any)
		if !ok {
			return fmt.Errorf("missing isolated agent entry")
		}
		entry["agentDir"] = filepath.Join(state, "agents", id, "agent")
		// Assigned task skills must survive a host agent's explicit allowlist.
		// An absent allowlist stays absent (all discovered skills remain available).
		if allowed, ok := original["skills"].([]any); ok && len(skills) > 0 {
			merged := append([]any{}, allowed...)
			seen := map[string]bool{}
			for _, value := range allowed {
				if name, ok := value.(string); ok {
					seen[name] = true
				}
			}
			for _, name := range skills {
				if !seen[name] {
					merged = append(merged, name)
					seen[name] = true
				}
			}
			entry["skills"] = merged
		}
	}
	return nil
}
