package handler

// TECH-3077: skill automation-link impact analysis handler.
// GET /api/skills/:id/impact — returns automation links and impact summary.
// Automation links are synced from skill.metadata->triggered_by on create/update.

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro"
)

// GetSkillImpact handles GET /api/skills/:id/impact.
func (h *Handler) GetSkillImpact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "impact analysis not available")
		return
	}

	impact, err := cerebro.GetSkillImpact(r.Context(), h.CerebroQueries, skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get skill impact")
		return
	}
	impact.SkillName = skill.Name

	writeJSON(w, http.StatusOK, impact)
}

// syncAutomationLinksAsync fires automation link sync in a goroutine.
// Errors are logged but not surfaced to the caller.
func (h *Handler) syncAutomationLinksAsync(skillID pgtype.UUID, metadataRaw []byte) {
	if h.CerebroQueries == nil || len(metadataRaw) == 0 {
		return
	}
	go func() {
		ctx := context.Background()
		if err := cerebro.SyncAutomationLinks(ctx, h.CerebroQueries, skillID, metadataRaw); err != nil {
			slog.Error("skill automation link sync failed", "skill_id", uuidToString(skillID), "error", err)
		}
	}()
}
