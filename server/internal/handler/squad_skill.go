package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxSquadPluginNameLen    = 128
	maxSquadPluginInstallLen = 1024
	maxSquadPlugins          = 50
)

// SquadSkillResponse is a workspace skill applied to a squad. Every member of
// the squad runs with it on every task.
type SquadSkillResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SquadPluginResponse is a plugin the squad leader must have on the runtime of
// each project the squad works on. InstallHint is optional: a source or command
// the leader can use to install it when it is missing.
type SquadPluginResponse struct {
	Name        string `json:"name"`
	InstallHint string `json:"install_hint"`
}

type SetSquadSkillsRequest struct {
	SkillIDs []string `json:"skill_ids"`
}

type SquadPluginRequest struct {
	Name        string `json:"name"`
	InstallHint string `json:"install_hint"`
}

type SetSquadPluginsRequest struct {
	Plugins []SquadPluginRequest `json:"plugins"`
}

func (h *Handler) ListSquadSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListSquadSkills(r.Context(), db.ListSquadSkillsParams{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad skills")
		return
	}
	resp := make([]SquadSkillResponse, len(rows))
	for i, row := range rows {
		resp[i] = SquadSkillResponse{
			ID:          uuidToString(row.ID),
			Name:        row.Name,
			Description: row.Description,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetSquadSkills replaces the squad's skill set. The client sends the full set,
// so the write clears and re-adds inside one transaction.
func (h *Handler) SetSquadSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if squad.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "squad is archived")
		return
	}

	var req SetSquadSkillsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	skillUUIDs, ok := parseUUIDSliceOrBadRequest(w, req.SkillIDs, "skill_ids")
	if !ok {
		return
	}
	if !h.validateSquadSkillIDsInWorkspace(w, r, squad, skillUUIDs) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteSquadSkills(r.Context(), db.DeleteSquadSkillsParams{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear squad skills")
		return
	}
	for _, skillID := range uniquePgUUIDs(skillUUIDs) {
		if err := qtx.AddSquadSkill(r.Context(), db.AddSquadSkillParams{
			WorkspaceID: squad.WorkspaceID,
			SquadID:     squad.ID,
			SkillID:     skillID,
			CreatedBy:   member.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add squad skill")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
	h.ListSquadSkills(w, r)
}

func (h *Handler) ListSquadPlugins(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListSquadPlugins(r.Context(), db.ListSquadPluginsParams{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list squad plugins")
		return
	}
	resp := make([]SquadPluginResponse, len(rows))
	for i, row := range rows {
		resp[i] = SquadPluginResponse{Name: row.PluginName, InstallHint: row.InstallHint}
	}
	writeJSON(w, http.StatusOK, resp)
}

// SetSquadPlugins replaces the squad's plugin requirements. Names are trimmed
// and de-duplicated case-sensitively; the first install hint given for a name wins.
func (h *Handler) SetSquadPlugins(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "workspaceId")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	squad, _, ok := h.loadSquadInWorkspace(w, r)
	if !ok {
		return
	}
	if !canManageSquad(member, squad) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if squad.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "squad is archived")
		return
	}

	var req SetSquadPluginsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	plugins, msg := normalizeSquadPlugins(req.Plugins)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.DeleteSquadPlugins(r.Context(), db.DeleteSquadPluginsParams{
		WorkspaceID: squad.WorkspaceID,
		SquadID:     squad.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear squad plugins")
		return
	}
	for _, p := range plugins {
		if err := qtx.AddSquadPlugin(r.Context(), db.AddSquadPluginParams{
			WorkspaceID: squad.WorkspaceID,
			SquadID:     squad.ID,
			PluginName:  p.Name,
			InstallHint: p.InstallHint,
			CreatedBy:   member.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add squad plugin")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	h.publish(protocol.EventSquadUpdated, workspaceID, "member", requestUserID(r), map[string]any{
		"squad_id": uuidToString(squad.ID),
	})
	h.ListSquadPlugins(w, r)
}

// normalizeSquadPlugins trims, validates and de-duplicates plugin requirements.
// A non-empty second result is the client-facing reason the input was rejected.
func normalizeSquadPlugins(in []SquadPluginRequest) ([]SquadPluginResponse, string) {
	seen := make(map[string]struct{}, len(in))
	out := make([]SquadPluginResponse, 0, len(in))
	for _, p := range in {
		name := strings.TrimSpace(p.Name)
		hint := strings.TrimSpace(p.InstallHint)
		if name == "" {
			return nil, "plugin name is required"
		}
		if len(name) > maxSquadPluginNameLen {
			return nil, "plugin name is too long"
		}
		if len(hint) > maxSquadPluginInstallLen {
			return nil, "plugin install hint is too long"
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, SquadPluginResponse{Name: name, InstallHint: hint})
	}
	if len(out) > maxSquadPlugins {
		return nil, "too many plugins on one squad"
	}
	return out, ""
}

func (h *Handler) validateSquadSkillIDsInWorkspace(w http.ResponseWriter, r *http.Request, squad db.Squad, skillUUIDs []pgtype.UUID) bool {
	for _, skillID := range uniquePgUUIDs(skillUUIDs) {
		if _, err := h.Queries.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skillID,
			WorkspaceID: squad.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "skill not found")
			return false
		}
	}
	return true
}

func uniquePgUUIDs(ids []pgtype.UUID) []pgtype.UUID {
	seen := make(map[string]struct{}, len(ids))
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		key := uuidToString(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	return out
}
