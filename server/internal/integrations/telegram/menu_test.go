package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestMenuPrivateCommandAndCallback(t *testing.T) {
	var sent, edited map[string]any
	var acknowledgements int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			sent = body
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":20}}`)
		case strings.HasSuffix(r.URL.Path, "/editMessageText"):
			edited = body
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(r.URL.Path, "/answerCallbackQuery"):
			acknowledgements++
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected API: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var requests []channel.InboundMessage
	denied := false
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot", api: newBotAPI(server.URL, "fake", server.Client()),
		logger: slog.Default(), menu: testMenu(),
		menuAuthorize: func(context.Context, channel.InboundMessage) error {
			if denied {
				return errors.New("revoked")
			}
			return nil
		},
		handler: func(_ context.Context, msg channel.InboundMessage) error {
			requests = append(requests, msg)
			return nil
		},
	}
	ctx := context.Background()
	if err := c.dispatch(ctx, Update{UpdateID: 1, Message: &Message{
		MessageID: 1, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}, Text: "/tools",
	}}); err != nil {
		t.Fatal(err)
	}
	if sent["text"] != "Tools" || sent["reply_markup"] == nil || len(requests) != 0 {
		t.Fatalf("command must render locally: sent=%v requests=%v", sent, requests)
	}
	q := &CallbackQuery{ID: "click-a", From: &User{ID: 111}, Data: "menu:v1:tools:search",
		Message: &Message{MessageID: 20, Date: 1, From: &User{ID: 999, IsBot: true}, Chat: Chat{ID: 111, Type: "private"}}}
	if err := c.dispatch(ctx, Update{UpdateID: 2, CallbackQuery: q}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Source.SenderID != "111" || requests[0].MessageID != "callback:click-a" || !strings.Contains(requests[0].Text, "what I want") {
		t.Fatalf("callback must route the human's fixed request: %+v", requests)
	}
	_ = c.dispatch(ctx, Update{CallbackQuery: q})
	if len(requests) != 1 || acknowledgements != 2 {
		t.Fatal("duplicate must ACK without dispatch")
	}
	q.ID = "back"
	q.Data = "menu:v1:root"
	_ = c.dispatch(ctx, Update{CallbackQuery: q})
	if edited["text"] != "Choose" {
		t.Fatalf("back: %v", edited)
	}
	denied = true
	q.ID = "revoked"
	q.Data = "menu:v1:tools"
	edited = nil
	_ = c.dispatch(ctx, Update{CallbackQuery: q})
	if edited != nil || len(requests) != 1 {
		t.Fatal("revoked identity must not navigate or route")
	}
}

func TestMenuRejectsUntrustedCallbacks(t *testing.T) {
	for _, name := range []string{"unknown", "other-user", "group", "other-bot", "unknown-message", "expired", "unavailable", "wrong-page", "future-version"} {
		t.Run(name, func(t *testing.T) {
			var ack bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
					t.Errorf("unexpected API %s", r.URL.Path)
				}
				ack = true
				_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
			}))
			defer srv.Close()
			c := &telegramChannel{botID: 999, menu: testMenu(), api: newBotAPI(srv.URL, "fake", srv.Client()),
				menuAuthorize: func(context.Context, channel.InboundMessage) error { return nil },
				handler:       func(context.Context, channel.InboundMessage) error { t.Error("untrusted callback routed"); return nil },
				menus:         map[string]*menuSession{"111:20": {owner: 111, page: "tools", expires: time.Now().Add(time.Minute)}},
			}
			q := &CallbackQuery{ID: "x", From: &User{ID: 111}, Data: "menu:v1:tools:search",
				Message: &Message{MessageID: 20, Date: 1, From: &User{ID: 999, IsBot: true}, Chat: Chat{ID: 111, Type: "private"}}}
			switch name {
			case "unknown":
				q.Data = "menu:v1:tools:trade"
			case "other-user":
				q.From.ID = 222
			case "group":
				q.Message.Chat.Type = "group"
			case "other-bot":
				q.Message.From.ID = 888
			case "unknown-message":
				q.Message.MessageID = 21
			case "expired":
				c.menus["111:20"].expires = time.Now().Add(-time.Second)
			case "unavailable":
				q.Message.Date = 0
			case "wrong-page":
				c.menus["111:20"].page = "life"
			case "future-version":
				q.Data = "menu:v2:tools:search"
			}
			if err := c.dispatch(context.Background(), Update{CallbackQuery: q}); err != nil {
				t.Fatal(err)
			}
			if !ack {
				t.Error("rejection must ACK")
			}
		})
	}
}

