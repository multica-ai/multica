package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type memoryAccount struct {
	state       db.WeixinAccountState
	cursorSaved bool
	expired     bool
	cancel      context.CancelFunc
}

func (m *memoryAccount) GetWeixinAccountState(context.Context, db.GetWeixinAccountStateParams) (db.WeixinAccountState, error) {
	return m.state, nil
}
func (m *memoryAccount) SaveWeixinContext(_ context.Context, p db.SaveWeixinContextParams) (int64, error) {
	m.state.ContextTokenEncrypted = p.ContextTokenEncrypted
	return 1, nil
}
func (m *memoryAccount) SaveWeixinCursor(_ context.Context, p db.SaveWeixinCursorParams) (int64, error) {
	m.cursorSaved = true
	m.state.GetUpdatesBuf = p.GetUpdatesBuf
	m.cancel()
	return 1, nil
}
func (m *memoryAccount) ExpireWeixinInstallation(context.Context, db.ExpireWeixinInstallationParams) (int64, error) {
	m.expired = true
	return 1, nil
}

func TestInboundCursorCommitsAfterAcceptedBatch(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "persistence-failed"}[fail], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &memoryAccount{cancel: cancel, state: db.WeixinAccountState{GetUpdatesBuf: "previous"}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "getupdates") {
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["get_updates_buf"] != "previous" {
						t.Error("lost saved cursor")
					}
					_, _ = w.Write([]byte(`{"ret":0,"get_updates_buf":"next","msgs":[{"message_id":123,"from_user_id":"owner","message_type":1,"context_token":"reply-token","item_list":[{"type":1,"text_item":{"text":"hello"}}]}]}`))
				} else {
					_, _ = w.Write([]byte(`{"ret":0}`))
				}
			}))
			defer server.Close()
			box := testBox(t)
			calls := 0
			c := &weixinChannel{creds: credentials{installConfig: installConfig{AppID: "bot", UserID: "owner"}}, api: newAPI(server.URL, "token", server.Client()), q: store, box: box, logger: slog.Default(), handler: func(_ context.Context, msg channel.InboundMessage) error {
				calls++
				if store.cursorSaved {
					t.Error("cursor moved before engine acceptance")
				}
				if strings.Contains(string(msg.Raw), "reply-token") {
					t.Error("reply credential leaked into raw message")
				}
				token, err := box.Open(store.state.ContextTokenEncrypted)
				if err != nil || string(token) != "reply-token" {
					t.Error("context not durable before dispatch")
				}
				if fail {
					return errors.New("database unavailable")
				}
				return nil
			}}
			err := c.Connect(ctx)
			if fail && err == nil || !fail && err != nil {
				t.Fatalf("connect: %v", err)
			}
			if calls != 1 || store.cursorSaved == fail {
				t.Fatalf("calls=%d cursorSaved=%v", calls, store.cursorSaved)
			}
		})
	}
}

func TestInboundRejectsOtherUsersGroupsAndBotEchoes(t *testing.T) {
	creds := credentials{installConfig: installConfig{AppID: "bot", UserID: "owner"}}
	msg := textMessage("bot", "context", "", "hello")
	msg.ID, msg.From, msg.Type = "id", "owner", 1
	if in, ok := normalizeMessage(creds, msg); !ok || in.Text != "hello" {
		t.Fatal("owner message rejected")
	}
	for _, state := range []int{0, 1, 99} {
		partial := msg
		partial.State = &state
		if _, ok := normalizeMessage(creds, partial); ok {
			t.Fatal("accepted an incomplete message before its final update")
		}
	}
	for _, mutate := range []func(*message){func(m *message) { m.From = "stranger" }, func(m *message) { m.Type = 2 }, func(m *message) { m.GroupID = "group" }, func(m *message) { m.To = "other-bot" }, func(m *message) { m.ID = "" }} {
		copy := msg
		mutate(&copy)
		if _, ok := normalizeMessage(creds, copy); ok {
			t.Fatal("accepted foreign or unsupported source")
		}
	}
}
