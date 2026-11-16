package weixin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type memoryTypingStore struct {
	active atomic.Bool
	state  db.WeixinAccountState
}

func (s *memoryTypingStore) IsWeixinTypingActive(context.Context, db.IsWeixinTypingActiveParams) (bool, error) {
	return s.active.Load(), nil
}
func (s *memoryTypingStore) SettleWeixinTypingInputs(context.Context, db.SettleWeixinTypingInputsParams) error {
	s.active.Store(false)
	return nil
}
func (s *memoryTypingStore) GetWeixinAccountState(context.Context, db.GetWeixinAccountStateParams) (db.WeixinAccountState, error) {
	return s.state, nil
}

func receiveTyping(t *testing.T, statuses <-chan int, want int) {
	t.Helper()
	select {
	case got := <-statuses:
		if got != want {
			t.Fatalf("typing status=%d want=%d", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("missing typing status %d", want)
	}
}

func TestTypingKeepaliveCacheAndTerminalCleanup(t *testing.T) {
	for _, end := range []string{"completed", "failed", "cancelled", "settled", "disconnect", "shutdown"} {
		t.Run(end, func(t *testing.T) {
			statuses := make(chan int, 32)
			var configs atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					User    string `json:"ilink_user_id"`
					Ticket  string `json:"typing_ticket"`
					Status  int    `json:"status"`
					Context string `json:"context_token"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.User != "owner" {
					t.Error("typing sent to wrong user")
				}
				if strings.HasSuffix(r.URL.Path, "getconfig") {
					configs.Add(1)
					if body.Context != "reply-context" {
						t.Error("missing reply context")
					}
					_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
				} else {
					if body.Ticket != "ticket" {
						t.Error("missing typing ticket")
					}
					statuses <- body.Status
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			box := testBox(t)
			encrypted, _ := box.Seal([]byte("token"))
			cfg, _ := json.Marshal(installConfig{AppID: "bot", UserID: "owner", BaseURL: DefaultBaseURL, TokenEncrypted: base64.StdEncoding.EncodeToString(encrypted)})
			contextToken, _ := box.Seal([]byte("reply-context"))
			store := &memoryTypingStore{state: db.WeixinAccountState{ContextTokenEncrypted: contextToken}}
			store.active.Store(true)
			n := NewTypingNotifier(store, box, nil)
			n.client = providerClient(server)
			n.interval = 10 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n.Start(ctx)
			sessionID := dbid.NewV7()
			inst := engine.ResolvedInstallation{ID: dbid.NewV7(), Platform: db.ChannelInstallation{Config: cfg}}
			msg := channel.InboundMessage{Source: channel.Source{SenderID: "owner"}}
			n.OnIngested(ctx, inst, msg, sessionID, dbid.NewV7())
			receiveTyping(t, statuses, 1)
			receiveTyping(t, statuses, 1)
			switch end {
			case "settled":
				n.OnSettled(ctx, sessionID, engine.TypingSettlement{})
			case "disconnect":
				n.StopInstallation(ctx, inst.ID)
			case "shutdown":
				cancel()
			default:
				store.active.Store(false)
				n.Wake(sessionID)
			}
			for {
				select {
				case status := <-statuses:
					if status == 2 {
						goto cleared
					}
				case <-time.After(3 * time.Second):
					t.Fatal("terminal outcome did not cancel typing")
				}
			}
		cleared:
			n.wg.Wait()
			if end == "completed" {
				store.active.Store(true)
				n.OnIngested(ctx, inst, msg, sessionID, dbid.NewV7())
				receiveTyping(t, statuses, 1)
				store.active.Store(false)
				n.Wake(sessionID)
				n.wg.Wait()
				if configs.Load() != 1 {
					t.Fatalf("ticket not cached: requests=%d", configs.Load())
				}
			}
		})
	}
}

func TestTypingContinuesAfterTransientSendFailure(t *testing.T) {
	for _, failAt := range []int32{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			statuses := make(chan int, 32)
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "getconfig") {
					_, _ = w.Write([]byte(`{"ret":0,"typing_ticket":"ticket"}`))
					return
				}
				var body struct {
					Status int `json:"status"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Status == 1 && attempts.Add(1) == failAt {
					statuses <- -1
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				statuses <- body.Status
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			box := testBox(t)
			encrypted, _ := box.Seal([]byte("token"))
			cfg, _ := json.Marshal(installConfig{AppID: "bot", UserID: "owner", BaseURL: DefaultBaseURL, TokenEncrypted: base64.StdEncoding.EncodeToString(encrypted)})
			store := &memoryTypingStore{}
			store.active.Store(true)
			n := NewTypingNotifier(store, box, nil)
			n.client = providerClient(server)
			n.interval = 10 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); n.wait() }()
			n.Start(ctx)
			n.OnIngested(ctx, engine.ResolvedInstallation{ID: dbid.NewV7(), Platform: db.ChannelInstallation{Config: cfg}}, channel.InboundMessage{Source: channel.Source{SenderID: "owner"}}, dbid.NewV7(), dbid.NewV7())
			if failAt == 2 {
				receiveTyping(t, statuses, 1)
			}
			receiveTyping(t, statuses, -1)
			receiveTyping(t, statuses, 1)
			cancel()
			n.wait()
			for {
				select {
				case status := <-statuses:
					if status == 2 {
						return
					}
				default:
					t.Fatal("typing was not cleared after cancellation")
				}
			}
		})
	}
}

func TestTypingSkipsUnavailableTicketsAndAlreadySettledInput(t *testing.T) {
	for _, body := range []string{`{"typing_ticket":"ticket"}`, `{"ret":1,"typing_ticket":"ticket"}`, `{"ret":0}`, `{"ret":0,"typing_ticket":"ticket"}`} {
		t.Run(body, func(t *testing.T) {
			var typingCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "sendtyping") {
					typingCalls.Add(1)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			box := testBox(t)
			encrypted, _ := box.Seal([]byte("token"))
			cfg, _ := json.Marshal(installConfig{AppID: "bot", UserID: "owner", BaseURL: DefaultBaseURL, TokenEncrypted: base64.StdEncoding.EncodeToString(encrypted)})
			store := &memoryTypingStore{}
			store.active.Store(!strings.Contains(body, `"ret":0,"typing_ticket"`))
			n := NewTypingNotifier(store, box, nil)
			n.client = providerClient(server)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n.Start(ctx)
			n.OnIngested(ctx, engine.ResolvedInstallation{ID: dbid.NewV7(), Platform: db.ChannelInstallation{Config: cfg}}, channel.InboundMessage{Source: channel.Source{SenderID: "owner"}}, dbid.NewV7(), dbid.NewV7())
			n.wg.Wait()
			if typingCalls.Load() != 0 {
				t.Fatal("unavailable ticket or settled input started typing")
			}
		})
	}
}
