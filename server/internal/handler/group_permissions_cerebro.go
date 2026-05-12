// CEREBRO-PATCH(group-permissions-cerebro): handler-side glue for the
// cerebro group permission model (JEH-1009, PR 3 of JEH-1006). Net-new fork
// file in the upstream-zone path. The upstream-handler package never imports
// the cerebro grouppermissions service directly — that would create an
// import cycle, since the service depends on db.Queries which lives one
// layer above. Instead the router wires the concrete service into the
// `Handler.GroupPermissions` invoker seam after construction, and these
// helpers type-assert through that seam.
//
// Helpers added here:
//
//	cerebroGroupViewer        — build a Viewer (admin + GroupIDs) for a request
//	cerebroRequireCapability  — gate a write endpoint on a capability
//
// Resolution mirrors cerebroAutopilotViewer for autopilot-scope and reuses
// the same getWorkspaceMember + isWorkspaceAdmin primitives. Admin override
// short-circuits the DB lookup so workspace owners/admins never block on a
// failed group resolution.
package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// GroupPermissionsInvoker is the upstream-side seam that the cerebro group-
// permission service plugs into. Methods on *Handler in
// group_permissions_cerebro.go type-assert this to call the concrete
// service without importing the cerebro package directly.
//
// Returning bool + error keeps the call-site idiom identical to the
// access.CanSee / .CanEdit pattern used by autopilot-scope.
//
// CEREBRO-PATCH(handler-group-permissions-iface): seam for cerebro group permissions.
type GroupPermissionsInvoker interface {
	ResolveGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID) ([]pgtype.UUID, error)
	CanCreateRuntime(ctx context.Context, viewer GroupPermissionsViewer, workspaceID pgtype.UUID) (bool, error)
	CanCreateAgent(ctx context.Context, viewer GroupPermissionsViewer, workspaceID pgtype.UUID) (bool, error)
	CanUseRuntime(ctx context.Context, viewer GroupPermissionsViewer, runtimeID pgtype.UUID) (bool, error)
	// JEH-1009 PR 4 — allowlist enforcement on `TriggerAgent` / `ListAgents` /
	// `ListRuntimes` / `ListProjects` / `ListIssues`. `VisibleXxxIDs` return
	// nil for admins (caller skips filtering) and the explicit (possibly
	// empty) allowlist for members.
	CanUseAgent(ctx context.Context, viewer GroupPermissionsViewer, agentID pgtype.UUID) (bool, error)
	CanSeeProjectViaGroup(ctx context.Context, viewer GroupPermissionsViewer, projectID pgtype.UUID) (bool, error)
	VisibleAgentIDs(ctx context.Context, viewer GroupPermissionsViewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
	VisibleRuntimeIDs(ctx context.Context, viewer GroupPermissionsViewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
	VisibleProjectIDs(ctx context.Context, viewer GroupPermissionsViewer, workspaceID pgtype.UUID) ([]pgtype.UUID, error)
	// ProjectAudienceUserIDs returns the user IDs with group access to the project
	// so WS event audiences include group-only viewers.
	ProjectAudienceUserIDs(ctx context.Context, projectID pgtype.UUID) ([]pgtype.UUID, error)
}

// GroupPermissionsViewer mirrors grouppermissions.Viewer at the handler-package
// boundary so handlers can assemble it without importing the cerebro package.
//
// CEREBRO-PATCH(handler-group-permissions-iface): viewer shape for the seam.
type GroupPermissionsViewer struct {
	UserID   pgtype.UUID
	IsAdmin  bool
	GroupIDs []pgtype.UUID
}

// cerebroGroupViewer assembles a viewer for the request user. Mirrors
// cerebroAutopilotViewer's resolution path: membership lookup → admin flag →
// effective group IDs. Returns false (and writes the HTTP error) when the
// request lacks workspace membership or carries an unparseable user id.
func (h *Handler) cerebroGroupViewer(w http.ResponseWriter, r *http.Request, workspaceID string) (GroupPermissionsViewer, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return GroupPermissionsViewer{}, false
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace context required")
		return GroupPermissionsViewer{}, false
	}
	member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
	if err != nil {
		writeError(w, http.StatusForbidden, "not a workspace member")
		return GroupPermissionsViewer{}, false
	}
	userUUID, perr := util.ParseUUID(userID)
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return GroupPermissionsViewer{}, false
	}
	viewer := GroupPermissionsViewer{
		UserID:  userUUID,
		IsAdmin: isWorkspaceAdmin(member),
	}
	// Admins bypass group resolution entirely — keeps the gate functional even
	// when the cerebro service is not wired (e.g. upstream-only test
	// fixtures). Members fall through and the seam decides.
	if !viewer.IsAdmin && h.GroupPermissions != nil {
		wsUUID, perr := util.ParseUUID(workspaceID)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid workspace id")
			return GroupPermissionsViewer{}, false
		}
		ids, err := h.GroupPermissions.ResolveGroupIDs(r.Context(), wsUUID, userUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve groups")
			return GroupPermissionsViewer{}, false
		}
		viewer.GroupIDs = ids
	}
	return viewer, true
}

