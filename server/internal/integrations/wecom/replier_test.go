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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// fakeBinder mints a fixed, recognizable raw token without touching a DB.
type fakeBinder struct{ raw string }

func (f fakeBinder) Mint(context.Context, pgtype.UUID, pgtype.UUID, string) (BindingToken, error) {
	return BindingToken{Raw: f.raw, ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
}

// recordingConn captures every frame written to it so a test can inspect the
// aibot_send_msg body (chatid + chat_type) without a real socket.
type recordingConn struct {
	mu     sync.Mutex
	frames []frameEnvelope
}

func (c *recordingConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	c.mu.Unlock()
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
	reg.set(inst.ID, newWSSender(conn, nil))
	r := NewOutboundReplier(OutboundReplierConfig{Senders: reg, AppURL: "https://multica.example"})
	return r, inst, conn
}

func TestPostPrivate_AddressesUserWithSingleChatType(t *testing.T) {
	t.Parallel()
	r, inst, conn := newReplierWithConn(t)

	const senderUserID = "SENDER_USERID"
	const secretURL = "https://multica.example/wecom/bind?token=SECRET_TOKEN"
	if err := r.postPrivate(inst, senderUserID, secretURL); err != nil {
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
	if err := r.post(nil, inst, msg, "a token-less line"); err != nil {
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
	reg.set(inst.ID, newWSSender(conn, nil))
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
	reg.set(inst.ID, newWSSender(conn, nil))
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
