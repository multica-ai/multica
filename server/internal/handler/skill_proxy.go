package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
)

// SkillProxyHandler proxies skill requests to the costrict-web internal API.
type SkillProxyHandler struct {
	proxy *service.SkillProxy
}

// NewSkillProxyHandler creates a new SkillProxyHandler.
func NewSkillProxyHandler(proxy *service.SkillProxy) *SkillProxyHandler {
	return &SkillProxyHandler{proxy: proxy}
}

// ListAgentSkills proxies a skill list request to costrict-web.
// GET /api/agent-skills
func (h *SkillProxyHandler) ListAgentSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := h.proxy.ListSkills()
	if err != nil {
		slog.Warn("skill proxy: list failed", "error", err)
		writeError(w, http.StatusBadGateway, "failed to list skills from upstream")
		return
	}

	writeJSON(w, http.StatusOK, skills)
}

// GetAgentSkill proxies a single skill request to costrict-web.
// GET /api/agent-skills/{id}
//
// Query params:
//   - agent_id: the requesting agent's ID (required for rate limiting and audit)
func (h *SkillProxyHandler) GetAgentSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing skill id")
		return
	}

	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "missing agent_id query parameter")
		return
	}

	skill, err := h.proxy.FetchSkill(r.Context(), id, agentID)
	if err != nil {
		if strings.Contains(err.Error(), "rate limit") {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		slog.Warn("skill proxy: fetch failed", "error", err, "skill_id", id, "agent_id", agentID)
		writeError(w, http.StatusBadGateway, "failed to fetch skill from upstream")
		return
	}

	writeJSON(w, http.StatusOK, skill)
}
