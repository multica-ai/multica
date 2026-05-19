package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channel/binding"
	channelprovider "github.com/multica-ai/multica/server/internal/channel/provider"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Channel listen modes (stored on channel_chat_binding.listen_mode).
const (
	channelListenModeMentions = "mentions"
	channelListenModeAll      = "all"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type ChannelBindingResponse struct {
	ID               string  `json:"id"`
	Provider         string  `json:"provider"`
	ConnectionID     string  `json:"connection_id"`
	ExternalChatID   string  `json:"external_chat_id"`
	ChatType         string  `json:"chat_type"`
	ExternalChatName *string `json:"external_chat_name"`
	DefaultProjectID *string `json:"default_project_id"`
	ListenMode       string  `json:"listen_mode"`
	AgentID          *string `json:"agent_id"`
	IsPrimary        bool    `json:"is_primary"`
	BoundByUserID    string  `json:"bound_by_user_id"`
	CreatedAt        string  `json:"created_at"`
}

type ChannelConnectionResponse struct {
	ID           string                       `json:"id"`
	Provider     string                       `json:"provider"`
	DisplayName  string                       `json:"display_name"`
	Enabled      bool                         `json:"enabled"`
	IsDefault    bool                         `json:"is_default"`
	Status       string                       `json:"status"`
	LastError    *string                      `json:"last_error"`
	Config       map[string]string            `json:"config"`
	CreatedAt    string                       `json:"created_at"`
	UpdatedAt    string                       `json:"updated_at"`
	ConfigSchema []ChannelConfigFieldResponse `json:"config_schema"`
}

type ListChannelConnectionsResponse struct {
	Connections []ChannelConnectionResponse `json:"connections"`
	CanManage   bool                        `json:"can_manage"`
}

type ChannelConfigFieldResponse struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Required   bool   `json:"required"`
	Secret     bool   `json:"secret"`
	Configured bool   `json:"configured,omitempty"`
}

type ChannelProviderResponse struct {
	Provider     string                       `json:"provider"`
	DisplayName  string                       `json:"display_name"`
	ConfigSchema []ChannelConfigFieldResponse `json:"config_schema"`
}

type ChannelBindTokenPreviewResponse struct {
	Kind                  string  `json:"kind"`
	Provider              string  `json:"provider"`
	ConnectionID          string  `json:"connection_id"`
	ConnectionDisplayName string  `json:"connection_display_name"`
	ExternalChatID        *string `json:"external_chat_id"`
	ExternalChatName      *string `json:"external_chat_name"`
	ExpiresAt             string  `json:"expires_at"`
}

type channelConnectionWriteRequest struct {
	Provider     string             `json:"provider"`
	DisplayName  string             `json:"display_name"`
	Enabled      *bool              `json:"enabled"`
	IsDefault    *bool              `json:"is_default"`
	Config       map[string]*string `json:"config"`
	SecretConfig map[string]*string `json:"secret_config"`
}

func connectionToResponse(c db.ChannelConnection, schema []ChannelConfigFieldResponse, includeSensitive bool) ChannelConnectionResponse {
	if !includeSensitive {
		return ChannelConnectionResponse{
			ID:           c.ID,
			Provider:     c.Provider,
			DisplayName:  c.DisplayName,
			Enabled:      c.Enabled,
			IsDefault:    c.IsDefault,
			Status:       c.Status,
			LastError:    nil,
			Config:       map[string]string{},
			CreatedAt:    timestampToString(c.CreatedAt),
			UpdatedAt:    timestampToString(c.UpdatedAt),
			ConfigSchema: nil,
		}
	}
	config := jsonStringMap(c.Config)
	secrets := jsonStringMap(c.SecretConfig)
	schemaWithState := make([]ChannelConfigFieldResponse, 0, len(schema))
	for _, field := range schema {
		if field.Secret {
			field.Configured = strings.TrimSpace(secrets[field.Key]) != ""
		}
		schemaWithState = append(schemaWithState, field)
	}
	return ChannelConnectionResponse{
		ID:           c.ID,
		Provider:     c.Provider,
		DisplayName:  c.DisplayName,
		Enabled:      c.Enabled,
		IsDefault:    c.IsDefault,
		Status:       c.Status,
		LastError:    textToPtr(c.LastError),
		Config:       config,
		CreatedAt:    timestampToString(c.CreatedAt),
		UpdatedAt:    timestampToString(c.UpdatedAt),
		ConfigSchema: schemaWithState,
	}
}

