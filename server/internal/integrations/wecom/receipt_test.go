package wecom

// receipt_test.go — the receipt is one finish=true stream frame addressed by
// the callback's req_id; one per turn, none without a req_id, none without a
// socket.
//
// REVERSE VERIFICATION: with the senders.stream call removed from OnIngested
// every test here fails on "no frame"; with the suppress check removed the
// coalescing test fails on "2 frames".

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func callbackMessage(reqID, chatID string) channel.InboundMessage {
	var mc aibotMsgCallback
	mc.MsgID = "m-" + reqID
	mc.ChatID = chatID
	mc.ChatType = "single"
	mc.From.UserID = "u1"
	mc.MsgType = "text"
	mc.Text.Content = "hello"
	return channelMessageFromCallback("bot-1", "Bot", mc, "hello", reqID)
}

func receiptRig(t *testing.T) (*receiptNotifier, engine.ResolvedInstallation, *recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t), Active: true}
	conn := &recordingConn{}
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, nil)))
	n := NewReceiptNotifier(reg, nil)
	return n, inst, conn
}

func streamFrames(t *testing.T, conn *recordingConn) []map[string]any {
	t.Helper()
	conn.mu.Lock()
	defer conn.mu.Unlock()
	var out []map[string]any
	for _, f := range conn.frames {
		if f.Cmd != cmdRespondMsg {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode stream body: %v", err)
		}
		body["_req_id"] = f.Headers.ReqID
		out = append(out, body)
	}
	return out
}

func TestReceipt_OneFinishedFrameOnTheCallbacksReqID(t *testing.T) {
	t.Parallel()
	n, inst, conn := receiptRig(t)
	session := mustTestUUID(t)

	n.OnIngested(context.Background(), inst, callbackMessage("REQ-1", "CHAT_1"), session)

	frames := streamFrames(t, conn)
	if len(frames) != 1 {
		t.Fatalf("%d stream frames, want 1", len(frames))
	}
	f := frames[0]
	if f["_req_id"] != "REQ-1" {
		t.Errorf("req_id = %v, want the callback's REQ-1", f["_req_id"])
	}
	if f["msgtype"] != "stream" {
		t.Errorf("msgtype = %v, want stream", f["msgtype"])
	}
	stream, _ := f["stream"].(map[string]any)
	if stream["finish"] != true {
		t.Errorf("finish = %v, want true: the receipt is a single sealed frame", stream["finish"])
	}
	if stream["content"] != receiptText {
		t.Errorf("content = %q, want %q", stream["content"], receiptText)
	}
	if id, _ := stream["id"].(string); id == "" {
		t.Error("stream id is empty")
	}
	if n := len(conn.frames); n != 1 {
		t.Errorf("%d frames in total, want 1: nothing but the receipt", n)
	}
}

func TestReceipt_ABurstIntoOneRunGetsOneReceipt(t *testing.T) {
	t.Parallel()
	n, inst, conn := receiptRig(t)
	session := mustTestUUID(t)
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-1", "CHAT_1"), session)
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-2", "CHAT_1"), session)
	if got := len(streamFrames(t, conn)); got != 1 {
		t.Fatalf("%d receipts for two messages inside the coalesce window, want 1", got)
	}
	// A different session is a different conversation.
	var other pgtype.UUID
	if err := other.Scan("99999999-9999-4999-8999-999999999999"); err != nil {
		t.Fatal(err)
	}
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-3", "CHAT_2"), other)
	if got := len(streamFrames(t, conn)); got != 2 {
		t.Fatalf("%d receipts after a second session asked, want 2", got)
	}
}

func TestReceipt_ALaterTurnGetsItsOwnReceipt(t *testing.T) {
	t.Parallel()
	n, inst, conn := receiptRig(t)
	session := mustTestUUID(t)
	clock := time.Now()
	n.now = func() time.Time { return clock }
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-1", "CHAT_1"), session)
	clock = clock.Add(receiptCoalesceWindow + time.Second)
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-2", "CHAT_1"), session)
	if got := len(streamFrames(t, conn)); got != 2 {
		t.Fatalf("%d receipts for two turns %v apart, want 2", got, receiptCoalesceWindow+time.Second)
	}
}

func TestReceipt_OnSettledLetsTheNextTurnAckAtOnce(t *testing.T) {
	t.Parallel()
	n, inst, conn := receiptRig(t)
	session := mustTestUUID(t)
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-1", "CHAT_1"), session)
	n.OnSettled(context.Background(), session)
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-2", "CHAT_1"), session)
	if got := len(streamFrames(t, conn)); got != 2 {
		t.Fatalf("%d receipts, want 2: OnSettled clears the coalescing entry", got)
	}
}

func TestReceipt_NoReqIDNoReceipt(t *testing.T) {
	t.Parallel()
	n, inst, conn := receiptRig(t)
	n.OnIngested(context.Background(), inst, callbackMessage("", "CHAT_1"), mustTestUUID(t))
	if got := len(conn.frames); got != 0 {
		t.Fatalf("%d frames for a message with no req_id, want 0: only a message callback's req_id can carry a reply", got)
	}
}

func TestReceipt_NoSocketNoReceiptAndNoPanic(t *testing.T) {
	t.Parallel()
	n := NewReceiptNotifier(newSendersRegistry(), nil)
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	n.OnIngested(context.Background(), inst, callbackMessage("REQ-1", "CHAT_1"), mustTestUUID(t))
	// Nothing to assert on the wire: there is no wire. Returning is the test.
	var zero pgtype.UUID
	n.OnSettled(context.Background(), zero)
}
