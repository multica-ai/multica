package channel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Test scaffolding -----------------------------------------------------------
//
// The channel package's tests run against a real Postgres because the schema
// and the SQL semantics (unique constraints, partial indexes, ON CONFLICT
// behavior) are part of the contract we're verifying. Mocking sqlc would
// only verify that we call the methods we wrote.
//
// The shape mirrors the handler package's TestMain: skip cleanly if no
// database is reachable, otherwise create a workspace fixture, run, clean
// up at the end. We do NOT touch the production seed data — every test
// inserts and deletes under its own UUIDs.

var (
	testPool        *pgxpool.Pool
	testWorkspaceID pgtype.UUID
	testUserID      pgtype.UUID
	testAgentID     pgtype.UUID
	testQueries     *db.Queries
	testService     *ChannelService
	testMessageSvc  *MessageService
)

const testWorkspaceSlug = "channel-svc-tests"

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping channel service tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping channel service tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	testQueries = db.New(pool)
	testService = NewChannelService(testQueries, pool)
	testMessageSvc = NewMessageService(testQueries)

	if err := setupFixture(ctx); err != nil {
		fmt.Printf("channel test fixture setup failed: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()

	if err := teardownFixture(context.Background()); err != nil {
		fmt.Printf("channel test teardown failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupFixture(ctx context.Context) error {
	// Wipe any leftover fixture from a previous failed run before we start.
	if err := teardownFixture(ctx); err != nil {
		return err
	}
	var userID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "Channel Tests", "channel-tests@multica.local").Scan(&userID); err != nil {
		return err
	}
	testUserID = userID

	var wsID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix, channels_enabled)
		VALUES ($1, $2, $3, $4, TRUE)
		RETURNING id
	`, "Channel Tests", testWorkspaceSlug, "channels test fixture", "CHN").Scan(&wsID); err != nil {
		return err
	}
	testWorkspaceID = wsID

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, userID); err != nil {
		return err
	}

	// A test agent so we can exercise the "agent member" path without a
	// runtime. The agent's runtime_id can be NULL because we never enqueue
	// a task in this package's tests.
	var runtimeID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, $2, 'cloud', 'channel_tests', 'online', $3, '{}'::jsonb, now())
		RETURNING id
	`, wsID, "Channel Test Runtime", "channel test runtime").Scan(&runtimeID); err != nil {
		return err
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, runtime_id, name, description, runtime_mode, runtime_config)
		VALUES ($1, $2, 'channel-test-agent', 'channel test agent', 'cloud', '{}'::jsonb)
		RETURNING id
	`, wsID, runtimeID).Scan(&testAgentID); err != nil {
		return err
	}
	return nil
}

func teardownFixture(ctx context.Context) error {
	// Cleanup in dependency order. ON DELETE CASCADE handles the rest.
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testWorkspaceSlug)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = 'channel-tests@multica.local'`)
	return nil
}

// Per-test cleanup of channels created within the workspace. Tests should
// call this in their t.Cleanup(...) so a failure in one test doesn't leak
// rows that another test depends on the absence of.
func wipeChannels(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM channel WHERE workspace_id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("wipeChannels: %v", err)
	}
}

func memberActor() Actor { return Actor{Type: ActorMember, ID: testUserID} }
func agentActor() Actor  { return Actor{Type: ActorAgent, ID: testAgentID} }

// Tests ----------------------------------------------------------------------

func TestCreate_PublicChannel(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID,
		Name:        "general",
		DisplayName: "General",
		Description: "Workspace-wide chatter",
		Kind:        KindChannel,
		Visibility:  VisibilityPublic,
		CreatedBy:   memberActor(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.Name != "general" || ch.Kind != KindChannel || ch.Visibility != VisibilityPublic {
		t.Fatalf("unexpected channel: %+v", ch)
	}

	// Creator should be a member with admin role.
	mem, err := testQueries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  ch.ID,
		MemberType: ActorMember,
		MemberID:   testUserID,
	})
	if err != nil {
		t.Fatalf("creator membership not found: %v", err)
	}
	if mem.Role != RoleAdmin {
		t.Fatalf("creator role = %q, want %q", mem.Role, RoleAdmin)
	}
}

