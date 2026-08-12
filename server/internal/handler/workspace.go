package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)
var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]`)
var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var issuePrefixPattern = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// defaultIssuePrefixFromSlug derives a new workspace's default issue prefix
// from its slug: alphanumerics only, first 4 chars, uppercased.
// Examples: "acme" → "ACME", "front-end" → "FRON", "team-2" → "TEAM".
//
// The slug — not the name — is the derivation source on purpose (MUL-6050).
// Name is the one field in the create flow that accepts non-ASCII, so deriving
// an ASCII-only prefix from it forced every CJK/emoji-named workspace onto the
// same "WS" fallback. Slug is validated as `^[a-z0-9]+(-[a-z0-9]+)*$` and
// required, so it always yields a non-empty, workspace-specific prefix.
//
// Kept byte-for-byte in sync with the client-side preview in
// packages/views/onboarding/steps/step-workspace.tsx — what the user sees on
// the create screen has to be what the server would derive.
func defaultIssuePrefixFromSlug(slug string) string {
	head := nonAlphanumeric.ReplaceAllString(slug, "")
	if head == "" {
		// Unreachable for a validated slug; keeps the function total so a
		// future caller can never persist an empty prefix.
		return "WS"
	}
	if len(head) > 4 {
		head = head[:4]
	}
	return strings.ToUpper(head)
}

// legacyIssuePrefixFromName is the pre-MUL-6050 derivation: first 3 ASCII
// letters of the workspace name, uppercased, "WS" when the name has none.
//
// FROZEN — do not "fix" this to match defaultIssuePrefixFromSlug. Its only
// caller is getIssuePrefix's fallback for workspaces whose stored prefix is
// empty (rows predating the column). Issue identifiers are computed at read
// time from the current prefix, so changing what this returns would silently
// rewrite the identifier of every historical issue in those workspaces.
// Deliberate product decision: no backfill, existing workspaces stay as they
// are; only newly created ones follow the slug-derived rule.
func legacyIssuePrefixFromName(name string) string {
	letters := nonAlpha.ReplaceAllString(name, "")
	if len(letters) == 0 {
		return "WS"
	}
	letters = strings.ToUpper(letters)
	if len(letters) > 3 {
		letters = letters[:3]
	}
	return letters
}

// normalizeIssuePrefix trims and uppercases a client-supplied issue prefix.
// An all-whitespace value means "not provided" and reports ("", true) so the
// caller can fall back to its default. Anything else must be 1-10 chars of
// A-Z0-9, mirroring the client-side guardrail in
// packages/views/settings/components/workspace-tab.tsx.
func normalizeIssuePrefix(raw string) (string, bool) {
	prefix := strings.ToUpper(strings.TrimSpace(raw))
	if prefix == "" {
		return "", true
	}
	if !issuePrefixPattern.MatchString(prefix) {
		return "", false
	}
	return prefix, true
}

const issuePrefixFormatError = "issue prefix must be 1-10 uppercase letters or digits"

type WorkspaceResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix string  `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func (h *Handler) workspaceToResponse(w db.Workspace) WorkspaceResponse {
	var settings any
	if w.Settings != nil {
		json.Unmarshal(w.Settings, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	var repos any
	if w.Repos != nil {
		json.Unmarshal(w.Repos, &repos)
	}
	if repos == nil {
		repos = []any{}
	}
	return WorkspaceResponse{
		ID:          uuidToString(w.ID),
		Name:        w.Name,
		Slug:        w.Slug,
		Description: textToPtr(w.Description),
		Context:     textToPtr(w.Context),
		Settings:    settings,
		Repos:       repos,
		IssuePrefix: w.IssuePrefix,
		AvatarURL:   h.resolveAvatarURLPtr(textToPtr(w.AvatarUrl)),
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
	}
}

type MemberResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
}

func memberToResponse(m db.Member) MemberResponse {
	return MemberResponse{
		ID:          uuidToString(m.ID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		UserID:      uuidToString(m.UserID),
		Role:        m.Role,
		CreatedAt:   timestampToString(m.CreatedAt),
	}
}

func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaces, err := h.Queries.ListWorkspaces(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}

	resp := make([]WorkspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = h.workspaceToResponse(ws)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, h.workspaceToResponse(ws))
}

type CreateWorkspaceRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	IssuePrefix *string `json:"issue_prefix"`
}

