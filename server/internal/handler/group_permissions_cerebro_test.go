// CEREBRO-PATCH(group-permissions-cerebro-test): unit coverage for the
// capability-gate helper that fronts CreateAgent / CreateRuntimeSetupToken
// (JEH-1009). The real seam is wired up in the integration test where a
// full DB-backed grouppermissions Service answers the can-do questions; this
// file pins the handler-side decision tree against a mock invoker so the
// behaviour stays predictable across refactors.
//
// CEREBRO-PATCH(group-permissions-cerebro-test-pr4): JEH-1009 PR 4 — extends
// the stub invoker with CanUseAgent / CanSeeProjectViaGroup / VisibleXxxIDs
// and adds nil-invoker fail-open coverage for the new agent-allowlist and
// list-filter helpers.
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
	resolve     func(ctx context.Context, ws, user pgtype.UUID) ([]pgtype.UUID, error)
	canRT       func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error)
	canAG       func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) (bool, error)
	canUseR     func(ctx context.Context, viewer GroupPermissionsViewer, rt pgtype.UUID) (bool, error)
	canUseA     func(ctx context.Context, viewer GroupPermissionsViewer, ag pgtype.UUID) (bool, error)
	canSeeProj  func(ctx context.Context, viewer GroupPermissionsViewer, pr pgtype.UUID) (bool, error)
	visAgents   func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error)
	visRuntimes func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error)
	visProjects func(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error)
	audUsers    func(ctx context.Context, pr pgtype.UUID) ([]pgtype.UUID, error)
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

func (s *stubGroupPermissions) CanUseAgent(ctx context.Context, viewer GroupPermissionsViewer, ag pgtype.UUID) (bool, error) {
	if s.canUseA == nil {
		return false, nil
	}
	return s.canUseA(ctx, viewer, ag)
}

func (s *stubGroupPermissions) CanSeeProjectViaGroup(ctx context.Context, viewer GroupPermissionsViewer, pr pgtype.UUID) (bool, error) {
	if s.canSeeProj == nil {
		return false, nil
	}
	return s.canSeeProj(ctx, viewer, pr)
}

func (s *stubGroupPermissions) VisibleAgentIDs(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error) {
	if s.visAgents == nil {
		return nil, nil
	}
	return s.visAgents(ctx, viewer, ws)
}

func (s *stubGroupPermissions) VisibleRuntimeIDs(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error) {
	if s.visRuntimes == nil {
		return nil, nil
	}
	return s.visRuntimes(ctx, viewer, ws)
}

func (s *stubGroupPermissions) VisibleProjectIDs(ctx context.Context, viewer GroupPermissionsViewer, ws pgtype.UUID) ([]pgtype.UUID, error) {
	if s.visProjects == nil {
		return nil, nil
	}
	return s.visProjects(ctx, viewer, ws)
}

