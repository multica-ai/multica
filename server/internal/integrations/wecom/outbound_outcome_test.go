package wecom

// outbound_outcome_test.go — that every branch which ends a turn without
// putting words in front of the user now names itself.
//
// The point is not that these branches exist; most of them are correct. It is
// that they used to be indistinguishable from outside. One symptom — the answer
// is in the Multica transcript, the WeCom chat stayed quiet — with five
// different causes and no way to tell which fired, is a report that can be
// argued about but not settled.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	outcomeSession = "22222222-2222-2222-2222-222222222222"
	outcomeTask    = "33333333-3333-3333-3333-333333333333"
)

// outcomeRig is one subscriber with a live socket, its log, and its counters.
// The socket is live in every case here, so nothing a test observes can be
// blamed on connectivity.
type outcomeRig struct {
	o    *Outbound
	conn *recordingConn
	logs *bytes.Buffer
	mx   *countingMetrics
}

func newOutcomeRig(t *testing.T, q *fakeOutboundQueries, withSocket bool) *outcomeRig {
	t.Helper()
	r := &outcomeRig{logs: &bytes.Buffer{}, mx: newCountingMetrics()}
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	if withSocket {
		r.conn = &recordingConn{}
		reg.set(instID, r.conn.autoAck(newWSSender(r.conn, nil)))
	}
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	r.o = NewOutbound(q, reg,
		slog.New(slog.NewTextHandler(r.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		WithOutboundMetrics(r.mx))
	return r
}

func (r *outcomeRig) frames() int {
	if r.conn == nil {
		return 0
	}
	r.conn.mu.Lock()
	defer r.conn.mu.Unlock()
	return len(r.conn.frames)
}

// deliverableTurn is a completed turn that has every right to be delivered:
// bound session, active installation, asked in the room, words to say.
func deliverableTurn(t *testing.T) *fakeOutboundQueries {
	t.Helper()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "p2p"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, outcomeTask)
	return q
}

func outcomeEvent() events.Event {
	return events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: outcomeSession,
		Payload:       protocol.ChatDonePayload{Content: "the answer", TaskID: outcomeTask},
	}
}

func TestEveryUndeliveredReplyNamesItself(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		reason     string
		actionable bool
		socket     bool
		setup      func(t *testing.T, q *fakeOutboundQueries)
	}{
		{
			name: "no socket in this process", reason: "no_live_connection", actionable: true, socket: false,
			setup: func(*testing.T, *fakeOutboundQueries) {},
		},
		{
			name: "the platform refused the frame", reason: "platform_refused", actionable: true, socket: true,
			setup: func(*testing.T, *fakeOutboundQueries) {},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := deliverableTurn(t)
			tc.setup(t, q)
			r := newOutcomeRig(t, q, tc.socket)
			if tc.reason == "platform_refused" {
				r.conn.refuseCode, r.conn.refuseMsg = 45002, "content exceed max length"
			}

			r.o.handleEvent(outcomeEvent())

			if got := r.mx.get("outbound_dropped:" + tc.reason); got != 1 {
				t.Fatalf("counter for %s = %d, want 1. log:\n%s", tc.reason, got, r.logs.String())
			}
			out := r.logs.String()
			if !strings.Contains(out, "reason="+tc.reason) {
				t.Errorf("log does not name the reason:\n%s", out)
			}
			// A reason somebody must act on is a WARN; one that is ordinary in
			// a healthy deployment is a DEBUG, so it can be read as a rate
			// without drowning the log.
			if warned := strings.Contains(out, "level=WARN"); warned != tc.actionable {
				t.Errorf("level=WARN present = %v, want %v:\n%s", warned, tc.actionable, out)
			}
			if got := r.mx.get("outbound_delivered"); got != 0 {
				t.Errorf("delivered = %d on a turn that was not delivered", got)
			}
		})
	}
}

