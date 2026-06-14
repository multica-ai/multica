package handler

import (
	"encoding/json"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Capability types ──────────────────────────────────────────────────────────

// SquadCapability is the JSON shape stored in the capability JSONB column.
type SquadCapability struct {
	Domains     []string `json:"domains"`
	Keywords    []string `json:"keywords"`
	Description string   `json:"description"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func parseCapability(raw []byte) SquadCapability {
	if len(raw) == 0 {
		return SquadCapability{}
	}
	var c SquadCapability
	if err := json.Unmarshal(raw, &c); err != nil {
		return SquadCapability{}
	}
	if c.Domains == nil {
		c.Domains = []string{}
	}
	if c.Keywords == nil {
		c.Keywords = []string{}
	}
	return c
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// SetSquadCapability upserts the capability JSON for a squad.
// Idempotent — calling it twice with the same payload produces the same result.
func (h *Handler) SetSquadCapability(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	var req SquadCapability
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	raw, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode capability")
		return
	}

	if err := h.Queries.SetSquadCapability(r.Context(), db.SetSquadCapabilityParams{
		ID:         squad.ID,
		Capability: raw,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set squad capability")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"squad_id":   uuidToString(squad.ID),
		"capability": req,
	})
}

// GetSquadCapability returns the capability for a single squad.
func (h *Handler) GetSquadCapability(w http.ResponseWriter, r *http.Request) {
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	// Re-fetch capability directly by squad ID.
	raw, err := h.Queries.GetSquadCapability(r.Context(), squad.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read squad capability")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"squad_id":   uuidToString(squad.ID),
		"name":       squad.Name,
		"capability": parseCapability(raw),
	})
}

// DeleteSquadCapability resets a squad's capability to empty.
func (h *Handler) DeleteSquadCapability(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}

	if err := h.Queries.SetSquadCapability(r.Context(), db.SetSquadCapabilityParams{
		ID:         squad.ID,
		Capability: []byte("{}"),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear squad capability")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"squad_id": uuidToString(squad.ID),
		"cleared":  true,
	})
}

// ListSquadCapabilities returns all squads' capabilities in the workspace.
func (h *Handler) ListSquadCapabilities(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListSquadsWithCapability(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad capabilities")
		return
	}

	type capabilityEntry struct {
		SquadID    string           `json:"squad_id"`
		Name       string           `json:"name"`
		Capability SquadCapability  `json:"capability"`
	}

	resp := make([]capabilityEntry, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, capabilityEntry{
			SquadID:    uuidToString(row.ID),
			Name:       row.Name,
			Capability: parseCapability(row.Capability),
		})
	}

	writeJSON(w, http.StatusOK, resp)
}