func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Self-host gate (#3433): when the operator has set
	// DISABLE_WORKSPACE_CREATION=true, no caller — including existing
	// workspace owners — may create additional workspaces. The frontend
	// hides every "Create workspace" affordance via /api/config, but the
	// 403 here is the only authoritative check.
	if h.cfg.DisableWorkspaceCreation {
		writeError(w, http.StatusForbidden, "workspace creation is disabled for this instance")
		return
	}

	var req CreateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if !workspaceSlugPattern.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must contain only lowercase letters, numbers, and hyphens")
		return
	}
	if isReservedSlug(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug is reserved")
		return
	}

	// Resolve the prefix before opening the transaction: an invalid one is a
	// 400 that should never cost a connection.
	issuePrefix := ""
	if req.IssuePrefix != nil {
		prefix, ok := normalizeIssuePrefix(*req.IssuePrefix)
		if !ok {
			writeError(w, http.StatusBadRequest, issuePrefixFormatError)
			return
		}
		issuePrefix = prefix
	}
	if issuePrefix == "" {
		issuePrefix = defaultIssuePrefixFromSlug(req.Slug)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)
	ws, err := qtx.CreateWorkspace(r.Context(), db.CreateWorkspaceParams{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: ptrToText(req.Description),
		Context:     ptrToText(req.Context),
		IssuePrefix: issuePrefix,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create workspace: "+err.Error())
		return
	}

	_, err = qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      parseUUID(userID),
		Role:        "owner",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add owner: "+err.Error())
		return
	}

	// NOTE: CreateWorkspace deliberately does NOT mark the user as
	// onboarded. The `onboarded_at` flag is owned by CompleteOnboarding
	// (Step 3 of the flow) and by AcceptInvitation (invitee joining an
	// existing workspace). This decouples "the user has a workspace"
	// from "the user has finished setup"; the workspace-layer route
	// gate (web layout / desktop App.tsx overlay) redirects un-onboarded
	// users back to /onboarding instead.

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	wsID := uuidToString(ws.ID)

	// "Is this the user's first workspace?" is derived in PostHog by looking
	// at whether they have a prior workspace_created event, not stamped at
	// emit time. Stamping here would race under concurrent creates without
	// a schema change, and the event stream answers the question exactly.
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.WorkspaceCreated(userID, wsID))
	h.notifyDaemonWorkspacesChanged(userID)

	slog.Info("workspace created", append(logger.RequestAttrs(r), "workspace_id", wsID, "name", ws.Name, "slug", ws.Slug)...)
	writeJSON(w, http.StatusCreated, h.workspaceToResponse(ws))
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix *string `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
}

type workspaceRepoRef struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

func validateAndNormalizeWorkspaceRepos(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var repos []workspaceRepoRef
	if err := json.Unmarshal(raw, &repos); err != nil {
		return nil, fmt.Errorf("repos must be an array of repository objects: %w", err)
	}

	normalized := make([]workspaceRepoRef, 0, len(repos))
	seen := make(map[string]struct{}, len(repos))
	for i, repo := range repos {
		repo.URL = strings.TrimSpace(repo.URL)
		repo.Description = strings.TrimSpace(repo.Description)
		if repo.URL == "" {
			return nil, fmt.Errorf("repos[%d]: url is required", i)
		}
		if !isValidGitRepoURL(repo.URL) {
			return nil, fmt.Errorf("repos[%d]: url must be a valid http(s) or ssh git URL", i)
		}
		if _, ok := seen[repo.URL]; ok {
			continue
		}
		seen[repo.URL] = struct{}{}
		normalized = append(normalized, repo)
	}

	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	var req UpdateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWorkspaceParams{
		ID: idUUID,
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Context != nil {
		params.Context = pgtype.Text{String: *req.Context, Valid: true}
	}
	if req.Settings != nil {
		s, _ := json.Marshal(req.Settings)
		params.Settings = s
	}
	if req.Repos != nil {
		reposJSON, err := validateAndNormalizeWorkspaceRepos(req.Repos)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Repos = reposJSON
	}
	if req.IssuePrefix != nil {
		prefix, ok := normalizeIssuePrefix(*req.IssuePrefix)
		if !ok {
			writeError(w, http.StatusBadRequest, issuePrefixFormatError)
			return
		}
		if prefix != "" {
			params.IssuePrefix = pgtype.Text{String: prefix, Valid: true}
		}
	}
	if req.AvatarURL != nil {
		// Read the stored value so an unchanged re-send skips revalidation —
		// this handler is the one avatar writer that doesn't already have the
		// row in hand.
		var current string
		if existing, err := h.Queries.GetWorkspace(r.Context(), idUUID); err == nil {
			current = existing.AvatarUrl.String
		}
		accepted, ok := h.acceptAvatarURL(w, r, *req.AvatarURL, current)
		if !ok {
			return
		}
		params.AvatarUrl = pgtype.Text{String: accepted, Valid: true}
	}

	ws, err := h.Queries.UpdateWorkspace(r.Context(), params)
	if err != nil {
		slog.Warn("update workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace: "+err.Error())
		return
	}

	slog.Info("workspace updated", append(logger.RequestAttrs(r), "workspace_id", id)...)
	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(ws.ID), "member", userID, map[string]any{"workspace": h.workspaceToResponse(ws)})
	if req.Name != nil {
		if members, err := h.Queries.ListMembers(r.Context(), ws.ID); err == nil {
			userIDs := make([]string, 0, len(members))
			for _, member := range members {
				userIDs = append(userIDs, uuidToString(member.UserID))
			}
			h.notifyDaemonWorkspacesChanged(userIDs...)
		}
	}

	writeJSON(w, http.StatusOK, h.workspaceToResponse(ws))
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]MemberResponse, len(members))
	for i, m := range members {
		resp[i] = memberToResponse(m)
	}

	writeJSON(w, http.StatusOK, resp)
}

type MemberWithUserResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	Role        string  `json:"role"`
	CreatedAt   string  `json:"created_at"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
}

