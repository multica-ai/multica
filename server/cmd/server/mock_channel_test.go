package main

// Helpers shared by the M2 channel integration tests
// (channel_m2_integration_test.go).
//
// The split between this file and channel_integration_helpers_test.go
// is by milestone: helpers added in M1 (T8) live there; helpers added
// in M2 (T18) live here. Both files are in package main and share the
// same testPool / fakeChannel infrastructure.
//
// Per the STA-46 Phase B directive: helpers used only by the M2
// integration tests (production-shaped DispatchConfig wiring and the
// in-test ChatBindingLookup / UserInfoResolver implementations) are kept
// out of channel_integration_helpers_test.go so the M1 acceptance
// helpers stay reviewable in isolation.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/gateway"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// dbChatBindingLookup — production-shaped ChatBindingLookup backed by
// the testPool. The production handler-wiring code (T47) builds the
// same shape; we re-implement it inline here so the M2 tests don't
// need to import handler internals.
// ---------------------------------------------------------------------------

type dbChatBindingLookup struct {
	pool *pgxpool.Pool
}

func newDBChatBindingLookup(pool *pgxpool.Pool) inbound.ChatBindingLookup {
	return &dbChatBindingLookup{pool: pool}
}

func (l *dbChatBindingLookup) LookupWorkspaceID(ctx context.Context, channelName, chatID string) (pgtype.UUID, error) {
	var wsID pgtype.UUID
	err := l.pool.QueryRow(ctx, `
		SELECT workspace_id FROM channel_chat_binding
		WHERE connection_id = $1 AND external_chat_id = $2
	`, channelName, chatID).Scan(&wsID)
	return wsID, err
}

func (l *dbChatBindingLookup) LookupPrimaryWorkspaceID(ctx context.Context, channelName, chatID string) (pgtype.UUID, error) {
	var wsID pgtype.UUID
	err := l.pool.QueryRow(ctx, `
		SELECT workspace_id FROM channel_chat_binding
		WHERE connection_id = $1 AND external_chat_id = $2 AND is_primary = true
	`, channelName, chatID).Scan(&wsID)
	return wsID, err
}

// ---------------------------------------------------------------------------
// dbUserInfoResolver — production-shaped UserInfoResolver backed by
// channel_user_binding + user. Identical to the wiring code that
// dispatch.go assumes in production (T47).
// ---------------------------------------------------------------------------

type dbUserInfoResolver struct {
	pool *pgxpool.Pool
}

func newDBUserInfoResolver(pool *pgxpool.Pool) inbound.UserInfoResolver {
	return &dbUserInfoResolver{pool: pool}
}

