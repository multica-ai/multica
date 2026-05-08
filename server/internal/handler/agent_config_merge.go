package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
)

// AgentConfigLayer represents a single layer of agent configuration extracted
// from workspace system defaults, personal (member) defaults, or the agent's
// own settings. All fields are optional — zero values are skipped during merge.
type AgentConfigLayer struct {
	Instructions string            `json:"instructions,omitempty"`
	CustomEnv    map[string]string `json:"custom_env,omitempty"`
	CustomArgs   []string          `json:"custom_args,omitempty"`
	Skills       []string          `json:"skills,omitempty"`
}

// MergedAgentConfig is the result of merging multiple AgentConfigLayer values.
type MergedAgentConfig struct {
	Instructions string
	CustomEnv    map[string]string
	CustomArgs   []string
	SkillIDs     []string // deduplicated union of skill IDs
}

// MergeAgentConfigs merges configuration layers in order (first = lowest
// priority, last = highest priority). Merge rules:
//
//   - instructions: concatenated with "\n", empty strings skipped
//   - custom_env:   map merge; later layers override earlier keys
//   - custom_args:  array concatenation in order
//   - skills:       union of IDs, deduplicated (insertion order preserved)
func MergeAgentConfigs(layers ...AgentConfigLayer) MergedAgentConfig {
	var instrParts []string
	merged := MergedAgentConfig{
		CustomEnv: make(map[string]string),
	}

	seen := make(map[string]struct{})

	for _, l := range layers {
		// instructions: append non-empty
		if s := strings.TrimSpace(l.Instructions); s != "" {
			instrParts = append(instrParts, s)
		}

		// custom_env: later overrides earlier
		for k, v := range l.CustomEnv {
			merged.CustomEnv[k] = v
		}

		// custom_args: concatenate
		merged.CustomArgs = append(merged.CustomArgs, l.CustomArgs...)

		// skills: union, deduplicate preserving order
		for _, id := range l.Skills {
			if _, dup := seen[id]; !dup {
				seen[id] = struct{}{}
				merged.SkillIDs = append(merged.SkillIDs, id)
			}
		}
	}

	merged.Instructions = strings.Join(instrParts, "\n")

	// Normalize nil slices to nil (not empty slices) for clean JSON.
	if len(merged.CustomEnv) == 0 {
		merged.CustomEnv = nil
	}
	return merged
}

// parseAgentConfigLayer extracts an AgentConfigLayer from raw JSON bytes.
// When key is non-empty, the function first extracts that nested key from the
// top-level JSON object (e.g. "agent_defaults" from workspace settings).
// Returns a zero-value layer on any parse error.
func parseAgentConfigLayer(data []byte, key string) AgentConfigLayer {
	if len(data) == 0 {
		return AgentConfigLayer{}
	}
	raw := data
	if key != "" {
		var outer map[string]json.RawMessage
		if err := json.Unmarshal(data, &outer); err != nil {
			return AgentConfigLayer{}
		}
		nested, ok := outer[key]
		if !ok || len(nested) == 0 {
			return AgentConfigLayer{}
		}
		raw = nested
	}
	var layer AgentConfigLayer
	if err := json.Unmarshal(raw, &layer); err != nil {
		return AgentConfigLayer{}
	}
	return layer
}

// loadExtraSkills loads skills whose IDs appear in extraIDs but are not
// already present in existing. Each skill is loaded with its files via
// individual GetSkill + ListSkillFiles queries.
func (h *Handler) loadExtraSkills(ctx context.Context, existing []service.AgentSkillData, extraIDs []string) []service.AgentSkillData {
	if len(extraIDs) == 0 {
		return nil
	}

	// Build set of already-loaded skill names for dedup.
	have := make(map[string]struct{}, len(existing))
	for _, sk := range existing {
		have[sk.Name] = struct{}{}
	}

	var result []service.AgentSkillData
	for _, idStr := range extraIDs {
		uid := parseUUID(idStr)
		if !uid.Valid {
			continue
		}
		sk, err := h.Queries.GetSkill(ctx, uid)
		if err != nil {
			slog.Debug("loadExtraSkills: skill not found", "skill_id", idStr, "error", err)
			continue
		}
		// Skip if a skill with the same name is already loaded (from agent_skill).
		if _, dup := have[sk.Name]; dup {
			continue
		}
		have[sk.Name] = struct{}{}

		data := service.AgentSkillData{Name: sk.Name, Content: sk.Content}
		files, _ := h.Queries.ListSkillFiles(ctx, sk.ID)
		for _, f := range files {
			data.Files = append(data.Files, service.AgentSkillFileData{Path: f.Path, Content: f.Content})
		}
		result = append(result, data)
	}
	return result
}

// loadExistingSkillIDs returns the set of skill IDs currently assigned to
// an agent via the agent_skill table. Used to exclude them from the extra
// skill IDs derived from defaults merge.
func (h *Handler) loadExistingSkillIDs(ctx context.Context, agentID pgtype.UUID) map[string]struct{} {
	skills, err := h.Queries.ListAgentSkills(ctx, agentID)
	if err != nil {
		return nil
	}
	ids := make(map[string]struct{}, len(skills))
	for _, sk := range skills {
		ids[uuidToString(sk.ID)] = struct{}{}
	}
	return ids
}
