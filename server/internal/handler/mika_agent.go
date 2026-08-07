package handler

import (
	"context"
	"errors"
	"strings"

	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"log/slog"
	"net/http"
)

// Mika's product-defined identity. These are server constants on purpose: the
// public CreateAgent API accepts neither `kind` nor `system_key`, so a client
// cannot mint an agent that would receive the system instruction layer or pass
// the onboarding gate. Letting it would both forge a "Mika" and break the
// one-per-workspace invariant.
const (
	mikaAgentMaxConcurrency = 3
	mikaAgentVisibility     = "workspace"
	mikaAgentPermissionMode = "public_to"
	// Placeholder until Mika has real artwork. Uses the same `emoji:` marker
	// every other agent avatar uses, so ActorAvatar renders it as text and no
	// surface needs to special-case her. The previous value was a hand-rolled
	// data-URI SVG, which only that one constant knew how to produce.
	mikaAgentAvatarURL = agentEmojiAvatarPrefix + "🦄"
)

// mikaAgentDescriptions is user-facing copy, so it is localized. Unlike the
// instructions it is stored on the row: description is an owner-editable field
// and the product does not reclaim it after creation.
var mikaAgentDescriptions = map[string]string{
	"en": "Your workspace Chief of Staff. Mika turns goals into issues, coordinates agents, and helps build reusable workflows.",
	"zh": "你的工作区 Chief of Staff。Mika 会把目标转化为任务、协调智能体，并帮你建立可复用的工作流。",
	"ko": "워크스페이스의 Chief of Staff입니다. Mika가 목표를 태스크로 구체화하고 에이전트를 조율하며 재사용 가능한 워크플로 구성을 돕습니다.",
	"ja": "ワークスペースの Chief of Staff。Mika は目標をタスクに落とし込み、エージェントを調整し、再利用できるワークフローづくりを支援します。",
}

type createMikaAgentRequest struct {
	RuntimeID string `json:"runtime_id"`
	Language  string `json:"language"`
	// Model is the runtime model Mika should run on. Optional: empty means
	// "whatever the runtime defaults to", which is what every deployment
	// without per-agent model support gets anyway.
	Model string `json:"model"`
	// SessionTitle names the onboarding conversation when this call is the one
	// that creates it. It is only ever a label: the session's identity is
	// (workspace, member, Mika), so sending a different title later reuses the
	// existing session rather than opening a second one.
	SessionTitle string `json:"session_title"`
}

// mikaAgentResponse is the agent plus the caller's onboarding conversation.
//
// They are returned together because every caller needs both and the pair has
// to be produced atomically. The client used to list sessions and then create
// one if it saw no match, which is an unprotected check-then-insert:
// LockWorkspaceForChatSessionCreate takes FOR KEY SHARE precisely so that
// concurrent session creators do not block each other, so two tabs each
// created a session and each got its own "idempotent" kickoff.
type mikaAgentResponse struct {
	AgentResponse
	OnboardingSession *ChatSessionResponse `json:"onboarding_session,omitempty"`
}

// CreateMikaAgent provisions the workspace's built-in Chief of Staff on the
// given runtime, or returns the existing one.
//
// Idempotency is by system_key inside the workspace, not by name: names are
// owner-editable, and migration 172's unique index covers
// (workspace_id, owner_id, runtime_id, system_key), which would still admit a
// second Mika on a different runtime or under a different owner. The lookup
// below is the invariant that actually holds "one Mika per workspace".
func (h *Handler) CreateMikaAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req createMikaAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	agent, created, ok := h.resolveMikaAgent(w, r, workspaceID, userID, req)
	if !ok {
		return
	}
	// Deliberately outside resolveMikaAgent's transaction: the session
	// get-or-create opens its own, and running it while the workspace
	// provisioning lock is still held would queue every member's session
	// behind a lock that has nothing to do with sessions.
	h.writeMikaAgentResponse(w, r, agent, workspaceID, userID, req.SessionTitle, created)
}

