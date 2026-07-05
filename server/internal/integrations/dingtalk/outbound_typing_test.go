package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// robotAPIServer extends the emotion fake with the group-send endpoint so
// the full chat-done path (clear emotion, then reply) can be observed.
type robotAPIServer struct {
	mu      sync.Mutex
	recalls int
	sends   []map[string]any
}

func newRobotAPIServer(t *testing.T) (*robotAPIServer, *httptest.Server) {
	t.Helper()
	rec := &robotAPIServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = w.Write([]byte(`{"accessToken":"tok_test","expireIn":7200}`))
		case "/v1.0/robot/emotion/reply":
			_, _ = w.Write([]byte(`{}`))
		case "/v1.0/robot/emotion/recall":
			rec.mu.Lock()
			rec.recalls++
			rec.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		case "/v1.0/robot/groupMessages/send":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			rec.mu.Lock()
			rec.sends = append(rec.sends, body)
			rec.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return rec, srv
}

func outboundFixture(t *testing.T, rec *httptest.Server) (*Outbound, *TypingIndicatorManager, *fakeTypingQueries) {
	t.Helper()
	messenger := NewRobotMessenger(rec.URL, rec.URL, rec.Client())
	instID := typingTestUUID(1)
	instRow := testInstallationRow(t, instID, "client_a")
	q := &fakeTypingQueries{
		binding: db.ChannelChatSessionBinding{
			InstallationID: instID,
			ChannelChatID:  "cid",
			ChatType:       string(channel.ChatTypeGroup),
		},
		inst: instRow,
	}
	mgr := NewTypingIndicatorManager(messenger, plaintextDecrypter, q, nil)
	// fakeTypingQueries satisfies outboundQueries too — same two methods.
	out := NewOutbound(q, plaintextDecrypter, messenger, mgr, nil)
	return out, mgr, q
}

func TestOutboundTaskFailedClearsTypingAndNotifies(t *testing.T) {
	rec, srv := newRobotAPIServer(t)
	out, mgr, q := outboundFixture(t, srv)

	session := typingTestUUID(2)
	mgr.Add(context.Background(), q.inst, session, EmotionTarget{OpenConversationID: "cid", OpenMsgID: "m1"}, time.Now().UnixMilli())

	// Production shape: broadcastTaskEvent carries chat_session_id only in
	// the payload map — the top-level ChatSessionID scope hint stays empty.
	err := out.processEvent(context.Background(), events.Event{
		Type: protocol.EventTaskFailed,
		Payload: map[string]any{
			"task_id":         "t1",
			"chat_session_id": util.UUIDToString(session),
			"status":          "failed",
		},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if rec.recalls != 1 {
		t.Fatalf("expected the processing emotion to be recalled, got %d", rec.recalls)
	}
	// A failed run must be visibly failed, not silent: the user gets the
	// failure notice after the emotion clears.
	if len(rec.sends) != 1 {
		t.Fatalf("task-failed must send the failure notice, got %d sends", len(rec.sends))
	}
	msgParam, _ := rec.sends[0]["msgParam"].(string)
	if !strings.Contains(msgParam, "处理失败") {
		t.Fatalf("failure notice text missing, got %q", msgParam)
	}
}

func TestOutboundChatDoneClearsTypingBeforeReply(t *testing.T) {
	rec, srv := newRobotAPIServer(t)
	out, mgr, q := outboundFixture(t, srv)

	session := typingTestUUID(2)
	mgr.Add(context.Background(), q.inst, session, EmotionTarget{OpenConversationID: "cid", OpenMsgID: "m1"}, time.Now().UnixMilli())

	err := out.processEvent(context.Background(), events.Event{
		Type:          protocol.EventChatDone,
		ChatSessionID: util.UUIDToString(session),
		Payload:       protocol.ChatDonePayload{Content: "done!"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if rec.recalls != 1 {
		t.Fatalf("expected the processing emotion to be recalled, got %d", rec.recalls)
	}
	if len(rec.sends) != 1 {
		t.Fatalf("expected the reply to be sent, got %d", len(rec.sends))
	}
}
