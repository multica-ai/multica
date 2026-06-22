package lark

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeDedupRow is the test-side model of a lark_inbound_message_dedup
// row. It tracks the three pieces of state that drive the dispatcher's
// finalize logic: terminal flag (processed_at IS NOT NULL), the
// currently-live claim_token, and a counter of how many distinct
// claim_tokens have ever been minted for this message_id (used by
// tests to assert that a stale-reclaim actually rotated the token).
type fakeDedupRow struct {
	processed bool
	token     pgtype.UUID
	// rotations is the number of times claim_token has been minted
	// for this row (1 = inserted, 2+ = stale-reclaimed N-1 times).
	rotations int
}

// fakeQueries is the unit-test seam for DispatcherQueries. Each field
// is the canned response the fake returns from the corresponding
// method; ErrNoRows variants pin specific failure modes.
//
// Dedup state mirrors lark_inbound_message_dedup with owner fencing:
// each row carries a current claim_token, and Mark/Release require a
// matching token to succeed (zero rows otherwise, exactly like the
// production query). Tests pre-seed terminal rows by writing
// processed=true; tests exercising the in-flight branch write
// processed=false. The default empty map means "first delivery for
// every message_id".
type fakeQueries struct {
	installationByApp db.LarkInstallation
	installationErr   error
	userBinding       db.LarkUserBinding
	userBindingErr    error
	// userBindingByOpenID, when set, overrides userBinding per Lark open ID so a
	// test can simulate distinct senders in one chat (MUL-2645 latest-sender-wins).
	userBindingByOpenID map[string]db.LarkUserBinding
	chatSession         db.ChatSession
	chatSessionErr      error
	workspace           db.Workspace
	workspaceErr        error
	dedup               map[string]*fakeDedupRow
	dedupClaimErr       error
	dedupReclaim        bool // when true, in-flight rows are re-claimable (simulates staleness)
	nextTokenByte       byte // monotonically incremented; ensures each minted token is distinct
	calledUserBinding   int
	calledChatSession   int
	calledInstallation  int
	calledClaim         int
	calledMark          int
	calledRelease       int
}

// mintToken produces a deterministic, distinct token per call so
// tests can compare them. Production uses gen_random_uuid(); the
// fake only needs uniqueness, not randomness.
func (f *fakeQueries) mintToken() pgtype.UUID {
	f.nextTokenByte++
	return validUUID(0xA0 + f.nextTokenByte)
}

func (f *fakeQueries) GetLarkInstallationByAppID(ctx context.Context, appID string) (db.LarkInstallation, error) {
	f.calledInstallation++
	return f.installationByApp, f.installationErr
}

func (f *fakeQueries) GetLarkUserBindingByOpenID(ctx context.Context, arg db.GetLarkUserBindingByOpenIDParams) (db.LarkUserBinding, error) {
	f.calledUserBinding++
	if f.userBindingByOpenID != nil {
		if b, ok := f.userBindingByOpenID[arg.LarkOpenID]; ok {
			return b, nil
		}
	}
	return f.userBinding, f.userBindingErr
}

func (f *fakeQueries) GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error) {
	f.calledChatSession++
	return f.chatSession, f.chatSessionErr
}

// ClaimLarkInboundDedup mirrors the production query's three outcomes:
//
//   - Row not present → INSERT succeeds, mints a fresh claim_token →
//     returns the row.
//   - Row present, processed=false, dedupReclaim=true → staleness
//     fallback re-takes the claim and ROTATES the claim_token →
//     returns the row.
//   - Row present otherwise → ON CONFLICT WHERE filter excludes the
//     UPDATE → RETURNING returns 0 rows → pgx.ErrNoRows.
func (f *fakeQueries) ClaimLarkInboundDedup(ctx context.Context, arg db.ClaimLarkInboundDedupParams) (db.LarkInboundMessageDedup, error) {
	f.calledClaim++
	if f.dedupClaimErr != nil {
		return db.LarkInboundMessageDedup{}, f.dedupClaimErr
	}
	if f.dedup == nil {
		f.dedup = map[string]*fakeDedupRow{}
	}
	key := dedupKey(arg.InstallationID, arg.MessageID)
	row, exists := f.dedup[key]
	if !exists {
		token := f.mintToken()
		f.dedup[key] = &fakeDedupRow{token: token, rotations: 1}
		return db.LarkInboundMessageDedup{
			InstallationID: arg.InstallationID,
			MessageID:      arg.MessageID,
			ClaimToken:     token,
		}, nil
	}
	if !row.processed && f.dedupReclaim {
		// In-flight claim re-taken — rotate the token. This is what
		// fences the previous worker out: their saved claim_token no
		// longer matches the row's live one, so Mark/Release becomes
		// a no-op for them and (for the in-tx Mark) the chat_message
		// tx rolls back.
		row.token = f.mintToken()
		row.rotations++
		return db.LarkInboundMessageDedup{
			InstallationID: arg.InstallationID,
			MessageID:      arg.MessageID,
			ClaimToken:     row.token,
		}, nil
	}
	return db.LarkInboundMessageDedup{}, pgx.ErrNoRows
}

// dedupKey mirrors the production (installation_id, message_id) composite
// PK in the test map. Installations are not isolated by message_id alone:
// a Lark group with multiple bots installed delivers the SAME message_id
// to every bot's WS, and each one must be free to claim, evaluate
// AddressedToBot independently, and either ingest or drop as
// not_addressed_in_group.
func dedupKey(installationID pgtype.UUID, messageID string) string {
	var b [16]byte
	if installationID.Valid {
		b = installationID.Bytes
	}
	return string(b[:]) + "|" + messageID
}

// MarkLarkInboundDedupProcessed mirrors the production UPDATE: only
// the holder of the current claim_token can mark the row, and only
// while it is still in-flight (processed_at IS NULL). Mismatched token
// or already-terminal row returns 0 rows affected (and nil error) —
// the dispatcher relies on this for the in-tx ErrClaimLost path.
func (f *fakeQueries) MarkLarkInboundDedupProcessed(ctx context.Context, arg db.MarkLarkInboundDedupProcessedParams) (int64, error) {
	f.calledMark++
	if f.dedup == nil {
		return 0, nil
	}
	row, ok := f.dedup[dedupKey(arg.InstallationID, arg.MessageID)]
	if !ok {
		return 0, nil
	}
	if row.processed {
		return 0, nil
	}
	if row.token != arg.ClaimToken {
		return 0, nil
	}
	row.processed = true
	return 1, nil
}

// ReleaseLarkInboundDedup mirrors the production DELETE: only the
// holder of the current claim_token can release the row, and only
// while it is still in-flight.
func (f *fakeQueries) GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error) {
	return f.workspace, f.workspaceErr
}