// TestDeliveredIsCounted — the denominator. Without it a drop rate of zero and
// a bot nobody messaged are the same number, which is the ambiguity the whole
// breakdown exists to remove.
func TestDeliveredIsCounted(t *testing.T) {
	t.Parallel()
	r := newOutcomeRig(t, deliverableTurn(t), true)

	r.o.handleEvent(outcomeEvent())

	if got := r.mx.get("outbound_delivered"); got != 1 {
		t.Errorf("delivered = %d, want 1. log:\n%s", got, r.logs.String())
	}
	if got := r.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("dropped = %d, want 0", got)
	}
	if n := r.frames(); n != 1 {
		t.Errorf("frames = %d, want 1", n)
	}
}

// TestNonWecomSessionIsNotADrop — this subscriber sees every chat:done in the
// deployment, including Slack's and the web UI's. Those are not this adapter's
// to answer and must not inflate the drop rate.
func TestNonWecomSessionIsNotADrop(t *testing.T) {
	t.Parallel()
	q := deliverableTurn(t)
	q.sessionErr = pgx.ErrNoRows
	r := newOutcomeRig(t, q, true)

	r.o.handleEvent(outcomeEvent())

	if got := r.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("dropped = %d; another platform's session is not a WeCom drop", got)
	}
	if r.logs.Len() != 0 {
		t.Errorf("logged something for another platform's session:\n%s", r.logs.String())
	}
}

// TestRefusalIsNotATransportError — a refusal the server stated and a frame
// that never left call for opposite responses (fix the message vs retry the
// connection), so they must not share a bucket.
func TestRefusalIsNotATransportError(t *testing.T) {
	t.Parallel()
	if got := classifyDrop(&wecomAPIError{Cmd: cmdSendMsg, Code: 45002, Msg: "too long"}); got != dropPlatformRefused {
		t.Errorf("a stated refusal classified as %q", got)
	}
	if got := unconfirmedReason(errAckTimeout); got != "ack_timeout" {
		t.Errorf("a missing verdict classified as %q, want the unconfirmed ack_timeout", got)
	}
	if got := unconfirmedReason(errWriteAttempted); got != "write_attempted" {
		t.Errorf("an attempted write classified as %q, want the unconfirmed write_attempted", got)
	}
	if got := unconfirmedReason(errors.New("wecom: send_msg requires chat_id")); got != "" {
		t.Errorf("a provably local failure marked unconfirmed (%q); it is a definite drop", got)
	}
	if got := classifyDrop(errNoLiveConnection); got != dropNoConnection {
		t.Errorf("a missing socket classified as %q", got)
	}
	if got := classifyDrop(nil); got != "" {
		t.Errorf("success classified as %q", got)
	}
}

// TestNilMetricsSinkIsSafe — a deployment with /metrics off must not panic on
// the first dropped reply.
func TestNilMetricsSinkIsSafe(t *testing.T) {
	t.Parallel()
	q := deliverableTurn(t)
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	o := NewOutbound(q, reg, slog.Default()) // no WithOutboundMetrics

	o.handleEvent(outcomeEvent()) // no socket registered: takes the drop path
}

// TestReasonStringsArePinned — these are metric label values. Renaming one
// silently retires whatever alert or dashboard reads it, and the rename would
// otherwise pass every test in this package: nothing else asserts the strings
// themselves, only that some reason was recorded.
func TestReasonStringsArePinned(t *testing.T) {
	t.Parallel()
	for want, got := range map[string]dropReason{
		"no_live_connection":      dropNoConnection,
		"task_missing":            dropTaskMissing,
		"platform_refused":        dropPlatformRefused,
		"transport_error":         dropTransport,
		"attachment_not_admitted": dropAttachmentNotAdmitted,
	} {
		if string(got) != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	}
}

// TestSkipReasonStringsArePinned — the skipped set is a metric label too.
func TestSkipReasonStringsArePinned(t *testing.T) {
	t.Parallel()
	for want, got := range map[string]skipReason{
		"origin_not_channel":    skipOriginNotChannel,
		"installation_inactive": skipInstallationInactive,
		"nothing_to_say":        skipNothingToSay,
	} {
		if string(got) != want {
			t.Errorf("skip reason = %q, want %q", got, want)
		}
	}
}

