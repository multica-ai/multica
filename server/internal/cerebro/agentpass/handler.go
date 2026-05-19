// Package agentpass — admin HTTP API for cerebro agent passes (JEH-1731).
//
// Endpoints (mounted under the workspace-member middleware in router.go):
//
//	GET    /api/cerebro/agent-passes              — list passes for the caller's workspace
//	POST   /api/cerebro/agent-passes              — issue a new pass (validates downscoping)
//	DELETE /api/cerebro/agent-passes/{passId}     — revoke an active pass
//
// Auth: workspace owner/admin only. The same threat model that gates
// runtime-tool admin (handler.runtime_tools_admin_cerebro.go) applies here
// — issuing a pass effectively governs what an agent can do.
//
// The POST handler calls Service.ValidateChildScope BEFORE the INSERT so
// privilege escalation (a child pass that widens its parent's scope) is
// rejected with HTTP 422 and never reaches the database. This is the
// production callsite for the ValidateChildScope code from PR #372 that
// was previously only exercised in unit tests.
package agentpass

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

// Handler owns the cerebro agent-pass admin endpoints. The Service it
// embeds is the same instance the gate uses; calling ValidateChildScope
// here keeps the rule "child cannot widen parent" enforced at the only
// place new passes are minted.
type Handler struct {
	Cerebro *cerebrodb.Queries
	Service *Service
}

// NewHandler wires the cerebro queries and the agentpass service into a
// handler. Both must be non-nil — the handler errors on construction
// rather than NPE'ing at request time.
func NewHandler(cerebro *cerebrodb.Queries, svc *Service) *Handler {
	return &Handler{Cerebro: cerebro, Service: svc}
}

