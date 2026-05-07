package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fork-specific endpoints implementing the ownership / approval / versioning /
// fork model from JEH-216. The shape matches the existing skill handler so the
// frontend can keep using a single SkillResponse plus per-feature sub-resources.

// --- Response structs ---

type SkillVersionResponse struct {
	ID          string                 `json:"id"`
	SkillID     string                 `json:"skill_id"`
	Version     string                 `json:"version"`
	Content     string                 `json:"content"`
	Files       []skillVersionFile     `json:"files"`
	Description string                 `json:"description"`
	CreatedBy   *string                `json:"created_by"`
	CreatedAt   string                 `json:"created_at"`
}

type skillVersionFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SkillChangeRequestResponse struct {
	ID              string             `json:"id"`
	SkillID         string             `json:"skill_id"`
	Title           string             `json:"title"`
	Description     string             `json:"description"`
	BaseVersion     string             `json:"base_version"`
	ProposedVersion string             `json:"proposed_version"`
	ProposedContent string             `json:"proposed_content"`
	ProposedFiles   []skillVersionFile `json:"proposed_files"`
	Status          string             `json:"status"`
	ProposedBy      string             `json:"proposed_by"`
	ReviewedBy      *string            `json:"reviewed_by"`
	ReviewedAt      *string            `json:"reviewed_at"`
	ReviewComment   string             `json:"review_comment"`
	CreatedAt       string             `json:"created_at"`
	UpdatedAt       string             `json:"updated_at"`
}

type SkillForkResponse struct {
	ID            string  `json:"id"`
	ParentSkillID string  `json:"parent_skill_id"`
	ForkedSkillID string  `json:"forked_skill_id"`
	ForkedBy      *string `json:"forked_by"`
	CreatedAt     string  `json:"created_at"`
}

// --- Request structs ---

type UpdateSkillOwnershipRequest struct {
	OwnerID     *string  `json:"owner_id"`
	ApproverIDs []string `json:"approver_ids"`
}

type CreateSkillChangeRequestRequest struct {
	Title           string                   `json:"title"`
	Description     string                   `json:"description"`
	ProposedVersion string                   `json:"proposed_version"`
	ProposedContent string                   `json:"proposed_content"`
	ProposedFiles   []CreateSkillFileRequest `json:"proposed_files"`
}

type ReviewSkillChangeRequestRequest struct {
	Action  string `json:"action"` // "approve" | "reject"
	Comment string `json:"comment"`
}

type ForkSkillRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	OwnerID     *string `json:"owner_id"`
}

// --- Conversion helpers ---

func skillVersionToResponse(v db.SkillVersion) SkillVersionResponse {
	files := decodeVersionFiles(v.Files)
	return SkillVersionResponse{
		ID:          uuidToString(v.ID),
		SkillID:     uuidToString(v.SkillID),
		Version:     v.Version,
		Content:     v.Content,
		Files:       files,
		Description: v.Description,
		CreatedBy:   uuidToPtr(v.CreatedBy),
		CreatedAt:   timestampToString(v.CreatedAt),
	}
}

func skillChangeRequestToResponse(c db.SkillChangeRequest) SkillChangeRequestResponse {
	return SkillChangeRequestResponse{
		ID:              uuidToString(c.ID),
		SkillID:         uuidToString(c.SkillID),
		Title:           c.Title,
		Description:     c.Description,
		BaseVersion:     c.BaseVersion,
		ProposedVersion: c.ProposedVersion,
		ProposedContent: c.ProposedContent,
		ProposedFiles:   decodeVersionFiles(c.ProposedFiles),
		Status:          c.Status,
		ProposedBy:      uuidToString(c.ProposedBy),
		ReviewedBy:      uuidToPtr(c.ReviewedBy),
		ReviewedAt:      timestampToPtr(c.ReviewedAt),
		ReviewComment:   c.ReviewComment,
		CreatedAt:       timestampToString(c.CreatedAt),
		UpdatedAt:       timestampToString(c.UpdatedAt),
	}
}