// TestNotOwedIsSkippedNotDropped — the review finding. A question typed in the
// web UI on a WeCom-bound session was never owed to a WeCom user, so counting
// it as a failed delivery makes ordinary web usage read as an outage.
func TestNotOwedIsSkippedNotDropped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, reason string
		setup        func(q *fakeOutboundQueries)
	}{
		{"asked in the web UI", "origin_not_channel", func(q *fakeOutboundQueries) { q.channelIngested = askedInTheWebUI() }},
		{"installation revoked", "installation_inactive", func(q *fakeOutboundQueries) { q.installation.Status = "revoked" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q := deliverableTurn(t)
			tc.setup(q)
			r := newOutcomeRig(t, q, true)

			r.o.handleEvent(outcomeEvent())

			if got := r.mx.get("outbound_skipped:" + tc.reason); got != 1 {
				t.Errorf("skipped:%s = %d, want 1", tc.reason, got)
			}
			if got := r.mx.get("outbound_dropped"); got != 0 {
				t.Errorf("dropped = %d; this reply was never owed to WeCom", got)
			}
			if strings.Contains(r.logs.String(), "level=WARN") {
				t.Errorf("logged a WARN for an ordinary outcome:\n%s", r.logs.String())
			}
		})
	}
}

// TestActionableSplitIsPinned — every drop reaches an operator now that the
// ordinary outcomes have their own counter.
func TestActionableSplitIsPinned(t *testing.T) {
	t.Parallel()
	for _, r := range []dropReason{
		dropNoConnection, dropTaskMissing, dropPlatformRefused,
		dropTransport, dropAttachmentNotAdmitted,
	} {
		if !r.actionable() {
			t.Errorf("%s is a drop and must reach an operator", r)
		}
	}
}

// TestCompletionWithNoTaskIdIsCounted — the origin gate cannot run without a
// task to read the provenance stamp off, so the turn is withheld. Rare, but it
// used to be the quietest branch of all: no log, no counter, nothing.
func TestCompletionWithNoTaskIdIsCounted(t *testing.T) {
	t.Parallel()
	r := newOutcomeRig(t, deliverableTurn(t), true)

	e := outcomeEvent()
	e.Payload = protocol.ChatDonePayload{Content: "the answer"} // no TaskID
	r.o.handleEvent(e)

	if got := r.mx.get("outbound_dropped:task_missing"); got != 1 {
		t.Errorf("task_missing = %d, want 1. log:\n%s", got, r.logs.String())
	}
	if n := r.frames(); n != 0 {
		t.Errorf("frames = %d, want 0", n)
	}
}

