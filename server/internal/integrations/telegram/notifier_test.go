package telegram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The response bodies below are the shapes the Bot API returns for each case,
// recorded from its documented envelope. None of these tests reach the real
// Telegram host.
const (
	notifierSuccessBody = `{"ok":true,"result":{"message_id":501,"from":{"id":111,"is_bot":true,"first_name":"Secretary","username":"secretary_bot"},"chat":{"id":4242,"first_name":"Ada","type":"private"},"date":1767225600,"text":"Brief ready"}}`
	notifier429Body     = `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
	notifier403Body     = `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	notifier500Body     = `{"ok":false,"error_code":500,"description":"Internal Server Error"}`
	notifierBotToken    = "111:secret-token"
)

// notifierInstallation is the installation row the notifier reads. The stored
// token is base64 of the plaintext because the tests decrypt with nil.
func notifierInstallation(status string) db.ChannelInstallation {
	cfg, _ := json.Marshal(map[string]string{
		"app_id":              "111",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte(notifierBotToken)),
	})
	return db.ChannelInstallation{ID: telegramTestUUID(1), ChannelType: string(TypeTelegram), Status: status, Config: cfg}
}

type fakeNotifierQueries struct {
	inst  db.ChannelInstallation
	err   error
	calls int
}

func (f *fakeNotifierQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	f.calls++
	if arg.ChannelType != string(TypeTelegram) {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return f.inst, f.err
}

// notifierServer records every request and answers with status and body.
type notifierServer struct {
	mu       sync.Mutex
	status   int
	body     string
	requests []recordedRequest
}

type recordedRequest struct {
	path string
	body map[string]any
}

func (s *notifierServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{path: r.URL.Path, body: body})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.body)
	}
}

func newNotifierFixture(t *testing.T, status int, body string) (*Notifier, *notifierServer, *fakeNotifierQueries, *httptest.Server) {
	t.Helper()
	srv := &notifierServer{status: status, body: body}
	ts := httptest.NewServer(srv.handler(t))
	t.Cleanup(ts.Close)
	q := &fakeNotifierQueries{inst: notifierInstallation("active")}
	return NewNotifier(q, nil, ts.URL, ts.Client()), srv, q, ts
}

func notifierBinding() channel.Binding {
	return channel.Binding{
		ID:             telegramTestUUID(2),
		WorkspaceID:    telegramTestUUID(3),
		MulticaUserID:  telegramTestUUID(4),
		InstallationID: telegramTestUUID(1),
		ChannelType:    TypeTelegram,
		ChannelUserID:  "4242",
	}
}

func notifierMessage() channel.Message {
	return channel.Message{
		Text: "Brief ready\nTwo decisions need you.",
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: "https://app.example/acme/office?brief=b1"},
			{Label: "Decide", CallbackData: "decide:p1"},
		},
	}
}

func TestNotifierSendsMessageWithInlineKeyboard(t *testing.T) {
	t.Parallel()
	n, srv, _, _ := newNotifierFixture(t, http.StatusOK, notifierSuccessBody)

	if err := n.Send(context.Background(), notifierBinding(), notifierMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(srv.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(srv.requests))
	}
	req := srv.requests[0]
	if want := "/bot" + notifierBotToken + "/sendMessage"; req.path != want {
		t.Fatalf("path = %q, want %q", req.path, want)
	}
	if got := req.body["chat_id"]; got != float64(4242) {
		t.Fatalf("chat_id = %v, want 4242", got)
	}
	if got := req.body["text"]; got != notifierMessage().Text {
		t.Fatalf("text = %v", got)
	}
	if _, ok := req.body["parse_mode"]; ok {
		t.Fatalf("parse_mode set on a plain-text notification: %v", req.body["parse_mode"])
	}
	markup, ok := req.body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("reply_markup missing: %v", req.body)
	}
	rows, _ := markup["inline_keyboard"].([]any)
	if len(rows) != 2 {
		t.Fatalf("inline_keyboard rows = %d, want 2 (one button per row)", len(rows))
	}
	first := rows[0].([]any)[0].(map[string]any)
	if first["url"] != "https://app.example/acme/office?brief=b1" || first["text"] != "Open the brief" {
		t.Fatalf("url button = %v", first)
	}
	second := rows[1].([]any)[0].(map[string]any)
	if second["callback_data"] != "decide:p1" {
		t.Fatalf("callback button = %v", second)
	}
}

func TestNotifierOmitsKeyboardWithoutButtons(t *testing.T) {
	t.Parallel()
	n, srv, _, _ := newNotifierFixture(t, http.StatusOK, notifierSuccessBody)

	if err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, ok := srv.requests[0].body["reply_markup"]; ok {
		t.Fatalf("reply_markup sent with no buttons: %v", srv.requests[0].body)
	}
}

