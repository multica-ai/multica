package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InvitationResponse is the JSON shape returned for a workspace invitation.
// Shareable links have invitee_email == nil and carry max_uses / use_count
// state; targeted invitations have invitee_email set and leave max_uses nil.
type InvitationResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	InviterID     string  `json:"inviter_id"`
	InviteeEmail  *string `json:"invitee_email"`
	InviteeUserID *string `json:"invitee_user_id"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     string  `json:"expires_at"`
	// Shareable-link fields. max_uses nil = unlimited.
	Shareable bool   `json:"shareable"`
	MaxUses   *int32 `json:"max_uses"`
	UseCount  int32  `json:"use_count"`
	// Enriched fields (present in list responses).
	InviterName   string `json:"inviter_name,omitempty"`
	InviterEmail  string `json:"inviter_email,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

func textToEmailPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func invitationToResponse(inv db.WorkspaceInvitation) InvitationResponse {
	var maxUses *int32
	if inv.MaxUses.Valid {
		v := inv.MaxUses.Int32
		maxUses = &v
	}
	return InvitationResponse{
		ID:            uuidToString(inv.ID),
		WorkspaceID:   uuidToString(inv.WorkspaceID),
		InviterID:     uuidToString(inv.InviterID),
		InviteeEmail:  textToEmailPtr(inv.InviteeEmail),
		InviteeUserID: uuidToPtr(inv.InviteeUserID),
		Role:          inv.Role,
		Status:        inv.Status,
		CreatedAt:     timestampToString(inv.CreatedAt),
		UpdatedAt:     timestampToString(inv.UpdatedAt),
		ExpiresAt:     timestampToString(inv.ExpiresAt),
		Shareable:     !inv.InviteeEmail.Valid,
		MaxUses:       maxUses,
		UseCount:      inv.UseCount,
	}
}

// ---------------------------------------------------------------------------
// CreateInvitation replaces the old "instant-add" CreateMember flow.
// POST /api/workspaces/{id}/members  (same endpoint, new behaviour)
// ---------------------------------------------------------------------------

