package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
type InvitationResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	InviterID     string  `json:"inviter_id"`
	InviteeEmail  string  `json:"invitee_email"`
	InviteeUserID *string `json:"invitee_user_id"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	ExpiresAt     string  `json:"expires_at"`
	// Enriched fields (present in list responses).
	InviterName   string `json:"inviter_name,omitempty"`
	InviterEmail  string `json:"inviter_email,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
}

type InviteLinkResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id,omitempty"`
	WorkspaceName string  `json:"workspace_name,omitempty"`
	InviterID     string  `json:"inviter_id,omitempty"`
	InviterName   string  `json:"inviter_name,omitempty"`
	InviterEmail  string  `json:"inviter_email,omitempty"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	Error         string  `json:"error,omitempty"`
	CreatedAt     string  `json:"created_at,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	ExpiresAt     string  `json:"expires_at"`
	MaxUses       int32   `json:"max_uses"`
	UsedCount     int32   `json:"used_count"`
	RevokedAt     *string `json:"revoked_at,omitempty"`
	LastUsedAt    *string `json:"last_used_at,omitempty"`
	Token         string  `json:"token,omitempty"`
	InviteURL     string  `json:"invite_url,omitempty"`
}

type CreateInviteLinkRequest struct {
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
	TTLHours  int    `json:"ttl_hours"`
	MaxUses   int32  `json:"max_uses"`
}

func invitationToResponse(inv db.WorkspaceInvitation) InvitationResponse {
	return InvitationResponse{
		ID:            uuidToString(inv.ID),
		WorkspaceID:   uuidToString(inv.WorkspaceID),
		InviterID:     uuidToString(inv.InviterID),
		InviteeEmail:  textValue(inv.InviteeEmail),
		InviteeUserID: uuidToPtr(inv.InviteeUserID),
		Role:          inv.Role,
		Status:        inv.Status,
		CreatedAt:     timestampToString(inv.CreatedAt),
		UpdatedAt:     timestampToString(inv.UpdatedAt),
		ExpiresAt:     timestampToString(inv.ExpiresAt),
	}
}

func inviteLinkStatus(status string, expiresAt, revokedAt pgtype.Timestamptz, usedCount, maxUses int32) (string, string) {
	if revokedAt.Valid {
		return "revoked", "revoked"
	}
	if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
		return "expired", "expired"
	}
	if status != "pending" || usedCount >= maxUses {
		return "used_up", "used_up"
	}
	return "valid", ""
}

func inviteLinkToResponse(inv db.WorkspaceInvitation, token string) InviteLinkResponse {
	status, reason := inviteLinkStatus(inv.Status, inv.ExpiresAt, inv.RevokedAt, inv.UsedCount, inv.MaxUses)
	return InviteLinkResponse{
		ID:          uuidToString(inv.ID),
		WorkspaceID: uuidToString(inv.WorkspaceID),
		InviterID:   uuidToString(inv.InviterID),
		Role:        inv.Role,
		Status:      status,
		Error:       reason,
		CreatedAt:   timestampToString(inv.CreatedAt),
		UpdatedAt:   timestampToString(inv.UpdatedAt),
		ExpiresAt:   timestampToString(inv.ExpiresAt),
		MaxUses:     inv.MaxUses,
		UsedCount:   inv.UsedCount,
		RevokedAt:   timestampToPtr(inv.RevokedAt),
		LastUsedAt:  timestampToPtr(inv.LastUsedAt),
		Token:       token,
	}
}

func inviteLinkRowToResponse(row db.GetInviteLinkByTokenHashRow) InviteLinkResponse {
	status, reason := inviteLinkStatus(row.Status, row.ExpiresAt, row.RevokedAt, row.UsedCount, row.MaxUses)
	return InviteLinkResponse{
		ID:            uuidToString(row.ID),
		WorkspaceID:   uuidToString(row.WorkspaceID),
		WorkspaceName: row.WorkspaceName,
		InviterID:     uuidToString(row.InviterID),
		InviterName:   row.InviterName,
		InviterEmail:  row.InviterEmail,
		Role:          row.Role,
		Status:        status,
		Error:         reason,
		CreatedAt:     timestampToString(row.CreatedAt),
		UpdatedAt:     timestampToString(row.UpdatedAt),
		ExpiresAt:     timestampToString(row.ExpiresAt),
		MaxUses:       row.MaxUses,
		UsedCount:     row.UsedCount,
		RevokedAt:     timestampToPtr(row.RevokedAt),
		LastUsedAt:    timestampToPtr(row.LastUsedAt),
	}
}