func (r *dbUserInfoResolver) Resolve(ctx context.Context, channelName, externalUserID string) (inbound.ResolvedUser, error) {
	var (
		userID pgtype.UUID
		name   string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT u.id, u.name
		FROM channel_user_binding b
		JOIN "user" u ON u.id = b.user_id
		WHERE b.connection_id = $1 AND b.external_user_id = $2
	`, channelName, externalUserID).Scan(&userID, &name)
	if err != nil {
		return inbound.ResolvedUser{}, err
	}
	return inbound.ResolvedUser{MulticaUserID: userID, DisplayName: name}, nil
}

func TestDBChannelHelpersUseConnectionID(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	suffix := time.Now().UnixNano()
	connA := fmt.Sprintf("feishu-test-a-%d", suffix)
	connB := fmt.Sprintf("feishu-test-b-%d", suffix)
	chatID := fmt.Sprintf("oc_conn_scoped_%d", suffix)
	externalUserID := fmt.Sprintf("ou_conn_scoped_%d", suffix)
	userAName := fmt.Sprintf("Connection A User %d", suffix)
	userBName := fmt.Sprintf("Connection B User %d", suffix)

	insertConnection := func(id string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_connection (id, provider, display_name, enabled, is_default)
			VALUES ($1, 'feishu', $2, TRUE, FALSE)
		`, id, id); err != nil {
			t.Fatalf("insert channel_connection %s: %v", id, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_connection WHERE id = $1`, id)
		})
	}
	insertConnection(connA)
	insertConnection(connB)

	createWorkspace := func(name string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO workspace (name, slug, description, issue_prefix)
			VALUES ($1, $2, 'connection-scoped helper test', 'CST')
			RETURNING id
		`, name, fmt.Sprintf("%s-%d", name, suffix)).Scan(&id); err != nil {
			t.Fatalf("create workspace %s: %v", name, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
		})
		return id
	}
	workspaceA := createWorkspace("conn-helper-a")
	workspaceB := createWorkspace("conn-helper-b")

	createUser := func(name string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO "user" (name, email)
			VALUES ($1, $2)
			RETURNING id
		`, name, fmt.Sprintf("%s-%d@test.local", name, suffix)).Scan(&id); err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id)
		})
		return id
	}
	userA := createUser(userAName)
	userB := createUser(userBName)

	insertChatBinding := func(connID, workspaceID, boundByUserID string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_chat_binding
				(provider, connection_id, external_chat_id, chat_type, workspace_id, is_primary, bound_by_user_id)
			VALUES ('feishu', $1, $2, 'group', $3, TRUE, $4)
		`, connID, chatID, workspaceID, boundByUserID); err != nil {
			t.Fatalf("insert chat binding %s: %v", connID, err)
		}
	}
	insertChatBinding(connA, workspaceA, userA)
	insertChatBinding(connB, workspaceB, userB)

	insertUserBinding := func(connID, userID string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_user_binding (provider, connection_id, external_user_id, user_id)
			VALUES ('feishu', $1, $2, $3)
		`, connID, externalUserID, userID); err != nil {
			t.Fatalf("insert user binding %s: %v", connID, err)
		}
	}
	insertUserBinding(connA, userA)
	insertUserBinding(connB, userB)

	lookup := &dbChatBindingLookup{pool: testPool}
	gotWorkspace, err := lookup.LookupWorkspaceID(ctx, connB, chatID)
	if err != nil {
		t.Fatalf("LookupWorkspaceID: %v", err)
	}
	if want := uuidFromString(t, workspaceB); gotWorkspace != want {
		t.Fatalf("LookupWorkspaceID workspace = %v, want %v", gotWorkspace, want)
	}
	gotPrimaryWorkspace, err := lookup.LookupPrimaryWorkspaceID(ctx, connB, chatID)
	if err != nil {
		t.Fatalf("LookupPrimaryWorkspaceID: %v", err)
	}
	if want := uuidFromString(t, workspaceB); gotPrimaryWorkspace != want {
		t.Fatalf("LookupPrimaryWorkspaceID workspace = %v, want %v", gotPrimaryWorkspace, want)
	}

	resolver := newDBUserInfoResolver(testPool)
	gotUser, err := resolver.Resolve(ctx, connB, externalUserID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := uuidFromString(t, userB); gotUser.MulticaUserID != want {
		t.Fatalf("Resolve user = %v, want %v", gotUser.MulticaUserID, want)
	}
	if gotUser.DisplayName != userBName {
		t.Fatalf("Resolve display name = %q, want %q", gotUser.DisplayName, userBName)
	}
}

// ---------------------------------------------------------------------------
// directIssueServiceFull — extends directIssueService (defined in
// channel_integration_helpers_test.go) with a working
// GetIssueByIdentifier. Several M2 tests need to look up an issue by
// its identifier (e.g. STA-N) so QueryIssue / SetStatus dispatchers
// can resolve it.
// ---------------------------------------------------------------------------

type directIssueServiceFull struct {
	*directIssueService
}

func newDirectIssueServiceFull(pool *pgxpool.Pool) facade.IssueService {
	return &directIssueServiceFull{directIssueService: &directIssueService{pool: pool}}
}

func (s *directIssueServiceFull) GetIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.Issue, error) {
	// Identifier shape is "<prefix>-<number>". Reverse it via the
	// workspace.issue_prefix join.
	var (
		id     pgtype.UUID
		title  string
		status string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.title, i.status
		FROM issue i
		JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1
		  AND (w.issue_prefix || '-' || i.number::text) = $2
	`, workspaceID, identifier).Scan(&id, &title, &status)
	if err != nil {
		return facade.Issue{}, err
	}
	return facade.Issue{
		ID:          id,
		WorkspaceID: workspaceID,
		Identifier:  identifier,
		Title:       title,
		Status:      status,
	}, nil
}

