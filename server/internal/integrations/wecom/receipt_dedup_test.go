package wecom

// receipt_dedup_test.go — the unsupported-kind receipt is answered once per
// message, not once per delivery.
//
// WeCom redelivers a callback it did not get an ack for. The receipt for a
// message the adapter cannot read is sent from dispatchFrame, which returns
// before c.handler, so the Router's own Claim never runs for that message and
// nothing downstream deduplicates it. Unclaimed, a redelivered photo puts a
// second "sorry, I can't read that" in the chat — and each copy spends one of
// the conversation's active pushes.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// fakeDeduper implements the same two-phase contract as the real deduper
// (wecom_resolvers.go) against a map: Claim mints a token and refuses a second
// claim on a live or processed key, Mark seals it, Release frees it again.
type fakeDeduper struct {
	mu sync.Mutex
	// state maps message id -> claimed (false) / processed (true).
	state map[string]bool
	// claimErr, when set, makes Claim fail the way an unreachable database
	// would — neither a duplicate nor a token.
	claimErr error

	claims, marks, releases int
}

func newFakeDeduper() *fakeDeduper { return &fakeDeduper{state: map[string]bool{}} }

func (f *fakeDeduper) Claim(_ context.Context, _ pgtype.UUID, messageID string) (pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	if f.claimErr != nil {
		return pgtype.UUID{}, f.claimErr
	}
	if _, held := f.state[messageID]; held {
		return pgtype.UUID{}, engine.ErrDuplicate
	}
	f.state[messageID] = false
	return pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, nil
}

func (f *fakeDeduper) Mark(_ context.Context, _ pgtype.UUID, messageID string, _ pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marks++
	f.state[messageID] = true
	return nil
}

func (f *fakeDeduper) Release(_ context.Context, _ pgtype.UUID, messageID string, _ pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	delete(f.state, messageID)
	return nil
}

func (f *fakeDeduper) counts() (claims, marks, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims, f.marks, f.releases
}

// sendFrames counts the outbound message sends on the double, ignoring
// anything else the connection carries.
func sendFrames(t *testing.T, conn *recordingConn) int {
	t.Helper()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	n := 0
	for _, f := range conn.frames {
		if f.Cmd == cmdSendMsg {
			n++
		}
	}
	return n
}

// dedupChannel assigns c.dedup straight onto the struct, which skips both
// newWecomFactory and router.go. That is fine for driving the state machine
// below, but it is why this file cannot tell whether production wires the thing
// at all — TestRegisterWecomCarriesTheDeduperIntoTheChannel goes through the
// factory, and cmd/server has the one that reads the real boot path.
func dedupChannel(t *testing.T, d engine.Deduper) *wecomChannel {
	t.Helper()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return nil })
	c.installationID = mustTestUUID(t)
	c.dedup = d
	return c
}

// The factory is what router.go actually calls, and it is where a ChannelDeps
// field goes missing without anything failing. Driving it here means the copy
// from deps into the channel cannot silently stop happening.
func TestRegisterWecomCarriesTheDeduperIntoTheChannel(t *testing.T) {
	t.Parallel()
	dedup := newFakeDeduper()
	factory := newWecomFactory(ChannelDeps{Credentials: fixedCredentials{}, Dedup: dedup})

	ch, err := factory(channel.Config{
		ID:      mustTestUUID(t),
		Raw:     []byte(`{"bot_id":"bot-1"}`),
		Handler: func(context.Context, channel.InboundMessage) error { return nil },
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	wc, ok := ch.(*wecomChannel)
	if !ok {
		t.Fatalf("factory returned %T, want *wecomChannel", ch)
	}
	if wc.dedup == nil {
		t.Fatal("the factory dropped ChannelDeps.Dedup, so every channel it builds answers a " +
			"redelivered unreadable message again on each delivery")
	}
}

// The user sends one location card. WeCom delivers the callback twice.
func TestUnsupportedReceipt_ARedeliveryDoesNotRepeatTheReceipt(t *testing.T) {
	t.Parallel()
	dedup := newFakeDeduper()
	c := dedupChannel(t, dedup)
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))

	// Same frame, same msg_id: one message, delivered twice.
	for i := 0; i < 2; i++ {
		if err := c.dispatchFrame(context.Background(),
			msgCallbackFrame(t, "location", ""), sender, slog.Default()); err != nil {
			t.Fatalf("dispatchFrame delivery %d: %v", i+1, err)
		}
	}

	if n := sendFrames(t, conn); n != 1 {
		t.Errorf("the user was told %d times that the message could not be read, want once — WeCom "+
			"redelivers a callback, and each repeat also spends one of the conversation's pushes", n)
	}
	if _, marks, releases := dedup.counts(); marks != 1 || releases != 0 {
		t.Errorf("marks=%d releases=%d, want the delivered receipt marked processed and nothing released",
			marks, releases)
	}
}

