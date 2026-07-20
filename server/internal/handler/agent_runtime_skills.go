package handler

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const maxRuntimeSkillKeyLength = 512

// DisabledRuntimeSkill identifies one runtime-local skill that an agent must
// not inherit. RuntimeID scopes the choice to a single machine/runtime so a
// same-named skill on another runtime is unaffected.
type DisabledRuntimeSkill struct {
	RuntimeID string `json:"runtime_id"`
	Provider  string `json:"provider"`
	Root      string `json:"root"`
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	Plugin    string `json:"plugin,omitempty"`
}

func decodeDisabledRuntimeSkills(raw []byte) []DisabledRuntimeSkill {
	if len(raw) == 0 {
		return []DisabledRuntimeSkill{}
	}
	var skills []DisabledRuntimeSkill
	if err := json.Unmarshal(raw, &skills); err != nil || skills == nil {
		return []DisabledRuntimeSkill{}
	}
	return skills
}

func disabledRuntimeSkillsFor(raw []byte, runtimeID, provider string) []DisabledRuntimeSkill {
	all := decodeDisabledRuntimeSkills(raw)
	result := make([]DisabledRuntimeSkill, 0, len(all))
	for _, skill := range all {
		if skill.RuntimeID == runtimeID && skill.Provider == provider {
			result = append(result, skill)
		}
	}
	return result
}

func normalizeRuntimeSkillIdentity(root, key, plugin string) (string, string, string, bool) {
	root = strings.TrimSpace(root)
	key = strings.TrimSpace(key)
	plugin = strings.TrimSpace(plugin)
	if len(key) == 0 || len(key) > maxRuntimeSkillKeyLength {
		return "", "", "", false
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(key)))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", "", "", false
	}
	switch root {
	case "provider", "universal":
		plugin = ""
	case "plugin":
		if plugin == "" {
			return "", "", "", false
		}
	default:
		return "", "", "", false
	}
	return root, cleaned, plugin, true
}

func sameDisabledRuntimeSkill(a, b DisabledRuntimeSkill) bool {
	return a.RuntimeID == b.RuntimeID && a.Provider == b.Provider &&
		a.Root == b.Root && a.Key == b.Key && a.Plugin == b.Plugin
}

// SetAgentRuntimeSkillEnabled persists a per-agent override for a skill that
// is discovered from the agent's currently assigned local runtime.
func (h *Handler) SetAgentRuntimeSkillEnabled(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	var req struct {
		RuntimeID string `json:"runtime_id"`
		Root      string `json:"root"`
		Key       string `json:"key"`
		Name      string `json:"name"`
		Plugin    string `json:"plugin"`
		Enabled   *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "runtime_id, root, key, and enabled are required")
		return
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	if !agent.RuntimeID.Valid || agent.RuntimeID != runtimeID {
		writeError(w, http.StatusConflict, "agent is no longer assigned to this runtime")
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeID)
	if err != nil || rt.WorkspaceID != agent.WorkspaceID {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if rt.RuntimeMode != "local" || (rt.Provider != "codex" && rt.Provider != "claude") {
		writeError(w, http.StatusBadRequest, "runtime skill controls are only supported for codex and claude")
		return
	}
	root, key, plugin, valid := normalizeRuntimeSkillIdentity(req.Root, req.Key, req.Plugin)
	if !valid || (root == "plugin" && rt.Provider != "claude") {
		writeError(w, http.StatusBadRequest, "invalid runtime skill identity")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > maxRuntimeSkillKeyLength {
		writeError(w, http.StatusBadRequest, "invalid runtime skill name")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	locked, err := qtx.GetAgentForUpdate(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	if !locked.RuntimeID.Valid || locked.RuntimeID != runtimeID {
		writeError(w, http.StatusConflict, "agent is no longer assigned to this runtime")
		return
	}

	target := DisabledRuntimeSkill{
		RuntimeID: req.RuntimeID,
		Provider:  rt.Provider,
		Root:      root,
		Key:       key,
		Name:      name,
		Plugin:    plugin,
	}
	current := decodeDisabledRuntimeSkills(locked.DisabledRuntimeSkills)
	next := make([]DisabledRuntimeSkill, 0, len(current)+1)
	for _, skill := range current {
		if sameDisabledRuntimeSkill(skill, target) {
			continue
		}
		next = append(next, skill)
	}
	if !*req.Enabled {
		next = append(next, target)
	}
	payload, err := json.Marshal(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode runtime skill settings")
		return
	}
	if _, err := qtx.UpdateAgentDisabledRuntimeSkills(r.Context(), db.UpdateAgentDisabledRuntimeSkillsParams{
		ID:                    locked.ID,
		DisabledRuntimeSkills: payload,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update runtime skill")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