func (f *fakeQueries) ReleaseLarkInboundDedup(ctx context.Context, arg db.ReleaseLarkInboundDedupParams) (int64, error) {
	f.calledRelease++
	if f.dedup == nil {
		return 0, nil
	}
	key := dedupKey(arg.InstallationID, arg.MessageID)
	row, ok := f.dedup[key]
	if !ok {
		return 0, nil
	}
	if row.processed {
		return 0, nil
	}
	if row.token != arg.ClaimToken {
		return 0, nil
	}
	delete(f.dedup, key)
	return 1, nil
}

// fakeChat is a stub ChatSessionService that records what the
// dispatcher asked of it and returns canned outcomes.
//
// When `queries` is non-nil and the dispatcher hands AppendUserMessage
// a valid ClaimToken, the stub mirrors the production in-tx Mark: it
// invokes fakeQueries.MarkLarkInboundDedupProcessed with the supplied
// token before returning success. This is what reproduces the
// stale-reclaim race in tests — if the token has been rotated by a
// concurrent Claim, the Mark matches zero rows and AppendUserMessage
// returns ErrClaimLost, exactly like the real chatSessionService's
// rolled-back transaction.
//
// `beforeAppend` is a hook fired at the top of AppendUserMessage, used
// by the stale-reclaim regression test to inject a concurrent reclaim
// between the dispatcher's Claim and AppendUserMessage's in-tx Mark.
type fakeChat struct {
	ensureID         pgtype.UUID
	ensureErr        error
	appendResult     AppendResult
	appendErr        error
	queries          *fakeQueries                  // when set, runs the in-tx Mark
	beforeAppend     func(AppendUserMessageParams) // race-injection hook
	calledEnsure     int
	calledAppend     int
	lastAppendParams AppendUserMessageParams
	lastEnsureParams EnsureChatSessionParams
}

func (f *fakeChat) EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (pgtype.UUID, error) {
	f.calledEnsure++
	f.lastEnsureParams = p
	return f.ensureID, f.ensureErr
}

func (f *fakeChat) AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error) {
	f.calledAppend++
	f.lastAppendParams = p
	if f.beforeAppend != nil {
		f.beforeAppend(p)
	}
	if f.appendErr != nil {
		return f.appendResult, f.appendErr
	}
	res := f.appendResult
	// Mirror chatSessionService.AppendUserMessage: when the dispatcher
	// supplies a claim token, Mark in-tx; zero rows ↔ stale-reclaim
	// rotated the token under our feet, surface ErrClaimLost.
	if f.queries != nil && p.ClaimToken.Valid && p.LarkMessageID != "" {
		rows, err := f.queries.MarkLarkInboundDedupProcessed(ctx, db.MarkLarkInboundDedupProcessedParams{
			InstallationID: p.InstallationID,
			MessageID:      p.LarkMessageID,
			ClaimToken:     p.ClaimToken,
		})
		if err != nil {
			return AppendResult{}, err
		}
		if rows == 0 {
			return AppendResult{}, ErrClaimLost
		}
		res.DedupMarked = true
	}
	return res, nil
}

type fakeAudit struct {
	drops []AuditDropParams
}

func (f *fakeAudit) RecordDrop(ctx context.Context, p AuditDropParams) error {
	f.drops = append(f.drops, p)
	return nil
}

type fakeIssueCreator struct {
	called int
	params service.IssueCreateParams
	result service.IssueCreateResult
	err    error
}

func (f *fakeIssueCreator) Create(ctx context.Context, p service.IssueCreateParams, _ service.IssueCreateOpts) (service.IssueCreateResult, error) {
	f.called++
	f.params = p
	return f.result, f.err
}

type fakeEnqueuer struct {
	called           int
	task             db.AgentTaskQueue
	err              error
	lastInitiatorUID pgtype.UUID
}

func (f *fakeEnqueuer) EnqueueChatTask(ctx context.Context, _ db.ChatSession, initiatorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	f.called++
	f.lastInitiatorUID = initiatorUserID
	return f.task, f.err
}

// validUUID builds a deterministic Valid pgtype.UUID from the supplied
// byte. Useful for distinguishing IDs in assertions.
func validUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

func activeInstallation() db.LarkInstallation {
	return db.LarkInstallation{
		ID:              validUUID(0x11),
		WorkspaceID:     validUUID(0x22),
		AgentID:         validUUID(0x33),
		InstallerUserID: validUUID(0x99),
		Status:          string(InstallationActive),
	}
}

// seedDedupKey composes a fake-table key for the default activeInstallation
// fixture (installation_id = validUUID(0x11)). Pre-seeded dedup rows in
// dispatcher tests use this to satisfy the new (installation_id,
// message_id) composite PK.
func seedDedupKey(messageID string) string {
	return dedupKey(validUUID(0x11), messageID)
}

func boundUser() db.LarkUserBinding {
	return db.LarkUserBinding{
		ID:             validUUID(0x44),
		WorkspaceID:    validUUID(0x22),
		MulticaUserID:  validUUID(0x55),
		InstallationID: validUUID(0x11),
		LarkOpenID:     "ou_user_a",
	}
}

func TestDispatcher_UnknownAppDropped(t *testing.T) {
	queries := &fakeQueries{installationErr: pgx.ErrNoRows}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:     "missing",
		EventType: "im.message.receive_v1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonInvalidEvent {
		t.Fatalf("unexpected outcome: %+v", res)
	}
	if len(audit.drops) != 1 || audit.drops[0].Reason != DropReasonInvalidEvent {
		t.Fatalf("expected one invalid_event audit row, got %+v", audit.drops)
	}
	if audit.drops[0].InstallationID.Valid {
		t.Fatalf("audit row should omit installation_id for unknown app: %+v", audit.drops[0])
	}
}

func TestDispatcher_RevokedInstallationDropped(t *testing.T) {
	inst := activeInstallation()
	inst.Status = string(InstallationRevoked)
	queries := &fakeQueries{installationByApp: inst}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, err := d.Handle(context.Background(), InboundMessage{AppID: "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DropReason != DropReasonRevokedInstallation {
		t.Fatalf("got drop reason %q", res.DropReason)
	}
	if len(audit.drops) != 1 || audit.drops[0].Reason != DropReasonRevokedInstallation {
		t.Fatalf("audit drops: %+v", audit.drops)
	}
}

func TestDispatcher_GroupWithoutMentionDropped(t *testing.T) {
	queries := &fakeQueries{installationByApp: activeInstallation()}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: false,
	})
	if res.DropReason != DropReasonNotAddressedInGroup {
		t.Fatalf("got drop reason %q", res.DropReason)
	}
	if queries.calledUserBinding != 0 {
		t.Fatalf("identity check should be skipped before group filter, got %d calls", queries.calledUserBinding)
	}
}

