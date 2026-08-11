package wecom

// outbox_sender_test.go — the WeCom half of the queue: which payload documents
// render, and which write failures are worth retrying. The queue's own settle
// policy is tested in channel/outbox; here the question is only what
// Disposition WeCom hands it.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func queueRow(t *testing.T, payload outboundPayload) db.ChannelOutboundQueue {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return db.ChannelOutboundQueue{
		InstallationID: mustTestUUID(t),
		ChannelType:    channelTypeWecom,
		TargetChatID:   "CHAT_1",
		TargetChatType: int16(chatTypeGroupInt),
		MsgType:        msgTypeMarkdown,
		PayloadVersion: payloadVersionV1,
		Payload:        raw,
	}
}

func TestQueueSender_SendsRenderedContentOverTheLiveSocket(t *testing.T) {
	t.Parallel()
	reg := newSendersRegistry()
	conn := &recordingConn{}
	row := queueRow(t, outboundPayload{Content: "the agent reply"})
	reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))

	disposition, err := newQueueSender(reg).Send(context.Background(), row)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if disposition != outbox.DispositionSent {
		t.Errorf("disposition = %v, want sent", disposition)
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "CHAT_1" {
		t.Errorf("chatid = %v, want CHAT_1", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeGroupInt) {
		t.Errorf("chat_type = %v, want group", body["chat_type"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["content"] != "the agent reply" {
		t.Errorf("content = %v, want the agent reply", body["markdown"])
	}
}

// No live socket on this replica means the lease moved or the Supervisor is
// mid-reconnect. Both are transient from the row's point of view — the holder
// will drain it — so this must retry, never dead-letter.
func TestQueueSender_NoLiveConnectionRetries(t *testing.T) {
	t.Parallel()
	row := queueRow(t, outboundPayload{Content: "hi"})
	disposition, err := newQueueSender(newSendersRegistry()).Send(context.Background(), row)
	if err == nil {
		t.Error("expected an error describing the missing connection")
	}
	if disposition != outbox.DispositionRetry {
		t.Errorf("disposition = %v, want retry", disposition)
	}
}

func TestQueueSender_NilRegistryRetries(t *testing.T) {
	t.Parallel()
	row := queueRow(t, outboundPayload{Content: "hi"})
	disposition, _ := newQueueSender(nil).Send(context.Background(), row)
	if disposition != outbox.DispositionRetry {
		t.Errorf("disposition = %v, want retry", disposition)
	}
}

// An unrenderable payload cannot become renderable by waiting, so it must
// dead-letter rather than burn the retry budget.
func TestQueueSender_UnrenderablePayloadIsTerminal(t *testing.T) {
	t.Parallel()

	cases := map[string]db.ChannelOutboundQueue{
		"unknown template": queueRow(t, outboundPayload{Template: "no_such_template"}),
		"empty content":    queueRow(t, outboundPayload{}),
	}
	malformed := queueRow(t, outboundPayload{Content: "x"})
	malformed.Payload = []byte("{not json")
	cases["malformed json"] = malformed

	for name, row := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Own conn and registry per subtest: autoAck writes to the double,
			// and these subtests run concurrently.
			conn := &recordingConn{}
			reg := newSendersRegistry()
			reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))
			disposition, err := newQueueSender(reg).Send(context.Background(), row)
			if err == nil {
				t.Error("expected an error")
			}
			if disposition != outbox.DispositionFailed {
				t.Errorf("disposition = %v, want failed", disposition)
			}
		})
	}
}

// A row written by a newer replica during a rolling deploy carries a payload
// schema this build does not know. Guessing at it risks delivering a mangled or
// mis-addressed message, so it dead-letters instead.
func TestQueueSender_FuturePayloadVersionIsTerminal(t *testing.T) {
	t.Parallel()
	reg := newSendersRegistry()
	conn := &recordingConn{}
	row := queueRow(t, outboundPayload{Content: "hi"})
	row.PayloadVersion = payloadVersionV1 + 1
	reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))

	disposition, err := newQueueSender(reg).Send(context.Background(), row)
	if err == nil {
		t.Error("expected an error naming the unsupported version")
	}
	if disposition != outbox.DispositionFailed {
		t.Errorf("disposition = %v, want failed", disposition)
	}
	if len(conn.frames) != 0 {
		t.Error("an unknown payload version must not be written to the socket")
	}
}

