package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler wires the approvals service to chi routes.
// Read operations (GET) require any workspace member.
// Decisions (approve/reject/delegate) and the intake seam require admin/owner —
// enforced on the router's admin group.
type Handler struct {
	Svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{Svc: svc} }

// --- response types ---------------------------------------------------------

type approvalResponse struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspace_id"`
	RequesterType   string   `json:"requester_type"`
	RequesterID     string   `json:"requester_id"`
	AgentID         *string  `json:"agent_id"`
	Capability      string   `json:"capability"`
	Resource        string   `json:"resource"`
	Reason          string   `json:"reason"`
	MatchedGrantIDs []string `json:"matched_grant_ids"`
	Context         any      `json:"context"`
	Status          string   `json:"status"`
	DecidedByID     *string  `json:"decided_by_id"`
	DecidedAt       *string  `json:"decided_at"`
	DecisionNote    *string  `json:"decision_note"`
	DelegatedToType *string  `json:"delegated_to_type"`
	DelegatedToID   *string  `json:"delegated_to_id"`
	ExpiresAt       *string  `json:"expires_at"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type approvalAuditResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ApprovalID  *string `json:"approval_id"`
	Action      string  `json:"action"`
	ActorType   *string `json:"actor_type"`
	ActorID     *string `json:"actor_id"`
	ActorName   *string `json:"actor_name"`
	Via         string  `json:"via"`
	Note        *string `json:"note"`
	CreatedAt   string  `json:"created_at"`
}

// --- request types ----------------------------------------------------------

type decisionRequest struct {
	Note string `json:"note"`
}

type delegateRequest struct {
	ToType string `json:"to_type"` // member | group
	ToID   string `json:"to_id"`
	Note   string `json:"note"`
}

// intakeRequest backs the admin-only intake seam. The live enforcement gate
// calls Service.Intake directly; this HTTP endpoint exists so the inbox flow
// can be exercised end-to-end before phase 2's gate is wired in.
type intakeRequest struct {
	RequesterType   string         `json:"requester_type"`
	RequesterID     string         `json:"requester_id"`
	AgentID         *string        `json:"agent_id"`
	Capability      string         `json:"capability"`
	Resource        string         `json:"resource"`
	Reason          string         `json:"reason"`
	MatchedGrantIDs []string       `json:"matched_grant_ids"`
	Context         map[string]any `json:"context"`
	ExpiresAt       *string        `json:"expires_at"`
}

// --- handlers ---------------------------------------------------------------

// List — GET /api/workspaces/{id}/approvals?status=&limit=&offset=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	f := ListFilter{Status: r.URL.Query().Get("status")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		f.Limit = int32(n)
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		f.Offset = int32(n)
	}

	rows, err := h.Svc.List(r.Context(), workspaceID, f)
	if err != nil {
		h.serverError(w, r, "list approvals", err)
		return
	}
	pending, err := h.Svc.CountPending(r.Context(), workspaceID)
	if err != nil {
		h.serverError(w, r, "count pending approvals", err)
		return
	}

	items := make([]approvalResponse, len(rows))
	total := int32(0)
	for i, row := range rows {
		if row.Total > total {
			total = row.Total
		}
		items[i] = listRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"approvals": items,
		"total":     total,
		"pending":   pending,
		"limit":     f.Limit,
		"offset":    f.Offset,
	})
}

// Get — GET /api/workspaces/{id}/approvals/{approvalId}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	row, err := h.Svc.Get(r.Context(), id, workspaceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "approval not found")
			return
		}
		h.serverError(w, r, "get approval", err)
		return
	}
	writeJSON(w, http.StatusOK, requestToResponse(row))
}

// Audit — GET /api/workspaces/{id}/approvals/audit?approval_id=&limit=&offset=
func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := cerebrodb.ListCerebroApprovalAuditParams{
		WorkspaceID: workspaceID,
		Limit:       50,
		Offset:      0,
	}
	if raw := q.Get("approval_id"); raw != "" {
		id, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid approval_id")
			return
		}
		filter.Column2 = id
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = int32(n)
	}
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		filter.Offset = int32(n)
	}

	rows, err := h.Svc.Cerebro.ListCerebroApprovalAudit(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, "list approval audit", err)
		return
	}
	items := make([]approvalAuditResponse, len(rows))
	total := int32(0)
	for i, row := range rows {
		if row.Total > total {
			total = row.Total
		}
		items[i] = auditRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// Approve — POST /api/workspaces/{id}/approvals/{approvalId}/approve
func (h *Handler) Approve(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, h.Svc.Approve)
}

// Reject — POST /api/workspaces/{id}/approvals/{approvalId}/reject
func (h *Handler) Reject(w http.ResponseWriter, r *http.Request) {
	h.decide(w, r, h.Svc.Reject)
}

// decideFn is the shared shape of Service.Approve / Service.Reject.
type decideFn func(ctx context.Context, id, workspaceID, actorID pgtype.UUID, note, surface string) (cerebrodb.CerebroApprovalRequest, error)

func (h *Handler) decide(w http.ResponseWriter, r *http.Request, fn decideFn) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	req := decisionRequest{}
	_ = decodeOptional(r, &req)

	row, err := fn(r.Context(), id, workspaceID, member.UserID, req.Note, surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")))
	if err != nil {
		h.writeDecisionError(w, r, "decide approval", err)
		return
	}
	writeJSON(w, http.StatusOK, requestToResponse(row))
}

// Delegate — POST /api/workspaces/{id}/approvals/{approvalId}/delegate
func (h *Handler) Delegate(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	id, ok := h.parseApprovalID(w, r)
	if !ok {
		return
	}
	var req delegateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ToType != DelegateToMember && req.ToType != DelegateToGroup {
		writeError(w, http.StatusBadRequest, "to_type must be 'member' or 'group'")
		return
	}
	toID, err := util.ParseUUID(req.ToID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to_id")
		return
	}

	row, err := h.Svc.Delegate(r.Context(), id, workspaceID, member.UserID, req.ToType, toID, req.Note, surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")))
	if err != nil {
		h.writeDecisionError(w, r, "delegate approval", err)
		return
	}
	writeJSON(w, http.StatusOK, requestToResponse(row))
}

// Intake — POST /api/workspaces/{id}/approvals/intake (admin/owner).
// Test/integration seam; the live gate calls Service.Intake directly.
func (h *Handler) Intake(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	var req intakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RequesterType != RequesterMember && req.RequesterType != RequesterAgent {
		writeError(w, http.StatusBadRequest, "requester_type must be 'member' or 'agent'")
		return
	}
	if req.Capability == "" {
		writeError(w, http.StatusBadRequest, "capability required")
		return
	}

	requesterID := member.UserID
	if req.RequesterID != "" {
		parsed, err := util.ParseUUID(req.RequesterID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid requester_id")
			return
		}
		requesterID = parsed
	}

	p := IntakeParams{
		WorkspaceID:   workspaceID,
		RequesterType: req.RequesterType,
		RequesterID:   requesterID,
		Capability:    req.Capability,
		Resource:      req.Resource,
		Reason:        req.Reason,
		Context:       req.Context,
		Surface:       surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")),
	}
	if req.AgentID != nil {
		agentID, err := util.ParseUUID(*req.AgentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid agent_id")
			return
		}
		p.Agent = agentID
	}
	for _, g := range req.MatchedGrantIDs {
		gid, err := util.ParseUUID(g)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid matched_grant_id")
			return
		}
		p.MatchedGrantIDs = append(p.MatchedGrantIDs, gid)
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		var ts pgtype.Timestamptz
		if err := ts.Scan(*req.ExpiresAt); err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at")
			return
		}
		p.ExpiresAt = ts
	}

	row, err := h.Svc.Intake(r.Context(), p)
	if err != nil {
		h.serverError(w, r, "intake approval", err)
		return
	}
	writeJSON(w, http.StatusCreated, requestToResponse(row))
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

func (h *Handler) parseApprovalID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	id, err := util.ParseUUID(chi.URLParam(r, "approvalId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid approval id")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (h *Handler) writeDecisionError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "approval not found")
	case errors.Is(err, ErrAlreadyDecided):
		// 409 lets the UI tell the approver someone else already handled it.
		writeError(w, http.StatusConflict, "approval already decided")
	default:
		h.serverError(w, r, op, err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("approvals request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func decodeOptional(r *http.Request, v any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(v)
}

func surfaceFromHeader(h string) string {
	switch h {
	case SurfaceCLI, SurfaceUI, SurfaceMCP:
		return h
	}
	return SurfaceAPI
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func requestToResponse(a cerebrodb.CerebroApprovalRequest) approvalResponse {
	return approvalResponse{
		ID:              util.UUIDToString(a.ID),
		WorkspaceID:     util.UUIDToString(a.WorkspaceID),
		RequesterType:   a.RequesterType,
		RequesterID:     util.UUIDToString(a.RequesterID),
		AgentID:         util.UUIDToPtr(a.AgentID),
		Capability:      a.Capability,
		Resource:        a.Resource,
		Reason:          a.Reason,
		MatchedGrantIDs: decodeGrantIDs(a.MatchedGrantIds),
		Context:         decodeJSON(a.Context),
		Status:          a.Status,
		DecidedByID:     util.UUIDToPtr(a.DecidedByID),
		DecidedAt:       timestampPtr(a.DecidedAt),
		DecisionNote:    util.TextToPtr(a.DecisionNote),
		DelegatedToType: util.TextToPtr(a.DelegatedToType),
		DelegatedToID:   util.UUIDToPtr(a.DelegatedToID),
		ExpiresAt:       timestampPtr(a.ExpiresAt),
		CreatedAt:       util.TimestampToString(a.CreatedAt),
		UpdatedAt:       util.TimestampToString(a.UpdatedAt),
	}
}

func listRowToResponse(a cerebrodb.ListCerebroApprovalRequestsRow) approvalResponse {
	return requestToResponse(cerebrodb.CerebroApprovalRequest{
		ID:              a.ID,
		WorkspaceID:     a.WorkspaceID,
		RequesterType:   a.RequesterType,
		RequesterID:     a.RequesterID,
		AgentID:         a.AgentID,
		Capability:      a.Capability,
		Resource:        a.Resource,
		Reason:          a.Reason,
		MatchedGrantIds: a.MatchedGrantIds,
		Context:         a.Context,
		Status:          a.Status,
		DecidedByID:     a.DecidedByID,
		DecidedAt:       a.DecidedAt,
		DecisionNote:    a.DecisionNote,
		DelegatedToType: a.DelegatedToType,
		DelegatedToID:   a.DelegatedToID,
		ExpiresAt:       a.ExpiresAt,
		CreatedAt:       a.CreatedAt,
		UpdatedAt:       a.UpdatedAt,
	})
}

func auditRowToResponse(row cerebrodb.ListCerebroApprovalAuditRow) approvalAuditResponse {
	var actorName *string
	if row.ActorName != "" {
		actorName = &row.ActorName
	}
	return approvalAuditResponse{
		ID:          util.UUIDToString(row.ID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		ApprovalID:  util.UUIDToPtr(row.ApprovalID),
		Action:      row.Action,
		ActorType:   util.TextToPtr(row.ActorType),
		ActorID:     util.UUIDToPtr(row.ActorID),
		ActorName:   actorName,
		Via:         row.Surface,
		Note:        util.TextToPtr(row.Note),
		CreatedAt:   util.TimestampToString(row.CreatedAt),
	}
}

func timestampPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := util.TimestampToString(t)
	return &s
}

func decodeGrantIDs(b []byte) []string {
	out := []string{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeJSON(b []byte) any {
	if len(b) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return map[string]any{}
	}
	return v
}
