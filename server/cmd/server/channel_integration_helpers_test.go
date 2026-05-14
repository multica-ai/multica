package main

// Helpers for cmd/server/channel_integration_test.go (T8 M1 acceptance).
//
// The split between this file and channel_integration_test.go is by
// concern: scenario assertions live in the _test.go counterpart, while
// fixture setup, the in-test fakeChannel, and the in-test IssueService
// implementation live here. Both files are in package main.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/binding"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// fakeChannel — a port.Channel implementation living entirely in this test
// package. It records outbound Send calls so the integration tests can
// assert on what the pipeline pushed back to the user.
// ---------------------------------------------------------------------------

type fakeChannel struct {
	name string

	mu        sync.Mutex
	sends     []port.OutboundMessage
	cards     []port.OutboundCardMessage
	connected bool
	out       chan port.InboundEvent
}

func newFakeChannel(name string) *fakeChannel {
	return &fakeChannel{
		name: name,
		out:  make(chan port.InboundEvent),
	}
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Connect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = true
	return nil
}

func (f *fakeChannel) Disconnect(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.connected {
		return nil
	}
	f.connected = false
	close(f.out)
	return nil
}

func (f *fakeChannel) Send(_ context.Context, msg port.OutboundMessage) (port.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, msg)
	return port.SendResult{
		PlatformMessageID: fmt.Sprintf("om_fake_%d", len(f.sends)),
	}, nil
}

func (f *fakeChannel) SendCard(_ context.Context, msg port.OutboundCardMessage) (port.SendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cards = append(f.cards, msg)
	return port.SendResult{
		PlatformMessageID: fmt.Sprintf("om_fake_card_%d", len(f.cards)),
	}, nil
}

func (f *fakeChannel) Events() <-chan port.InboundEvent { return f.out }

func (f *fakeChannel) GetChatInfo(_ context.Context, chatID string) (port.ChatInfo, error) {
	return port.ChatInfo{ID: chatID, Name: "fake-chat", Type: port.ChatTypeGroup}, nil
}

func (f *fakeChannel) GetUserInfo(_ context.Context, userID string) (port.UserInfo, error) {
	return port.UserInfo{ID: userID, Name: "fake-user"}, nil
}

func (f *fakeChannel) snapshotSends() []port.OutboundMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]port.OutboundMessage, len(f.sends))
	copy(out, f.sends)
	return out
}

// Compile-time interface conformance — drift surfaces here, not at the
// (registry, pipeline) call sites.
var _ port.Channel = (*fakeChannel)(nil)

// ---------------------------------------------------------------------------
// Test-only inbound steps. They live here because identity-bind and
// dispatch are still placeholders in the production code (DESIGN §4.4
// pinned for M1); the M2 implementations land in T11. Until then, the
// integration tests need real impls so the flow is observable end-to-end.
// ---------------------------------------------------------------------------

// identityBindStep is the test-time real implementation of identity-bind.
// On a hit (channel_user_binding row exists for (connection_id, sender_id)) it
// sets evt.SenderID to the Multica user_id and Continues. On a miss it
// issues a binding token and pushes the one-shot link via the registered
// channel, returning Skip.
type identityBindStep struct {
	pool     *pgxpool.Pool
	registry *channel.Registry
	issuer   *binding.TokenIssuer
}

func newIdentityBindStep(pool *pgxpool.Pool, registry *channel.Registry, issuer *binding.TokenIssuer) inbound.Step {
	return &identityBindStep{pool: pool, registry: registry, issuer: issuer}
}

func (s *identityBindStep) Name() string { return "identity-bind" }