func TestCreate_RejectsInvalid(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	cases := []struct {
		name   string
		params CreateChannelParams
	}{
		{"empty name", CreateChannelParams{
			WorkspaceID: testWorkspaceID, Name: "", DisplayName: "X",
			Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		}},
		{"uppercase name", CreateChannelParams{
			WorkspaceID: testWorkspaceID, Name: "General", DisplayName: "X",
			Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		}},
		{"name with space", CreateChannelParams{
			WorkspaceID: testWorkspaceID, Name: "general chat", DisplayName: "X",
			Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		}},
		{"unknown kind", CreateChannelParams{
			WorkspaceID: testWorkspaceID, Name: "ok", DisplayName: "X",
			Kind: "broadcast", Visibility: VisibilityPublic, CreatedBy: memberActor(),
		}},
		{"public DM", CreateChannelParams{
			WorkspaceID: testWorkspaceID, Name: "dm-x", DisplayName: "X",
			Kind: KindDM, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testService.Create(ctx, tc.params)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}
}

func TestCreate_DuplicateNameConflict(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	if _, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "alpha", DisplayName: "Alpha",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "alpha", DisplayName: "Alpha 2",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestListForActor_VisibilityFiltering(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	// Public channel — visible to everyone in the workspace.
	pub, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "open", DisplayName: "Open",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	if err != nil {
		t.Fatalf("create public: %v", err)
	}
	// Private channel created by member; the agent is NOT added.
	priv, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "secret", DisplayName: "Secret",
		Kind: KindChannel, Visibility: VisibilityPrivate, CreatedBy: memberActor(),
	})
	if err != nil {
		t.Fatalf("create private: %v", err)
	}

	// Member sees both.
	got, err := testService.ListForActor(ctx, testWorkspaceID, memberActor())
	if err != nil {
		t.Fatalf("ListForActor member: %v", err)
	}
	if !containsChannel(got, pub.ID) || !containsChannel(got, priv.ID) {
		t.Fatalf("member should see both public and private channels: %v", channelNames(got))
	}

	// Agent (not a member) sees only the public one.
	got, err = testService.ListForActor(ctx, testWorkspaceID, agentActor())
	if err != nil {
		t.Fatalf("ListForActor agent: %v", err)
	}
	if !containsChannel(got, pub.ID) {
		t.Fatalf("agent should see public channel: %v", channelNames(got))
	}
	if containsChannel(got, priv.ID) {
		t.Fatalf("agent should NOT see private channel: %v", channelNames(got))
	}
}

func TestListForActor_ArchivedExcluded(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "to-archive", DisplayName: "To archive",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := testService.Archive(ctx, ch.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, err := testService.ListForActor(ctx, testWorkspaceID, memberActor())
	if err != nil {
		t.Fatalf("ListForActor: %v", err)
	}
	if containsChannel(got, ch.ID) {
		t.Fatalf("archived channel should be excluded: %v", channelNames(got))
	}
}

func TestUpdate_PartialFields(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "patchme", DisplayName: "Patchme",
		Description: "before", Kind: KindChannel, Visibility: VisibilityPublic,
		CreatedBy: memberActor(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	desc := "after"
	updated, err := testService.Update(ctx, ch.ID, UpdateChannelParams{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description != "after" {
		t.Fatalf("description = %q, want %q", updated.Description, "after")
	}
	if updated.DisplayName != "Patchme" {
		t.Fatalf("DisplayName should be unchanged, got %q", updated.DisplayName)
	}

	// Set retention, then clear it.
	val := int32(30)
	updated, err = testService.Update(ctx, ch.ID, UpdateChannelParams{
		RetentionDays: &val, RetentionDaysSet: true,
	})
	if err != nil {
		t.Fatalf("Update retention set: %v", err)
	}
	if !updated.RetentionDays.Valid || updated.RetentionDays.Int32 != 30 {
		t.Fatalf("retention not applied: %+v", updated.RetentionDays)
	}
	updated, err = testService.Update(ctx, ch.ID, UpdateChannelParams{
		RetentionDaysSet: true, // clear
	})
	if err != nil {
		t.Fatalf("Update retention clear: %v", err)
	}
	if updated.RetentionDays.Valid {
		t.Fatalf("retention not cleared: %+v", updated.RetentionDays)
	}
}

func TestAddRemoveMember(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, err := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "joiners", DisplayName: "Joiners",
		Kind: KindChannel, Visibility: VisibilityPrivate, CreatedBy: memberActor(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	added, err := testService.AddMember(ctx, ch.ID, AddMemberParams{
		Member:  agentActor(),
		Role:    RoleMember,
		AddedBy: ptrActor(memberActor()),
	})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if added.MemberType != ActorAgent {
		t.Fatalf("added member type = %q", added.MemberType)
	}

	// Idempotent: second call returns the same row.
	again, err := testService.AddMember(ctx, ch.ID, AddMemberParams{
		Member: agentActor(), Role: RoleMember,
	})
	if err != nil {
		t.Fatalf("AddMember again: %v", err)
	}
	if again.JoinedAt.Time != added.JoinedAt.Time {
		t.Fatalf("idempotent add changed JoinedAt: %v -> %v", added.JoinedAt.Time, again.JoinedAt.Time)
	}

	if err := testService.RemoveMember(ctx, ch.ID, agentActor()); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	_, err = testQueries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  ch.ID,
		MemberType: ActorAgent,
		MemberID:   testAgentID,
	})
	if err == nil {
		t.Fatalf("expected membership absent after RemoveMember")
	}
}

func TestCanActorAccess(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	pub, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "open2", DisplayName: "Open",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	priv, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "closed", DisplayName: "Closed",
		Kind: KindChannel, Visibility: VisibilityPrivate, CreatedBy: memberActor(),
	})

	// Member: yes for both (creator is in private).
	if ok, err := testService.CanActorAccess(ctx, pub.ID, testWorkspaceID, memberActor()); err != nil || !ok {
		t.Fatalf("public not accessible to member: ok=%v err=%v", ok, err)
	}
	if ok, err := testService.CanActorAccess(ctx, priv.ID, testWorkspaceID, memberActor()); err != nil || !ok {
		t.Fatalf("private not accessible to creator: ok=%v err=%v", ok, err)
	}
	// Agent: yes for public, no for private.
	if ok, err := testService.CanActorAccess(ctx, pub.ID, testWorkspaceID, agentActor()); err != nil || !ok {
		t.Fatalf("public should be accessible to agent: ok=%v err=%v", ok, err)
	}
	if ok, err := testService.CanActorAccess(ctx, priv.ID, testWorkspaceID, agentActor()); err != nil || ok {
		t.Fatalf("private should NOT be accessible to non-member agent: ok=%v err=%v", ok, err)
	}

	// Cross-workspace: not found.
	otherWS := pgtype.UUID{Bytes: [16]byte{0xde, 0xad, 0xbe, 0xef}, Valid: true}
	if _, err := testService.CanActorAccess(ctx, pub.ID, otherWS, memberActor()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for wrong workspace, got %v", err)
	}

	// Archived channel: ErrChannelClosed.
	_ = testService.Archive(ctx, pub.ID)
	if _, err := testService.CanActorAccess(ctx, pub.ID, testWorkspaceID, memberActor()); !errors.Is(err, ErrChannelClosed) {
		t.Fatalf("expected ErrChannelClosed for archived, got %v", err)
	}
}

func TestGetOrCreateDM_Idempotent(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	parts := []Actor{memberActor(), agentActor()}

	first, err := testService.GetOrCreateDM(ctx, testWorkspaceID, parts)
	if err != nil {
		t.Fatalf("GetOrCreateDM #1: %v", err)
	}
	if first.Kind != KindDM || first.Visibility != VisibilityPrivate {
		t.Fatalf("DM has wrong kind/visibility: %+v", first)
	}
	if !strings.HasPrefix(first.Name, "dm-") {
		t.Fatalf("DM name should be a hash prefixed dm-: %q", first.Name)
	}

	second, err := testService.GetOrCreateDM(ctx, testWorkspaceID, parts)
	if err != nil {
		t.Fatalf("GetOrCreateDM #2: %v", err)
	}
	if first.ID.Bytes != second.ID.Bytes {
		t.Fatalf("expected same DM row, got %v vs %v", first.ID, second.ID)
	}

	// Order-independent: reversed participant list gives the same DM.
	reversed := []Actor{agentActor(), memberActor()}
	third, err := testService.GetOrCreateDM(ctx, testWorkspaceID, reversed)
	if err != nil {
		t.Fatalf("GetOrCreateDM reversed: %v", err)
	}
	if first.ID.Bytes != third.ID.Bytes {
		t.Fatalf("reversed participants should yield same DM: %v vs %v", first.ID, third.ID)
	}

	// Both participants are members of the DM.
	mems, err := testService.ListMembers(ctx, first.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(mems) != 2 {
		t.Fatalf("expected 2 DM members, got %d", len(mems))
	}
}

func TestMarkRead(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "readtrack", DisplayName: "ReadTrack",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	msg, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "first message",
	})
	if err != nil {
		t.Fatalf("Create message: %v", err)
	}
	if err := testService.MarkRead(ctx, ch.ID, memberActor(), msg.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	mem, err := testQueries.GetChannelMembership(ctx, db.GetChannelMembershipParams{
		ChannelID:  ch.ID,
		MemberType: ActorMember,
		MemberID:   testUserID,
	})
	if err != nil {
		t.Fatalf("GetChannelMembership: %v", err)
	}
	if !mem.LastReadMessageID.Valid || mem.LastReadMessageID.Bytes != msg.ID.Bytes {
		t.Fatalf("LastReadMessageID = %+v, want %+v", mem.LastReadMessageID, msg.ID)
	}
	if !mem.LastReadAt.Valid {
		t.Fatalf("LastReadAt should be set")
	}
}

// Message tests --------------------------------------------------------------

func TestMessageList_TopLevelOnlyAndCursor(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "convo", DisplayName: "Convo",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})

	// Three top-level messages; one thread reply on the second.
	var msgs []db.ChannelMessage
	for i := 0; i < 3; i++ {
		m, err := testMessageSvc.Create(ctx, CreateMessageParams{
			ChannelID: ch.ID, Author: memberActor(), Content: fmt.Sprintf("m%d", i),
		})
		if err != nil {
			t.Fatalf("Create m%d: %v", i, err)
		}
		msgs = append(msgs, m)
		// Pin createdAt apart so the cursor is stable.
		time.Sleep(10 * time.Millisecond)
	}
	parent := msgs[1].ID
	if _, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "reply to m1",
		ParentMessageID: &parent,
	}); err != nil {
		t.Fatalf("Create reply: %v", err)
	}

	// Top-level view excludes the reply.
	top, err := testMessageSvc.List(ctx, ListMessagesParams{ChannelID: ch.ID, Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("expected 3 top-level messages, got %d (%+v)", len(top), top)
	}

	// Cursor: "before the oldest message" returns nothing.
	oldest := top[len(top)-1].CreatedAt
	older, err := testMessageSvc.List(ctx, ListMessagesParams{
		ChannelID: ch.ID, BeforeCreatedAt: &oldest, Limit: 50,
	})
	if err != nil {
		t.Fatalf("List with cursor: %v", err)
	}
	if len(older) != 0 {
		t.Fatalf("expected empty older page, got %d", len(older))
	}

	// IncludeThreaded brings the reply back.
	all, err := testMessageSvc.List(ctx, ListMessagesParams{
		ChannelID: ch.ID, Limit: 50, IncludeThreaded: true,
	})
	if err != nil {
		t.Fatalf("List threaded: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 messages incl. reply, got %d", len(all))
	}
}

