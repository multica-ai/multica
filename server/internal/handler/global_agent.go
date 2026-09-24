package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/agentconfig"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Global agents (#8775) let a user define an agent once and run it in any of
// their workspaces. The definition lives in global_agent, owned by the user;
// each workspace it is enabled in holds an ordinary agent row linked through
// agent.global_agent_id. Dispatch, tasks, chats and permissions only ever see
// the workspace row, so nothing downstream knows about global agents.
//
// The synced fields — name, description, instructions, avatar and
// conversation starters — are copied onto every linked row in the same
// transaction that changes them. Everything tied to a workspace or a machine
// (runtime, model, thinking level, env, MCP config, skills, access) stays per
// workspace.

const (
	globalAgentOwnerNameIndex = "global_agent_owner_name_uidx"
	agentWorkspaceNameUnique  = "agent_workspace_name_unique"

	linkedAgentSyncedFieldsForbidden = "this agent is synced from a global agent; only its owner can change its name, description, instructions, avatar or conversation starters"
)

type GlobalAgentLinkResponse struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	WorkspaceSlug string `json:"workspace_slug"`
	AgentID       string `json:"agent_id"`
	// Archived is true when the owner disabled the global agent in this
	// workspace. The row is kept so enabling it again restores its history.
	Archived     bool `json:"archived"`
	RuntimeBound bool `json:"runtime_bound"`
}

type GlobalAgentResponse struct {
	ID                   string                     `json:"id"`
	OwnerID              string                     `json:"owner_id"`
	Name                 string                     `json:"name"`
	Description          string                     `json:"description"`
	Instructions         string                     `json:"instructions"`
	AvatarURL            *string                    `json:"avatar_url"`
	ConversationStarters []AgentConversationStarter `json:"conversation_starters"`
	Links                []GlobalAgentLinkResponse  `json:"links"`
	CreatedAt            string                     `json:"created_at"`
	UpdatedAt            string                     `json:"updated_at"`
}

type GlobalAgentRuntimeOption struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	OwnedByMe bool   `json:"owned_by_me"`
}

type GlobalAgentWorkspaceAgent struct {
	ID        string `json:"id"`
	Archived  bool   `json:"archived"`
	RuntimeID string `json:"runtime_id"`
}

// GlobalAgentWorkspaceTarget describes one of the caller's workspaces from the
// point of view of a global agent: whether it is enabled there, and which
// runtimes the caller may bind it to.
type GlobalAgentWorkspaceTarget struct {
	WorkspaceID   string                     `json:"workspace_id"`
	WorkspaceName string                     `json:"workspace_name"`
	WorkspaceSlug string                     `json:"workspace_slug"`
	Agent         *GlobalAgentWorkspaceAgent `json:"agent"`
	Runtimes      []GlobalAgentRuntimeOption `json:"runtimes"`
	// SuggestedRuntimeID is empty when the caller has no usable runtime in the
	// workspace.
	SuggestedRuntimeID string `json:"suggested_runtime_id"`
}

type CreateGlobalAgentRequest struct {
	Name                 string                     `json:"name"`
	Description          string                     `json:"description"`
	Instructions         string                     `json:"instructions"`
	AvatarURL            *string                    `json:"avatar_url"`
	ConversationStarters []AgentConversationStarter `json:"conversation_starters"`
}

type UpdateGlobalAgentRequest struct {
	Name                 *string                     `json:"name"`
	Description          *string                     `json:"description"`
	Instructions         *string                     `json:"instructions"`
	AvatarURL            *string                     `json:"avatar_url"`
	ConversationStarters *[]AgentConversationStarter `json:"conversation_starters"`
}

type EnableGlobalAgentRequest struct {
	WorkspaceID string `json:"workspace_id"`
	RuntimeID   string `json:"runtime_id"`
}

// globalAgentFields is a validated write to the synced fields. An invalid
// pgtype.Text or a nil ConversationStarters means "leave unchanged".
type globalAgentFields struct {
	Name                 pgtype.Text
	Description          pgtype.Text
	Instructions         pgtype.Text
	AvatarURL            pgtype.Text
	ConversationStarters []byte
}

func (f globalAgentFields) empty() bool {
	return !f.Name.Valid && !f.Description.Valid && !f.Instructions.Valid &&
		!f.AvatarURL.Valid && f.ConversationStarters == nil
}

func (h *Handler) globalAgentToResponse(g db.GlobalAgent, links []GlobalAgentLinkResponse) GlobalAgentResponse {
	if links == nil {
		links = []GlobalAgentLinkResponse{}
	}
	return GlobalAgentResponse{
		ID:                   uuidToString(g.ID),
		OwnerID:              uuidToString(g.OwnerID),
		Name:                 g.Name,
		Description:          g.Description,
		Instructions:         g.Instructions,
		AvatarURL:            h.resolveAvatarURLPtr(textToPtr(g.AvatarUrl)),
		ConversationStarters: decodeConversationStarters(g.ConversationStarters),
		Links:                links,
		CreatedAt:            timestampToString(g.CreatedAt),
		UpdatedAt:            timestampToString(g.UpdatedAt),
	}
}

