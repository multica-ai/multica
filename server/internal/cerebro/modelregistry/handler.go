package modelregistry

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler exposes the model-registry governance REST surface. Read endpoints
// require any workspace member; write endpoints additionally require the
// actor to be the registry owner, an approver, or a workspace admin/owner.
// The registry is a deployment-wide singleton, so no entity id appears in the
// routes.
type Handler struct {
	Svc *Service
}

// NewHandler builds the HTTP handler from a service.
func NewHandler(svc *Service) *Handler { return &Handler{Svc: svc} }

// --- Response structs ---

// RegistryResponse is the live registry document plus its governance columns.
type RegistryResponse struct {
	ID             string   `json:"id"`
	OwnerID        *string  `json:"owner_id"`
	ApproverIDs    []string `json:"approver_ids"`
	CurrentVersion string   `json:"current_version"`
	Snapshot       Snapshot `json:"snapshot"`
	UpdatedAt      string   `json:"updated_at"`
}

// VersionResponse is an append-only version snapshot.
type VersionResponse struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	Snapshot    Snapshot `json:"snapshot"`
	Description string   `json:"description"`
	CreatedBy   *string  `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
}

// ChangeRequestResponse is a proposed edit to the registry.
type ChangeRequestResponse struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	BaseVersion      string   `json:"base_version"`
	ProposedVersion  string   `json:"proposed_version"`
	ProposedSnapshot Snapshot `json:"proposed_snapshot"`
	Diff             string   `json:"diff"`
	Status           string   `json:"status"`
	ProposedBy       string   `json:"proposed_by"`
	ReviewedBy       *string  `json:"reviewed_by"`
	ReviewedAt       *string  `json:"reviewed_at"`
	ReviewComment    string   `json:"review_comment"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

// --- Request structs ---

// UpdateOwnershipRequest is a partial update of the governance columns; an
// omitted field leaves the stored value untouched.
type UpdateOwnershipRequest struct {
	OwnerID     *string   `json:"owner_id"`
	ApproverIDs *[]string `json:"approver_ids"`
}

// CreateChangeRequestRequest proposes a versioned edit. Either supply a full
// ProposedSnapshot, or supply the convenience overrides which are merged onto
// the current snapshot (the common "add/update/remove one model" case).
type CreateChangeRequestRequest struct {
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	ProposedVersion  string    `json:"proposed_version"`
	ProposedSnapshot *Snapshot `json:"proposed_snapshot"`
	// Convenience overrides applied on top of the current snapshot when
	// ProposedSnapshot is absent.
	SetModels     map[string]ModelEntry `json:"set_models"`
	RemoveModels  []string              `json:"remove_models"`
	FallbackModel *string               `json:"fallback_model"`
	WorkSessionID *string               `json:"work_session_id"`
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

// --- HTTP helpers (cerebro-local, mirroring agentoffice/handler.go) ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// loadRegistry loads the singleton registry row and the acting member.
func (h *Handler) loadRegistry(w http.ResponseWriter, r *http.Request) (cerebrodb.ModelRegistry, db.Member, bool) {
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return cerebrodb.ModelRegistry{}, db.Member{}, false
	}
	reg, err := h.Svc.Cerebro.GetModelRegistry(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "model registry not found")
		return cerebrodb.ModelRegistry{}, db.Member{}, false
	}
	return reg, member, true
}