func skillForkToResponse(f db.SkillFork) SkillForkResponse {
	return SkillForkResponse{
		ID:            uuidToString(f.ID),
		ParentSkillID: uuidToString(f.ParentSkillID),
		ForkedSkillID: uuidToString(f.ForkedSkillID),
		ForkedBy:      uuidToPtr(f.ForkedBy),
		CreatedAt:     timestampToString(f.CreatedAt),
	}
}

func decodeVersionFiles(raw []byte) []skillVersionFile {
	if len(raw) == 0 {
		return []skillVersionFile{}
	}
	var out []skillVersionFile
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []skillVersionFile{}
	}
	return out
}

func encodeVersionFiles(files []CreateSkillFileRequest) []byte {
	if len(files) == 0 {
		return []byte("[]")
	}
	out := make([]skillVersionFile, len(files))
	for i, f := range files {
		out[i] = skillVersionFile{Path: f.Path, Content: f.Content}
	}
	b, err := json.Marshal(out)
	if err != nil || len(b) == 0 {
		return []byte("[]")
	}
	return b
}

// --- Semver helpers ---

var semverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// validSemver returns true if s is a strict X.Y.Z form.
func validSemver(s string) bool {
	return semverRe.MatchString(s)
}

// semverGT returns true if a > b under strict semver.
func semverGT(a, b string) bool {
	am := semverRe.FindStringSubmatch(a)
	bm := semverRe.FindStringSubmatch(b)
	if am == nil || bm == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if am[i] == bm[i] {
			continue
		}
		// String comparison would mis-rank "10" vs "2"; use length+lex.
		if len(am[i]) != len(bm[i]) {
			return len(am[i]) > len(bm[i])
		}
		return am[i] > bm[i]
	}
	return false
}

// --- Ownership endpoint ---

func (h *Handler) UpdateSkillOwnership(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}

	var req UpdateSkillOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateSkillOwnershipParams{ID: parseUUID(id)}
	if req.OwnerID != nil {
		params.OwnerID = parseUUID(*req.OwnerID)
	}
	if req.ApproverIDs != nil {
		approvers := make([]pgtype.UUID, 0, len(req.ApproverIDs))
		for _, raw := range req.ApproverIDs {
			approvers = append(approvers, parseUUID(raw))
		}
		params.ApproverIds = approvers
	}

	updated, err := h.Queries.UpdateSkillOwnership(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update ownership: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, skillToResponse(updated))
}

// --- Version endpoints ---