func decodeConversationStarters(raw []byte) []AgentConversationStarter {
	starters := []AgentConversationStarter{}
	if len(raw) == 0 {
		return starters
	}
	if err := json.Unmarshal(raw, &starters); err != nil || starters == nil {
		return []AgentConversationStarter{}
	}
	return starters
}

// globalAgentLinks returns the owner's workspace copies grouped by global
// agent id: of every global agent, or of one when globalAgentID is valid.
func (h *Handler) globalAgentLinks(ctx context.Context, ownerID, globalAgentID pgtype.UUID) (map[string][]GlobalAgentLinkResponse, error) {
	rows, err := h.Queries.ListGlobalAgentLinksByOwner(ctx, db.ListGlobalAgentLinksByOwnerParams{
		OwnerID:       ownerID,
		GlobalAgentID: globalAgentID,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]GlobalAgentLinkResponse, len(rows))
	for _, row := range rows {
		key := uuidToString(row.GlobalAgentID)
		out[key] = append(out[key], GlobalAgentLinkResponse{
			WorkspaceID:   uuidToString(row.WorkspaceID),
			WorkspaceName: row.WorkspaceName,
			WorkspaceSlug: row.WorkspaceSlug,
			AgentID:       uuidToString(row.AgentID),
			Archived:      row.ArchivedAt.Valid,
			RuntimeBound:  row.RuntimeID.Valid,
		})
	}
	return out, nil
}

// writeGlobalAgent answers with g and its workspace copies. It runs after the
// request's writes have committed, so a failed link lookup is logged and
// answered without links rather than reported as a failed write.
func (h *Handler) writeGlobalAgent(w http.ResponseWriter, r *http.Request, status int, g db.GlobalAgent) {
	links, err := h.globalAgentLinks(r.Context(), g.OwnerID, g.ID)
	if err != nil {
		slog.Warn("global agent: load links failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(g.ID))...)
	}
	writeJSON(w, status, h.globalAgentToResponse(g, links[uuidToString(g.ID)]))
}

// parseGlobalAgentFields validates the synced fields shared by the create and
// update paths. currentAvatar is the stored avatar, so an unchanged re-send
// skips re-authorization. ok=false means the error response is written.
func (h *Handler) parseGlobalAgentFields(w http.ResponseWriter, r *http.Request, req UpdateGlobalAgentRequest, currentAvatar string) (globalAgentFields, bool) {
	var f globalAgentFields
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return f, false
		}
		f.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxAgentDescriptionLength {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxAgentDescriptionLength))
			return f, false
		}
		f.Description = pgtype.Text{String: *req.Description, Valid: true}
	}
	if req.Instructions != nil {
		f.Instructions = pgtype.Text{String: *req.Instructions, Valid: true}
	}
	// An empty avatar_url clears the avatar, as it does on PUT /api/agents.
	if req.AvatarURL != nil {
		avatarURL, ok := h.acceptAvatarURL(w, r, *req.AvatarURL, currentAvatar)
		if !ok {
			return f, false
		}
		f.AvatarURL = pgtype.Text{String: avatarURL, Valid: true}
	}
	if req.ConversationStarters != nil {
		starters, err := normaliseAgentConversationStarters(*req.ConversationStarters)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return f, false
		}
		f.ConversationStarters, _ = json.Marshal(starters)
	}
	return f, true
}

// changedFrom keeps only the values that differ from the stored ones. An echo
// of unchanged values — clients round-trip whole objects — is not a write.
func (f globalAgentFields) changedFrom(name, description, instructions string, avatarURL pgtype.Text, starters []byte) globalAgentFields {
	if f.Name.Valid && f.Name.String == name {
		f.Name = pgtype.Text{}
	}
	if f.Description.Valid && f.Description.String == description {
		f.Description = pgtype.Text{}
	}
	if f.Instructions.Valid && f.Instructions.String == instructions {
		f.Instructions = pgtype.Text{}
	}
	if f.AvatarURL.Valid && f.AvatarURL.String == avatarURL.String {
		f.AvatarURL = pgtype.Text{}
	}
	if f.ConversationStarters != nil &&
		slices.Equal(decodeConversationStarters(f.ConversationStarters), normalisedStoredStarters(starters)) {
		f.ConversationStarters = nil
	}
	return f
}

// normalisedStoredStarters decodes stored starters the way a request is
// normalised, so rows saved before normalisation compare equal to their echo.
func normalisedStoredStarters(raw []byte) []AgentConversationStarter {
	stored := decodeConversationStarters(raw)
	if normalised, err := normaliseAgentConversationStarters(stored); err == nil {
		return normalised
	}
	return stored
}