func (h *Handler) ListMembersWithUser(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	members, err := h.Queries.ListMembersWithUser(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}

	resp := make([]MemberWithUserResponse, len(members))
	for i, m := range members {
		resp[i] = MemberWithUserResponse{
			ID:          uuidToString(m.ID),
			WorkspaceID: uuidToString(m.WorkspaceID),
			UserID:      uuidToString(m.UserID),
			Role:        m.Role,
			CreatedAt:   timestampToString(m.CreatedAt),
			Name:        m.UserName,
			Email:       m.UserEmail,
			AvatarURL:   h.resolveAvatarURLPtr(textToPtr(m.UserAvatarUrl)),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) memberWithUserResponse(member db.Member, user db.User) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:          uuidToString(member.ID),
		WorkspaceID: uuidToString(member.WorkspaceID),
		UserID:      uuidToString(member.UserID),
		Role:        member.Role,
		CreatedAt:   timestampToString(member.CreatedAt),
		Name:        user.Name,
		Email:       user.Email,
		AvatarURL:   h.resolveAvatarURLPtr(textToPtr(user.AvatarUrl)),
	}
}

func normalizeMemberRole(role string) (string, bool) {
	if role == "" {
		return "member", true
	}

	role = strings.TrimSpace(role)
	switch role {
	case "owner", "admin", "member":
		return role, true
	default:
		return "", false
	}
}

func (h *Handler) CreateMember(w http.ResponseWriter, r *http.Request) {
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
	if role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	user, err := h.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			// Auto-create user with email so they can be invited before signing up
			user, err = h.Queries.CreateUser(r.Context(), db.CreateUserParams{
				Name:  email,
				Email: email,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create user")
				return
			}
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
	}

	member, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: requester.WorkspaceID,
		UserID:      user.ID,
		Role:        role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "user is already a member")
			return
		}
		slog.Warn("create member failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "email", email)...)
		writeError(w, http.StatusInternalServerError, "failed to create member")
		return
	}

	slog.Info("member added", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "email", email, "role", role)...)
	userID := requestUserID(r)
	eventPayload := map[string]any{"member": h.memberWithUserResponse(member, user)}
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)
	h.notifyDaemonWorkspacesChanged(uuidToString(user.ID))

	writeJSON(w, http.StatusCreated, h.memberWithUserResponse(member, user))
}

type UpdateMemberRequest struct {
	Role string `json:"role"`
}

func (h *Handler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	var req UpdateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Role) == "" {
		writeError(w, http.StatusBadRequest, "role is required")
		return
	}

	role, valid := normalizeMemberRole(req.Role)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid member role")
		return
	}

	if (target.Role == "owner" || role == "owner") && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" && role != "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	updatedMember, err := h.Queries.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{
		ID:   target.ID,
		Role: role,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update member")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(target.UserID), workspaceID)

	user, err := h.Queries.GetUser(r.Context(), updatedMember.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load member")
		return
	}

	userID := requestUserID(r)
	h.publish(protocol.EventMemberUpdated, uuidToString(requester.WorkspaceID), "member", userID, map[string]any{
		"member": h.memberWithUserResponse(updatedMember, user),
	})

	writeJSON(w, http.StatusOK, h.memberWithUserResponse(updatedMember, user))
}

