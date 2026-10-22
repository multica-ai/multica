package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// disconnectQoderRequest carries the active agents the admin was shown as
// losing their runtime. A different live set refuses with the latest list.
type disconnectQoderRequest struct {
	ExpectedActiveAgentIDs []string `json:"expected_active_agent_ids"`
}

// DisconnectQoderConnection removes the workspace's Qoder connection for good:
// the encrypted credentials and the managed runtime go, and agents on that
// runtime are unbound (kept, with their history). Unlike stop, nothing is left
// to resume from.
//
// The bridge is stopped first so no step or dispatch can start, then remote
// sessions are cancelled while the credentials still exist, and only then is
// the connection deleted. A refusal after the stop leaves the connection
// stopped, which is where the admin was headed anyway.
func (h *Handler) DisconnectQoderConnection(w http.ResponseWriter, r *http.Request) {
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

	if !h.stopQoderForDisconnect(w, r, ws, expected) {
		return
	}
	if err := h.cleanupQoderJournal(r.Context(), wsID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("qoder disconnect waiting on remote cleanup", "workspace_id", wsID, "error", err)
		writeError(w, http.StatusConflict, "Qoder is stopped and still cancelling remote sessions; retry shortly")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, wsID); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot lock QCA configuration")
		return
	}
	qtx := h.Queries.WithTx(tx)
	runtimeIDs, refused := h.checkQoderDisconnectPlan(w, r.Context(), tx, qtx, ws, expected)
	if refused {
		return
	}
	teardowns := make([]service.RuntimeTeardownResult, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		teardown, err := service.TeardownRuntime(r.Context(), qtx, runtimeID, service.RuntimeTeardownOptions{CancelNonTerminalTasks: true})
		if err != nil {
			if errors.Is(err, service.ErrRuntimeNotDrained) {
				writeError(w, http.StatusConflict, "Qoder still has tasks in flight; retry in a moment")
				return
			}
			slog.Error("qoder disconnect teardown failed", "runtime_id", uuidToString(runtimeID), "error", err)
			writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
			return
		}
		if err := qtx.DeleteAgentRuntime(r.Context(), runtimeID); err != nil {
			writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
			return
		}
		teardowns = append(teardowns, teardown)
	}
	tag, err := tx.Exec(r.Context(), `DELETE FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1 AND journal_encrypted=''`, ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "Qoder is stopped and still cancelling remote sessions; retry shortly")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
		return
	}

	userID := user
	for i, runtimeID := range runtimeIDs {
		h.NotifyRuntimeGone(uuidToString(runtimeID))
		h.publishRuntimeTeardown(r.Context(), teardowns[i], wsID, userID)
	}
	slog.Info("qoder disconnected", "workspace_id", wsID, "disconnected_by", userID, "runtimes_removed", len(runtimeIDs))
	h.GetQoderConnection(w, r)
}

// stopQoderForDisconnect disables the connection and revokes the bridge's
// credential, after the same checks as stop plus the confirmed agent plan.
// It reports whether the caller may continue; on false a response was written.
func (h *Handler) stopQoderForDisconnect(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, expected map[string]struct{}) bool {
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot disconnect Qoder")
		return false
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,19))`, uuidToString(ws)); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot lock QCA configuration")
		return false
	}
	var exists, busy bool
	if err := tx.QueryRow(r.Context(), `SELECT true, COALESCE(busy_until>now(),false) FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1`, ws).Scan(&exists, &busy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Qoder is not connected")
			return false
		}
		writeError(w, http.StatusInternalServerError, "cannot check QCA connection")
		return false
	}
	if busy {
		writeError(w, http.StatusConflict, "QCA is synchronizing; retry shortly")
		return false
	}
	if _, refused := h.checkQoderDisconnectPlan(w, r.Context(), tx, h.Queries.WithTx(tx), ws, expected); refused {
		return false
	}
	var revokedHash string
	_ = tx.QueryRow(r.Context(), `SELECT d.token_hash FROM daemon_token d JOIN cloud_agent_connection c ON c.token_id=d.id AND c.provider='qoder_cloud' WHERE c.workspace_id=$1`, ws).Scan(&revokedHash)
	if _, err = tx.Exec(r.Context(), `DELETE FROM daemon_token WHERE id IN (SELECT token_id FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, ws); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot revoke runtime credential")
		return false
	}
	if _, err = tx.Exec(r.Context(), `UPDATE cloud_agent_connection SET enabled=false,revision=revision+1,status='stopped',last_error='' WHERE provider='qoder_cloud' AND workspace_id=$1`, ws); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stop QCA runtime")
		return false
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot stop QCA runtime")
		return false
	}
	if revokedHash != "" {
		h.DaemonTokenCache.Invalidate(r.Context(), revokedHash)
	}
	return true
}

// checkQoderDisconnectPlan locks the workspace's Qoder runtimes and refuses
// while tasks run on them or when their active agents differ from the
// confirmed set. It returns the runtimes to remove.
func (h *Handler) checkQoderDisconnectPlan(w http.ResponseWriter, ctx context.Context, tx pgx.Tx, qtx *db.Queries, ws pgtype.UUID, expected map[string]struct{}) ([]pgtype.UUID, bool) {
	rows, err := tx.Query(ctx, `SELECT id FROM agent_runtime WHERE workspace_id=$1 AND provider='qoder_cloud' ORDER BY id FOR UPDATE`, ws)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load Qoder runtime")
		return nil, true
	}
	runtimeIDs, err := pgx.CollectRows(rows, pgx.RowTo[pgtype.UUID])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot load Qoder runtime")
		return nil, true
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent_runtime rt ON rt.id=t.runtime_id WHERE rt.workspace_id=$1 AND rt.provider='qoder_cloud' AND t.status IN ('running','dispatched'))`, ws).Scan(&active); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot check active QCA tasks")
		return nil, true
	}
	if active {
		writeError(w, http.StatusConflict, "wait for or cancel active QCA tasks before disconnecting")
		return nil, true
	}
	var agents []db.Agent
	for _, runtimeID := range runtimeIDs {
		bound, err := qtx.ListActiveAgentsByRuntimeForUpdate(ctx, runtimeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cannot check Qoder agents")
			return nil, true
		}
		agents = append(agents, bound...)
	}
	if !activeAgentSetMatches(agents, expected) {
		body := h.runtimeHasActiveAgentsResponse(agents)
		body["code"] = "runtime_delete_plan_changed"
		body["error"] = "the agents on Qoder changed; please review and confirm again."
		writeJSON(w, http.StatusConflict, body)
		return nil, true
	}
	return runtimeIDs, false
}

// qoderManagesRuntime reports whether a runtime belongs to a live Qoder
// connection, whose lifecycle is owned by connect and disconnect rather than
// the generic runtime endpoints.
func (h *Handler) qoderManagesRuntime(ctx context.Context, rt db.AgentRuntime) (bool, error) {
	if rt.Provider != "qoder_cloud" {
		return false, nil
	}
	var managed bool
	err := h.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cloud_agent_connection WHERE provider='qoder_cloud' AND workspace_id=$1)`, rt.WorkspaceID).Scan(&managed)
	return managed, err
}

// refuseQoderManagedRuntime writes the refusal for generic runtime mutations
// on a Qoder-managed runtime and reports whether it did.
func (h *Handler) refuseQoderManagedRuntime(w http.ResponseWriter, ctx context.Context, rt db.AgentRuntime, message string) bool {
	managed, err := h.qoderManagesRuntime(ctx, rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check runtime connection")
		return true
	}
	if !managed {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": message,
		"code":  "runtime_managed_by_connection",
	})
	return true
}
