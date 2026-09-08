package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type pollCall struct {
	baseURL string
	token   string
	cursor  string
	timeout time.Duration
}

type fakeUpdatesClient struct {
	calls []pollCall
	fn    func(context.Context, int, pollCall) (Updates, error)
}

func (f *fakeUpdatesClient) GetUpdates(ctx context.Context, baseURL, token, cursor string, timeout time.Duration) (Updates, error) {
	call := pollCall{baseURL: baseURL, token: token, cursor: cursor, timeout: timeout}
	index := len(f.calls)
	f.calls = append(f.calls, call)
	return f.fn(ctx, index, call)
}

type fakeCursorStore struct {
	initial  string
	loadErr  error
	saveErr  error
	loadKeys []string
	saves    []struct {
		key    string
		cursor string
	}
}

func (f *fakeCursorStore) LoadCursor(_ context.Context, key string) (string, error) {
	f.loadKeys = append(f.loadKeys, key)
	return f.initial, f.loadErr
}

func (f *fakeCursorStore) SaveCursor(_ context.Context, key, cursor string) error {
	f.saves = append(f.saves, struct {
		key    string
		cursor string
	}{key: key, cursor: cursor})
	return f.saveErr
}

func TestReceiverSavesCursorAfterCompleteBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &fakeUpdatesClient{}
	client.fn = func(ctx context.Context, index int, call pollCall) (Updates, error) {
		if index > 0 {
			<-ctx.Done()
			return Updates{}, ctx.Err()
		}
		return Updates{
			Cursor: "cursor-2",
			Messages: []Message{
				testInboundMessage(1, "first"),
				// Bot echoes are intentionally ignored but do not prevent the
				// successfully handled batch cursor from advancing.
				func() Message {
					m := testInboundMessage(2, "echo")
					m.MessageType = messageTypeBot
					return m
				}(),
				testInboundMessage(3, "second"),
			},
		}, nil
	}
	cursors := &fakeCursorStore{initial: "cursor-1"}
	var handled []string
	receiver := mustReceiver(t, ReceiverConfig{
		Client:  client,
		Cursors: cursors,
		Handler: func(_ context.Context, msg channel.InboundMessage) error {
			handled = append(handled, msg.Text)
			if len(handled) == 2 {
				cancel()
			}
			return nil
		},
	})

	if err := receiver.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fmt.Sprint(handled) != "[first second]" {
		t.Fatalf("handled = %#v", handled)
	}
	if len(cursors.saves) != 1 || cursors.saves[0].cursor != "cursor-2" {
		t.Fatalf("cursor saves = %#v", cursors.saves)
	}
	if len(client.calls) != 1 || client.calls[0].cursor != "cursor-1" {
		t.Fatalf("poll calls = %#v", client.calls)
	}
}

func TestReceiverDoesNotAdvanceCursorWhenHandlerFails(t *testing.T) {
	wantErr := errors.New("database unavailable")
	client := &fakeUpdatesClient{fn: func(context.Context, int, pollCall) (Updates, error) {
		return Updates{Cursor: "cursor-2", Messages: []Message{testInboundMessage(1, "first")}}, nil
	}}
	cursors := &fakeCursorStore{initial: "cursor-1"}
	receiver := mustReceiver(t, ReceiverConfig{
		Client:  client,
		Cursors: cursors,
		Handler: func(context.Context, channel.InboundMessage) error { return wantErr },
	})

	err := receiver.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v", err)
	}
	if len(cursors.saves) != 0 {
		t.Fatalf("cursor advanced after failed handler: %#v", cursors.saves)
	}
}

func TestReceiverResumesAndClampsSuggestedTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &fakeUpdatesClient{}
	client.fn = func(ctx context.Context, index int, call pollCall) (Updates, error) {
		if index == 0 {
			return Updates{Cursor: "next", LongPollMillis: 1000}, nil
		}
		cancel()
		return Updates{}, context.Canceled
	}
	cursors := &fakeCursorStore{initial: "resume"}
	receiver := mustReceiver(t, ReceiverConfig{
		Client:  client,
		Cursors: cursors,
		Handler: func(context.Context, channel.InboundMessage) error { return nil },
	})

	if err := receiver.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(client.calls) != 2 {
		t.Fatalf("poll calls = %#v", client.calls)
	}
	if client.calls[0].cursor != "resume" || client.calls[1].cursor != "next" {
		t.Fatalf("poll cursors = %#v", client.calls)
	}
	if client.calls[1].timeout != minPollTimeout {
		t.Fatalf("second timeout = %s, want %s", client.calls[1].timeout, minPollTimeout)
	}
	if len(cursors.saves) != 1 || cursors.saves[0].cursor != "next" {
		t.Fatalf("cursor saves = %#v", cursors.saves)
	}
}

