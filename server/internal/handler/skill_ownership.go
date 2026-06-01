package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	// CEREBRO-PATCH(skill-change-request-session): FIR-2627 link back to the
	// agent run that proposed this change so the inbox can deep-link to it.
	WorkSessionID *string `json:"work_session_id"`
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
	// CEREBRO-PATCH(skill-change-request-session): FIR-2627 optional pointer to
	// the agent work_session that produced this proposal.
	WorkSessionID *string `json:"work_session_id"`
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
		// CEREBRO-PATCH(skill-change-request-session): FIR-2627 session link.
		WorkSessionID: uuidToPtr(c.WorkSessionID),
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

	// CEREBRO-PATCH(skill-change-request-session): FIR-2627 — accept optional
	// work_session_id, validate it points at a real session in this workspace
	// before persisting so the link in the inbox never resolves to a stranded id.
	var sessionUUID pgtype.UUID
	if req.WorkSessionID != nil && *req.WorkSessionID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, *req.WorkSessionID, "work_session_id")
		if !ok {
			return
		}
		session, err := h.Queries.GetWorkSession(r.Context(), parsed)
		if err != nil || uuidToString(session.WorkspaceID) != uuidToString(skill.WorkspaceID) {
			writeError(w, http.StatusBadRequest, "work_session_id not found in this workspace")
			return
		}
		sessionUUID = parsed
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
		WorkSessionID:   sessionUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create change request: "+err.Error())
		return
	}

	// CEREBRO-PATCH(skill-change-request-inbox): FIR-2627 — notify owner +
	// approvers so the proposal lands in their inbox with diff + reason + link.
	h.notifySkillChangeRequestCreated(r.Context(), skill, cr)

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
		// CEREBRO-PATCH(skill-change-request-inbox): FIR-2627 — proposer inbox.
		h.notifySkillChangeRequestReviewed(r.Context(), skill, updatedCR, parseUUID(userID))
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
		// CEREBRO-PATCH(skill-change-request-inbox): FIR-2627 — proposer inbox.
		h.notifySkillChangeRequestReviewed(r.Context(), skill, updatedCR, parseUUID(userID))
		writeJSON(w, http.StatusOK, skillChangeRequestToResponse(updatedCR))

	default:
		writeError(w, http.StatusBadRequest, "action must be 'approve' or 'reject'")
	}
}

// --- Fork endpoints ---

// GetSkillForkParent returns the lineage link for a skill that is itself a fork
// — i.e. the parent it was forked from. Returns 404 when the skill is not a
// fork (no parent link), which the UI treats as "this is an original skill".
// CEREBRO-PATCH(skill-fork-parent-lineage): FIR-2629 expose "forked from" so the web UI can show both lineage directions.
func (h *Handler) GetSkillForkParent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.loadSkillForUser(w, r, id)
	if !ok {
		return
	}

	row, err := h.Queries.GetSkillForkParent(r.Context(), skill.ID)
	if err != nil {
		// No parent link → not a fork. 404 is the "no lineage" signal the
		// frontend query treats as null rather than an error.
		writeError(w, http.StatusNotFound, "skill is not a fork")
		return
	}

	writeJSON(w, http.StatusOK, struct {
		SkillForkResponse
		ParentName string `json:"parent_name"`
	}{
		SkillForkResponse: SkillForkResponse{
			ID:            uuidToString(row.ID),
			ParentSkillID: uuidToString(row.ParentSkillID),
			ForkedSkillID: uuidToString(row.ForkedSkillID),
			ForkedBy:      uuidToPtr(row.ForkedBy),
			CreatedAt:     timestampToString(row.CreatedAt),
		},
		ParentName: row.ParentName,
	})
}

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

// CEREBRO-PATCH(skill-change-request-inbox): FIR-2627 notification + diff
// helpers. Fan out one inbox_item per recipient — the owner first, then every
// approver — skipping the proposer themselves so an owner-self-proposal does
// not page anyone. Errors are logged, not returned: a failed notification must
// never roll back the change request itself.

const (
	inboxTypeSkillChangeRequestCreated  = "skill_change_request_created"
	inboxTypeSkillChangeRequestReviewed = "skill_change_request_reviewed"
	maxSkillDiffChars                   = 8 * 1024
)