func inviteLinkIDRowToResponse(row db.GetInviteLinkByIDRow) InviteLinkResponse {
	status, reason := inviteLinkStatus(row.Status, row.ExpiresAt, row.RevokedAt, row.UsedCount, row.MaxUses)
	return InviteLinkResponse{
		ID:            uuidToString(row.ID),
		WorkspaceID:   uuidToString(row.WorkspaceID),
		WorkspaceName: row.WorkspaceName,
		InviterID:     uuidToString(row.InviterID),
		InviterName:   row.InviterName,
		InviterEmail:  row.InviterEmail,
		Role:          row.Role,
		Status:        status,
		Error:         reason,
		CreatedAt:     timestampToString(row.CreatedAt),
		UpdatedAt:     timestampToString(row.UpdatedAt),
		ExpiresAt:     timestampToString(row.ExpiresAt),
		MaxUses:       row.MaxUses,
		UsedCount:     row.UsedCount,
		RevokedAt:     timestampToPtr(row.RevokedAt),
		LastUsedAt:    timestampToPtr(row.LastUsedAt),
		InviteURL:     "/invite/" + uuidToString(row.ID),
	}
}

func inviteLinkListRowToResponse(row db.ListInviteLinksByWorkspaceRow) InviteLinkResponse {
	status, reason := inviteLinkStatus(row.Status, row.ExpiresAt, row.RevokedAt, row.UsedCount, row.MaxUses)
	token := textValue(row.TokenHash)
	inviteURL := "/invite/" + uuidToString(row.ID)
	if token != "" {
		inviteURL = "/invite/" + token
	}
	return InviteLinkResponse{
		ID:           uuidToString(row.ID),
		WorkspaceID:  uuidToString(row.WorkspaceID),
		InviterID:    uuidToString(row.InviterID),
		InviterName:  row.InviterName,
		InviterEmail: row.InviterEmail,
		Role:         row.Role,
		Status:       status,
		Error:        reason,
		CreatedAt:    timestampToString(row.CreatedAt),
		UpdatedAt:    timestampToString(row.UpdatedAt),
		ExpiresAt:    timestampToString(row.ExpiresAt),
		MaxUses:      row.MaxUses,
		UsedCount:    row.UsedCount,
		RevokedAt:    timestampToPtr(row.RevokedAt),
		LastUsedAt:   timestampToPtr(row.LastUsedAt),
		Token:        token,
		InviteURL:    inviteURL,
	}
}