func TestDispatcher_UnboundUserAsksForBinding(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBindingErr:    pgx.ErrNoRows,
	}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
	})
	if res.Outcome != OutcomeNeedsBinding {
		t.Fatalf("expected OutcomeNeedsBinding, got %q", res.Outcome)
	}
	if res.DropReason != DropReasonUnboundUser {
		t.Fatalf("expected unbound_user drop reason, got %q", res.DropReason)
	}
	if res.SenderOpenID != "ou_user_a" {
		t.Fatalf("sender propagation broken: %q", res.SenderOpenID)
	}
	if len(audit.drops) != 1 || audit.drops[0].Reason != DropReasonUnboundUser {
		t.Fatalf("expected one unbound_user audit row, got %+v", audit.drops)
	}
}

func TestDispatcher_PlainMessageEnqueuesTask(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{
		ensureID:     sessionID,
		appendResult: AppendResult{},
	}
	enq := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: enq,
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi bot",
		MessageID:    "msg-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("expected ingested, got %q", res.Outcome)
	}
	// The run trigger is debounced (MUL-2968): no TaskID is surfaced
	// synchronously anymore, and with batching disabled the flush fires
	// inline so the enqueue is observable right here.
	if res.TaskID.Valid {
		t.Fatalf("TaskID must not be set synchronously after debounce; got %+v", res.TaskID)
	}
	if enq.called != 1 {
		t.Fatalf("expected exactly one EnqueueChatTask at flush; called=%d", enq.called)
	}
	// For p2p the session creator should be the bound user, not the
	// installer — verifies the chat-type branch in Handle.
	if chat.lastEnsureParams.Sender != queries.userBinding.MulticaUserID {
		t.Fatalf("p2p session creator should be sender; got %+v", chat.lastEnsureParams.Sender)
	}
	// The task initiator is also the sender (MUL-2645).
	if enq.lastInitiatorUID != queries.userBinding.MulticaUserID {
		t.Fatalf("p2p task initiator should be sender; got %+v want %+v",
			enq.lastInitiatorUID, queries.userBinding.MulticaUserID)
	}
}

// TestDispatcher_ForwardsThreadIDToAppend verifies the dispatcher
// threads the inbound message's thread_id into AppendUserMessage, which
// is what lets the chat binding remember the topic so the outbound
// patcher can reply back into it.
func TestDispatcher_ForwardsThreadIDToAppend(t *testing.T) {
	inst := activeInstallation()
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_user_a",
		Body:           "hey in a topic",
		MessageID:      "om_in_thread",
		ThreadID:       "omt_topic_42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chat.lastAppendParams.LarkThreadID != "omt_topic_42" {
		t.Errorf("LarkThreadID forwarded to AppendUserMessage: got %q want omt_topic_42",
			chat.lastAppendParams.LarkThreadID)
	}
	if chat.lastAppendParams.LarkMessageID != "om_in_thread" {
		t.Errorf("LarkMessageID forwarded to AppendUserMessage: got %q want om_in_thread",
			chat.lastAppendParams.LarkMessageID)
	}
}

func TestDispatcher_GroupMessageUsesInstallerAsCreator(t *testing.T) {
	inst := activeInstallation()
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	_, _ = d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_user_a",
		Body:           "hey",
		MessageID:      "msg-g",
	})
	if chat.lastEnsureParams.Sender != inst.InstallerUserID {
		t.Fatalf("group session creator should be installer; got %+v want %+v",
			chat.lastEnsureParams.Sender, inst.InstallerUserID)
	}
}

// TestDispatcher_GroupMessageEnqueuesWithSenderAsInitiator is the MUL-2645
// review regression: in a Lark group chat the session creator is the installer,
// but the TASK INITIATOR must be the actual message sender. Before the fix the
// claim derived the initiator from chat_session.creator_id (= installer), so
// every group member appeared to the agent as the installer. The dispatcher now
// passes the sender's MulticaUserID to EnqueueChatTask; this asserts that the
// enqueued initiator is the sender (boundUser), NOT the installer.
func TestDispatcher_GroupMessageEnqueuesWithSenderAsInitiator(t *testing.T) {
	inst := activeInstallation()
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	enq := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: enq,
	}

	// Batching is disabled (d.batcher == nil), so the flush fires inline and the
	// enqueue is observable synchronously.
	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_user_a",
		Body:           "hey bot",
		MessageID:      "msg-g2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enq.called != 1 {
		t.Fatalf("expected exactly one EnqueueChatTask at flush; called=%d", enq.called)
	}
	// Session creator stays the installer (member-churn stability)...
	if chat.lastEnsureParams.Sender != inst.InstallerUserID {
		t.Fatalf("group session creator should be installer; got %+v", chat.lastEnsureParams.Sender)
	}
	// ...but the task initiator is the SENDER, not the installer.
	if enq.lastInitiatorUID != queries.userBinding.MulticaUserID {
		t.Fatalf("group task initiator should be the sender; got %+v want %+v",
			enq.lastInitiatorUID, queries.userBinding.MulticaUserID)
	}
	if enq.lastInitiatorUID == inst.InstallerUserID {
		t.Fatalf("group task initiator must NOT be the installer (%+v)", inst.InstallerUserID)
	}
}

func TestDispatcher_DedupHitDoesNotEnqueue(t *testing.T) {
	// Pre-seed the dedup table so the top-level dedup gate trips on
	// the first Handle call — simulates a Lark reconnect replaying an
	// event we already processed in a previous run.
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		dedup:             map[string]*fakeDedupRow{seedDedupKey("msg-dup"): {processed: true, token: validUUID(0xAB)}},
	}
	chat := &fakeChat{
		ensureID: validUUID(0x66),
	}
	enq := &fakeEnqueuer{}
	audit := &fakeAudit{}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       audit,
		TaskService: enq,
	}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "replay",
		MessageID:    "msg-dup",
	})
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonDuplicate {
		t.Fatalf("expected duplicate drop, got %+v", res)
	}
	if enq.called != 0 {
		t.Fatalf("dedup hit must not enqueue task, called=%d", enq.called)
	}
	if chat.calledEnsure != 0 || chat.calledAppend != 0 {
		t.Fatalf("dedup hit must short-circuit before chat lookup; ensure=%d append=%d",
			chat.calledEnsure, chat.calledAppend)
	}
	if queries.calledUserBinding != 0 {
		t.Fatalf("dedup hit must short-circuit before identity check, got %d binding calls",
			queries.calledUserBinding)
	}
	if len(audit.drops) != 1 || audit.drops[0].Reason != DropReasonDuplicate {
		t.Fatalf("expected duplicate audit row, got %+v", audit.drops)
	}
}

// TestDispatcher_DedupBeforeGroupFilter pins the §4.3 ordering: a
// replayed group event that was NOT addressed to the Bot must NOT
// re-write a not_addressed_in_group audit row on every reconnect, and
// must NOT re-trigger any binding-prompt side effect. The top-level
// dedup gate is what guarantees this; before this fix the group
// filter ran first and unbounded replays produced unbounded audit
// noise + reply-card spam.
func TestDispatcher_DedupBeforeGroupFilter(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		dedup:             map[string]*fakeDedupRow{seedDedupKey("msg-replay"): {processed: true, token: validUUID(0xAB)}},
	}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: false,
		MessageID:      "msg-replay",
	})
	if res.DropReason != DropReasonDuplicate {
		t.Fatalf("dedup must beat group filter; got drop reason %q", res.DropReason)
	}
	if len(audit.drops) != 1 || audit.drops[0].Reason != DropReasonDuplicate {
		t.Fatalf("expected exactly one duplicate audit row, got %+v", audit.drops)
	}
}

