package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Hook invocation from a person: a button inside a surface (`ui`) or an entry in
// the issue actions menu and command palette (`manual`).
//
// Both block — but only this request. A person is waiting for an answer they
// asked for, so returning immediately would mean returning nothing useful. What
// must never block is the host's own work, and that is the `event` trigger,
// which does not come through here at all: it is dispatched off the event bus
// onto a worker pool and no request ever waits for it.

type invokePluginHookRequest struct {
	// Trigger is which declared call site this is. The client says which one it
	// is using, and the server checks the manifest declared it — a client
	// cannot invent a trigger to reach a hook that never offered it.
	Trigger string          `json:"trigger"`
	IssueID string          `json:"issue_id,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
}

// InvokePluginHook — POST /api/plugin-bridge/v1/hooks/{key}
func (h *Handler) InvokePluginHook(w http.ResponseWriter, r *http.Request) {
	// No scope of its own: a hook is the plugin's own capability, and what it
	// may do on the way back is bounded by the installation's scopes when the
	// callback token is redeemed.
	caller, actor, ok := h.pluginCaller(w, r, "")
	if !ok {
		return
	}
	// A hook invoked from the UI is invoked BY somebody. A plugin's own server
	// calling this would be asking us to call it back, which is a loop with no
	// person in it and no reason to exist.
	if !actor.requireMember(w) {
		return
	}

	var req invokePluginHookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Trigger {
	case plugincontract.TriggerUI, plugincontract.TriggerManual:
	default:
		// event is dispatched by the host, never asked for; agent arrives over
		// MCP. Accepting either here would let a browser drive a call site that
		// is supposed to have a different identity attached.
		writeError(w, http.StatusBadRequest, "trigger must be ui or manual")
		return
	}

	hook, err := service.FindHook(caller.Installation, chi.URLParam(r, "key"))
	if err != nil {
		writePluginError(w, err, "failed to load the hook")
		return
	}

	invocation := service.HookInvocation{
		Installation: caller.Installation,
		Hook:         hook,
		Trigger:      req.Trigger,
		// The person who pressed it. Writes the handler makes with the callback
		// token land as theirs, marked with via_plugin_id.
		Actor: service.HookActor{Type: "member", ID: actor.Member.UserID},
		Input: rawOrNil(req.Input),
	}
	if req.IssueID != "" {
		issue, ok := h.pluginIssueForUser(w, r, caller, req.IssueID)
		if !ok {
			return
		}
		invocation.IssueID = issue.ID
	}

	result, err := h.PluginService.InvokeHook(r.Context(), invocation, 1)
	if err != nil {
		// The status matters to the person watching: "the admin did not grant
		// this" and "the plugin author's server is down" are different problems
		// with different owners, and a generic 500 hides which one it is.
		writePluginError(w, err, "the hook call failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// rawOrNil keeps an omitted input out of the request body entirely rather than
// sending a literal null a handler has to special-case.
func rawOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// --- Installation-scoped hook administration ---

type pluginInvocationResponse struct {
	ID         string  `json:"id"`
	HookKey    string  `json:"hook_key"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	EventType  *string `json:"event_type,omitempty"`
	Attempt    int     `json:"attempt"`
	LatencyMs  int     `json:"latency_ms"`
	Error      *string `json:"error,omitempty"`
	DeliveryID *string `json:"delivery_id,omitempty"`
	PlannedAt  *string `json:"planned_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// ListPluginInvocations — GET /api/workspaces/{id}/plugins/{installationId}/invocations
//
// The triage view: why is this hook failing. Bodies were never stored, so this
// answers "what happened and how often", not "what was sent".
func (h *Handler) ListPluginInvocations(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPluginInvocations(r.Context(), db.ListPluginInvocationsParams{
		InstallationID: installation.ID,
		Limit:          100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the Plugin activity")
		return
	}
	items := make([]pluginInvocationResponse, 0, len(rows))
	for _, row := range rows {
		item := pluginInvocationResponse{
			ID:        uuidToString(row.ID),
			HookKey:   row.HookKey,
			Trigger:   row.Trigger,
			Status:    row.Status,
			Attempt:   int(row.Attempt),
			LatencyMs: int(row.LatencyMs),
			CreatedAt: row.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}
		if row.EventType.Valid {
			value := row.EventType.String
			item.EventType = &value
		}
		if row.Error.Valid {
			value := row.Error.String
			item.Error = &value
		}
		if row.DeliveryID.Valid {
			value := row.DeliveryID.String
			item.DeliveryID = &value
		}
		if row.PlannedAt.Valid {
			value := row.PlannedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
			item.PlannedAt = &value
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"invocations": items})
}

type pluginTokenResponse struct {
	// Token is returned exactly once, on the request that minted it. There is
	// no read endpoint: an admin who loses it rotates rather than recovers.
	Token string `json:"token"`
	// SigningSecret is what the author configures on their own server to verify
	// our signature. Derived from the deployment key rather than stored, so it
	// is stable for an installation and reproducible without a database copy.
	SigningSecret string `json:"signing_secret"`
}

// RotatePluginToken — POST /api/workspaces/{id}/plugins/{installationId}/token
func (h *Handler) RotatePluginToken(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	token, err := h.PluginService.IssueInstallToken(r.Context(), installation.ID)
	if err != nil {
		writePluginError(w, err, "failed to issue the Plugin token")
		return
	}
	secret, err := h.PluginService.HookSigningSecret(installation.ID)
	if err != nil {
		writePluginError(w, err, "failed to derive the signing secret")
		return
	}
	writeJSON(w, http.StatusOK, pluginTokenResponse{Token: token, SigningSecret: secret})
}

// RevokePluginToken — DELETE /api/workspaces/{id}/plugins/{installationId}/token
func (h *Handler) RevokePluginToken(w http.ResponseWriter, r *http.Request) {
	installation, ok := h.pluginInstallationFromURL(w, r)
	if !ok {
		return
	}
	if err := h.PluginService.RevokeInstallToken(r.Context(), installation.ID); err != nil {
		writePluginError(w, err, "failed to revoke the Plugin token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