// ---------------------------------------------------------------------------
// noopCommentService — minimal facade.CommentService implementation.
// The M2 tests focus on issue-level intents; comments are not exercised
// end-to-end here, but the dispatcher requires a non-nil CommentFacade
// so we wire a no-op double.
// ---------------------------------------------------------------------------

type noopCommentService struct{}

func (noopCommentService) AddComment(_ context.Context, _ facade.AddCommentReq) (facade.Comment, error) {
	return facade.Comment{}, errors.New("noopCommentService: not implemented for M2 tests")
}

// staticIntentStep — test-time intent-recog step. Production (T9/T10)
// has rule + chat resolvers; the M2 integration tests do not exercise
// classification accuracy (that lives in TC-risk-1~3 in STA-65 and the
// resolver unit tests). They DO exercise the dispatcher's response to
// a given Intent. So this step simply attaches a pre-baked Intent to
// the event and Continues.
// ---------------------------------------------------------------------------

type staticIntentStep struct {
	intent port.InboundIntent
}

func newStaticIntentStep(intent port.InboundIntent) inbound.Step {
	return &staticIntentStep{intent: intent}
}

func (staticIntentStep) Name() string { return "intent" }

func (s *staticIntentStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	evt.Intent = s.intent
	return evt, inbound.DecisionContinue, nil
}

// ---------------------------------------------------------------------------
// fnDispatch — convenience wrapper letting a test inline a dispatch
// implementation as a closure rather than declaring a new type per
// test. Used by TC-risk-4 (concurrency) where the goal is to count
// dispatch calls under contention.
// ---------------------------------------------------------------------------

type fnDispatch struct {
	name string
	fn   func(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error)
}

func newFnDispatch(name string, fn func(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error)) inbound.Step {
	return &fnDispatch{name: name, fn: fn}
}

func (d *fnDispatch) Name() string { return d.name }

func (d *fnDispatch) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	return d.fn(ctx, evt)
}

// ---------------------------------------------------------------------------
// newM2Pipeline composes the standard M2 inbound pipeline:
//
//	dedup → static-intent → dispatch
//
// This is a thin wrapper around inbound.NewPipeline that injects a
// pre-baked Intent so every TC-int-* asserts on dispatcher behaviour
// without re-writing the rule engine.
//
// Identity resolution is delegated to the production dispatch step's
// UserResolver (which looks up channel_user_binding by the *external*
// SenderID). The M1 in-test identity-bind step is intentionally NOT
// in this pipeline because it overwrites evt.SenderID with the
// Multica user_id — that contract is M1-only and is incompatible
// with the M2 production dispatcher's resolver shape.
// ---------------------------------------------------------------------------

func newM2Pipeline(
	pool *pgxpool.Pool,
	_ *channel.Registry,
	intent port.InboundIntent,
	dispatch inbound.Step,
) *inbound.Pipeline {
	return inbound.NewPipeline(
		inbound.NewDedupStep(inbound.NewDBDedupStore(pool)),
		newStaticIntentStep(intent),
		dispatch,
	)
}

// newProductionDispatchStep builds an inbound.NewDispatchStep wired
// against the supplied issue / comment services and the testPool-
// backed lookup helpers. This is the dispatch step the production code
// instantiates in cmd/server/main.go (T47); we re-create it here so
// the M2 integration tests assert against production behaviour, not
// against the M1 in-test createIssueDispatchStep.
func newProductionDispatchStep(
	pool *pgxpool.Pool,
	registry *channel.Registry,
	issueSvc facade.IssueService,
) inbound.Step {
	gw := gateway.NewRegistryGateway(registry)
	return inbound.NewDispatchStep(inbound.DispatchConfig{
		IssueFacade:      facade.NewIssueFacade(issueSvc),
		CommentFacade:    facade.NewCommentFacade(noopCommentService{}),
		ReplySink:        inbound.NewGatewayReplySink(gw),
		ChatBinding:      newDBChatBindingLookup(pool),
		UserResolver:     newDBUserInfoResolver(pool),
		ProjectValidator: inbound.NewDBProjectWorkspaceValidator(pool),
	})
}

// uuidFromString parses a canonical UUID string (the form pgtype.UUID
// produces when scanned into a Go `string`) into pgtype.UUID. Tests
// use this when they hold the workspace_id as a string (e.g. the
// shared testWorkspaceID) but the dispatcher needs the typed form.
func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse UUID %q: %v", s, err)
	}
	return u
}
