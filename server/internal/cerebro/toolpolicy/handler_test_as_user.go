package toolpolicy

// handler_test_as_user.go — the "Test as user" feature (FIR-1771).
//
// It lets an authorised admin resolve, from inside the app, what a DIFFERENT
// user+agent combination is allowed/asked/denied for one tool — the same answer
// the `multica tool-policy explain` CLI gives, without leaving the UI. The one
// thing it adds over the CLI is that it resolves the target user's REAL group
// membership automatically (Store.Table → resolveGroupIDs), so the caller never
// has to enumerate groups by hand.
//
// The feature itself is gated by the tools:test-as-user capability resolved
// through the unified chain with Base = Deny (OFF for everyone until granted),
// so only an explicitly-allowed user (or group) may look up another user's
// permissions. There is deliberately NO admin bypass: being a workspace admin
// does not by itself grant the ability to inspect other users' permissions —
// only the policy does.
//
// Routes (mounted member-level in cmd/server/router.go):
//
//	GET  /api/workspaces/{id}/cerebro/test-as-user/access  → {"allowed": bool}
//	POST /api/workspaces/{id}/cerebro/test-as-user         → {"tools": [<row>...]}

import (
	"encoding/json"
	"net/http"
)

// TestAsUserToolKey is the capability key that gates the Test as user feature.
// It is settable Allow/Ask/Deny on every layer in the Permissions screen like
// any other tool; only an effective Allow lets a caller use the feature.
const TestAsUserToolKey = "tools:test-as-user"

// testAsUserAccessResponse is the body of GET .../cerebro/test-as-user/access.
type testAsUserAccessResponse struct {
	Allowed bool `json:"allowed"`
}

// TestAsUserAccess — GET /api/workspaces/{id}/cerebro/test-as-user/access.
//
// Reports whether the calling member may use the Test as user feature, so the
// client can show or hide the profile-menu entry. It never 403s: a caller
// without the capability simply gets {"allowed": false}.
func (h *Handler) TestAsUserAccess(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}
	eff, err := h.Store.Resolve(r.Context(), Query{
		WorkspaceID: workspaceID,
		ToolKey:     TestAsUserToolKey,
		UserID:      member.UserID,
		Base:        SettingDeny,
	})
	if err != nil {
		h.serverError(w, r, "resolve test-as-user access", err)
		return
	}
	writeJSON(w, http.StatusOK, testAsUserAccessResponse{Allowed: eff.Setting == SettingAllow})
}

// testAsUserRequest is the POST body: which (user, agent, runtime) combination
// to resolve, and which tool to report. runtime_id is optional — the client
// passes the chosen agent's runtime so the Runtime layer is reflected, matching
// what really happens at run time.
type testAsUserRequest struct {
	UserID    string `json:"user_id"`
	AgentID   string `json:"agent_id"`
	RuntimeID string `json:"runtime_id"`
	ToolKey   string `json:"tool_key"`
}

// TestAsUser — POST /api/workspaces/{id}/cerebro/test-as-user.
//
// Gated by the tools:test-as-user capability (Base = Deny, no admin bypass).
// Resolves every tool for the target (user, agent, runtime) context — the
// target user's real groups are auto-resolved by Store.Table — and returns the
// rows (verdict, deciding/capping layer, capped group(s), reason), identical in
// shape to the Permissions table and the CLI explain. An optional tool_key
// narrows the result to one tool; empty returns the full list.
func (h *Handler) TestAsUser(w http.ResponseWriter, r *http.Request) {
	member, workspaceID, ok := h.loadWorkspace(w, r)
	if !ok {
		return
	}

	// Gate the caller: only an explicit Allow on tools:test-as-user lets anyone
	// look up another user's permissions. Base = Deny, no admin shortcut.
	access, err := h.Store.Resolve(r.Context(), Query{
		WorkspaceID: workspaceID,
		ToolKey:     TestAsUserToolKey,
		UserID:      member.UserID,
		Base:        SettingDeny,
	})
	if err != nil {
		h.serverError(w, r, "resolve test-as-user access", err)
		return
	}
	if access.Setting != SettingAllow {
		writeError(w, http.StatusForbidden, "the Test as user feature is not enabled for you — ask a workspace admin")
		return
	}

	var req testAsUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	userID, err := uuidParam(req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	agentID, err := uuidParam(req.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent_id")
		return
	}
	runtimeID, err := uuidParam(req.RuntimeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}

	// Resolve the whole table for this context (the target user's groups are
	// auto-resolved inside Table). Base = Allow mirrors the normal per-tool gate:
	// an unconfigured tool resolves to Allow, and stored Deny/Ask rows tighten it.
	rows, err := h.Store.Table(r.Context(), TableQuery{
		WorkspaceID: workspaceID,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      userID,
		Base:        SettingAllow,
	})
	if err != nil {
		h.serverError(w, r, "resolve test-as-user table", err)
		return
	}
	// An empty tool_key returns every resolved tool (the dialog renders the full
	// list with a client-side filter); a non-empty tool_key narrows to one.
	out := make([]toolPolicyRow, 0, len(rows))
	for _, row := range rows {
		if req.ToolKey == "" || row.ToolKey == req.ToolKey {
			out = append(out, toRowResponse(row))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": out})
}
