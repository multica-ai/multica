package wecom

// outbound_whitespace_test.go — a completion of whitespace is not a message,
// on either of the two paths an answer can take out of this adapter.
//
// Which one runs is decided by where the WS lease happens to sit, so the two
// have to read the same completion the same way: otherwise the replica holding
// the socket decides whether the user sees a blank bubble, and whether the
// words or the file is what the reply counter counted.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// whitespaceQueries is a turn with a bound chat, a live installation, and no
// file bound after all. No file is what makes the reply's own outcome
// observable: with the files carrying it, an empty turn is a skip.
func whitespaceQueries(t *testing.T) *fakeOutboundQueries {
	t.Helper()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		attachments:     nil,
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, testTaskID)
	return q
}

// The local path: the words go out from the replica that produced them.
//
// REVERSE VERIFICATION: restore `content != ""` and `content == ""` in
// processEvent and this reports a blank message on the socket, outbound
// _delivered = 1, and no skip. Build and vet stay silent — both spellings
// compile.
func TestOutbound_WhitespaceOnlyCompletionIsNotSentAndTheFilesCarryTheReply(t *testing.T) {
	t.Parallel()
	q := whitespaceQueries(t)
	mx := newCountingMetrics()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := newMediaConn()
	reg.set(instID, conn.newSender())
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	o := NewOutbound(q, reg, testLogger(),
		WithOutboundMetrics(mx), WithAttachments(&fakeObjectStore{key: "obj/bin", data: []byte("DATA")}))
	o.spawn = func(f func()) { f() }

	if err := o.processEvent(context.Background(), chatDoneEvent("\n")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}

	assertWhitespaceWasNotAMessage(t, conn, mx)
}

// The relayed path: the same completion, on the replica that holds the socket.
//
// REVERSE VERIFICATION: restore `f.Content != ""` and `f.Content == ""` in
// deliverRelayed and this reports a blank message on the socket, outbound
// _delivered = 1, and no skip. Build and vet stay silent — both spellings
// compile. The _delivered half of that only holds because the settlement
// callback is run below: this path hands its record to the dispatcher instead
// of counting inline.
func TestRelayedReply_WhitespaceOnlyContentIsNotSentAndTheFilesCarryTheReply(t *testing.T) {
	t.Parallel()
	q := whitespaceQueries(t)
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := newMediaConn()
	reg.set(instID, conn.newSender())
	mx := newCountingMetrics()
	o := NewOutbound(q, reg, testLogger(),
		WithOutboundMetrics(mx), WithAttachments(&fakeObjectStore{key: "obj/bin", data: []byte("DATA")}))
	o.spawn = func(f func()) { f() }

	res := o.deliverRelayed(context.Background(), relayFrame{
		Kind:           relayKindReply,
		InstallationID: util.UUIDToString(instID),
		ChatID:         "CHAT_1",
		ChatType:       chatTypeGroupInt,
		Content:        "\n",
		MessageID:      testMessageID,
		WorkspaceID:    testWorkspaceID,
		SessionID:      testSessionID,
		TaskID:         testTaskID,
		CarriesFiles:   true,
	})
	if res.outcome != outcomeDone {
		t.Fatalf("outcome = %v, want outcomeDone", res.outcome)
	}
	// A relayed delivery does not move the reply counters itself; it hands
	// back the one record for the frame and the dispatcher runs it once the
	// claim is settled. Run it here, or the outbound_delivered assertion below
	// passes because nobody counted rather than because there was nothing to
	// count — and it would go on passing with `f.Content != ""` restored.
	if res.record != nil {
		res.record()
	}

	assertWhitespaceWasNotAMessage(t, conn, mx)
}

func assertWhitespaceWasNotAMessage(t *testing.T, conn *mediaConn, mx *countingMetrics) {
	t.Helper()
	if got := markdownSends(t, conn); len(got) != 0 {
		t.Errorf("the chat received %q, want nothing — a completion of whitespace is not a "+
			"message", got)
	}
	if got := mx.get("outbound_delivered"); got != 0 {
		t.Errorf("outbound_delivered = %d, want 0 — no words reached anybody", got)
	}
	if got := mx.get("outbound_skipped:" + string(skipNothingToSay)); got != 1 {
		t.Errorf("outbound_skipped:%s = %d, want 1 — with no words, the files carry this "+
			"reply's outcome, and there turned out to be none", skipNothingToSay, got)
	}
}