func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func generateInviteToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func inviteTokenLookupValues(token string) []pgtype.Text {
	hashed := hashInviteToken(token)
	if token == hashed {
		return []pgtype.Text{strToText(token)}
	}
	return []pgtype.Text{strToText(hashed), strToText(token)}
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

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
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
		InviteeEmail: strToText(email),
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
		InviteeEmail:  strToText(email),
		InviteeUserID: inviteeUserID,
		Role:          role,
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
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  textValue(row.InviteeEmail),
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
			InviterName:   row.InviterName,
			InviterEmail:  row.InviterEmail,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateInviteLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateInviteLinkRequest
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

	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if req.TTLHours > 0 {
		if req.TTLHours > 24*30 {
			writeError(w, http.StatusBadRequest, "ttl_hours must be 720 or less")
			return
		}
		expiresAt = time.Now().Add(time.Duration(req.TTLHours) * time.Hour)
	}
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid expires_at")
			return
		}
		if parsed.Before(time.Now()) {
			writeError(w, http.StatusBadRequest, "expires_at must be in the future")
			return
		}
		expiresAt = parsed
	}

	maxUses := req.MaxUses
	if maxUses == 0 {
		maxUses = 1
	}
	if maxUses < 1 || maxUses > 100 {
		writeError(w, http.StatusBadRequest, "max_uses must be between 1 and 100")
		return
	}

	token, tokenHash, err := generateInviteToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate invite token")
		return
	}

	inv, err := h.Queries.CreateInviteLink(r.Context(), db.CreateInviteLinkParams{
		WorkspaceID:        parseUUID(workspaceID),
		InviterID:          requester.UserID,
		TokenHash:          strToText(tokenHash),
		Role:               role,
		ExpiresAt:          pgtype.Timestamptz{Time: expiresAt, Valid: true},
		MaxUses:            maxUses,
		CreatedByIp:        strToText(strings.Split(r.RemoteAddr, ":")[0]),
		CreatedByUserAgent: strToText(r.UserAgent()),
	})
	if err != nil {
		slog.Warn("create invite link failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "role", role)...)
		writeError(w, http.StatusInternalServerError, "failed to create invite link")
		return
	}

	resp := inviteLinkToResponse(inv, token)
	resp.InviteURL = "/invite/" + token
	h.Analytics.Capture(analytics.TeamInviteSent(uuidToString(requester.UserID), workspaceID, "", "link"))

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListInviteLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	rows, err := h.Queries.ListInviteLinksByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invite links")
		return
	}

	resp := make([]InviteLinkResponse, len(rows))
	for i, row := range rows {
		resp[i] = inviteLinkListRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RevokeInviteLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	invitationID := chi.URLParam(r, "invitationId")

	if err := h.Queries.DeleteInviteLink(r.Context(), db.DeleteInviteLinkParams{
		ID:          parseUUID(invitationID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete invite link")
		return
	}

	slog.Info("invite link deleted", "invitation_id", invitationID, "workspace_id", workspaceID)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getInviteLinkByToken(r *http.Request, token string) (db.GetInviteLinkByTokenHashRow, error) {
	var lastErr error
	for _, lookupValue := range inviteTokenLookupValues(token) {
		row, err := h.Queries.GetInviteLinkByTokenHash(r.Context(), lookupValue)
		if err == nil {
			return row, nil
		}
		lastErr = err
		if !isNotFound(err) {
			break
		}
	}
	return db.GetInviteLinkByTokenHashRow{}, lastErr
}

func (h *Handler) getInviteLinkByID(r *http.Request, token string) (db.GetInviteLinkByIDRow, error) {
	id := parseUUID(token)
	if !id.Valid {
		return db.GetInviteLinkByIDRow{}, errors.New("invalid invite link id")
	}
	return h.Queries.GetInviteLinkByID(r.Context(), id)
}

func (h *Handler) getInviteLinkByTokenForUpdate(qtx *db.Queries, r *http.Request, token string) (db.WorkspaceInvitation, error) {
	var lastErr error
	for _, lookupValue := range inviteTokenLookupValues(token) {
		inv, err := qtx.GetInviteLinkByTokenHashForUpdate(r.Context(), lookupValue)
		if err == nil {
			return inv, nil
		}
		lastErr = err
		if !isNotFound(err) {
			break
		}
	}
	if id := parseUUID(token); id.Valid {
		inv, err := qtx.GetInviteLinkByIDForUpdate(r.Context(), id)
		if err == nil {
			return inv, nil
		}
		lastErr = err
	}
	return db.WorkspaceInvitation{}, lastErr
}

func (h *Handler) ValidateInviteLink(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		writeError(w, http.StatusNotFound, "invite link not found")
		return
	}

	row, err := h.getInviteLinkByToken(r, token)
	if err != nil {
		idRow, idErr := h.getInviteLinkByID(r, token)
		if idErr != nil {
			writeError(w, http.StatusNotFound, "invite link not found")
			return
		}
		writeJSON(w, http.StatusOK, inviteLinkIDRowToResponse(idRow))
		return
	}

	writeJSON(w, http.StatusOK, inviteLinkRowToResponse(row))
}

func (h *Handler) AcceptInviteLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	token := strings.TrimSpace(chi.URLParam(r, "token"))
	if token == "" {
		writeError(w, http.StatusNotFound, "invite link not found")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invite link")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	inv, err := h.getInviteLinkByTokenForUpdate(qtx, r, token)
	if err != nil {
		writeError(w, http.StatusNotFound, "invite link not found")
		return
	}

	if inv.RevokedAt.Valid {
		writeError(w, http.StatusGone, "invite link has been revoked")
		return
	}
	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invite link has expired")
		return
	}
	if inv.Status != "pending" || inv.UsedCount >= inv.MaxUses {
		writeError(w, http.StatusGone, "invite link has been used up")
		return
	}

	if existing, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: inv.WorkspaceID,
	}); err == nil {
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to accept invite link")
			return
		}
		writeJSON(w, http.StatusOK, memberWithUserResponse(existing, user))
		return
	}

	consumed, err := qtx.ConsumeInviteLink(r.Context(), inv.ID)
	if err != nil {
		writeError(w, http.StatusGone, "invite link has been used up")
		return
	}
	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: consumed.WorkspaceID,
		UserID:      user.ID,
		Role:        consumed.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			existing, existingErr := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      user.ID,
				WorkspaceID: consumed.WorkspaceID,
			})
			if existingErr == nil {
				if err := tx.Commit(r.Context()); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to accept invite link")
					return
				}
				writeJSON(w, http.StatusOK, memberWithUserResponse(existing, user))
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invite link")
		return
	}

	wsID := uuidToString(consumed.WorkspaceID)
	memberResp := memberWithUserResponse(member, user)
	eventPayload := map[string]any{"member": memberResp}
	if ws, err := h.Queries.GetWorkspace(r.Context(), consumed.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, wsID, "member", userID, eventPayload)
	h.publish(protocol.EventInvitationAccepted, wsID, "member", userID, map[string]any{
		"invitation_id": uuidToString(consumed.ID),
		"member":        memberResp,
	})

	var daysSinceInvite int64
	if consumed.CreatedAt.Valid {
		daysSinceInvite = int64(time.Since(consumed.CreatedAt.Time).Hours() / 24)
	}
	h.Analytics.Capture(analytics.TeamInviteAccepted(userID, wsID, daysSinceInvite))

	writeJSON(w, http.StatusOK, memberResp)
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
		"invitee_email":   textValue(inv.InviteeEmail),
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

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != textValue(inv.InviteeEmail) && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
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
		InviteeEmail:  strToText(user.Email),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}

	resp := make([]InvitationResponse, len(rows))
	for i, row := range rows {
		resp[i] = InvitationResponse{
			ID:            uuidToString(row.ID),
			WorkspaceID:   uuidToString(row.WorkspaceID),
			InviterID:     uuidToString(row.InviterID),
			InviteeEmail:  textValue(row.InviteeEmail),
			InviteeUserID: uuidToPtr(row.InviteeUserID),
			Role:          row.Role,
			Status:        row.Status,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
			ExpiresAt:     timestampToString(row.ExpiresAt),
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

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != textValue(inv.InviteeEmail) && uuidToString(inv.InviteeUserID) != userID {
		writeError(w, http.StatusForbidden, "invitation does not belong to you")
		return
	}

	if inv.Status != "pending" {
		writeError(w, http.StatusBadRequest, "invitation is not pending")
		return
	}

	// Check expiry.
	if inv.ExpiresAt.Valid && inv.ExpiresAt.Time.Before(time.Now()) {
		writeError(w, http.StatusGone, "invitation has expired")
		return
	}

	// Use a transaction: mark accepted + create member atomically.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	accepted, err := qtx.AcceptInvitation(r.Context(), inv.ID)
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

	slog.Info("invitation accepted", "invitation_id", invitationID, "user_id", userID, "workspace_id", uuidToString(accepted.WorkspaceID))

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

	// Verify the invitation belongs to the current user.
	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if strings.ToLower(user.Email) != textValue(inv.InviteeEmail) && uuidToString(inv.InviteeUserID) != userID {
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
		"invitee_email": textValue(declined.InviteeEmail),
	})

	w.WriteHeader(http.StatusNoContent)
}