// resolveMikaAgent returns the workspace's Mika — the existing one if there is
// one, otherwise a freshly provisioned one. The bool pair is (created, ok);
// when ok is false it has already written the error response.
func (h *Handler) resolveMikaAgent(w http.ResponseWriter, r *http.Request, workspaceID, userID string, req createMikaAgentRequest) (db.Agent, bool, bool) {
	description, ok := mikaAgentDescriptions[req.Language]
	if !ok {
		writeError(w, http.StatusBadRequest, "language must be en, zh, ko, or ja")
		return db.Agent{}, false, false
	}
	runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return db.Agent{}, false, false
	}
	wsUUID := parseUUID(workspaceID)

	// Already provisioned: hand back the same agent so a retry, a second tab,
	// or a re-run of onboarding cannot produce a duplicate.
	if existing, err := h.Queries.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{
		WorkspaceID: wsUUID,
		SystemKey:   pgtype.Text{String: service.MikaSystemKey, Valid: true},
	}); err == nil {
		return existing, false, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to look up the workspace agent")
		return db.Agent{}, false, false
	}

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Agent{}, false, false
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), runtimeID)
	if err != nil || uuidToString(runtime.WorkspaceID) != workspaceID {
		writeError(w, http.StatusBadRequest, "runtime not found in this workspace")
		return db.Agent{}, false, false
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "you cannot bind an agent to this runtime")
		return db.Agent{}, false, false
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start agent create transaction")
		return db.Agent{}, false, false
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Serialize provisioning per workspace. The check above is only a fast
	// path: without this, two members clicking "Start with Mika" at the same
	// moment both miss and both insert. Migration 172's unique index would not
	// stop them either — it covers (workspace_id, owner_id, runtime_id,
	// system_key), so different owners or different runtimes are distinct
	// tuples, and the workspace would end up with two live Mikas.
	if _, err := tx.Exec(r.Context(),
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"mika:"+workspaceID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock the workspace agent")
		return db.Agent{}, false, false
	}
	// Re-check under the lock: a request that was mid-flight when we started
	// may have committed by now.
	if existing, err := qtx.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{
		WorkspaceID: wsUUID,
		SystemKey:   pgtype.Text{String: service.MikaSystemKey, Valid: true},
	}); err == nil {
		return existing, false, true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to look up the workspace agent")
		return db.Agent{}, false, false
	}

	created, err := qtx.CreateSystemUserAgent(r.Context(), db.CreateSystemUserAgentParams{
		WorkspaceID:        wsUUID,
		Name:               service.MikaDefaultName,
		Description:        description,
		AvatarUrl:          pgtype.Text{String: mikaAgentAvatarURL, Valid: true},
		RuntimeMode:        runtime.RuntimeMode,
		RuntimeID:          runtime.ID,
		Model:              pgtype.Text{String: strings.TrimSpace(req.Model), Valid: strings.TrimSpace(req.Model) != ""},
		Visibility:         mikaAgentVisibility,
		PermissionMode:     mikaAgentPermissionMode,
		MaxConcurrentTasks: mikaAgentMaxConcurrency,
		OwnerID:            parseUUID(userID),
		SystemKey:          pgtype.Text{String: service.MikaSystemKey, Valid: true},
	})
	if err != nil {
		slog.Warn("create mika agent failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create the workspace agent")
		return db.Agent{}, false, false
	}
	// Workspace-invocable: every member may chat with Mika and assign it work.
	if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, created.ID, parseUUID(userID), []targetSpec{
		{targetType: invocationTargetWorkspace, targetID: wsUUID},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent access")
		return db.Agent{}, false, false
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit agent create")
		return db.Agent{}, false, false
	}
	slog.Info("mika agent created", append(logger.RequestAttrs(r), "agent_id", uuidToString(created.ID), "workspace_id", workspaceID)...)

	if runtime.Status == "online" {
		h.TaskService.ReconcileAgentStatus(r.Context(), created.ID)
		created, _ = h.Queries.GetAgent(r.Context(), created.ID)
	}
	return created, true, true
}