func TestNotifierRateLimitCarriesRetryAfter(t *testing.T) {
	t.Parallel()
	n, _, _, _ := newNotifierFixture(t, http.StatusTooManyRequests, notifier429Body)

	err := n.Send(context.Background(), notifierBinding(), notifierMessage())
	if err == nil {
		t.Fatal("Send succeeded on a 429")
	}
	if channel.IsPermanentDeliveryError(err) {
		t.Fatal("429 marked permanent; it must be retried")
	}
	if got := channel.DeliveryRetryAfter(err); got != 7*time.Second {
		t.Fatalf("RetryAfter = %v, want 7s", got)
	}
}

func TestNotifierBlockedBotIsPermanent(t *testing.T) {
	t.Parallel()
	n, _, _, _ := newNotifierFixture(t, http.StatusForbidden, notifier403Body)

	err := n.Send(context.Background(), notifierBinding(), notifierMessage())
	if !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("403 not permanent: %v", err)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("error lost Telegram's description: %v", err)
	}
}

func TestNotifierServerErrorIsRetryable(t *testing.T) {
	t.Parallel()
	n, _, _, _ := newNotifierFixture(t, http.StatusInternalServerError, notifier500Body)

	err := n.Send(context.Background(), notifierBinding(), notifierMessage())
	if err == nil {
		t.Fatal("Send succeeded on a 500")
	}
	if channel.IsPermanentDeliveryError(err) {
		t.Fatalf("500 marked permanent: %v", err)
	}
	if channel.DeliveryRetryAfter(err) != 0 {
		t.Fatal("500 carried a retry_after")
	}
}

func TestNotifierTransportFailureDoesNotLeakToken(t *testing.T) {
	t.Parallel()
	n, _, _, ts := newNotifierFixture(t, http.StatusOK, notifierSuccessBody)
	ts.Close() // every request now fails at the transport

	err := n.Send(context.Background(), notifierBinding(), notifierMessage())
	if err == nil {
		t.Fatal("Send succeeded with the server closed")
	}
	if channel.IsPermanentDeliveryError(err) {
		t.Fatal("transport failure marked permanent")
	}
	if strings.Contains(err.Error(), notifierBotToken) {
		t.Fatalf("bot token in error: %v", err)
	}
}

func TestNotifierRefusesWithoutCallingTelegram(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		binding func(*channel.Binding)
		inst    func(*fakeNotifierQueries)
		msg     channel.Message
	}{
		{
			name:    "binding of another chat app",
			binding: func(b *channel.Binding) { b.ChannelType = "slack" },
			msg:     notifierMessage(),
		},
		{
			name:    "chat id is not a number",
			binding: func(b *channel.Binding) { b.ChannelUserID = "U0ABC" },
			msg:     notifierMessage(),
		},
		{
			name: "installation missing",
			inst: func(q *fakeNotifierQueries) { q.err = pgx.ErrNoRows },
			msg:  notifierMessage(),
		},
		{
			name: "installation paused",
			inst: func(q *fakeNotifierQueries) { q.inst = notifierInstallation("paused") },
			msg:  notifierMessage(),
		},
		{
			name: "button sets both URL and callback",
			msg: channel.Message{Text: "x", Buttons: []channel.Button{
				{Label: "both", URL: "https://a", CallbackData: "x"},
			}},
		},
		{
			name: "empty text",
			msg:  channel.Message{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, srv, q, _ := newNotifierFixture(t, http.StatusOK, notifierSuccessBody)
			if tc.inst != nil {
				tc.inst(q)
			}
			binding := notifierBinding()
			if tc.binding != nil {
				tc.binding(&binding)
			}
			err := n.Send(context.Background(), binding, tc.msg)
			if !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("want permanent error, got %v", err)
			}
			if len(srv.requests) != 0 {
				t.Fatalf("Telegram was called %d times", len(srv.requests))
			}
		})
	}
}

func TestNotifierInstallationLookupErrorIsRetryable(t *testing.T) {
	t.Parallel()
	n, _, q, _ := newNotifierFixture(t, http.StatusOK, notifierSuccessBody)
	q.err = errors.New("connection reset")

	err := n.Send(context.Background(), notifierBinding(), notifierMessage())
	if err == nil || channel.IsPermanentDeliveryError(err) {
		t.Fatalf("database failure should be retryable, got %v", err)
	}
}

func TestClassifyNotifyErrorLeavesUnknownErrorsRetryable(t *testing.T) {
	t.Parallel()
	plain := errors.New("boom")
	if got := classifyNotifyError(plain); got != plain {
		t.Fatalf("plain error rewrapped: %v", got)
	}
	if classifyNotifyError(nil) != nil {
		t.Fatal("nil error classified")
	}
	if !channel.IsPermanentDeliveryError(classifyNotifyError(&apiError{Code: 400, Description: "Bad Request: chat not found"})) {
		t.Fatal("400 not permanent")
	}
}
