package channels

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Tests in this file run against the local development database. When
// DATABASE_URL is unset or the DB is unreachable the suite is skipped so
// "go test ./..." stays green on machines without a local Postgres — the
// service is exercised by the handler suite anyway.

var (
	testPool        *pgxpool.Pool
	testSvc         *Service
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
		fmt.Printf("Skipping cerebro/channels tests: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping cerebro/channels tests: db unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	testSvc = New(cerebrodb.New(pool), db.New(pool), nil, events.New())

	// Single workspace shared across tests. Re-create on every run so the
	// suite is order-independent.
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'cerebro-channels-tests'`); err != nil {
		fmt.Printf("workspace cleanup: %v\n", err)
		os.Exit(1)
	}
	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Channels Tests', 'cerebro-channels-tests', 'unit-test fixture', 'CCT')
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

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

// createTestUserAndMember adds a user + member row to the test workspace and
// returns the user_id. Cleanup is best-effort — the workspace teardown
// cascades.
func createTestUserAndMember(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, name+"@cerebro-channels-tests.local").Scan(&userID); err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("create member %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return parseUUID(userID)
}

// createTestAgent inserts a workspace agent (and a backing runtime row,
// which is NOT NULL on the schema) and returns the agent id. Used as the
// "third party" target in DM-promotion tests.
func createTestAgent(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	ownerID := createTestUserAndMember(t, name+"-owner")
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, last_seen_at
		) VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, name+"-runtime", name+"-provider", name+"-device").Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime for agent %q: %v", name, err)
	}
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, ownerID).Scan(&agentID); err != nil {
		t.Fatalf("create agent %q: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, parseUUID(agentID))
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, parseUUID(runtimeID))
	})
	return parseUUID(agentID)
}

// createTestDM creates a kind='dm' issue with the two given members as
// subscribers and returns the issue + the loaded row. Mirrors the path
// CreateChannel takes for kind='dm'.
func createTestDM(t *testing.T, memberA, memberB pgtype.UUID) db.Issue {
	t.Helper()
	ctx := context.Background()
	q := db.New(testPool)

	number, err := q.IncrementIssueCounter(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("increment issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: testWorkspaceID,
		Title:       "",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   memberA,
		Position:    0,
		Number:      number,
		Kind:        pgtype.Text{String: "dm", Valid: true},
	})
	if err != nil {
		t.Fatalf("create dm issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	for _, m := range []pgtype.UUID{memberA, memberB} {
		if err := q.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: issue.ID, UserType: "member", UserID: m, Reason: "creator",
		}); err != nil {
			t.Fatalf("subscribe %v: %v", m, err)
		}
	}
	return issue
}

func loadIssue(t *testing.T, id pgtype.UUID) db.Issue {
	t.Helper()
	issue, err := db.New(testPool).GetIssue(context.Background(), id)
	if err != nil {
		t.Fatalf("reload issue: %v", err)
	}
	return issue
}

func mentionAgent(id pgtype.UUID) string {
	var u pgtype.UUID = id
	out := ""
	_ = out
	// pgtype.UUID has Bytes when Valid; format as canonical UUID string.
	return "Hi [@bot](mention://agent/" + uuidString(u) + ") please look"
}

func mentionMember(id pgtype.UUID) string {
	return "ping [@user](mention://member/" + uuidString(id) + ")"
}

func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TestPromoteDMOnMention_AgentMention covers the happy path: a DM with two
// members receives a comment that mentions a third-party agent. The issue's
// kind should flip to 'channel', the agent should be subscribed, and the
// title should be auto-generated.
func TestPromoteDMOnMention_AgentMention(t *testing.T) {
	ctx := context.Background()
	memberA := createTestUserAndMember(t, "alice-agent")
	memberB := createTestUserAndMember(t, "bob-agent")
	agentID := createTestAgent(t, "bot-agent")
	dm := createTestDM(t, memberA, memberB)

	comment := db.Comment{
		ID:         parseUUID("11111111-1111-1111-1111-111111111111"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    mentionAgent(agentID),
		Type:       "comment",
	}

	testSvc.PromoteDMOnMention(ctx, dm, comment, nil, uuidString(testWorkspaceID), "member", uuidString(memberA))

	after := loadIssue(t, dm.ID)
	if after.Kind != "channel" {
		t.Fatalf("expected kind=channel after agent mention, got %q", after.Kind)
	}
	if after.Title == "" {
		t.Fatalf("expected auto-generated title, got empty string")
	}

	subscribed, err := db.New(testPool).IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
		IssueID: dm.ID, UserType: "agent", UserID: agentID,
	})
	if err != nil || !subscribed {
		t.Fatalf("expected mentioned agent to be a subscriber, got err=%v subscribed=%v", err, subscribed)
	}
}

// TestPromoteDMOnMention_NoOpOnExistingMember ensures mentioning a member
// who is already a participant of the DM does NOT promote — the conversation
// is still 1:1, just with the peer's name pinged for emphasis.
func TestPromoteDMOnMention_NoOpOnExistingMember(t *testing.T) {
	ctx := context.Background()
	memberA := createTestUserAndMember(t, "alice-noop")
	memberB := createTestUserAndMember(t, "bob-noop")
	dm := createTestDM(t, memberA, memberB)

	comment := db.Comment{
		ID:         parseUUID("22222222-2222-2222-2222-222222222222"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    mentionMember(memberB), // mention the existing peer
		Type:       "comment",
	}
	testSvc.PromoteDMOnMention(ctx, dm, comment, nil, uuidString(testWorkspaceID), "member", uuidString(memberA))

	after := loadIssue(t, dm.ID)
	if after.Kind != "dm" {
		t.Fatalf("expected kind=dm to remain after mentioning existing member, got %q", after.Kind)
	}
}

// TestPromoteDMOnMention_NoOpOnAlreadyChannel verifies the function is a
// no-op when the issue is already a channel. This is the dedup guard for
// the case where two comments land back-to-back and the second one is
// processed after the first has flipped the kind.
func TestPromoteDMOnMention_NoOpOnAlreadyChannel(t *testing.T) {
	ctx := context.Background()
	memberA := createTestUserAndMember(t, "alice-already")
	memberB := createTestUserAndMember(t, "bob-already")
	agentID := createTestAgent(t, "bot-already")
	dm := createTestDM(t, memberA, memberB)

	// Pre-flip the kind so the function sees an already-promoted issue.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET kind = 'channel' WHERE id = $1`, dm.ID); err != nil {
		t.Fatalf("pre-flip kind: %v", err)
	}
	dm.Kind = "channel"

	comment := db.Comment{
		ID:         parseUUID("33333333-3333-3333-3333-333333333333"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    mentionAgent(agentID),
		Type:       "comment",
	}
	testSvc.PromoteDMOnMention(ctx, dm, comment, nil, uuidString(testWorkspaceID), "member", uuidString(memberA))

	// Subscriber list should not have the agent — the early-return on
	// kind!='dm' kicked in.
	subscribed, _ := db.New(testPool).IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
		IssueID: dm.ID, UserType: "agent", UserID: agentID,
	})
	if subscribed {
		t.Fatalf("expected no subscription on already-channel kind, but agent was added")
	}
}

