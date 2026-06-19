package permissionexplain

// permissionexplain is the FIR-1496 "one read model" first slice: given ONE
// subject (a workspace member or an agent), report for every platform/tool
// capability the EFFECTIVE permission and WHY — which layer decided it, plus a
// plain-language how_to_fix and who_can_fix.
//
// It is deliberately thin and reuses the existing tool-policy read model
// (toolpolicy.Store.Table) rather than re-deriving resolution. The handler only
// resolves the subject's chain context (UserID/role/groups for a member,
// AgentID/runtime/groups for an agent), runs the same Table query the admin
// permission screen reads from, and annotates each row with how_to_fix /
// who_can_fix derived from the row's Effective verdict.
//
// Route (mounted under /api/workspaces/{id} in cmd/server/router.go, member-read
// group next to /tool-policy and /grants):
//
//	GET /api/workspaces/{id}/permissions/explain
//	    ?subject_type=member|agent (required)
//	    &subject_id=<uuid>          (required)
//	    &capability=<tool_key>      (optional filter)
//	    &base=<setting>             (optional default passthrough)

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler resolves a subject's chain context and runs the tool-policy Table read
// model, annotating each row with how_to_fix / who_can_fix.
type Handler struct {
	store   *toolpolicy.Store
	queries *db.Queries
	pool    *pgxpool.Pool
}

// NewHandler constructs a Handler. It builds its own toolpolicy.Store over the
// pool (matching how the other cerebro handlers each take a fresh Store) and
// keeps the upstream queries for member/agent lookups plus the pool for the
// agent-group read that has no generated query.
func NewHandler(pool *pgxpool.Pool, queries *db.Queries) *Handler {
	return &Handler{
		store:   toolpolicy.NewStore(pool),
		queries: queries,
		pool:    pool,
	}
}

// --- response types ---------------------------------------------------------

type subjectResponse struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	Role      *string  `json:"role"`
	RuntimeID *string  `json:"runtime_id"`
	GroupIDs  []string `json:"group_ids"`
}

// layerSettings mirrors toolpolicy/handler.go: each field nil when that layer
// carries no explicit setting (Inherit).
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

type groupAttributionResponse struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type capabilityResponse struct {
	ToolKey           string                     `json:"tool_key"`
	ResourcePattern   string                     `json:"resource_pattern"`
	Title             string                     `json:"title"`
	Category          string                     `json:"category"`
	Source            string                     `json:"source"`
	ManagedExternally bool                       `json:"managed_externally"`
	Layers            layerSettings              `json:"layers"`
	Effective         effectiveResponse          `json:"effective"`
	CappedByGroups    []groupAttributionResponse `json:"capped_by_groups"`
	HowToFix          string                     `json:"how_to_fix"`
	WhoCanFix         string                     `json:"who_can_fix"`
}

type explainResponse struct {
	Subject      subjectResponse      `json:"subject"`
	Capabilities []capabilityResponse `json:"capabilities"`
}

// --- handler ----------------------------------------------------------------

// Explain — GET /api/workspaces/{id}/permissions/explain
func (h *Handler) Explain(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	subjectType := q.Get("subject_type")
	if subjectType != "member" && subjectType != "agent" {
		writeError(w, http.StatusBadRequest, "subject_type must be 'member' or 'agent'")
		return
	}
	subjectID, err := util.ParseUUID(q.Get("subject_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid subject_id")
		return
	}

	base := toolpolicy.Setting(q.Get("base"))
	if base != "" && !validSetting(base) {
		writeError(w, http.StatusBadRequest, "invalid base")
		return
	}

	tq := toolpolicy.TableQuery{WorkspaceID: workspaceID, Base: base}
	subj := subjectResponse{Type: subjectType, ID: q.Get("subject_id")}

	switch subjectType {
	case "member":
		m, err := h.queries.GetMember(r.Context(), subjectID)
		if err != nil {
			h.notFoundOrServerError(w, r, "load member", err)
			return
		}
		if !uuidEqual(m.WorkspaceID, workspaceID) {
			writeError(w, http.StatusNotFound, "member not found in workspace")
			return
		}
		tq.UserID = m.UserID
		role := m.Role
		subj.Role = &role
		// Leave tq.GroupIDs nil so Store.Table expands the user's real groups
		// itself (same path the admin tool-policy table relies on). We resolve
		// them separately only to echo them back in the subject block.
		groupIDs, err := h.resolveUserGroupIDs(r.Context(), workspaceID, m.UserID)
		if err != nil {
			h.serverError(w, r, "resolve member groups", err)
			return
		}
		subj.GroupIDs = uuidStrings(groupIDs)
	case "agent":
		a, err := h.queries.GetAgent(r.Context(), subjectID)
		if err != nil {
			h.notFoundOrServerError(w, r, "load agent", err)
			return
		}
		if !uuidEqual(a.WorkspaceID, workspaceID) {
			writeError(w, http.StatusNotFound, "agent not found in workspace")
			return
		}
		tq.AgentID = subjectID
		if a.RuntimeID.Valid {
			tq.RuntimeID = a.RuntimeID
			rid := uuidString(a.RuntimeID)
			subj.RuntimeID = &rid
		}
		groupIDs, err := h.resolveAgentGroupIDs(r.Context(), workspaceID, subjectID)
		if err != nil {
			h.serverError(w, r, "resolve agent groups", err)
			return
		}
		tq.GroupIDs = groupIDs
		subj.GroupIDs = uuidStrings(groupIDs)
	}

	tq.IncludePlatform = h.store.PlatformCapabilitiesEnabled(r.Context(), workspaceID, member.UserID)

	rows, err := h.store.Table(r.Context(), tq)
	if err != nil {
		h.serverError(w, r, "list permission table", err)
		return
	}

	capabilityFilter := q.Get("capability")
	caps := make([]capabilityResponse, 0, len(rows))
	for _, row := range rows {
		if capabilityFilter != "" && row.ToolKey != capabilityFilter {
			continue
		}
		caps = append(caps, toCapabilityResponse(row))
	}

	writeJSON(w, http.StatusOK, explainResponse{Subject: subj, Capabilities: caps})
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

// resolveAgentGroupIDs returns the group ids whose agent allowlist
// (cerebro_group_agent_access) includes this agent, scoped to the workspace.
// There is no generated query for this direction, so it reads the join table
// directly with the pool — the same raw-pool pattern the tool-policy table read
// model uses for reads only the explain/admin views need.
func (h *Handler) resolveAgentGroupIDs(ctx context.Context, workspaceID, agentID pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT a.group_id
		FROM cerebro_group_agent_access a
		JOIN cerebro_group g ON g.id = a.group_id
		WHERE a.agent_id = $1 AND g.workspace_id = $2
	`, agentID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]pgtype.UUID, 0)
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// resolveUserGroupIDs returns the group ids the user belongs to in the
// workspace (cerebro_group_member), mirroring the generated
// ListCerebroGroupsForUser query. Read with the pool so the explain view does
// not depend on the cerebro query set the rest of this handler avoids.
func (h *Handler) resolveUserGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT g.id
		FROM cerebro_group g
		JOIN cerebro_group_member gm ON gm.group_id = g.id
		WHERE g.workspace_id = $1 AND gm.user_id = $2
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]pgtype.UUID, 0)
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("permission-explain request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func (h *Handler) notFoundOrServerError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "subject not found")
		return
	}
	h.serverError(w, r, op, err)
}