// cerebroRequireCapability gates a write endpoint on a group-resolved
// capability. Admins always pass. Members must belong to at least one group
// that has the capability granted. Writes 403 on deny.
//
// When GroupPermissions is nil (upstream-only test fixtures or feature flag
// off in a future iteration) the gate is open — failing closed in a single-
// instance dev setup with no groups is worse than failing open here, and
// the cerebro service is always wired in production.
func (h *Handler) cerebroRequireCapability(w http.ResponseWriter, r *http.Request, workspaceID, capability string) bool {
	if h.GroupPermissions == nil {
		return true
	}
	// Validate the capability identifier BEFORE any DB lookups so a typo in
	// the handler fails fast with 500 — we'd rather see this in a test than
	// ship a backdoor that silently grants an unknown right.
	switch capability {
	case "create_runtime", "create_agent":
		// known
	default:
		writeError(w, http.StatusInternalServerError, "unknown capability")
		return false
	}
	viewer, ok := h.cerebroGroupViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	wsUUID, perr := util.ParseUUID(workspaceID)
	if perr != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return false
	}
	var (
		allowed bool
		err     error
	)
	switch capability {
	case "create_runtime":
		allowed, err = h.GroupPermissions.CanCreateRuntime(r.Context(), viewer, wsUUID)
	case "create_agent":
		allowed, err = h.GroupPermissions.CanCreateAgent(r.Context(), viewer, wsUUID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "no group grants this capability — ask a workspace admin")
		return false
	}
	return true
}

// cerebroRequireRuntimeAccess gates an action on the viewer being a member of
// at least one group that grants access to the runtime. Admins bypass. Used
// from `CreateAgent` so a member with the `create_agent` capability still
// cannot point a new agent at a runtime their groups don't allow.
//
// nil-invoker fails open for the same reason as cerebroRequireCapability:
// upstream-only test fixtures must keep working.
func (h *Handler) cerebroRequireRuntimeAccess(w http.ResponseWriter, r *http.Request, workspaceID string, runtimeID pgtype.UUID) bool {
	if h.GroupPermissions == nil {
		return true
	}
	viewer, ok := h.cerebroGroupViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	allowed, err := h.GroupPermissions.CanUseRuntime(r.Context(), viewer, runtimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "no group grants access to this runtime — ask a workspace admin")
		return false
	}
	return true
}

