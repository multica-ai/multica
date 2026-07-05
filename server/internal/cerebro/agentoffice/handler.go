package agentoffice

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler exposes the agent-context governance REST surface. Read endpoints
// require any workspace member; write endpoints additionally require the actor
// to be the context owner, an approver, or a workspace admin/owner.
type Handler struct {
	Svc *Service
}

// NewHandler builds the HTTP handler from a service.
func NewHandler(svc *Service) *Handler { return &Handler{Svc: svc} }

// --- Response structs ---

// AgentContextVersionResponse is an append-only version snapshot.
type AgentContextVersionResponse struct {
	ID          string          `json:"id"`
	AgentID     string          `json:"agent_id"`
	Version     string          `json:"version"`
	Snapshot    ContextSnapshot `json:"snapshot"`
	Description string          `json:"description"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
}

// AgentChangeRequestResponse is a proposed edit to an agent's context.
type AgentChangeRequestResponse struct {
	ID               string          `json:"id"`
	AgentID          string          `json:"agent_id"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	BaseVersion      string          `json:"base_version"`
	ProposedVersion  string          `json:"proposed_version"`
	ProposedSnapshot ContextSnapshot `json:"proposed_snapshot"`
	Status           string          `json:"status"`
	ProposedBy       string          `json:"proposed_by"`
	ReviewedBy       *string         `json:"reviewed_by"`
	ReviewedAt       *string         `json:"reviewed_at"`
	ReviewComment    string          `json:"review_comment"`
	WorkSessionID    *string         `json:"work_session_id"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

// AgentContextOwnershipResponse echoes the governance columns after a change.
type AgentContextOwnershipResponse struct {
	AgentID            string   `json:"agent_id"`
	ContextOwnerID     *string  `json:"context_owner_id"`
	ContextApproverIDs []string `json:"context_approver_ids"`
	ContextVersion     string   `json:"context_version"`
}

// --- Request structs ---

// UpdateOwnershipRequest is a partial update of the governance columns; an
// omitted field leaves the stored value untouched.
type UpdateOwnershipRequest struct {
	OwnerID     *string   `json:"owner_id"`
	ApproverIDs *[]string `json:"approver_ids"`
}

// CreateChangeRequestRequest proposes a versioned edit. Either supply a full
// ProposedSnapshot, or supply individual override fields that are merged onto the
// agent's current snapshot (the common "just edit the instructions" case).
type CreateChangeRequestRequest struct {
	Title            string           `json:"title"`
	Description      string           `json:"description"`
	ProposedVersion  string           `json:"proposed_version"`
	ProposedSnapshot *ContextSnapshot `json:"proposed_snapshot"`
	// Convenience overrides applied on top of the current snapshot when
	// ProposedSnapshot is absent.
	Instructions   *string          `json:"instructions"`
	Model          *string          `json:"model"`
	ThinkingLevel  *string          `json:"thinking_level"`
	PersonaSandbox *string          `json:"persona_sandbox"`
	SkillIDs       *[]string        `json:"skill_ids"`
	McpConfig      *json.RawMessage `json:"mcp_config"`
	CustomArgs     *json.RawMessage `json:"custom_args"`
	RuntimeConfig  *json.RawMessage `json:"runtime_config"`
	WorkSessionID  *string          `json:"work_session_id"`
}

// ReviewChangeRequestRequest approves or rejects a pending proposal.
type ReviewChangeRequestRequest struct {
	Action  string `json:"action"` // "approve" | "reject"
	Comment string `json:"comment"`
}

// RollbackRequest restores a historical version as a new version.
type RollbackRequest struct {
	Version string `json:"version"`
	Comment string `json:"comment"`
}

// --- HTTP helpers (cerebro-local, mirroring grants/handler.go) ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// loadAgent resolves the {id} path param to an agent scoped to the caller's
// workspace, and returns the acting member. 404 when the agent is not in the
// caller's workspace.
func (h *Handler) loadAgent(w http.ResponseWriter, r *http.Request) (cerebrodb.Agent, db.Member, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return cerebrodb.Agent{}, db.Member{}, false
	}
	agentID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return cerebrodb.Agent{}, db.Member{}, false
	}
	agent, err := h.Svc.Cerebro.GetAgentContextInWorkspace(r.Context(), cerebrodb.GetAgentContextInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: member.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return cerebrodb.Agent{}, db.Member{}, false
	}
	return agent, member, true
}

// canManage reports whether the member may mutate the agent's context: workspace
// owner/admin, the context owner, or a named approver.
func canManage(agent cerebrodb.Agent, member db.Member) bool {
	role := strings.ToLower(member.Role)
	if role == "owner" || role == "admin" {
		return true
	}
	if agent.ContextOwnerID.Valid && uuidEq(agent.ContextOwnerID, member.UserID) {
		return true
	}
	for _, a := range agent.ContextApproverIds {
		if uuidEq(a, member.UserID) {
			return true
		}
	}
	return false
}

func uuidEq(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && util.UUIDToString(a) == util.UUIDToString(b)
}

// --- Conversions ---

func versionToResponse(v cerebrodb.AgentContextVersion) AgentContextVersionResponse {
	return AgentContextVersionResponse{
		ID:          util.UUIDToString(v.ID),
		AgentID:     util.UUIDToString(v.AgentID),
		Version:     v.Version,
		Snapshot:    DecodeSnapshot(v.Snapshot),
		Description: v.Description,
		CreatedBy:   util.UUIDToPtr(v.CreatedBy),
		CreatedAt:   util.TimestampToString(v.CreatedAt),
	}
}

func changeRequestToResponse(c cerebrodb.AgentChangeRequest) AgentChangeRequestResponse {
	return AgentChangeRequestResponse{
		ID:               util.UUIDToString(c.ID),
		AgentID:          util.UUIDToString(c.AgentID),
		Title:            c.Title,
		Description:      c.Description,
		BaseVersion:      c.BaseVersion,
		ProposedVersion:  c.ProposedVersion,
		ProposedSnapshot: DecodeSnapshot(c.ProposedSnapshot),
		Status:           c.Status,
		ProposedBy:       util.UUIDToString(c.ProposedBy),
		ReviewedBy:       util.UUIDToPtr(c.ReviewedBy),
		ReviewedAt:       util.TimestampToPtr(c.ReviewedAt),
		ReviewComment:    c.ReviewComment,
		WorkSessionID:    util.UUIDToPtr(c.WorkSessionID),
		CreatedAt:        util.TimestampToString(c.CreatedAt),
		UpdatedAt:        util.TimestampToString(c.UpdatedAt),
	}
}

// currentSnapshot composes the live snapshot of an agent (row + bound skills).
func (h *Handler) currentSnapshot(r *http.Request, agent cerebrodb.Agent) (ContextSnapshot, error) {
	skillIDs, err := h.Svc.Cerebro.ListAgentSkillIDsForContext(r.Context(), agent.ID)
	if err != nil {
		return ContextSnapshot{}, err
	}
	return ComposeCurrentSnapshot(agent, skillIDs), nil
}

// --- Endpoints ---

// ListVersions returns the append-only version history, newest first.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	rows, err := h.Svc.Cerebro.ListAgentContextVersions(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	resp := make([]AgentContextVersionResponse, len(rows))
	for i, v := range rows {
		resp[i] = versionToResponse(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateOwnership sets the context owner and/or approvers.
func (h *Handler) UpdateOwnership(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	if !canManage(agent, member) {
		writeError(w, http.StatusForbidden, "not allowed to manage this agent's context")
		return
	}
	var req UpdateOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Start from the current values so an omitted field is preserved.
	owner := agent.ContextOwnerID
	approvers := agent.ContextApproverIds
	if req.OwnerID != nil {
		if *req.OwnerID == "" {
			owner = pgtype.UUID{}
		} else {
			parsed, err := util.ParseUUID(*req.OwnerID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid owner_id")
				return
			}
			owner = parsed
		}
	}
	if req.ApproverIDs != nil {
		next := make([]pgtype.UUID, 0, len(*req.ApproverIDs))
		for _, raw := range *req.ApproverIDs {
			parsed, err := util.ParseUUID(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid approver id: "+raw)
				return
			}
			next = append(next, parsed)
		}
		approvers = next
	}

	updated, err := h.Svc.Cerebro.UpdateAgentContextOwnership(r.Context(), cerebrodb.UpdateAgentContextOwnershipParams{
		ID:                 agent.ID,
		ContextOwnerID:     owner,
		ContextApproverIds: approvers,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update ownership: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ownershipResponse(updated))
}

func ownershipResponse(a cerebrodb.Agent) AgentContextOwnershipResponse {
	approvers := make([]string, 0, len(a.ContextApproverIds))
	for _, id := range a.ContextApproverIds {
		approvers = append(approvers, util.UUIDToString(id))
	}
	return AgentContextOwnershipResponse{
		AgentID:            util.UUIDToString(a.ID),
		ContextOwnerID:     util.UUIDToPtr(a.ContextOwnerID),
		ContextApproverIDs: approvers,
		ContextVersion:     a.ContextVersion,
	}
}

// ListChangeRequests returns every change request for one agent, newest first.
func (h *Handler) ListChangeRequests(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	rows, err := h.Svc.Cerebro.ListAgentChangeRequestsByAgent(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list change requests")
		return
	}
	resp := make([]AgentChangeRequestResponse, len(rows))
	for i, c := range rows {
		resp[i] = changeRequestToResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListPendingChangeRequests returns all pending proposals across the workspace.
func (h *Handler) ListPendingChangeRequests(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	rows, err := h.Svc.Cerebro.ListPendingAgentChangeRequestsByWorkspace(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending change requests")
		return
	}
	resp := make([]AgentChangeRequestResponse, len(rows))
	for i, c := range rows {
		resp[i] = changeRequestToResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

// derefRawOrNil returns the pointed-to raw JSON, or nil when the pointer is nil
// (an explicit JSON null) so the field is cleared rather than set to "null".
func derefRawOrNil(p *json.RawMessage) json.RawMessage {
	if p == nil {
		return nil
	}
	return json.RawMessage(*p)
}

// CreateChangeRequest proposes a versioned edit to an agent's context.
func (h *Handler) CreateChangeRequest(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req CreateChangeRequestRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Decode the raw keys too so we can tell an explicit `mcp_config: null`
	// (clear it) apart from an omitted field (leave it unchanged) — a pointer
	// alone cannot, because JSON null and absence both decode to a nil pointer.
	var rawKeys map[string]json.RawMessage
	_ = json.Unmarshal(bodyBytes, &rawKeys)
	keyPresent := func(k string) bool { _, ok := rawKeys[k]; return ok }
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if !ValidSemver(req.ProposedVersion) {
		writeError(w, http.StatusBadRequest, "proposed_version must be semver X.Y.Z")
		return
	}
	if !SemverGT(req.ProposedVersion, agent.ContextVersion) {
		writeError(w, http.StatusBadRequest, "proposed_version must be greater than current "+agent.ContextVersion)
		return
	}

	// Build the proposed snapshot: full snapshot if supplied, else current +
	// overrides.
	var snap ContextSnapshot
	if req.ProposedSnapshot != nil {
		snap = *req.ProposedSnapshot
	} else {
		cur, err := h.currentSnapshot(r, agent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read current context")
			return
		}
		snap = cur
		if req.Instructions != nil {
			snap.Instructions = *req.Instructions
		}
		if req.Model != nil {
			snap.Model = *req.Model
		}
		if req.ThinkingLevel != nil {
			snap.ThinkingLevel = *req.ThinkingLevel
		}
		if req.PersonaSandbox != nil {
			snap.PersonaSandbox = *req.PersonaSandbox
		}
		if req.SkillIDs != nil {
			snap.SkillIDs = *req.SkillIDs
		}
		// For the free-form JSON fields, presence of the key (even as null) means
		// "set this field"; a null value clears it. Absence leaves it unchanged.
		if keyPresent("mcp_config") {
			snap.McpConfig = derefRawOrNil(req.McpConfig)
		}
		if keyPresent("custom_args") {
			snap.CustomArgs = derefRawOrNil(req.CustomArgs)
		}
		if keyPresent("runtime_config") {
			snap.RuntimeConfig = derefRawOrNil(req.RuntimeConfig)
		}
	}

	var sessionUUID pgtype.UUID
	if req.WorkSessionID != nil && *req.WorkSessionID != "" {
		// Soft-validate: an unresolvable id (e.g. a task id forwarded by the
		// CLI) stores null rather than blocking the propose flow.
		if parsed, err := util.ParseUUID(*req.WorkSessionID); err == nil {
			sessionUUID = parsed
		}
	}

	cr, err := h.Svc.Cerebro.CreateAgentChangeRequest(r.Context(), cerebrodb.CreateAgentChangeRequestParams{
		AgentID:          agent.ID,
		Title:            req.Title,
		Description:      req.Description,
		BaseVersion:      agent.ContextVersion,
		ProposedVersion:  req.ProposedVersion,
		ProposedSnapshot: EncodeSnapshot(snap),
		ProposedBy:       member.UserID,
		WorkSessionID:    sessionUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create change request: "+err.Error())
		return
	}
	// Notify the reviewers (context owner + approvers) that a proposal is
	// waiting, mirroring the skill change-request flow. Best-effort: a failed
	// notification must not fail the request.
	h.notifyChangeRequestCreated(r.Context(), agent, cr, member.UserID)
	writeJSON(w, http.StatusCreated, changeRequestToResponse(cr))
}

// ReviewChangeRequest approves (merge) or rejects a pending proposal.
func (h *Handler) ReviewChangeRequest(w http.ResponseWriter, r *http.Request) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	crID, err := util.ParseUUID(chi.URLParam(r, "crId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid change request id")
		return
	}
	cr, err := h.Svc.Cerebro.GetAgentChangeRequest(r.Context(), crID)
	if err != nil {
		writeError(w, http.StatusNotFound, "change request not found")
		return
	}
	agent, err := h.Svc.Cerebro.GetAgentContextInWorkspace(r.Context(), cerebrodb.GetAgentContextInWorkspaceParams{
		ID:          cr.AgentID,
		WorkspaceID: member.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if !canManage(agent, member) {
		writeError(w, http.StatusForbidden, "not allowed to review this agent's context")
		return
	}
	var req ReviewChangeRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if cr.Status != "pending" {
		writeError(w, http.StatusConflict, "change request is not pending review")
		return
	}

	switch req.Action {
	case "approve":
		updated, err := h.approveAndMerge(r, agent, cr, member.UserID, req.Comment)
		if err != nil {
			writeError(w, statusForMergeError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, changeRequestToResponse(updated))
	case "reject":
		updated, err := h.Svc.Cerebro.ReviewAgentChangeRequest(r.Context(), cerebrodb.ReviewAgentChangeRequestParams{
			ID:            cr.ID,
			Status:        "rejected",
			ReviewedBy:    member.UserID,
			ReviewComment: req.Comment,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, changeRequestToResponse(updated))
	default:
		writeError(w, http.StatusBadRequest, "action must be 'approve' or 'reject'")
	}
}

// Diff renders a unified diff between two versions of an agent's context. `from`
// is a required version; `to` defaults to the live snapshot when omitted.
func (h *Handler) Diff(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		writeError(w, http.StatusBadRequest, "from version is required")
		return
	}
	baseV, err := h.Svc.Cerebro.GetAgentContextVersion(r.Context(), cerebrodb.GetAgentContextVersionParams{
		AgentID: agent.ID,
		Version: from,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "from version not found")
		return
	}
	baseSnap := DecodeSnapshot(baseV.Snapshot)

	var targetSnap ContextSnapshot
	if to == "" {
		cur, err := h.currentSnapshot(r, agent)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read current context")
			return
		}
		targetSnap = cur
		to = agent.ContextVersion + " (live)"
	} else {
		toV, err := h.Svc.Cerebro.GetAgentContextVersion(r.Context(), cerebrodb.GetAgentContextVersionParams{
			AgentID: agent.ID,
			Version: to,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "to version not found")
			return
		}
		targetSnap = DecodeSnapshot(toV.Snapshot)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": util.UUIDToString(agent.ID),
		"from":     from,
		"to":       to,
		"diff":     DiffSnapshots(baseSnap, targetSnap),
	})
}

// Rollback restores a historical version's snapshot as a new version. It is a
// privileged manage action that applies directly (no review round-trip).
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.loadAgent(w, r)
	if !ok {
		return
	}
	if !canManage(agent, member) {
		writeError(w, http.StatusForbidden, "not allowed to roll back this agent's context")
		return
	}
	var req RollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Version == "" {
		writeError(w, http.StatusBadRequest, "version is required")
		return
	}
	target, err := h.Svc.Cerebro.GetAgentContextVersion(r.Context(), cerebrodb.GetAgentContextVersionParams{
		AgentID: agent.ID,
		Version: req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "target version not found")
		return
	}
	snap := DecodeSnapshot(target.Snapshot)
	newVersion := BumpPatch(agent.ContextVersion)
	desc := req.Comment
	if desc == "" {
		desc = "Rollback to " + req.Version
	}

	tx, err := h.Svc.Tx.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Svc.Cerebro.WithTx(tx)

	if _, err := h.Svc.ApplySnapshotTx(r.Context(), qtx, agent.ID, snap, newVersion); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	version, err := qtx.CreateAgentContextVersion(r.Context(), cerebrodb.CreateAgentContextVersionParams{
		AgentID:     agent.ID,
		Version:     newVersion,
		Snapshot:    EncodeSnapshot(snap),
		Description: desc,
		CreatedBy:   member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snapshot rollback version: "+err.Error())
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versionToResponse(version))
}