func (s *stubGroupPermissions) ProjectAudienceUserIDs(ctx context.Context, pr pgtype.UUID) ([]pgtype.UUID, error) {
	if s.audUsers == nil {
		return nil, nil
	}
	return s.audUsers(ctx, pr)
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

// JEH-1009 PR 4 — pin the decision tree for the new agent-allowlist gate. The
// runtime-allowlist gate already covers admin / nil-invoker / member-grant /
// member-deny in PR 3's tests; the agent gate must match that shape so the
// trigger path can't drift away from the runtime path silently.

func TestCerebroRequireAgentAccess_NilInvokerPasses(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/api/chat/sessions", nil)
	w := httptest.NewRecorder()
	if !h.cerebroRequireAgentAccess(w, r, "00000000-0000-0000-0000-000000000000", pgtype.UUID{}) {
		t.Fatalf("nil GroupPermissions: gate must pass, body=%q", w.Body.String())
	}
}

func TestCerebroCanUseAgent_NilInvokerReturnsTrue(t *testing.T) {
	// The non-writing companion used by canAssignAgent / validateAssigneePair
	// must mirror cerebroRequireAgentAccess and fail open on nil-invoker so
	// upstream-only test fixtures continue to work.
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	allowed, err := h.cerebroCanUseAgent(context.Background(), r, "00000000-0000-0000-0000-000000000000", pgtype.UUID{})
	if err != nil {
		t.Fatalf("nil invoker should not error, got %v", err)
	}
	if !allowed {
		t.Fatalf("nil invoker should return true (fail-open)")
	}
}

func TestCerebroAgentAccessAsValidatorError_NilInvokerPasses(t *testing.T) {
	// validatorError must report status=0 (no rejection) when there is no
	// seam — otherwise existing upstream tests that drive validateAssigneePair
	// without a cerebro service would fail closed.
	h := &Handler{}
	r := httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	status, msg := h.cerebroAgentAccessAsValidatorError(context.Background(), r, "00000000-0000-0000-0000-000000000000", pgtype.UUID{})
	if status != 0 {
		t.Fatalf("nil-invoker validatorError: expected status 0, got %d (%q)", status, msg)
	}
}

func TestCerebroVisibleAgentIDSet_NilInvokerNoFilter(t *testing.T) {
	// No seam → hasFilter=false, ok=true, ownerExempt=nil. ListAgents skips
	// the filter loop and returns the upstream-visible set unchanged.
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	set, hasFilter, exempt, ok := h.cerebroVisibleAgentIDSet(context.Background(), r, "00000000-0000-0000-0000-000000000000")
	if !ok {
		t.Fatalf("nil invoker: expected ok=true")
	}
	if hasFilter {
		t.Fatalf("nil invoker: expected hasFilter=false")
	}
	if set != nil {
		t.Fatalf("nil invoker: expected nil set, got %v", set)
	}
	if exempt != nil {
		t.Fatalf("nil invoker: expected nil ownerExempt, got non-nil")
	}
}

func TestCerebroVisibleRuntimeIDSet_NilInvokerNoFilter(t *testing.T) {
	h := &Handler{}
	r := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	_, hasFilter, exempt, ok := h.cerebroVisibleRuntimeIDSet(context.Background(), r, "00000000-0000-0000-0000-000000000000")
	if !ok || hasFilter {
		t.Fatalf("nil invoker: expected ok=true hasFilter=false, got ok=%v hasFilter=%v", ok, hasFilter)
	}
	if exempt != nil {
		t.Fatalf("nil invoker: expected nil ownerExempt")
	}
}

// CEREBRO-PATCH(group-permissions-cerebro-test-owner-exempt): JEH-1056 / JEH-1057 —
// decision table coverage for ownerExemptFn so visibility ≤ create rights stays
// pinned across refactors.
//
// JEH-1056 / JEH-1057 — owner-exemption decision table. Bind the rule "see
// your own row iff you have the create capability" against the four input
// combinations so the cerebro list-filter can't silently regress to either
// "always show owner's rows" (security) or "never show owner's rows"
// (the bug being fixed here).
func TestOwnerExemptFn_TruthTable(t *testing.T) {
	viewer := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}
	other := pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}, Valid: true}
	unset := pgtype.UUID{} // OwnerID column null in DB.

	cases := []struct {
		name      string
		canCreate bool
		owner     pgtype.UUID
		want      bool
	}{
		{"owns and can create — admitted", true, viewer, true},
		{"owns but cannot create — denied (no ghost-rows after capability revoke)", false, viewer, false},
		{"does not own but can create — denied (group allowlist is the only path)", true, other, false},
		{"does not own and cannot create — denied", false, other, false},
		{"owner column unset — never admitted (no NULL == NULL match)", true, unset, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ownerExemptFn(viewer, tc.canCreate)(tc.owner)
			if got != tc.want {
				t.Fatalf("canCreate=%v owner=%v: got %v, want %v", tc.canCreate, tc.owner, got, tc.want)
			}
		})
	}
}

// Defence in depth: when the viewer's own UserID column is unset (should not
// happen post-requireUserID, but the closure is constructed early), the
// exemption must fail closed — otherwise a NULL == NULL match would admit
// every owner-less runtime/agent to the unauthenticated viewer.
func TestOwnerExemptFn_InvalidViewerUserIDFailsClosed(t *testing.T) {
	exempt := ownerExemptFn(pgtype.UUID{}, true)
	if exempt(pgtype.UUID{Bytes: [16]byte{1}, Valid: true}) {
		t.Fatalf("invalid viewer.UserID with canCreate=true must NOT admit any owner")
	}
	if exempt(pgtype.UUID{}) {
		t.Fatalf("invalid viewer.UserID must NOT admit owner=null either")
	}
}

func TestCerebroCanSeeProjectViaGroup_NilInvokerReturnsFalse(t *testing.T) {
	// The helper layered into canAccessProject: when no seam is wired, it
	// returns false so the existing access decision (admin / workspace /
	// project_member) is the sole authority — fail-closed on this branch
	// only widens access via the seam when it exists, never narrows it.
	h := &Handler{}
	if h.cerebroCanSeeProjectViaGroup(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}) {
		t.Fatalf("nil invoker: cerebroCanSeeProjectViaGroup must return false")
	}
}
