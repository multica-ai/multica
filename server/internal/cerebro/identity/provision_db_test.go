package identity

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/identitysync"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// These tests exercise the real auto-provisioning path against a live DB.
// They auto-skip when DATABASE_URL / the default local pg is unreachable,
// matching the convention in server/internal/handler/handler_test.go.
//
// Investigation harness for FIR "auto-creation of members" (issue #2662):
// confirms (1) the mechanism creates a member on a domain match, (2) it is
// silently skipped when the workspace has no matching domain, and (3) it is
// idempotent on a repeated call.

func provisionTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skip: cannot connect to db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skip: db not reachable: %v", err)
	}
	// Close LAST: registered first, so LIFO runs it after every fixture
	// cleanup. A defer pool.Close() in the test would close the pool before
	// t.Cleanup fixtures run, leaking rows on a dead connection.
	t.Cleanup(pool.Close)
	return pool
}

// seedWorkspace inserts a throwaway workspace and returns its UUID string.
func seedWorkspace(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "Provision Test "+slug, slug, "provision test", "PRV").Scan(&id); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() {
		// member rows FK-reference the workspace; clear them first so the
		// workspace delete (and its cascade to auth settings) succeeds.
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// seedUser inserts a throwaway user and returns the db.User.
func seedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, email string) db.User {
	t.Helper()
	q := db.New(pool)
	u, err := q.CreateUser(ctx, db.CreateUserParams{Name: email, Email: email})
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		// Clear member rows that FK-reference this user before deleting it.
		// (Cleanups run LIFO, so this fires before the workspace cleanup.)
		_, _ = pool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, uuidString(u.ID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, uuidString(u.ID))
	})
	return u
}

func memberCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workspaceID, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	return n
}

func TestProvisionMembership_HappyPath(t *testing.T) {
	pool := provisionTestPool(t)
	ctx := context.Background()

	svc := New(cerebrodb.New(pool), db.New(pool))
	wsID := seedWorkspace(t, ctx, pool, fmt.Sprintf("prov-happy-%d", os.Getpid()))
	user := seedUser(t, ctx, pool, fmt.Sprintf("happy-%d@firtal.com", os.Getpid()))

	// Configure the workspace to auto-provision firtal.com with role "admin".
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_workspace_auth_settings (workspace_id, google_signup_domains, default_role)
		VALUES ($1, ARRAY['firtal.com'], 'admin')
	`, wsID); err != nil {
		t.Fatalf("seed auth settings: %v", err)
	}

	res, err := svc.ProvisionMembershipForNewUser(ctx, user, "code")
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected provisioning to run, got skipped: %s", res.SkipReason)
	}
	if got := memberCount(t, ctx, pool, wsID, uuidString(user.ID)); got != 1 {
		t.Fatalf("expected 1 member row, got %d", got)
	}
	// Verify the configured default_role was honored.
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM member WHERE workspace_id=$1 AND user_id=$2`,
		wsID, uuidString(user.ID)).Scan(&role); err != nil {
		t.Fatalf("read member role: %v", err)
	}
	if role != "admin" {
		t.Fatalf("expected role 'admin', got %q", role)
	}

	// Idempotency: a second call must not error or duplicate.
	if _, err := svc.ProvisionMembershipForNewUser(ctx, user, "code"); err != nil {
		t.Fatalf("second provision returned error: %v", err)
	}
	if got := memberCount(t, ctx, pool, wsID, uuidString(user.ID)); got != 1 {
		t.Fatalf("idempotency broken: expected 1 member row, got %d", got)
	}
}

func TestProvisionMembership_SkippedWhenDomainNotConfigured(t *testing.T) {
	pool := provisionTestPool(t)
	ctx := context.Background()

	svc := New(cerebrodb.New(pool), db.New(pool))
	wsID := seedWorkspace(t, ctx, pool, fmt.Sprintf("prov-skip-%d", os.Getpid()))
	// Use a domain no workspace in the instance lists, so we isolate the
	// "no match" path. (Note: matching is GLOBAL across all workspaces, not
	// scoped to wsID — a real firtal.com user fans into every workspace that
	// lists firtal.com.)
	user := seedUser(t, ctx, pool, fmt.Sprintf("skip-%d@unconfigured-%d.example", os.Getpid(), os.Getpid()))

	// No workspace lists this domain — the common real-world state before an
	// admin opens the settings tab and adds their domain.
	res, err := svc.ProvisionMembershipForNewUser(ctx, user, "code")
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if !res.Skipped {
		t.Fatalf("expected skip when no workspace matches the domain")
	}
	if got := memberCount(t, ctx, pool, wsID, uuidString(user.ID)); got != 0 {
		t.Fatalf("expected 0 member rows when skipped, got %d", got)
	}
}

