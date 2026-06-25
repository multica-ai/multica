package lark

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubAPIClientWithRecorder is a fake APIClient that captures the
// arguments of each outbound call so the replier tests can assert
// what landed.
type stubAPIClientWithRecorder struct {
	mu             sync.Mutex
	configured     bool
	bindingCalls   []BindingPromptParams
	interactiveOut []SendCardParams
	textOut        []SendTextParams
	sendErr        error
	textErr        error
	bindingErr     error
	// threadSendErr, when non-nil, is returned by SendInteractiveCard /
	// SendTextMessage only when the call carries a thread ReplyTarget.
	// Used to exercise the shared classified fallback on the immediate
	// replies. The attempt is still recorded so tests can count the
	// thread attempt plus any chat-level fallback.
	threadSendErr error
}

func (s *stubAPIClientWithRecorder) IsConfigured() bool { return s.configured }

func (s *stubAPIClientWithRecorder) SendInteractiveCard(ctx context.Context, p SendCardParams) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interactiveOut = append(s.interactiveOut, p)
	if s.threadSendErr != nil && p.ReplyTarget.IsSet() {
		return "", s.threadSendErr
	}
	if s.sendErr != nil {
		return "", s.sendErr
	}
	return "lark-msg-id", nil
}

func (s *stubAPIClientWithRecorder) PatchInteractiveCard(ctx context.Context, p PatchCardParams) error {
	return nil
}

func (s *stubAPIClientWithRecorder) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.textOut = append(s.textOut, p)
	if s.threadSendErr != nil && p.ReplyTarget.IsSet() {
		return "", s.threadSendErr
	}
	if s.textErr != nil {
		return "", s.textErr
	}
	return "lark-text-msg-id", nil
}

func (s *stubAPIClientWithRecorder) SendMarkdownCard(ctx context.Context, p SendMarkdownCardParams) (string, error) {
	return "lark-md-msg-id", nil
}

func (s *stubAPIClientWithRecorder) SendBindingPromptCard(ctx context.Context, p BindingPromptParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bindingErr != nil {
		return s.bindingErr
	}
	s.bindingCalls = append(s.bindingCalls, p)
	return nil
}

func (s *stubAPIClientWithRecorder) GetBotInfo(ctx context.Context, creds InstallationCredentials) (BotInfo, error) {
	return BotInfo{}, nil
}

func (s *stubAPIClientWithRecorder) GetMessage(ctx context.Context, creds InstallationCredentials, messageID string) ([]LarkMessage, error) {
	return nil, nil
}
func (s *stubAPIClientWithRecorder) ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	return nil, nil
}
func (s *stubAPIClientWithRecorder) BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error) {
	return nil, nil
}
func (s *stubAPIClientWithRecorder) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	return "stub-reaction-id", nil
}
func (s *stubAPIClientWithRecorder) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	return nil
}

// stubCredentialsResolver returns a fixed plaintext secret.
type stubCredentialsResolver struct{ secret string }

func (s stubCredentialsResolver) DecryptAppSecret(inst Installation) (string, error) {
	if s.secret == "" {
		return "", errors.New("no secret configured")
	}
	return s.secret, nil
}

// stubReplierQueries returns a fixed agent.
type stubReplierQueries struct {
	agent db.Agent
	err   error
}

func (s stubReplierQueries) GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error) {
	if s.err != nil {
		return db.Agent{}, s.err
	}
	return s.agent, nil
}

type fakeBindingMinter struct {
	raw string
	err error
}

func (f fakeBindingMinter) Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, openID OpenID) (BindingToken, error) {
	if f.err != nil {
		return BindingToken{}, f.err
	}
	return BindingToken{Raw: f.raw}, nil
}

// TestLarkOutcomeReplierFallsBackToNoopWhenStubAPI ensures the
// production replier downgrades to noop when the supplied APIClient
// reports IsConfigured()=false. This avoids a misconfigured
// deployment burning binding tokens that can never be delivered.
func TestLarkOutcomeReplierFallsBackToNoopWhenStubAPI(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: false}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{}, // not nil so we exercise the IsConfigured guard
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})
	if _, isNoop := rep.(*noopReplier); !isNoop {
		t.Fatalf("expected noopReplier when APIClient.IsConfigured()=false, got %T", rep)
	}
}

