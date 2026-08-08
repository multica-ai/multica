package wecom

// replier_test.go — the security fix for the group-chat binding-token leak. A
// binding token is a bearer credential, so it must go to the sender privately
// (chat_type=1), never to the group room in Source.ChatID. The key test drives
// the real sendBindingPrompt group branch (via a fake binder, so no DB-backed
// token mint) and asserts the token reaches only the sender and no
// group-addressed frame carries it. The postPrivate / post primitives are
// checked separately below.
//
// Original defect report and analysis: seacen (PR #5833 review).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// fakeBinder mints a fixed, recognizable raw token without touching a DB.
// With reused set it behaves like the throttle suppressing a mint: no raw
// secret, only the live token's expiry.
type fakeBinder struct {
	raw    string
	reused bool
}

func (f fakeBinder) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	if f.reused {
		return BindingToken{ExpiresAt: time.Now().Add(14 * time.Minute), Reused: true}, nil
	}
	return BindingToken{Raw: f.raw, ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
}

// recordingConn captures every frame written to it so a test can inspect the
// aibot_send_msg body (chatid + chat_type) without a real socket.
type recordingConn struct {
	mu     sync.Mutex
	frames []frameEnvelope

	// sender, when set, makes this double answer its writes the way the real
	// server does: an ack frame echoing the req_id with errcode 0. Senders
	// that read their verdict block until one arrives, so a double that never
	// answers turns every send into a 5-second timeout.
	//
	// Set refuseCode to make the server refuse instead.
	sender     *wsSender
	refuseCode int
	refuseMsg  string
}

// autoAck wires the double to answer the sender's writes. Call it after
// newWSSender, which needs the conn first.
func (c *recordingConn) autoAck(s *wsSender) *wsSender {
	c.sender = s
	return s
}

func (c *recordingConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	s := c.sender
	code, msg := c.refuseCode, c.refuseMsg
	c.mu.Unlock()
	if s != nil {
		s.routeResponse(frameEnvelope{
			Headers: frameHeaders{ReqID: env.Headers.ReqID},
			ErrCode: code,
			ErrMsg:  msg,
		})
	}
	return nil
}
func (c *recordingConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *recordingConn) SetReadDeadline(time.Time) error   { return nil }
func (c *recordingConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *recordingConn) Close() error                      { return nil }

// sendBody decodes the body of the i-th recorded aibot_send_msg frame.
func (c *recordingConn) sendBody(t *testing.T, i int) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.frames) {
		t.Fatalf("frame %d not recorded (have %d)", i, len(c.frames))
	}
	var body map[string]any
	if err := json.Unmarshal(c.frames[i].Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func newReplierWithConn(t *testing.T) (*OutboundReplier, engine.ResolvedInstallation, *recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	conn := &recordingConn{}
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, nil)))
	r := NewOutboundReplier(OutboundReplierConfig{Senders: reg, AppURL: "https://multica.example"})
	return r, inst, conn
}

func TestPostPrivate_AddressesUserWithSingleChatType(t *testing.T) {
	t.Parallel()
	r, inst, conn := newReplierWithConn(t)

	const senderUserID = "SENDER_USERID"
	const secretURL = "https://multica.example/wecom/bind?token=SECRET_TOKEN"
	if err := r.postPrivate(context.Background(), inst, senderUserID, secretURL); err != nil {
		t.Fatalf("postPrivate: %v", err)
	}

	body := conn.sendBody(t, 0)
	if body["chatid"] != senderUserID {
		t.Errorf("private send chatid = %v, want the sender's own userid %q", body["chatid"], senderUserID)
	}
	// chat_type round-trips through JSON as float64.
	if body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("private send chat_type = %v, want %d (single)", body["chat_type"], chatTypeSingleInt)
	}
}