// groupMemberCount returns how many cerebro_group_member rows exist for a
// (group, user) pair — used to assert placement + idempotency.
func groupMemberCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, groupID, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM cerebro_group_member WHERE group_id = $1 AND user_id = $2
	`, groupID, userID).Scan(&n); err != nil {
		t.Fatalf("count group members: %v", err)
	}
	return n
}

type recordingGroupSyncer struct {
	workspaceIDs []pgtype.UUID
}

func (s *recordingGroupSyncer) SyncWorkspaceNow(ctx context.Context, workspaceID pgtype.UUID) (identitysync.SyncResult, error) {
	s.workspaceIDs = append(s.workspaceIDs, workspaceID)
	return identitysync.SyncResult{GroupsProcessed: len(identitysync.SyncedGroups)}, nil
}

// TestProvisionMembership_PlacesUserInDefaultGroup proves that auto-provisioning
// puts the user in the workspace's configured default group (FIR-2523 +
// onboarding: "automatically placed in a group"), and is idempotent on a
// repeated (returning-login / self-heal) call.
func TestProvisionMembership_PlacesUserInDefaultGroup(t *testing.T) {
	pool := provisionTestPool(t)
	ctx := context.Background()

	svc := New(cerebrodb.New(pool), db.New(pool))
	wsID := seedWorkspace(t, ctx, pool, fmt.Sprintf("prov-group-%d", os.Getpid()))
	// Unique domain so ONLY this workspace matches — isolates the assertion
	// from any other workspace in the instance that lists a shared domain.
	domain := fmt.Sprintf("grpdom-%d.example", os.Getpid())
	user := seedUser(t, ctx, pool, fmt.Sprintf("groupuser-%d@%s", os.Getpid(), domain))

	// Create a group in the workspace and mark it the workspace default.
	var groupID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_group (workspace_id, name) VALUES ($1, 'Default Team') RETURNING id
	`, wsID).Scan(&groupID); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_workspace_default_group (workspace_id, group_id) VALUES ($1, $2)
	`, wsID, groupID); err != nil {
		t.Fatalf("seed default group: %v", err)
	}
	// Group artifacts are cascade-deleted when the workspace is removed by the
	// seedWorkspace cleanup, but clear the membership first defensively.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cerebro_group_member WHERE group_id = $1`, groupID)
	})

	// Configure the workspace to auto-provision this domain.
	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_workspace_auth_settings (workspace_id, google_signup_domains, default_role)
		VALUES ($1, ARRAY[$2], 'member')
	`, wsID, domain); err != nil {
		t.Fatalf("seed auth settings: %v", err)
	}

	res, err := svc.ProvisionMembershipForNewUser(ctx, user, "google")
	if err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected provisioning to run, got skipped: %s", res.SkipReason)
	}
	if got := memberCount(t, ctx, pool, wsID, uuidString(user.ID)); got != 1 {
		t.Fatalf("expected 1 workspace member row, got %d", got)
	}
	if got := groupMemberCount(t, ctx, pool, groupID, uuidString(user.ID)); got != 1 {
		t.Fatalf("expected user placed in default group (1 row), got %d", got)
	}
	if len(res.GroupIDs) != 1 {
		t.Fatalf("expected 1 group id in result, got %d", len(res.GroupIDs))
	}

	// The user must be marked onboarded so the post-login router sends them
	// straight into the workspace rather than the first-run onboarding wall.
	var onboardedAt *string
	if err := pool.QueryRow(ctx, `SELECT onboarded_at::text FROM "user" WHERE id = $1`,
		uuidString(user.ID)).Scan(&onboardedAt); err != nil {
		t.Fatalf("read onboarded_at: %v", err)
	}
	if onboardedAt == nil {
		t.Fatalf("expected onboarded_at to be set after auto-provisioning, got NULL")
	}

	// Self-heal / idempotency: a second login must not duplicate or error.
	if _, err := svc.ProvisionMembershipForNewUser(ctx, user, "google"); err != nil {
		t.Fatalf("second provision returned error: %v", err)
	}
	if got := groupMemberCount(t, ctx, pool, groupID, uuidString(user.ID)); got != 1 {
		t.Fatalf("idempotency broken: expected 1 group member row, got %d", got)
	}
}

func TestProvisionMembership_TriggersGoogleWorkspaceGroupSyncWhenEnabled(t *testing.T) {
	pool := provisionTestPool(t)
	ctx := context.Background()

	syncer := &recordingGroupSyncer{}
	svc := NewWithGroupSyncer(cerebrodb.New(pool), db.New(pool), syncer)
	wsID := seedWorkspace(t, ctx, pool, fmt.Sprintf("prov-sync-%d", os.Getpid()))
	domain := fmt.Sprintf("syncdom-%d.example", os.Getpid())
	user := seedUser(t, ctx, pool, fmt.Sprintf("syncuser-%d@%s", os.Getpid(), domain))

	if _, err := pool.Exec(ctx, `
		INSERT INTO cerebro_workspace_auth_settings (workspace_id, google_signup_domains, default_role, google_workspace_sync_enabled)
		VALUES ($1, ARRAY[$2], 'member', true)
	`, wsID, domain); err != nil {
		t.Fatalf("seed auth settings: %v", err)
	}

	if _, err := svc.ProvisionMembershipForNewUser(ctx, user, "google"); err != nil {
		t.Fatalf("provision returned error: %v", err)
	}
	if len(syncer.workspaceIDs) != 1 {
		t.Fatalf("expected one on-demand group sync, got %d", len(syncer.workspaceIDs))
	}
	if got := uuidString(syncer.workspaceIDs[0]); got != wsID {
		t.Fatalf("sync workspace = %s, want %s", got, wsID)
	}
}
