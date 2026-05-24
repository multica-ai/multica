package approvals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// These tests are DB-backed and mirror the references package harness: they
// skip cleanly when no Postgres is reachable (e.g. `go test ./...` on a laptop
// without a DB), and run in full under CI where a pgvector service is present.

var (
	testPool      *pgxpool.Pool
	testWorkspace pgtype.UUID
	testApprover  pgtype.UUID
	testRequester pgtype.UUID
	testGroup     pgtype.UUID
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Println("approvals: skipping tests, DATABASE_URL unset")
		os.Exit(0)
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("approvals: skipping tests, no DB: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("approvals: skipping tests, DB unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool

	if err := setupFixture(ctx, pool); err != nil {
		fmt.Printf("approvals: setup failed: %v\n", err)
		_ = teardownFixture(context.Background(), pool)
		pool.Close()
		os.Exit(1)
	}
	code := m.Run()
	_ = teardownFixture(context.Background(), pool)
	pool.Close()
	os.Exit(code)
}

const (
	fxApproverEmail  = "approvals-approver@multica.ai"
	fxRequesterEmail = "approvals-requester@multica.ai"
	fxWorkspaceSlug  = "approvals-test-ws"
)

func setupFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := teardownFixture(ctx, pool); err != nil {
		return err
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Approvals Approver", fxApproverEmail,
	).Scan(&testApprover); err != nil {
		return fmt.Errorf("create approver: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Approvals Requester", fxRequesterEmail,
	).Scan(&testRequester); err != nil {
		return fmt.Errorf("create requester: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"Approvals Tests", fxWorkspaceSlug, "Approval inbox test workspace", "APR",
	).Scan(&testWorkspace); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')`, testWorkspace, testApprover); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO cerebro_group (workspace_id, name)
		VALUES ($1, $2) RETURNING id`,
		testWorkspace, "Approvals Reviewers",
	).Scan(&testGroup); err != nil {
		return fmt.Errorf("create group: %w", err)
	}
	return nil
}

func teardownFixture(ctx context.Context, pool *pgxpool.Pool) error {
	// Deleting the workspace cascades to grants/approvals; users are removed by email.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, fxWorkspaceSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = ANY($1)`,
		[]string{fxApproverEmail, fxRequesterEmail}); err != nil {
		return err
	}
	return nil
}

func newService() *Service {
	return New(cerebrodb.New(testPool), testPool, nil)
}

func seedPending(t *testing.T, svc *Service, expires pgtype.Timestamptz) cerebrodb.CerebroApprovalRequest {
	t.Helper()
	row, err := svc.Intake(context.Background(), IntakeParams{
		WorkspaceID:   testWorkspace,
		RequesterType: RequesterAgent,
		RequesterID:   testRequester,
		Capability:    "issue.delete",
		Resource:      "issues/*",
		Reason:        "approval required",
		Context:       map[string]any{"tool": "delete_issue"},
		ExpiresAt:     expires,
	})
	if err != nil {
		t.Fatalf("intake: %v", err)
	}
	if row.Status != StatusPending {
		t.Fatalf("expected pending, got %q", row.Status)
	}
	return row
}

// TestConcurrentApprove_OnlyOneWins fires N approvers at the same pending ask.
// Exactly one must succeed; the rest must get ErrAlreadyDecided. This proves
// the conditional UPDATE (WHERE status='pending') serialises decisions.
func TestConcurrentApprove_OnlyOneWins(t *testing.T) {
	svc := newService()
	ask := seedPending(t, svc, pgtype.Timestamptz{})

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins, conflicts, others int

	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Approve(context.Background(), ask.ID, testWorkspace, testApprover, "", SurfaceUI)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrAlreadyDecided):
				conflicts++
			default:
				others++
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("expected exactly 1 winner, got %d (conflicts=%d others=%d)", wins, conflicts, others)
	}
	if conflicts != racers-1 {
		t.Fatalf("expected %d conflicts, got %d", racers-1, conflicts)
	}

	got, err := svc.Get(context.Background(), ask.ID, testWorkspace)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusApproved {
		t.Fatalf("final status = %q, want approved", got.Status)
	}

	// Exactly one terminal audit row should accompany the single winning decision.
	audit, err := svc.Cerebro.ListCerebroApprovalAudit(context.Background(), cerebrodb.ListCerebroApprovalAuditParams{
		WorkspaceID: testWorkspace, Column2: ask.ID, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	approvedRows := 0
	for _, a := range audit {
		if a.Action == "approved" {
			approvedRows++
		}
	}
	if approvedRows != 1 {
		t.Fatalf("expected 1 'approved' audit row, got %d", approvedRows)
	}
}

// TestApproveThenReject_SecondConflicts proves a terminal ask cannot be
// re-decided through a different verb either.
func TestApproveThenReject_SecondConflicts(t *testing.T) {
	svc := newService()
	ask := seedPending(t, svc, pgtype.Timestamptz{})

	if _, err := svc.Approve(context.Background(), ask.ID, testWorkspace, testApprover, "ok", SurfaceUI); err != nil {
		t.Fatalf("approve: %v", err)
	}
	_, err := svc.Reject(context.Background(), ask.ID, testWorkspace, testApprover, "changed mind", SurfaceUI)
	if !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("expected ErrAlreadyDecided on re-decide, got %v", err)
	}
}

