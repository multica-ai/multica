package grouppermissions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
)

// IsKnownCapability is a pure function; no DB needed.
func TestIsKnownCapability(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{CapabilityCreateRuntime, true},
		{CapabilityCreateAgent, true},
		{"", false},
		{"manage_billing", false},
		{"CREATE_RUNTIME", false}, // case-sensitive
	}
	for _, c := range cases {
		if got := IsKnownCapability(c.in); got != c.want {
			t.Fatalf("IsKnownCapability(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Admin override short-circuits before touching the DB. A Service with nil
// queries proves the early return — if the admin bypass regressed and asked
// the DB, this test would nil-panic.
func TestAdminOverride_SkipsDB(t *testing.T) {
	svc := &Service{} // intentionally no Cerebro / Bus — admin must short-circuit.
	ctx := context.Background()
	ws := uuid(1)
	viewer := Viewer{UserID: uuid(2), IsAdmin: true}

	if ok, err := svc.CanCreateRuntime(ctx, viewer, ws); err != nil || !ok {
		t.Fatalf("admin CanCreateRuntime: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanCreateAgent(ctx, viewer, ws); err != nil || !ok {
		t.Fatalf("admin CanCreateAgent: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanUseRuntime(ctx, viewer, uuid(3)); err != nil || !ok {
		t.Fatalf("admin CanUseRuntime: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanUseAgent(ctx, viewer, uuid(4)); err != nil || !ok {
		t.Fatalf("admin CanUseAgent: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanSeeProjectViaGroup(ctx, viewer, uuid(5)); err != nil || !ok {
		t.Fatalf("admin CanSeeProjectViaGroup: ok=%v err=%v", ok, err)
	}
}

// Non-admin, no user id — also short-circuits to false without touching DB.
func TestUnauthenticatedViewer_DeniesWithoutDB(t *testing.T) {
	svc := &Service{}
	ctx := context.Background()
	ws := uuid(1)
	viewer := Viewer{} // zero value: !IsAdmin and !UserID.Valid

	if ok, err := svc.CanCreateRuntime(ctx, viewer, ws); err != nil || ok {
		t.Fatalf("unauthenticated CanCreateRuntime: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanUseRuntime(ctx, viewer, uuid(3)); err != nil || ok {
		t.Fatalf("unauthenticated CanUseRuntime: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanUseAgent(ctx, viewer, uuid(4)); err != nil || ok {
		t.Fatalf("unauthenticated CanUseAgent: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.CanSeeProjectViaGroup(ctx, viewer, uuid(5)); err != nil || ok {
		t.Fatalf("unauthenticated CanSeeProjectViaGroup: ok=%v err=%v", ok, err)
	}
}

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

// -----------------------------------------------------------------------------
// DB integration tests — skipped when DATABASE_URL points at an unreachable
// instance. Schema (and the JEH-1008 backfill) must already be applied.
// -----------------------------------------------------------------------------

const (
	gpTestEmail         = "grouppermissions-test@multica.ai"
	gpTestName          = "Group Permissions Test"
	gpTestWorkspaceSlug = "grouppermissions-tests"
)

var (
	gpTestPool        *pgxpool.Pool
	gpTestUserID      pgtype.UUID
	gpTestWorkspaceID pgtype.UUID
	gpTestRuntimeID   pgtype.UUID
	gpTestAgentID     pgtype.UUID
	gpTestProjectID   pgtype.UUID
	gpTestGroupID     pgtype.UUID
	gpTestDefaultID   pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping grouppermissions integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping grouppermissions integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}

	if err := cleanupGPTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean grouppermissions test fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	if err := setupGPTestFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up grouppermissions test fixture: %v\n", err)
		_ = cleanupGPTestFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}

	gpTestPool = pool
	code := m.Run()

	if err := cleanupGPTestFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean grouppermissions test fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupGPTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, gpTestName, gpTestEmail).Scan(&gpTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Group Permissions Tests", gpTestWorkspaceSlug, "Temporary workspace", "GPT").Scan(&gpTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, gpTestWorkspaceID, gpTestUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		VALUES ($1, 'gp-test-runtime', 'cloud', 'gp-test', 'online')
		RETURNING id
	`, gpTestWorkspaceID).Scan(&gpTestRuntimeID); err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'gp-test-agent', 'cloud', $2)
		RETURNING id
	`, gpTestWorkspaceID, gpTestRuntimeID).Scan(&gpTestAgentID); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'GP test project')
		RETURNING id
	`, gpTestWorkspaceID).Scan(&gpTestProjectID); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	// Backfill triggers off workspace existence at migration time; for a
	// workspace created after the migration we synthesise the same state.
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_group (workspace_id, name, description)
		VALUES ($1, 'All members', 'GP test default group')
		RETURNING id
	`, gpTestWorkspaceID).Scan(&gpTestDefaultID); err != nil {
		return fmt.Errorf("create default group: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_workspace_default_group (workspace_id, group_id)
		VALUES ($1, $2)
	`, gpTestWorkspaceID, gpTestDefaultID); err != nil {
		return fmt.Errorf("set default group: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_group (workspace_id, name, description)
		VALUES ($1, 'Engineering', 'GP test secondary group')
		RETURNING id
	`, gpTestWorkspaceID).Scan(&gpTestGroupID); err != nil {
		return fmt.Errorf("create secondary group: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_group_member (group_id, user_id)
		VALUES ($1, $2)
	`, gpTestGroupID, gpTestUserID); err != nil {
		return fmt.Errorf("add group member: %w", err)
	}
	return nil
}

func cleanupGPTestFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// workspace cascade drops member + agent + runtime + project + cerebro_*.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, gpTestWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, gpTestEmail); err != nil {
		return err
	}
	return nil
}

func newGPTestService(t *testing.T) *Service {
	t.Helper()
	if gpTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping grouppermissions integration test")
	}
	return NewService(cerebrodb.New(gpTestPool), events.New())
}

// Resolve picks up the seeded group membership; PR 3 callers depend on this.
func TestResolveGroupIDs_ReturnsUserGroups(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()

	got, err := svc.ResolveGroupIDs(ctx, gpTestWorkspaceID, gpTestUserID)
	if err != nil {
		t.Fatalf("ResolveGroupIDs failed: %v", err)
	}
	// Only the Engineering group was seeded for this user; default-group
	// membership requires the runtime-side auto-add hook (PR 3).
	if len(got) != 1 || got[0].Bytes != gpTestGroupID.Bytes {
		t.Fatalf("ResolveGroupIDs = %v, want [%v]", got, gpTestGroupID)
	}
}

// Capability set + remove round-trip; HasCerebroCapability reflects state.
func TestCapability_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()

	if _, err := svc.SetCapability(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, CapabilityCreateRuntime); err != nil {
		t.Fatalf("SetCapability failed: %v", err)
	}

	viewer := Viewer{UserID: gpTestUserID}
	ok, err := svc.CanCreateRuntime(ctx, viewer, gpTestWorkspaceID)
	if err != nil || !ok {
		t.Fatalf("CanCreateRuntime after grant: ok=%v err=%v", ok, err)
	}

	// A capability not granted to the user's groups stays denied.
	if ok, err := svc.CanCreateAgent(ctx, viewer, gpTestWorkspaceID); err != nil || ok {
		t.Fatalf("CanCreateAgent without grant: ok=%v err=%v", ok, err)
	}

	if err := svc.RemoveCapability(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, CapabilityCreateRuntime); err != nil {
		t.Fatalf("RemoveCapability failed: %v", err)
	}
	if ok, err := svc.CanCreateRuntime(ctx, viewer, gpTestWorkspaceID); err != nil || ok {
		t.Fatalf("CanCreateRuntime after revoke: ok=%v err=%v", ok, err)
	}

	// Removing an unknown capability is a 400 at the handler layer; the
	// service surfaces ErrUnknownCapability so the caller can map it.
	err = svc.RemoveCapability(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, "manage_billing")
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

// Runtime allowlist: add → CanUseRuntime true; remove → false.
func TestRuntimeAllowlist_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	if ok, err := svc.CanUseRuntime(ctx, viewer, gpTestRuntimeID); err != nil || ok {
		t.Fatalf("CanUseRuntime before grant: ok=%v err=%v", ok, err)
	}

	if _, err := svc.AddRuntime(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestRuntimeID); err != nil {
		t.Fatalf("AddRuntime failed: %v", err)
	}
	if ok, err := svc.CanUseRuntime(ctx, viewer, gpTestRuntimeID); err != nil || !ok {
		t.Fatalf("CanUseRuntime after grant: ok=%v err=%v", ok, err)
	}

	if err := svc.RemoveRuntime(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestRuntimeID); err != nil {
		t.Fatalf("RemoveRuntime failed: %v", err)
	}
	if ok, err := svc.CanUseRuntime(ctx, viewer, gpTestRuntimeID); err != nil || ok {
		t.Fatalf("CanUseRuntime after revoke: ok=%v err=%v", ok, err)
	}
}

// Agent allowlist: same shape as runtime.
func TestAgentAllowlist_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	if _, err := svc.AddAgent(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestAgentID); err != nil {
		t.Fatalf("AddAgent failed: %v", err)
	}
	if ok, err := svc.CanUseAgent(ctx, viewer, gpTestAgentID); err != nil || !ok {
		t.Fatalf("CanUseAgent after grant: ok=%v err=%v", ok, err)
	}
	if err := svc.RemoveAgent(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestAgentID); err != nil {
		t.Fatalf("RemoveAgent failed: %v", err)
	}
	if ok, err := svc.CanUseAgent(ctx, viewer, gpTestAgentID); err != nil || ok {
		t.Fatalf("CanUseAgent after revoke: ok=%v err=%v", ok, err)
	}
}

// Project group-access: add → CanSeeProjectViaGroup true.
func TestProjectGroupAccess_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	if ok, err := svc.CanSeeProjectViaGroup(ctx, viewer, gpTestProjectID); err != nil || ok {
		t.Fatalf("CanSeeProjectViaGroup before grant: ok=%v err=%v", ok, err)
	}
	if _, err := svc.AddProjectGroup(ctx, gpTestWorkspaceID, gpTestUserID, gpTestProjectID, gpTestGroupID); err != nil {
		t.Fatalf("AddProjectGroup failed: %v", err)
	}
	if ok, err := svc.CanSeeProjectViaGroup(ctx, viewer, gpTestProjectID); err != nil || !ok {
		t.Fatalf("CanSeeProjectViaGroup after grant: ok=%v err=%v", ok, err)
	}
	if err := svc.RemoveProjectGroup(ctx, gpTestWorkspaceID, gpTestUserID, gpTestProjectID, gpTestGroupID); err != nil {
		t.Fatalf("RemoveProjectGroup failed: %v", err)
	}
	if ok, err := svc.CanSeeProjectViaGroup(ctx, viewer, gpTestProjectID); err != nil || ok {
		t.Fatalf("CanSeeProjectViaGroup after revoke: ok=%v err=%v", ok, err)
	}
}

// Default-group helpers: pointer present after fixture setup; mismatched
// workspace returns pgx.ErrNoRows.
func TestDefaultGroupLookup(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()

	id, err := svc.DefaultGroupID(ctx, gpTestWorkspaceID)
	if err != nil {
		t.Fatalf("DefaultGroupID failed: %v", err)
	}
	if id.Bytes != gpTestDefaultID.Bytes {
		t.Fatalf("DefaultGroupID = %v, want %v", id, gpTestDefaultID)
	}

	isDefault, err := svc.IsDefaultGroup(ctx, gpTestDefaultID)
	if err != nil || !isDefault {
		t.Fatalf("IsDefaultGroup(default): ok=%v err=%v", isDefault, err)
	}
	isDefault, err = svc.IsDefaultGroup(ctx, gpTestGroupID)
	if err != nil || isDefault {
		t.Fatalf("IsDefaultGroup(secondary) should be false: ok=%v err=%v", isDefault, err)
	}

	// A workspace with no default returns pgx.ErrNoRows so the caller can
	// fall back to whatever pre-migration behaviour they need.
	var phantom pgtype.UUID
	phantom.Valid = true
	for i := range phantom.Bytes {
		phantom.Bytes[i] = 0xee
	}
	if _, err := svc.DefaultGroupID(ctx, phantom); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("DefaultGroupID(phantom) = %v, want pgx.ErrNoRows", err)
	}
}

// Unknown capability is rejected by SetCapability with a typed error.
func TestSetCapability_RejectsUnknown(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()

	_, err := svc.SetCapability(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, "manage_billing")
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

// JEH-1009 PR 4 — VisibleAgentIDs / VisibleRuntimeIDs / VisibleProjectIDs +
// the inverse ProjectAudienceUserIDs are how ListAgents / ListAgentRuntimes /
// audienceForRestrictedProject pick which rows a non-admin viewer sees.

func TestVisibleAgentIDs_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	ids, err := svc.VisibleAgentIDs(ctx, viewer, gpTestWorkspaceID)
	if err != nil {
		t.Fatalf("VisibleAgentIDs failed: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("VisibleAgentIDs before grant: expected empty, got %v", ids)
	}

	if _, err := svc.AddAgent(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestAgentID); err != nil {
		t.Fatalf("AddAgent failed: %v", err)
	}
	defer svc.RemoveAgent(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestAgentID)

	ids, err = svc.VisibleAgentIDs(ctx, viewer, gpTestWorkspaceID)
	if err != nil {
		t.Fatalf("VisibleAgentIDs after grant: %v", err)
	}
	if len(ids) != 1 || ids[0].Bytes != gpTestAgentID.Bytes {
		t.Fatalf("VisibleAgentIDs after grant: expected [%v], got %v", gpTestAgentID, ids)
	}

	// Admin override → nil (no filter).
	if got, err := svc.VisibleAgentIDs(ctx, Viewer{UserID: gpTestUserID, IsAdmin: true}, gpTestWorkspaceID); err != nil || got != nil {
		t.Fatalf("VisibleAgentIDs(admin) should return (nil, nil), got %v / %v", got, err)
	}
}

func TestVisibleRuntimeIDs_RoundTrip(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	if _, err := svc.AddRuntime(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestRuntimeID); err != nil {
		t.Fatalf("AddRuntime failed: %v", err)
	}
	defer svc.RemoveRuntime(ctx, gpTestWorkspaceID, gpTestUserID, gpTestGroupID, gpTestRuntimeID)

	ids, err := svc.VisibleRuntimeIDs(ctx, viewer, gpTestWorkspaceID)
	if err != nil {
		t.Fatalf("VisibleRuntimeIDs failed: %v", err)
	}
	if len(ids) != 1 || ids[0].Bytes != gpTestRuntimeID.Bytes {
		t.Fatalf("VisibleRuntimeIDs: expected [%v], got %v", gpTestRuntimeID, ids)
	}
}

func TestVisibleProjectIDsAndAudience(t *testing.T) {
	svc := newGPTestService(t)
	ctx := context.Background()
	viewer := Viewer{UserID: gpTestUserID}

	if _, err := svc.AddProjectGroup(ctx, gpTestWorkspaceID, gpTestUserID, gpTestProjectID, gpTestGroupID); err != nil {
		t.Fatalf("AddProjectGroup failed: %v", err)
	}
	defer svc.RemoveProjectGroup(ctx, gpTestWorkspaceID, gpTestUserID, gpTestProjectID, gpTestGroupID)

	ids, err := svc.VisibleProjectIDs(ctx, viewer, gpTestWorkspaceID)
	if err != nil {
		t.Fatalf("VisibleProjectIDs failed: %v", err)
	}
	if len(ids) != 1 || ids[0].Bytes != gpTestProjectID.Bytes {
		t.Fatalf("VisibleProjectIDs: expected [%v], got %v", gpTestProjectID, ids)
	}

	users, err := svc.ProjectAudienceUserIDs(ctx, gpTestProjectID)
	if err != nil {
		t.Fatalf("ProjectAudienceUserIDs failed: %v", err)
	}
	if len(users) != 1 || users[0].Bytes != gpTestUserID.Bytes {
		t.Fatalf("ProjectAudienceUserIDs: expected [%v], got %v", gpTestUserID, users)
	}
}