func (h *Handler) ListSkillVersions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	versions, err := h.Queries.ListSkillVersions(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}

	resp := make([]SkillVersionResponse, len(versions))
	for i, v := range versions {
		resp[i] = skillVersionToResponse(v)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Change request endpoints ---

func (h *Handler) ListSkillChangeRequests(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	rows, err := h.Queries.ListSkillChangeRequestsBySkill(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list change requests")
		return
	}
	resp := make([]SkillChangeRequestResponse, len(rows))
	for i, c := range rows {
		resp[i] = skillChangeRequestToResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListPendingChangeRequests(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	rows, err := h.Queries.ListPendingChangeRequestsByWorkspace(r.Context(), parseUUID(wsID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending change requests")
		return
	}
	resp := make([]SkillChangeRequestResponse, len(rows))
	for i, c := range rows {
		resp[i] = skillChangeRequestToResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateSkillChangeRequest(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateSkillChangeRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if !validSemver(req.ProposedVersion) {
		writeError(w, http.StatusBadRequest, "proposed_version must be semver X.Y.Z")
		return
	}
	if !semverGT(req.ProposedVersion, skill.CurrentVersion) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"proposed_version %s must be greater than current %s", req.ProposedVersion, skill.CurrentVersion))
		return
	}
	for _, f := range req.ProposedFiles {
		if !validateFilePath(f.Path) {
			writeError(w, http.StatusBadRequest, "invalid file path: "+f.Path)
			return
		}
	}

	cr, err := h.Queries.CreateSkillChangeRequest(r.Context(), db.CreateSkillChangeRequestParams{
		SkillID:         skill.ID,
		Title:           req.Title,
		Description:     req.Description,
		BaseVersion:     skill.CurrentVersion,
		ProposedVersion: req.ProposedVersion,
		ProposedContent: req.ProposedContent,
		ProposedFiles:   encodeVersionFiles(req.ProposedFiles),
		ProposedBy:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create change request: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, skillChangeRequestToResponse(cr))
}

func (h *Handler) ReviewSkillChangeRequest(w http.ResponseWriter, r *http.Request) {
	crID := chi.URLParam(r, "crId")
	cr, err := h.Queries.GetSkillChangeRequest(r.Context(), parseUUID(crID))
	if err != nil {
		writeError(w, http.StatusNotFound, "change request not found")
		return
	}
	skill, ok := h.loadSkillForUser(w, r, uuidToString(cr.SkillID))
	if !ok {
		return
	}
	if !h.canManageSkill(w, r, skill) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req ReviewSkillChangeRequestRequest
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
		// Approve transitions to merged in one shot — we apply the proposed
		// content/version atomically, snapshot the new version, then mark the
		// change request merged. We re-fetch the CR inside the tx with
		// FOR UPDATE so two approvers can't race past the status check, and
		// we re-validate semverGT in case the owner did a direct UpdateSkill
		// (which bumps current_version) between propose and approve.
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.Queries.WithTx(tx)

		locked, err := qtx.GetSkillChangeRequestForUpdate(r.Context(), cr.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock change request: "+err.Error())
			return
		}
		if locked.Status != "pending" {
			writeError(w, http.StatusConflict, "change request is no longer pending")
			return
		}
		freshSkill, err := qtx.GetSkillInWorkspace(r.Context(), db.GetSkillInWorkspaceParams{
			ID:          skill.ID,
			WorkspaceID: skill.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reload skill: "+err.Error())
			return
		}
		if !semverGT(locked.ProposedVersion, freshSkill.CurrentVersion) {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"proposed_version %s is no longer greater than current %s — rebase the change request",
				locked.ProposedVersion, freshSkill.CurrentVersion))
			return
		}

		updatedSkill, err := qtx.UpdateSkillCurrentVersion(r.Context(), db.UpdateSkillCurrentVersionParams{
			ID:             skill.ID,
			CurrentVersion: locked.ProposedVersion,
			Content:        locked.ProposedContent,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply change: "+err.Error())
			return
		}

		// Replace skill files from the snapshot.
		if err := qtx.DeleteSkillFilesBySkill(r.Context(), updatedSkill.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear files: "+err.Error())
			return
		}
		for _, f := range decodeVersionFiles(locked.ProposedFiles) {
			if _, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
				SkillID: updatedSkill.ID,
				Path:    f.Path,
				Content: f.Content,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to write file: "+err.Error())
				return
			}
		}

		if _, err := qtx.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
			SkillID:     updatedSkill.ID,
			Version:     locked.ProposedVersion,
			Content:     locked.ProposedContent,
			Files:       locked.ProposedFiles,
			Description: locked.Title,
			CreatedBy:   parseUUID(userID),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to snapshot version: "+err.Error())
			return
		}

		updatedCR, err := qtx.ReviewSkillChangeRequest(r.Context(), db.ReviewSkillChangeRequestParams{
			ID:            locked.ID,
			Status:        "merged",
			ReviewedBy:    parseUUID(userID),
			ReviewComment: req.Comment,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark merged: "+err.Error())
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, skillChangeRequestToResponse(updatedCR))

	case "reject":
		updatedCR, err := h.Queries.ReviewSkillChangeRequest(r.Context(), db.ReviewSkillChangeRequestParams{
			ID:            cr.ID,
			Status:        "rejected",
			ReviewedBy:    parseUUID(userID),
			ReviewComment: req.Comment,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, skillChangeRequestToResponse(updatedCR))

	default:
		writeError(w, http.StatusBadRequest, "action must be 'approve' or 'reject'")
	}
}

// --- Fork endpoints ---

func (h *Handler) ListSkillForks(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	rows, err := h.Queries.ListSkillForksByParent(r.Context(), skill.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list forks")
		return
	}

	type forkRow struct {
		SkillForkResponse
		ForkedName        string `json:"forked_name"`
		ForkedDescription string `json:"forked_description"`
		CurrentVersion    string `json:"current_version"`
	}
	resp := make([]forkRow, len(rows))
	for i, f := range rows {
		resp[i] = forkRow{
			SkillForkResponse: SkillForkResponse{
				ID:            uuidToString(f.ID),
				ParentSkillID: uuidToString(f.ParentSkillID),
				ForkedSkillID: uuidToString(f.ForkedSkillID),
				ForkedBy:      uuidToPtr(f.ForkedBy),
				CreatedAt:     timestampToString(f.CreatedAt),
			},
			ForkedName:        f.ForkedName,
			ForkedDescription: f.ForkedDescription,
			CurrentVersion:    f.CurrentVersion,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateSkillFork copies the parent skill (content + files) into a new skill
// owned by the requesting user. The parent → fork link is recorded so the UI
// can show lineage. Forks live as full skill rows so they get their own
// version history and ownership chain — that's the "fork = branch" model from
// JEH-216.
func (h *Handler) CreateSkillFork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	parent, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req ForkSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Carry over the parent's config so origin metadata isn't lost.
	config := parent.Config
	if len(config) == 0 {
		config = []byte("{}")
	}

	owner := parseUUID(userID)
	if req.OwnerID != nil && *req.OwnerID != "" {
		owner = parseUUID(*req.OwnerID)
	}

	desc := req.Description
	if desc == "" {
		desc = parent.Description
	}

	forked, err := qtx.CreateSkill(r.Context(), db.CreateSkillParams{
		WorkspaceID: parent.WorkspaceID,
		Name:        req.Name,
		Description: desc,
		Content:     parent.Content,
		Config:      config,
		CreatedBy:   parseUUID(userID),
		OwnerID:     owner,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a skill with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fork skill: "+err.Error())
		return
	}

	parentFiles, err := qtx.ListSkillFiles(r.Context(), parent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read parent files: "+err.Error())
		return
	}
	fileResps := make([]SkillFileResponse, 0, len(parentFiles))
	for _, f := range parentFiles {
		sf, err := qtx.UpsertSkillFile(r.Context(), db.UpsertSkillFileParams{
			SkillID: forked.ID,
			Path:    f.Path,
			Content: f.Content,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to copy file: "+err.Error())
			return
		}
		fileResps = append(fileResps, skillFileToResponse(sf))
	}

	if _, err := qtx.CreateSkillVersion(r.Context(), db.CreateSkillVersionParams{
		SkillID:     forked.ID,
		Version:     forked.CurrentVersion,
		Content:     forked.Content,
		Files:       skillFilesToVersionJSON(fileResps),
		Description: fmt.Sprintf("Forked from %s @ %s", parent.Name, parent.CurrentVersion),
		CreatedBy:   parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to snapshot fork version: "+err.Error())
		return
	}

	if _, err := qtx.CreateSkillFork(r.Context(), db.CreateSkillForkParams{
		ParentSkillID: parent.ID,
		ForkedSkillID: forked.ID,
		ForkedBy:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record fork link: "+err.Error())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, SkillWithFilesResponse{
		SkillResponse: skillToResponse(forked),
		Files:         fileResps,
	})
}