// TestDispatcher_DedupIsScopedPerInstallation pins MUL-2671's multi-bot
// invariant: in a Lark group with TWO Multica bots installed, the
// same Lark message_id arrives at both WS supervisors and each one
// MUST be free to claim, evaluate AddressedToBot independently, and
// either ingest or drop. Before the (installation_id, message_id)
// composite PK landed, whichever WS claimed first would mark the
// shared row terminal and the bot that was actually @-ed would lose
// to dedup before its filter ran — every @ to the "second" bot
// silently disappeared.
func TestDispatcher_DedupIsScopedPerInstallation(t *testing.T) {
	otherInst := activeInstallation()
	otherInst.ID = validUUID(0x12) // distinct from the default 0x11
	queries := &fakeQueries{
		installationByApp: otherInst,
		// Pre-seed a terminal dedup row for installation 0x11 — that's
		// the OTHER bot's already-processed claim. The current Handle
		// runs under installation 0x12 (composite-key miss → fresh
		// claim → continues processing).
		dedup: map[string]*fakeDedupRow{
			dedupKey(validUUID(0x11), "msg-shared"): {processed: true, token: validUUID(0xAB)},
		},
	}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:          "ok",
		ChatType:       ChatTypeGroup,
		AddressedToBot: false, // simulate "not @-ed FROM this bot's perspective"
		MessageID:      "msg-shared",
	})
	if res.DropReason != DropReasonNotAddressedInGroup {
		t.Fatalf("composite-key dedup miss must let group filter run, got drop reason %q", res.DropReason)
	}
	// And the new (0x12, msg-shared) row must now exist — only the
	// other installation's row was pre-seeded.
	if _, ok := queries.dedup[dedupKey(otherInst.ID, "msg-shared")]; !ok {
		t.Fatalf("expected a fresh claim row for installation 0x12; got %v", queries.dedup)
	}
}

// TestDispatcher_DedupBeforeIdentityCheck pins the same ordering for
// unbound users: a replayed event from an unbound open_id must not
// re-fire the OutcomeNeedsBinding path on every reconnect — that
// would spam the user with binding-prompt cards.
func TestDispatcher_DedupBeforeIdentityCheck(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBindingErr:    pgx.ErrNoRows, // unbound — would normally trigger OutcomeNeedsBinding
		dedup:             map[string]*fakeDedupRow{seedDedupKey("msg-replay"): {processed: true, token: validUUID(0xAB)}},
	}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	res, _ := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		MessageID:    "msg-replay",
	})
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonDuplicate {
		t.Fatalf("dedup must beat identity check; got %+v", res)
	}
	if queries.calledUserBinding != 0 {
		t.Fatalf("identity check must not run for a deduped replay, got %d calls",
			queries.calledUserBinding)
	}
}

func TestDispatcher_IssueCommandCreatesIssue(t *testing.T) {
	sessionID := validUUID(0x66)
	inst := activeInstallation()
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
		workspace:         db.Workspace{ID: inst.WorkspaceID, IssuePrefix: "MUL"},
	}
	chat := &fakeChat{
		ensureID: sessionID,
		appendResult: AppendResult{
			IssueCommand: &IssueCommand{Title: "ship it", Description: "ship the thing"},
		},
	}
	issueSvc := &fakeIssueCreator{result: service.IssueCreateResult{Issue: db.Issue{
		ID:     validUUID(0x88),
		Number: 42,
		Title:  "ship it",
	}}}
	d := &Dispatcher{
		Queries:      queries,
		Chat:         chat,
		Audit:        &fakeAudit{},
		IssueService: issueSvc,
		TaskService:  &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "/issue ship it\nship the thing",
		MessageID:    "msg-ic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issueSvc.called != 1 {
		t.Fatalf("expected IssueService.Create called once, got %d", issueSvc.called)
	}
	if issueSvc.params.Title != "ship it" || issueSvc.params.Description.String != "ship the thing" {
		t.Fatalf("wrong issue params: %+v", issueSvc.params)
	}
	if issueSvc.params.OriginType.String != originLarkChat {
		t.Fatalf("origin_type should be lark_chat, got %q", issueSvc.params.OriginType.String)
	}
	if !issueSvc.params.AssigneeType.Valid || issueSvc.params.AssigneeType.String != "agent" ||
		issueSvc.params.AssigneeID != inst.AgentID {
		t.Fatalf("assignee should default to the installation's agent: %+v", issueSvc.params)
	}
	if !res.IssueID.Valid || res.IssueNumber != 42 {
		t.Fatalf("issue id/number not propagated: %+v", res)
	}
	// IssueIdentifier and IssueTitle are how the OutcomeReplier knows
	// what to put in the "Created [MUL-42] ship it" confirmation
	// message. They MUST be populated whenever a /issue command
	// produced a row.
	if res.IssueIdentifier != "MUL-42" {
		t.Fatalf("issue identifier should reflect workspace prefix; got %q", res.IssueIdentifier)
	}
	if res.IssueTitle != "ship it" {
		t.Fatalf("issue title should be propagated; got %q", res.IssueTitle)
	}
}

// TestDispatcher_IssueIdentifierFallsBackToNumberOnWorkspaceLookupErr
// pins the degrade-gracefully behaviour: a Postgres blip on the
// workspace row should NOT silently drop the issue-created
// confirmation. We emit "#42" instead of "MUL-42" in that case so
// the user still sees that the issue was created.
func TestDispatcher_IssueIdentifierFallsBackToNumberOnWorkspaceLookupErr(t *testing.T) {
	sessionID := validUUID(0x66)
	inst := activeInstallation()
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
		workspaceErr:      errors.New("workspace lookup failed"),
	}
	chat := &fakeChat{
		ensureID: sessionID,
		appendResult: AppendResult{
			IssueCommand: &IssueCommand{Title: "fallback path", Description: ""},
		},
	}
	issueSvc := &fakeIssueCreator{result: service.IssueCreateResult{Issue: db.Issue{
		ID:     validUUID(0x88),
		Number: 7,
		Title:  "fallback path",
	}}}
	d := &Dispatcher{
		Queries:      queries,
		Chat:         chat,
		Audit:        &fakeAudit{},
		IssueService: issueSvc,
		TaskService:  &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "/issue fallback path",
		MessageID:    "msg-fallback",
	})
	if err != nil {
		t.Fatalf("workspace lookup error must NOT abort dispatch; got %v", err)
	}
	if res.IssueIdentifier != "#7" {
		t.Errorf("expected fallback identifier '#7'; got %q", res.IssueIdentifier)
	}
}