// linkedAgentSyncedFieldChanges extracts the synced-field part of a validated
// UpdateAgent write, keeping only values that differ from the stored row.
func linkedAgentSyncedFieldChanges(existing db.Agent, params db.UpdateAgentParams) globalAgentFields {
	f := globalAgentFields{
		Name:                 params.Name,
		Description:          params.Description,
		Instructions:         params.Instructions,
		AvatarURL:            params.AvatarUrl,
		ConversationStarters: params.ConversationStarters,
	}
	return f.changedFrom(existing.Name, existing.Description, existing.Instructions, existing.AvatarUrl, existing.ConversationStarters)
}

// applyGlobalAgentUpdate writes the synced fields to the global agent and to
// every linked workspace agent in one transaction, then broadcasts the linked
// agents. ok=false means the error response is written.
func (h *Handler) applyGlobalAgentUpdate(w http.ResponseWriter, r *http.Request, globalAgentID, ownerID pgtype.UUID, f globalAgentFields) (db.GlobalAgent, bool) {
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("update global agent: begin failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start global agent update")
		return db.GlobalAgent{}, false
	}
	defer tx.Rollback(ctx)
	updated, linked, ok := h.syncGlobalAgentInTx(w, r, h.Queries.WithTx(tx), globalAgentID, ownerID, f, false)
	if !ok {
		return db.GlobalAgent{}, false
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("update global agent: commit failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(globalAgentID))...)
		writeError(w, http.StatusInternalServerError, "failed to commit global agent update")
		return db.GlobalAgent{}, false
	}
	slog.Info("global agent updated", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(updated.ID), "linked_agents", len(linked))...)
	h.publishLinkedAgents(r, protocol.EventAgentStatus, linked, pgtype.UUID{})
	return updated, true
}