func (h *Handler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	memberID := chi.URLParam(r, "memberId")
	memberUUID, ok := parseUUIDOrBadRequest(w, memberID, "member id")
	if !ok {
		return
	}
	target, err := h.Queries.GetMember(r.Context(), memberUUID)
	if err != nil || uuidToString(target.WorkspaceID) != uuidToString(requester.WorkspaceID) {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	if target.Role == "owner" && requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if target.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), target.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete member")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	requesterUserID := requestUserID(r)
	result, err := h.revokeAndRemoveMember(r.Context(), target.WorkspaceID, target.UserID, target.ID, parseUUID(requesterUserID))
	if err != nil {
		slog.Warn("delete member failed", append(logger.RequestAttrs(r), "error", err, "member_id", memberID, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete member")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(target.UserID), workspaceID)

	wsIDStr := uuidToString(requester.WorkspaceID)
	logRevocation(result, wsIDStr, uuidToString(target.UserID))
	h.publishRevocation(r.Context(), result, wsIDStr, "member", requesterUserID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(target.ID), "workspace_id", workspaceID, "user_id", uuidToString(target.UserID))...)
	h.publish(protocol.EventMemberRemoved, wsIDStr, "member", requesterUserID, map[string]any{
		"member_id":    uuidToString(target.ID),
		"workspace_id": wsIDStr,
		"user_id":      uuidToString(target.UserID),
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(target.UserID))

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) LeaveWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	if member.Role == "owner" {
		members, err := h.Queries.ListMembers(r.Context(), member.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to leave workspace")
			return
		}
		if countOwners(members) <= 1 {
			writeError(w, http.StatusBadRequest, "workspace must have at least one owner")
			return
		}
	}

	result, err := h.revokeAndRemoveMember(r.Context(), member.WorkspaceID, member.UserID, member.ID, member.UserID)
	if err != nil {
		slog.Warn("leave workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to leave workspace")
		return
	}

	h.MembershipCache.Invalidate(r.Context(), uuidToString(member.UserID), workspaceID)

	userID := requestUserID(r)
	logRevocation(result, workspaceID, uuidToString(member.UserID))
	h.publishRevocation(r.Context(), result, workspaceID, "member", userID)

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "user_id", uuidToString(member.UserID))...)
	h.publish(protocol.EventMemberRemoved, workspaceID, "member", userID, map[string]any{
		"member_id":    uuidToString(member.ID),
		"workspace_id": workspaceID,
		"user_id":      uuidToString(member.UserID),
	})
	h.notifyDaemonWorkspacesChanged(uuidToString(member.UserID))

	w.WriteHeader(http.StatusNoContent)
}

// workspaceDeleteLockTimeout bounds every lock wait inside the workspace
// teardown transaction.
//
// The teardown waits on three kinds of lock: the workspace row (FOR UPDATE),
// every chat_session row in the workspace (FOR UPDATE), and the global
// advisory lock 4246 that serialises it against the task_usage_hourly rollup.
// The advisory lock is the dangerous one — it is global and batch-held. A
// rollup tick may hold it for as long as its 25 min scheduler RunTimeout, the
// backfill commands (cmd/backfill_task_usage_hourly, cmd/backfill_codex_usage_cache)
// hold it for an entire run, and before migration 272 a tick whose query was
// cancelled leaked it for the remaining life of its pooled connection.
//
// Waiting on that lock without a cap is what the user sees as "delete does
// nothing": the request never returns, and no layer above it times out — the
// browser/Electron fetch in packages/core/api/client.ts has no deadline and
// the delete dialog stays in its "Deleting…" state forever (MUL-5983). A
// bounded wait turns the same contention into a retryable error.
//
// 10 s is far above the millisecond-scale waits an uncontended teardown sees,
// and short enough to fail while the user is still watching the dialog.
const workspaceDeleteLockTimeout = 10 * time.Second

// workspaceDeleteLockTimeoutOverride, when non-zero, replaces
// workspaceDeleteLockTimeout for the duration of a test. Never read outside
// effectiveWorkspaceDeleteLockTimeout — see workspace_delete_lock_test.go.
var workspaceDeleteLockTimeoutOverride time.Duration

func effectiveWorkspaceDeleteLockTimeout() time.Duration {
	if workspaceDeleteLockTimeoutOverride > 0 {
		return workspaceDeleteLockTimeoutOverride
	}
	return workspaceDeleteLockTimeout
}

// isRetryableLockFailure reports whether err is one of the two transient lock
// outcomes a teardown can hit: lock_not_available (55P03), which is what
// `SET LOCAL lock_timeout` raises when a wait exceeds its budget, and
// deadlock_detected (40P01), which the owner-row locks can hit against a
// concurrent task update that takes the same locks in the opposite order (task
// row first, then its owner). Neither says anything about the workspace itself:
// the transaction rolled back untouched and the caller can retry.
func isRetryableLockFailure(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "55P03" || pgErr.Code == "40P01"
	}
	return false
}