// The send fails. The claim must not survive it: the redelivery is the only
// remaining chance this message is ever answered.
func TestUnsupportedReceipt_AFailedSendReleasesTheClaim(t *testing.T) {
	t.Parallel()
	dedup := newFakeDeduper()
	c := dedupChannel(t, dedup)
	conn := &recordingConn{refuseCode: 40001, refuseMsg: "refused"}
	sender := conn.autoAck(newWSSender(conn, nil))

	if err := c.dispatchFrame(context.Background(),
		msgCallbackFrame(t, "location", ""), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if _, _, releases := dedup.counts(); releases != 1 {
		t.Fatalf("releases=%d after a refused send, want 1 — a claim held over a receipt that never "+
			"arrived turns one failed send into permanent silence", releases)
	}

	// The server stops refusing and WeCom redelivers.
	conn.mu.Lock()
	conn.refuseCode, conn.refuseMsg = 0, ""
	conn.mu.Unlock()
	if err := c.dispatchFrame(context.Background(),
		msgCallbackFrame(t, "location", ""), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame on the redelivery: %v", err)
	}
	if n := sendFrames(t, conn); n != 2 {
		t.Errorf("send frames = %d, want 2 (the refused one and the redelivery that succeeded) — the "+
			"user must still get an answer after a transient failure", n)
	}
	if _, marks, _ := dedup.counts(); marks != 1 {
		t.Errorf("marks=%d, want the successful redelivery marked exactly once", marks)
	}
}

// The dedup itself fails — the database is unreachable. Silence reads as a
// broken bot, so the receipt still goes out: the user's worst case stays the
// behaviour they had before the claim existed.
func TestUnsupportedReceipt_AnUnreachableDedupStillAnswers(t *testing.T) {
	t.Parallel()
	dedup := newFakeDeduper()
	dedup.claimErr = errors.New("connection refused")
	c := dedupChannel(t, dedup)
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))

	if err := c.dispatchFrame(context.Background(),
		msgCallbackFrame(t, "location", ""), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if n := sendFrames(t, conn); n != 1 {
		t.Errorf("send frames = %d, want 1 — a dedup that cannot answer must not silence the receipt, "+
			"or an unreachable database makes the bot look broken to every user who sends a photo", n)
	}
	if _, marks, releases := dedup.counts(); marks != 0 || releases != 0 {
		t.Errorf("marks=%d releases=%d, want neither on a claim that was never granted", marks, releases)
	}
}

// A message the adapter CAN read is the Router's to claim. The adapter must
// not claim it here, or the Router's own Claim sees a duplicate and the
// message is dropped without ever reaching the agent.
func TestUnsupportedReceipt_AReadableMessageIsNotClaimedByTheAdapter(t *testing.T) {
	t.Parallel()
	dedup := newFakeDeduper()
	handled := false
	c := dedupChannel(t, dedup)
	c.handler = func(context.Context, channel.InboundMessage) error { handled = true; return nil }
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))

	if err := c.dispatchFrame(context.Background(),
		msgCallbackFrame(t, "text", "hello"), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if !handled {
		t.Fatal("a text message did not reach the handler")
	}
	if claims, _, _ := dedup.counts(); claims != 0 {
		t.Errorf("the adapter claimed %d readable message(s); the Router claims those, and a double "+
			"claim makes it see a duplicate and drop the message before the agent ever sees it", claims)
	}
}

// Without a dedup wired the receipt still goes out — the behaviour every
// caller that does not pass ChannelDeps.Dedup keeps.
func TestUnsupportedReceipt_NoDedupWiredStillAnswersOnce(t *testing.T) {
	t.Parallel()
	c := testChannel(func(context.Context, channel.InboundMessage) error { return nil })
	c.installationID = mustTestUUID(t)
	conn := &recordingConn{}
	sender := conn.autoAck(newWSSender(conn, nil))

	if err := c.dispatchFrame(context.Background(),
		msgCallbackFrame(t, "location", ""), sender, slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if n := sendFrames(t, conn); n != 1 {
		t.Errorf("send frames = %d, want 1 with no dedup configured", n)
	}
}
