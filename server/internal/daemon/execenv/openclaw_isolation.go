package execenv

import (
	"fmt"
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
	if err := ensureOpenclawPrivateDirectory(envRoot, "openclaw-state"); err != nil {
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
		if err := ensureOpenclawPrivateDirectory(envRoot, filepath.Join("openclaw-state", "agents", id, "agent")); err != nil {
			return err
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