func (s *identityBindStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	var multicaUserID pgtype.UUID
	err := s.pool.QueryRow(ctx, `
			SELECT user_id FROM channel_user_binding
			WHERE connection_id = $1 AND external_user_id = $2
		`, evt.ChannelName, evt.SenderID).Scan(&multicaUserID)

	switch {
	case err == nil:
		// Hit: enrich the event so downstream steps can attribute the
		// message. We stuff the multica user_id into SenderID — the
		// channel layer's "actor identity" by convention. The original
		// platform open_id is retained on RawPayload so a future audit
		// step can recover it.
		evt.SenderID = util.UUIDToString(multicaUserID)
		return evt, inbound.DecisionContinue, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Miss: issue a token and push the link via the registered
		// channel. Skip so downstream steps don't see an unbound user.
		token, issueErr := s.issuer.Issue(ctx, evt.ChannelName, evt.SenderID)
		if issueErr != nil {
			return evt, inbound.DecisionContinue, fmt.Errorf("identity-bind: issue token: %w", issueErr)
		}
		ch, getErr := s.registry.Get(evt.ChannelName)
		if getErr != nil {
			return evt, inbound.DecisionContinue, fmt.Errorf("identity-bind: get channel: %w", getErr)
		}
		body := fmt.Sprintf("点击绑定（10 分钟内有效）: https://multica.local/bind?token=%s", token.Plaintext)
		if _, sendErr := ch.Send(ctx, port.OutboundMessage{ChatID: evt.ChatID, Text: body}); sendErr != nil {
			return evt, inbound.DecisionContinue, fmt.Errorf("identity-bind: send link: %w", sendErr)
		}
		return evt, inbound.DecisionSkip, nil
	default:
		return evt, inbound.DecisionContinue, fmt.Errorf("identity-bind: lookup binding: %w", err)
	}
}

// createIssueDispatchStep is the test-time real implementation of dispatch
// for TC-int-2 / TC-int-3. It looks up the workspace via channel_chat_binding
// and either:
//
//   - If the chat has no binding (CASCADE fired or never bound): pushes the
//     "WS_NOT_BOUND" template via the channel and Skips.
//   - Otherwise: extracts the title from "create issue: <title>" prefix,
//     calls IssueFacade.CreateIssue, and pushes "STA-N created" via the
//     channel.
type createIssueDispatchStep struct {
	pool     *pgxpool.Pool
	registry *channel.Registry
	facade   facade.IssueFacade
}

func newCreateIssueDispatchStep(pool *pgxpool.Pool, registry *channel.Registry, fac facade.IssueFacade) inbound.Step {
	return &createIssueDispatchStep{pool: pool, registry: registry, facade: fac}
}

func (s *createIssueDispatchStep) Name() string { return "dispatch" }

