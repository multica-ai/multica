package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)
var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// generateIssuePrefix produces a 2-5 char uppercase prefix from a workspace name.
// Examples: "Jiayuan's Workspace" → "JIA", "My Team" → "MYT", "AB" → "AB".
func generateIssuePrefix(name string) string {
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

type WorkspaceResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	WikiContent *string `json:"wiki_content"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix string  `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func workspaceToResponse(w db.Workspace) WorkspaceResponse {
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
		WikiContent: textToPtr(w.WikiContent),
		Settings:    settings,
		Repos:       repos,
		IssuePrefix: w.IssuePrefix,
		AvatarURL:   textToPtr(w.AvatarUrl),
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
	}
}

func settingsIncludesAgentDefaults(settings any) bool {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return false
	}
	_, ok = settingsMap["agent_defaults"]
	return ok
}

func settingsIncludesKnowledgeCurator(settings any) bool {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return false
	}
	_, ok = settingsMap["knowledge_curator"]
	return ok
}

func settingsIncludesKnowledgeRAG(settings any) bool {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return false
	}
	_, ok = settingsMap["knowledge_rag"]
	return ok
}

func validateKnowledgeCuratorSettings(settings any) error {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := settingsMap["knowledge_curator"]
	if !ok || raw == nil {
		return nil
	}
	curator, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("knowledge_curator must be an object")
	}
	if v, ok := curator["enabled"]; ok {
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("knowledge_curator.enabled must be a boolean")
		}
	}
	stringFields := []string{"provider", "model", "embedding_model", "runtime_mode", "base_url", "secret_ref"}
	for _, field := range stringFields {
		if v, ok := curator[field]; ok && v != nil {
			value, ok := v.(string)
			if !ok {
				return fmt.Errorf("knowledge_curator.%s must be a string", field)
			}
			if len(value) > 500 {
				return fmt.Errorf("knowledge_curator.%s is too long", field)
			}
		}
	}
	if v, ok := curator["runtime_mode"].(string); ok && strings.TrimSpace(v) != "" {
		switch v {
		case "local", "cloud", "external":
		default:
			return fmt.Errorf("knowledge_curator.runtime_mode is invalid")
		}
	}
	if v, ok := curator["provider"].(string); ok && strings.TrimSpace(v) == "" {
		return fmt.Errorf("knowledge_curator.provider cannot be empty")
	}
	return nil
}

func validateKnowledgeRAGSettings(settings any) error {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := settingsMap["knowledge_rag"]
	if !ok || raw == nil {
		return nil
	}
	policy, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("knowledge_rag must be an object")
	}
	if v, ok := policy["auto_inject"]; ok {
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("knowledge_rag.auto_inject must be a boolean")
		}
	}
	if v, ok := policy["limit"]; ok && v != nil {
		limit, ok := jsonNumberToFloat(v)
		if !ok || limit < 1 || limit > 8 || limit != float64(int(limit)) {
			return fmt.Errorf("knowledge_rag.limit must be an integer between 1 and 8")
		}
	}
	if v, ok := policy["confidence_threshold"]; ok && v != nil {
		threshold, ok := v.(string)
		if !ok {
			return fmt.Errorf("knowledge_rag.confidence_threshold must be a string")
		}
		switch threshold {
		case "low", "medium", "high":
		default:
			return fmt.Errorf("knowledge_rag.confidence_threshold is invalid")
		}
	}
	if v, ok := policy["curator_runtime_policy"]; ok && v != nil {
		runtimePolicy, ok := v.(string)
		if !ok {
			return fmt.Errorf("knowledge_rag.curator_runtime_policy must be a string")
		}
		switch runtimePolicy {
		case "workspace_default", "cloud", "local":
		default:
			return fmt.Errorf("knowledge_rag.curator_runtime_policy is invalid")
		}
	}
	if v, ok := policy["type_filters"]; ok && v != nil {
		values, ok := v.([]any)
		if !ok {
			return fmt.Errorf("knowledge_rag.type_filters must be an array")
		}
		for _, rawType := range values {
			itemType, ok := rawType.(string)
			if !ok {
				return fmt.Errorf("knowledge_rag.type_filters must contain strings")
			}
			switch itemType {
			case "lesson", "playbook", "reference":
			default:
				return fmt.Errorf("knowledge_rag.type_filters contains an invalid type")
			}
		}
	}
	if v, ok := policy["token_budget"]; ok && v != nil {
		budget, ok := jsonNumberToFloat(v)
		if !ok || budget < 500 || budget > 8000 || budget != float64(int(budget)) {
			return fmt.Errorf("knowledge_rag.token_budget must be an integer between 500 and 8000")
		}
	}
	return nil
}

func jsonNumberToFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

func instructionsFromSettingsJSON(raw []byte) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return "", false
	}
	return instructionsFromSettingsMap(settings)
}

func instructionsFromSettingsAny(settings any) (string, bool) {
	settingsMap, ok := settings.(map[string]any)
	if !ok {
		return "", false
	}
	return instructionsFromSettingsMap(settingsMap)
}

func instructionsFromSettingsMap(settings map[string]any) (string, bool) {
	agentDefaults, ok := settings["agent_defaults"].(map[string]any)
	if !ok {
		return "", false
	}
	instructions, _ := agentDefaults["instructions"].(string)
	_, present := agentDefaults["instructions"]
	return instructions, present
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
		resp[i] = workspaceToResponse(ws)
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
	writeJSON(w, http.StatusOK, workspaceToResponse(ws))
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

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}
	defer tx.Rollback(r.Context())

	issuePrefix := generateIssuePrefix(req.Name)
	if req.IssuePrefix != nil && strings.TrimSpace(*req.IssuePrefix) != "" {
		issuePrefix = strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
	}

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

	slog.Info("workspace created", append(logger.RequestAttrs(r), "workspace_id", wsID, "name", ws.Name, "slug", ws.Slug)...)
	writeJSON(w, http.StatusCreated, workspaceToResponse(ws))
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	WikiContent *string `json:"wiki_content"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix *string `json:"issue_prefix"`
	AvatarURL   *string `json:"avatar_url"`
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

	var agentDefaultsActor db.Member
	trackAgentDefaultsInstructions := false

	// Sensitive workspace-level runtime settings require owner/admin role.
	if req.Settings != nil {
		includesAgentDefaults := settingsIncludesAgentDefaults(req.Settings)
		includesKnowledgeCurator := settingsIncludesKnowledgeCurator(req.Settings)
		includesKnowledgeRAG := settingsIncludesKnowledgeRAG(req.Settings)
		if includesAgentDefaults || includesKnowledgeCurator || includesKnowledgeRAG {
			actor, ok := h.requireWorkspaceRole(w, r, id, "workspace not found", "owner", "admin")
			if !ok {
				return
			}
			if includesAgentDefaults {
				agentDefaultsActor = actor
				_, trackAgentDefaultsInstructions = instructionsFromSettingsAny(req.Settings)
			}
		}
		if includesKnowledgeCurator {
			if err := validateKnowledgeCuratorSettings(req.Settings); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if includesKnowledgeRAG {
			if err := validateKnowledgeRAGSettings(req.Settings); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
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
	if req.WikiContent != nil {
		params.WikiContent = pgtype.Text{String: *req.WikiContent, Valid: true}
	}
	if req.Settings != nil {
		s, _ := json.Marshal(req.Settings)
		params.Settings = s
	}
	if req.Repos != nil {
		reposJSON, _ := json.Marshal(req.Repos)
		params.Repos = reposJSON
	}
	if req.IssuePrefix != nil {
		prefix := strings.ToUpper(strings.TrimSpace(*req.IssuePrefix))
		if prefix != "" {
			params.IssuePrefix = pgtype.Text{String: prefix, Valid: true}
		}
	}
	if req.AvatarURL != nil {
		params.AvatarUrl = pgtype.Text{String: *req.AvatarURL, Valid: true}
	}

	q := h.Queries
	commit := func() error { return nil }
	if trackAgentDefaultsInstructions && h.TxStarter != nil {
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update workspace")
			return
		}
		defer tx.Rollback(r.Context())
		q = h.Queries.WithTx(tx)
		commit = func() error { return tx.Commit(r.Context()) }
	}

	previousSystemInstructions := ""
	nextSystemInstructions := ""
	if trackAgentDefaultsInstructions {
		existing, err := q.GetWorkspace(r.Context(), idUUID)
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "workspace not found")
				return
			}
			slog.Warn("get workspace before update failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to update workspace")
			return
		}
		previousSystemInstructions, _ = instructionsFromSettingsJSON(existing.Settings)
		nextSystemInstructions, _ = instructionsFromSettingsAny(req.Settings)
	}

	ws, err := q.UpdateWorkspace(r.Context(), params)
	if err != nil {
		slog.Warn("update workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace: "+err.Error())
		return
	}

	if trackAgentDefaultsInstructions && nextSystemInstructions != previousSystemInstructions {
		if _, err := q.InsertInstructionsHistory(r.Context(), db.InsertInstructionsHistoryParams{
			WorkspaceID: idUUID,
			Scope:       instructionsHistoryScopeSystem,
			Content:     nextSystemInstructions,
			ActorID:     agentDefaultsActor.ID,
		}); err != nil {
			slog.Warn("insert system instructions history failed",
				append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
			writeError(w, http.StatusInternalServerError, "failed to update workspace")
			return
		}
	}

	if err := commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}

	slog.Info("workspace updated", append(logger.RequestAttrs(r), "workspace_id", id)...)
	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(ws.ID), "member", userID, map[string]any{"workspace": workspaceToResponse(ws)})

	writeJSON(w, http.StatusOK, workspaceToResponse(ws))
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
			AvatarURL:   textToPtr(m.UserAvatarUrl),
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type CreateMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func memberWithUserResponse(member db.Member, user db.User) MemberWithUserResponse {
	return MemberWithUserResponse{
		ID:          uuidToString(member.ID),
		WorkspaceID: uuidToString(member.WorkspaceID),
		UserID:      uuidToString(member.UserID),
		Role:        member.Role,
		CreatedAt:   timestampToString(member.CreatedAt),
		Name:        user.Name,
		Email:       user.Email,
		AvatarURL:   textToPtr(user.AvatarUrl),
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
	eventPayload := map[string]any{"member": memberWithUserResponse(member, user)}
	if ws, err := h.Queries.GetWorkspace(r.Context(), requester.WorkspaceID); err == nil {
		eventPayload["workspace_name"] = ws.Name
	}
	h.publish(protocol.EventMemberAdded, uuidToString(requester.WorkspaceID), "member", userID, eventPayload)

	writeJSON(w, http.StatusCreated, memberWithUserResponse(member, user))
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
		"member": memberWithUserResponse(updatedMember, user),
	})

	writeJSON(w, http.StatusOK, memberWithUserResponse(updatedMember, user))
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

	w.WriteHeader(http.StatusNoContent)
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
	if members, err := h.Queries.ListMembers(r.Context(), requester.WorkspaceID); err == nil {
		for _, m := range members {
			h.MembershipCache.Invalidate(r.Context(), uuidToString(m.UserID), workspaceID)
		}
	}

	// At this point workspaceMember has resolved → workspaceID is a valid UUID
	// (the lookup would have errored otherwise), so reuse the resolved value.
	if err := h.Queries.DeleteWorkspace(r.Context(), requester.WorkspaceID); err != nil {
		slog.Warn("delete workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	slog.Info("workspace deleted", append(logger.RequestAttrs(r), "workspace_id", workspaceID)...)
	h.publish(protocol.EventWorkspaceDeleted, workspaceID, "member", requestUserID(r), map[string]any{
		"workspace_id": workspaceID,
	})

	w.WriteHeader(http.StatusNoContent)
}
