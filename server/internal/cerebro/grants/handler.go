package grants

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler wires the grants service to chi routes.
// Read operations (GET) require any workspace member.
// Write operations (POST/PATCH/DELETE) require admin/owner — enforced via
// RequireWorkspaceRoleFromURL on the router's admin group.
type Handler struct {
	Svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{Svc: svc} }

// --- response types ---------------------------------------------------------

type grantResponse struct {
	ID                    string  `json:"id"`
	WorkspaceID           string  `json:"workspace_id"`
	SubjectType           string  `json:"subject_type"`
	SubjectID             *string `json:"subject_id"`
	ResourcePattern       string  `json:"resource_pattern"`
	Capability            string  `json:"capability"`
	ClassificationCeiling *string `json:"classification_ceiling"`
	TimeWindowStart       *string `json:"time_window_start"`
	TimeWindowEnd         *string `json:"time_window_end"`
	ApprovalRequired      bool    `json:"approval_required"`
	Status                string  `json:"status"`
	GrantedByType         *string `json:"granted_by_type"`
	GrantedByID           *string `json:"granted_by_id"`
	GrantedAt             string  `json:"granted_at"`
	RevokedByID           *string `json:"revoked_by_id"`
	RevokedAt             *string `json:"revoked_at"`
	UpdatedAt             string  `json:"updated_at"`
}

type grantAuditResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	GrantID     *string `json:"grant_id"`
	Action      string  `json:"action"`
	ActorType   *string `json:"actor_type"`
	ActorID     *string `json:"actor_id"`
	ActorName   *string `json:"actor_name"`
	Via         string  `json:"via"`
	Summary     *string `json:"summary"`
	Before      any     `json:"before"`
	After       any     `json:"after"`
	CreatedAt   string  `json:"created_at"`
}

// --- request types ----------------------------------------------------------

type createGrantRequest struct {
	SubjectType           string  `json:"subject_type"`
	SubjectID             *string `json:"subject_id"`
	ResourcePattern       string  `json:"resource_pattern"`
	Capability            string  `json:"capability"`
	ClassificationCeiling *string `json:"classification_ceiling"`
	TimeWindowStart       *string `json:"time_window_start"`
	TimeWindowEnd         *string `json:"time_window_end"`
	ApprovalRequired      bool    `json:"approval_required"`
}

type updateGrantRequest struct {
	ResourcePattern       *string `json:"resource_pattern"`
	Capability            *string `json:"capability"`
	ClassificationCeiling *string `json:"classification_ceiling"`
	TimeWindowStart       *string `json:"time_window_start"`
	TimeWindowEnd         *string `json:"time_window_end"`
	ApprovalRequired      *bool   `json:"approval_required"`
	Status                *string `json:"status"`
}

// --- handlers ---------------------------------------------------------------

// List — GET /api/workspaces/{id}/grants
// Query params: subject_type, subject_id, status
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	_ = member

	f := ListFilter{
		SubjectType: r.URL.Query().Get("subject_type"),
		Status:      r.URL.Query().Get("status"),
	}
	if raw := r.URL.Query().Get("subject_id"); raw != "" {
		sid, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subject_id")
			return
		}
		f.SubjectID = sid
	}

	grants, err := h.Svc.List(r.Context(), workspaceID, f)
	if err != nil {
		h.serverError(w, r, "list grants", err)
		return
	}
	resp := make([]grantResponse, len(grants))
	for i, g := range grants {
		resp[i] = toResponse(g)
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": resp})
}

// Audit — GET /api/workspaces/{id}/grants/audit
// Query params: subject_id, grant_id, since, limit, offset
func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()
	filter := cerebrodb.ListCerebroGrantAuditParams{
		WorkspaceID: workspaceID,
		Limit:       50,
		Offset:      0,
	}
	if raw := q.Get("subject_id"); raw != "" {
		subjectID, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subject_id")
			return
		}
		filter.Column2 = subjectID
	}
	if raw := q.Get("grant_id"); raw != "" {
		grantID, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid grant_id")
			return
		}
		filter.Column3 = grantID
	}
	if raw := q.Get("since"); raw != "" {
		filter.Column4 = optTimestamp(&raw)
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

	rows, err := h.Svc.Cerebro.ListCerebroGrantAudit(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, "list grant audit", err)
		return
	}

	items := make([]grantAuditResponse, len(rows))
	total := int32(0)
	for i, row := range rows {
		if row.Total > total {
			total = row.Total
		}
		items[i] = toAuditResponse(row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

// Get — GET /api/workspaces/{id}/grants/{grantId}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	grantID, err := util.ParseUUID(chi.URLParam(r, "grantId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid grant id")
		return
	}
	g, err := h.Svc.Get(r.Context(), grantID, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "grant not found")
			return
		}
		h.serverError(w, r, "get grant", err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(g))
}

