package handler

// CEREBRO-PATCH(workspace-handler-cerebro): cerebro modification of upstream file

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/cerebro/firtalgateway"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)
var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const (
	firtalGatewaySettingsKey = "firtal_gateway"
	firtalGatewayAPIKeyMask  = "********"
)

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
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix string  `json:"issue_prefix"`
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
	settings = sanitizeWorkspaceSettingsForResponse(settings)
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
		CreatedAt:   timestampToString(w.CreatedAt),
		UpdatedAt:   timestampToString(w.UpdatedAt),
	}
}

func sanitizeWorkspaceSettingsForResponse(settings any) any {
	settingsMap, err := settingsMapFromAny(settings)
	if err != nil {
		return map[string]any{}
	}
	if gateway, ok := mapFromAny(settingsMap[firtalGatewaySettingsKey]); ok {
		if gatewayString(gateway, "api_key") != "" {
			delete(gateway, "api_key")
			gateway["api_key_configured"] = true
		}
		settingsMap[firtalGatewaySettingsKey] = gateway
	}
	return settingsMap
}

func sanitizeWorkspaceSettingsJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var settings any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return json.RawMessage(`{}`)
	}
	sanitized, err := json.Marshal(sanitizeWorkspaceSettingsForResponse(settings))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(sanitized)
}

func prepareWorkspaceSettingsUpdate(currentRaw []byte, incoming any) ([]byte, bool, error) {
	current := settingsMapFromJSON(currentRaw)
	next, err := settingsMapFromAny(incoming)
	if err != nil {
		return nil, false, fmt.Errorf("settings must be an object")
	}

	currentGateway, hasCurrentGateway := mapFromAny(current[firtalGatewaySettingsKey])
	incomingGateway, hasIncomingGateway := next[firtalGatewaySettingsKey]
	gatewayChanged := false

	if hasIncomingGateway {
		normalized, changed, err := normalizeFirtalGatewaySettingsForStorage(currentGateway, incomingGateway)
		if err != nil {
			return nil, false, err
		}
		gatewayChanged = changed
		if normalized == nil {
			delete(next, firtalGatewaySettingsKey)
		} else {
			next[firtalGatewaySettingsKey] = normalized
		}
	} else if hasCurrentGateway {
		next[firtalGatewaySettingsKey] = cloneMap(currentGateway)
	}

	raw, err := json.Marshal(next)
	if err != nil {
		return nil, false, fmt.Errorf("marshal settings: %w", err)
	}
	return raw, gatewayChanged, nil
}

func normalizeFirtalGatewaySettingsForStorage(current map[string]any, incoming any) (map[string]any, bool, error) {
	if incoming == nil {
		return nil, current != nil, nil
	}
	next, ok := mapFromAny(incoming)
	if !ok {
		return nil, false, fmt.Errorf("firtal gateway settings must be an object")
	}
	next = cloneMap(next)
	delete(next, "api_key_configured")

	if err := normalizeBoolField(next, "enabled"); err != nil {
		return nil, false, err
	}
	if err := normalizeStringField(next, "gateway_url"); err != nil {
		return nil, false, err
	}
	if err := normalizeStringField(next, "model"); err != nil {
		return nil, false, err
	}

	gatewayURL := gatewayString(next, "gateway_url")
	if gatewayURL != "" {
		normalizedURL, err := firtalgateway.ValidateBaseURL(gatewayURL)
		if err != nil {
			return nil, false, fmt.Errorf("firtal gateway URL %w", err)
		}
		next["gateway_url"] = normalizedURL
	}
	preserveCurrentKey := !gatewayHostChanged(current, next)
	if err := normalizeGatewayAPIKey(next, gatewayString(current, "api_key"), preserveCurrentKey); err != nil {
		return nil, false, err
	}
	if enabled, _ := next["enabled"].(bool); enabled {
		if gatewayString(next, "gateway_url") == "" {
			return nil, false, fmt.Errorf("firtal gateway URL is required when enabled")
		}
		if gatewayString(next, "api_key") == "" {
			return nil, false, fmt.Errorf("firtal gateway API key is required when enabled")
		}
	}

	return next, !reflect.DeepEqual(gatewayComparable(current), gatewayComparable(next)), nil
}

func normalizeBoolField(values map[string]any, key string) error {
	value, ok := values[key]
	if !ok {
		return nil
	}
	if value == nil {
		delete(values, key)
		return nil
	}
	if _, ok := value.(bool); !ok {
		return fmt.Errorf("%s must be a boolean", key)
	}
	return nil
}

func normalizeStringField(values map[string]any, key string) error {
	value, ok := values[key]
	if !ok {
		return nil
	}
	if value == nil {
		delete(values, key)
		return nil
	}
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s must be a string", key)
	}
	str = strings.TrimSpace(str)
	if str == "" {
		delete(values, key)
		return nil
	}
	if key == "gateway_url" {
		str = strings.TrimRight(str, "/")
	}
	values[key] = str
	return nil
}

