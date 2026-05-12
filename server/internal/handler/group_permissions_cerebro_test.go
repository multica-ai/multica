// CEREBRO-PATCH(group-permissions-cerebro-test): unit coverage for the
// capability-gate helper that fronts CreateAgent / CreateRuntimeSetupToken
// (JEH-1009). The real seam is wired up in the integration test where a
// full DB-backed grouppermissions Service answers the can-do questions; this
// file pins the handler-side decision tree against a mock invoker so the
// behaviour stays predictable across refactors.
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

type stubGroupPermissions struct {
	resolve func(ctx context.Context, ws, user pgtype.UUID) ([]pgtype.UUID, error)
	canRT   func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error)
	canAG   func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error)
	canUseR func(ctx context.Context, viewer GroupPermissionsViewer, rt pgtype.UUID) (bool, error)
}

func (s *stubGroupPermissions) ResolveGroupIDs(ctx context.Context, ws, user pgtype.UUID) ([]pgtype.UUID, error) {
	if s.resolve == nil {
		return nil, nil
	}
	return s.resolve(ctx, ws, user)
}

func (s *stubGroupPermissions) CanCreateRuntime(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error) {
	if s.canRT == nil {
		return false, nil
	}
	return s.canRT(ctx, viewer, ws)
}

func (s *stubGroupPermissions) CanCreateAgent(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error) {
	if s.canAG == nil {
		return false, nil
	}
	return s.canAG(ctx, viewer, ws)
}

func (s *stubGroupPermissions) CanUseRuntime(ctx context.Context, viewer GroupPermissionsViewer, rt pgtype.UUID) (bool, error) {
	if s.canUseR == nil {
		return false, nil
	}
	return s.canUseR(ctx, viewer, rt)
}

func TestCerebroRequireCapability_NilInvokerPasses(t *testing.T) {
	// When no cerebro seam is wired, the gate must be open. This is the path
	// that lets upstream-only test fixtures (testHandler in handler_test.go)
	// keep calling CreateAgent without DB-backed group permissions.
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/api/agents", nil)
	w := httptest.NewRecorder()
	if !h.cerebroRequireCapability(w, r, "00000000-0000-0000-0000-000000000000", "create_agent") {
		t.Fatalf("nil GroupPermissions: gate must pass, got body=%q", w.Body.String())
	}
}

func TestCerebroRequireCapability_UnknownCapabilityFailsClosed(t *testing.T) {
	// Mistyped capability identifier in the handler should never silently
	// pass — we'd rather see 500 in a test than ship a backdoor.
	h := &Handler{GroupPermissions: &stubGroupPermissions{}}
	r := makeAuthedRequest("user-id", "ws-id")
	w := httptest.NewRecorder()
	if h.cerebroRequireCapability(w, r, "00000000-0000-0000-0000-000000000001", "delete_workspace_lol") {
		t.Fatalf("unknown capability must be rejected")
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("unknown capability: expected 500, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown capability") {
		t.Fatalf("unknown capability: body should describe the error, got %q", w.Body.String())
	}
}

// makeAuthedRequest forges a request that satisfies requireUserID +
// resolveWorkspaceID by setting the X-User-ID + X-Workspace-ID headers used
// by middleware in production. Tests that exercise the seam still need a
// DB-backed handler to clear getWorkspaceMember; the two short-circuit
// tests above don't reach that far.
func makeAuthedRequest(userID, workspaceID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/agents", nil)
	r.Header.Set("X-User-ID", userID)
	r.Header.Set("X-Workspace-ID", workspaceID)
	return r
}