// Create — POST /api/workspaces/{id}/grants
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.Svc.Validate(req.SubjectType, req.ResourcePattern, req.Capability); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.SubjectType != SubjectTypeWorkspaceDefault && req.SubjectID == nil {
		writeError(w, http.StatusBadRequest, "subject_id required for subject_type "+req.SubjectType)
		return
	}

	var subjectID pgtype.UUID
	if req.SubjectID != nil {
		var err error
		subjectID, err = util.ParseUUID(*req.SubjectID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subject_id")
			return
		}
		if err := h.Svc.ValidateSubjectExists(r.Context(), req.SubjectType, subjectID, workspaceID); err != nil {
			if errors.Is(err, ErrSubjectNotFound) {
				writeError(w, http.StatusUnprocessableEntity, "subject not found in workspace")
				return
			}
			h.serverError(w, r, "validate subject", err)
			return
		}
	}

	surface := surfaceFromHeader(r.Header.Get("X-Cerebro-Surface"))

	g, err := h.Svc.Create(r.Context(), CreateParams{
		WorkspaceID:           workspaceID,
		SubjectType:           req.SubjectType,
		SubjectID:             subjectID,
		ResourcePattern:       req.ResourcePattern,
		Capability:            req.Capability,
		ClassificationCeiling: optText(req.ClassificationCeiling),
		TimeWindowStart:       optTimestamp(req.TimeWindowStart),
		TimeWindowEnd:         optTimestamp(req.TimeWindowEnd),
		ApprovalRequired:      req.ApprovalRequired,
		GrantedByType:         pgtype.Text{String: "member", Valid: true},
		GrantedByID:           member.UserID,
		Surface:               surface,
	})
	if err != nil {
		h.serverError(w, r, "create grant", err)
		return
	}
	writeJSON(w, http.StatusCreated, toResponse(g))
}

// Update — PATCH /api/workspaces/{id}/grants/{grantId}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	grantID, err := util.ParseUUID(chi.URLParam(r, "grantId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid grant id")
		return
	}

	var req updateGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ResourcePattern == nil && req.Capability == nil && req.ClassificationCeiling == nil &&
		req.TimeWindowStart == nil && req.TimeWindowEnd == nil && req.ApprovalRequired == nil && req.Status == nil {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}

	if req.Status != nil {
		if *req.Status != "revoked" {
			writeError(w, http.StatusBadRequest, "unsupported status")
			return
		}
		g, err := h.Svc.Revoke(r.Context(), grantID, workspaceID, member.UserID, "member", surfaceFromHeader(r.Header.Get("X-Cerebro-Surface")))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "grant not found")
				return
			}
			h.serverError(w, r, "revoke grant", err)
			return
		}
		writeJSON(w, http.StatusOK, toResponse(g))
		return
	}

	if req.ResourcePattern != nil {
		if *req.ResourcePattern == "" || !resourcePatternPattern.MatchString(*req.ResourcePattern) {
			writeError(w, http.StatusBadRequest, ErrInvalidResourcePattern.Error())
			return
		}
	}
	if req.Capability != nil {
		if *req.Capability == "" || !capabilityPattern.MatchString(*req.Capability) {
			writeError(w, http.StatusBadRequest, ErrInvalidCapability.Error())
			return
		}
	}

	surface := surfaceFromHeader(r.Header.Get("X-Cerebro-Surface"))

	p := UpdateParams{
		ID:                    grantID,
		WorkspaceID:           workspaceID,
		ClassificationCeiling: optText(req.ClassificationCeiling),
		TimeWindowStart:       optTimestamp(req.TimeWindowStart),
		TimeWindowEnd:         optTimestamp(req.TimeWindowEnd),
		ActorType:             pgtype.Text{String: "member", Valid: true},
		ActorID:               member.UserID,
		Surface:               surface,
	}
	if req.ResourcePattern != nil {
		p.ResourcePattern = pgtype.Text{String: *req.ResourcePattern, Valid: true}
	}
	if req.Capability != nil {
		p.Capability = pgtype.Text{String: *req.Capability, Valid: true}
	}
	if req.ApprovalRequired != nil {
		p.ApprovalRequired = pgtype.Bool{Bool: *req.ApprovalRequired, Valid: true}
	}

	g, err := h.Svc.Update(r.Context(), p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "grant not found")
			return
		}
		h.serverError(w, r, "update grant", err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(g))
}