// failWorkspaceDelete logs a failed teardown step and writes the response for
// it. A lock timeout or deadlock is transient and retryable, so it answers 503
// with a message the delete dialog can show; every other failure stays a 500.
func failWorkspaceDelete(w http.ResponseWriter, r *http.Request, workspaceID, step string, err error) {
	attrs := append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "step", step)
	if isRetryableLockFailure(err) {
		slog.Warn("workspace delete blocked by lock contention", attrs...)
		writeError(w, http.StatusServiceUnavailable, "workspace deletion is temporarily blocked by another operation, please try again")
		return
	}
	slog.Warn("workspace delete step failed", attrs...)
	writeError(w, http.StatusInternalServerError, "failed to delete workspace")
}

// workspaceDeleteTaskPageSize bounds one task page: the ids held in this process,
// the array parameter sent back to Postgres, and the row count of a single
// DELETE. A whole workspace's task set is not bounded by anything — one busy
// agent can own millions of historical rows — so it must never be materialized at
// once (MUL-5999 review).
const workspaceDeleteTaskPageSize = 1000

// workspaceDeleteOwnerPageSize bounds owner enumeration the same way. A workspace
// with a very large agent or issue set must not have its whole id list held here
// either.
const workspaceDeleteOwnerPageSize = 500

// workspaceDeleteVerifyPasses caps how many times a single owner may be swept.
//
// Pass 1 removes what is there; pass 2 confirms the owner came back empty. This is
// a cheap self-check, NOT the concurrency guarantee: a task inserted after the
// final empty scan and before COMMIT would not be caught by any number of passes.
// What closes that window is the in-transaction fence on task writes
// (migration 284) plus the workspace row lock teardown already holds. The passes
// exist so that anything unexpected — a path that somehow bypassed the fence — is
// either cleaned up or turns into a failed, rolled-back teardown rather than a
// tenant with rows left behind.
const workspaceDeleteVerifyPasses = 3