// passResponse is the wire shape returned by all three endpoints. Mirrors
// cerebrodb.CerebroAgentPass but only exposes the fields the UI needs and
// uses plain Go types (no pgtype.X) so the JSON is stable across upgrades.
type passResponse struct {
	ID                 string          `json:"id"`
	WorkspaceID        string          `json:"workspace_id"`
	IssuerType         string          `json:"issuer_type"`
	IssuerID           string          `json:"issuer_id"`
	AgentID            string          `json:"agent_id"`
	IssueID            string          `json:"issue_id"`
	ParentPassID       string          `json:"parent_pass_id,omitempty"`
	Scope              json.RawMessage `json:"scope"`
	SpendCeilingMicros *int64          `json:"spend_ceiling_micros,omitempty"`
	Status             string          `json:"status"`
	IssuedAt           string          `json:"issued_at"`
	ExpiresAt          string          `json:"expires_at,omitempty"`
	RevokedByType      string          `json:"revoked_by_type,omitempty"`
	RevokedByID        string          `json:"revoked_by_id,omitempty"`
	RevokedAt          string          `json:"revoked_at,omitempty"`
	RevokeReason       string          `json:"revoke_reason,omitempty"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
}

type listResponse struct {
	Passes []passResponse `json:"passes"`
}

// createRequest is the POST body. Scope is raw JSON so callers can attach
// any narrowing keys (allowed_tools, allowed_capabilities, …) without a
// schema migration. The agentpass scope validator parses it.
type createRequest struct {
	AgentID            string          `json:"agent_id"`
	IssueID            string          `json:"issue_id"`
	ParentPassID       string          `json:"parent_pass_id,omitempty"`
	Scope              json.RawMessage `json:"scope,omitempty"`
	SpendCeilingMicros *int64          `json:"spend_ceiling_micros,omitempty"`
	ExpiresAt          string          `json:"expires_at,omitempty"`
}

type revokeRequest struct {
	Reason string `json:"reason,omitempty"`
}

// List handles GET /api/cerebro/agent-passes.
//
// Returns every pass in the caller's workspace, active rows first, then
// terminal-state rows by most recently updated. Owner/admin only.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	if !isAdminRole(member.Role) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can view agent passes")
		return
	}

	rows, err := h.Cerebro.ListCerebroAgentPassesForWorkspace(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list agent passes: "+err.Error())
		return
	}

	out := make([]passResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPassResponse(row))
	}
	writeJSON(w, http.StatusOK, listResponse{Passes: out})
}

// Create handles POST /api/cerebro/agent-passes.
//
// The flow is:
//
//  1. Auth: owner/admin only.
//  2. Parse body, validate UUIDs.
//  3. If parent_pass_id is set, validate child scope narrows parent scope
//     — reject with 422 otherwise. This is the production callsite for
//     ValidateChildScope and the only place where new passes are minted.
//  4. Insert. The unique partial index on (agent_id, issue_id) WHERE
//     status='active' surfaces concurrent duplicates as a 409.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	if !isAdminRole(member.Role) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can issue agent passes")
		return
	}

	var body createRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	agentID, err := util.ParseUUID(body.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	issueID, err := util.ParseUUID(body.IssueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue_id")
		return
	}

	var parentPassID pgtype.UUID
	if strings.TrimSpace(body.ParentPassID) != "" {
		parentPassID, err = util.ParseUUID(body.ParentPassID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid parent_pass_id")
			return
		}
	}

	scope := normalizeScope(body.Scope)

	// Production callsite for ValidateChildScope (JEH-1327 milestone 2).
	// Privilege escalation — a child that widens its parent's scope — must
	// fail BEFORE the INSERT so the database never holds an escalated row
	// even briefly.
	if parentPassID.Valid {
		if err := h.Service.ValidateChildScope(r.Context(), parentPassID, scope); err != nil {
			if errors.Is(err, ErrScopeWiden) {
				writeError(w, http.StatusUnprocessableEntity, "agent_pass_scope_widening: "+err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, "validate scope: "+err.Error())
			return
		}
	}

	params := cerebrodb.CreateCerebroAgentPassParams{
		WorkspaceID:        member.WorkspaceID,
		IssuerType:         IssuerMember,
		IssuerID:           member.UserID,
		AgentID:            agentID,
		IssueID:            issueID,
		ParentPassID:       parentPassID,
		Scope:              scope,
		SpendCeilingMicros: pgInt8Pointer(body.SpendCeilingMicros),
		ExpiresAt:          pgTimestamp(body.ExpiresAt),
	}

	pass, err := h.Cerebro.CreateCerebroAgentPass(r.Context(), params)
	if err != nil {
		// 23505 = unique_violation. The partial unique index on
		// (agent_id, issue_id) WHERE status='active' triggers this when
		// the operator tries to issue a second active pass for the same
		// (agent, issue) — surface as 409 so the UI can prompt to
		// revoke the existing pass first instead of just failing.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "an active pass already exists for this agent + issue")
			return
		}
		writeError(w, http.StatusInternalServerError, "create agent pass: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toPassResponse(pass))
}

// Revoke handles DELETE /api/cerebro/agent-passes/{passId}.
//
// Idempotent on the active → revoked transition; revoking an already-
// revoked / expired / exhausted pass returns 409 so the operator gets a
// clear signal rather than silent success.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	if !isAdminRole(member.Role) {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can revoke agent passes")
		return
	}

	passID, err := util.ParseUUID(chi.URLParam(r, "passId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pass id")
		return
	}

	// Confirm the pass belongs to the caller's workspace before mutating.
	// Without this check the DELETE would 404 cross-workspace passes via
	// the WHERE status='active' guard in RevokeAgentPass, but the error
	// message would be misleading.
	existing, err := h.Cerebro.GetCerebroAgentPass(r.Context(), passID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent pass not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "load agent pass: "+err.Error())
		return
	}
	if !uuidEqual(existing.WorkspaceID, member.WorkspaceID) {
		writeError(w, http.StatusNotFound, "agent pass not found")
		return
	}

	var body revokeRequest
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	reason := strings.TrimSpace(body.Reason)
	revokeParams := cerebrodb.RevokeAgentPassParams{
		ID:            passID,
		RevokedByType: pgtype.Text{String: IssuerMember, Valid: true},
		RevokedByID:   member.UserID,
		RevokeReason:  pgtype.Text{String: reason, Valid: reason != ""},
	}

	pass, err := h.Cerebro.RevokeAgentPass(r.Context(), revokeParams)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// RevokeAgentPass only matches WHERE status='active', so an
			// ErrNoRows here means the pass exists but is already in a
			// terminal state. Tell the caller what state it's in.
			writeError(w, http.StatusConflict, "agent pass is not active (status="+existing.Status+")")
			return
		}
		writeError(w, http.StatusInternalServerError, "revoke agent pass: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toPassResponse(pass))
}

// normalizeScope returns a non-empty JSON object for storage. An empty or
// null scope from the client is rewritten to "{}" so the column never
// holds NULL or invalid JSON — Postgres also enforces this via the column
// DEFAULT but we prefer to keep the wire shape consistent.
func normalizeScope(raw json.RawMessage) []byte {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []byte("{}")
	}
	return []byte(trimmed)
}

func pgInt8Pointer(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

func pgTimestamp(raw string) pgtype.Timestamptz {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func toPassResponse(p cerebrodb.CerebroAgentPass) passResponse {
	resp := passResponse{
		ID:          util.UUIDToString(p.ID),
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		IssuerType:  p.IssuerType,
		IssuerID:    util.UUIDToString(p.IssuerID),
		AgentID:     util.UUIDToString(p.AgentID),
		IssueID:     util.UUIDToString(p.IssueID),
		Scope:       json.RawMessage(p.Scope),
		Status:      p.Status,
		IssuedAt:    timestampString(p.IssuedAt),
		CreatedAt:   timestampString(p.CreatedAt),
		UpdatedAt:   timestampString(p.UpdatedAt),
	}
	if len(resp.Scope) == 0 {
		resp.Scope = json.RawMessage("{}")
	}
	if p.ParentPassID.Valid {
		resp.ParentPassID = util.UUIDToString(p.ParentPassID)
	}
	if p.SpendCeilingMicros.Valid {
		v := p.SpendCeilingMicros.Int64
		resp.SpendCeilingMicros = &v
	}
	if p.ExpiresAt.Valid {
		resp.ExpiresAt = timestampString(p.ExpiresAt)
	}
	if p.RevokedByType.Valid {
		resp.RevokedByType = p.RevokedByType.String
	}
	if p.RevokedByID.Valid {
		resp.RevokedByID = util.UUIDToString(p.RevokedByID)
	}
	if p.RevokedAt.Valid {
		resp.RevokedAt = timestampString(p.RevokedAt)
	}
	if p.RevokeReason.Valid {
		resp.RevokeReason = p.RevokeReason.String
	}
	return resp
}

func timestampString(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
}

func isAdminRole(role string) bool {
	return role == "owner" || role == "admin"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