func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}
	if role == "owner" {
		writeError(w, http.StatusBadRequest, "cannot invite as owner")
		return
	}

	// Shareable links carry a NULL invitee_email and (optional) max_uses cap.
	// No duplicate/member checks are useful here because the link isn't tied
	// to any specific recipient — the recipient is whoever holds the URL.
	if req.Shareable {
		var maxUses pgtype.Int4
		if req.MaxUses != nil {
			if *req.MaxUses <= 0 {
				writeError(w, http.StatusBadRequest, "max_uses must be positive")
				return
			}
			maxUses = pgtype.Int4{Int32: *req.MaxUses, Valid: true}
		}

		inv, err := h.Queries.CreateInvitation(r.Context(), db.CreateInvitationParams{
			WorkspaceID:   parseUUID(workspaceID),
			InviterID:     requester.UserID,
			InviteeEmail:  pgtype.Text{Valid: false},
			InviteeUserID: pgtype.UUID{},
			Role:          role,
			MaxUses:       maxUses,
		})
		if err != nil {
			slog.Warn("create shareable invitation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create invitation")
			return
		}

		slog.Info("shareable invitation created", append(logger.RequestAttrs(r), "invitation_id", uuidToString(inv.ID), "workspace_id", workspaceID, "role", role, "max_uses", req.MaxUses)...)

		resp := invitationToResponse(inv)
		userID := requestUserID(r)
		eventPayload := map[string]any{"invitation": resp}
		if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID)); err == nil {
			eventPayload["workspace_name"] = ws.Name
		}
		h.publish(protocol.EventInvitationCreated, workspaceID, "member", userID, eventPayload)

		h.Analytics.Capture(analytics.TeamInviteSent(
			uuidToString(requester.UserID),
			workspaceID,
			"", // shareable links have no recipient email
			"link",
		))

		writeJSON(w, http.StatusCreated, resp)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	// Check if the user is already a member.
	existingUser, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err == nil {
		_, memberErr := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      existingUser.ID,
			WorkspaceID: parseUUID(workspaceID),
		})
		if memberErr == nil {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
	}

	// Check if there is already a pending invitation.
	_, err = h.Queries.GetPendingInvitationByEmail(r.Context(), db.GetPendingInvitationByEmailParams{
		WorkspaceID:  parseUUID(workspaceID),
		InviteeEmail: pgtype.Text{String: email, Valid: true},
	})
	if err == nil {
		writeError(w, http.StatusConflict, "invitation already pending for this email")
		return
	}

	// Resolve invitee_user_id if the user already exists.
	var inviteeUserID pgtype.UUID
	if existingUser.ID.Valid {
		inviteeUserID = existingUser.ID
	}

	inv, err := h.Queries.CreateInvitation(r.Context(), db.CreateInvitationParams{
		WorkspaceID:   parseUUID(workspaceID),
		InviterID:     requester.UserID,
		InviteeEmail:  pgtype.Text{String: email, Valid: true},
		InviteeUserID: inviteeUserID,
		Role:          role,
		MaxUses:       pgtype.Int4{Valid: false},
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "invitation already pending for this email")
			return
		}
		slog.Warn("create invitation failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	slog.Info("invitation created", append(logger.RequestAttrs(r), "invitation_id", uuidToString(inv.ID), "workspace_id", workspaceID, "email", email, "role", role)...)

	resp := invitationToResponse(inv)

	// Notify the invitee in real time if they are a registered user.
	userID := requestUserID(r)
	eventPayload := map[string]any{"invitation": resp}
	var workspaceName string
	if ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID)); err == nil {
		workspaceName = ws.Name
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventInvitationCreated, workspaceID, "member", userID, eventPayload)

	h.Analytics.Capture(analytics.TeamInviteSent(
		uuidToString(requester.UserID),
		workspaceID,
		email,
		"email",
	))

	// Send invitation email (fire-and-forget).
	if h.EmailService != nil && workspaceName != "" {
		inviterName := email // fallback
		if inviter, err := h.Queries.GetUser(r.Context(), requester.UserID); err == nil {
			inviterName = inviter.Name
		}
		invID := uuidToString(inv.ID)
		go func() {
			if err := h.EmailService.SendInvitationEmail(email, inviterName, workspaceName, invID); err != nil {
				slog.Warn("failed to send invitation email", "email", email, "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// ListWorkspaceInvitations — pending invitations for a workspace (admin view).
// GET /api/workspaces/{id}/invitations
// ---------------------------------------------------------------------------

func (h *Handler) ListWorkspaceInvitations(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")

	rows, err := h.Queries.ListPendingInvitationsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		var maxUses *int32
		if row.MaxUses.Valid {
			v := row.MaxUses.Int32
			maxUses = &v
		}
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  textToEmailPtr(row.InviteeEmail),
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			Shareable:     !row.InviteeEmail.Valid,
			MaxUses:       maxUses,
			UseCount:      row.UseCount,
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// RevokeInvitation — admin cancels a pending invitation.
// DELETE /api/workspaces/{id}/invitations/{invitationId}
// ---------------------------------------------------------------------------

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	invitationID := chi.URLParam(r, "invitationId")

	inv, err := h.Queries.GetInvitation(r.Context(), parseUUID(invitationID))
	if err != nil || uuidToString(inv.WorkspaceID) != workspaceID || inv.Status != "pending" {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	if err := h.Queries.RevokeInvitation(r.Context(), inv.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}

	slog.Info("invitation revoked", "invitation_id", invitationID, "workspace_id", workspaceID)

	userID := requestUserID(r)
	h.publish(protocol.EventInvitationRevoked, workspaceID, "member", userID, map[string]any{
		"invitation_id":   invitationID,
		"invitee_email":   textToEmailPtr(inv.InviteeEmail),
		"invitee_user_id": uuidToPtr(inv.InviteeUserID),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// GetMyInvitation — get a single invitation by ID (for the invite accept page).
// GET /api/invitations/{id}
// ---------------------------------------------------------------------------

func (h *Handler) GetMyInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	inv, err := h.Queries.GetInvitation(r.Context(), parseUUID(invitationID))
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	// For targeted invitations, verify ownership by email or user_id.
	// Shareable links (invitee_email IS NULL) are treated as capability
	// URLs — anyone logged in who has the link may look it up.
	if inv.InviteeEmail.Valid {
		user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
		if strings.ToLower(user.Email) != inv.InviteeEmail.String && uuidToString(inv.InviteeUserID) != userID {
			writeError(w, http.StatusForbidden, "invitation does not belong to you")
			return
		}
	}

	resp := invitationToResponse(inv)

	// Enrich with workspace name and inviter name.
	if ws, err := h.Queries.GetWorkspace(r.Context(), inv.WorkspaceID); err == nil {
		resp.WorkspaceName = ws.Name
	}
	if inviter, err := h.Queries.GetUser(r.Context(), inv.InviterID); err == nil {
		resp.InviterName = inviter.Name
		resp.InviterEmail = inviter.Email
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// ListMyInvitations — current user's pending invitations across all workspaces.
// GET /api/invitations
// ---------------------------------------------------------------------------

func (h *Handler) ListMyInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	rows, err := h.Queries.ListPendingInvitationsForUser(r.Context(), db.ListPendingInvitationsForUserParams{
		InviteeUserID: user.ID,
		InviteeEmail:  pgtype.Text{String: user.Email, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		var maxUses *int32
		if row.MaxUses.Valid {
			v := row.MaxUses.Int32
			maxUses = &v
		}
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  textToEmailPtr(row.InviteeEmail),
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			Shareable:     !row.InviteeEmail.Valid,
			MaxUses:       maxUses,
			UseCount:      row.UseCount,
			WorkspaceName: row.WorkspaceName,
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// AcceptInvitation — user accepts a pending invitation.
// POST /api/invitations/{id}/accept
// ---------------------------------------------------------------------------

func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	inv, err := h.Queries.GetInvitation(r.Context(), parseUUID(invitationID))
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	// Targeted invitations require matching email or user_id. Shareable
	// links (invitee_email IS NULL) are capability URLs — skip the check.
	if inv.InviteeEmail.Valid {
		if strings.ToLower(user.Email) != inv.InviteeEmail.String && uuidToString(inv.InviteeUserID) != userID {
			writeError(w, http.StatusForbidden, "invitation does not belong to you")
			return
		}
	}

	// A shareable link flips status='accepted' only when max_uses has been
	// reached; for that path, surface a clearer 410 instead of the generic
	// "not pending" message.
	if inv.Status != "pending" {
		if !inv.InviteeEmail.Valid {
			writeError(w, http.StatusGone, "invitation link has been used up")
			return
		}
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}

	// Check expiry.
	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invitation has expired")
		return
	}

	// Use a transaction: mark accepted (or bump use_count) + create member atomically.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	var accepted db.WorkspaceInvitation
	if inv.InviteeEmail.Valid {
		accepted, err = qtx.AcceptInvitation(r.Context(), inv.ID)
	} else {
		// Shareable: atomic bump of use_count under the pending + capacity
		// guard. If another redeem raced past max_uses this returns ErrNoRows
		// and we surface it as 410 Gone.
		accepted, err = qtx.RedeemShareableInvitation(r.Context(), inv.ID)
		if isNotFound(err) {
			writeError(w, http.StatusGone, "invitation link has been used up or expired")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: accepted.WorkspaceID,
		UserID:      user.ID,
		Role:        accepted.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "you are already a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}

	slog.Info("invitation accepted", "invitation_id", invitationID, "user_id", userID, "workspace_id", uuidToString(accepted.WorkspaceID), "shareable", !inv.InviteeEmail.Valid)

	wsID := uuidToString(accepted.WorkspaceID)
	memberResp := memberWithUserResponse(member, user)

	// Broadcast member:added so existing clients update their member lists.
	eventPayload := map[string]any{"member": memberResp}
	if ws, err := h.Queries.GetWorkspace(r.Context(), accepted.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, wsID, "member", userID, eventPayload)

	// Notify the workspace about the acceptance.
	h.publish(protocol.EventInvitationAccepted, wsID, "member", userID, map[string]any{
		"invitation_id": invitationID,
		"member":        memberResp,
	})

	// days_since_invite rounds down to whole days so the funnel segments
	// "accepted same day" cleanly from "accepted later". inv.CreatedAt is
	// the invitation row's insertion time so this is safe to compute here.
	var daysSinceInvite int64
	if inv.CreatedAt.Valid {
		daysSinceInvite = int64(time.Since(inv.CreatedAt.Time).Hours() / 24)
	}
	h.Analytics.Capture(analytics.TeamInviteAccepted(
		userID,
		wsID,
		daysSinceInvite,
	))

	writeJSON(w, http.StatusOK, memberResp)
}

// ---------------------------------------------------------------------------
// DeclineInvitation — user declines a pending invitation.
// POST /api/invitations/{id}/decline
// ---------------------------------------------------------------------------

func (h *Handler) DeclineInvitation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	invitationID := chi.URLParam(r, "id")
	inv, err := h.Queries.GetInvitation(r.Context(), parseUUID(invitationID))
	if err != nil {
		writeError(w, http.StatusNotFound, "invitation not found")
		return
	}

	// Declining only makes sense for a targeted invitation — a shareable
	// link has no single recipient to decline on behalf of.
	if !inv.InviteeEmail.Valid {
		writeError(w, http.StatusBadRequest, "shareable invitation links cannot be declined")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != inv.InviteeEmail.String && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}

	declined, err := h.Queries.DeclineInvitation(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decline invitation")
		return
	}

	slog.Info("invitation declined", "invitation_id", invitationID, "user_id", userID)

	wsID := uuidToString(declined.WorkspaceID)
	h.publish(protocol.EventInvitationDeclined, wsID, "member", userID, map[string]any{
		"invitation_id": invitationID,
		"invitee_email": textToEmailPtr(declined.InviteeEmail),
	})

	w.WriteHeader(http.StatusNoContent)
}
