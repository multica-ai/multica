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
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
			_, err := svc.Approve(context.Background(), ask.ID, testWorkspace, testApprover, "", SurfaceUI, futureExpiry())
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

	if _, err := svc.Approve(context.Background(), ask.ID, testWorkspace, testApprover, "ok", SurfaceUI, futureExpiry()); err != nil {
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
	_, err := svc.Approve(context.Background(), randomUUID(t), testWorkspace, testApprover, "", SurfaceUI, futureExpiry())
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
	_, err = svc.Approve(context.Background(), stale.ID, testWorkspace, testApprover, "", SurfaceUI, futureExpiry())
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

// TestEnrich_NamesAndIssue proves the TECH-3498 human-readable contract: the
// approvals list/get responses carry the requester's NAME (not a raw UUID) and
// the issue identifier + title, resolved from the stored context. Two rows are
// seeded: one carrying context.task_id (resolved via the agent task → issue) and
// one carrying context.issue_id directly. Neither requester_name may be a UUID.
func TestEnrich_NamesAndIssue(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	h := NewHandler(svc).WithToolPolicy(toolpolicy.NewStore(testPool), db.New(testPool))

	// Seed a runtime + agent so the requester resolves to the agent's name.
	var runtimeID, agentID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
		VALUES ($1, 'Enrich Runtime', 'cloud', 'test') RETURNING id`,
		testWorkspace).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	const agentName = "Sara CTO Bot"
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'cloud', $3, $4) RETURNING id`,
		testWorkspace, agentName, runtimeID, testApprover).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Seed an issue (number 4321) the agent is working on.
	const issueTitle = "Make the approvals inbox human-readable"
	const issueNumber = 4321
	var issueID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, number, creator_type, creator_id)
		VALUES ($1, $2, $3, 'agent', $4) RETURNING id`,
		testWorkspace, issueTitle, issueNumber, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Seed an agent task linking the agent to the issue (the task_id → issue path).
	var taskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id)
		VALUES ($1, $2, $3) RETURNING id`,
		agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create agent task: %v", err)
	}

	// The workspace fixture uses issue_prefix "APR", so the identifier is APR-4321.
	wantIdentifier := "APR-" + fmt.Sprintf("%d", issueNumber)

	t.Run("via task_id", func(t *testing.T) {
		row, err := svc.Intake(ctx, IntakeParams{
			WorkspaceID:   testWorkspace,
			RequesterType: RequesterAgent,
			RequesterID:   agentID,
			Agent:         agentID,
			Capability:    "draft_reply",
			Resource:      "draft_reply",
			Reason:        "connection tool requires approval",
			Context:       map[string]any{"task_id": uuidStr(t, taskID), "tool_name": "draft_reply"},
		})
		if err != nil {
			t.Fatalf("intake: %v", err)
		}
		got := h.enrich(ctx, requestToResponse(row), row, nil)
		assertEnriched(t, got, agentName, wantIdentifier, issueTitle, agentID)
	})

	t.Run("via issue_id", func(t *testing.T) {
		row, err := svc.Intake(ctx, IntakeParams{
			WorkspaceID:   testWorkspace,
			RequesterType: RequesterAgent,
			RequesterID:   agentID,
			Agent:         agentID,
			Capability:    "web_fetch",
			Resource:      "web_fetch",
			Reason:        "external network requires approval",
			Context:       map[string]any{"issue_id": uuidStr(t, issueID), "tool_name": "web_fetch"},
		})
		if err != nil {
			t.Fatalf("intake: %v", err)
		}
		got := h.enrich(ctx, requestToResponse(row), row, nil)
		assertEnriched(t, got, agentName, wantIdentifier, issueTitle, agentID)
	})

	t.Run("missing context yields humanized label, never a uuid", func(t *testing.T) {
		// No agent_id and no context → requester_name falls back to "Agent".
		row, err := svc.Intake(ctx, IntakeParams{
			WorkspaceID:   testWorkspace,
			RequesterType: RequesterAgent,
			RequesterID:   testRequester,
			Capability:    "credential_list",
			Resource:      "*",
			Reason:        "no enrichable context",
		})
		if err != nil {
			t.Fatalf("intake: %v", err)
		}
		got := h.enrich(ctx, requestToResponse(row), row, nil)
		if got.RequesterName == "" {
			t.Fatal("requester_name must never be empty")
		}
		if got.RequesterName == uuidStr(t, testRequester) || got.RequesterName == uuidStr(t, agentID) {
			t.Fatalf("requester_name leaked a raw UUID: %q", got.RequesterName)
		}
		if got.IssueIdentifier != nil {
			t.Fatalf("expected no issue identifier without context, got %q", *got.IssueIdentifier)
		}
	})
}