// TestLarkOutcomeReplierFallsBackToNoopWhenNilDep verifies that any
// missing dependency yields a noop replier rather than a half-wired
// production one (which would panic on first use).
func TestLarkOutcomeReplierFallsBackToNoopWhenNilDep(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []OutcomeReplierConfig{
		{}, // all nil
		{APIClient: &stubAPIClientWithRecorder{configured: true}},
		{APIClient: &stubAPIClientWithRecorder{configured: true}, BindingSvc: &BindingTokenService{}},
		{APIClient: &stubAPIClientWithRecorder{configured: true}, BindingSvc: &BindingTokenService{}, Credentials: stubCredentialsResolver{secret: "s"}},
	}
	for i, cfg := range cases {
		cfg.Logger = log
		if _, isNoop := NewLarkOutcomeReplier(cfg).(*noopReplier); !isNoop {
			t.Errorf("case %d: expected noopReplier with missing dep, got production", i)
		}
	}
}

// TestLarkOutcomeReplierAgentOfflineSendsCard exercises the
// non-binding path, which doesn't require the BindingTokenService
// machinery — we can construct the production replier and assert
// SendInteractiveCard was called with the expected chat_id + body.
func TestLarkOutcomeReplierAgentOfflineSendsCard(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{agent: db.Agent{Name: "Trump"}},
		AppURL:      "https://multica.test",
		Logger:      log,
	})
	inst := Installation{AppID: "cli_x"}
	inst.ID = mustUUID("11111111-1111-1111-1111-111111111111")
	msg := InboundMessage{ChatID: "oc_chat_1", SenderOpenID: "ou_user_1"}
	rep.Reply(context.Background(), inst, msg, DispatchResult{Outcome: OutcomeAgentOffline})

	if len(stub.interactiveOut) != 1 {
		t.Fatalf("expected one SendInteractiveCard call, got %d", len(stub.interactiveOut))
	}
	got := stub.interactiveOut[0]
	if got.ChatID != "oc_chat_1" {
		t.Errorf("ChatID = %q; want oc_chat_1", got.ChatID)
	}
	if got.InstallationID.AppID != "cli_x" {
		t.Errorf("AppID = %q", got.InstallationID.AppID)
	}
	if got.InstallationID.AppSecret != "s" {
		t.Errorf("AppSecret = %q", got.InstallationID.AppSecret)
	}
	if !contains(got.CardJSON, "离线") || !contains(got.CardJSON, "Trump") {
		t.Errorf("CardJSON should embed offline copy and agent name: %s", got.CardJSON)
	}
}

func TestLarkOutcomeReplierAgentArchivedSendsCard(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})
	msg := InboundMessage{ChatID: "oc_chat_arch"}
	rep.Reply(context.Background(), Installation{}, msg, DispatchResult{Outcome: OutcomeAgentArchived})
	if len(stub.interactiveOut) != 1 {
		t.Fatalf("expected one SendInteractiveCard call, got %d", len(stub.interactiveOut))
	}
	if !contains(stub.interactiveOut[0].CardJSON, "归档") {
		t.Errorf("CardJSON should embed archived copy: %s", stub.interactiveOut[0].CardJSON)
	}
}

// TestLarkOutcomeReplierIngestedAndDroppedAreSilent asserts that the
// replier does NOT call the APIClient on outcomes owned elsewhere
// (Patcher handles Ingested; Dropped is informational only).
func TestLarkOutcomeReplierIngestedAndDroppedAreSilent(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})
	msg := InboundMessage{ChatID: "oc_x"}
	rep.Reply(context.Background(), Installation{}, msg, DispatchResult{Outcome: OutcomeIngested})
	rep.Reply(context.Background(), Installation{}, msg, DispatchResult{Outcome: OutcomeDropped, DropReason: DropReasonDuplicate})
	if len(stub.interactiveOut) != 0 || len(stub.bindingCalls) != 0 {
		t.Errorf("Ingested/Dropped should not trigger any APIClient call; got interactive=%d binding=%d",
			len(stub.interactiveOut), len(stub.bindingCalls))
	}
}

// TestLarkOutcomeReplierOfflineSwallowsAPIError verifies the
// best-effort contract: an APIClient failure must NOT panic or
// propagate — Reply has no return signal — but the test still
// observes the side effect (single attempted SendInteractiveCard).
func TestLarkOutcomeReplierOfflineSwallowsAPIError(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true, sendErr: errors.New("lark 5xx")}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})
	// Should NOT panic.
	rep.Reply(context.Background(), Installation{}, InboundMessage{ChatID: "oc"}, DispatchResult{Outcome: OutcomeAgentOffline})
}

// The legacy "install a noop replier by default" safety is now split: the
// engine Router skips reply scheduling entirely when no OutboundReplier is
// registered (boot registers one only when larkClient.IsConfigured()), and
// NewLarkOutcomeReplier still falls back to its own noop when unconfigured.