func TestMenuPollSubscribesToCallbacks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AllowedUpdates []string `json:"allowed_updates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Join(body.AllowedUpdates, ",") != "message,callback_query" {
			t.Errorf("subscriptions: %v", body.AllowedUpdates)
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":2,"callback_query":{"id":"q","from":{"id":111},"data":"menu:v1:root"}}]}`)
	}))
	defer srv.Close()
	updates, err := newBotAPI(srv.URL, "fake", srv.Client()).GetUpdates(context.Background(), 0)
	if err != nil || len(updates) != 1 || updates[0].CallbackQuery == nil || updates[0].CallbackQuery.From.ID != 111 {
		t.Fatalf("callback decode: %+v %v", updates, err)
	}
}

func TestReplyToMenuCallbackDoesNotQuoteCallbackID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["reply_parameters"] != nil {
			t.Error("numeric callback ID was used as a Telegram message ID")
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":99}}`)
	}))
	defer srv.Close()
	_, err := newSender(newBotAPI(srv.URL, "fake", srv.Client()), testLogger()).Send(context.Background(), channel.OutboundMessage{
		ChatID: "111", Text: "Done", ReplyTo: "callback:123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMenuFailuresDoNotLoseOrDuplicateRequests(t *testing.T) {
	for _, failure := range []string{"ack", "handler"} {
		t.Run(failure, func(t *testing.T) {
			failACK := failure == "ack"
			var routed, notices int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") && failACK {
					_, _ = io.WriteString(w, `{"ok":false,"error_code":500,"description":"test failure"}`)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/sendMessage") {
					notices++
				}
				_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":30}}`)
			}))
			defer srv.Close()
			c := &telegramChannel{botID: 999, menu: testMenu(), api: newBotAPI(srv.URL, "fake", srv.Client()),
				menuAuthorize: func(context.Context, channel.InboundMessage) error { return nil },
				menus:         map[string]*menuSession{"111:20": {owner: 111, page: "tools", expires: time.Now().Add(time.Minute)}},
				handler: func(_ context.Context, msg channel.InboundMessage) error {
					routed++
					if failure == "handler" && routed == 1 {
						return errors.New("queue unavailable")
					}
					return nil
				},
			}
			q := &CallbackQuery{ID: "123", From: &User{ID: 111}, Data: "menu:v1:tools:status",
				Message: &Message{MessageID: 20, Date: 1, From: &User{ID: 999, IsBot: true}, Chat: Chat{ID: 111, Type: "private"}}}
			if err := c.dispatch(context.Background(), Update{CallbackQuery: q}); err == nil {
				t.Fatal("failure was swallowed")
			}
			if failure == "ack" && routed != 0 {
				t.Fatal("unacknowledged request routed")
			}
			if failure == "handler" && notices != 1 {
				t.Fatal("failed submission needs a visible retry notice")
			}
			failACK = false
			if err := c.dispatch(context.Background(), Update{CallbackQuery: q}); err != nil {
				t.Fatal(err)
			}
			before := routed
			if err := c.dispatch(context.Background(), Update{CallbackQuery: q}); err != nil {
				t.Fatal(err)
			}
			if routed != before {
				t.Fatal("successful retry was submitted twice")
			}
		})
	}
}

func TestMenuScopeAndInstallationIsolation(t *testing.T) {
	for _, kind := range []string{"disabled", "another-chat", "another-bot"} {
		t.Run(kind, func(t *testing.T) {
			c := &telegramChannel{botID: 999, menu: testMenu(),
				menuAuthorize: func(context.Context, channel.InboundMessage) error {
					t.Fatal("non-menu traffic authorized")
					return nil
				},
				handler: func(context.Context, channel.InboundMessage) error { return nil },
			}
			m := &Message{MessageID: 1, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}, Text: "/tools"}
			switch kind {
			case "disabled", "another-bot":
				c.menu = nil
			case "another-chat":
				m.From.ID = 222
				m.Chat.ID = 222
			}
			if err := c.dispatch(context.Background(), Update{Message: m}); err != nil {
				t.Fatal(err)
			}
			if len(c.menus) != 0 {
				t.Fatal("personal menu leaked outside installation/chat scope")
			}
		})
	}
}

func TestMenuDoesNotConsumeMediaCaptions(t *testing.T) {
	var routed, menuSends int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		menuSends++
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":20}}`)
	}))
	defer srv.Close()
	c := &telegramChannel{botID: 999, menu: testMenu(), acceptsMedia: true, api: newBotAPI(srv.URL, "fake", srv.Client()),
		menuAuthorize: func(context.Context, channel.InboundMessage) error { return nil },
		handler:       func(context.Context, channel.InboundMessage) error { routed++; return nil },
	}
	if err := c.dispatch(context.Background(), Update{Message: &Message{
		MessageID: 1, From: &User{ID: 111}, Chat: Chat{ID: 111, Type: "private"}, Caption: "/tools", Photo: []PhotoSize{{FileID: "fake"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if routed != 1 || menuSends != 0 {
		t.Fatal("a media caption was consumed as a menu command")
	}
}
