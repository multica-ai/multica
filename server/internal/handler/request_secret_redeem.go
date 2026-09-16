// Package handler: request-scoped secret redeem endpoint for per-visitor
// identity passthrough (WS-11 P1, cross-process closing half).
//
// The claim path mints an opaque handle into the in-process
// requestsecret.Store and places it on the claimed task as RequestSecretRef.
// The daemon runs in a different process, so it cannot read that store
// directly. This endpoint is how the daemon exchanges the handle for the
// short-lived secret at child-launch time.
//
// Authorization is by DAEMON identity, NOT the task-scoped mat_ token. The
// endpoint is mounted under /api/daemon behind middleware.DaemonAuth, which
// only accepts a daemon credential (mdt_ / mcn_). This is load-bearing: the
// mat_ task token is ALSO held by the agent runtime itself, so authorizing
// redemption with it would let the agent redeem its own visitor plaintext.
// Requiring daemon identity keeps the raw secret on the server↔daemon trusted
// boundary — the agent process can never present a daemon token.
//
// The caller supplies {handle, task_id}. The task_id is bound to the daemon's
// workspace by requireDaemonTaskAccessWithWorkspace (a daemon can only redeem
// for tasks in a workspace it owns — otherwise 404). The Consume principal is
// the SERVER-authoritative task.OriginatorUserID (the same value the claim
// path minted the handle against), never a client-supplied value: the daemon
// can only present a handle + task id, never choose the principal.
//
// Consume is atomic and one-time (see requestsecret.Store): a wrong
// task/principal is rejected WITHOUT burning the handle, a replay returns
// not-found, and an expired handle is rejected and swept. The secret is
// returned only in the response body to the authenticated daemon; it is never
// logged, never persisted, and never written back onto the task.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/requestsecret"
)

// redeemRequestSecretRequest is the daemon-supplied body: the opaque handle
// and the task id the handle is bound to. The principal is derived
// server-side from the task row, never from the client.
type redeemRequestSecretRequest struct {
	Handle string `json:"handle"`
	TaskID string `json:"task_id"`
}

// redeemRequestSecretResponse carries the short-lived secret back to the
// authenticated daemon. It exists only in this response body.
type redeemRequestSecretResponse struct {
	Secret string `json:"secret"`
}

// RedeemRequestSecret exchanges a request-secret handle for its short-lived
// secret, authorized by DAEMON identity and bound to the daemon's workspace +
// the task's server-side originator principal. Fail-closed on every mismatch.
//
// Mounted under /api/daemon (middleware.DaemonAuth). A missing daemon
// workspace context means the request did not carry a daemon credential — fail
// closed rather than fall through to any user-membership path.
func (h *Handler) RedeemRequestSecret(w http.ResponseWriter, r *http.Request) {
	// Daemon identity is required — see package doc. DaemonAuth stamps a daemon
	// workspace id only for a genuine daemon credential; its absence means a
	// non-daemon token reached this endpoint. Never serve plaintext to it.
	if middleware.DaemonWorkspaceIDFromContext(r.Context()) == "" {
		writeError(w, http.StatusForbidden, "request secret redemption requires daemon identity")
		return
	}

	// The store lives on the TaskService (the claim path mints into the same
	// instance). Nil means the feature is unwired — fail closed, never 200.
	if h.TaskService == nil || h.TaskService.RequestSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "request secret store unavailable")
		return
	}

	var body redeemRequestSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	handle := strings.TrimSpace(body.Handle)
	if handle == "" {
		writeError(w, http.StatusBadRequest, "missing handle")
		return
	}
	taskID := strings.TrimSpace(body.TaskID)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing task id")
		return
	}

	// Bind the task to the daemon's workspace. This both validates the task id
	// and enforces that the daemon may only redeem for its own workspace's
	// tasks; any mismatch is surfaced as 404 by the helper.
	task, _, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, taskID)
	if !ok {
		return
	}

	// Principal is the server-authoritative originator the claim path minted
	// against — read from the task row, NEVER from the client. An unbound task
	// has no principal to redeem against; fail closed.
	if !task.OriginatorUserID.Valid {
		writeError(w, http.StatusNotFound, "request secret handle not redeemable")
		return
	}
	principalID := uuidToString(task.OriginatorUserID)

	secret, err := h.TaskService.RequestSecrets.Consume(handle, principalID, uuidToString(task.ID))
	if err != nil {
		// Map sentinels to status codes WITHOUT echoing the handle or secret.
		// Wrong task/principal and not-found/expired/replay all collapse to a
		// generic 404 so a caller cannot probe which dimension mismatched.
		switch {
		case errors.Is(err, requestsecret.ErrNotFound),
			errors.Is(err, requestsecret.ErrExpired),
			errors.Is(err, requestsecret.ErrPrincipal),
			errors.Is(err, requestsecret.ErrTask):
			writeError(w, http.StatusNotFound, "request secret handle not redeemable")
			return
		default:
			writeError(w, http.StatusInternalServerError, "request secret redemption failed")
			return
		}
	}

	writeJSON(w, http.StatusOK, redeemRequestSecretResponse{Secret: secret})
}