func TestLarkOutcomeReplierUsesAppURLForWebLinks(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  fakeBindingMinter{raw: "token with space"},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://app.multica.test/",
		Logger:      log,
	})

	inst := Installation{AppID: "cli_x"}
	inst.ID = mustUUID("11111111-1111-1111-1111-111111111111")
	inst.WorkspaceID = mustUUID("33333333-3333-3333-3333-333333333333")
	rep.Reply(context.Background(), inst, InboundMessage{ChatID: "oc_chat", SenderOpenID: "ou_user"},
		DispatchResult{Outcome: OutcomeNeedsBinding, SenderOpenID: "ou_user"})
	rep.Reply(context.Background(), inst, InboundMessage{ChatID: "oc_chat", SenderOpenID: "ou_user"},
		DispatchResult{
			Outcome:         OutcomeIngested,
			IssueID:         mustUUID("22222222-2222-2222-2222-222222222222"),
			IssueNumber:     42,
			IssueIdentifier: "MUL-42",
		})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.bindingCalls) != 1 {
		t.Fatalf("expected one binding prompt, got %d", len(stub.bindingCalls))
	}
	if got := stub.bindingCalls[0].BindURL; got != "https://app.multica.test/lark/bind?token=token+with+space" {
		t.Fatalf("binding URL should use AppURL; got %q", got)
	}
	if len(stub.textOut) != 1 {
		t.Fatalf("expected one issue-created text, got %d", len(stub.textOut))
	}
	if !strings.Contains(stub.textOut[0].Text, "https://app.multica.test/issues/MUL-42") {
		t.Fatalf("issue-created text should use AppURL; got %q", stub.textOut[0].Text)
	}
}