// syncGlobalAgentInTx writes the synced fields to the global agent and to every
// linked workspace agent inside the caller's transaction, holding the global
// agent's row lock until the caller commits. It returns the linked agents for
// the caller to broadcast after commit. fromLinkedAgent marks an edit made on
// one copy: a definition deleted meanwhile is then a conflict to retry, not a
// missing resource. ok=false means the error response is written.
func (h *Handler) syncGlobalAgentInTx(w http.ResponseWriter, r *http.Request, qtx *db.Queries, globalAgentID, ownerID pgtype.UUID, f globalAgentFields, fromLinkedAgent bool) (db.GlobalAgent, []db.Agent, bool) {
	ctx := r.Context()
	current, err := qtx.LockGlobalAgentForOwner(ctx, db.LockGlobalAgentForOwnerParams{ID: globalAgentID, OwnerID: ownerID})
	if err != nil {
		switch {
		case isNotFound(err) && fromLinkedAgent:
			writeError(w, http.StatusConflict, "this agent's global agent changed while saving; reload and try again")
		case isNotFound(err):
			writeError(w, http.StatusNotFound, "global agent not found")
		default:
			slog.Warn("sync global agent: lock failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(globalAgentID))...)
			writeError(w, http.StatusInternalServerError, "failed to load global agent")
		}
		return db.GlobalAgent{}, nil, false
	}

	if f.Name.Valid && f.Name.String != current.Name {
		workspaceName, err := qtx.FindAgentNameConflictForGlobalAgent(ctx, db.FindAgentNameConflictForGlobalAgentParams{
			Name:          f.Name.String,
			GlobalAgentID: globalAgentID,
		})
		if err == nil {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in workspace %q", f.Name.String, workspaceName))
			return db.GlobalAgent{}, nil, false
		}
		if !isNotFound(err) {
			slog.Warn("sync global agent: name check failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(globalAgentID))...)
			writeError(w, http.StatusInternalServerError, "failed to check agent names")
			return db.GlobalAgent{}, nil, false
		}
	}

	updated, err := qtx.UpdateGlobalAgent(ctx, db.UpdateGlobalAgentParams{
		ID:                   globalAgentID,
		OwnerID:              ownerID,
		Name:                 f.Name,
		Description:          f.Description,
		Instructions:         f.Instructions,
		AvatarUrl:            f.AvatarURL,
		ConversationStarters: f.ConversationStarters,
	})
	if err != nil {
		if isUniqueConstraint(err, globalAgentOwnerNameIndex) {
			writeError(w, http.StatusConflict, fmt.Sprintf("you already have a global agent named %q", f.Name.String))
			return db.GlobalAgent{}, nil, false
		}
		slog.Warn("update global agent failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(globalAgentID))...)
		writeError(w, http.StatusInternalServerError, "failed to update global agent")
		return db.GlobalAgent{}, nil, false
	}

	linked, err := qtx.SyncLinkedAgentsFromGlobalAgent(ctx, db.SyncLinkedAgentsFromGlobalAgentParams{
		Name:                 updated.Name,
		Description:          updated.Description,
		Instructions:         updated.Instructions,
		AvatarUrl:            updated.AvatarUrl,
		ConversationStarters: updated.ConversationStarters,
		GlobalAgentID:        updated.ID,
	})
	if err != nil {
		// The pre-check above names the workspace; this is the race where
		// another agent took the name between the check and the write.
		if isUniqueConstraint(err, agentWorkspaceNameUnique) {
			writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in one of this global agent's workspaces", updated.Name))
			return db.GlobalAgent{}, nil, false
		}
		slog.Warn("sync linked agents failed", append(logger.RequestAttrs(r), "error", err, "global_agent_id", uuidToString(globalAgentID))...)
		writeError(w, http.StatusInternalServerError, "failed to update linked agents")
		return db.GlobalAgent{}, nil, false
	}
	return updated, linked, true
}

// publishLinkedAgents broadcasts each agent to its own workspace, except skip
// — the copy an UpdateAgent request broadcasts itself.
func (h *Handler) publishLinkedAgents(r *http.Request, eventType string, agents []db.Agent, skip pgtype.UUID) {
	userID := requestUserID(r)
	for _, a := range agents {
		if skip.Valid && a.ID == skip {
			continue
		}
		resp := h.agentToResponse(a)
		if err := h.attachAgentSkills(r.Context(), &resp, a.ID); err != nil {
			slog.Warn("global agent: load skills for broadcast failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(a.ID))...)
		}
		wsID := uuidToString(a.WorkspaceID)
		actorType, actorID := h.resolveActor(r, userID, wsID)
		h.publish(eventType, wsID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	}
}

// failGlobalAgent logs err with the request and answers 500 with msg.
func failGlobalAgent(w http.ResponseWriter, r *http.Request, msg string, err error) {
	slog.Warn("global agent: "+msg, append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, msg)
}

// lockMembershipForLink serializes linking an agent in a workspace with the
// caller's removal from it. revokeAndRemoveMember takes the same
// (workspace, user) lock before it unlinks the leaving member's copies, so
// taking it first here — before any row lock, in the same order — means a link
// either commits before the unlink sees it, or sees the member already gone.
// ok=false means the error response is written.
func (h *Handler) lockMembershipForLink(w http.ResponseWriter, r *http.Request, qtx *db.Queries, workspaceID, userID pgtype.UUID) bool {
	if err := qtx.LockSubscriberWrites(r.Context(), db.LockSubscriberWritesParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		failGlobalAgent(w, r, "failed to lock workspace membership", err)
		return false
	}
	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return false
		}
		failGlobalAgent(w, r, "failed to check workspace membership", err)
		return false
	}
	return true
}

// requireHumanUser backs up RequireHumanActor inside the global agent
// handlers: a global agent reaches into every workspace its owner belongs to,
// so no machine credential may manage one even if a route loses the
// middleware.
func requireHumanUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return "", false
	}
	return requireUserID(w, r)
}

func isUniqueConstraint(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// loadGlobalAgentForOwner resolves the {id} path param to one of the caller's
// global agents. Another user's global agent is indistinguishable from a
// missing one.
func (h *Handler) loadGlobalAgentForOwner(w http.ResponseWriter, r *http.Request) (db.GlobalAgent, bool) {
	userID, ok := requireHumanUser(w, r)
	if !ok {
		return db.GlobalAgent{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "global agent id")
	if !ok {
		return db.GlobalAgent{}, false
	}
	g, err := h.Queries.GetGlobalAgentForOwner(r.Context(), db.GetGlobalAgentForOwnerParams{ID: id, OwnerID: parseUUID(userID)})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "global agent not found")
			return db.GlobalAgent{}, false
		}
		slog.Warn("load global agent failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load global agent")
		return db.GlobalAgent{}, false
	}
	return g, true
}

func (h *Handler) ListGlobalAgents(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireHumanUser(w, r)
	if !ok {
		return
	}
	ownerID := parseUUID(userID)
	globals, err := h.Queries.ListGlobalAgentsByOwner(r.Context(), ownerID)
	if err != nil {
		slog.Warn("list global agents failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list global agents")
		return
	}
	links, err := h.globalAgentLinks(r.Context(), ownerID, pgtype.UUID{})
	if err != nil {
		slog.Warn("list global agents: load links failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load global agent workspaces")
		return
	}
	resp := make([]GlobalAgentResponse, len(globals))
	for i, g := range globals {
		resp[i] = h.globalAgentToResponse(g, links[uuidToString(g.ID)])
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetGlobalAgent(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	h.writeGlobalAgent(w, r, http.StatusOK, g)
}

func (h *Handler) CreateGlobalAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireHumanUser(w, r)
	if !ok {
		return
	}
	var req CreateGlobalAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	f, ok := h.parseGlobalAgentFields(w, r, UpdateGlobalAgentRequest{
		Name:                 &req.Name,
		Description:          &req.Description,
		Instructions:         &req.Instructions,
		AvatarURL:            req.AvatarURL,
		ConversationStarters: &req.ConversationStarters,
	}, "")
	if !ok {
		return
	}
	if f.AvatarURL.String == "" {
		f.AvatarURL = pgtype.Text{String: randomAgentEmojiAvatar(), Valid: true}
	}

	created, err := h.Queries.CreateGlobalAgent(r.Context(), db.CreateGlobalAgentParams{
		OwnerID:              parseUUID(userID),
		Name:                 f.Name.String,
		Description:          f.Description.String,
		Instructions:         f.Instructions.String,
		AvatarUrl:            f.AvatarURL,
		ConversationStarters: f.ConversationStarters,
	})
	if err != nil {
		if isUniqueConstraint(err, globalAgentOwnerNameIndex) {
			writeError(w, http.StatusConflict, fmt.Sprintf("you already have a global agent named %q", req.Name))
			return
		}
		slog.Warn("create global agent failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create global agent")
		return
	}
	slog.Info("global agent created", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(created.ID))...)
	writeJSON(w, http.StatusCreated, h.globalAgentToResponse(created, nil))
}

