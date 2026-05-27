package privateagentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

var (
	testPool        *pgxpool.Pool
	testWorkspaceID pgtype.UUID
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping cerebro/privateagentrun tests: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping cerebro/privateagentrun tests: db unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'cerebro-private-agent-run-tests'`)
	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Private Agent Run Tests', 'cerebro-private-agent-run-tests', 'unit-test fixture', 'PAR')
		RETURNING id
	`).Scan(&wsID); err != nil {
		fmt.Printf("workspace insert: %v\n", err)
		os.Exit(1)
	}
	testWorkspaceID = parseUUID(wsID)

	code := m.Run()

	pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	pool.Close()
	os.Exit(code)
}

// TestRequestPrivateAgentRun_CreatesOwnerInboxItemAndDedups covers the two
// DONE criteria for the backend side: a member tag of an unowned private agent
// creates exactly one run-request in the owner's inbox, and a repeated tag of
// the same agent on the same comment does not duplicate it.
func TestRequestPrivateAgentRun_CreatesOwnerInboxItemAndDedups(t *testing.T) {
	svc, ownerID, requesterID, agentID, issueID := setupFixture(t, "dedup")
	ctx := context.Background()
	commentID := newUUID(t)

	if err := svc.RequestPrivateAgentRun(ctx, uuidString(testWorkspaceID), agentID, ownerID, issueID, commentID, requesterID, "Designer Agent"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if got := countRequests(t, ownerID); got != 1 {
		t.Fatalf("after first tag: want 1 run-request, got %d", got)
	}

	// Verify the stored shape: recipient = owner, actor = requester, the
	// (agent, comment, requester) triple in details, issue carried.
	var (
		recipientID string
		actorType   string
		actorID     string
		typ         string
		severity    string
		title       string
		gotIssue    string
		detailsJSON []byte
	)
	if err := testPool.QueryRow(ctx, `
		SELECT recipient_id, actor_type, actor_id, type, severity, title, issue_id, details
		FROM inbox_item
		WHERE recipient_id = $1 AND type = 'private_agent_run_request'
	`, uuidString(ownerID)).Scan(&recipientID, &actorType, &actorID, &typ, &severity, &title, &gotIssue, &detailsJSON); err != nil {
		t.Fatalf("read item: %v", err)
	}
	if recipientID != uuidString(ownerID) {
		t.Errorf("recipient = %s, want owner %s", recipientID, uuidString(ownerID))
	}
	if actorType != "member" || actorID != uuidString(requesterID) {
		t.Errorf("actor = %s/%s, want member/%s", actorType, actorID, uuidString(requesterID))
	}
	if severity != "action_required" {
		t.Errorf("severity = %s, want action_required", severity)
	}
	if title != "Designer Agent" {
		t.Errorf("title = %q, want agent name", title)
	}
	if gotIssue != uuidString(issueID) {
		t.Errorf("issue_id = %s, want %s", gotIssue, uuidString(issueID))
	}
	var details map[string]string
	if err := json.Unmarshal(detailsJSON, &details); err != nil {
		t.Fatalf("details json: %v", err)
	}
	if details["agent_id"] != uuidString(agentID) || details["comment_id"] != uuidString(commentID) || details["requester_id"] != uuidString(requesterID) {
		t.Errorf("details = %v, missing agent/comment/requester ids", details)
	}

	// Repeated tag on the same comment must not spam the owner.
	if err := svc.RequestPrivateAgentRun(ctx, uuidString(testWorkspaceID), agentID, ownerID, issueID, commentID, requesterID, "Designer Agent"); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if got := countRequests(t, ownerID); got != 1 {
		t.Fatalf("after duplicate tag: want 1 run-request (deduped), got %d", got)
	}

	// A tag on a different comment is a distinct request.
	if err := svc.RequestPrivateAgentRun(ctx, uuidString(testWorkspaceID), agentID, ownerID, issueID, newUUID(t), requesterID, "Designer Agent"); err != nil {
		t.Fatalf("third request: %v", err)
	}
	if got := countRequests(t, ownerID); got != 2 {
		t.Fatalf("after tag on a new comment: want 2 run-requests, got %d", got)
	}
}

// TestRequestPrivateAgentRun_FlagOffNoRequest verifies the whole behavior is
// gated: with the requester's flag overridden off, the tag drops silently
// (legacy behavior) and no inbox item is created.
func TestRequestPrivateAgentRun_FlagOffNoRequest(t *testing.T) {
	svc, ownerID, requesterID, agentID, issueID := setupFixture(t, "flagoff")
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `
		INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		VALUES ($1, $2, 'cerebro_private_agent_requests', false)
	`, uuidString(testWorkspaceID), uuidString(requesterID)); err != nil {
		t.Fatalf("set flag override: %v", err)
	}

	if err := svc.RequestPrivateAgentRun(ctx, uuidString(testWorkspaceID), agentID, ownerID, issueID, newUUID(t), requesterID, "Designer Agent"); err != nil {
		t.Fatalf("request with flag off: %v", err)
	}
	if got := countRequests(t, ownerID); got != 0 {
		t.Fatalf("flag off: want 0 run-requests, got %d", got)
	}
}

func setupFixture(t *testing.T, label string) (*Service, pgtype.UUID, pgtype.UUID, pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	cq := cerebrodb.New(testPool)
	svc := New(cq, events.New())

	ownerID := createMember(t, label+"-owner")
	requesterID := createMember(t, label+"-requester")

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'PAR Runtime '||$2, 'cloud', 'test', 'online', 'device', '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, label).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'par-private-agent-'||$2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, label, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'par-issue-'||$2, 'member', $3)
		RETURNING id
	`, testWorkspaceID, label, requesterID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	return svc, ownerID, requesterID, parseUUID(agentID), parseUUID(issueID)
}

func createMember(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	email := name + "@cerebro-private-agent-run-tests.local"
	var userID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, name, email).Scan(&userID); err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, testWorkspaceID, userID); err != nil {
		t.Fatalf("create member %q: %v", name, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	return parseUUID(userID)
}

func countRequests(t *testing.T, ownerID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM inbox_item WHERE recipient_id = $1 AND type = 'private_agent_run_request'
	`, uuidString(ownerID)).Scan(&n); err != nil {
		t.Fatalf("count requests: %v", err)
	}
	return n
}

func newUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var s string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&s); err != nil {
		t.Fatalf("gen uuid: %v", err)
	}
	return parseUUID(s)
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func uuidString(u pgtype.UUID) string {
	return util.UUIDToString(u)
}