func (h *Handler) notifySkillChangeRequestCreated(ctx context.Context, skill db.Skill, cr db.SkillChangeRequest) {
	recipients := skillChangeRequestRecipients(skill, cr.ProposedBy)
	if len(recipients) == 0 {
		return
	}
	details := h.buildSkillChangeRequestDetails(ctx, skill, cr)
	body := fmt.Sprintf("%s proposed %s → %s on %s",
		shortUUID(cr.ProposedBy), cr.BaseVersion, cr.ProposedVersion, skill.Name)
	title := fmt.Sprintf("Change request on %s", skill.Name)
	for _, rid := range recipients {
		h.writeSkillInboxItem(ctx, skill.WorkspaceID, rid, inboxTypeSkillChangeRequestCreated,
			"action_required", title, body, "member", cr.ProposedBy, details)
	}
}

func (h *Handler) notifySkillChangeRequestReviewed(ctx context.Context, skill db.Skill, cr db.SkillChangeRequest, reviewerID pgtype.UUID) {
	if !cr.ProposedBy.Valid {
		return
	}
	if reviewerID.Valid && uuidToString(cr.ProposedBy) == uuidToString(reviewerID) {
		// Reviewer == proposer (owner self-merge). Skipping keeps the
		// inbox clean — they already see the action they just took.
		return
	}
	details := h.buildSkillChangeRequestDetails(ctx, skill, cr)
	details["review_status"] = cr.Status
	details["review_comment"] = cr.ReviewComment
	if reviewerID.Valid {
		details["reviewed_by"] = uuidToString(reviewerID)
	}
	action := "rejected"
	severity := "info"
	if cr.Status == "merged" || cr.Status == "approved" {
		action = "approved"
	}
	title := fmt.Sprintf("Change request %s on %s", action, skill.Name)
	body := fmt.Sprintf("%s → %s on %s", cr.BaseVersion, cr.ProposedVersion, skill.Name)
	if cr.ReviewComment != "" {
		body = body + ": " + cr.ReviewComment
	}
	h.writeSkillInboxItem(ctx, skill.WorkspaceID, cr.ProposedBy, inboxTypeSkillChangeRequestReviewed,
		severity, title, body, "member", reviewerID, details)
}

// skillChangeRequestRecipients returns the deduplicated set of users who should
// see a proposal in their inbox: the owner and every approver, minus the
// proposer themselves.
func skillChangeRequestRecipients(skill db.Skill, proposer pgtype.UUID) []pgtype.UUID {
	seen := map[string]struct{}{}
	if proposer.Valid {
		seen[uuidToString(proposer)] = struct{}{}
	}
	out := make([]pgtype.UUID, 0, 1+len(skill.ApproverIds))
	add := func(u pgtype.UUID) {
		if !u.Valid {
			return
		}
		key := uuidToString(u)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, u)
	}
	add(skill.OwnerID)
	for _, a := range skill.ApproverIds {
		add(a)
	}
	return out
}

// buildSkillChangeRequestDetails composes the JSONB payload attached to the
// inbox_item. The frontend uses these fields to render the proposal — full
// content (so it can mount a rich diff viewer) plus a precomputed text diff
// for terminal / push surfaces.
func (h *Handler) buildSkillChangeRequestDetails(ctx context.Context, skill db.Skill, cr db.SkillChangeRequest) map[string]any {
	baseFiles, err := h.Queries.ListSkillFiles(ctx, skill.ID)
	if err != nil {
		slog.Warn("skill change request: failed to load base files for diff",
			"skill_id", uuidToString(skill.ID), "error", err)
	}
	baseFileSnapshots := make([]skillVersionFile, 0, len(baseFiles))
	for _, f := range baseFiles {
		baseFileSnapshots = append(baseFileSnapshots, skillVersionFile{Path: f.Path, Content: f.Content})
	}
	details := map[string]any{
		"change_request_id": uuidToString(cr.ID),
		"skill_id":          uuidToString(skill.ID),
		"skill_name":        skill.Name,
		"proposer_id":       uuidToString(cr.ProposedBy),
		"title":             cr.Title,
		"description":       cr.Description,
		"base_version":      cr.BaseVersion,
		"proposed_version":  cr.ProposedVersion,
		"base_content":      skill.Content,
		"proposed_content":  cr.ProposedContent,
		"base_files":        baseFileSnapshots,
		"proposed_files":    decodeVersionFiles(cr.ProposedFiles),
		"diff":              truncateForInbox(computeUnifiedDiff(skill.Content, cr.ProposedContent, "SKILL.md")),
	}
	if cr.WorkSessionID.Valid {
		details["work_session_id"] = uuidToString(cr.WorkSessionID)
		details["session_url"] = fmt.Sprintf("/sessions/%s", uuidToString(cr.WorkSessionID))
	}
	return details
}