func (s *createIssueDispatchStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	ch, err := s.registry.Get(evt.ChannelName)
	if err != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: get channel: %w", err)
	}

	// Look up the workspace this chat is bound to.
	var workspaceID pgtype.UUID
	err = s.pool.QueryRow(ctx, `
			SELECT workspace_id FROM channel_chat_binding
			WHERE connection_id = $1 AND external_chat_id = $2
		`, evt.ChannelName, evt.ChatID).Scan(&workspaceID)

	if errors.Is(err, pgx.ErrNoRows) {
		body := "WS_NOT_BOUND: 该群尚未绑定到 workspace，请群主先 /bind"
		if _, sendErr := ch.Send(ctx, port.OutboundMessage{ChatID: evt.ChatID, Text: body}); sendErr != nil {
			return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: send WS_NOT_BOUND: %w", sendErr)
		}
		return evt, inbound.DecisionSkip, nil
	}
	if err != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: lookup chat binding: %w", err)
	}

	// Trivial intent parser: any text beginning with "create issue: " is
	// treated as a CreateIssue command.
	const prefix = "create issue: "
	if !strings.HasPrefix(evt.Text, prefix) {
		// Not a recognised command; M1 falls back to a no-op reply so
		// the path is observable. (Real intent recognition lands in T9.)
		return evt, inbound.DecisionContinue, nil
	}
	title := strings.TrimSpace(strings.TrimPrefix(evt.Text, prefix))
	if title == "" {
		return evt, inbound.DecisionContinue, errors.New("dispatch: empty title")
	}

	// evt.SenderID has been overwritten by identity-bind to the Multica
	// user_id (string form). Parse it back through the existing util
	// helper — invalid input here is a programming error in the test
	// (identity-bind's contract guarantees a canonical UUID).
	actorID, parseErr := util.ParseUUID(evt.SenderID)
	if parseErr != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: parse actor id: %w", parseErr)
	}

	issue, createErr := s.facade.CreateIssue(ctx, facade.CreateIssueReq{
		WorkspaceID: workspaceID,
		ActorID:     actorID,
		Title:       title,
	})
	if createErr != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: create issue: %w", createErr)
	}

	// Render STA-N. The facade only returns ID/WS/Title/Status, so we
	// re-read the identifier from the DB via the workspace prefix +
	// number columns.
	var (
		number  int32
		prefix2 string
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT i.number, w.issue_prefix
		FROM issue i JOIN workspace w ON w.id = i.workspace_id
		WHERE i.id = $1
	`, issue.ID).Scan(&number, &prefix2); err != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: read identifier: %w", err)
	}
	identifier := fmt.Sprintf("%s-%d", prefix2, number)

	body := fmt.Sprintf("已创建 %s: %s", identifier, title)
	if _, sendErr := ch.Send(ctx, port.OutboundMessage{ChatID: evt.ChatID, Text: body}); sendErr != nil {
		return evt, inbound.DecisionContinue, fmt.Errorf("dispatch: send reply: %w", sendErr)
	}
	return evt, inbound.DecisionContinue, nil
}

// ---------------------------------------------------------------------------
// directIssueService — a facade.IssueService implementation that writes
// directly to the testPool. Used in TC-int-2; not a production type.
//
// We deliberately do NOT call the HTTP handler here: the integration test
// asserts on the channel-layer flow, not on the existing REST surface.
// Going through HTTP would also pull in JWT plumbing irrelevant to the
// thing-under-test.
// ---------------------------------------------------------------------------

type directIssueService struct {
	pool *pgxpool.Pool
}

func newDirectIssueService(pool *pgxpool.Pool) facade.IssueService {
	return &directIssueService{pool: pool}
}

func (s *directIssueService) CreateIssue(ctx context.Context, req facade.CreateIssueReq) (facade.Issue, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return facade.Issue{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Bump issue_counter and read it back atomically — same shape the
	// production handler uses.
	var number int32
	if err := tx.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(
			issue_counter,
			COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0)
		) + 1
		WHERE id = $1
		RETURNING issue_counter
	`, req.WorkspaceID).Scan(&number); err != nil {
		return facade.Issue{}, fmt.Errorf("bump counter: %w", err)
	}

	var (
		id     pgtype.UUID
		status string
	)
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, number, creator_id, creator_type)
		VALUES ($1, $2, 'todo', 'none', $3, $4, 'member')
		RETURNING id, status
	`, req.WorkspaceID, req.Title, number, req.ActorID).Scan(&id, &status); err != nil {
		return facade.Issue{}, fmt.Errorf("insert issue: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return facade.Issue{}, fmt.Errorf("commit: %w", err)
	}
	return facade.Issue{
		ID:          id,
		WorkspaceID: req.WorkspaceID,
		Title:       req.Title,
		Status:      status,
	}, nil
}

func (s *directIssueService) GetIssue(ctx context.Context, id pgtype.UUID) (facade.Issue, error) {
	var (
		out    facade.Issue
		status string
		title  string
		wsID   pgtype.UUID
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT workspace_id, title, status FROM issue WHERE id = $1
	`, id).Scan(&wsID, &title, &status); err != nil {
		return facade.Issue{}, err
	}
	out.ID = id
	out.WorkspaceID = wsID
	out.Title = title
	out.Status = status
	return out, nil
}

func (s *directIssueService) GetIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.Issue, error) {
	// Trivial — TC-int-2 does not exercise this path; impl present so
	// the IssueService interface is fully satisfied.
	return facade.Issue{}, errors.New("directIssueService.GetIssueByIdentifier: not implemented for tests")
}