func assertEnriched(t *testing.T, got approvalResponse, wantAgentName, wantIdentifier, wantTitle string, agentID pgtype.UUID) {
	t.Helper()
	if got.RequesterName != wantAgentName {
		t.Fatalf("requester_name = %q, want %q", got.RequesterName, wantAgentName)
	}
	if got.RequesterName == uuidStr(t, agentID) {
		t.Fatalf("requester_name leaked a raw UUID: %q", got.RequesterName)
	}
	if got.AgentName == nil || *got.AgentName != wantAgentName {
		t.Fatalf("agent_name = %v, want %q", got.AgentName, wantAgentName)
	}
	if got.IssueIdentifier == nil || *got.IssueIdentifier != wantIdentifier {
		t.Fatalf("issue_identifier = %v, want %q", got.IssueIdentifier, wantIdentifier)
	}
	if got.IssueTitle == nil || *got.IssueTitle != wantTitle {
		t.Fatalf("issue_title = %v, want %q", got.IssueTitle, wantTitle)
	}
	if got.IssueID == nil || *got.IssueID == "" {
		t.Fatal("issue_id must be set when the issue resolves")
	}
}

func uuidStr(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	var s pgtype.Text
	if err := testPool.QueryRow(context.Background(), `SELECT $1::uuid::text`, id).Scan(&s); err != nil {
		t.Fatalf("uuid to string: %v", err)
	}
	return s.String
}

func randomUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatalf("gen uuid: %v", err)
	}
	return id
}

// futureExpiry is the period-grant expiry the legacy tests pass to Approve so
// the approved row stays reusable (FindReusable filters expires_at > now()).
func futureExpiry() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
}

// TestApproveWithExpiry_FutureIsReusable proves the TECH-3498 "approve for a
// period" contract end-to-end: an approve with a future expires_at makes the
// row reusable (FindReusable returns it), while an approve with a now() expiry
// (the one-shot default) is NOT reusable for a future call.
func TestApproveWithExpiry_FutureIsReusable(t *testing.T) {
	svc := newService()
	agent := randomUUID(t)
	const capName, res = "draft_reply", "draft_reply"
	q := ReusableQuery{WorkspaceID: testWorkspace, Agent: agent, Capability: capName, Resource: res}

	// Period grant: approve with a future expiry → reusable.
	pending := seedAskFor(t, svc, agent, capName, res, pgtype.Timestamptz{})
	if _, err := svc.Approve(context.Background(), pending.ID, testWorkspace, testApprover, "", SurfaceUI,
		pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}); err != nil {
		t.Fatalf("approve (period): %v", err)
	}
	got, ok, err := svc.FindReusable(context.Background(), q)
	if err != nil || !ok {
		t.Fatalf("period grant: ok=%v err=%v, want reusable", ok, err)
	}
	if got.ID != pending.ID || got.Status != StatusApproved {
		t.Fatalf("period grant: got id=%v status=%q, want approved id=%v", got.ID, got.Status, pending.ID)
	}

	// One-shot: a fresh ask approved with a now() expiry must NOT be reusable for
	// a future call (the in-flight blocked call proceeds via Await, not expiry).
	onceAgent := randomUUID(t)
	once := seedAskFor(t, svc, onceAgent, capName, res, pgtype.Timestamptz{})
	if _, err := svc.Approve(context.Background(), once.ID, testWorkspace, testApprover, "", SurfaceUI,
		pgtype.Timestamptz{Time: time.Now(), Valid: true}); err != nil {
		t.Fatalf("approve (once): %v", err)
	}
	if _, ok, _ := svc.FindReusable(context.Background(), ReusableQuery{WorkspaceID: testWorkspace, Agent: onceAgent, Capability: capName, Resource: res}); ok {
		t.Fatal("one-shot approve (now() expiry) must not be reusable for a future call")
	}
}

// TestGrantAtLevel_AgentWritesAllow proves the TECH-3498 "grant at a permission
// level" contract: after a successful approve, escalating to the agent level
// writes a permanent Allow into the tool-policy chain for that agent. Uses a
// connection-tool approval row so the connection:<name> key + tool-name
// resource_pattern derivation is exercised too.
func TestGrantAtLevel_AgentWritesAllow(t *testing.T) {
	svc := newService()
	store := toolpolicy.NewStore(testPool)
	h := NewHandler(svc).WithToolPolicy(store, nil)

	agent := randomUUID(t)
	const conn, tool = "customer-service", "draft_reply"
	row, err := svc.Intake(context.Background(), IntakeParams{
		WorkspaceID:   testWorkspace,
		RequesterType: RequesterAgent,
		RequesterID:   testRequester,
		Agent:         agent,
		Capability:    tool,
		Resource:      tool,
		Reason:        "connection tool requires approval",
		Context:       map[string]any{"connection": conn, "connection_tool": true},
	})
	if err != nil {
		t.Fatalf("intake: %v", err)
	}
	approved, err := svc.Approve(context.Background(), row.ID, testWorkspace, testApprover, "", SurfaceUI, futureExpiry())
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	if gErr := h.grantAtLevel(context.Background(), approved, "agent", testApprover); gErr != nil {
		t.Fatalf("grantAtLevel: %v", gErr)
	}

	// A permanent Allow must now exist at the agent layer, keyed on the
	// connection:<conn> tool with the tool name as the resource_pattern.
	settings, err := store.ListForSubject(context.Background(), testWorkspace, toolpolicy.LayerAgent, agent)
	if err != nil {
		t.Fatalf("list for subject: %v", err)
	}
	var found bool
	for _, st := range settings {
		if st.ToolKey == "connection:"+conn && st.ResourcePattern == tool {
			found = true
			if st.Setting != toolpolicy.SettingAllow {
				t.Fatalf("grant-at-level agent: setting = %q, want allow", st.Setting)
			}
		}
	}
	if !found {
		t.Fatalf("grant-at-level agent: no Allow row written for connection:%s / %s; got %+v", conn, tool, settings)
	}
}