// Delete — DELETE /api/workspaces/{id}/grants/{grantId}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	grantID, err := util.ParseUUID(chi.URLParam(r, "grantId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid grant id")
		return
	}

	surface := surfaceFromHeader(r.Header.Get("X-Cerebro-Surface"))

	if err := h.Svc.Delete(r.Context(), grantID, workspaceID, member.UserID, "member", surface); err != nil {
		h.serverError(w, r, "delete grant", err)
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
	slog.Error("grants request failed", append(logger.RequestAttrs(r), "op", op, "error", err)...)
	writeError(w, http.StatusInternalServerError, op+" failed")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func toResponse(g cerebrodb.CerebroWorkspaceGrant) grantResponse {
	var revokedAt *string
	if g.RevokedAt.Valid {
		s := util.TimestampToString(g.RevokedAt)
		revokedAt = &s
	}
	var twStart, twEnd *string
	if g.TimeWindowStart.Valid {
		s := util.TimestampToString(g.TimeWindowStart)
		twStart = &s
	}
	if g.TimeWindowEnd.Valid {
		s := util.TimestampToString(g.TimeWindowEnd)
		twEnd = &s
	}
	return grantResponse{
		ID:                    util.UUIDToString(g.ID),
		WorkspaceID:           util.UUIDToString(g.WorkspaceID),
		SubjectType:           g.SubjectType,
		SubjectID:             util.UUIDToPtr(g.SubjectID),
		ResourcePattern:       g.ResourcePattern,
		Capability:            g.Capability,
		ClassificationCeiling: util.TextToPtr(g.ClassificationCeiling),
		TimeWindowStart:       twStart,
		TimeWindowEnd:         twEnd,
		ApprovalRequired:      g.ApprovalRequired,
		Status:                g.Status,
		GrantedByType:         util.TextToPtr(g.GrantedByType),
		GrantedByID:           util.UUIDToPtr(g.GrantedByID),
		GrantedAt:             util.TimestampToString(g.GrantedAt),
		RevokedByID:           util.UUIDToPtr(g.RevokedByID),
		RevokedAt:             revokedAt,
		UpdatedAt:             util.TimestampToString(g.UpdatedAt),
	}
}

func toAuditResponse(row cerebrodb.ListCerebroGrantAuditRow) grantAuditResponse {
	var diff struct {
		Before any `json:"before"`
		After  any `json:"after"`
	}
	if len(row.Diff) > 0 {
		_ = json.Unmarshal(row.Diff, &diff)
	}
	var actorName *string
	if row.ActorName != "" {
		actorName = &row.ActorName
	}
	summary := grantAuditSummary(row.Action)
	return grantAuditResponse{
		ID:          util.UUIDToString(row.ID),
		WorkspaceID: util.UUIDToString(row.WorkspaceID),
		GrantID:     util.UUIDToPtr(row.GrantID),
		Action:      row.Action,
		ActorType:   util.TextToPtr(row.ActorType),
		ActorID:     util.UUIDToPtr(row.ActorID),
		ActorName:   actorName,
		Via:         row.Surface,
		Summary:     &summary,
		Before:      diff.Before,
		After:       diff.After,
		CreatedAt:   util.TimestampToString(row.CreatedAt),
	}
}

func grantAuditSummary(action string) string {
	switch action {
	case "created":
		return "Grant oprettet"
	case "updated":
		return "Grant opdateret"
	case "revoked":
		return "Grant tilbagekaldt"
	case "deleted":
		return "Grant slettet"
	default:
		return "Grant ændret"
	}
}

// optText converts a *string to pgtype.Text.
func optText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// optTimestamp parses an RFC3339 string pointer into pgtype.Timestamptz.
// Invalid or missing strings return an invalid (null) Timestamptz.
func optTimestamp(s *string) pgtype.Timestamptz {
	if s == nil || *s == "" {
		return pgtype.Timestamptz{}
	}
	var ts pgtype.Timestamptz
	if err := ts.Scan(*s); err != nil {
		return pgtype.Timestamptz{}
	}
	return ts
}