func TestReceiverPropagatesReauthorizationAndStoreErrors(t *testing.T) {
	t.Run("load", func(t *testing.T) {
		wantErr := errors.New("load failed")
		receiver := mustReceiver(t, ReceiverConfig{
			Client:  &fakeUpdatesClient{fn: func(context.Context, int, pollCall) (Updates, error) { return Updates{}, nil }},
			Cursors: &fakeCursorStore{loadErr: wantErr},
			Handler: func(context.Context, channel.InboundMessage) error { return nil },
		})
		if err := receiver.Run(context.Background()); !errors.Is(err, wantErr) {
			t.Fatalf("Run error = %v", err)
		}
	})

	t.Run("save", func(t *testing.T) {
		wantErr := errors.New("save failed")
		receiver := mustReceiver(t, ReceiverConfig{
			Client: &fakeUpdatesClient{fn: func(context.Context, int, pollCall) (Updates, error) {
				return Updates{Cursor: "next"}, nil
			}},
			Cursors: &fakeCursorStore{saveErr: wantErr},
			Handler: func(context.Context, channel.InboundMessage) error { return nil },
		})
		if err := receiver.Run(context.Background()); !errors.Is(err, wantErr) {
			t.Fatalf("Run error = %v", err)
		}
	})

	t.Run("stale token", func(t *testing.T) {
		receiver := mustReceiver(t, ReceiverConfig{
			Client: &fakeUpdatesClient{fn: func(context.Context, int, pollCall) (Updates, error) {
				return Updates{}, ErrReauthorizationRequired
			}},
			Cursors: &fakeCursorStore{},
			Handler: func(context.Context, channel.InboundMessage) error { return nil },
		})
		if err := receiver.Run(context.Background()); !errors.Is(err, ErrReauthorizationRequired) {
			t.Fatalf("Run error = %v", err)
		}
	})
}

func TestNewReceiverValidatesDurableDependencies(t *testing.T) {
	valid := ReceiverConfig{
		InstallationKey: "installation-id",
		BotID:           "bot@im.bot",
		BotToken:        "token",
		BaseURL:         defaultBaseURL,
		Client: &fakeUpdatesClient{fn: func(context.Context, int, pollCall) (Updates, error) {
			return Updates{}, nil
		}},
		Cursors: &fakeCursorStore{},
		Handler: func(context.Context, channel.InboundMessage) error { return nil },
	}
	tests := []struct {
		name   string
		mutate func(*ReceiverConfig)
	}{
		{"installation key", func(c *ReceiverConfig) { c.InstallationKey = "" }},
		{"bot id", func(c *ReceiverConfig) { c.BotID = "" }},
		{"bot token", func(c *ReceiverConfig) { c.BotToken = "" }},
		{"base URL", func(c *ReceiverConfig) { c.BaseURL = "" }},
		{"client", func(c *ReceiverConfig) { c.Client = nil }},
		{"cursor store", func(c *ReceiverConfig) { c.Cursors = nil }},
		{"handler", func(c *ReceiverConfig) { c.Handler = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			if _, err := NewReceiver(cfg); err == nil {
				t.Fatal("NewReceiver accepted invalid config")
			}
		})
	}
}

func mustReceiver(t *testing.T, cfg ReceiverConfig) *Receiver {
	t.Helper()
	if cfg.InstallationKey == "" {
		cfg.InstallationKey = "installation-id"
	}
	if cfg.BotID == "" {
		cfg.BotID = "bot@im.bot"
	}
	if cfg.BotToken == "" {
		cfg.BotToken = "token"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	receiver, err := NewReceiver(cfg)
	if err != nil {
		t.Fatalf("NewReceiver: %v", err)
	}
	return receiver
}

func testInboundMessage(id int, text string) Message {
	return Message{
		MessageID:    json.RawMessage(fmt.Sprintf("%d", id)),
		FromUserID:   "user@im.wechat",
		ToUserID:     "bot@im.bot",
		MessageType:  messageTypeUser,
		MessageState: messageStateFinish,
		Items:        []MessageItem{{Type: messageItemTypeText, Text: &TextItem{Text: text}}},
	}
}