// TestLarkOutcomeReplierIssueCreatedSendsConfirmation pins the
// recovered /issue confirmation path. Before the plain-text refactor
// the design called for a "已创建 [MUL-xxx]" card; the refactor
// dropped the whole card lifecycle, which had the side effect of
// silently dropping the issue-created signal. Trump flagged it as a
// blocker on PR #3277 review. Fix: OutcomeIngested with IssueID.Valid
// triggers a plain text confirmation send via SendTextMessage,
// composing the workspace-qualified identifier with the title and a
// deep link back to Multica.
func TestLarkOutcomeReplierIssueCreatedSendsConfirmation(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	inst := Installation{AppID: "cli_x"}
	inst.ID = mustUUID("11111111-1111-1111-1111-111111111111")
	msg := InboundMessage{ChatID: "oc_chat_42", SenderOpenID: "ou_user"}
	rep.Reply(context.Background(), inst, msg, DispatchResult{
		Outcome:         OutcomeIngested,
		IssueID:         mustUUID("22222222-2222-2222-2222-222222222222"),
		IssueNumber:     42,
		IssueIdentifier: "MUL-42",
		IssueTitle:      "fix login bug",
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 1 {
		t.Fatalf("expected one SendTextMessage call, got %d", len(stub.textOut))
	}
	got := stub.textOut[0]
	if got.ChatID != "oc_chat_42" {
		t.Errorf("ChatID = %q; want oc_chat_42", got.ChatID)
	}
	if !strings.Contains(got.Text, "MUL-42") {
		t.Errorf("text should embed the workspace-qualified key; got %q", got.Text)
	}
	if !strings.Contains(got.Text, "fix login bug") {
		t.Errorf("text should embed the issue title; got %q", got.Text)
	}
	if !strings.Contains(got.Text, "https://multica.test/issues/MUL-42") {
		t.Errorf("text should embed the deep link back to Multica; got %q", got.Text)
	}
	// No interactive card on this path — the confirmation must be
	// plain text, matching how chat replies render.
	if len(stub.interactiveOut) != 0 {
		t.Errorf("issue-created confirmation must not send a card; got %d cards", len(stub.interactiveOut))
	}
}

// TestLarkOutcomeReplierOutcomeIngestedSilentWithoutIssue pins the
// silent-by-default behaviour for plain chat messages. The "Created"
// text is gated on IssueID.Valid; a chat that didn't include /issue
// must NOT trigger an outbound from the OutcomeReplier (the agent's
// reply is delivered separately by the Patcher on EventChatDone).
func TestLarkOutcomeReplierOutcomeIngestedSilentWithoutIssue(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	rep.Reply(context.Background(), Installation{}, InboundMessage{ChatID: "oc"},
		DispatchResult{Outcome: OutcomeIngested}) // no IssueID

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 0 || len(stub.interactiveOut) != 0 {
		t.Errorf("plain chat ingest must be silent at the replier; got text=%d cards=%d",
			len(stub.textOut), len(stub.interactiveOut))
	}
}

// threadedInboundMsg builds an inbound message that originated inside a
// Lark topic, so the replier targets the thread (and can fall back).
func threadedInboundMsg(chatID ChatID) InboundMessage {
	return InboundMessage{ChatID: chatID, MessageID: "om_trigger", ThreadID: "omt_topic", SenderOpenID: "ou_user"}
}

// TestLarkOutcomeReplierIssueCreatedThreadFallback verifies the /issue
// confirmation reuses the Patcher's classified fallback: a thread reply
// rejected with a "topic cannot receive this" Lark error retries once at
// the chat level so the confirmation is not lost.
func TestLarkOutcomeReplierIssueCreatedThreadFallback(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true, threadSendErr: errThreadReplyClassified}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	rep.Reply(context.Background(), Installation{AppID: "cli_x"}, threadedInboundMsg("oc_chat_42"), DispatchResult{
		Outcome:         OutcomeIngested,
		IssueID:         mustUUID("22222222-2222-2222-2222-222222222222"),
		IssueNumber:     42,
		IssueIdentifier: "MUL-42",
		IssueTitle:      "fix login bug",
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 2 {
		t.Fatalf("expected thread attempt + chat-level fallback (2 sends); got %d", len(stub.textOut))
	}
	if !stub.textOut[0].ReplyTarget.IsSet() {
		t.Errorf("first attempt should be the thread reply; got %+v", stub.textOut[0].ReplyTarget)
	}
	if stub.textOut[1].ReplyTarget.IsSet() {
		t.Errorf("fallback attempt must be chat-level; got %+v", stub.textOut[1].ReplyTarget)
	}
}

// TestLarkOutcomeReplierIssueCreatedNoFallbackOnAmbiguous verifies the
// /issue confirmation does NOT retry at chat level on an ambiguous
// transport failure, so the confirmation cannot be duplicated.
func TestLarkOutcomeReplierIssueCreatedNoFallbackOnAmbiguous(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true, threadSendErr: errThreadReplyTransport}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	rep.Reply(context.Background(), Installation{AppID: "cli_x"}, threadedInboundMsg("oc_chat_42"), DispatchResult{
		Outcome:         OutcomeIngested,
		IssueID:         mustUUID("22222222-2222-2222-2222-222222222222"),
		IssueNumber:     42,
		IssueIdentifier: "MUL-42",
	})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.textOut) != 1 {
		t.Fatalf("expected a single thread attempt with no fallback; got %d sends", len(stub.textOut))
	}
	if !stub.textOut[0].ReplyTarget.IsSet() {
		t.Errorf("the single attempt should be the thread reply; got %+v", stub.textOut[0].ReplyTarget)
	}
}

// TestLarkOutcomeReplierNoticeThreadFallback verifies the offline /
// archived notice card shares the same classified fallback.
func TestLarkOutcomeReplierNoticeThreadFallback(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true, threadSendErr: errThreadReplyClassified}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{agent: db.Agent{Name: "Trump"}},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	rep.Reply(context.Background(), Installation{AppID: "cli_x"}, threadedInboundMsg("oc_chat_1"), DispatchResult{Outcome: OutcomeAgentOffline})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.interactiveOut) != 2 {
		t.Fatalf("expected thread attempt + chat-level fallback (2 cards); got %d", len(stub.interactiveOut))
	}
	if !stub.interactiveOut[0].ReplyTarget.IsSet() {
		t.Errorf("first attempt should be the thread reply; got %+v", stub.interactiveOut[0].ReplyTarget)
	}
	if stub.interactiveOut[1].ReplyTarget.IsSet() {
		t.Errorf("fallback attempt must be chat-level; got %+v", stub.interactiveOut[1].ReplyTarget)
	}
}

// TestLarkOutcomeReplierNoticeNoFallbackOnAmbiguous verifies the notice
// card is not retried at chat level on an ambiguous transport failure.
func TestLarkOutcomeReplierNoticeNoFallbackOnAmbiguous(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stub := &stubAPIClientWithRecorder{configured: true, threadSendErr: errThreadReplyTransport}
	rep := NewLarkOutcomeReplier(OutcomeReplierConfig{
		APIClient:   stub,
		BindingSvc:  &BindingTokenService{},
		Credentials: stubCredentialsResolver{secret: "s"},
		Queries:     stubReplierQueries{},
		AppURL:      "https://multica.test",
		Logger:      log,
	})

	rep.Reply(context.Background(), Installation{}, threadedInboundMsg("oc_chat_arch"), DispatchResult{Outcome: OutcomeAgentArchived})

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.interactiveOut) != 1 {
		t.Fatalf("expected a single thread attempt with no fallback; got %d cards", len(stub.interactiveOut))
	}
	if !stub.interactiveOut[0].ReplyTarget.IsSet() {
		t.Errorf("the single attempt should be the thread reply; got %+v", stub.interactiveOut[0].ReplyTarget)
	}
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

// silence the unused import warnings for the dependencies we keep
// reaching for via reflection in future test cases.
var _ = pgx.ErrNoRows
