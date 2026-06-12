// TECH-3422
package typing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Tests in this file run against the local development database, mirroring the
// sibling cerebro/channels suite. When DATABASE_URL is unset or the DB is
// unreachable the suite is skipped so "go test ./..." stays green on machines
// without a local Postgres.

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
		fmt.Printf("Skipping cerebro/typing tests: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping cerebro/typing tests: db unreachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool

	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = 'cerebro-typing-tests'`); err != nil {
		fmt.Printf("workspace cleanup: %v\n", err)
		os.Exit(1)
	}
	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Typing Tests', 'cerebro-typing-tests', 'unit-test fixture', 'CTT')
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

func uuidString(u pgtype.UUID) string {
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// captureBus wraps an events.Bus with a SubscribeAll listener that records every
// published event, so a test can assert whether (and what) the handler broadcast.
type captureBus struct {
	bus    *events.Bus
	events []events.Event
}

func newCaptureBus() *captureBus {
	cb := &captureBus{bus: events.New()}
	cb.bus.SubscribeAll(func(e events.Event) {
		cb.events = append(cb.events, e)
	})
	return cb
}

func createTestUserAndMember(t *testing.T, name string) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, name, name+"@cerebro-typing-tests.local").Scan(&userID); err != nil {
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

// createTestChannel creates a kind='channel' issue with the given members as
// subscribers and returns the loaded issue.
func createTestChannel(t *testing.T, members ...pgtype.UUID) db.Issue {
	t.Helper()
	ctx := context.Background()
	q := db.New(testPool)

	number, err := q.IncrementIssueCounter(ctx, testWorkspaceID)
	if err != nil {
		t.Fatalf("increment issue counter: %v", err)
	}
	issue, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: testWorkspaceID,
		Title:       "typing-test-channel",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   members[0],
		Position:    0,
		Number:      number,
		Kind:        pgtype.Text{String: "channel", Valid: true},
	})
	if err != nil {
		t.Fatalf("create channel issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})

	for _, m := range members {
		if err := q.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
			IssueID: issue.ID, UserType: "member", UserID: m, Reason: "creator",
		}); err != nil {
			t.Fatalf("subscribe %v: %v", m, err)
		}
	}
	return issue
}

// doTyping issues POST /api/cerebro/channels/{id}/typing through a chi router so
// the {id} URL param is populated exactly as it would be in production.
func doTyping(t *testing.T, h *Handler, channelIDParam, callerID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/channels/"+channelIDParam+"/typing", nil)
	if callerID != "" {
		req.Header.Set("X-User-ID", callerID)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestTyping_MemberBroadcasts: a member of the channel gets 204 and exactly one
// cerebro:typing event is published to the channel's member audience.
func TestTyping_MemberBroadcasts(t *testing.T) {
	memberA := createTestUserAndMember(t, "alice-typing")
	memberB := createTestUserAndMember(t, "bob-typing")
	channel := createTestChannel(t, memberA, memberB)

	cb := newCaptureBus()
	h := NewHandler(db.New(testPool), cb.bus)

	rec := doTyping(t, h, uuidString(channel.ID), uuidString(memberA))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(cb.events) != 1 {
		t.Fatalf("expected exactly 1 published event, got %d", len(cb.events))
	}
	e := cb.events[0]
	if e.Type != EventCerebroTyping {
		t.Fatalf("expected event type %q, got %q", EventCerebroTyping, e.Type)
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T", e.Payload)
	}
	if payload["channel_id"] != uuidString(channel.ID) {
		t.Fatalf("expected channel_id %q, got %v", uuidString(channel.ID), payload["channel_id"])
	}
	if payload["user_id"] != uuidString(memberA) {
		t.Fatalf("expected user_id %q, got %v", uuidString(memberA), payload["user_id"])
	}
	// Audience must cover both members of the channel.
	if !containsAll(e.AudienceUserIDs, uuidString(memberA), uuidString(memberB)) {
		t.Fatalf("expected audience to include both members, got %v", e.AudienceUserIDs)
	}
}

// TestTyping_NonMemberForbidden: a user who is not a participant of the channel
// is rejected (403) and NO event is published.
func TestTyping_NonMemberForbidden(t *testing.T) {
	memberA := createTestUserAndMember(t, "alice-nonmember")
	memberB := createTestUserAndMember(t, "bob-nonmember")
	outsider := createTestUserAndMember(t, "carol-outsider")
	channel := createTestChannel(t, memberA, memberB)

	cb := newCaptureBus()
	h := NewHandler(db.New(testPool), cb.bus)

	rec := doTyping(t, h, uuidString(channel.ID), uuidString(outsider))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d", rec.Code)
	}
	if len(cb.events) != 0 {
		t.Fatalf("expected no published event for non-member, got %d", len(cb.events))
	}
}

// TestTyping_InvalidChannelID: a malformed channel id is rejected with 400 and
// no event is published.
func TestTyping_InvalidChannelID(t *testing.T) {
	caller := createTestUserAndMember(t, "alice-badid")

	cb := newCaptureBus()
	h := NewHandler(db.New(testPool), cb.bus)

	rec := doTyping(t, h, "not-a-uuid", uuidString(caller))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid channel id, got %d", rec.Code)
	}
	if len(cb.events) != 0 {
		t.Fatalf("expected no published event for invalid id, got %d", len(cb.events))
	}
}

func containsAll(haystack []string, needles ...string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}
