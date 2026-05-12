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
