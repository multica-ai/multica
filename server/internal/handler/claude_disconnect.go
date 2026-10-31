package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/claudemanagedruntime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DisconnectClaudeConnection removes the workspace's Claude Managed Agents
// connection for good, in the same order as DisconnectQoderConnection: stop
// the bridge, cancel remote sessions while the key still exists, then delete
// the connection and its managed runtime, unbinding the runtime's agents.
func (h *Handler) DisconnectClaudeConnection(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	user, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req disconnectQoderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expected, ok := parseExpectedActiveAgentIDs(req.ExpectedActiveAgentIDs)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_active_agent_ids must be a list of valid UUIDs")
		return
	}
	wsID := uuidToString(ws)

	if !h.stopClaudeForDisconnect(w, r, ws, expected) {
		return
	}
	if err := h.cleanupClaudeJournal(r.Context(), wsID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("claude disconnect waiting on remote cleanup", "workspace_id", wsID, "error", err)
		writeError(w, http.StatusConflict, "Claude Managed Agents is stopped and still cancelling remote sessions; retry shortly")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, wsID, claudeConnectionLockSeed); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot lock Claude Managed Agents configuration")
		return
	}
	qtx := h.Queries.WithTx(tx)
	runtimeIDs, refused := h.checkClaudeDisconnectPlan(w, r.Context(), tx, qtx, ws, expected)
	if refused {
		return
	}
	teardowns := make([]service.RuntimeTeardownResult, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		teardown, err := service.TeardownRuntime(r.Context(), qtx, runtimeID, service.RuntimeTeardownOptions{CancelNonTerminalTasks: true})
		if err != nil {
			if errors.Is(err, service.ErrRuntimeNotDrained) {
				writeError(w, http.StatusConflict, "Claude Managed Agents still has tasks in flight; retry in a moment")
				return
			}
			slog.Error("claude disconnect teardown failed", "runtime_id", uuidToString(runtimeID), "error", err)
			writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
			return
		}
		if err := qtx.DeleteAgentRuntime(r.Context(), runtimeID); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
			return
		}
		teardowns = append(teardowns, teardown)
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1 AND journal_encrypted=''`, ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "Claude Managed Agents is stopped and still cancelling remote sessions; retry shortly")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
		return
	}
	for i, runtimeID := range runtimeIDs {
		h.NotifyRuntimeGone(uuidToString(runtimeID))
		h.publishRuntimeTeardown(r.Context(), teardowns[i], wsID, user)
	}
	slog.Info("claude managed agents disconnected", "workspace_id", wsID, "disconnected_by", user, "runtimes_removed", len(runtimeIDs))
	h.GetClaudeConnection(w, r)
}

// stopClaudeForDisconnect disables the connection and revokes the bridge's
// credential after the stop checks plus the confirmed agent plan. It reports
// whether the caller may continue; on false a response was written.
func (h *Handler) stopClaudeForDisconnect(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, expected map[string]struct{}) bool {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Claude Managed Agents")
		return false
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, uuidToString(ws), claudeConnectionLockSeed); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot lock Claude Managed Agents configuration")
		return false
	}
	var exists, busy bool
	if err := tx.QueryRow(r.Context(), `SELECT true, COALESCE(busy_until>now(),false) FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, ws).Scan(&exists, &busy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Claude Managed Agents is not connected")
			return false
		}
		writeError(w, http.StatusInternalServerError, "cannot check Claude Managed Agents connection")
		return false
	}
	if busy {
		writeError(w, http.StatusConflict, "Claude Managed Agents is synchronizing; retry shortly")
		return false
	}
	if _, refused := h.checkClaudeDisconnectPlan(w, r.Context(), tx, h.Queries.WithTx(tx), ws, expected); refused {
		return false
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='claude_managed' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	if _, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1)`, ws); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot revoke runtime credential")
		return false
	}
	if _, err = tx.Exec(r.Context(), `UPDATE cloud_agent_connection SET enabled=false,revision=revision+1,status='stopped',last_error='' WHERE provider='claude_managed' AND workspace_id=$1`, ws); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stop Claude Managed Agents runtime")
		return false
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stop Claude Managed Agents runtime")
		return false
	}
	if revokedHash != "" {
		h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	}
	return true
}

// checkClaudeDisconnectPlan locks the managed Claude runtime and refuses while
// tasks run on it or when its active agents differ from the confirmed set.
// A standalone Claude bridge's runtime in the same workspace is left alone.
func (h *Handler) checkClaudeDisconnectPlan(w http.ResponseWriter, ctx context.Context, tx pgx.Tx, qtx *db.Queries, ws pgtype.UUID, expected map[string]struct{}) ([]pgtype.UUID, bool) {
	daemonID := claudeManagedDaemonPrefix + uuidToString(ws)
	rows, err := tx.Query(ctx, `SELECT id FROM agent_runtime WHERE workspace_id=$1 AND provider='claude_managed' AND daemon_id=$2 ORDER BY id FOR UPDATE`, ws, daemonID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load Claude Managed Agents runtime")
		return nil, true
	}
	runtimeIDs, err := pgx.CollectRows(rows, pgx.RowTo[pgtype.UUID])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load Claude Managed Agents runtime")
		return nil, true
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='claude_managed' AND rt.daemon_id=$2 AND t.status IN ('running','dispatched'))`, ws, daemonID).Scan(&active); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot check active Claude Managed Agents tasks")
		return nil, true
	}
	if active {
		writeError(w, http.StatusConflict, "wait for or cancel active Claude Managed Agents tasks before disconnecting")
		return nil, true
	}
	var agents []db.Agent
	for _, runtimeID := range runtimeIDs {
		bound, err := qtx.ListActiveAgentsByRuntimeForUpdate(ctx, runtimeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot check Claude Managed Agents agents")
			return nil, true
		}
		agents = append(agents, bound...)
	}
	if !activeAgentSetMatches(agents, expected) {
		body := h.runtimeHasActiveAgentsResponse(agents)
		body["code"] = "runtime_delete_plan_changed"
		body["error"] = "the agents on Claude Managed Agents changed; please review and confirm again."
		writeJSON(w, http.StatusConflict, body)
		return nil, true
	}
	return runtimeIDs, false
}