func (s *directIssueService) SetIssueStatus(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, status string, _ facade.ChannelMutationContext) error {
	_, err := s.pool.Exec(ctx, `UPDATE issue SET status=$1 WHERE id=$2`, status, id)
	return err
}

func (s *directIssueService) SetIssueAssignee(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, assigneeIdentifier string, _ facade.ChannelMutationContext) error {
	var assigneeID pgtype.UUID
	clean := strings.TrimPrefix(assigneeIdentifier, "@")
	if err := s.pool.QueryRow(ctx, `
		SELECT m.user_id FROM member m
		JOIN issue i ON i.workspace_id = m.workspace_id
		LEFT JOIN "user" u ON u.id = m.user_id
		WHERE i.id = $1
		  AND (u.name = $2 OR m.user_id::text = $2)
		LIMIT 1
	`, id, clean).Scan(&assigneeID); err != nil {
		return fmt.Errorf("用户 %s 不在此 workspace", assigneeIdentifier)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'member', assignee_id = $1 WHERE id = $2
	`, assigneeID, id)
	return err
}

func (s *directIssueService) SetIssuePriority(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, priority string, _ facade.ChannelMutationContext) error {
	valid := map[string]bool{"urgent": true, "high": true, "medium": true, "low": true, "no_priority": true, "none": true}
	if !valid[priority] {
		return fmt.Errorf("优先级仅支持 urgent/high/medium/low/none")
	}
	if priority == "no_priority" {
		priority = "none"
	}
	_, err := s.pool.Exec(ctx, `UPDATE issue SET priority = $1 WHERE id = $2`, priority, id)
	return err
}

func (s *directIssueService) AddIssueLabel(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	var labelID pgtype.UUID
	var wsID pgtype.UUID
	if err := s.pool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, id).Scan(&wsID); err != nil {
		return err
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT id FROM issue_label WHERE workspace_id = $1 AND name = $2
	`, wsID, labelName).Scan(&labelID); err != nil {
		return fmt.Errorf("标签 %s 不存在，请先在 Web 端创建", labelName)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, id, labelID)
	return err
}

func (s *directIssueService) RemoveIssueLabel(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, _ facade.ChannelMutationContext) error {
	var wsID pgtype.UUID
	if err := s.pool.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, id).Scan(&wsID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM issue_to_label
		WHERE issue_id = $1
		  AND label_id = (SELECT id FROM issue_label WHERE workspace_id = $2 AND name = $3)
	`, id, wsID, labelName)
	return err
}

func (s *directIssueService) ListMyTodos(_ context.Context, _, _ pgtype.UUID) ([]facade.Issue, error) {
	return nil, nil
}

var _ facade.IssueService = (*directIssueService)(nil)

// ---------------------------------------------------------------------------
// Fixture helpers shared by the channel integration tests.
// ---------------------------------------------------------------------------

// bindChatToWorkspace inserts a channel_chat_binding row for the given
// connection/chat → workspace. Registers a t.Cleanup so the row is removed
// after the test, and tolerates the case where ON DELETE CASCADE already
// did the job (TC-int-3).
func bindChatToWorkspace(t *testing.T, provider, externalChatID string, chatType port.ChatType, workspaceID, boundByUserID string) {
	t.Helper()
	ctx := context.Background()
	dbChatType := "group"
	if chatType == port.ChatTypeDirect {
		dbChatType = "dm"
	}
	if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_chat_binding
				(provider, connection_id, external_chat_id, chat_type, workspace_id, is_primary, bound_by_user_id)
			VALUES ($1, $1, $2, $3, $4, TRUE, $5)
			ON CONFLICT (connection_id, external_chat_id) DO UPDATE
			  SET workspace_id = EXCLUDED.workspace_id,
			      chat_type = EXCLUDED.chat_type,
			      bound_by_user_id = EXCLUDED.bound_by_user_id,
			      is_primary = TRUE
	`, provider, externalChatID, dbChatType, workspaceID, boundByUserID); err != nil {
		t.Fatalf("bind chat→workspace: %v", err)
	}
	t.Cleanup(func() {
		cleanupChatBindings(context.Background(), provider, externalChatID)
	})
}

