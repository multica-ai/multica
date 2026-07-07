package dingtalk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// emotionAPIServer fakes the DingTalk token + emotion endpoints and
// records emotion posts.
type emotionAPIServer struct {
	mu      sync.Mutex
	replies []map[string]any
	recalls []map[string]any
}

func newEmotionAPIServer(t *testing.T) (*emotionAPIServer, *httptest.Server) {
	t.Helper()
	rec := &emotionAPIServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_, _ = w.Write([]byte(`{"accessToken":"tok_test","expireIn":7200}`))
		case "/v1.0/robot/emotion/reply", "/v1.0/robot/emotion/recall":
			if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "tok_test" {
				t.Errorf("emotion call missing access token, got %q", got)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			rec.mu.Lock()
			if r.URL.Path == "/v1.0/robot/emotion/reply" {
				rec.replies = append(rec.replies, body)
			} else {
				rec.recalls = append(rec.recalls, body)
			}
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

func testInstallationRow(t *testing.T, id pgtype.UUID, clientID string) db.ChannelInstallation {
	t.Helper()
	cfg, err := json.Marshal(dingtalkInstallConfig{
		AppID:              clientID,
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret_" + clientID)),
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return db.ChannelInstallation{ID: id, ChannelType: string(TypeDingtalk), Config: cfg, Status: "active"}
}

// plaintextDecrypter round-trips the "ciphertext" unchanged.
func plaintextDecrypter(b []byte) ([]byte, error) { return b, nil }

type fakeTypingQueries struct {
	binding db.ChannelChatSessionBinding
	inst    db.ChannelInstallation
	calls   int
}

func (f *fakeTypingQueries) GetChannelChatSessionBindingBySession(_ context.Context, _ db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	f.calls++
	return f.binding, nil
}

func (f *fakeTypingQueries) GetChannelInstallation(_ context.Context, _ db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.inst, nil
}

func typingTestUUID(b byte) pgtype.UUID {
	var raw [16]byte
	raw[15] = b
	return pgtype.UUID{Bytes: raw, Valid: true}
}

func TestTypingIndicatorAddAndClear(t *testing.T) {
	rec, srv := newEmotionAPIServer(t)
	messenger := NewRobotMessenger(srv.URL, srv.URL, srv.Client())

	instID := typingTestUUID(1)
	instRow := testInstallationRow(t, instID, "client_a")
	q := &fakeTypingQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: instID},
		inst:    instRow,
	}
	mgr := NewTypingIndicatorManager(messenger, plaintextDecrypter, q, nil)

	session := typingTestUUID(2)
	target := EmotionTarget{OpenConversationID: "cid_1", OpenMsgID: "msg_1"}
	mgr.Add(context.Background(), instRow, session, target, time.Now().UnixMilli())

	if len(rec.replies) != 1 {
		t.Fatalf("expected 1 emotion reply, got %d", len(rec.replies))
	}
	reply := rec.replies[0]
	if reply["robotCode"] != "client_a" || reply["openMsgId"] != "msg_1" || reply["openConversationId"] != "cid_1" {
		t.Fatalf("unexpected reply payload: %v", reply)
	}
	if reply["emotionType"] != float64(2) || reply["textEmotion"] == nil {
		t.Fatalf("expected text emotion payload, got %v", reply)
	}

	mgr.Clear(context.Background(), session)
	if len(rec.recalls) != 1 {
		t.Fatalf("expected 1 emotion recall, got %d", len(rec.recalls))
	}
	if rec.recalls[0]["openMsgId"] != "msg_1" {
		t.Fatalf("recall targets wrong message: %v", rec.recalls[0])
	}

	// Second clear is a state no-op: no further lookups or recalls.
	q.calls = 0
	mgr.Clear(context.Background(), session)
	if q.calls != 0 || len(rec.recalls) != 1 {
		t.Fatalf("expected cleared session to be a no-op, lookups=%d recalls=%d", q.calls, len(rec.recalls))
	}
}

func TestTypingIndicatorSkipsStaleMessages(t *testing.T) {
	rec, srv := newEmotionAPIServer(t)
	messenger := NewRobotMessenger(srv.URL, srv.URL, srv.Client())
	instRow := testInstallationRow(t, typingTestUUID(1), "client_a")
	mgr := NewTypingIndicatorManager(messenger, plaintextDecrypter, &fakeTypingQueries{}, nil)

	stale := time.Now().Add(-3 * time.Minute).UnixMilli()
	mgr.Add(context.Background(), instRow, typingTestUUID(2), EmotionTarget{OpenConversationID: "cid", OpenMsgID: "m"}, stale)

	if len(rec.replies) != 0 {
		t.Fatalf("stale message must not get an emotion, got %d", len(rec.replies))
	}
}

func TestTypingIndicatorSkipsEmptyTarget(t *testing.T) {
	rec, srv := newEmotionAPIServer(t)
	messenger := NewRobotMessenger(srv.URL, srv.URL, srv.Client())
	instRow := testInstallationRow(t, typingTestUUID(1), "client_a")
	mgr := NewTypingIndicatorManager(messenger, plaintextDecrypter, &fakeTypingQueries{}, nil)

	mgr.Add(context.Background(), instRow, typingTestUUID(2), EmotionTarget{OpenMsgID: "m"}, 0)
	mgr.Add(context.Background(), instRow, typingTestUUID(2), EmotionTarget{OpenConversationID: "cid"}, 0)

	if len(rec.replies) != 0 {
		t.Fatalf("empty target must not get an emotion, got %d", len(rec.replies))
	}
}