// cleanupInactiveClaude cancels the remote sessions of retired connections,
// which keep their encrypted journal until cancellation succeeds.
func (h *Handler) cleanupInactiveClaude(ctx context.Context) {
	rows, err := h.DB.Query(ctx, `SELECT c.workspace_id::text FROM cloud_agent_connection c WHERE c.provider='claude_managed' AND c.journal_encrypted<>'' AND (NOT c.enabled OR NOT EXISTS(SELECT 1 FROM member m WHERE m.workspace_id=c.workspace_id AND m.user_id=c.owner_id AND m.role IN ('owner','admin')))`)
	if err != nil {
		return
	}
	var workspaces []string
	for rows.Next() {
		var ws string
		if rows.Scan(&ws) == nil {
			workspaces = append(workspaces, ws)
		}
	}
	rows.Close()
	for _, ws := range workspaces {
		cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_ = h.cleanupClaudeJournal(cleanupCtx, ws)
		cancel()
	}
}

func (h *Handler) cleanupClaudeJournal(ctx context.Context, ws string) error {
	cfg, _, _, _, err := h.readClaudeSettings(ctx, ws)
	if err != nil {
		return err
	}
	var encrypted string
	if err = h.DB.QueryRow(ctx, `SELECT journal_encrypted FROM cloud_agent_connection WHERE provider='claude_managed' AND workspace_id=$1`, ws).Scan(&encrypted); err != nil {
		return err
	}
	if encrypted == "" {
		return nil
	}
	raw, err := h.openVCSSecret(encrypted)
	if err != nil {
		return err
	}
	var state struct {
		Runs []struct {
			TaskID    string `json:"task_id"`
			SessionID string `json:"session_id"`
		} `json:"runs"`
		Cleanup []string `json:"cleanup"`
	}
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return err
	}
	sessions := state.Cleanup
	for _, run := range state.Runs {
		if run.SessionID != "" {
			sessions = append(sessions, run.SessionID)
		}
		id, err := util.ParseUUID(run.TaskID)
		if err != nil {
			return err
		}
		if _, err = h.TaskService.FailTask(ctx, id, "Claude Managed Agents connection stopped", run.SessionID, "", "", "claude_managed_execution", false, "", ""); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	for _, id := range sessions {
		if err = claudemanagedruntime.CancelRemoteSession(ctx, claudemanagedruntime.DefaultAnthropicURL, cfg.AnthropicToken, id); err != nil {
			return fmt.Errorf("remote cleanup pending: %w", err)
		}
	}
	_, err = h.DB.Exec(ctx, `UPDATE cloud_agent_connection SET journal_encrypted='' WHERE provider='claude_managed' AND workspace_id=$1`, ws)
	return err
}

func (h *Handler) prepareClaudeWorkspaceDeletion(ctx context.Context, ws string) error {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, ws, claudeConnectionLockSeed); err != nil {
		return err
	}
	var busy bool
	err = tx.QueryRow(ctx, `UPDATE cloud_agent_connection SET enabled=false,status='stopped' WHERE provider='claude_managed' AND workspace_id=$1 RETURNING COALESCE(busy_until>now(),false)`, ws).Scan(&busy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("remote step still active")
	}
	return h.cleanupClaudeJournal(ctx, ws)
}

// ListClaudeAgents lists the Claude agents of a saved connection so members
// can pick one when they bind a Multica agent.
func (h *Handler) ListClaudeAgents(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	if h.QoderRateLimiter != nil && !h.QoderRateLimiter.Allow(r.Context(), "claude-catalog:"+uuidToString(ws)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Claude catalog request limit reached")
		return
	}
	cfg, _, _, _, err := h.readClaudeSettings(r.Context(), uuidToString(ws))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "connect Claude Managed Agents first")
		return
	}
	if err != nil {
		writeError(w, 500, "cannot read Claude Managed Agents configuration")
		return
	}
	items, err := claudemanagedruntime.ListCatalog(r.Context(), claudemanagedruntime.DefaultAnthropicURL, cfg.AnthropicToken, "agents")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// ListClaudeEnvironments lists environments for the connect form, using the
// key in the request or, when it is blank, the saved one.
func (h *Handler) ListClaudeEnvironments(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	if h.QoderRateLimiter != nil && !h.QoderRateLimiter.Allow(r.Context(), "claude-catalog:"+uuidToString(ws)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Claude catalog request limit reached")
		return
	}
	var input struct {
		Token string `json:"anthropic_token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil {
		writeError(w, 400, "invalid Anthropic key request")
		return
	}
	cfg, _, _, _, err := h.readClaudeSettings(r.Context(), uuidToString(ws))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "cannot read Claude Managed Agents configuration")
		return
	}
	token := input.Token
	if token == "" {
		token = cfg.AnthropicToken
	}
	if token == "" {
		writeError(w, 400, "Anthropic API key is required")
		return
	}
	items, err := claudemanagedruntime.ListCatalog(r.Context(), claudemanagedruntime.DefaultAnthropicURL, token, "environments")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