func TestDispatcher_EmptyTitleSurfacesError(t *testing.T) {
	sessionID := validUUID(0x66)
	inst := activeInstallation()
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
	}
	chat := &fakeChat{
		ensureID: sessionID,
		appendResult: AppendResult{
			IssueCommand: &IssueCommand{Title: ""},
		},
	}
	issueSvc := &fakeIssueCreator{}
	d := &Dispatcher{
		Queries:      queries,
		Chat:         chat,
		Audit:        &fakeAudit{},
		IssueService: issueSvc,
		TaskService:  &fakeEnqueuer{},
	}

	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "/issue",
		MessageID:    "msg-empty",
	})
	if !errors.Is(err, ErrEmptyIssueTitle) {
		t.Fatalf("expected ErrEmptyIssueTitle wrapped, got %v", err)
	}
	if issueSvc.called != 0 {
		t.Fatalf("IssueService.Create must not run when title is empty")
	}
}

// captureReply is a FlushReply seam: it records every offline/archived
// notice the dispatcher emits at flush time so tests can assert what the
// user-facing card would say.
type captureReply struct {
	count   int
	results []DispatchResult
}

func (c *captureReply) reply(_ context.Context, _ db.LarkInstallation, _ InboundMessage, res DispatchResult) {
	c.count++
	c.results = append(c.results, res)
}

func TestDispatcher_AgentOfflineRepliesAtFlush(t *testing.T) {
	// With the run trigger debounced (MUL-2968), the agent-offline verdict
	// is only known at flush time. Handle itself returns OutcomeIngested
	// (the message is durable + ACKed); the offline notice is delivered
	// through FlushReply. With batching disabled the flush fires inline.
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	enq := &fakeEnqueuer{err: service.ErrChatTaskAgentNoRuntime}
	cap := &captureReply{}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: enq,
		FlushReply:  cap.reply,
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-off",
	})
	if err != nil {
		t.Fatalf("offline path should not return error, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("synchronous outcome must be ingested, got %q", res.Outcome)
	}
	if enq.called != 1 {
		t.Fatalf("flush must call EnqueueChatTask exactly once; called=%d", enq.called)
	}
	if cap.count != 1 {
		t.Fatalf("expected exactly one flush reply; got %d", cap.count)
	}
	if cap.results[0].Outcome != OutcomeAgentOffline {
		t.Fatalf("expected OutcomeAgentOffline at flush, got %q", cap.results[0].Outcome)
	}
	if cap.results[0].ChatSessionID != sessionID {
		t.Fatalf("session id not propagated to flush reply: %+v", cap.results[0].ChatSessionID)
	}
}

func TestDispatcher_AgentArchivedRepliesAtFlush(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	enq := &fakeEnqueuer{err: service.ErrChatTaskAgentArchived}
	cap := &captureReply{}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: enq,
		FlushReply:  cap.reply,
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-arch",
	})
	if err != nil {
		t.Fatalf("archived path should not return error, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("synchronous outcome must be ingested, got %q", res.Outcome)
	}
	if cap.count != 1 || cap.results[0].Outcome != OutcomeAgentArchived {
		t.Fatalf("expected OutcomeAgentArchived at flush, got count=%d results=%+v", cap.count, cap.results)
	}
}

func TestDispatcher_FlushInfraFailureIsNotReplied(t *testing.T) {
	// A DB / load / create failure from EnqueueChatTask is NOT a
	// productizable state. The inbound frame was ACKed and the message is
	// already durable, so Handle returns no error (nothing for the
	// connector to retry against), the failure is logged, and NO
	// offline/archived card is sent — a bogus "offline" card would
	// silently hide the outage.
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	infraErr := errors.New("create chat task: connection refused")
	enq := &fakeEnqueuer{err: infraErr}
	cap := &captureReply{}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: enq,
		FlushReply:  cap.reply,
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-infra",
	})
	if err != nil {
		t.Fatalf("flush infra failure must not surface from Handle, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("synchronous outcome must be ingested, got %q", res.Outcome)
	}
	if enq.called != 1 {
		t.Fatalf("flush must attempt EnqueueChatTask once; called=%d", enq.called)
	}
	if cap.count != 0 {
		t.Fatalf("infra failure must not emit any offline/archived card; replies=%d", cap.count)
	}
}

func TestDispatcher_DebounceCoalescesRunTrigger(t *testing.T) {
	// Two messages in the same chat_session within the silence window must
	// produce exactly ONE EnqueueChatTask (one agent run). The run reads
	// the whole session history, so both messages are covered by it. This
	// is the core MUL-2968 behaviour: "forward a transcript, then type a
	// note" stops triggering two runs.
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	enq := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}}
	f := &fakeTimerFactory{}
	d := &Dispatcher{Queries: queries, Chat: chat, Audit: &fakeAudit{}, TaskService: enq}
	d.batcher = newTestBatcher(f)

	send := func(id string) {
		res, err := d.Handle(context.Background(), InboundMessage{
			AppID:        "ok",
			ChatType:     ChatTypeP2P,
			SenderOpenID: "ou_user_a",
			Body:         "hi",
			MessageID:    id,
		})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", id, err)
		}
		if res.Outcome != OutcomeIngested {
			t.Fatalf("expected ingested for %s, got %q", id, res.Outcome)
		}
	}

	send("m1")
	send("m2")

	// Both messages are durable + ACKed, but the run is still pending.
	if enq.called != 0 {
		t.Fatalf("run trigger must be debounced; enqueue called=%d before window closed", enq.called)
	}
	if got := d.batcher.pendingCount(); got != 1 {
		t.Fatalf("both messages share one session window; pending=%d", got)
	}

	f.fireArmed() // window closes
	if enq.called != 1 {
		t.Fatalf("a coalesced burst must enqueue exactly once; called=%d", enq.called)
	}

	// A message arriving after the window fired is a new burst → new run.
	send("m3")
	f.fireArmed()
	if enq.called != 2 {
		t.Fatalf("a message after the window must start a new run; called=%d", enq.called)
	}
}