// seedAskFor intakes a pending ask scoped to a specific agent/capability/resource
// so the FindReusable matching can be exercised.
func seedAskFor(t *testing.T, svc *Service, agent pgtype.UUID, capability, resource string, expires pgtype.Timestamptz) cerebrodb.CerebroApprovalRequest {
	t.Helper()
	row, err := svc.Intake(context.Background(), IntakeParams{
		WorkspaceID:   testWorkspace,
		RequesterType: RequesterAgent,
		RequesterID:   testRequester,
		Agent:         agent,
		Capability:    capability,
		Resource:      resource,
		Reason:        "approval required",
		ExpiresAt:     expires,
	})
	if err != nil {
		t.Fatalf("intake: %v", err)
	}
	return row
}

// TestFindReusable proves the FIR-2586 dedup contract that stops a retried
// repo checkout from piling up duplicate asks: a pending ask for the same
// (agent, capability, resource) is rejoined, an approved one is honoured, and
// anything else (different scope, rejected, expired) yields no match so a fresh
// ask is raised.
func TestFindReusable(t *testing.T) {
	svc := newService()
	agent := randomUUID(t)
	const cap, res = "repo.checkout", "https://github.com/firtal-group/x"
	q := ReusableQuery{WorkspaceID: testWorkspace, Agent: agent, Capability: cap, Resource: res}

	// No ask yet → no match.
	if _, ok, err := svc.FindReusable(context.Background(), q); err != nil || ok {
		t.Fatalf("empty: ok=%v err=%v, want ok=false", ok, err)
	}

	// Pending ask → matched and returned so the retry rejoins it.
	pending := seedAskFor(t, svc, agent, cap, res, pgtype.Timestamptz{})
	got, ok, err := svc.FindReusable(context.Background(), q)
	if err != nil || !ok {
		t.Fatalf("pending: ok=%v err=%v, want ok=true", ok, err)
	}
	if got.ID != pending.ID || got.Status != StatusPending {
		t.Fatalf("pending: got id=%v status=%q, want id=%v pending", got.ID, got.Status, pending.ID)
	}

	// A different resource must NOT match this ask.
	if _, ok, _ := svc.FindReusable(context.Background(), ReusableQuery{WorkspaceID: testWorkspace, Agent: agent, Capability: cap, Resource: "https://github.com/firtal-group/y"}); ok {
		t.Fatal("different resource must not match")
	}
	// A different agent must NOT match this ask.
	if _, ok, _ := svc.FindReusable(context.Background(), ReusableQuery{WorkspaceID: testWorkspace, Agent: randomUUID(t), Capability: cap, Resource: res}); ok {
		t.Fatal("different agent must not match")
	}

	// Once approved, the ask is honoured (approved status returned) so a later
	// retry proceeds instead of asking again.
	if _, err := svc.Approve(context.Background(), pending.ID, testWorkspace, testApprover, "", SurfaceUI, futureExpiry()); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, ok, err = svc.FindReusable(context.Background(), q)
	if err != nil || !ok {
		t.Fatalf("approved: ok=%v err=%v, want ok=true", ok, err)
	}
	if got.Status != StatusApproved {
		t.Fatalf("approved: got status=%q, want approved", got.Status)
	}

	// A rejected ask (different scope, isolated) must NOT be reusable.
	rej := seedAskFor(t, svc, agent, cap, "https://github.com/firtal-group/rejected", pgtype.Timestamptz{})
	if _, err := svc.Reject(context.Background(), rej.ID, testWorkspace, testApprover, "", SurfaceUI); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, ok, _ := svc.FindReusable(context.Background(), ReusableQuery{WorkspaceID: testWorkspace, Agent: agent, Capability: cap, Resource: "https://github.com/firtal-group/rejected"}); ok {
		t.Fatal("rejected ask must not be reusable")
	}

	// An already-expired ask (expires_at in the past) must NOT be reusable.
	expAgent := randomUUID(t)
	seedAskFor(t, svc, expAgent, cap, res, pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true})
	if _, ok, _ := svc.FindReusable(context.Background(), ReusableQuery{WorkspaceID: testWorkspace, Agent: expAgent, Capability: cap, Resource: res}); ok {
		t.Fatal("expired ask must not be reusable")
	}
}