func TestMessageCreate_RejectsEmptyContent(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "empty", DisplayName: "Empty",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	_, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}

func TestSoftDeleteOldMessages(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "retention", DisplayName: "Retention",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})

	old, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "ancient",
	})
	if err != nil {
		t.Fatalf("create old: %v", err)
	}
	// Backdate the message so it's a candidate for retention sweep.
	if _, err := testPool.Exec(ctx, `UPDATE channel_message SET created_at = $1 WHERE id = $2`,
		time.Now().Add(-365*24*time.Hour), old.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if _, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "fresh",
	}); err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	swept, err := testMessageSvc.SoftDeleteOldMessages(ctx, ch.ID, cutoff, 1000)
	if err != nil {
		t.Fatalf("SoftDeleteOldMessages: %v", err)
	}
	if swept != 1 {
		t.Fatalf("expected 1 swept message, got %d", swept)
	}

	// Top-level list excludes soft-deleted rows.
	visible, err := testMessageSvc.List(ctx, ListMessagesParams{ChannelID: ch.ID, Limit: 50})
	if err != nil {
		t.Fatalf("List after sweep: %v", err)
	}
	if len(visible) != 1 || visible[0].Content != "fresh" {
		t.Fatalf("expected only the fresh message visible, got %+v", visible)
	}

	// Sweep again — nothing left to remove.
	swept2, err := testMessageSvc.SoftDeleteOldMessages(ctx, ch.ID, cutoff, 1000)
	if err != nil {
		t.Fatalf("SoftDeleteOldMessages #2: %v", err)
	}
	if swept2 != 0 {
		t.Fatalf("expected 0 swept on idempotent re-run, got %d", swept2)
	}
}