func (h *Handler) writeSkillInboxItem(
	ctx context.Context,
	workspaceID, recipientID pgtype.UUID,
	itemType, severity, title, body, actorType string,
	actorID pgtype.UUID,
	details map[string]any,
) {
	payload, err := json.Marshal(details)
	if err != nil {
		slog.Warn("skill change request: failed to encode inbox details",
			"workspace_id", uuidToString(workspaceID), "recipient_id", uuidToString(recipientID), "error", err)
		return
	}
	item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   workspaceID,
		RecipientType: "member",
		RecipientID:   recipientID,
		Type:          itemType,
		Severity:      severity,
		IssueID:       pgtype.UUID{},
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: body != ""},
		ActorType:     pgtype.Text{String: actorType, Valid: actorType != ""},
		ActorID:       actorID,
		Details:       payload,
		Route:         "inbox",
	})
	if err != nil {
		slog.Warn("skill change request: inbox write failed",
			"workspace_id", uuidToString(workspaceID), "recipient_id", uuidToString(recipientID), "error", err)
		return
	}
	if h.Bus == nil {
		return
	}
	h.Bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: uuidToString(workspaceID),
		ActorType:   actorType,
		ActorID:     uuidToString(actorID),
		Payload: map[string]any{"item": map[string]any{
			"id":             uuidToString(item.ID),
			"workspace_id":   uuidToString(item.WorkspaceID),
			"recipient_type": item.RecipientType,
			"recipient_id":   uuidToString(item.RecipientID),
			"type":           item.Type,
			"severity":       item.Severity,
			"title":          item.Title,
			"body":           textToPtr(item.Body),
			"read":           item.Read,
			"archived":       item.Archived,
			"created_at":     timestampToString(item.CreatedAt),
			"actor_type":     textToPtr(item.ActorType),
			"actor_id":       uuidToPtr(item.ActorID),
			"details":        json.RawMessage(item.Details),
		}},
	})
}

// shortUUID returns the first 8 hex chars of a UUID for use in inbox titles
// when we don't have the user's display name on hand. The frontend resolves
// the full name from proposer_id in details; the body is the push / terminal
// fallback only.
func shortUUID(u pgtype.UUID) string {
	if !u.Valid {
		return "someone"
	}
	s := uuidToString(u)
	if len(s) >= 8 {
		return s[:8]
	}
	return s
}

// computeUnifiedDiff produces a small unified-diff-style string for SKILL.md
// content. This is not a real Myers diff — it's a line-by-line LCS scan that's
// small (~50 LOC, no external deps) and exact for the only shape that matters
// here: humans tweaking prose. The full base/proposed content is also shipped
// in details, so the frontend can swap in a richer viewer when it wants.
func computeUnifiedDiff(base, proposed, label string) string {
	if base == proposed {
		return ""
	}
	baseLines := splitLines(base)
	proposedLines := splitLines(proposed)
	ops := diffLines(baseLines, proposedLines)
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s (v base)\n+++ %s (v proposed)\n", label, label)
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			fmt.Fprintf(&b, " %s\n", op.line)
		case diffDel:
			fmt.Fprintf(&b, "-%s\n", op.line)
		case diffAdd:
			fmt.Fprintf(&b, "+%s\n", op.line)
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

const (
	diffEqual = iota
	diffAdd
	diffDel
)

type diffOp struct {
	kind int
	line string
}

// diffLines is a textbook LCS-based diff. Sufficient for SKILL.md-sized inputs
// (rarely over a few hundred lines); the O(N*M) memory cost is bounded by the
// truncation we apply before storage.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 {
		out := make([]diffOp, m)
		for i, line := range b {
			out[i] = diffOp{kind: diffAdd, line: line}
		}
		return out
	}
	if m == 0 {
		out := make([]diffOp, n)
		for i, line := range a {
			out[i] = diffOp{kind: diffDel, line: line}
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var ops []diffOp
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			ops = append([]diffOp{{kind: diffEqual, line: a[i-1]}}, ops...)
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
			i--
		} else {
			ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
			j--
		}
	}
	for i > 0 {
		ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
		i--
	}
	for j > 0 {
		ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
		j--
	}
	return ops
}

// truncateForInbox keeps the precomputed diff under the soft cap so we do not
// store giant text blobs in the inbox_item.details JSONB. The full base /
// proposed content is also stored, so a frontend that wants the real thing
// can re-diff client-side without hitting this ceiling.
func truncateForInbox(s string) string {
	if len(s) <= maxSkillDiffChars {
		return s
	}
	return s[:maxSkillDiffChars] + "\n... (truncated)"
}