func (h *Handler) UpdateGlobalAgent(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	var req UpdateGlobalAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	f, ok := h.parseGlobalAgentFields(w, r, req, g.AvatarUrl.String)
	if !ok {
		return
	}
	f = f.changedFrom(g.Name, g.Description, g.Instructions, g.AvatarUrl, g.ConversationStarters)
	if f.empty() {
		h.writeGlobalAgent(w, r, http.StatusOK, g)
		return
	}
	updated, ok := h.applyGlobalAgentUpdate(w, r, g.ID, g.OwnerID, f)
	if !ok {
		return
	}
	h.writeGlobalAgent(w, r, http.StatusOK, updated)
}

// DeleteGlobalAgent removes the definition and keeps every workspace copy as a
// regular agent: an agent may be assigned issues, hold chats and run
// autopilots, and none of that should disappear because the shared definition
// did. Archiving the copies stays an explicit per-workspace action.
func (h *Handler) DeleteGlobalAgent(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		failGlobalAgent(w, r, "failed to start global agent delete", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockGlobalAgentForOwner(ctx, db.LockGlobalAgentForOwnerParams{ID: g.ID, OwnerID: g.OwnerID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "global agent not found")
			return
		}
		failGlobalAgent(w, r, "failed to load global agent", err)
		return
	}
	unlinked, err := qtx.UnlinkAgentsFromGlobalAgent(ctx, g.ID)
	if err != nil {
		failGlobalAgent(w, r, "failed to unlink workspace agents", err)
		return
	}
	if err := qtx.DeleteGlobalAgent(ctx, db.DeleteGlobalAgentParams{ID: g.ID, OwnerID: g.OwnerID}); err != nil {
		failGlobalAgent(w, r, "failed to delete global agent", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		failGlobalAgent(w, r, "failed to commit global agent delete", err)
		return
	}
	slog.Info("global agent deleted", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(g.ID), "unlinked_agents", len(unlinked))...)
	h.publishLinkedAgents(r, protocol.EventAgentStatus, unlinked, pgtype.UUID{})
	w.WriteHeader(http.StatusNoContent)
}