// deleteWorkspaceTasks removes every agent_task_queue row a workspace owns,
// together with the rows that hang off those tasks, in bounded keyset pages.
//
// Shape, and why:
//
//   - One owner key per query. agent_task_queue has no workspace_id and all three
//     ownership paths must stay (nothing enforces that a task's agent, runtime and
//     issue share a workspace), but combining them in one statement — as the
//     previous three-way OR, as a UNION of joins, or as `= ANY(array)` over a
//     whole workspace's agents — leaves the planner estimating a proportional
//     share of the global table and choosing a Seq Scan.
//   - Keyset pages, so the SCAN is bounded and not just the result. Each page is
//     an index range scan that resumes where the previous one stopped. Bounding
//     only the result (`ORDER BY id LIMIT n` with no id in the index) puts a Sort
//     over the owner's whole task set in front of every page, which costs roughly
//     O(N² / page) for a busy owner.
//   - Row locks, not a read snapshot. Each page takes FOR UPDATE, so under READ
//     COMMITTED Postgres re-checks ownership after acquiring each lock: a task a
//     concurrent commit already moved elsewhere is skipped rather than deleted on
//     a stale claim, and a task we locked cannot move before we delete it.
//   - Children first, in the application layer. DetachTaskBatchReferences breaks
//     inbound references and DeleteTaskBatch removes task_usage, task_message,
//     task_token, the two outbound card tables and chat_draft_restore before the
//     task rows themselves. The legacy FK cascades stay a safety net only.
//
// Late arrivals are handled by the fence, not here: migration 284 makes every task
// write take FOR KEY SHARE on its owners' workspace rows, which conflicts with the
// FOR UPDATE that LockWorkspaceForDelete holds for this whole transaction.
func deleteWorkspaceTasks(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID) error {
	ownerKinds := []struct {
		label          string
		ownerFirstPage func(context.Context, int32) ([]pgtype.UUID, error)
		ownerPage      func(context.Context, pgtype.UUID, int32) ([]pgtype.UUID, error)
		taskFirstPage  func(context.Context, pgtype.UUID, int32) ([]pgtype.UUID, error)
		taskPage       func(context.Context, pgtype.UUID, pgtype.UUID, int32) ([]pgtype.UUID, error)
		sweepAgent     bool
	}{
		{
			label: "agent",
			ownerFirstPage: func(ctx context.Context, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceAgentIDFirstPage(ctx, db.ListWorkspaceAgentIDFirstPageParams{
					WorkspaceID: workspaceID, Limit: limit,
				})
			},
			ownerPage: func(ctx context.Context, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceAgentIDPage(ctx, db.ListWorkspaceAgentIDPageParams{
					WorkspaceID: workspaceID, ID: cursor, Limit: limit,
				})
			},
			taskFirstPage: func(ctx context.Context, owner pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByAgentFirstPage(ctx, db.ListTaskIDsByAgentFirstPageParams{
					AgentID: owner, Limit: limit,
				})
			},
			taskPage: func(ctx context.Context, owner, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByAgentPage(ctx, db.ListTaskIDsByAgentPageParams{
					AgentID: owner, ID: cursor, Limit: limit,
				})
			},
			// The third explicit task_token path. workspace_id (leaf data) and
			// task_id (DeleteTaskBatch) leave one shape uncovered: a token whose
			// own workspace_id points at a neighbour while its agent lives here.
			sweepAgent: true,
		},
		{
			label: "issue",
			ownerFirstPage: func(ctx context.Context, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceIssueIDFirstPage(ctx, db.ListWorkspaceIssueIDFirstPageParams{
					WorkspaceID: workspaceID, Limit: limit,
				})
			},
			ownerPage: func(ctx context.Context, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceIssueIDPage(ctx, db.ListWorkspaceIssueIDPageParams{
					WorkspaceID: workspaceID, ID: cursor, Limit: limit,
				})
			},
			taskFirstPage: func(ctx context.Context, owner pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByIssueFirstPage(ctx, db.ListTaskIDsByIssueFirstPageParams{
					IssueID: owner, Limit: limit,
				})
			},
			taskPage: func(ctx context.Context, owner, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByIssuePage(ctx, db.ListTaskIDsByIssuePageParams{
					IssueID: owner, ID: cursor, Limit: limit,
				})
			},
		},
		{
			label: "runtime",
			ownerFirstPage: func(ctx context.Context, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceRuntimeIDFirstPage(ctx, db.ListWorkspaceRuntimeIDFirstPageParams{
					WorkspaceID: workspaceID, Limit: limit,
				})
			},
			ownerPage: func(ctx context.Context, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListWorkspaceRuntimeIDPage(ctx, db.ListWorkspaceRuntimeIDPageParams{
					WorkspaceID: workspaceID, ID: cursor, Limit: limit,
				})
			},
			taskFirstPage: func(ctx context.Context, owner pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByRuntimeFirstPage(ctx, db.ListTaskIDsByRuntimeFirstPageParams{
					RuntimeID: owner, Limit: limit,
				})
			},
			taskPage: func(ctx context.Context, owner, cursor pgtype.UUID, limit int32) ([]pgtype.UUID, error) {
				return qtx.ListTaskIDsByRuntimePage(ctx, db.ListTaskIDsByRuntimePageParams{
					RuntimeID: owner, ID: cursor, Limit: limit,
				})
			},
		},
	}

	for _, kind := range ownerKinds {
		owners, err := kind.ownerFirstPage(ctx, workspaceDeleteOwnerPageSize)
		if err != nil {
			return fmt.Errorf("list workspace %ss: %w", kind.label, err)
		}
		for len(owners) > 0 {
			for _, ownerID := range owners {
				if err := sweepTasksForOwner(ctx, kind.label, ownerID,
					kind.taskFirstPage, kind.taskPage,
					qtx.DetachTaskBatchReferences, qtx.DeleteTaskBatch); err != nil {
					return err
				}
				if kind.sweepAgent {
					if err := qtx.DeleteTaskTokensByAgent(ctx, ownerID); err != nil {
						return fmt.Errorf("delete task tokens by agent: %w", err)
					}
				}
			}
			if owners, err = kind.ownerPage(ctx, owners[len(owners)-1], workspaceDeleteOwnerPageSize); err != nil {
				return fmt.Errorf("list workspace %ss: %w", kind.label, err)
			}
		}
	}

	return nil
}

// sweepTasksForOwner deletes one owner's tasks, then re-verifies that owner.
//
// Each pass walks the owner's tasks by keyset and deletes each page. A pass that
// deletes nothing means the owner is clean. A later pass that still finds tasks
// means something reached agent_task_queue without going through the fence, which
// is logged and retried; exhausting the passes fails the whole teardown rather
// than committing a workspace with tasks left behind.
func sweepTasksForOwner(
	ctx context.Context,
	label string,
	ownerID pgtype.UUID,
	taskFirstPage func(context.Context, pgtype.UUID, int32) ([]pgtype.UUID, error),
	taskPage func(context.Context, pgtype.UUID, pgtype.UUID, int32) ([]pgtype.UUID, error),
	detach func(context.Context, []pgtype.UUID) error,
	deletePage func(context.Context, []pgtype.UUID) error,
) error {
	for pass := 1; pass <= workspaceDeleteVerifyPasses; pass++ {
		deleted := 0
		taskIDs, err := taskFirstPage(ctx, ownerID, workspaceDeleteTaskPageSize)
		if err != nil {
			return fmt.Errorf("list tasks by %s: %w", label, err)
		}
		for len(taskIDs) > 0 {
			if err := detach(ctx, taskIDs); err != nil {
				return fmt.Errorf("detach task page references: %w", err)
			}
			if err := deletePage(ctx, taskIDs); err != nil {
				return fmt.Errorf("delete task page: %w", err)
			}
			deleted += len(taskIDs)
			if taskIDs, err = taskPage(ctx, ownerID, taskIDs[len(taskIDs)-1], workspaceDeleteTaskPageSize); err != nil {
				return fmt.Errorf("list tasks by %s: %w", label, err)
			}
		}
		if deleted == 0 {
			return nil
		}
		if pass > 1 {
			slog.Warn("workspace delete swept tasks that appeared after the owner was fenced",
				"owner_kind", label, "owner_id", uuidToString(ownerID), "pass", pass, "deleted", deleted)
		}
	}
	return fmt.Errorf("tasks kept appearing for %s %s after %d sweep passes; refusing to commit a partial teardown",
		label, uuidToString(ownerID), workspaceDeleteVerifyPasses)
}

