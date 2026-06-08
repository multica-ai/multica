package handler

// TECH-3077: Skill self-learning — observation recording and retrieval.
// POST /api/skill-observations — record a delta behavior (called by agent runtime)
// GET  /api/skills/:id/observations — list observations + aggregates for a skill

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/cerebro"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// RecordSkillObservation handles POST /api/skill-observations.
// Called by the agent runtime when a delta behavior is detected during a skill run.
func (h *Handler) RecordSkillObservation(w http.ResponseWriter, r *http.Request) {
	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "skill learning not available")
		return
	}

	var req struct {
		SkillID      string  `json:"skill_id"`
		SkillVersion string  `json:"skill_version"`
		RunID        string  `json:"run_id"`
		AutopilotID  string  `json:"autopilot_id"`
		DeltaType    string  `json:"delta_type"`
		DeltaValue   string  `json:"delta_value"`
		Confidence   float64 `json:"confidence"`
		Outcome      string  `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SkillID == "" || req.DeltaType == "" || req.DeltaValue == "" {
		writeError(w, http.StatusBadRequest, "skill_id, delta_type, and delta_value are required")
		return
	}

	skillUUID, ok := parseUUIDOrBadRequest(w, req.SkillID, "skill_id")
	if !ok {
		return
	}

	obs := cerebro.ObservationInput{
		SkillID:      skillUUID,
		SkillVersion: req.SkillVersion,
		DeltaType:    req.DeltaType,
		DeltaValue:   req.DeltaValue,
		Confidence:   req.Confidence,
		Outcome:      req.Outcome,
	}
	if req.RunID != "" {
		uuid, ok := parseUUIDOrBadRequest(w, req.RunID, "run_id")
		if !ok {
			return
		}
		obs.RunID = uuid
	}
	if req.AutopilotID != "" {
		uuid, ok := parseUUIDOrBadRequest(w, req.AutopilotID, "autopilot_id")
		if !ok {
			return
		}
		obs.AutopilotID = uuid
	}

	if err := cerebro.RecordObservation(r.Context(), h.CerebroQueries, obs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record observation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

// GetSkillObservations handles GET /api/skills/:id/observations.
func (h *Handler) GetSkillObservations(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	if h.CerebroQueries == nil {
		writeError(w, http.StatusServiceUnavailable, "skill learning not available")
		return
	}

	aggregates, err := h.CerebroQueries.ListObservationAggregates(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get observations")
		return
	}

	recentObs, err := h.CerebroQueries.ListSkillObservations(r.Context(), cerebrodb.ListSkillObservationsParams{
		SkillID: skill.ID,
		Lim:     20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get observations")
		return
	}

	type aggView struct {
		DeltaType      string  `json:"delta_type"`
		DeltaValue     string  `json:"delta_value"`
		RunCount       int32   `json:"run_count"`
		ConsistencyPct float64 `json:"consistency_pct"`
		Proposed       bool    `json:"proposed"`
		AboveThreshold bool    `json:"above_threshold"`
	}
	type obsView struct {
		DeltaType  string  `json:"delta_type"`
		DeltaValue string  `json:"delta_value"`
		Confidence float64 `json:"confidence"`
		Outcome    string  `json:"outcome"`
		CreatedAt  string  `json:"created_at"`
	}

	aggViews := make([]aggView, len(aggregates))
	for i, a := range aggregates {
		aggViews[i] = aggView{
			DeltaType:      a.DeltaType,
			DeltaValue:     a.DeltaValue,
			RunCount:       a.RunCount,
			ConsistencyPct: a.ConsistencyPct,
			Proposed:       a.AutoProposedAt.Valid,
			AboveThreshold: a.RunCount >= 10 && a.ConsistencyPct >= 0.70,
		}
	}

	obsViews := make([]obsView, len(recentObs))
	for i, o := range recentObs {
		obsViews[i] = obsView{
			DeltaType:  o.DeltaType,
			DeltaValue: o.DeltaValue,
			Confidence: o.Confidence,
			Outcome:    o.Outcome,
			CreatedAt:  timestampToString(o.CreatedAt),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"skill_id":   uuidToString(skill.ID),
		"skill_name": skill.Name,
		"patterns":   aggViews,
		"recent":     obsViews,
	})
}
