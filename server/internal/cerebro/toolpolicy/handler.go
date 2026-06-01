package toolpolicy

// handler.go exposes the tool-policy engine over HTTP (FIR-2230 phase 2 — the
// data layer the admin screen reads from). It is deliberately thin: the table
// view and the resolution live in table.go / chain.go; here we only translate
// request strings into a TableQuery / SetParams and shape the JSON.
//
// Routes (mounted under /api/workspaces/{id} in cmd/server/router.go):
//
//	GET    /tool-policy   list every tool with per-layer settings + Effective,
//	                      for the (runtime, agent, user, groups) context in the
//	                      query string. Any workspace member.
//	PUT    /tool-policy   set one layer's choice for one tool. Admin/owner.
//	DELETE /tool-policy   clear one layer's choice for one tool. Admin/owner.
//
// Read is member-level; writes are gated to admin/owner by the router group,
// mirroring grants. The admin table never shows raw ids: Title/Category come
// from the capability register, and subject ids are resolved to names by the
// frontend from data it already holds (members, agents, runtimes, groups).

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler wires the tool-policy Store to chi routes.
type Handler struct {
	Store *Store
}

// NewHandler constructs a Handler over the given Store.
func NewHandler(s *Store) *Handler { return &Handler{Store: s} }

// --- response types ---------------------------------------------------------

// layerSettings is the explicit per-layer choice for one tool, each field nil
// when that layer carries no explicit setting (Inherit). Keeping them as
// pointers lets the UI distinguish "no override" from an explicit "inherit".
type layerSettings struct {
	Workspace *string `json:"workspace"`
	Runtime   *string `json:"runtime"`
	Agent     *string `json:"agent"`
	Group     *string `json:"group"`
	User      *string `json:"user"`
}

type effectiveResponse struct {
	Setting   string `json:"setting"`
	DecidedBy string `json:"decided_by"`
	CappedBy  string `json:"capped_by"`
	Reason    string `json:"reason"`
}

type toolPolicyRow struct {
	ToolKey           string            `json:"tool_key"`
	ResourcePattern   string            `json:"resource_pattern"`
	Title             string            `json:"title"`
	Category          string            `json:"category"`
	Source            string            `json:"source"`
	ManagedExternally bool              `json:"managed_externally"`
	Layers            layerSettings     `json:"layers"`
	Effective         effectiveResponse `json:"effective"`
}

// --- request types ----------------------------------------------------------

type setRequest struct {
	ToolKey         string `json:"tool_key"`
	Layer           string `json:"layer"`
	SubjectID       string `json:"subject_id"`
	ResourcePattern string `json:"resource_pattern"`
	Setting         string `json:"setting"`
}

// --- handlers ---------------------------------------------------------------

// Table — GET /api/workspaces/{id}/tool-policy
// Query params: runtime_id, agent_id, user_id, group_id (repeatable), base.
func (h *Handler) Table(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	runtimeID, err := uuidParam(q.Get("runtime_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	agentID, err := uuidParam(q.Get("agent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	userID, err := uuidParam(q.Get("user_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	groupIDs, err := parseUUIDList(q["group_id"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid group_id")
		return
	}

	base := Setting(q.Get("base"))
	if base != "" && !validSetting(base) {
		writeError(w, http.StatusBadRequest, "invalid base")
		return
	}

	rows, err := h.Store.Table(r.Context(), TableQuery{
		WorkspaceID:     workspaceID,
		RuntimeID:       runtimeID,
		AgentID:         agentID,
		UserID:          userID,
		GroupIDs:        groupIDs,
		Base:            base,
		IncludePlatform: h.Store.PlatformCapabilitiesEnabled(r.Context(), workspaceID, member.UserID),
	})
	if err != nil {
		h.serverError(w, r, "list tool policy table", err)
		return
	}

	resp := make([]toolPolicyRow, len(rows))
	for i, row := range rows {
		resp[i] = toRowResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": resp})
}

// Set — PUT /api/workspaces/{id}/tool-policy
// Body: { tool_key, layer, subject_id, setting }.
func (h *Handler) Set(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ToolKey == "" {
		writeError(w, http.StatusBadRequest, "tool_key required")
		return
	}
	if !validLayer(Layer(req.Layer)) {
		writeError(w, http.StatusBadRequest, "invalid layer")
		return
	}
	if !validSetting(Setting(req.Setting)) {
		writeError(w, http.StatusBadRequest, "invalid setting")
		return
	}
	subjectID, err := util.ParseUUID(req.SubjectID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}

	if _, err := h.Store.Set(r.Context(), SetParams{
		WorkspaceID:     workspaceID,
		ToolKey:         req.ToolKey,
		Layer:           Layer(req.Layer),
		SubjectID:       subjectID,
		ResourcePattern: req.ResourcePattern,
		Setting:         Setting(req.Setting),
		UpdatedBy:       member.UserID,
	}); err != nil {
		if errors.Is(err, ErrUnknownLayer) || errors.Is(err, ErrUnknownSetting) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, r, "set tool policy", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Clear — DELETE /api/workspaces/{id}/tool-policy
// Query params: tool_key, layer, subject_id.
func (h *Handler) Clear(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	toolKey := q.Get("tool_key")
	if toolKey == "" {
		writeError(w, http.StatusBadRequest, "tool_key required")
		return
	}
	layer := Layer(q.Get("layer"))
	if !validLayer(layer) {
		writeError(w, http.StatusBadRequest, "invalid layer")
		return
	}
	subjectID, err := util.ParseUUID(q.Get("subject_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}

	resourcePattern := q.Get("resource_pattern")
	if err := h.Store.Clear(r.Context(), workspaceID, toolKey, layer, subjectID, resourcePattern); err != nil {
		if errors.Is(err, ErrUnknownLayer) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.serverError(w, r, "clear tool policy", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ----------------------------------------------------------------

func (h *Handler) loadWorkspace(w http.ResponseWriter, r *http.Request) (db.Member, pgtype.UUID, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return db.Member{}, pgtype.UUID{}, false
	}
	wsID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return db.Member{}, pgtype.UUID{}, false
	}
	return member, wsID, true
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("tool-policy request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func toRowResponse(row TableRow) toolPolicyRow {
	return toolPolicyRow{
		ToolKey:           row.ToolKey,
		ResourcePattern:   row.ResourcePattern,
		Title:             row.Title,
		Category:          row.Category,
		Source:            row.Source,
		ManagedExternally: row.ManagedExternally,
		Layers: layerSettings{
			Workspace: settingPtr(row.Layers, LayerWorkspace),
			Runtime:   settingPtr(row.Layers, LayerRuntime),
			Agent:     settingPtr(row.Layers, LayerAgent),
			Group:     settingPtr(row.Layers, LayerGroup),
			User:      settingPtr(row.Layers, LayerUser),
		},
		Effective: effectiveResponse{
			Setting:   string(row.Effective.Setting),
			DecidedBy: string(row.Effective.DecidedBy),
			CappedBy:  string(row.Effective.CappedBy),
			Reason:    row.Effective.Reason,
		},
	}
}

func settingPtr(layers map[Layer]Setting, l Layer) *string {
	if s, ok := layers[l]; ok {
		v := string(s)
		return &v
	}
	return nil
}

func parseUUIDList(raw []string) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, 0, len(raw))
	for _, item := range raw {
		if item == "" {
			continue
		}
		id, err := util.ParseUUID(item)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