// payload_version 0 predates the column default and must read as v1 rather than
// dead-lettering every row an older writer produced.
func TestQueueSender_ZeroPayloadVersionRendersAsV1(t *testing.T) {
	t.Parallel()
	reg := newSendersRegistry()
	conn := &recordingConn{}
	row := queueRow(t, outboundPayload{Content: "hi"})
	row.PayloadVersion = 0
	reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))

	disposition, err := newQueueSender(reg).Send(context.Background(), row)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if disposition != outbox.DispositionSent {
		t.Errorf("disposition = %v, want sent", disposition)
	}
}

func TestRenderOutbound_TaskFailedTemplate(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(outboundPayload{
		Template:      templateTaskFailed,
		AgentName:     "Mika",
		FailureReason: "runtime offline",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := renderOutbound(raw)
	if err != nil {
		t.Fatalf("renderOutbound: %v", err)
	}
	for _, want := range []string{"Mika", "runtime offline"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %q, want it to mention %q", got, want)
		}
	}

	// A failure with no reason still renders a notice: silence reads as a
	// broken bot.
	raw, err = json.Marshal(outboundPayload{Template: templateTaskFailed})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err = renderOutbound(raw)
	if err != nil {
		t.Fatalf("renderOutbound: %v", err)
	}
	if got == "" {
		t.Error("a reasonless failure must still render a notice")
	}
}

func TestRetryableSendError(t *testing.T) {
	t.Parallel()
	retryable := []error{
		context.DeadlineExceeded,
		// A shutdown or a lease loss: the consumer stopped, not the row.
		context.Canceled,
		// The frame went out and no verdict came back. Ambiguous, and retried
		// on purpose — see the comment on retryableSendError.
		errAckTimeout,
		// Quota, not content: the same frame succeeds once the window moves.
		&wecomAPIError{Cmd: cmdSendMsg, Code: errCodeAPIFreqLimit, Msg: "api freq out of limit"},
		&wecomAPIError{Cmd: cmdSendMsg, Code: errCodeAPIConcurrencyLimit, Msg: "api concurrency out of limit"},
		&wecomAPIError{Cmd: cmdSendMsg, Code: errCodeSystemBusy, Msg: "system busy"},
		errors.New("write tcp: i/o timeout"),
		errors.New("write: broken pipe"),
		errors.New("read: connection reset by peer"),
		errors.New("use of closed network connection"),
		errors.New("websocket: close 1006 (abnormal closure)"),
	}
	for _, err := range retryable {
		if !retryableSendError(err) {
			t.Errorf("retryableSendError(%v) = false, want true", err)
		}
	}
	// A refusal the server stated is decided on the code, and the default for a
	// stated refusal is terminal: WeCom looked at this exact frame and declined
	// it, so eight identical frames only delay the dead letter.
	terminal := []error{
		nil,
		&wecomAPIError{Cmd: cmdSendMsg, Code: errCodeMsgTooLong, Msg: "msg too long"},
		&wecomAPIError{Cmd: cmdSendMsg, Code: 999999, Msg: "something new"},
		errors.New("wecom: send_msg chat_type must be 1 (single) or 2 (group)"),
		errors.New("wecom: send_msg requires chat_id"),
	}
	for _, err := range terminal {
		if retryableSendError(err) {
			t.Errorf("retryableSendError(%v) = true, want false", err)
		}
	}
	// The prose fallback must not be what decides a stated refusal: WeCom's
	// errmsg is not ours, and a transient-looking word in it would flip a
	// permanent refusal into eight pointless retries.
	if retryableSendError(&wecomAPIError{Cmd: cmdSendMsg, Code: errCodeMsgTooLong, Msg: "connection reset while checking length"}) {
		t.Error("a permanent errcode was overridden by transient-looking prose in the server's errmsg")
	}
}

// sendWithVerdict runs one row against a socket double that answers with the
// given errcode, and reports what the queue was told.
func sendWithVerdict(t *testing.T, code int, msg string, content string) (outbox.Disposition, error, *recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	conn := &recordingConn{refuseCode: code, refuseMsg: msg}
	row := queueRow(t, outboundPayload{Content: content})
	reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))
	d, err := newQueueSender(reg).Send(context.Background(), row)
	return d, err, conn
}