func (h *Handler) writeMikaAgentResponse(w http.ResponseWriter, r *http.Request, agent db.Agent, workspaceID, userID, sessionTitle string, created bool) {
	resp := h.agentToResponse(agent)
	if err := h.enrichAgentResponseWithTargets(r.Context(), &resp, agent.ID); err != nil {
		slog.Warn("mika agent: load invocation targets failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
	}

	// Announced before the session step can fail, because the agent itself is
	// already committed and other clients' agent lists are stale until they
	// hear about it.
	if created {
		actorType, actorID := h.resolveActor(r, uuidToString(agent.OwnerID), workspaceID)
		h.publish(protocol.EventAgentCreated, workspaceID, actorType, actorID, map[string]any{"agent": broadcastAgentResponse(resp)})
	}

	session, err := h.getOrCreateMikaSession(r.Context(), agent, workspaceID, userID, sessionTitle)
	if err != nil {
		// Reporting success with the session omitted was worse than failing:
		// the caller cannot tell a partial bootstrap from a complete one, and
		// the surfaces that offer to finish it key off the agent, which now
		// exists. Every step here is idempotent, so a retry converges — say so
		// with an error rather than handing back a half-built flow.
		slog.Warn("mika agent: get-or-create onboarding session failed", append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to open the Mika conversation")
		return
	}
	sessionResp := chatSessionToResponse(session)
	out := mikaAgentResponse{AgentResponse: resp, OnboardingSession: &sessionResp}

	if created {
		writeJSON(w, http.StatusCreated, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getOrCreateMikaSession returns the caller's Mika conversation, creating it
// only if they do not have one yet.
//
// Serialized per (workspace, member) by an advisory lock rather than by a
// unique index, because chat_session has no uniqueness constraint to lean on
// and the repository does not add database FKs or indexes casually. The lookup
// is by (workspace, creator, agent) — deliberately not by title, which is
// localized: changing language between a failed attempt and its retry used to
// produce a second onboarding conversation with its own kickoff task.
func (h *Handler) getOrCreateMikaSession(ctx context.Context, agent db.Agent, workspaceID, userID, title string) (db.ChatSession, error) {
	wsUUID := parseUUID(workspaceID)
	creatorUUID := parseUUID(userID)

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ChatSession{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	if _, err := tx.Exec(ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"mika-session:"+workspaceID+":"+userID,
	); err != nil {
		return db.ChatSession{}, err
	}
	// Same workspace delete/create protocol every other session creator uses
	// (#5219): FOR KEY SHARE here conflicts with DeleteWorkspace's FOR UPDATE.
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, wsUUID); err != nil {
		return db.ChatSession{}, err
	}

	existing, err := qtx.GetOldestActiveChatSessionForCreatorAgent(ctx, db.GetOldestActiveChatSessionForCreatorAgentParams{
		WorkspaceID: wsUUID,
		CreatorID:   creatorUUID,
		AgentID:     agent.ID,
	})
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return db.ChatSession{}, err
		}
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChatSession{}, err
	}

	created, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: wsUUID,
		AgentID:     agent.ID,
		CreatorID:   creatorUUID,
		Title:       strings.TrimSpace(title),
	})
	if err != nil {
		return db.ChatSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChatSession{}, err
	}
	return created, nil
}

// systemInstructionsFor exposes the product-owned half of a system agent's
// prompt so the UI can show it read-only. Ordinary agents get "" — their whole
// prompt is already in the editable Instructions field.
func systemInstructionsFor(a db.Agent) string {
	if a.SystemKey.String == service.MikaSystemKey {
		return service.MikaSystemInstructions(a.Name)
	}
	return ""
}