// CEREBRO-PATCH(group-permissions-cerebro-pr4): JEH-1009 PR 4 — agent
// allowlist gate (CanUseAgent), project group-access OR-clause helper, and
// the viewer / visible-ID-set helpers used by the list-filter handlers.
//
// cerebroCanUseAgent is the non-writing companion to cerebroRequireAgentAccess.
// Used by callers that already own their HTTP response shape (e.g. validators
// returning a (status, msg) tuple). Returns ok=true when the viewer is allowed
// to trigger the agent, ok=false otherwise. The boolean is the answer; the
// error is reserved for service-level failures (DB error, missing context).
//
// nil-invoker → (true, nil). Same fail-open rationale as
// cerebroRequireCapability — upstream-only test fixtures must keep working.
func (h *Handler) cerebroCanUseAgent(ctx context.Context, r *http.Request, workspaceID string, agentID pgtype.UUID) (bool, error) {
	if h.GroupPermissions == nil {
		return true, nil
	}
	viewer, ok := h.cerebroBuildViewer(ctx, r, workspaceID)
	if !ok {
		// Bad request — caller should already have rejected on workspace context.
		// Returning (false, nil) here is safer than fail-open: a request that
		// can't resolve to a workspace member shouldn't be allowed to trigger
		// an agent.
		return false, nil
	}
	return h.GroupPermissions.CanUseAgent(ctx, viewer, agentID)
}

// cerebroAgentAccessAsValidatorError calls cerebroCanUseAgent and maps the
// result to the (status, message) tuple shape used by validateAssigneePair.
// status == 0 means the gate passed.
func (h *Handler) cerebroAgentAccessAsValidatorError(ctx context.Context, r *http.Request, workspaceID string, agentID pgtype.UUID) (int, string) {
	allowed, err := h.cerebroCanUseAgent(ctx, r, workspaceID, agentID)
	if err != nil {
		return http.StatusInternalServerError, "failed to check agent access"
	}
	if !allowed {
		return http.StatusForbidden, "no group grants access to this agent — ask a workspace admin"
	}
	return 0, ""
}

// cerebroRequireAgentAccess gates an endpoint on the viewer being in at least
// one group that grants access to the agent. Admins bypass. Writes 403 on
// deny. Mirrors cerebroRequireRuntimeAccess; used by direct trigger endpoints
// that don't already own their own error responses.
func (h *Handler) cerebroRequireAgentAccess(w http.ResponseWriter, r *http.Request, workspaceID string, agentID pgtype.UUID) bool {
	if h.GroupPermissions == nil {
		return true
	}
	viewer, ok := h.cerebroGroupViewer(w, r, workspaceID)
	if !ok {
		return false
	}
	allowed, err := h.GroupPermissions.CanUseAgent(r.Context(), viewer, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "no group grants access to this agent — ask a workspace admin")
		return false
	}
	return true
}

// cerebroBuildViewer is a non-writing variant of cerebroGroupViewer for
// callers that don't want HTTP error responses written when membership lookup
// fails — they map the resulting (zero-value, false) into their own
// (statusCode, message) shape.
//
// Used by canAssignAgent / validateAssigneePair and CreateChatSession which
// each have their own (bool, errMsg) error contract.
func (h *Handler) cerebroBuildViewer(ctx context.Context, r *http.Request, workspaceID string) (GroupPermissionsViewer, bool) {
	userID := requestUserID(r)
	if userID == "" || workspaceID == "" {
		return GroupPermissionsViewer{}, false
	}
	member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return GroupPermissionsViewer{}, false
	}
	userUUID, perr := util.ParseUUID(userID)
	if perr != nil {
		return GroupPermissionsViewer{}, false
	}
	viewer := GroupPermissionsViewer{
		UserID:  userUUID,
		IsAdmin: isWorkspaceAdmin(member),
	}
	if !viewer.IsAdmin && h.GroupPermissions != nil {
		wsUUID, perr := util.ParseUUID(workspaceID)
		if perr != nil {
			return GroupPermissionsViewer{}, false
		}
		ids, err := h.GroupPermissions.ResolveGroupIDs(ctx, wsUUID, userUUID)
		if err != nil {
			return GroupPermissionsViewer{}, false
		}
		viewer.GroupIDs = ids
	}
	return viewer, true
}