// TestPost_AddressesRoom is the contrast: the ordinary reply path targets the
// message's Source.ChatID (the group in a group chat). This is exactly why the
// binding token must NOT go through here — it would land in the room.
func TestPost_AddressesRoomChatID(t *testing.T) {
	t.Parallel()
	r, inst, conn := newReplierWithConn(t)

	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "GROUP_CHAT_ID",
		ChatType: channel.ChatTypeGroup,
		SenderID: "SENDER_USERID",
	}}
	if err := r.post(context.Background(), inst, msg, "a token-less line"); err != nil {
		t.Fatalf("post: %v", err)
	}

	body := conn.sendBody(t, 0)
	if body["chatid"] != "GROUP_CHAT_ID" {
		t.Errorf("group reply chatid = %v, want the group chatid", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeGroupInt) {
		t.Errorf("group reply chat_type = %v, want %d (group)", body["chat_type"], chatTypeGroupInt)
	}
}

// TestSendBindingPrompt_GroupNeverLeaksToken drives the REAL sendBindingPrompt
// group branch — the single line the whole #1 fix rests on. It asserts the
// token-bearing frame goes only to the sender at chat_type=1, the group gets a
// token-less acknowledgement, and NO group-addressed frame carries the raw
// token. Re-pointing sendBindingPrompt at post() (the pre-fix bug) fails this.
func TestSendBindingPrompt_GroupNeverLeaksToken(t *testing.T) {
	t.Parallel()
	const rawToken = "SECRET_BEARER_TOKEN_do_not_leak"
	const senderID = "SENDER_USERID"
	const groupID = "GROUP_CHAT_ID"

	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	conn := &recordingConn{}
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, nil)))
	r := NewOutboundReplier(OutboundReplierConfig{
		Binding: nil, // set the interface field directly with the fake below
		Senders: reg,
		AppURL:  "https://multica.example",
	})
	r.binding = fakeBinder{raw: rawToken}

	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   groupID,
		ChatType: channel.ChatTypeGroup,
		SenderID: senderID,
	}}
	if err := r.sendBindingPrompt(context.Background(), inst, msg, engine.Result{Sender: senderID}); err != nil {
		t.Fatalf("sendBindingPrompt: %v", err)
	}

	conn.mu.Lock()
	frames := append([]frameEnvelope(nil), conn.frames...)
	conn.mu.Unlock()
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames (private token + group ack), got %d", len(frames))
	}

	tokenFrames, groupFrames := 0, 0
	for i := range frames {
		var body map[string]any
		if err := json.Unmarshal(frames[i].Body, &body); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		content := ""
		if md, ok := body["markdown"].(map[string]any); ok {
			content, _ = md["content"].(string)
		}
		carriesToken := strings.Contains(content, rawToken)
		chatID, _ := body["chatid"].(string)

		if chatID == groupID {
			groupFrames++
			if carriesToken {
				t.Errorf("SECURITY: a group-addressed frame carries the raw token: %q", content)
			}
			if body["chat_type"] != float64(chatTypeGroupInt) {
				t.Errorf("group frame chat_type = %v, want group", body["chat_type"])
			}
		}
		if carriesToken {
			tokenFrames++
			if chatID != senderID {
				t.Errorf("SECURITY: token frame addressed to %q, want the sender %q", chatID, senderID)
			}
			if body["chat_type"] != float64(chatTypeSingleInt) {
				t.Errorf("token frame chat_type = %v, want single (1)", body["chat_type"])
			}
		}
	}
	if tokenFrames != 1 {
		t.Errorf("expected exactly one token-bearing frame (the private send), got %d", tokenFrames)
	}
	if groupFrames != 1 {
		t.Errorf("expected exactly one group acknowledgement frame, got %d", groupFrames)
	}
}

// TestSendBindingPrompt_P2PSendsOnlyPrivately: a 1:1 trigger already IS the
// private chat, so exactly one frame (the token) goes out, no group ack.
func TestSendBindingPrompt_P2PSendsOnlyPrivately(t *testing.T) {
	t.Parallel()
	const rawToken = "P2P_TOKEN"
	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	conn := &recordingConn{}
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, nil)))
	r := NewOutboundReplier(OutboundReplierConfig{Senders: reg, AppURL: "https://multica.example"})
	r.binding = fakeBinder{raw: rawToken}

	msg := channel.InboundMessage{Source: channel.Source{ChatID: "USER_A", ChatType: channel.ChatTypeP2P, SenderID: "USER_A"}}
	if err := r.sendBindingPrompt(context.Background(), inst, msg, engine.Result{Sender: "USER_A"}); err != nil {
		t.Fatalf("sendBindingPrompt: %v", err)
	}
	conn.mu.Lock()
	n := len(conn.frames)
	conn.mu.Unlock()
	if n != 1 {
		t.Fatalf("p2p trigger should send exactly one (private) frame, got %d", n)
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "USER_A" || body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("p2p token frame = %v, want USER_A at chat_type 1", body)
	}
}

