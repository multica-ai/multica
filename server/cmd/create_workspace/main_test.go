package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	testPool   *pgxpool.Pool
	testUserID string
)

const (
	testEmail     = "create-workspace-cmd@multica.ai"
	testSlugInfix = "cmd-tests-"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool

	if err := cleanupFixtures(ctx, pool); err != nil {
		fmt.Printf("Failed to clean fixtures: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Create Workspace Cmd Test User", testEmail,
	).Scan(&testUserID); err != nil {
		fmt.Printf("Failed to create fixture user: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()

	if err := cleanupFixtures(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean fixtures: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

// deleteWorkspacesBySlug removes workspaces whose slug matches the LIKE
// pattern, along with the rows that hang off them. The schema has no foreign
// keys and no cascading deletes (AGENTS.md), so deleting the workspace alone
// would leave its members and issue statuses behind as orphans that accumulate
// across runs. Slugs here only contain hyphens, so the pattern needs no
// escaping.
func deleteWorkspacesBySlug(ctx context.Context, pool *pgxpool.Pool, pattern string) error {
	for _, stmt := range []string{
		`DELETE FROM issue_status WHERE workspace_id IN (SELECT id FROM workspace WHERE slug LIKE $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug LIKE $1)`,
		`DELETE FROM workspace WHERE slug LIKE $1`,
	} {
		if _, err := pool.Exec(ctx, stmt, pattern); err != nil {
			return err
		}
	}
	return nil
}

// cleanupFixtures removes everything this suite creates. Workspaces are matched
// by the shared slug infix rather than by id so a test that dies between the
// insert and its own cleanup cannot leave a row behind for the next run.
func cleanupFixtures(ctx context.Context, pool *pgxpool.Pool) error {
	if err := deleteWorkspacesBySlug(ctx, pool, testSlugInfix+"%"); err != nil {
		return err
	}
	// The fixture user is deleted and re-inserted on every run, so a run that
	// died mid-suite would otherwise fail the next one on the unique email.
	// Its memberships go first for the same no-cascade reason.
	if _, err := pool.Exec(ctx,
		`DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, testEmail,
	); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, testEmail)
	return err
}

func cleanupWorkspace(t *testing.T, slug string) {
	t.Helper()
	if err := deleteWorkspacesBySlug(context.Background(), testPool, slug); err != nil {
		t.Fatalf("cleanup workspace %q: %v", slug, err)
	}
}

// A workspace is only usable once it has an owner member and a status catalog:
// every workspace-scoped query filters on membership, and an issue cannot be
// created before its status resolves. Asserting on the row the command writes
// is the point of the command — the API path cannot be used when
// DISABLE_WORKSPACE_CREATION is on.
func TestRunCreateWorkspace_CreatesOwnerMemberAndIssueStatuses(t *testing.T) {
	ctx := context.Background()
	const slug = testSlugInfix + "happy"
	cleanupWorkspace(t, slug)

	created, err := runCreateWorkspace(ctx, testPool, createInput{
		Slug:  slug,
		Name:  "Cmd Happy",
		Owner: testEmail,
	})
	if err != nil {
		t.Fatalf("runCreateWorkspace: %v", err)
	}
	if created.Slug != slug {
		t.Fatalf("created slug = %q, want %q", created.Slug, slug)
	}

	var role string
	if err := testPool.QueryRow(ctx,
		`SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2`,
		created.ID, testUserID,
	).Scan(&role); err != nil {
		t.Fatalf("owner member row missing for workspace %s: %v", created.ID, err)
	}
	if role != "owner" {
		t.Fatalf("member role = %q, want owner", role)
	}

	var statuses int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_status WHERE workspace_id = $1`, created.ID,
	).Scan(&statuses); err != nil {
		t.Fatalf("count issue_status: %v", err)
	}
	if statuses != 7 {
		t.Fatalf("issue_status rows = %d, want the 7 built-in statuses", statuses)
	}
}

// The owner has to exist before the workspace does. Resolving the email first
// keeps a typo from producing an ownerless workspace nobody can administer.
func TestRunCreateWorkspace_RejectsUnknownOwner(t *testing.T) {
	ctx := context.Background()
	const slug = testSlugInfix + "unknown-owner"
	cleanupWorkspace(t, slug)

	_, err := runCreateWorkspace(ctx, testPool, createInput{
		Slug:  slug,
		Name:  "Cmd Unknown Owner",
		Owner: "nobody@multica.ai",
	})
	if err == nil {
		t.Fatal("expected an error for an owner email with no account, got nil")
	}
	if !strings.Contains(err.Error(), "nobody@multica.ai") {
		t.Fatalf("error should name the missing owner, got: %v", err)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("count workspace: %v", err)
	}
	if count != 0 {
		t.Fatalf("workspace was created despite the missing owner (rows=%d)", count)
	}
}

// A reserved slug would collide with a top-level frontend route, which is why
// the API rejects it. The command must not be a way around that.
func TestRunCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	ctx := context.Background()

	_, err := runCreateWorkspace(ctx, testPool, createInput{
		Slug:  "login",
		Name:  "Cmd Reserved",
		Owner: testEmail,
	})
	if err == nil {
		t.Fatal("expected the reserved slug to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("error = %v, want a reserved-slug rejection", err)
	}
}

func TestRunCreateWorkspace_RejectsDuplicateSlug(t *testing.T) {
	ctx := context.Background()
	const slug = testSlugInfix + "duplicate"
	cleanupWorkspace(t, slug)

	if _, err := runCreateWorkspace(ctx, testPool, createInput{
		Slug: slug, Name: "Cmd Duplicate", Owner: testEmail,
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := runCreateWorkspace(ctx, testPool, createInput{
		Slug: slug, Name: "Cmd Duplicate Again", Owner: testEmail,
	})
	if err == nil {
		t.Fatal("expected the duplicate slug to be rejected, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v, want a slug-already-exists rejection", err)
	}
}