// ListGlobalAgentWorkspaces returns every workspace the caller belongs to, with
// the global agent's state there and the runtimes the caller may bind it to.
func (h *Handler) ListGlobalAgentWorkspaces(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	userID := requestUserID(r)
	userUUID := parseUUID(userID)

	workspaces, err := h.Queries.ListWorkspaces(ctx, userUUID)
	if err != nil {
		failGlobalAgent(w, r, "failed to list workspaces", err)
		return
	}
	linked, err := h.Queries.ListAgentsByGlobalAgent(ctx, g.ID)
	if err != nil {
		failGlobalAgent(w, r, "failed to list linked agents", err)
		return
	}
	linkedByWorkspace := make(map[string]db.Agent, len(linked))
	var linkedRuntimeIDs []pgtype.UUID
	for _, a := range linked {
		linkedByWorkspace[uuidToString(a.WorkspaceID)] = a
		if a.RuntimeID.Valid && !a.ArchivedAt.Valid {
			linkedRuntimeIDs = append(linkedRuntimeIDs, a.RuntimeID)
		}
	}
	// The machine + provider the owner already runs this agent on elsewhere is
	// the best guess for a new workspace: one daemon registers one runtime per
	// workspace and provider.
	preferred := map[string]bool{}
	if len(linkedRuntimeIDs) > 0 {
		runtimes, err := h.Queries.GetAgentRuntimes(ctx, linkedRuntimeIDs)
		if err != nil {
			failGlobalAgent(w, r, "failed to load runtimes", err)
			return
		}
		for _, rt := range runtimes {
			if key := runtimeMachineKey(rt); key != "" {
				preferred[key] = true
			}
		}
	}

	workspaceIDs := make([]pgtype.UUID, len(workspaces))
	for i, ws := range workspaces {
		workspaceIDs[i] = ws.ID
	}
	visible, err := h.Queries.ListUsableRuntimesForUserInWorkspaces(ctx, db.ListUsableRuntimesForUserInWorkspacesParams{
		WorkspaceIds: workspaceIDs,
		OwnerID:      userUUID,
	})
	if err != nil {
		failGlobalAgent(w, r, "failed to list runtimes", err)
		return
	}
	runtimesByWorkspace := make(map[string][]db.AgentRuntime, len(workspaces))
	for _, rt := range visible {
		key := uuidToString(rt.WorkspaceID)
		runtimesByWorkspace[key] = append(runtimesByWorkspace[key], rt)
	}

	// canUseRuntimeForAgent only reads the member's user id.
	caller := db.Member{UserID: userUUID}
	resp := make([]GlobalAgentWorkspaceTarget, 0, len(workspaces))
	for _, ws := range workspaces {
		runtimes := runtimesByWorkspace[uuidToString(ws.ID)]
		usable := make([]db.AgentRuntime, 0, len(runtimes))
		options := make([]GlobalAgentRuntimeOption, 0, len(runtimes))
		for _, rt := range runtimes {
			if !canUseRuntimeForAgent(caller, rt) {
				continue
			}
			usable = append(usable, rt)
			name := rt.Name
			if rt.CustomName.Valid && rt.CustomName.String != "" {
				name = rt.CustomName.String
			}
			options = append(options, GlobalAgentRuntimeOption{
				ID:        uuidToString(rt.ID),
				Name:      name,
				Provider:  rt.Provider,
				Status:    rt.Status,
				OwnedByMe: uuidToString(rt.OwnerID) == userID,
			})
		}
		target := GlobalAgentWorkspaceTarget{
			WorkspaceID:        uuidToString(ws.ID),
			WorkspaceName:      ws.Name,
			WorkspaceSlug:      ws.Slug,
			Runtimes:           options,
			SuggestedRuntimeID: suggestGlobalAgentRuntime(usable, userID, preferred),
		}
		if a, ok := linkedByWorkspace[target.WorkspaceID]; ok {
			target.Agent = &GlobalAgentWorkspaceAgent{
				ID:        uuidToString(a.ID),
				Archived:  a.ArchivedAt.Valid,
				RuntimeID: uuidToString(a.RuntimeID),
			}
		}
		resp = append(resp, target)
	}
	writeJSON(w, http.StatusOK, resp)
}

// runtimeMachineKey identifies "this provider on this machine" across
// workspaces. Empty for runtimes without a daemon, which share no machine.
func runtimeMachineKey(rt db.AgentRuntime) string {
	if !rt.DaemonID.Valid || rt.DaemonID.String == "" {
		return ""
	}
	return rt.DaemonID.String + "\x00" + rt.Provider + "\x00" + uuidToString(rt.ProfileID)
}

// suggestGlobalAgentRuntime picks the runtime to preselect: the same machine
// and provider the agent already runs on elsewhere, then online, then the
// caller's own. Ties keep list order (oldest first). Empty when none.
func suggestGlobalAgentRuntime(runtimes []db.AgentRuntime, userID string, preferred map[string]bool) string {
	best, bestScore := "", -1
	for _, rt := range runtimes {
		score := 0
		if key := runtimeMachineKey(rt); key != "" && preferred[key] {
			score += 4
		}
		if rt.Status == "online" {
			score += 2
		}
		if uuidToString(rt.OwnerID) == userID {
			score++
		}
		if score > bestScore {
			best, bestScore = uuidToString(rt.ID), score
		}
	}
	return best
}