// TestSendBindingPrompt_ThrottledSendsNoURL: when the throttle suppresses a
// mint there is no raw secret to build a link from — the hash is all the table
// ever held. Building the URL anyway yields "?token=" with nothing after it,
// which is a dead link the user will tap and be refused by. The reply must
// point them at the message they already have instead, and the room must
// still get its answer.
func TestSendBindingPrompt_ThrottledSendsNoURL(t *testing.T) {
	t.Parallel()
	const senderID = "SENDER_USERID"
	const groupID = "GROUP_CHAT_ID"

	reg := newSendersRegistry()
	inst := engine.ResolvedInstallation{ID: mustTestUUID(t)}
	conn := &recordingConn{}
	reg.set(inst.ID, conn.autoAck(newWSSender(conn, nil)))
	r := NewOutboundReplier(OutboundReplierConfig{
		Senders: reg,
		AppURL:  "https://multica.example",
	})
	r.binding = fakeBinder{reused: true}

	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   groupID,
		ChatType: channel.ChatTypeGroup,
		SenderID: senderID,
	}}
	if err := r.sendBindingPrompt(context.Background(), inst, msg, engine.Result{Sender: senderID}); err != nil {
		t.Fatalf("sendBindingPrompt: %v", err)
	}

	conn.mu.Lock()
	frames := append([]frameEnvelope(nil), conn.frames...)
	conn.mu.Unlock()
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames (private notice + group ack), got %d", len(frames))
	}

	privateFrames := 0
	for i := range frames {
		var body map[string]any
		if err := json.Unmarshal(frames[i].Body, &body); err != nil {
			t.Fatalf("decode frame %d: %v", i, err)
		}
		content := ""
		if md, ok := body["markdown"].(map[string]any); ok {
			content, _ = md["content"].(string)
		}
		if strings.Contains(content, "token=") {
			t.Errorf("a throttled prompt built a URL with no token in it: %q", content)
		}
		if strings.Contains(content, "https://multica.example") {
			t.Errorf("a throttled prompt must not carry a bind link at all: %q", content)
		}
		if chatID, _ := body["chatid"].(string); chatID == senderID {
			privateFrames++
			if body["chat_type"] != float64(chatTypeSingleInt) {
				t.Errorf("private frame chat_type = %v, want single (1)", body["chat_type"])
			}
		}
	}
	if privateFrames != 1 {
		t.Errorf("expected exactly one privately-addressed frame, got %d", privateFrames)
	}
}

// TestPost_HonoursTheCallersDeadline guards the budget the calling code
// already believed it had. Bus delivery is synchronous, so the reply path runs
// on the publishing goroutine; outbound.go and handleInboxNew each build a
// bounded ctx precisely so a stalled WeCom round trip cannot hold it. Waiting
// for the server's verdict on a hardcoded context.Background() made those
// bounds decorative — a lost ack cost the full ackTimeout per subscriber, in
// series, on an HTTP handler's goroutine.
//
// A ctx that is already done is the cheap, deterministic stand-in: it must come
// back at once rather than serve out the ack wait.
func TestPost_HonoursTheCallersDeadline(t *testing.T) {
	t.Parallel()
	r, inst, _ := newReplierWithConn(t)

	msg := channel.InboundMessage{Source: channel.Source{
		ChatID:   "GROUP_CHAT_ID",
		ChatType: channel.ChatTypeGroup,
		SenderID: "SENDER_USERID",
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- r.post(ctx, inst, msg, "a line nobody is waiting for") }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("post on a cancelled ctx returned %v, want context.Canceled", err)
		}
	case <-time.After(ackTimeout / 2):
		t.Fatal("post ignored the caller's cancelled ctx and sat on the ack wait — " +
			"the deadline the publishing goroutine budgeted for is not being applied")
	}
}
