package wecom

// replier_test.go — the delivery primitive behind the #1 security fix. A
// binding token is a bearer credential, so it must go to the sender privately
// (chat_type=1), never to the group room in Source.ChatID. Here we assert the
// primitive postPrivate() addresses the user directly, and that the ordinary
// post() addresses the room — the two are distinct on purpose.
//
// The end-to-end sendBindingPrompt path (which mints a real token via the
// DB-backed BindingTokenService and then routes it through postPrivate) is
// covered by a handler-suite test that runs against Postgres in CI.
//
// Original defect report and analysis: seacen (PR #5833 review).

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

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