// lockWorkspaceTaskOwners locks every row through which a task can belong to this
// workspace.
//
// Defence in depth on top of the real fence. The fence that closes the
// insert-after-sweep window is migration 284's lock_task_owner_rows, a predicate
// every ownership-writing statement calls in its own WHERE clause: it takes
// FOR KEY SHARE on the workspace rows behind the task's owners and then on the
// owner rows themselves, which conflicts with the FOR UPDATE that
// LockWorkspaceForDelete holds for this whole transaction. Called from the writing
// statement, so it is in the writer's transaction by construction, and independent
// of the legacy foreign keys.
//
// These owner-row locks add a second, narrower barrier — they also block
// ownership-changing UPDATEs of tasks that already exist — and cost nothing to
// hold, since teardown deletes these very rows a few steps later. They are taken
// set-based and return no ids, so a workspace with a huge owner set costs no
// memory here.
//
// The wait is bounded by SET LOCAL lock_timeout, and taking these locks can
// deadlock against a concurrent task update that locks in the opposite order
// (task row, then owner); both outcomes surface as a retryable 503.
func lockWorkspaceTaskOwners(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID) error {
	if err := qtx.LockWorkspaceTaskOwnerAgents(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace agents: %w", err)
	}
	if err := qtx.LockWorkspaceTaskOwnerIssues(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace issues: %w", err)
	}
	if err := qtx.LockWorkspaceTaskOwnerRuntimes(ctx, workspaceID); err != nil {
		return fmt.Errorf("lock workspace runtimes: %w", err)
	}
	return nil
}