func normalizeGatewayAPIKey(values map[string]any, currentKey string, preserveCurrent bool) error {
	value, ok := values["api_key"]
	if !ok {
		if preserveCurrent && currentKey != "" {
			values["api_key"] = currentKey
		}
		return nil
	}
	if value == nil {
		delete(values, "api_key")
		return nil
	}
	apiKey, ok := value.(string)
	if !ok {
		return fmt.Errorf("api_key must be a string")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || apiKey == firtalGatewayAPIKeyMask {
		if preserveCurrent && currentKey != "" {
			values["api_key"] = currentKey
		} else {
			delete(values, "api_key")
		}
		return nil
	}
	values["api_key"] = apiKey
	return nil
}

func gatewayHostChanged(current, next map[string]any) bool {
	currentURL := gatewayString(current, "gateway_url")
	nextURL := gatewayString(next, "gateway_url")
	if currentURL == "" || nextURL == "" {
		return currentURL != nextURL
	}
	currentHost, currentOK := gatewayHost(currentURL)
	nextHost, nextOK := gatewayHost(nextURL)
	if !currentOK || !nextOK {
		return currentURL != nextURL
	}
	return currentHost != nextHost
}

func gatewayHost(raw string) (string, bool) {
	host, err := firtalgateway.BaseURLHost(raw)
	return host, err == nil
}

func gatewayComparable(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := cloneMap(values)
	delete(out, "api_key_configured")
	_ = normalizeBoolField(out, "enabled")
	_ = normalizeStringField(out, "gateway_url")
	_ = normalizeStringField(out, "model")
	_ = normalizeStringField(out, "api_key")
	return out
}

func settingsMapFromJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func settingsMapFromAny(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func mapFromAny(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return nil, false
	}
	return out, true
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func gatewayString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
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

	// Becoming a workspace member is the physical event that "completes" onboarding —
	// keep this atomic with CreateMember so `member` and `onboarded_at`
	// can never disagree. COALESCE in MarkUserOnboarded keeps it idempotent.
	if _, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark user onboarded")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace")
		return
	}

	// "Is this the user's first workspace?" is derived in PostHog by looking
	// at whether they have a prior workspace_created event, not stamped at
	// emit time. Stamping here would race under concurrent creates without
	// a schema change, and the event stream answers the question exactly.
	h.Analytics.Capture(analytics.WorkspaceCreated(userID, uuidToString(ws.ID)))

	slog.Info("workspace created", append(logger.RequestAttrs(r), "workspace_id", uuidToString(ws.ID), "name", ws.Name, "slug", ws.Slug)...)
	writeJSON(w, http.StatusCreated, workspaceToResponse(ws))
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Context     *string `json:"context"`
	Settings    any     `json:"settings"`
	Repos       any     `json:"repos"`
	IssuePrefix *string `json:"issue_prefix"`
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
		current, err := h.Queries.GetWorkspace(r.Context(), idUUID)
		if err != nil {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		settingsJSON, gatewayChanged, err := prepareWorkspaceSettingsUpdate(current.Settings, req.Settings)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if gatewayChanged {
			if _, ok := h.requireWorkspaceRole(w, r, id, "workspace not found", "owner"); !ok {
				return
			}
		}
		params.Settings = settingsJSON
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

	ws, err := h.Queries.UpdateWorkspace(r.Context(), params)
	if err != nil {
		slog.Warn("update workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", id)...)
		writeError(w, http.StatusInternalServerError, "failed to update workspace: "+err.Error())
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
	ID                       string  `json:"id"`
	WorkspaceID              string  `json:"workspace_id"`
	UserID                   string  `json:"user_id"`
	Role                     string  `json:"role"`
	CreatedAt                string  `json:"created_at"`
	Name                     string  `json:"name"`
	Email                    string  `json:"email"`
	AvatarURL                *string `json:"avatar_url"`
	BudgetEnforcementEnabled bool    `json:"budget_enforcement_enabled"`
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
			ID:                       uuidToString(m.ID),
			WorkspaceID:              uuidToString(m.WorkspaceID),
			UserID:                   uuidToString(m.UserID),
			Role:                     m.Role,
			CreatedAt:                timestampToString(m.CreatedAt),
			Name:                     m.UserName,
			Email:                    m.UserEmail,
			AvatarURL:                textToPtr(m.UserAvatarUrl),
			BudgetEnforcementEnabled: m.BudgetEnforcementEnabled,
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
		ID:                       uuidToString(member.ID),
		WorkspaceID:              uuidToString(member.WorkspaceID),
		UserID:                   uuidToString(member.UserID),
		Role:                     member.Role,
		CreatedAt:                timestampToString(member.CreatedAt),
		Name:                     user.Name,
		Email:                    user.Email,
		AvatarURL:                textToPtr(user.AvatarUrl),
		BudgetEnforcementEnabled: member.BudgetEnforcementEnabled,
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

	if err := h.Queries.DeleteMember(r.Context(), target.ID); err != nil {
		slog.Warn("delete member failed", append(logger.RequestAttrs(r), "error", err, "member_id", memberID, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to delete member")
		return
	}

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(target.ID), "workspace_id", workspaceID, "user_id", uuidToString(target.UserID))...)
	userID := requestUserID(r)
	h.publish(protocol.EventMemberRemoved, uuidToString(requester.WorkspaceID), "member", userID, map[string]any{
		"member_id":    uuidToString(target.ID),
		"workspace_id": uuidToString(requester.WorkspaceID),
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

	if err := h.Queries.DeleteMember(r.Context(), member.ID); err != nil {
		slog.Warn("leave workspace failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to leave workspace")
		return
	}

	slog.Info("member removed", append(logger.RequestAttrs(r), "member_id", uuidToString(member.ID), "workspace_id", workspaceID, "user_id", uuidToString(member.UserID))...)
	userID := requestUserID(r)
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