// canManage reports whether the member may mutate the registry: workspace
// owner/admin, the registry owner, or a named approver. (The registry is
// deployment-wide; workspace roles are the trust boundary of this fork's
// single-workspace deployments.)
func canManage(reg cerebrodb.ModelRegistry, member db.Member) bool {
	role := strings.ToLower(member.Role)
	if role == "owner" || role == "admin" {
		return true
	}
	if reg.OwnerID.Valid && uuidEq(reg.OwnerID, member.UserID) {
		return true
	}
	for _, a := range reg.ApproverIds {
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

func registryToResponse(reg cerebrodb.ModelRegistry) RegistryResponse {
	approvers := make([]string, 0, len(reg.ApproverIds))
	for _, id := range reg.ApproverIds {
		approvers = append(approvers, util.UUIDToString(id))
	}
	return RegistryResponse{
		ID:             util.UUIDToString(reg.ID),
		OwnerID:        util.UUIDToPtr(reg.OwnerID),
		ApproverIDs:    approvers,
		CurrentVersion: reg.CurrentVersion,
		Snapshot:       DecodeSnapshot(reg.Snapshot),
		UpdatedAt:      util.TimestampToString(reg.UpdatedAt),
	}
}

func versionToResponse(v cerebrodb.ModelRegistryVersion) VersionResponse {
	return VersionResponse{
		ID:          util.UUIDToString(v.ID),
		Version:     v.Version,
		Snapshot:    DecodeSnapshot(v.Snapshot),
		Description: v.Description,
		CreatedBy:   util.UUIDToPtr(v.CreatedBy),
		CreatedAt:   util.TimestampToString(v.CreatedAt),
	}
}

func (h *Handler) changeRequestToResponse(c cerebrodb.ModelRegistryChangeRequest, base Snapshot) ChangeRequestResponse {
	proposed := DecodeSnapshot(c.ProposedSnapshot)
	return ChangeRequestResponse{
		ID:               util.UUIDToString(c.ID),
		Title:            c.Title,
		Description:      c.Description,
		BaseVersion:      c.BaseVersion,
		ProposedVersion:  c.ProposedVersion,
		ProposedSnapshot: proposed,
		Diff:             DiffSnapshots(base, proposed),
		Status:           c.Status,
		ProposedBy:       util.UUIDToString(c.ProposedBy),
		ReviewedBy:       util.UUIDToPtr(c.ReviewedBy),
		ReviewedAt:       util.TimestampToPtr(c.ReviewedAt),
		ReviewComment:    c.ReviewComment,
		CreatedAt:        util.TimestampToString(c.CreatedAt),
		UpdatedAt:        util.TimestampToString(c.UpdatedAt),
	}
}

// --- Endpoints ---

// Get returns the live registry document.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	reg, _, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, registryToResponse(reg))
}

// ListVersions returns the append-only version history, newest first.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	reg, _, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	rows, err := h.Svc.Cerebro.ListModelRegistryVersions(r.Context(), reg.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	resp := make([]VersionResponse, len(rows))
	for i, v := range rows {
		resp[i] = versionToResponse(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateOwnership sets the registry owner and/or approvers.
func (h *Handler) UpdateOwnership(w http.ResponseWriter, r *http.Request) {
	reg, member, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	if !canManage(reg, member) {
		writeError(w, http.StatusForbidden, "not allowed to manage the model registry")
		return
	}
	var req UpdateOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	owner := reg.OwnerID
	approvers := reg.ApproverIds
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

	updated, err := h.Svc.Cerebro.UpdateModelRegistryOwnership(r.Context(), cerebrodb.UpdateModelRegistryOwnershipParams{
		OwnerID:     owner,
		ApproverIds: approvers,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update ownership: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, registryToResponse(updated))
}

// ListChangeRequests returns change requests, newest first. ?status=pending
// narrows to pending ones.
func (h *Handler) ListChangeRequests(w http.ResponseWriter, r *http.Request) {
	reg, _, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	var (
		rows []cerebrodb.ModelRegistryChangeRequest
		err  error
	)
	if r.URL.Query().Get("status") == "pending" {
		rows, err = h.Svc.Cerebro.ListPendingModelRegistryChangeRequests(r.Context(), reg.ID)
	} else {
		rows, err = h.Svc.Cerebro.ListModelRegistryChangeRequests(r.Context(), reg.ID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list change requests")
		return
	}
	base := DecodeSnapshot(reg.Snapshot)
	resp := make([]ChangeRequestResponse, len(rows))
	for i, c := range rows {
		resp[i] = h.changeRequestToResponse(c, base)
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateChangeRequest proposes a versioned edit to the registry.
func (h *Handler) CreateChangeRequest(w http.ResponseWriter, r *http.Request) {
	reg, member, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	var req CreateChangeRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if !versioning.ValidSemver(req.ProposedVersion) {
		writeError(w, http.StatusBadRequest, "proposed_version must be semver X.Y.Z")
		return
	}
	if !versioning.SemverGT(req.ProposedVersion, reg.CurrentVersion) {
		writeError(w, http.StatusBadRequest, "proposed_version must be greater than current "+reg.CurrentVersion)
		return
	}

	// Build the proposed snapshot: full snapshot if supplied, else current +
	// convenience overrides.
	var snap Snapshot
	if req.ProposedSnapshot != nil {
		snap = *req.ProposedSnapshot
	} else {
		snap = DecodeSnapshot(reg.Snapshot)
		models := make(map[string]ModelEntry, len(snap.Models))
		for id, e := range snap.Models {
			models[id] = e
		}
		for id, e := range req.SetModels {
			models[strings.ToLower(strings.TrimSpace(id))] = e
		}
		for _, id := range req.RemoveModels {
			delete(models, strings.ToLower(strings.TrimSpace(id)))
		}
		snap.Models = models
		if req.FallbackModel != nil {
			snap.FallbackModel = *req.FallbackModel
		}
	}
	if err := ValidateSnapshot(snap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid proposed snapshot: "+err.Error())
		return
	}

	var sessionUUID pgtype.UUID
	if req.WorkSessionID != nil && *req.WorkSessionID != "" {
		// Soft-validate: an unresolvable id stores null rather than blocking
		// the propose flow.
		if parsed, err := util.ParseUUID(*req.WorkSessionID); err == nil {
			sessionUUID = parsed
		}
	}

	cr, err := h.Svc.Cerebro.CreateModelRegistryChangeRequest(r.Context(), cerebrodb.CreateModelRegistryChangeRequestParams{
		RegistryID:       reg.ID,
		Title:            req.Title,
		Description:      req.Description,
		BaseVersion:      reg.CurrentVersion,
		ProposedVersion:  req.ProposedVersion,
		ProposedSnapshot: EncodeSnapshot(snap),
		ProposedBy:       member.UserID,
		WorkSessionID:    sessionUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create change request: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, h.changeRequestToResponse(cr, DecodeSnapshot(reg.Snapshot)))
}

// ReviewChangeRequest approves (merge) or rejects a pending proposal.
func (h *Handler) ReviewChangeRequest(w http.ResponseWriter, r *http.Request) {
	reg, member, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	if !canManage(reg, member) {
		writeError(w, http.StatusForbidden, "not allowed to review model registry changes")
		return
	}
	crID, err := util.ParseUUID(chi.URLParam(r, "crId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid change request id")
		return
	}
	cr, err := h.Svc.Cerebro.GetModelRegistryChangeRequest(r.Context(), crID)
	if err != nil {
		writeError(w, http.StatusNotFound, "change request not found")
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
		updated, err := h.approveAndMerge(r, cr, member.UserID, req.Comment)
		if err != nil {
			writeError(w, versioning.StatusForMergeError(err), err.Error())
			return
		}
		writeJSON(w, http.StatusOK, h.changeRequestToResponse(updated, DecodeSnapshot(reg.Snapshot)))
	case "reject":
		updated, err := h.Svc.Cerebro.ReviewModelRegistryChangeRequest(r.Context(), cerebrodb.ReviewModelRegistryChangeRequestParams{
			ID:            cr.ID,
			Status:        "rejected",
			ReviewedBy:    member.UserID,
			ReviewComment: req.Comment,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, h.changeRequestToResponse(updated, DecodeSnapshot(reg.Snapshot)))
	default:
		writeError(w, http.StatusBadRequest, "action must be 'approve' or 'reject'")
	}
}

// Diff renders a unified diff between two versions. `from` is a required
// version; `to` defaults to the live snapshot when omitted.
func (h *Handler) Diff(w http.ResponseWriter, r *http.Request) {
	reg, _, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		writeError(w, http.StatusBadRequest, "from version is required")
		return
	}
	baseV, err := h.Svc.Cerebro.GetModelRegistryVersion(r.Context(), cerebrodb.GetModelRegistryVersionParams{
		RegistryID: reg.ID,
		Version:    from,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "from version not found")
		return
	}
	baseSnap := DecodeSnapshot(baseV.Snapshot)

	var targetSnap Snapshot
	if to == "" {
		targetSnap = DecodeSnapshot(reg.Snapshot)
		to = reg.CurrentVersion + " (live)"
	} else {
		toV, err := h.Svc.Cerebro.GetModelRegistryVersion(r.Context(), cerebrodb.GetModelRegistryVersionParams{
			RegistryID: reg.ID,
			Version:    to,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "to version not found")
			return
		}
		targetSnap = DecodeSnapshot(toV.Snapshot)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from": from,
		"to":   to,
		"diff": DiffSnapshots(baseSnap, targetSnap),
	})
}

// Rollback restores a historical version's snapshot as a new version. It is a
// privileged manage action that applies directly (no review round-trip).
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	reg, member, ok := h.loadRegistry(w, r)
	if !ok {
		return
	}
	if !canManage(reg, member) {
		writeError(w, http.StatusForbidden, "not allowed to roll back the model registry")
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
	target, err := h.Svc.Cerebro.GetModelRegistryVersion(r.Context(), cerebrodb.GetModelRegistryVersionParams{
		RegistryID: reg.ID,
		Version:    req.Version,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "target version not found")
		return
	}
	snap := DecodeSnapshot(target.Snapshot)
	if err := ValidateSnapshot(snap); err != nil {
		writeError(w, http.StatusConflict, "target version snapshot is not valid: "+err.Error())
		return
	}
	newVersion := versioning.BumpPatch(reg.CurrentVersion)
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

	if _, err := h.Svc.ApplySnapshotTx(r.Context(), qtx, snap, newVersion); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	version, err := qtx.CreateModelRegistryVersion(r.Context(), cerebrodb.CreateModelRegistryVersionParams{
		RegistryID:  reg.ID,
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
	// Make the restored table live for this process immediately.
	Publish(snap, newVersion)
	writeJSON(w, http.StatusOK, versionToResponse(version))
}