func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")

	// Defense in depth: the route is already gated by the
	// RequireWorkspaceRoleFromURL("owner") middleware, but we re-check here
	// so that the handler is safe regardless of how it gets wired up
	// (direct calls in tests, future router refactors, etc.).
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Invalidate membership cache for all workspace members before deletion.
	// After CASCADE deletes the member rows, cache entries become harmless
	// orphans (downstream lookups for the deleted workspace will fail), but
	// proactive invalidation prevents any stale-access window up to TTL.
	var affectedUserIDs []string
	if members, err := h.Queries.ListMembers(r.Context(), requester.WorkspaceID); err == nil {
		affectedUserIDs = make([]string, 0, len(members))
		for _, m := range members {
			userID := uuidToString(m.UserID)
			h.MembershipCache.Invalidate(r.Context(), userID, workspaceID)
			affectedUserIDs = append(affectedUserIDs, userID)
		}
	}

	// The teardown runs in one transaction so the chat_session row locks below
	// are still held when DeleteWorkspace sweeps chat_draft_restore. Without
	// them, FinalizeDeferredCancelledChat could commit a restore for one of
	// these sessions after the sweep's snapshot was taken: the session cascades
	// away, the restore has no FK to follow it (MUL-3515) and no reaper, and the
	// user's prompt is stranded forever (#5219). The finalizer takes the same
	// lock before inserting, so it either blocks until the session is gone and
	// skips the insert, or commits first and the sweep sees its row.
	//
	// The workspace row is locked first, because the session locks only cover
	// sessions that already exist: a CreateChatSession committing inside the
	// delete window would otherwise slip in a session nobody locked, and its
	// restore would outlive the cascade the same way. Holding the workspace row
	// FOR UPDATE blocks that insert on its workspace FK (FOR KEY SHARE).
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("begin workspace delete tx failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// SET LOCAL is transaction-scoped, so pgxpool hands this connection back
	// out with the default (unbounded) lock_timeout after COMMIT / ROLLBACK.
	// It caps waiting for a lock, not the teardown work itself — deleting a
	// large workspace is allowed to take as long as it takes.
	lockTimeoutMs := int(effectiveWorkspaceDeleteLockTimeout() / time.Millisecond)
	if _, err := tx.Exec(r.Context(), fmt.Sprintf("SET LOCAL lock_timeout = %d", lockTimeoutMs)); err != nil {
		failWorkspaceDelete(w, r, workspaceID, "set lock timeout", err)
		return
	}

	if _, err := qtx.LockWorkspaceForDelete(r.Context(), requester.WorkspaceID); err != nil {
		failWorkspaceDelete(w, r, workspaceID, "lock workspace", err)
		return
	}

	if _, err := qtx.LockChatSessionsByWorkspace(r.Context(), requester.WorkspaceID); err != nil {
		failWorkspaceDelete(w, r, workspaceID, "lock chat sessions", err)
		return
	}

	// Keep the relationship graph in the application layer. Each step is a
	// set-based delete scoped by workspace_id; the legacy cascades remain only
	// as an expand-phase safety net until a later schema contract.
	ctx := r.Context()
	deleteSteps := []struct {
		name string
		run  func() error
	}{
		{
			name: "set teardown mode",
			run:  func() error { return qtx.SetWorkspaceTeardownMode(ctx) },
		},
		{
			// Fences task enqueue / reassignment for the rest of the
			// transaction. Must run before anything sweeps tasks — see
			// lockWorkspaceTaskOwners.
			name: "lock task owners",
			run:  func() error { return lockWorkspaceTaskOwners(ctx, qtx, requester.WorkspaceID) },
		},
		{
			name: "prepare relationship graph",
			run:  func() error { return qtx.PrepareWorkspaceDeletionLinks(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete chat pins",
			run:  func() error { return qtx.DeleteChatPinnedAgentsByWorkspace(ctx, requester.WorkspaceID) },
		},
		{
			// This is the first stage that touches usage rollups. Keep the
			// global rollup lock out of relationship preparation so unrelated
			// workspaces skip the shortest possible rollup window. This wait
			// is bounded by workspaceDeleteLockTimeout — 4246 is held by
			// batch jobs, so an unbounded wait here is what hangs the delete.
			name: "lock task usage rollup",
			run:  func() error { return qtx.LockTaskUsageRollupForWorkspaceDelete(ctx) },
		},
		{
			// Bounded batches, after the rollup lock because it deletes
			// task_usage. Replaces both the task-keyed arms of the old leaf
			// data statement and the separate whole-workspace task delete.
			name: "delete tasks",
			run:  func() error { return deleteWorkspaceTasks(ctx, qtx, requester.WorkspaceID) },
		},
		{
			name: "delete leaf data",
			run:  func() error { return qtx.DeleteWorkspaceLeafData(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilot runs",
			run:  func() error { return qtx.DeleteWorkspaceAutopilotRuns(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete chat messages",
			run:  func() error { return qtx.DeleteWorkspaceChatMessages(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete communication roots",
			run:  func() error { return qtx.DeleteWorkspaceCommunicationRoots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete comments",
			run:  func() error { return qtx.DeleteWorkspaceComments(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete issue roots",
			run:  func() error { return qtx.DeleteWorkspaceIssueRoots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilot children",
			run:  func() error { return qtx.DeleteWorkspaceAutopilotChildren(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete autopilots",
			run:  func() error { return qtx.DeleteWorkspaceAutopilots(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete pull requests",
			run:  func() error { return qtx.DeleteWorkspacePullRequests(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete integrations",
			run:  func() error { return qtx.DeleteWorkspaceConnections(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete squads and skills",
			run:  func() error { return qtx.DeleteWorkspaceSquadsAndSkills(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete agents",
			run:  func() error { return qtx.DeleteWorkspaceAgents(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete runtimes and projects",
			run:  func() error { return qtx.DeleteWorkspaceRuntimesAndProjects(ctx, requester.WorkspaceID) },
		},
		{
			name: "delete administration data",
			run:  func() error { return qtx.DeleteWorkspaceAdministration(ctx, requester.WorkspaceID) },
		},
		{
			// At this point workspaceMember has resolved → workspaceID is a
			// valid UUID, so reuse the resolved value. The existing final
			// statement also sweeps any expand-phase compatibility leftovers.
			name: "delete workspace",
			run:  func() error { return qtx.DeleteWorkspace(ctx, requester.WorkspaceID) },
		},
	}
	for _, step := range deleteSteps {
		if err := step.run(); err != nil {
			failWorkspaceDelete(w, r, workspaceID, step.name, err)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit workspace delete failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	slog.Info("workspace deleted", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
	h.publish(protocol.EventWorkspaceDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"workspace_id": workspaceID,
	})
	h.notifyDaemonWorkspacesChanged(affectedUserIDs...)

	w.WriteHeader(http.StatusNoContent)
}