// TestPromoteDMOnMention_NoOpOnMentionAll ensures @all does not promote.
// "Everyone in this DM" still means the same two people.
func TestPromoteDMOnMention_NoOpOnMentionAll(t *testing.T) {
	ctx := context.Background()
	memberA := createTestUserAndMember(t, "alice-all")
	memberB := createTestUserAndMember(t, "bob-all")
	dm := createTestDM(t, memberA, memberB)

	comment := db.Comment{
		ID:         parseUUID("44444444-4444-4444-4444-444444444444"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    "[@everyone](mention://all/all) lunch?",
		Type:       "comment",
	}
	testSvc.PromoteDMOnMention(ctx, dm, comment, nil, uuidString(testWorkspaceID), "member", uuidString(memberA))

	after := loadIssue(t, dm.ID)
	if after.Kind != "dm" {
		t.Fatalf("expected kind=dm to remain after @all, got %q", after.Kind)
	}
}

// TestPromoteDMOnMention_InheritsFromParent covers the inheritance rule:
// a member reply with no explicit mention inherits the thread root's
// mentions when the root was authored by a member. This matters because
// the comment handler reuses the same shouldInheritParentMentions logic
// for enqueueing — the promotion path must agree, otherwise an inherited
// agent mention would queue work in a channel that's still a DM.
func TestPromoteDMOnMention_InheritsFromParent(t *testing.T) {
	ctx := context.Background()
	memberA := createTestUserAndMember(t, "alice-inherit")
	memberB := createTestUserAndMember(t, "bob-inherit")
	agentID := createTestAgent(t, "bot-inherit")
	dm := createTestDM(t, memberA, memberB)

	parent := db.Comment{
		ID:         parseUUID("55555555-5555-5555-5555-555555555555"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    mentionAgent(agentID),
		Type:       "comment",
	}
	reply := db.Comment{
		ID:         parseUUID("66666666-6666-6666-6666-666666666666"),
		IssueID:    dm.ID,
		AuthorType: "member",
		AuthorID:   memberA,
		Content:    "follow-up", // no mentions
		Type:       "comment",
		ParentID:   parent.ID,
	}

	testSvc.PromoteDMOnMention(ctx, dm, reply, &parent, uuidString(testWorkspaceID), "member", uuidString(memberA))

	after := loadIssue(t, dm.ID)
	if after.Kind != "channel" {
		t.Fatalf("expected promotion via parent inheritance, got kind=%q", after.Kind)
	}
}