// TestDispatcher_LatestSenderWinsAsInitiator pins the MUL-2645 batching
// decision: when several people speak in one chat within a single silence
// window, the coalesced run's initiator is the LAST sender. The debouncer keeps
// only the latest scheduled flush per session, and each flush captures its own
// message's sender, so the final enqueue carries the last sender's identity.
func TestDispatcher_LatestSenderWinsAsInitiator(t *testing.T) {
	sessionID := validUUID(0x66)
	alice := boundUser() // MulticaUserID 0x55, open id ou_alice below
	bob := boundUser()
	bob.MulticaUserID = validUUID(0xBB)
	queries := &fakeQueries{
		installationByApp:   activeInstallation(),
		chatSession:         db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
		userBindingByOpenID: map[string]db.LarkUserBinding{"ou_alice": alice, "ou_bob": bob},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	enq := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}}
	f := &fakeTimerFactory{}
	d := &Dispatcher{Queries: queries, Chat: chat, Audit: &fakeAudit{}, TaskService: enq}
	d.batcher = newTestBatcher(f)

	send := func(openID, msgID string) {
		if _, err := d.Handle(context.Background(), InboundMessage{
			AppID:        "ok",
			ChatType:     ChatTypeP2P,
			SenderOpenID: OpenID(openID),
			Body:         "hi",
			MessageID:    msgID,
		}); err != nil {
			t.Fatalf("unexpected error for %s: %v", msgID, err)
		}
	}

	send("ou_alice", "m1") // Alice first...
	send("ou_bob", "m2")   // ...then Bob, within the same window.
	f.fireArmed()          // window closes → one coalesced run

	if enq.called != 1 {
		t.Fatalf("a coalesced burst must enqueue exactly once; called=%d", enq.called)
	}
	if enq.lastInitiatorUID != bob.MulticaUserID {
		t.Fatalf("latest sender (Bob) should be the initiator; got %+v want %+v",
			enq.lastInitiatorUID, bob.MulticaUserID)
	}
}

// TestDispatcher_EnsureChatSessionFailureReleasesClaim is the
// regression for the dedup-blocker Elon flagged in PR #3277: the
// pre-fix Dispatcher inserted the dedup row before EnsureChatSession,
// so an infra error in EnsureChatSession would leave a permanent
// dedup row behind and the WS adapter's retry would be mis-classified
// as a duplicate — the user's message would be silently lost.
//
// With the two-phase claim/Release contract, the first attempt's
// claim is released, and the retry must observe a fresh first
// delivery: identity check + EnsureChatSession + AppendUserMessage
// run normally, no duplicate drop.
func TestDispatcher_EnsureChatSessionFailureReleasesClaim(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{
		ensureID:  sessionID,
		ensureErr: errors.New("ensure chat session: connection refused"),
	}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	// First attempt — infra error in EnsureChatSession.
	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-retry",
	})
	if err == nil {
		t.Fatalf("first attempt should surface ensure-chat-session error, got nil")
	}
	if queries.calledMark != 0 {
		t.Fatalf("must not mark processed when no durable side effect landed; calledMark=%d", queries.calledMark)
	}
	if queries.calledRelease != 1 {
		t.Fatalf("must release the claim on pre-durable infra error; calledRelease=%d", queries.calledRelease)
	}
	if _, present := queries.dedup[seedDedupKey("msg-retry")]; present {
		t.Fatalf("release must delete the in-flight claim row; dedup=%+v", queries.dedup)
	}

	// Retry — same message_id, ensure succeeds this time. The claim
	// was released, so the retry must be able to re-claim and run
	// the full ingest pipeline. The bug being regressed would have
	// caused a DropReasonDuplicate here.
	chat.ensureErr = nil
	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-retry",
	})
	if err != nil {
		t.Fatalf("retry should succeed, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("retry must ingest; got outcome=%q reason=%q", res.Outcome, res.DropReason)
	}
	if chat.calledAppend != 1 {
		t.Fatalf("retry must reach AppendUserMessage; calledAppend=%d", chat.calledAppend)
	}
	if queries.calledMark != 1 {
		t.Fatalf("retry must mark processed; calledMark=%d", queries.calledMark)
	}
	if row, ok := queries.dedup[seedDedupKey("msg-retry")]; !ok || !row.processed {
		t.Fatalf("retry must finalize claim as processed; dedup=%+v", queries.dedup)
	}

	// A third attempt with the same message_id (post-success replay)
	// must now be a duplicate-drop — the Mark from the retry is the
	// terminal state.
	queries.calledClaim = 0
	chat.calledAppend = 0
	res, err = d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-retry",
	})
	if err != nil {
		t.Fatalf("post-success replay should not error, got %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonDuplicate {
		t.Fatalf("post-success replay must duplicate-drop; got %+v", res)
	}
	if chat.calledAppend != 0 {
		t.Fatalf("post-success replay must not re-append; calledAppend=%d", chat.calledAppend)
	}
}

// TestDispatcher_AppendUserMessageFailureReleasesClaim is the
// regression for the second variant of the dedup blocker: an infra
// error from AppendUserMessage (e.g. tx commit failure) must also
// release the claim so a retry can re-ingest. AppendUserMessage's
// transaction is atomic — an error means rollback, no chat_message
// landed.
func TestDispatcher_AppendUserMessageFailureReleasesClaim(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{
		ensureID:  sessionID,
		appendErr: errors.New("create chat message: connection refused"),
	}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	// First attempt — append fails.
	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-append-retry",
	})
	if err == nil {
		t.Fatalf("first attempt should surface append error, got nil")
	}
	if queries.calledMark != 0 {
		t.Fatalf("must not mark processed when AppendUserMessage rolled back; calledMark=%d", queries.calledMark)
	}
	if queries.calledRelease != 1 {
		t.Fatalf("must release the claim; calledRelease=%d", queries.calledRelease)
	}

	// Retry — same message_id, append succeeds.
	chat.appendErr = nil
	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-append-retry",
	})
	if err != nil {
		t.Fatalf("retry should succeed, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("retry must ingest; got %+v", res)
	}
	if chat.calledAppend != 2 {
		t.Fatalf("expected exactly two append attempts (1 failed + 1 retry); calledAppend=%d", chat.calledAppend)
	}
}

// TestDispatcher_DurableErrorMarksClaim pins the inverse of the
// release path: when AppendUserMessage has succeeded (chat_message
// committed) but a downstream step returns an error, the dispatcher
// MUST mark the claim processed. Otherwise a replay would re-process
// the message and write a second chat_message row.
//
// The run-trigger enqueue is now debounced and cannot fail synchronously,
// so the synchronous downstream error this pins is the /issue create
// path — the remaining step that runs after the chat_message is durable
// and can still surface an error to the caller.
func TestDispatcher_DurableErrorMarksClaim(t *testing.T) {
	sessionID := validUUID(0x66)
	inst := activeInstallation()
	queries := &fakeQueries{
		installationByApp: inst,
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: inst.AgentID},
	}
	chat := &fakeChat{
		ensureID:     sessionID,
		appendResult: AppendResult{IssueCommand: &IssueCommand{Title: "boom"}},
	}
	issueErr := errors.New("create issue: connection refused")
	d := &Dispatcher{
		Queries:      queries,
		Chat:         chat,
		Audit:        &fakeAudit{},
		IssueService: &fakeIssueCreator{err: issueErr},
		TaskService:  &fakeEnqueuer{},
	}

	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "/issue boom",
		MessageID:    "msg-durable-err",
	})
	if !errors.Is(err, issueErr) {
		t.Fatalf("expected post-append durable error to propagate, got %v", err)
	}
	if queries.calledRelease != 0 {
		t.Fatalf("must not release: chat_message already committed; calledRelease=%d", queries.calledRelease)
	}
	if queries.calledMark != 1 {
		t.Fatalf("must mark processed: chat_message committed before the enqueue error; calledMark=%d", queries.calledMark)
	}
	if row, ok := queries.dedup[seedDedupKey("msg-durable-err")]; !ok || !row.processed {
		t.Fatalf("dedup row must end up processed=true; got %+v", queries.dedup)
	}
}