// bindUserToMulticaUser inserts a channel_user_binding row for
// (connection_id, externalUserID) → multicaUserID. Registers cleanup.
func bindUserToMulticaUser(t *testing.T, provider, externalUserID, multicaUserID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
			INSERT INTO channel_user_binding (provider, connection_id, external_user_id, user_id)
			VALUES ($1, $1, $2, $3)
			ON CONFLICT (connection_id, external_user_id) DO UPDATE
			  SET user_id = EXCLUDED.user_id, updated_at = now()
		`, provider, externalUserID, multicaUserID); err != nil {
		t.Fatalf("bind user→multica_user: %v", err)
	}
	t.Cleanup(func() {
		cleanupChannelUserBinding(context.Background(), provider, externalUserID)
	})
}

func cleanupChatBindings(ctx context.Context, provider, externalChatID string) {
	_, _ = testPool.Exec(ctx,
		`DELETE FROM channel_chat_binding WHERE connection_id=$1 AND external_chat_id=$2`,
		provider, externalChatID)
}

func cleanupChannelUserBinding(ctx context.Context, provider, externalUserID string) {
	_, _ = testPool.Exec(ctx,
		`DELETE FROM channel_user_binding WHERE connection_id=$1 AND external_user_id=$2`,
		provider, externalUserID)
	_, _ = testPool.Exec(ctx,
		`DELETE FROM channel_bind_token WHERE connection_id=$1 AND external_user_id=$2`,
		provider, externalUserID)
}

// mustCreateUser inserts a new user row and returns the generated UUID.
func mustCreateUser(t *testing.T, displayName string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, displayName, displayName+"@test.local").Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, id)
	})
	return id
}

// mustAddMember inserts a member row linking userID to workspaceID.
func mustAddMember(t *testing.T, workspaceID, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, workspaceID, userID); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

// freshEventID returns a per-run-unique event id with the supplied
// prefix. The integration tests share the channel_inbound_event_dedup
// table (a real DB table, not a per-test fixture), so reusing the same
// "evt_int1_first" string across re-runs of `go test` would have the
// dedup step short-circuit the pipeline before reaching identity-bind /
// dispatch. The time.Now().UnixNano() suffix gives us guaranteed
// uniqueness without needing a UUID dependency.
//
// Cleanup is centralised here: every freshEventID call registers a
// t.Cleanup that deletes the dedup row keyed on (connection_id, eventID).
// That keeps the dedup table tidy across test runs and matches the
// "explicit fixture cleanup" idiom the rest of the cmd/server tests
// use.
func freshEventID(t *testing.T, provider, prefix string) string {
	t.Helper()
	id := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_inbound_event_dedup WHERE connection_id=$1 AND event_id=$2`,
			provider, id)
	})
	return id
}

// newChannelTestPipeline composes the standard three-step inbound
// pipeline used by the M1 acceptance tests:
//
//	dedup  →  identity-bind  →  dispatch (caller-supplied)
//
// The first two steps are always the same — production-shaped real
// implementations of dedup and identity-bind that touch testPool. The
// dispatch step varies per test (recordingStep for the
// "did the pipeline reach dispatch?" tests, createIssueDispatchStep for
// the "did the pipeline produce a real side effect?" tests).
//
// Returning a *inbound.Pipeline (vs the steps individually) keeps every
// TC-int-* call site to one line and makes future step insertions
// (e.g. an outbound logger after dispatch in M2) a single-edit change
// here rather than a sweep across four tests.
func newChannelTestPipeline(pool *pgxpool.Pool, registry *channel.Registry, dispatch inbound.Step) *inbound.Pipeline {
	queries := db.New(pool)
	return inbound.NewPipeline(
		inbound.NewDedupStep(inbound.NewDBDedupStore(pool)),
		newIdentityBindStep(pool, registry, binding.NewTokenIssuer(queries)),
		dispatch,
	)
}
