package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/pricing"
)

func (h *Handler) GetModelPricing(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	id, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	result, err := h.ModelPricing.Snapshot(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load model prices")
		return
	}
	result.CanManage = roleAllowed(member.Role, "owner", "admin")
	writeJSON(w, http.StatusOK, modelPricingResponse(result))
}

func (h *Handler) SaveModelPricing(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	id, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var body struct {
		Revision  *int64                     `json:"revision"`
		Overrides map[string]modelPricingRow `json:"overrides"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.Revision == nil || *body.Revision < 0 || body.Overrides == nil {
		writeError(w, http.StatusBadRequest, "invalid model prices")
		return
	}
	userID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	rows := make(map[string]pricing.Row, len(body.Overrides))
	for key, row := range body.Overrides {
		rows[key] = row.price()
	}
	err := h.ModelPricing.SaveOverrides(r.Context(), id, userID, *body.Revision, rows)
	if errors.Is(err, pricing.ErrConflict) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if errors.Is(err, pricing.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "could not save model prices")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save model prices")
		return
	}
	h.GetModelPricing(w, r)
}

func (h *Handler) RefreshModelPricing(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if err := h.ModelPricing.Refresh(r.Context(), true); err != nil {
		if !errors.Is(err, pricing.ErrRefresh) {
			writeError(w, http.StatusServiceUnavailable, "could not refresh model prices")
			return
		}
		// The response retains the last good prices and carries the refresh
		// diagnostic; clients can keep rendering without clearing the table.
		h.GetModelPricing(w, r)
		return
	}
	h.GetModelPricing(w, r)
}
