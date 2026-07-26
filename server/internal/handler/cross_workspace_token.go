package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// crossWorkspaceTokenActivityMinted is the activity_log `action` constant
// written to the TARGET workspace every time a cross-workspace task token
// is minted (BUS-171) — the queryable trail of which agent reached into
// which other workspace, and when.
const crossWorkspaceTokenActivityMinted = "agent_cross_workspace_token_minted"

// crossWorkspaceTokenTTL matches the expiry the daemon uses when minting an
// agent's primary task token at claim time (see ClaimTasksByRuntime in
// daemon.go) so a cross-workspace token never outlives that convention.
const crossWorkspaceTokenTTL = 24 * time.Hour

// MintCrossWorkspaceTokenRequest is the wire shape for
// POST /api/tokens/cross-workspace.
type MintCrossWorkspaceTokenRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

// MintCrossWorkspaceTokenResponse returns the newly minted mat_ token. The
// CLI's existing marker-based guard (cmd_agent.go) already accepts any
// mat_-prefixed token inside a daemon-managed task, so no CLI-side change
// is needed to use it.
type MintCrossWorkspaceTokenResponse struct {
	Token       string `json:"token"`
	WorkspaceID string `json:"workspace_id"`
	ExpiresAt   string `json:"expires_at"`
}

// MintCrossWorkspaceToken lets a running agent task request a second,
// scoped mat_ task token for a DIFFERENT workspace, replacing the
// pre-0.4.11 "unset MULTICA_TOKEN/MULTICA_WORKSPACE_ID and fall back to
// the owner's config-file token" mechanism that 0.4.11's daemon-task-marker
// guard correctly closed (that fallback was the exact vulnerability class
// MUL-2600 closed for the primary-workspace case; re-opening it here via a
// CLI override flag would have been the same hole with extra steps).
//
// Why this is safe to add with zero changes to the existing auth guard:
// the new token is a normal task_token row — same table, same shape, same
// (task_id, agent_id, user_id) triple as the agent's primary token, just
// bound to a different workspace_id. Every downstream authorization check
// (middleware/auth.go's mat_ branch, middleware/workspace.go's
// X-Actor-Source == "task_token" scoping) already treats the token row as
// the sole source of truth, so a second row scoped to workspace B is
// indistinguishable, to the rest of the server, from a token minted for
// workspace B by the normal claim path. It shares task_id with the
// primary token, so task cancellation's existing
// DeleteTaskTokensByTask cleanup revokes it for free.
//
// Access is gated three ways before a token is minted:
//  1. Actor must be a genuine task_token caller (X-Actor-Source ==
//     "task_token") — not a human PAT, not a cloud-node PAT, and not the
//     resolveActor legacy-header fallback (which a compromised agent
//     process could try to forge). This header can only be set by the
//     Auth middleware itself.
//  2. The target workspace must be present on the calling agent's
//     owner/admin-configured cross_workspace_ids allow-list.
//  3. The task's owning user must actually be a member of the target
//     workspace (defense-in-depth: a stale or misconfigured grant can
//     never mint a token into a workspace the owner has no membership
//     in — every existing workspace-scoped handler still enforces
//     membership independently, but this fails the request closed at
//     the point of mint rather than at first use).
//
// Persist + audit run in one transaction, same fail-closed reasoning as
// UpdateAgentEnv / UpdateAgentCrossWorkspaceGrants: an audit-write outage
// cannot leave an unaudited cross-workspace token in the caller's hands.
func (h *Handler) MintCrossWorkspaceToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "this endpoint is only available to a running agent task")
		return
	}

	agentID := r.Header.Get("X-Agent-ID")
	taskID := r.Header.Get("X-Task-ID")
	originWorkspaceID := r.Header.Get("X-Workspace-ID")
	userID := r.Header.Get("X-User-ID")

	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent id")
	if !ok {
		return
	}
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	var req MintCrossWorkspaceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetWorkspaceUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	targetWorkspaceID := uuidToString(targetWorkspaceUUID)
	if targetWorkspaceID == originWorkspaceID {
		writeError(w, http.StatusBadRequest, "workspace_id must differ from the task's own workspace")
		return
	}

	agentRow, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		writeError(w, http.StatusForbidden, "agent not found")
		return
	}

	granted := false
	for _, id := range agentRow.CrossWorkspaceIds {
		if uuidToString(id) == targetWorkspaceID {
			granted = true
			break
		}
	}
	if !granted {
		writeError(w, http.StatusForbidden, "agent is not granted access to workspace "+targetWorkspaceID)
		return
	}

	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: targetWorkspaceUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "task owner is not a member of workspace "+targetWorkspaceID)
		return
	}

	tokenStr, err := auth.GenerateAgentTaskToken()
	if err != nil {
		slog.Error("mint cross-workspace token: generate token failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", agentID, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to mint cross-workspace token")
		return
	}
	expiresAt := time.Now().Add(crossWorkspaceTokenTTL)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("mint cross-workspace token: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", agentID, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to mint cross-workspace token")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.CreateTaskToken(r.Context(), db.CreateTaskTokenParams{
		TokenHash:   auth.HashToken(tokenStr),
		TaskID:      taskUUID,
		AgentID:     agentUUID,
		WorkspaceID: targetWorkspaceUUID,
		UserID:      userUUID,
		ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		slog.Error("mint cross-workspace token: create task_token failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", agentID, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to mint cross-workspace token")
		return
	}

	details, _ := json.Marshal(map[string]any{
		"agent_id":            agentID,
		"task_id":             taskID,
		"origin_workspace_id": originWorkspaceID,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: targetWorkspaceUUID,
		IssueID:     pgtype.UUID{}, // token minting is not tied to an issue
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     agentUUID,
		Action:      crossWorkspaceTokenActivityMinted,
		Details:     details,
	}); err != nil {
		slog.Error("agent_cross_workspace_token_minted audit write failed; refusing to hand out token",
			append(logger.RequestAttrs(r), "error", err, "agent_id", agentID, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; refusing to mint token without a recorded trail")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("mint cross-workspace token: tx commit failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", agentID, "task_id", taskID)...)
		writeError(w, http.StatusInternalServerError, "failed to mint cross-workspace token")
		return
	}

	writeJSON(w, http.StatusOK, MintCrossWorkspaceTokenResponse{
		Token:       tokenStr,
		WorkspaceID: targetWorkspaceID,
		ExpiresAt:   expiresAt.UTC().Format(time.RFC3339),
	})
}