// TestDecide_NotFound returns ErrNotFound for an unknown id.
func TestDecide_NotFound(t *testing.T) {
	svc := newService()
	_, err := svc.Approve(context.Background(), randomUUID(t), testWorkspace, testApprover, "", SurfaceUI)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestExpireDue sweeps a past-deadline pending ask to expired and leaves a
// future-deadline one untouched.
func TestExpireDue(t *testing.T) {
	svc := newService()
	past := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	future := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	stale := seedPending(t, svc, past)
	fresh := seedPending(t, svc, future)

	n, err := svc.ExpireDue(context.Background())
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 expired, got %d", n)
	}

	gotStale, _ := svc.Get(context.Background(), stale.ID, testWorkspace)
	if gotStale.Status != StatusExpired {
		t.Fatalf("stale ask status = %q, want expired", gotStale.Status)
	}
	gotFresh, _ := svc.Get(context.Background(), fresh.ID, testWorkspace)
	if gotFresh.Status != StatusPending {
		t.Fatalf("fresh ask status = %q, want pending", gotFresh.Status)
	}

	// An expired ask can no longer be approved.
	_, err = svc.Approve(context.Background(), stale.ID, testWorkspace, testApprover, "", SurfaceUI)
	if !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("expected ErrAlreadyDecided on expired ask, got %v", err)
	}
}

// TestDelegate reassigns a pending ask and records the target + audit.
func TestDelegate(t *testing.T) {
	svc := newService()
	ask := seedPending(t, svc, pgtype.Timestamptz{})

	row, err := svc.Delegate(context.Background(), ask.ID, testWorkspace, testApprover, DelegateToGroup, testGroup, "please review", SurfaceUI)
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if row.Status != StatusDelegated {
		t.Fatalf("status = %q, want delegated", row.Status)
	}
	if row.DelegatedToType.String != DelegateToGroup || row.DelegatedToID != testGroup {
		t.Fatalf("delegate target not recorded: type=%q id=%v", row.DelegatedToType.String, row.DelegatedToID)
	}

	// A delegated ask is no longer pending, so a second delegate conflicts.
	_, err = svc.Delegate(context.Background(), ask.ID, testWorkspace, testApprover, DelegateToMember, testApprover, "", SurfaceUI)
	if !errors.Is(err, ErrAlreadyDecided) {
		t.Fatalf("expected ErrAlreadyDecided on re-delegate, got %v", err)
	}

	audit, err := svc.Cerebro.ListCerebroApprovalAudit(context.Background(), cerebrodb.ListCerebroApprovalAuditParams{
		WorkspaceID: testWorkspace, Column2: ask.ID, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var created, delegated int
	for _, a := range audit {
		switch a.Action {
		case "created":
			created++
		case "delegated":
			delegated++
		}
	}
	if created != 1 || delegated != 1 {
		t.Fatalf("audit trail wrong: created=%d delegated=%d", created, delegated)
	}
}

// TestDelegate_InvalidTarget rejects bad delegate inputs before touching the DB.
func TestDelegate_InvalidTarget(t *testing.T) {
	svc := newService()
	ask := seedPending(t, svc, pgtype.Timestamptz{})
	if _, err := svc.Delegate(context.Background(), ask.ID, testWorkspace, testApprover, "everyone", testGroup, "", SurfaceUI); err == nil {
		t.Fatal("expected error for invalid delegate type")
	}
	if _, err := svc.Delegate(context.Background(), ask.ID, testWorkspace, testApprover, DelegateToMember, pgtype.UUID{}, "", SurfaceUI); err == nil {
		t.Fatal("expected error for missing delegate id")
	}
}

func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatalf("gen uuid: %v", err)
	}
	return id
}