// EnableGlobalAgentInWorkspace creates the global agent's copy in a workspace,
// or restores the copy the owner disabled there before.
func (h *Handler) EnableGlobalAgentInWorkspace(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	userID := requestUserID(r)

	var req EnableGlobalAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	workspaceID := uuidToString(wsUUID)
	member, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found")
	if !ok {
		return
	}
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner can create agents on it")
		return
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		failGlobalAgent(w, r, "failed to start global agent enable", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if !h.lockMembershipForLink(w, r, qtx, wsUUID, g.OwnerID) {
		return
	}
	// Locking the global row serializes this with edits, so the copy is
	// created from the definition an in-flight edit is about to commit.
	g, err = qtx.LockGlobalAgentForOwner(ctx, db.LockGlobalAgentForOwnerParams{ID: g.ID, OwnerID: g.OwnerID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "global agent not found")
			return
		}
		failGlobalAgent(w, r, "failed to load global agent", err)
		return
	}

	var result db.Agent
	restored := false
	existing, err := qtx.GetAgentByGlobalAgentAndWorkspace(ctx, db.GetAgentByGlobalAgentAndWorkspaceParams{
		GlobalAgentID: g.ID,
		WorkspaceID:   wsUUID,
	})
	switch {
	case err == nil:
		if !existing.ArchivedAt.Valid {
			writeError(w, http.StatusConflict, "this global agent is already enabled in the workspace")
			return
		}
		result, ok = h.restoreLinkedAgent(w, r, qtx, existing, runtime)
		if !ok {
			return
		}
		restored = true
	case isNotFound(err):
		created, err := qtx.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:          wsUUID,
			Name:                 g.Name,
			Description:          g.Description,
			Instructions:         g.Instructions,
			AvatarUrl:            g.AvatarUrl,
			RuntimeMode:          runtime.RuntimeMode,
			RuntimeConfig:        []byte("{}"),
			RuntimeID:            runtime.ID,
			Visibility:           "private",
			PermissionMode:       permissionModePrivate,
			MaxConcurrentTasks:   agentconfig.DefaultMaxConcurrentTasks,
			OwnerID:              g.OwnerID,
			CustomEnv:            []byte("{}"),
			CustomArgs:           []byte("[]"),
			ConversationStarters: g.ConversationStarters,
		})
		if err != nil {
			if isUniqueConstraint(err, agentWorkspaceNameUnique) {
				writeError(w, http.StatusConflict, fmt.Sprintf("an agent named %q already exists in this workspace", g.Name))
				return
			}
			slog.Warn("enable global agent: create agent failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create agent")
			return
		}
		result, err = qtx.LinkAgentToGlobalAgent(ctx, db.LinkAgentToGlobalAgentParams{ID: created.ID, GlobalAgentID: g.ID})
		if err != nil {
			failGlobalAgent(w, r, "failed to link agent", err)
			return
		}
	default:
		failGlobalAgent(w, r, "failed to load linked agent", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		failGlobalAgent(w, r, "failed to commit global agent enable", err)
		return
	}
	slog.Info("global agent enabled", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(g.ID), "agent_id", uuidToString(result.ID), "workspace_id", workspaceID, "restored", restored)...)

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(ctx, result.ID)
		if reloaded, err := h.Queries.GetAgent(ctx, result.ID); err == nil {
			result = reloaded
		}
	}

	resp := h.agentToResponse(result)
	if err := h.attachAgentSkills(ctx, &resp, result.ID); err != nil {
		slog.Warn("enable global agent: load skills failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(result.ID))...)
	}
	if err := h.enrichAgentResponseWithTargets(ctx, &resp, result.ID); err != nil {
		slog.Warn("enable global agent: load invocation targets failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(result.ID))...)
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	status := http.StatusCreated
	if restored {
		status = http.StatusOK
		h.publish(protocol.EventAgentRestored, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	} else {
		h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
			userID,
			workspaceID,
			uuidToString(result.ID),
			runtime.Provider,
			runtime.RuntimeMode,
			"global_agent",
			false,
		))
	}
	if !h.composioMCPAppsEnabled(ctx) {
		suppressComposioToolkitAllowlist(&resp)
	}
	writeJSON(w, status, resp)
}

// restoreLinkedAgent un-archives a disabled copy and binds it to the chosen
// runtime. Moving to another provider drops the runtime-native model,
// thinking level and service tier, which the new runtime may not serve.
func (h *Handler) restoreLinkedAgent(w http.ResponseWriter, r *http.Request, qtx *db.Queries, existing db.Agent, runtime db.AgentRuntime) (db.Agent, bool) {
	ctx := r.Context()
	restored, err := qtx.RestoreAgent(ctx, existing.ID)
	if err != nil {
		failGlobalAgent(w, r, "failed to restore agent", err)
		return db.Agent{}, false
	}
	if restored.RuntimeID == runtime.ID {
		return restored, true
	}
	providerChanged := true
	if restored.RuntimeID.Valid {
		if previous, err := qtx.GetAgentRuntime(ctx, restored.RuntimeID); err == nil {
			providerChanged = previous.Provider != runtime.Provider
		}
	}
	params := db.UpdateAgentParams{
		ID:          restored.ID,
		RuntimeID:   runtime.ID,
		RuntimeMode: pgtype.Text{String: runtime.RuntimeMode, Valid: true},
	}
	if providerChanged && restored.Model.Valid && agent.ModelKnownIncompatibleWithProvider(runtime.Provider, restored.Model.String) {
		params.Model = pgtype.Text{String: "", Valid: true}
	}
	rebound, err := qtx.UpdateAgent(ctx, params)
	if err != nil {
		failGlobalAgent(w, r, "failed to bind agent to runtime", err)
		return db.Agent{}, false
	}
	if providerChanged {
		if rebound.ThinkingLevel.Valid {
			if rebound, err = qtx.ClearAgentThinkingLevel(ctx, rebound.ID); err != nil {
				failGlobalAgent(w, r, "failed to clear thinking_level", err)
				return db.Agent{}, false
			}
		}
		if rebound.ServiceTier.Valid {
			if rebound, err = qtx.ClearAgentServiceTier(ctx, rebound.ID); err != nil {
				failGlobalAgent(w, r, "failed to clear service_tier", err)
				return db.Agent{}, false
			}
		}
	}
	return rebound, true
}

// DisableGlobalAgentInWorkspace archives the global agent's copy in one
// workspace. Archiving (not deleting) keeps its issues, chats and history, and
// enabling it again restores the same row.
func (h *Handler) DisableGlobalAgentInWorkspace(w http.ResponseWriter, r *http.Request) {
	g, ok := h.loadGlobalAgentForOwner(w, r)
	if !ok {
		return
	}
	userID := requestUserID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "workspaceId"), "workspace id")
	if !ok {
		return
	}
	workspaceID := uuidToString(wsUUID)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	linked, err := h.Queries.GetAgentByGlobalAgentAndWorkspace(r.Context(), db.GetAgentByGlobalAgentAndWorkspaceParams{
		GlobalAgentID: g.ID,
		WorkspaceID:   wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "this global agent is not enabled in the workspace")
			return
		}
		failGlobalAgent(w, r, "failed to load linked agent", err)
		return
	}
	if uuidToString(linked.OwnerID) != userID {
		writeError(w, http.StatusForbidden, "only the agent owner can manage this agent")
		return
	}
	if linked.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "this global agent is already disabled in the workspace")
		return
	}
	archived, err := h.Queries.ArchiveAgent(r.Context(), db.ArchiveAgentParams{
		ID:         linked.ID,
		ArchivedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("disable global agent: archive failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(linked.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to archive agent")
		return
	}
	if _, err := h.TaskService.CancelTasksForArchivedAgent(r.Context(), linked.ID); err != nil {
		slog.Warn("disable global agent: cancel tasks failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(linked.ID))...)
	}
	slog.Info("global agent disabled", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(g.ID), "agent_id", uuidToString(linked.ID), "workspace_id", workspaceID)...)

	resp := h.agentToResponse(archived)
	if err := h.attachAgentSkills(r.Context(), &resp, archived.ID); err != nil {
		slog.Warn("disable global agent: load skills failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(archived.ID))...)
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventAgentArchived, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	if !h.composioMCPAppsEnabled(r.Context()) {
		suppressComposioToolkitAllowlist(&resp)
	}
	writeJSON(w, http.StatusOK, resp)
}

// MakeAgentGlobal turns an existing workspace agent into a global agent: the
// definition is created from the agent's synced fields and the agent becomes
// its first linked copy. Owner-only — the owner is who the global agent will
// belong to, and an admin must not move someone else's agent into their own
// account.
func (h *Handler) MakeAgentGlobal(w http.ResponseWriter, r *http.Request) {
	loaded, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireHumanUser(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		failGlobalAgent(w, r, "failed to start make-global", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if !h.lockMembershipForLink(w, r, qtx, loaded.WorkspaceID, parseUUID(userID)) {
		return
	}
	// Re-read under a row lock so the definition is built from the agent as
	// it is when it gets linked, not as an earlier read saw it.
	source, err := qtx.GetAgentForUpdate(ctx, loaded.ID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		failGlobalAgent(w, r, "failed to load agent", err)
		return
	}
	switch {
	case uuidToString(source.OwnerID) != userID:
		writeError(w, http.StatusForbidden, "only the agent owner can make this agent global")
		return
	case source.SystemKey.Valid && source.SystemKey.String != "":
		writeError(w, http.StatusBadRequest, "this agent is built into Multica and cannot be made global")
		return
	case source.ArchivedAt.Valid:
		writeError(w, http.StatusBadRequest, "an archived agent cannot be made global")
		return
	case source.GlobalAgentID.Valid:
		writeError(w, http.StatusConflict, "this agent is already linked to a global agent")
		return
	}

	created, err := qtx.CreateGlobalAgent(ctx, db.CreateGlobalAgentParams{
		OwnerID:              source.OwnerID,
		Name:                 source.Name,
		Description:          source.Description,
		Instructions:         source.Instructions,
		AvatarUrl:            source.AvatarUrl,
		ConversationStarters: source.ConversationStarters,
	})
	if err != nil {
		if isUniqueConstraint(err, globalAgentOwnerNameIndex) {
			writeError(w, http.StatusConflict, fmt.Sprintf("you already have a global agent named %q", source.Name))
			return
		}
		failGlobalAgent(w, r, "failed to create global agent", err)
		return
	}
	linked, err := qtx.LinkAgentToGlobalAgent(ctx, db.LinkAgentToGlobalAgentParams{ID: source.ID, GlobalAgentID: created.ID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusConflict, "this agent is already linked to a global agent")
			return
		}
		failGlobalAgent(w, r, "failed to link agent", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		failGlobalAgent(w, r, "failed to commit make-global", err)
		return
	}
	slog.Info("agent made global", append(logger.RequestAttrs(r), "global_agent_id", uuidToString(created.ID), "agent_id", uuidToString(linked.ID))...)
	h.publishLinkedAgents(r, protocol.EventAgentStatus, []db.Agent{linked}, pgtype.UUID{})
	h.writeGlobalAgent(w, r, http.StatusCreated, created)
}