// Quota is about the window, not the frame: the same message succeeds a moment
// later. Dead-lettering it throws away a reply the user is waiting for, and
// prose-matching cannot tell this apart from a malformed frame — errcode can.
func TestQueueSender_TransientRefusalRetries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		code int
		msg  string
	}{
		{errCodeAPIFreqLimit, "api freq out of limit"},
		{errCodeAPIConcurrencyLimit, "api concurrency out of limit"},
		{errCodeSystemBusy, "system busy"},
	} {
		d, err, _ := sendWithVerdict(t, tc.code, tc.msg, "hi")
		if err == nil {
			t.Fatalf("errcode %d reported success", tc.code)
		}
		if d != outbox.DispositionRetry {
			t.Errorf("errcode %d (%s) → %v, want retry", tc.code, tc.msg, d)
		}
	}
}

// A refusal the server stated about this exact frame is permanent: it has
// looked at these bytes and declined them, so eight identical frames only
// delay the dead letter.
func TestQueueSender_StatedRefusalIsTerminal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		code int
	}{
		{"over the size cap", errCodeMsgTooLong},
		{"a code this build has never seen", 999999},
	} {
		d, err, _ := sendWithVerdict(t, tc.code, "refused", "hi")
		if err == nil {
			t.Fatalf("%s reported success", tc.name)
		}
		if d != outbox.DispositionFailed {
			t.Errorf("%s (errcode %d) → %v, want failed", tc.name, tc.code, d)
		}
	}
}

// WeCom refuses an over-cap aibot_send_msg with errcode 45002 and writes
// nothing, identically every time. Catching it here costs the row one attempt
// instead of eight, and no frame should reach the socket at all.
func TestQueueSender_OverCapContentIsTerminalAndWritesNothing(t *testing.T) {
	t.Parallel()
	reg := newSendersRegistry()
	conn := &recordingConn{}
	row := queueRow(t, outboundPayload{Content: strings.Repeat("x", maxSendContentBytes+1)})
	reg.set(row.InstallationID, conn.autoAck(newWSSender(conn, nil)))

	d, err := newQueueSender(reg).Send(context.Background(), row)
	if err == nil {
		t.Fatal("an over-cap row reported success")
	}
	if d != outbox.DispositionFailed {
		t.Errorf("disposition = %v, want failed — retrying cannot make the bytes shorter", d)
	}
	conn.mu.Lock()
	frames := len(conn.frames)
	conn.mu.Unlock()
	if frames != 0 {
		t.Errorf("wrote %d frames, want 0 — the cap must be checked before the socket", frames)
	}
}

// The consumer's context bounds the ack wait. A lease loss or a shutdown has to
// stop the send now, and leave the row queued for whoever holds the socket next
// rather than spending its attempt budget.
func TestQueueSender_CancelledContextRetriesWithoutWaitingForTheAck(t *testing.T) {
	t.Parallel()
	reg := newSendersRegistry()
	conn := &recordingConn{} // no autoAck: no verdict ever arrives, so only ctx can end the wait
	row := queueRow(t, outboundPayload{Content: "hi"})
	reg.set(row.InstallationID, newWSSender(conn, nil))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	d, err := newQueueSender(reg).Send(ctx, row)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a cancelled send reported success")
	}
	if d != outbox.DispositionRetry {
		t.Errorf("disposition = %v, want retry — a shutdown is not the row's fault", d)
	}
	if elapsed >= ackTimeout {
		t.Errorf("took %v, want well under the %v ack timeout: ctx is not reaching the send", elapsed, ackTimeout)
	}
}
