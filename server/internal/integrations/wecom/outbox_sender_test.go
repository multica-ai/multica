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
	reg := newSendersRegistry()
	conn := &recordingConn{}

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
	// The default is terminal: an unrecognized write failure is more likely a
	// malformed frame than a blip, and retrying it eight times only delays the
	// dead letter.
	terminal := []error{
		nil,
		errors.New("wecom: send_msg chat_type must be 1 (single) or 2 (group)"),
		errors.New("wecom: send_msg requires chat_id"),
	}
	for _, err := range terminal {
		if retryableSendError(err) {
			t.Errorf("retryableSendError(%v) = true, want false", err)
		}
	}
}