func boolValue(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func jsonStringMap(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func mustMarshalJSON(v map[string]string) []byte {
	if v == nil {
		return []byte(`{}`)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func buildConnectionConfig(fields []channelprovider.ConfigField, existingConfig, existingSecrets map[string]string, configPatch, secretPatch map[string]*string, enabled bool) (map[string]string, map[string]string, error) {
	config := copyStringMap(existingConfig)
	secrets := copyStringMap(existingSecrets)
	known := make(map[string]channelprovider.ConfigField, len(fields))
	for _, field := range fields {
		known[field.Key] = field
	}
	for key, value := range configPatch {
		field, ok := known[key]
		if !ok {
			return nil, nil, fmt.Errorf("unknown config field: %s", key)
		}
		if field.Secret {
			return nil, nil, fmt.Errorf("field %s must be sent in secret_config", key)
		}
		if value == nil {
			delete(config, key)
		} else {
			config[key] = strings.TrimSpace(*value)
		}
	}
	for key, value := range secretPatch {
		field, ok := known[key]
		if !ok {
			return nil, nil, fmt.Errorf("unknown secret field: %s", key)
		}
		if !field.Secret {
			return nil, nil, fmt.Errorf("field %s must be sent in config", key)
		}
		if value == nil {
			delete(secrets, key)
		} else {
			secrets[key] = strings.TrimSpace(*value)
		}
	}
	if enabled {
		for _, field := range fields {
			if !field.Required {
				continue
			}
			if field.Secret {
				if strings.TrimSpace(secrets[field.Key]) == "" {
					return nil, nil, fmt.Errorf("missing required secret field: %s", field.Key)
				}
				continue
			}
			if strings.TrimSpace(config[field.Key]) == "" {
				return nil, nil, fmt.Errorf("missing required config field: %s", field.Key)
			}
		}
	}
	return config, secrets, nil
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func bindTokenKind(purpose string) string {
	if purpose == binding.PurposeChatWorkspace {
		return "chat"
	}
	return "user"
}

// canManageBinding returns true if the member is allowed to manage (delete or
// change primary status of) a binding. The rule is: binding creator OR
// workspace admin/owner.
func canManageBinding(binding db.ChannelChatBinding, member db.Member) bool {
	return uuidToString(binding.BoundByUserID) == uuidToString(member.UserID) ||
		member.Role == "owner" || member.Role == "admin"
}

// mergeAndPersistExistingChatBindingSettings applies create-request fields to a
// chat binding that already exists for the same workspace. Requires
// canManageBinding; otherwise responds 403 and returns ok=false.
func (h *Handler) mergeAndPersistExistingChatBindingSettings(
	w http.ResponseWriter,
	r *http.Request,
	qtx *db.Queries,
	member db.Member,
	existing db.ChannelChatBinding,
	req CreateChannelBindingRequest,
	defaultProjectID pgtype.UUID,
	listenM string,
) (db.ChannelChatBinding, bool) {
	if !canManageBinding(existing, member) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.ChannelChatBinding{}, false
	}
	defProj := existing.DefaultProjectID
	if req.DefaultProjectID.Set {
		if strings.TrimSpace(req.DefaultProjectID.Value) == "" {
			defProj = pgtype.UUID{Valid: false}
		} else {
			defProj = defaultProjectID
		}
	}
	agentOut, ok := h.resolveBindingAgentID(w, r, member.WorkspaceID, req.AgentID.Ptr(), existing.AgentID, true)
	if !ok {
		return db.ChannelChatBinding{}, false
	}
	updated, updateErr := qtx.UpdateChannelChatBindingSettings(r.Context(), db.UpdateChannelChatBindingSettingsParams{
		ID:               existing.ID,
		DefaultProjectID: defProj,
		ListenMode:       listenM,
		AgentID:          agentOut,
	})
	if updateErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to update binding settings")
		return db.ChannelChatBinding{}, false
	}
	return updated, true
}

func bindingToResponse(b db.ChannelChatBinding) ChannelBindingResponse {
	listen := b.ListenMode
	if listen == "" {
		listen = channelListenModeMentions
	}
	resp := ChannelBindingResponse{
		ID:               uuidToString(b.ID),
		Provider:         b.Provider,
		ConnectionID:     b.ConnectionID,
		ExternalChatID:   b.ExternalChatID,
		ChatType:         b.ChatType,
		ExternalChatName: textToPtr(b.ExternalChatName),
		DefaultProjectID: uuidToPtr(b.DefaultProjectID),
		ListenMode:       listen,
		IsPrimary:        b.IsPrimary,
		BoundByUserID:    uuidToString(b.BoundByUserID),
		CreatedAt:        timestampToString(b.CreatedAt),
	}
	if b.AgentID.Valid {
		s := uuidToString(b.AgentID)
		resp.AgentID = &s
	}
	return resp
}

func normalizeListenMode(s string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "", channelListenModeMentions:
		return channelListenModeMentions, nil
	case channelListenModeAll:
		return channelListenModeAll, nil
	default:
		return "", fmt.Errorf("listen_mode must be %q or %q", channelListenModeMentions, channelListenModeAll)
	}
}

func (h *Handler) validateBindingAgent(ctx context.Context, workspaceID pgtype.UUID, agentID pgtype.UUID) error {
	if !agentID.Valid {
		return nil
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("agent not found")
		}
		return err
	}
	if uuidToString(agent.WorkspaceID) != uuidToString(workspaceID) {
		return fmt.Errorf("agent does not belong to this workspace")
	}
	if agent.ArchivedAt.Valid {
		return fmt.Errorf("agent is archived")
	}
	if !agent.RuntimeID.Valid {
		return fmt.Errorf("agent has no runtime configured")
	}
	return nil
}

// resolveBindingAgentID maps optional request agent_id to a UUID for persistence.
// When keepExistingWhenReqNil is true and reqAgent is nil, existing is returned unchanged.
func (h *Handler) resolveBindingAgentID(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, reqAgent *string, existing pgtype.UUID, keepExistingWhenReqNil bool) (pgtype.UUID, bool) {
	if reqAgent == nil {
		if keepExistingWhenReqNil {
			return existing, true
		}
		return pgtype.UUID{Valid: false}, true
	}
	trim := strings.TrimSpace(*reqAgent)
	if trim == "" {
		return pgtype.UUID{Valid: false}, true
	}
	aid, ok := parseUUIDOrBadRequest(w, trim, "agent_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if err := h.validateBindingAgent(r.Context(), workspaceID, aid); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return pgtype.UUID{}, false
	}
	return aid, true
}

func lockChannelBindingProvider(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, connectionID string) error {
	_, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtext($1::text), hashtext($2))
	`, workspaceID, connectionID)
	return err
}

func (h *Handler) canManageChannelConnections(ctx context.Context, userID string) (bool, error) {
	return h.Queries.UserHasWorkspaceOwnerRole(ctx, parseUUID(userID))
}

func (h *Handler) requireChannelConnectionOwner(w http.ResponseWriter, r *http.Request) bool {
	userID, ok := requireUserID(w, r)
	if !ok {
		return false
	}
	canManage, err := h.canManageChannelConnections(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check channel connection permissions")
		return false
	}
	if !canManage {
		writeError(w, http.StatusForbidden, "only workspace owners can manage channel connections")
		return false
	}
	return true
}

type CreateChannelBindingRequest struct {
	Token            string             `json:"token"`
	Provider         string             `json:"provider"`
	ConnectionID     string             `json:"connection_id"`
	DefaultProjectID jsonOptionalString `json:"default_project_id"`
	ListenMode       string             `json:"listen_mode"`
	// AgentID optional: omit = keep existing on rebind; null/empty string clears.
	AgentID jsonOptionalString `json:"agent_id"`
}

type CreateChannelUserBindingRequest struct {
	Token        string `json:"token"`
	Provider     string `json:"provider"`
	ConnectionID string `json:"connection_id"`
}

// PatchChannelBindingRequest partially updates a channel chat binding.
// Fields without presence are left unchanged; explicit null/empty string clears
// nullable string settings.
type PatchChannelBindingRequest struct {
	IsPrimary        *bool              `json:"is_primary"`
	DefaultProjectID jsonOptionalString `json:"default_project_id"`
	ListenMode       *string            `json:"listen_mode"`
	AgentID          jsonOptionalString `json:"agent_id"`
}

type jsonOptionalString struct {
	Set   bool
	Value string
}

func (f *jsonOptionalString) UnmarshalJSON(data []byte) error {
	f.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		f.Value = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	f.Value = value
	return nil
}

func (f jsonOptionalString) Ptr() *string {
	if !f.Set {
		return nil
	}
	value := f.Value
	return &value
}

// ---------------------------------------------------------------------------
// POST /api/channel-user-bindings
// ---------------------------------------------------------------------------

func (h *Handler) CreateChannelUserBinding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateChannelUserBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	// Fast-fail validation: Peek before transaction to avoid starting a tx
	// for obviously bad input.
	peeker := binding.NewTokenConsumer(h.Queries)
	peeked, err := peeker.Peek(r.Context(), req.Token)
	if err != nil {
		switch {
		case errors.Is(err, binding.ErrTokenExpired):
			writeError(w, http.StatusBadRequest, "binding token expired")
		case errors.Is(err, binding.ErrTokenAlreadyConsumed):
			writeError(w, http.StatusConflict, "binding token already consumed")
		case errors.Is(err, binding.ErrTokenInvalid):
			writeError(w, http.StatusBadRequest, "invalid binding token")
		default:
			writeError(w, http.StatusInternalServerError, "failed to consume binding token")
		}
		return
	}
	if req.Provider != "" && peeked.Provider != req.Provider {
		writeError(w, http.StatusBadRequest, "provider mismatch")
		return
	}
	if req.ConnectionID != "" && peeked.ConnectionID != req.ConnectionID {
		writeError(w, http.StatusBadRequest, "connection mismatch")
		return
	}
	if peeked.Purpose != binding.PurposeUserIdentity {
		writeError(w, http.StatusBadRequest, "token purpose mismatch")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start binding transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	qtx := db.New(tx)
	consumer := binding.NewTokenConsumer(qtx)

	token, err := consumer.Consume(r.Context(), req.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		DELETE FROM channel_user_binding
		WHERE connection_id = $1 AND user_id = $2 AND external_user_id <> $3
	`, token.ConnectionID, parseUUID(userID), token.ExternalUserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace existing user binding")
		return
	}

	_, err = tx.Exec(r.Context(), `
		INSERT INTO channel_user_binding (provider, connection_id, external_user_id, user_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (connection_id, external_user_id)
		DO UPDATE SET user_id = EXCLUDED.user_id, updated_at = now()
	`, token.Provider, token.ConnectionID, token.ExternalUserID, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user binding")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit user binding")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider":         token.Provider,
		"connection_id":    token.ConnectionID,
		"external_user_id": token.ExternalUserID,
		"user_id":          userID,
	})
}