// TestDispatcher_DropMarksClaim pins that audit-drop branches (group
// filter, unbound user) finalize their claim as processed, so a
// reconnect replay does NOT re-write the audit row or re-fire any
// binding-prompt side effect. This is the "no audit / card spam"
// invariant from §4.3.
func TestDispatcher_DropMarksClaim(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBindingErr:    pgx.ErrNoRows,
	}
	audit := &fakeAudit{}
	d := &Dispatcher{Queries: queries, Audit: audit}

	_, _ = d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		MessageID:    "msg-unbound",
	})
	if queries.calledMark != 1 {
		t.Fatalf("unbound-user drop must mark claim processed; calledMark=%d", queries.calledMark)
	}
	if queries.calledRelease != 0 {
		t.Fatalf("unbound-user drop must not release; calledRelease=%d", queries.calledRelease)
	}
}

// TestDispatcher_EmptyMessageIDSkipsDedup pins that non-message
// events (no MessageID) bypass dedup entirely — there is no key to
// deduplicate by, and the dispatcher must not call Claim / Mark /
// Release for them.
func TestDispatcher_EmptyMessageIDSkipsDedup(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
	}
	d := &Dispatcher{Queries: queries, Audit: &fakeAudit{}}

	_, _ = d.Handle(context.Background(), InboundMessage{
		AppID:    "ok",
		ChatType: ChatTypeGroup, // group filter drops it
		// MessageID intentionally empty
	})
	if queries.calledClaim != 0 || queries.calledMark != 0 || queries.calledRelease != 0 {
		t.Fatalf("empty MessageID must skip dedup entirely; claim=%d mark=%d release=%d",
			queries.calledClaim, queries.calledMark, queries.calledRelease)
	}
}

// TestDispatcher_InFlightClaimDropsReplay covers the "another worker
// is processing" branch: a fresh in-flight claim (processed=false,
// not yet stale) must duplicate-drop a concurrent replay, NOT
// re-process. This is the protection against two replicas
// simultaneously consuming the same Lark event during a brief
// double-leased window.
func TestDispatcher_InFlightClaimDropsReplay(t *testing.T) {
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		// In-flight claim (processed=false) and reclaim disabled
		// (simulates "the row is fresh — not stale yet").
		dedup: map[string]*fakeDedupRow{seedDedupKey("msg-inflight"): {processed: false, token: validUUID(0xAB)}},
	}
	chat := &fakeChat{}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{},
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "race",
		MessageID:    "msg-inflight",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonDuplicate {
		t.Fatalf("in-flight claim must drop the replay; got %+v", res)
	}
	if chat.calledEnsure != 0 || chat.calledAppend != 0 {
		t.Fatalf("in-flight drop must short-circuit before chat lookup; ensure=%d append=%d",
			chat.calledEnsure, chat.calledAppend)
	}
}

// TestDispatcher_StaleInFlightClaimReclaimable covers the
// crash-recovery branch: an in-flight claim older than the staleness
// TTL must be re-takeable so a process crash mid-pipeline does not
// leave the message stuck forever.
func TestDispatcher_StaleInFlightClaimReclaimable(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
		dedup:             map[string]*fakeDedupRow{seedDedupKey("msg-stale"): {processed: false, token: validUUID(0xAB)}},
		dedupReclaim:      true, // simulates received_at < now() - 60s
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "after-crash retry",
		MessageID:    "msg-stale",
	})
	if err != nil {
		t.Fatalf("stale-claim retry should succeed, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("stale-claim retry must ingest; got %+v", res)
	}
	if queries.calledMark != 1 {
		t.Fatalf("stale-claim retry must mark processed; calledMark=%d", queries.calledMark)
	}
}

// TestDispatcher_StaleReclaimRaceDoesNotDoubleWrite is the regression
// for Elon's first must-fix on PR #3277 dedup: worker A claims a
// dedup row at t=0 with token T_A, runs slowly past the 60-second
// staleness TTL, and is still alive. Worker B (a replay) sees the row
// as stale-reclaimable, takes the claim, rotates the token to T_B,
// and runs the full ingest pipeline. A subsequently reaches its in-tx
// Mark with the old T_A. WITHOUT owner fencing both A and B would
// commit a chat_message for the same Lark message_id — the bug Elon
// flagged. WITH owner fencing A's Mark matches zero rows, the in-tx
// Mark returns ErrClaimLost, A's chat_message+session transaction
// rolls back, and B is the sole writer.
//
// The test reproduces this by inverting the timeline: worker A is
// Handle()'s active call, and worker B is injected by the
// `beforeAppend` hook, which rotates the row's claim_token between
// the dispatcher's ClaimLarkInboundDedup call and AppendUserMessage's
// in-tx Mark. The hook fires exactly once so the second Handle()
// continues normally.
func TestDispatcher_StaleReclaimRaceDoesNotDoubleWrite(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	chat.queries = queries // model the production in-tx Mark
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	// Inject worker B's reclaim. The hook fires while worker A's
	// AppendUserMessage is running with its original (now-stale)
	// token; ClaimLarkInboundDedup with dedupReclaim=true rotates the
	// row's claim_token to T_B. When fakeChat then attempts the in-tx
	// Mark with T_A it must match zero rows and return ErrClaimLost.
	raceFired := false
	originalToken := pgtype.UUID{}
	chat.beforeAppend = func(p AppendUserMessageParams) {
		if raceFired {
			return
		}
		raceFired = true
		originalToken = p.ClaimToken
		// Make the existing in-flight row reclaimable, then have
		// worker B re-Claim. This rotates claim_token under A's feet.
		queries.dedupReclaim = true
		if _, err := queries.ClaimLarkInboundDedup(context.Background(), db.ClaimLarkInboundDedupParams{
			InstallationID: p.InstallationID,
			MessageID:      p.LarkMessageID,
		}); err != nil {
			t.Fatalf("worker-B reclaim setup failed: %v", err)
		}
		// Switch reclaim off so the dispatcher-level retry path (the
		// second Handle below) doesn't keep rotating the token.
		queries.dedupReclaim = false
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "slow-worker A",
		MessageID:    "msg-race",
	})
	if err != nil {
		t.Fatalf("stale-reclaim race should surface as duplicate drop, not error; got %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonDuplicate {
		t.Fatalf("worker A must observe duplicate drop after losing claim; got %+v", res)
	}

	// Critical regression assertion: worker A must NOT have committed
	// a chat_message. AppendUserMessage was called (race hook fired),
	// but its in-tx Mark matched zero rows, so the tx rolled back.
	// The "chat_message committed" signal in this fake is appendResult
	// — fakeChat returning success would have made calledAppend bump
	// AND the row would have been marked under A's token; instead A
	// got ErrClaimLost. To pin "no double write" we check that the
	// row's processed_at was set by worker B's path (the rotated
	// token, T_B), not by worker A's old token (T_A).
	row, ok := queries.dedup[seedDedupKey("msg-race")]
	if !ok {
		t.Fatalf("dedup row must still exist after race; got %+v", queries.dedup)
	}
	if row.rotations < 2 {
		t.Fatalf("worker B's reclaim must have rotated the token; rotations=%d", row.rotations)
	}
	if row.token == originalToken {
		t.Fatalf("token must have rotated away from worker A's original; both=%v", originalToken)
	}

	// And the loser's audit row records duplicate (not double-ingest).
	if chat.calledAppend != 1 {
		t.Fatalf("worker A's append must have been attempted exactly once; calledAppend=%d", chat.calledAppend)
	}

	// A subsequent replay of the same message_id must still
	// duplicate-drop — the row is in the in-flight state belonging to
	// worker B's (uncompleted) run; the dispatcher cannot double-write
	// even if B's process never finishes. We simulate that by leaving
	// dedupReclaim=false so the row is treated as fresh in-flight.
	chat.beforeAppend = nil
	res2, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "third-arrival replay",
		MessageID:    "msg-race",
	})
	if err != nil {
		t.Fatalf("post-race replay should not error, got %v", err)
	}
	if res2.Outcome != OutcomeDropped || res2.DropReason != DropReasonDuplicate {
		t.Fatalf("post-race replay must duplicate-drop; got %+v", res2)
	}
}