// cerebroVisibleAgentIDSet returns (set, hasFilter, ok). hasFilter=false means
// the viewer is an admin (or the seam is not wired) — caller skips filtering.
// hasFilter=true returns the explicit allowlist (possibly empty: viewer has no
// group grant → no agents visible).
//
// ok=false signals a DB-level failure or that the request lacks a resolvable
// workspace member; caller should leave the list untouched and rely on the
// upstream access path. Conservative: if the lookup fails, don't widen access,
// but also don't blank the page — the user sees the upstream-filtered list.
func (h *Handler) cerebroVisibleAgentIDSet(ctx context.Context, r *http.Request, workspaceID string) (set map[string]struct{}, hasFilter bool, ok bool) {
	if h.GroupPermissions == nil {
		return nil, false, true
	}
	viewer, viewerOk := h.cerebroBuildViewer(ctx, r, workspaceID)
	if !viewerOk {
		return nil, false, false
	}
	if viewer.IsAdmin {
		return nil, false, true
	}
	wsUUID, perr := util.ParseUUID(workspaceID)
	if perr != nil {
		return nil, false, false
	}
	ids, err := h.GroupPermissions.VisibleAgentIDs(ctx, viewer, wsUUID)
	if err != nil {
		return nil, false, false
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[uuidToString(id)] = struct{}{}
	}
	return out, true, true
}

// cerebroVisibleRuntimeIDSet mirrors cerebroVisibleAgentIDSet for runtimes.
func (h *Handler) cerebroVisibleRuntimeIDSet(ctx context.Context, r *http.Request, workspaceID string) (set map[string]struct{}, hasFilter bool, ok bool) {
	if h.GroupPermissions == nil {
		return nil, false, true
	}
	viewer, viewerOk := h.cerebroBuildViewer(ctx, r, workspaceID)
	if !viewerOk {
		return nil, false, false
	}
	if viewer.IsAdmin {
		return nil, false, true
	}
	wsUUID, perr := util.ParseUUID(workspaceID)
	if perr != nil {
		return nil, false, false
	}
	ids, err := h.GroupPermissions.VisibleRuntimeIDs(ctx, viewer, wsUUID)
	if err != nil {
		return nil, false, false
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[uuidToString(id)] = struct{}{}
	}
	return out, true, true
}

// cerebroVisibleProjectIDsForViewer returns the project IDs the viewer has
// group-level access to. nil = no extra IDs (admin or no seam). Used by
// ListProjects to widen the upstream access-filtered list with group-only
// projects the viewer would otherwise miss.
//
// Returns (nil, true) when the viewer is an admin; the caller already has
// every project from the upstream query.
func (h *Handler) cerebroVisibleProjectIDsForViewer(ctx context.Context, r *http.Request, workspaceID string) (ids []pgtype.UUID, ok bool) {
	if h.GroupPermissions == nil {
		return nil, true
	}
	viewer, viewerOk := h.cerebroBuildViewer(ctx, r, workspaceID)
	if !viewerOk {
		return nil, false
	}
	if viewer.IsAdmin {
		return nil, true
	}
	wsUUID, perr := util.ParseUUID(workspaceID)
	if perr != nil {
		return nil, false
	}
	res, err := h.GroupPermissions.VisibleProjectIDs(ctx, viewer, wsUUID)
	if err != nil {
		return nil, false
	}
	return res, true
}

// cerebroCanSeeProjectViaGroup is the canAccessProject extension: returns
// true when the viewer has access to the project via any group. Layered on
// top of the existing workspace/admin/project_member checks by the caller.
//
// userID and workspaceID come from a resolved db.Member row — callers must
// only pass values they already trust (the member was loaded via the regular
// membership lookup before this call).
func (h *Handler) cerebroCanSeeProjectViaGroup(ctx context.Context, userID, workspaceID, projectID pgtype.UUID) bool {
	if h.GroupPermissions == nil {
		return false
	}
	viewer := GroupPermissionsViewer{
		UserID: userID,
	}
	ids, err := h.GroupPermissions.ResolveGroupIDs(ctx, workspaceID, userID)
	if err != nil {
		return false
	}
	viewer.GroupIDs = ids
	allowed, err := h.GroupPermissions.CanSeeProjectViaGroup(ctx, viewer, projectID)
	if err != nil {
		return false
	}
	return allowed
}
