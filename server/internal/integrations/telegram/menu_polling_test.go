package telegram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Pending updates are acknowledged only by the next higher polling offset.
// Cancelling on an empty queue makes each Connect attempt deterministic.
type menuPollingAPI struct {
	mu                  sync.Mutex
	pending             []Update
	offsets             []int64
	failMethod          string
	failCode            int
	failNotice          bool
	menuSends           int
	callbackAfterMenu   bool
	ackCount            int
	expireAfterFirstACK bool
	cancel              context.CancelFunc
}

func menuTextUpdate(id int64, text string) Update {
	return Update{UpdateID: id, Message: &Message{MessageID: id, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}, Text: text}}
}

func menuClickUpdate(id int64, data string) Update {
	return Update{UpdateID: id, CallbackQuery: &CallbackQuery{ID: "click", From: &User{ID: 111}, Data: data,
		Message: &Message{MessageID: 20, Date: 1, From: &User{ID: 999, IsBot: true}, Chat: Chat{ID: 111, Type: "private"}},
	}}
}

func (a *menuPollingAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
	var request struct {
		Offset int64  `json:"offset"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		panic(err)
	}
	if method == "getUpdates" {
		a.offsets = append(a.offsets, request.Offset)
		var pending []Update
		for _, u := range a.pending {
			if u.UpdateID >= request.Offset {
				pending = append(pending, u)
			}
		}
		a.pending = pending
		if len(pending) == 0 {
			a.cancel()
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": pending})
		return
	}
	if method == "answerCallbackQuery" {
		a.ackCount++
	}
	expiredACK := method == "answerCallbackQuery" && a.expireAfterFirstACK && a.ackCount > 1
	if method == a.failMethod || expiredACK || (method == "sendMessage" && a.failNotice && a.menuSends > 0) {
		code := a.failCode
		if code == 0 || expiredACK {
			code = 400
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": code, "description": "query is too old or test API failure"})
		return
	}
	if method == "sendMessage" {
		a.menuSends++
		if a.callbackAfterMenu && a.menuSends == 1 {
			a.pending = append(a.pending, menuClickUpdate(2, "menu:v1:tools:status"), menuTextUpdate(3, "ordinary message"))
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":20}}`))
		return
	}
	_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
}

func menuPollingFactory(t *testing.T, api *menuPollingAPI, authorize func(context.Context, channel.InboundMessage) error) channel.Factory {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return newTelegramFactory(ChannelDeps{APIBase: srv.URL, HTTPClient: srv.Client(), Logger: testLogger(), MenuAuthorize: authorize})
}

func connectMenuAttempt(t *testing.T, factory channel.Factory, api *menuPollingAPI, handler channel.InboundHandler) error {
	t.Helper()
	raw, err := json.Marshal(installConfig{AppID: "999", BotUsername: "my_bot", BotTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("fake")), Menu: testMenu()})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := factory(channel.Config{Type: TypeTelegram, Raw: raw, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	api.mu.Lock()
	api.cancel = cancel
	api.mu.Unlock()
	return ch.Connect(ctx)
}

func TestMenuAPIFailuresDoNotBlockPolling(t *testing.T) {
	for _, kind := range []string{"expired-ack", "business-ack", "navigation-ack", "edit", "menu-send", "denial-notice"} {
		t.Run(kind, func(t *testing.T) {
			api := &menuPollingAPI{failCode: 400}
			authorize := func(context.Context, channel.InboundMessage) error { return nil }
			switch kind {
			case "expired-ack":
				api.failMethod = "answerCallbackQuery"
				api.pending = []Update{menuClickUpdate(1, "menu:v1:tools:status"), menuTextUpdate(2, "ordinary message")}
			case "business-ack", "navigation-ack", "edit":
				api.failMethod = "answerCallbackQuery"
				data := "menu:v1:tools:status"
				if kind == "business-ack" {
					api.failCode = 503
				}
				if kind != "business-ack" {
					data = "menu:v1:root"
				}
				if kind == "edit" {
					api.failMethod = "editMessageText"
				}
				api.pending = []Update{menuTextUpdate(1, "/tools"), menuClickUpdate(2, data), menuTextUpdate(3, "ordinary message")}
			case "menu-send", "denial-notice":
				api.failMethod = "sendMessage"
				api.pending = []Update{menuTextUpdate(1, "/tools"), menuTextUpdate(2, "ordinary message")}
				if kind == "denial-notice" {
					authorize = func(context.Context, channel.InboundMessage) error { return errors.New("access denied") }
				}
			}
			factory := menuPollingFactory(t, api, authorize)
			var got []string
			handler := func(_ context.Context, msg channel.InboundMessage) error { got = append(got, msg.Text); return nil }
			if err := connectMenuAttempt(t, factory, api, handler); err != nil {
				t.Fatalf("menu API failure escaped Connect: %v", err)
			}
			if fmt.Sprint(got) != "[ordinary message]" {
				t.Fatalf("normal message not delivered: %q", got)
			}
			api.mu.Lock()
			if len(api.pending) != 0 {
				t.Errorf("updates not acknowledged; offsets=%v pending=%v", api.offsets, api.pending)
			}
			sends := api.menuSends
			api.mu.Unlock()
			// Rebuilding must not replay an acknowledged callback or resend menus.
			if err := connectMenuAttempt(t, factory, api, handler); err != nil {
				t.Fatal(err)
			}
			api.mu.Lock()
			if len(got) != 1 || api.menuSends != sends {
				t.Error("acknowledged batch replayed after channel rebuild")
			}
			api.mu.Unlock()
		})
	}
}

func TestMenuHandlerFailureRequiresFreshCommandAfterChannelRebuild(t *testing.T) {
	api := &menuPollingAPI{pending: []Update{menuTextUpdate(1, "/tools")}, callbackAfterMenu: true, expireAfterFirstACK: true, failNotice: true, failCode: 503}
	factory := menuPollingFactory(t, api, func(context.Context, channel.InboundMessage) error { return nil })
	infraErr := errors.New("queue unavailable")
	var businessCalls, ordinaryCalls int
	handler := func(_ context.Context, msg channel.InboundMessage) error {
		if strings.HasPrefix(msg.MessageID, "callback:") {
			businessCalls++
			return infraErr
		}
		ordinaryCalls++
		return nil
	}
	if err := connectMenuAttempt(t, factory, api, handler); !errors.Is(err, infraErr) {
		t.Fatalf("want handler error, got %v", err)
	}
	if err := connectMenuAttempt(t, factory, api, handler); err != nil {
		t.Fatalf("replayed expired callback blocked rebuilt channel: %v", err)
	}
	if businessCalls != 1 || ordinaryCalls != 1 {
		t.Fatalf("business calls=%d ordinary calls=%d", businessCalls, ordinaryCalls)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.pending) != 0 {
		t.Fatalf("replayed updates unacknowledged: offsets=%v", api.offsets)
	}
}