// TestDispatcher_InTxMarkPreventsPostCommitReclaim is the regression
// for Elon's second must-fix on PR #3277 dedup: in the previous
// design, a process that committed chat_message but crashed or failed
// before MarkLarkInboundDedupProcessed left the dedup row in-flight;
// 60 seconds later a retry would treat the row as stale, re-claim it,
// and write a second chat_message. The fix moves Mark INSIDE the
// chat_message+session transaction, so the durable write and the Mark
// commit (or roll back) atomically — there is no window where
// chat_message is committed but dedup is not.
//
// This test pins the invariant by simulating a successful in-tx Mark,
// then "crashing" (Handle returns without further bookkeeping), then
// replaying the same message_id and verifying it is duplicate-dropped
// regardless of the staleness TTL setting.
func TestDispatcher_InTxMarkPreventsPostCommitReclaim(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	chat.queries = queries
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	res, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "first delivery",
		MessageID:    "msg-atomic",
	})
	if err != nil {
		t.Fatalf("first delivery should succeed, got %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("first delivery must ingest; got %+v", res)
	}

	// AppendUserMessage's in-tx Mark fired; the dispatcher's post-
	// pipeline applyFinalize saw DedupMarked=true and skipped its
	// own Mark call. Total fakeQueries.MarkLarkInboundDedupProcessed
	// calls must therefore be exactly 1 — proves the in-tx path was
	// the sole writer.
	if queries.calledMark != 1 {
		t.Fatalf("expected exactly one Mark call (in-tx only, no post-finalize duplicate); calledMark=%d",
			queries.calledMark)
	}
	row, ok := queries.dedup[seedDedupKey("msg-atomic")]
	if !ok || !row.processed {
		t.Fatalf("dedup row must be terminal after in-tx Mark; got %+v", queries.dedup)
	}

	// Now simulate the dangerous scenario the OLD design failed: a
	// retry replays the same message_id AFTER the staleness TTL would
	// have expired. With the new design, processed_at IS NOT NULL
	// shadows the staleness check, so even with dedupReclaim=true the
	// Claim cannot re-acquire the row. The retry is a duplicate-drop.
	queries.dedupReclaim = true
	chat.calledAppend = 0
	res2, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "after-crash retry",
		MessageID:    "msg-atomic",
	})
	if err != nil {
		t.Fatalf("post-mark retry should not error, got %v", err)
	}
	if res2.Outcome != OutcomeDropped || res2.DropReason != DropReasonDuplicate {
		t.Fatalf("post-mark retry must duplicate-drop; got %+v", res2)
	}
	if chat.calledAppend != 0 {
		t.Fatalf("post-mark retry must short-circuit before AppendUserMessage; calledAppend=%d",
			chat.calledAppend)
	}
	// The dedup row must remain processed=true and unrotated — the
	// Claim hit the terminal-row branch (no UPDATE), so claim_token
	// did NOT mint a new value.
	if row, ok := queries.dedup[seedDedupKey("msg-atomic")]; !ok || !row.processed {
		t.Fatalf("dedup row must stay terminal after replay; got %+v", queries.dedup)
	}
	if queries.dedup[seedDedupKey("msg-atomic")].rotations != 1 {
		t.Fatalf("claim_token must not rotate when the row is already processed; rotations=%d",
			queries.dedup[seedDedupKey("msg-atomic")].rotations)
	}
}

// TestDispatcher_InTxMarkSucceedsAndSkipsPostFinalize is a positive
// regression for the in-tx Mark path: when AppendUserMessage returns
// DedupMarked=true the dispatcher must NOT issue an additional Mark
// from applyFinalize. This is the contract that makes the new design
// safe — calling Mark twice is benign at the SQL layer (the
// processed_at IS NULL guard makes the second call a no-op), but the
// extra round-trip would defeat the "in-tx atomic finalize" goal.
func TestDispatcher_InTxMarkSucceedsAndSkipsPostFinalize(t *testing.T) {
	sessionID := validUUID(0x66)
	queries := &fakeQueries{
		installationByApp: activeInstallation(),
		userBinding:       boundUser(),
		chatSession:       db.ChatSession{ID: sessionID, AgentID: validUUID(0x33)},
	}
	chat := &fakeChat{ensureID: sessionID, appendResult: AppendResult{}}
	chat.queries = queries
	d := &Dispatcher{
		Queries:     queries,
		Chat:        chat,
		Audit:       &fakeAudit{},
		TaskService: &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x77)}},
	}

	_, err := d.Handle(context.Background(), InboundMessage{
		AppID:        "ok",
		ChatType:     ChatTypeP2P,
		SenderOpenID: "ou_user_a",
		Body:         "hi",
		MessageID:    "msg-in-tx",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queries.calledMark != 1 {
		t.Fatalf("exactly one Mark call expected (in-tx only); calledMark=%d", queries.calledMark)
	}
	// Verify the dispatcher did not also Release — applyFinalize must
	// be a no-op (finalizeNone) when AppendUserMessage marked in-tx.
	if queries.calledRelease != 0 {
		t.Fatalf("dispatcher must not release after successful in-tx Mark; calledRelease=%d",
			queries.calledRelease)
	}
}