// ---------------------------------------------------------------------------
// GET /api/channel-providers
// ---------------------------------------------------------------------------

func (h *Handler) ListChannelProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	resp := make([]ChannelProviderResponse, 0, len(h.ChannelProviderFactories))
	for providerKey, factory := range h.ChannelProviderFactories {
		resp = append(resp, ChannelProviderResponse{
			Provider:     providerKey,
			DisplayName:  factory.DisplayName(),
			ConfigSchema: h.ChannelProviderSchemas[providerKey],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": resp})
}

func (h *Handler) GetChannelBindTokenPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	peeker := binding.NewTokenConsumer(h.Queries)
	row, err := peeker.Peek(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	connectionName := row.ConnectionID
	if conn, err := h.Queries.GetChannelConnection(r.Context(), row.ConnectionID); err == nil {
		connectionName = conn.DisplayName
	}
	writeJSON(w, http.StatusOK, ChannelBindTokenPreviewResponse{
		Kind:                  bindTokenKind(row.Purpose),
		Provider:              row.Provider,
		ConnectionID:          row.ConnectionID,
		ConnectionDisplayName: connectionName,
		ExternalChatID:        textToPtr(row.ExternalChatID),
		ExternalChatName:      textToPtr(row.ExternalChatName),
		ExpiresAt:             timestampToString(row.ExpiresAt),
	})
}

// ---------------------------------------------------------------------------
// /api/channel-connections
// ---------------------------------------------------------------------------

func (h *Handler) ListChannelConnections(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	canManage, err := h.canManageChannelConnections(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check channel connection permissions")
		return
	}

	connections, err := h.Queries.ListChannelConnections(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list channel connections")
		return
	}

	resp := make([]ChannelConnectionResponse, 0, len(connections))
	for _, connection := range connections {
		if !canManage && !connection.Enabled {
			continue
		}
		resp = append(resp, connectionToResponse(connection, h.ChannelProviderSchemas[connection.Provider], canManage))
	}
	writeJSON(w, http.StatusOK, ListChannelConnectionsResponse{
		Connections: resp,
		CanManage:   canManage,
	})
}

func (h *Handler) CreateChannelConnection(w http.ResponseWriter, r *http.Request) {
	if !h.requireChannelConnectionOwner(w, r) {
		return
	}
	var req channelConnectionWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	factory := h.ChannelProviderFactories[req.Provider]
	if factory == nil {
		writeError(w, http.StatusBadRequest, "unknown channel provider")
		return
	}
	enabled := boolValue(req.Enabled, false)
	isDefault := boolValue(req.IsDefault, false)
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = factory.DisplayName()
	}
	config, secrets, err := buildConnectionConfig(factory.ConfigSchema(), nil, nil, req.Config, req.SecretConfig, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	row, err := h.Queries.CreateChannelConnection(r.Context(), db.CreateChannelConnectionParams{
		ID:           uuid.NewString(),
		Provider:     req.Provider,
		DisplayName:  displayName,
		Enabled:      enabled,
		IsDefault:    isDefault,
		Config:       mustMarshalJSON(config),
		SecretConfig: mustMarshalJSON(secrets),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create channel connection")
		return
	}
	writeJSON(w, http.StatusCreated, connectionToResponse(row, h.ChannelProviderSchemas[row.Provider], true))
}

func (h *Handler) UpdateChannelConnection(w http.ResponseWriter, r *http.Request) {
	if !h.requireChannelConnectionOwner(w, r) {
		return
	}
	connectionID := chi.URLParam(r, "connectionId")
	existing, err := h.Queries.GetChannelConnection(r.Context(), connectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel connection not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load channel connection")
		return
	}
	var req channelConnectionWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	factory := h.ChannelProviderFactories[existing.Provider]
	if factory == nil {
		writeError(w, http.StatusBadRequest, "unknown channel provider")
		return
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	isDefault := existing.IsDefault
	if req.IsDefault != nil {
		isDefault = *req.IsDefault
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = existing.DisplayName
	}
	config, secrets, err := buildConnectionConfig(factory.ConfigSchema(), jsonStringMap(existing.Config), jsonStringMap(existing.SecretConfig), req.Config, req.SecretConfig, enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	row, err := h.Queries.UpdateChannelConnection(r.Context(), db.UpdateChannelConnectionParams{
		ID:           existing.ID,
		DisplayName:  displayName,
		Enabled:      enabled,
		IsDefault:    isDefault,
		Config:       mustMarshalJSON(config),
		SecretConfig: mustMarshalJSON(secrets),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update channel connection")
		return
	}
	writeJSON(w, http.StatusOK, connectionToResponse(row, h.ChannelProviderSchemas[row.Provider], true))
}

func (h *Handler) DeleteChannelConnection(w http.ResponseWriter, r *http.Request) {
	if !h.requireChannelConnectionOwner(w, r) {
		return
	}
	if err := h.Queries.DeleteChannelConnection(r.Context(), chi.URLParam(r, "connectionId")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete channel connection")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TestChannelConnection(w http.ResponseWriter, r *http.Request) {
	if !h.requireChannelConnectionOwner(w, r) {
		return
	}
	row, err := h.Queries.GetChannelConnection(r.Context(), chi.URLParam(r, "connectionId"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel connection not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load channel connection")
		return
	}
	factory := h.ChannelProviderFactories[row.Provider]
	if factory == nil {
		writeError(w, http.StatusBadRequest, "unknown channel provider")
		return
	}
	values := jsonStringMap(row.Config)
	for key, value := range jsonStringMap(row.SecretConfig) {
		values[key] = value
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	bundle, err := factory.Build(ctx, channelprovider.ConnectionConfig{
		Provider:     row.Provider,
		ConnectionID: row.ID,
		DisplayName:  row.DisplayName,
		Enabled:      row.Enabled,
		Values:       values,
	})
	if err == nil && bundle.Channel != nil {
		err = bundle.Channel.Connect(ctx)
		if err == nil {
			_ = bundle.Channel.Disconnect(ctx)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("connection test failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// GET /api/workspaces/{id}/channel-bindings
// ---------------------------------------------------------------------------

func (h *Handler) ListChannelBindings(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	bindings, err := h.Queries.ListChannelChatBindings(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list bindings")
		return
	}

	resp := make([]ChannelBindingResponse, len(bindings))
	for i, b := range bindings {
		resp[i] = bindingToResponse(b)
	}

	writeJSON(w, http.StatusOK, map[string]any{"bindings": resp})
}

// ---------------------------------------------------------------------------
// POST /api/workspaces/{id}/channel-bindings
// ---------------------------------------------------------------------------

func (h *Handler) CreateChannelBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateChannelBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	var defaultProjectID pgtype.UUID
	if req.DefaultProjectID.Set {
		if strings.TrimSpace(req.DefaultProjectID.Value) == "" {
			defaultProjectID = pgtype.UUID{Valid: false}
		} else {
			projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.DefaultProjectID.Value), "default_project_id")
			if !ok {
				return
			}
			if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
				ID:          projectID,
				WorkspaceID: member.WorkspaceID,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					writeError(w, http.StatusBadRequest, "default_project_id does not belong to this workspace")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to validate default project")
				return
			}
			defaultProjectID = projectID
		}
	}

	listenM, err := normalizeListenMode(req.ListenMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start binding transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	qtx := db.New(tx)
	consumer := binding.NewTokenConsumer(qtx)
	peeked, err := consumer.Peek(r.Context(), req.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	if req.Provider != "" && peeked.Provider != req.Provider {
		writeError(w, http.StatusBadRequest, "provider mismatch")
		return
	}
	if req.ConnectionID != "" && peeked.ConnectionID != req.ConnectionID {
		writeError(w, http.StatusBadRequest, "connection mismatch")
		return
	}
	if peeked.Purpose != binding.PurposeChatWorkspace {
		writeError(w, http.StatusBadRequest, "token purpose mismatch")
		return
	}
	if !peeked.ExternalChatID.Valid || !peeked.ExternalChatType.Valid {
		writeError(w, http.StatusBadRequest, "invalid chat binding token")
		return
	}
	if !userOwnsExternalChannelIdentity(r, tx, member.UserID, peeked.ConnectionID, peeked.ExternalUserID) {
		writeError(w, http.StatusForbidden, "binding link belongs to another channel user")
		return
	}

	existing, err := qtx.GetChannelChatBindingByProviderAndChatID(r.Context(), db.GetChannelChatBindingByProviderAndChatIDParams{
		ConnectionID:   peeked.ConnectionID,
		ExternalChatID: peeked.ExternalChatID.String,
	})
	if err == nil {
		if existing.WorkspaceID == member.WorkspaceID {
			updated, ok := h.mergeAndPersistExistingChatBindingSettings(w, r, qtx, member, existing, req, defaultProjectID, listenM)
			if !ok {
				return
			}
			existing = updated
			if _, consumeErr := consumer.Consume(r.Context(), req.Token); consumeErr != nil {
				writeError(w, http.StatusBadRequest, "invalid or expired token")
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit binding transaction")
				return
			}
			writeJSON(w, http.StatusOK, bindingToResponse(existing))
			return
		}
		writeError(w, http.StatusConflict, "this chat is already bound to another workspace")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check existing chat binding")
		return
	}

	token, err := consumer.Consume(r.Context(), req.Token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired token")
		return
	}

	if err := lockChannelBindingProvider(r.Context(), tx, member.WorkspaceID, token.ConnectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock channel bindings")
		return
	}

	existing, err = qtx.GetChannelChatBindingByProviderAndChatID(r.Context(), db.GetChannelChatBindingByProviderAndChatIDParams{
		ConnectionID:   peeked.ConnectionID,
		ExternalChatID: peeked.ExternalChatID.String,
	})
	if err == nil {
		if existing.WorkspaceID == member.WorkspaceID {
			updated, ok := h.mergeAndPersistExistingChatBindingSettings(w, r, qtx, member, existing, req, defaultProjectID, listenM)
			if !ok {
				return
			}
			existing = updated
			if err := tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit binding transaction")
				return
			}
			writeJSON(w, http.StatusOK, bindingToResponse(existing))
			return
		}
		writeError(w, http.StatusConflict, "this chat is already bound to another workspace")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to check existing chat binding")
		return
	}

	// Lock and check existing bindings for this workspace/channel connection
	// to determine is_primary
	if _, err := tx.Exec(r.Context(), `
		SELECT id FROM channel_chat_binding
		WHERE workspace_id = $1 AND connection_id = $2
		FOR UPDATE
	`, member.WorkspaceID, token.ConnectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock channel bindings")
		return
	}

	existingBindings, err := qtx.ListChannelChatBindings(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check existing bindings")
		return
	}
	providerCount := 0
	for _, b := range existingBindings {
		if b.ConnectionID == token.ConnectionID {
			providerCount++
		}
	}
	isPrimary := providerCount == 0

	agentForCreate, ok := h.resolveBindingAgentID(w, r, member.WorkspaceID, req.AgentID.Ptr(), pgtype.UUID{}, false)
	if !ok {
		return
	}

	binding, err := qtx.CreateChannelChatBinding(r.Context(), db.CreateChannelChatBindingParams{
		Provider:         token.Provider,
		ConnectionID:     token.ConnectionID,
		ExternalChatID:   token.ExternalChatID.String,
		ChatType:         normalizeChannelChatType(token.ExternalChatType.String),
		WorkspaceID:      member.WorkspaceID,
		IsPrimary:        isPrimary,
		BoundByUserID:    member.UserID,
		ExternalChatName: token.ExternalChatName,
		DefaultProjectID: defaultProjectID,
		ListenMode:       listenM,
		AgentID:          agentForCreate,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this chat is already bound to another workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create binding")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit binding transaction")
		return
	}

	writeJSON(w, http.StatusCreated, bindingToResponse(binding))
}

func userOwnsExternalChannelIdentity(r *http.Request, exec interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, userID pgtype.UUID, connectionID, externalUserID string) bool {
	var count int
	err := exec.QueryRow(r.Context(), `
		SELECT count(*) FROM channel_user_binding
		WHERE connection_id = $1 AND external_user_id = $2 AND user_id = $3
	`, connectionID, externalUserID, userID).Scan(&count)
	return err == nil && count > 0
}

func normalizeChannelChatType(chatType string) string {
	if chatType == "direct" {
		return "dm"
	}
	return "group"
}

// ---------------------------------------------------------------------------
// DELETE /api/workspaces/{id}/channel-bindings/{bindingId}
// ---------------------------------------------------------------------------

func (h *Handler) DeleteChannelBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	bindingID := chi.URLParam(r, "bindingId")
	bindingUUID, ok := parseUUIDOrBadRequest(w, bindingID, "binding id")
	if !ok {
		return
	}

	binding, err := h.Queries.GetChannelChatBinding(r.Context(), bindingUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load binding")
		return
	}

	if uuidToString(binding.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "binding not found")
		return
	}

	// Only binding creator or workspace admin/owner can delete
	if !canManageBinding(binding, member) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start binding transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := db.New(tx)

	if err := lockChannelBindingProvider(r.Context(), tx, binding.WorkspaceID, binding.ConnectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock channel bindings")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		SELECT id FROM channel_chat_binding
		WHERE workspace_id = $1 AND connection_id = $2
		FOR UPDATE
	`, binding.WorkspaceID, binding.ConnectionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock channel bindings")
		return
	}

	binding, err = qtx.GetChannelChatBinding(r.Context(), bindingUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reload binding")
		return
	}
	if uuidToString(binding.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "binding not found")
		return
	}
	if !canManageBinding(binding, member) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	// Prevent deleting the primary binding while other bindings for the
	// same channel connection still exist — zero bindings is a valid state, but
	// orphaned non-primary bindings are not.
	if binding.IsPrimary {
		bindings, err := qtx.ListChannelChatBindings(r.Context(), binding.WorkspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check primary bindings")
			return
		}
		providerBindingCount := 0
		for _, b := range bindings {
			if b.ConnectionID == binding.ConnectionID {
				providerBindingCount++
			}
		}
		if providerBindingCount > 1 {
			writeError(w, http.StatusBadRequest, "cannot delete primary binding: promote another binding first")
			return
		}
	}

	if err := qtx.DeleteChannelChatBinding(r.Context(), bindingUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete binding")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit binding transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// PATCH /api/workspaces/{id}/channel-bindings/{bindingId}
// ---------------------------------------------------------------------------

func (h *Handler) SetPrimaryChannelBinding(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	bindingID := chi.URLParam(r, "bindingId")
	bindingUUID, ok := parseUUIDOrBadRequest(w, bindingID, "binding id")
	if !ok {
		return
	}

	var req PatchChannelBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	binding, err := h.Queries.GetChannelChatBinding(r.Context(), bindingUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load binding")
		return
	}

	if uuidToString(binding.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "binding not found")
		return
	}

	if !canManageBinding(binding, member) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	if req.IsPrimary == nil && !req.DefaultProjectID.Set && req.ListenMode == nil && !req.AgentID.Set {
		writeError(w, http.StatusBadRequest, "no updates provided")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start binding transaction")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	qtx := db.New(tx)

	if req.IsPrimary != nil {
		if !*req.IsPrimary && binding.IsPrimary {
			bindings, err := qtx.ListChannelChatBindings(r.Context(), binding.WorkspaceID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to check primary bindings")
				return
			}
			primaryCount := 0
			for _, b := range bindings {
				if b.ConnectionID == binding.ConnectionID && b.IsPrimary {
					primaryCount++
				}
			}
			if primaryCount <= 1 {
				writeError(w, http.StatusBadRequest, "cannot unset primary: workspace would have no primary binding")
				return
			}
		}

		if *req.IsPrimary {
			if _, err := tx.Exec(r.Context(), `
			SELECT id FROM channel_chat_binding
			WHERE workspace_id = $1 AND connection_id = $2
			FOR UPDATE
		`, binding.WorkspaceID, binding.ConnectionID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to lock channel bindings")
				return
			}
			if err := qtx.ClearPrimaryBindingsForWorkspaceProvider(r.Context(), db.ClearPrimaryBindingsForWorkspaceProviderParams{
				WorkspaceID:  binding.WorkspaceID,
				ConnectionID: binding.ConnectionID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to clear primary bindings")
				return
			}
		}

		binding, err = qtx.SetChannelChatBindingPrimary(r.Context(), db.SetChannelChatBindingPrimaryParams{
			ID:        bindingUUID,
			IsPrimary: *req.IsPrimary,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update binding")
			return
		}
	}

	if req.DefaultProjectID.Set || req.ListenMode != nil || req.AgentID.Set {
		defProj := binding.DefaultProjectID
		if req.DefaultProjectID.Set {
			if strings.TrimSpace(req.DefaultProjectID.Value) == "" {
				defProj = pgtype.UUID{Valid: false}
			} else {
				pid, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.DefaultProjectID.Value), "default_project_id")
				if !ok {
					return
				}
				if _, err := qtx.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
					ID:          pid,
					WorkspaceID: binding.WorkspaceID,
				}); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						writeError(w, http.StatusBadRequest, "default_project_id does not belong to this workspace")
						return
					}
					writeError(w, http.StatusInternalServerError, "failed to validate default project")
					return
				}
				defProj = pid
			}
		}
		listenM := binding.ListenMode
		if listenM == "" {
			listenM = channelListenModeMentions
		}
		if req.ListenMode != nil {
			lm, errLM := normalizeListenMode(*req.ListenMode)
			if errLM != nil {
				writeError(w, http.StatusBadRequest, errLM.Error())
				return
			}
			listenM = lm
		}
		agentOut, ok := h.resolveBindingAgentID(w, r, binding.WorkspaceID, req.AgentID.Ptr(), binding.AgentID, !req.AgentID.Set)
		if !ok {
			return
		}
		binding, err = qtx.UpdateChannelChatBindingSettings(r.Context(), db.UpdateChannelChatBindingSettingsParams{
			ID:               bindingUUID,
			DefaultProjectID: defProj,
			ListenMode:       listenM,
			AgentID:          agentOut,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update binding settings")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit binding transaction")
		return
	}

	writeJSON(w, http.StatusOK, bindingToResponse(binding))
}
