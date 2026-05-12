// JEH-1009 handler-seam adapter. The upstream handler package declares a
// `GroupPermissionsInvoker` interface keyed on its own `GroupPermissionsViewer`
// struct (carries pgtype.UUID + IsAdmin + GroupIDs) so it can call into
// permission checks without importing this package directly — the upstream
// handler package can't depend on `cerebro/grouppermissions` without
// introducing a cycle.
//
// `HandlerSeam` wraps a Service and satisfies that interface by converting
// between the upstream-shape Viewer and the cerebro-shape one. The router
// constructs it once after wiring grouppermissions.New(...) and assigns it
// to `Handler.GroupPermissions`.
package grouppermissions

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/handler"
)

// HandlerSeam adapts *Service to handler.GroupPermissionsInvoker.
type HandlerSeam struct {
	Service *Service
}

// NewHandlerSeam returns the adapter. Cheap; safe to call once at startup.
func NewHandlerSeam(s *Service) *HandlerSeam {
	return &HandlerSeam{Service: s}
}

// ResolveGroupIDs forwards to the service.
func (a *HandlerSeam) ResolveGroupIDs(ctx context.Context, workspaceID, userID pgtype.UUID) ([]pgtype.UUID, error) {
	return a.Service.ResolveGroupIDs(ctx, workspaceID, userID)
}

// CanCreateRuntime converts the upstream viewer shape and calls the service.
func (a *HandlerSeam) CanCreateRuntime(ctx context.Context, viewer handler.GroupPermissionsViewer, workspaceID pgtype.UUID) (bool, error) {
	return a.Service.CanCreateRuntime(ctx, toServiceViewer(viewer), workspaceID)
}

// CanCreateAgent — same shape as CanCreateRuntime.
func (a *HandlerSeam) CanCreateAgent(ctx context.Context, viewer handler.GroupPermissionsViewer, workspaceID pgtype.UUID) (bool, error) {
	return a.Service.CanCreateAgent(ctx, toServiceViewer(viewer), workspaceID)
}

// CanUseRuntime checks the runtime allowlist for the resolved viewer.
func (a *HandlerSeam) CanUseRuntime(ctx context.Context, viewer handler.GroupPermissionsViewer, runtimeID pgtype.UUID) (bool, error) {
	return a.Service.CanUseRuntime(ctx, toServiceViewer(viewer), runtimeID)
}

func toServiceViewer(v handler.GroupPermissionsViewer) Viewer {
	return Viewer{
		UserID:   v.UserID,
		IsAdmin:  v.IsAdmin,
		GroupIDs: v.GroupIDs,
	}
}
