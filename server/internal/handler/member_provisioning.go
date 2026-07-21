package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const maxProvisionMembersPerRequest = 100

const (
	ProvisionMemberStatusCreated       = "created"
	ProvisionMemberStatusAlreadyMember = "already_member"
	ProvisionMemberStatusDuplicate     = "duplicate"
	ProvisionMemberStatusInvalid       = "invalid"
	ProvisionMemberStatusFailed        = "failed"
)

type ProvisionMemberEntry struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type ProvisionMembersRequest struct {
	Entries []ProvisionMemberEntry `json:"entries"`
}

type ProvisionMemberResult struct {
	Email    string `json:"email"`
	Role     string `json:"role,omitempty"`
	Status   string `json:"status"`
	UserID   string `json:"user_id,omitempty"`
	MemberID string `json:"member_id,omitempty"`
	Error    string `json:"error,omitempty"`
}

type ProvisionMembersSummary struct {
	Total         int `json:"total"`
	Created       int `json:"created"`
	AlreadyMember int `json:"already_member"`
	Duplicate     int `json:"duplicate"`
	Invalid       int `json:"invalid"`
	Failed        int `json:"failed"`
}

type ProvisionMembersResponse struct {
	Summary ProvisionMembersSummary `json:"summary"`
	Results []ProvisionMemberResult `json:"results"`
}

type provisionedMember struct {
	user              db.User
	member            db.Member
	status            string
	revokedInvitation string
}

// ProvisionMembers pre-creates users and workspace memberships for a bounded
// seed cohort. It intentionally sends no invitation email. The release flag
// and owner check make that stronger-than-invite behavior explicit.
func (h *Handler) ProvisionMembers(w http.ResponseWriter, r *http.Request) {
	if !featureflags.BulkMemberProvisioningEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "only workspace owners can provision members")
		return
	}

	var req ProvisionMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Entries) == 0 {
		writeError(w, http.StatusBadRequest, "entries are required")
		return
	}
	if len(req.Entries) > maxProvisionMembersPerRequest {
		writeError(w, http.StatusBadRequest, "too many entries (maximum 100)")
		return
	}

	resp := ProvisionMembersResponse{
		Summary: ProvisionMembersSummary{Total: len(req.Entries)},
		Results: make([]ProvisionMemberResult, 0, len(req.Entries)),
	}
	seen := make(map[string]struct{}, len(req.Entries))
	actorID := requestUserID(r)

	for _, entry := range req.Entries {
		email, emailErr := normalizeProvisioningEmail(entry.Email)
		role, roleOK := normalizeProvisioningRole(entry.Role)
		result := ProvisionMemberResult{Email: email, Role: role}
		if emailErr != nil {
			result.Email = strings.ToLower(strings.TrimSpace(entry.Email))
			result.Status = ProvisionMemberStatusInvalid
			result.Error = emailErr.Error()
			resp.Summary.Invalid++
			resp.Results = append(resp.Results, result)
			continue
		}
		if !roleOK {
			result.Status = ProvisionMemberStatusInvalid
			result.Error = "role must be member or admin"
			resp.Summary.Invalid++
			resp.Results = append(resp.Results, result)
			continue
		}
		if _, duplicate := seen[email]; duplicate {
			result.Status = ProvisionMemberStatusDuplicate
			resp.Summary.Duplicate++
			resp.Results = append(resp.Results, result)
			continue
		}
		seen[email] = struct{}{}

		provisioned, err := h.provisionWorkspaceMember(r, requester.WorkspaceID, email, role)
		if err != nil {
			result.Status = ProvisionMemberStatusFailed
			result.Error = provisionMemberPublicError(err)
			resp.Summary.Failed++
			resp.Results = append(resp.Results, result)
			slog.Warn("provision workspace member failed", "workspace_id", workspaceID, "email", email, "error", err)
			continue
		}

		result.Status = provisioned.status
		result.UserID = uuidToString(provisioned.user.ID)
		result.MemberID = uuidToString(provisioned.member.ID)
		if provisioned.status == ProvisionMemberStatusCreated {
			resp.Summary.Created++
			memberResp := memberWithUserResponse(provisioned.member, provisioned.user)
			eventPayload := map[string]any{"member": memberResp}
			if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
				eventPayload["workspace_name"] = ws.Name
			}
			h.publish(protocol.EventMemberAdded, workspaceID, "member", actorID, eventPayload)
			h.notifyDaemonWorkspacesChanged(result.UserID)
		} else {
			resp.Summary.AlreadyMember++
		}
		if provisioned.revokedInvitation != "" {
			h.publish(protocol.EventInvitationRevoked, workspaceID, "member", actorID, map[string]any{
				"invitation_id": provisioned.revokedInvitation,
			})
		}
		resp.Results = append(resp.Results, result)
	}

	writeJSON(w, http.StatusOK, resp)
}

func normalizeProvisioningEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", errors.New("email is required")
	}
	if len(email) > 254 {
		return "", errors.New("email is too long")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", errors.New("email is invalid")
	}
	return email, nil
}

func normalizeProvisioningRole(raw string) (string, bool) {
	role := strings.ToLower(strings.TrimSpace(raw))
	if role == "" {
		role = "member"
	}
	return role, role == "member" || role == "admin"
}

func (h *Handler) provisionWorkspaceMember(r *http.Request, workspaceID pgtype.UUID, email, role string) (provisionedMember, error) {
	provisioned, err := h.provisionWorkspaceMemberOnce(r, workspaceID, email, role)
	if isUniqueViolation(err) {
		// A concurrent sign-in or provisioning request may create the global
		// user or workspace membership after our initial lookup. The failed
		// transaction has already rolled back, so one fresh attempt observes
		// the winning row and returns the normal already_member outcome.
		return h.provisionWorkspaceMemberOnce(r, workspaceID, email, role)
	}
	return provisioned, err
}

func (h *Handler) provisionWorkspaceMemberOnce(r *http.Request, workspaceID pgtype.UUID, email, role string) (provisionedMember, error) {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return provisionedMember{}, err
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	user, err := qtx.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return provisionedMember{}, err
		}
		if !h.provisioningEmailAllowed(email) {
			return provisionedMember{}, errProvisioningEmailNotAllowed
		}
		name := email
		if at := strings.IndexByte(email, '@'); at > 0 {
			name = email[:at]
		}
		user, err = qtx.CreateUser(r.Context(), db.CreateUserParams{Name: name, Email: email})
		if err != nil {
			return provisionedMember{}, err
		}
	}

	member, memberErr := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		WorkspaceID: workspaceID,
		UserID:      user.ID,
	})
	status := ProvisionMemberStatusAlreadyMember
	if memberErr != nil {
		if !errors.Is(memberErr, pgx.ErrNoRows) {
			return provisionedMember{}, memberErr
		}
		member, err = qtx.CreateMember(r.Context(), db.CreateMemberParams{
			WorkspaceID: workspaceID,
			UserID:      user.ID,
			Role:        role,
		})
		if err != nil {
			return provisionedMember{}, err
		}
		status = ProvisionMemberStatusCreated
	}

	user, err = qtx.MarkUserOnboarded(r.Context(), user.ID)
	if err != nil {
		return provisionedMember{}, err
	}

	revokedInvitation := ""
	invitation, err := qtx.GetPendingInvitationByEmail(r.Context(), db.GetPendingInvitationByEmailParams{
		WorkspaceID:  workspaceID,
		InviteeEmail: email,
	})
	if err == nil {
		if err := qtx.RevokeInvitation(r.Context(), invitation.ID); err != nil {
			return provisionedMember{}, err
		}
		revokedInvitation = uuidToString(invitation.ID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return provisionedMember{}, err
	}

	if err := tx.Commit(r.Context()); err != nil {
		return provisionedMember{}, err
	}
	return provisionedMember{user: user, member: member, status: status, revokedInvitation: revokedInvitation}, nil
}

var errProvisioningEmailNotAllowed = errors.New("email is not allowed by this deployment")

func (h *Handler) provisioningEmailAllowed(email string) bool {
	if len(h.cfg.AllowedEmails) == 0 && len(h.cfg.AllowedEmailDomains) == 0 {
		return true
	}
	if contains(h.cfg.AllowedEmails, email) {
		return true
	}
	domain := ""
	if at := strings.LastIndexByte(email, '@'); at >= 0 {
		domain = email[at+1:]
	}
	return contains(h.cfg.AllowedEmailDomains, domain)
}

func provisionMemberPublicError(err error) string {
	if errors.Is(err, errProvisioningEmailNotAllowed) {
		return err.Error()
	}
	return "failed to provision member"
}