func TestRunRetentionSweep_HonorsEffectiveRetention(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	// Workspace default = 30 days. Channel A inherits (null override),
	// channel B overrides to 7. Both contain one ancient message
	// (timestamp = 365 days ago) and one fresh message; we expect the
	// sweep to soft-delete the ancient ones in both, leaving the fresh.
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET channel_retention_days = 30 WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("set workspace retention: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET channel_retention_days = NULL WHERE id = $1`, testWorkspaceID)
	})

	chA, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "sweep-a", DisplayName: "A",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	override := int32(7)
	chB, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "sweep-b", DisplayName: "B",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		RetentionDays: &override,
	})

	insertMsg := func(channelID pgtype.UUID, age time.Duration) pgtype.UUID {
		t.Helper()
		m, err := testMessageSvc.Create(ctx, CreateMessageParams{
			ChannelID: channelID, Author: memberActor(), Content: "x",
		})
		if err != nil {
			t.Fatalf("create message: %v", err)
		}
		// Backdate.
		if _, err := testPool.Exec(ctx, `UPDATE channel_message SET created_at = $1 WHERE id = $2`,
			time.Now().Add(-age), m.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		return m.ID
	}
	oldA := insertMsg(chA.ID, 365*24*time.Hour)
	freshA := insertMsg(chA.ID, 1*time.Hour)
	oldB := insertMsg(chB.ID, 14*24*time.Hour) // 14d old > 7d retention → delete
	freshB := insertMsg(chB.ID, 1*time.Hour)

	// Run with a deterministic "now" — production passes time.Now().UTC().
	stats, err := testMessageSvc.RunRetentionSweep(ctx, time.Now(), 100)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if stats.ChannelsScanned != 2 {
		t.Fatalf("expected 2 channels scanned, got %d", stats.ChannelsScanned)
	}
	if stats.MessagesDeleted != 2 {
		t.Fatalf("expected 2 messages deleted, got %d", stats.MessagesDeleted)
	}

	assertDeleted := func(id pgtype.UUID, want bool) {
		t.Helper()
		var hasDeletedAt bool
		if err := testPool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM channel_message WHERE id = $1`, id).Scan(&hasDeletedAt); err != nil {
			t.Fatalf("query deleted_at: %v", err)
		}
		if hasDeletedAt != want {
			t.Fatalf("message %s: deleted=%v want=%v", uuidString(id), hasDeletedAt, want)
		}
	}
	assertDeleted(oldA, true)
	assertDeleted(freshA, false)
	assertDeleted(oldB, true)
	assertDeleted(freshB, false)
}

func TestRunRetentionSweep_RetainForeverSkips(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	// Workspace retention NULL (default), channel retention NULL —
	// this represents "retain forever". An ancient message must NOT be
	// touched by the sweep.
	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "forever", DisplayName: "Forever",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	m, err := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "ancient",
	})
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE channel_message SET created_at = now() - interval '365 days' WHERE id = $1`, m.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	stats, err := testMessageSvc.RunRetentionSweep(ctx, time.Now(), 100)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if stats.ChannelsScanned != 0 {
		t.Fatalf("expected 0 channels scanned (none have retention), got %d", stats.ChannelsScanned)
	}
	if stats.MessagesDeleted != 0 {
		t.Fatalf("expected 0 messages deleted, got %d", stats.MessagesDeleted)
	}

	// Belt-and-braces: the ancient message is still visible.
	visible, _ := testMessageSvc.List(ctx, ListMessagesParams{ChannelID: ch.ID, Limit: 50})
	if len(visible) != 1 {
		t.Fatalf("expected the ancient message to remain visible, got %d", len(visible))
	}
}

func TestRunRetentionSweep_ArchivedChannelsExcluded(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, `UPDATE workspace SET channel_retention_days = 30 WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatalf("set workspace retention: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET channel_retention_days = NULL WHERE id = $1`, testWorkspaceID)
	})

	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "abandoned", DisplayName: "Abandoned",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
	})
	m, _ := testMessageSvc.Create(ctx, CreateMessageParams{
		ChannelID: ch.ID, Author: memberActor(), Content: "ancient",
	})
	if _, err := testPool.Exec(ctx, `UPDATE channel_message SET created_at = now() - interval '365 days' WHERE id = $1`, m.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if err := testService.Archive(ctx, ch.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	stats, err := testMessageSvc.RunRetentionSweep(ctx, time.Now(), 100)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if stats.ChannelsScanned != 0 {
		t.Fatalf("expected archived channel to be skipped, got %d scanned", stats.ChannelsScanned)
	}
}

func TestRunRetentionSweep_BatchedDrain(t *testing.T) {
	t.Cleanup(func() { wipeChannels(t) })
	ctx := context.Background()

	override := int32(7)
	ch, _ := testService.Create(ctx, CreateChannelParams{
		WorkspaceID: testWorkspaceID, Name: "drain", DisplayName: "Drain",
		Kind: KindChannel, Visibility: VisibilityPublic, CreatedBy: memberActor(),
		RetentionDays: &override,
	})

	// Insert 5 ancient messages; with batchSize=2 the drain should make
	// three trips (2+2+1) and report 5 deletions.
	for i := 0; i < 5; i++ {
		m, _ := testMessageSvc.Create(ctx, CreateMessageParams{
			ChannelID: ch.ID, Author: memberActor(), Content: "x",
		})
		if _, err := testPool.Exec(ctx, `UPDATE channel_message SET created_at = now() - interval '30 days' WHERE id = $1`, m.ID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}

	stats, err := testMessageSvc.RunRetentionSweep(ctx, time.Now(), 2)
	if err != nil {
		t.Fatalf("RunRetentionSweep: %v", err)
	}
	if stats.MessagesDeleted != 5 {
		t.Fatalf("expected 5 deletions, got %d", stats.MessagesDeleted)
	}
}

func TestDMName_DeterministicAndOrderIndependent(t *testing.T) {
	a := Actor{Type: ActorMember, ID: pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}}
	b := Actor{Type: ActorAgent, ID: pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9}, Valid: true}}
	ws := pgtype.UUID{Bytes: [16]byte{42}, Valid: true}

	n1 := DMName(ws, []Actor{a, b})
	n2 := DMName(ws, []Actor{b, a})
	if n1 != n2 {
		t.Fatalf("DMName not order-independent: %q vs %q", n1, n2)
	}

	other := pgtype.UUID{Bytes: [16]byte{43}, Valid: true}
	if DMName(other, []Actor{a, b}) == n1 {
		t.Fatalf("DMName should differ across workspaces")
	}
}

// Helpers --------------------------------------------------------------------

func containsChannel(chs []db.Channel, id pgtype.UUID) bool {
	for _, c := range chs {
		if c.ID.Bytes == id.Bytes {
			return true
		}
	}
	return false
}

func channelNames(chs []db.Channel) []string {
	out := make([]string, len(chs))
	for i, c := range chs {
		out[i] = c.Name
	}
	return out
}

func ptrActor(a Actor) *Actor { return &a }