// Every way out of sendAttachments before the per-file loop has to settle BOTH
// units: one outcome per file that will not be sent, and — when the files were
// the reply — exactly one reply outcome. Table-driven because the four exits
// were fixed one at a time and each fix left the others unasserted.
//
// It also pins which reason each exit files under, which is the half a comment
// cannot enforce: attachment_not_admitted appears on the reply counter only
// here, and only because these files WERE the answer.
func TestSendAttachments_EveryEarlyExitSettlesBothUnits(t *testing.T) {
	t.Parallel()
	const files = 3
	for _, tc := range []struct {
		name       string
		arrange    func(t *testing.T, o *Outbound, q *fakeOutboundQueries) context.Context
		wantFiles  string // attachment_dropped:<reason>, "" when the rows are not known yet
		wantReply  string // outbound_dropped:<reason>
		wantNFiles int
	}{
		{
			name: "the lookup itself failed, so no file is known",
			arrange: func(_ *testing.T, _ *Outbound, q *fakeOutboundQueries) context.Context {
				q.attachmentsErr = errors.New("wecom test: database is unwell")
				return context.Background()
			},
			wantReply:  "transport_error",
			wantNFiles: 0,
		},
		{
			name: "too many deliveries are already pending",
			arrange: func(_ *testing.T, o *Outbound, _ *fakeOutboundQueries) context.Context {
				o.pendingMu.Lock()
				o.pendingAttachments = maxPendingAttachmentDeliveries
				o.pendingMu.Unlock()
				return context.Background()
			},
			wantFiles:  "attachment_not_admitted",
			wantReply:  "attachment_not_admitted",
			wantNFiles: files,
		},
		{
			name: "the budget ran out waiting for a concurrency slot",
			arrange: func(_ *testing.T, _ *Outbound, _ *fakeOutboundQueries) context.Context {
				// Every slot taken, and a context already spent: the select
				// takes its ctx.Done arm without waiting on anything.
				for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
					attachmentSlots <- struct{}{}
				}
				t.Cleanup(func() {
					for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
						<-attachmentSlots
					}
				})
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantFiles:  "transport_error",
			wantReply:  "transport_error",
			wantNFiles: files,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := deliverableTurn(t)
			for i := 0; i < files; i++ {
				q.attachments = append(q.attachments, db.Attachment{
					ID: mustTestUUID(t), Filename: "f.bin",
					Url: "https://cdn.example/obj/f", SizeBytes: 4,
				})
			}
			r := newOutcomeRig(t, q, true)
			ctx := tc.arrange(t, r.o, q)

			// carriesTheReply: an empty completion whose files ARE the answer,
			// which is the only shape where a file outcome is also the reply's.
			r.o.sendAttachments(ctx, mustTestUUID(t), mustTestUUID(t),
				attachmentTarget{InstallationID: q.installation.ID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt},
				true)

			if got := r.mx.get("attachment_dropped"); got != tc.wantNFiles {
				t.Errorf("attachment_dropped = %d, want %d — one per file that will not be sent",
					got, tc.wantNFiles)
			}
			if tc.wantFiles != "" {
				if got := r.mx.get("attachment_dropped:" + tc.wantFiles); got != tc.wantNFiles {
					t.Errorf("attachment_dropped:%s = %d, want %d", tc.wantFiles, got, tc.wantNFiles)
				}
			}
			if got := r.mx.get("outbound_dropped"); got != 1 {
				t.Errorf("outbound_dropped = %d, want exactly 1 — the files were the reply", got)
			}
			if got := r.mx.get("outbound_dropped:" + tc.wantReply); got != 1 {
				t.Errorf("outbound_dropped:%s = %d, want 1. log:\n%s", tc.wantReply, got, r.logs.String())
			}
		})
	}
}

// A reply whose words already landed is settled: the same exits must move the
// file counter and leave the reply counter alone.
func TestSendAttachments_AnEarlyExitDoesNotReCountASettledReply(t *testing.T) {
	t.Parallel()
	q := deliverableTurn(t)
	q.attachments = append(q.attachments, db.Attachment{
		ID: mustTestUUID(t), Filename: "f.bin", Url: "https://cdn.example/obj/f", SizeBytes: 4,
	})
	r := newOutcomeRig(t, q, true)
	r.o.pendingMu.Lock()
	r.o.pendingAttachments = maxPendingAttachmentDeliveries
	r.o.pendingMu.Unlock()

	r.o.sendAttachments(context.Background(), mustTestUUID(t), mustTestUUID(t),
		attachmentTarget{InstallationID: q.installation.ID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt},
		false) // the words went out on their own

	if got := r.mx.get("attachment_dropped:attachment_not_admitted"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
	if got := r.mx.get("outbound_dropped"); got != 0 {
		t.Errorf("outbound_dropped = %d, want 0 — that reply was delivered and counted already", got)
	}
}

// The unknown side needs a rule for the same reason the definite side does: a
// reply with several unconfirmed files must not report whichever reason the
// loop happened to end on.
func TestWorseUnconfirmedReason_IsARuleNotALoopOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ a, b, want string }{
		{"", "interrupted", "interrupted"},
		{"interrupted", "write_attempted", "write_attempted"},
		{"write_attempted", "interrupted", "write_attempted"},
		{"write_attempted", "ack_timeout", "ack_timeout"},
		{"ack_timeout", "interrupted", "ack_timeout"},
		{"ack_timeout", "ack_timeout", "ack_timeout"},
	} {
		if got := worseUnconfirmedReason(tc.a, tc.b); got != tc.want {
			t.Errorf("worseUnconfirmedReason(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
