package handler

// CEREBRO-PATCH(skill-autolearn-handler): TECH-3692 — toggle a skill's
// self-learning (auto_learn) switch on an existing skill. New cerebro feature;
// all logic lives in server/internal/cerebro/skill_metadata.go.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/cerebro"
)

// SetSkillAutoLearn handles POST /api/skills/{id}/auto-learn.
// Body: {"enabled": true|false}. Flips the skill's self-learning switch.
// Only the owner, an approver, or a workspace admin may toggle it.
func (h *Handler) SetSkillAutoLearn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "skill learning not available")
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	meta, err := cerebro.SetSkillAutoLearn(r.Context(), h.CerebroQueries, skill.ID, skill.Content, req.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update auto_learn")
		return
	}

	tags := meta.Tags
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"skill_id":   uuidToString(skill.ID),
		"skill_name": skill.Name,
		"auto_learn": meta.AutoLearn,
		"metadata": SkillMetadataView{
			Category:  meta.Category,
			Domain:    meta.Domain,
			Type:      meta.Type,
			Scope:     meta.Scope,
			Status:    meta.Status,
			Tags:      tags,
			AutoLearn: meta.AutoLearn,
		},
	})
}
