package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The response bodies below are the shapes the Open Platform returns for each
// case. None of these tests reach the real Lark host: the API client points at
// an httptest server.
const (
	notifierSendOKBody         = `{"code":0,"msg":"success","data":{"message_id":"om_brief_1","msg_type":"text","create_time":"1767225600000","chat_id":"oc_dm_1","sender":{"id":"cli_app_xx","id_type":"app_id","sender_type":"app"}}}`
	notifierNoAvailabilityBody = `{"code":230013,"msg":"Bot has NO availability to this user."}`
	notifierRateLimitedBody    = `{"code":99991400,"msg":"request trigger frequency limit"}`
	notifierServerErrorBody    = `{"code":1,"msg":"internal error"}`
	notifierMemberOpenID       = "ou_member_1"
	notifierInstallationID     = "11111111-1111-4111-8111-111111111111"
)

// notifierInstallationRow is the channel_installation row the notifier reads.
func notifierInstallationRow(t *testing.T, status string) db.ChannelInstallation {
	t.Helper()
	cfg, err := json.Marshal(feishuInstallConfig{AppID: "cli_app_xx", BotOpenID: "ou_bot_1"})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return db.ChannelInstallation{
		ID:          uuidFromString(t, notifierInstallationID),
		ChannelType: string(channel.TypeFeishu),
		Status:      status,
		Config:      cfg,
	}
}

// fakeNotifierQueries answers the one installation lookup the notifier makes. It
// records the call so a test can see whether the notifier got that far.
type fakeNotifierQueries struct {
	row   db.ChannelInstallation
	err   error
	calls int
}

func (f *fakeNotifierQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	f.calls++
	if arg.ChannelType != string(channel.TypeFeishu) || arg.ID != f.row.ID {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return f.row, f.err
}

// fakeNotifierCreds stands in for the installation service's secret decryption.
type fakeNotifierCreds struct{}

func (fakeNotifierCreds) DecryptAppSecret(Installation) (string, error) {
	return "secret_xx", nil
}

// notifierSendCall is one message the notifier posted to the fake.
type notifierSendCall struct {
	query url.Values
	body  map[string]string
	auth  string
}

// notifierFake is the Open Platform as the notifier sees it: a token endpoint
// and one message endpoint that answers with a recorded status, headers and body.
type notifierFake struct {
	*larkFakeServer
	mu    sync.Mutex
	calls []notifierSendCall
}

func newNotifierFake(t *testing.T, status int, header map[string]string, reply string) *notifierFake {
	t.Helper()
	f := &notifierFake{larkFakeServer: newLarkFake(t)}
	f.stubToken("t-brief", 7200)
	f.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("send: decode body: %v", err)
		}
		f.mu.Lock()
		f.calls = append(f.calls, notifierSendCall{query: r.URL.Query(), body: body, auth: r.Header.Get("Authorization")})
		f.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	})
	return f
}

func (f *notifierFake) sent() []notifierSendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]notifierSendCall(nil), f.calls...)
}

// newNotifierUnderTest wires the notifier to the fake through the real API client.
func newNotifierUnderTest(t *testing.T, fake *notifierFake, row db.ChannelInstallation) (*Notifier, *fakeNotifierQueries) {
	t.Helper()
	q := &fakeNotifierQueries{row: row}
	api := newTestClient(fake.larkFakeServer, time.Now)
	return NewNotifier(q, fakeNotifierCreds{}, api), q
}

func notifierBinding(t *testing.T) channel.Binding {
	t.Helper()
	return channel.Binding{
		ChannelType:    channel.TypeFeishu,
		ChannelUserID:  notifierMemberOpenID,
		InstallationID: uuidFromString(t, notifierInstallationID),
	}
}

// briefMessageWithDecision is the push for one decision: the text, the brief
// link, and the buttons renderBriefPush lays out (Approve and Reject on one row,
// Snooze and Revise on the next).
func briefMessageWithDecision() channel.Message {
	const briefURL = "https://app.example/acme/company?brief=b1"
	const decisionURL = "https://app.example/acme/company?decision=p1"
	return channel.Message{
		Text: "Weekly plan is ready.\n\n1 decision needs you:\n- Hire a designer\n\nBrief: " + briefURL,
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: briefURL},
			{Label: "Approve: Hire a designer", CallbackData: "tok-approve", LinkURL: decisionURL},
			{Label: "Reject: Hire a designer", CallbackData: "tok-reject", LinkURL: decisionURL, SameRow: true},
			{Label: "Snooze 7 days: Hire a designer", CallbackData: "tok-snooze", LinkURL: decisionURL},
			{Label: "Revise: Hire a designer", URL: decisionURL, SameRow: true},
		},
	}
}

// cardElements decodes an interactive card's body elements.
func cardElements(t *testing.T, content string) []map[string]any {
	t.Helper()
	var card struct {
		Schema string `json:"schema"`
		Body   struct {
			Elements []map[string]any `json:"elements"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(content), &card); err != nil {
		t.Fatalf("card is not JSON: %v\n%s", err, content)
	}
	if card.Schema != "2.0" {
		t.Fatalf("card schema = %q, want 2.0", card.Schema)
	}
	return card.Body.Elements
}

// cardButtons walks the card's rows and returns the buttons in order, with the
// row each one sits on.
func cardButtons(t *testing.T, elements []map[string]any) (rows [][]map[string]any) {
	t.Helper()
	for _, el := range elements {
		if el["tag"] != "column_set" {
			continue
		}
		var row []map[string]any
		for _, col := range el["columns"].([]any) {
			for _, child := range col.(map[string]any)["elements"].([]any) {
				row = append(row, child.(map[string]any))
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func TestNotifierSendsPlainTextWhenThereAreNoButtons(t *testing.T) {
	fake := newNotifierFake(t, http.StatusOK, nil, notifierSendOKBody)
	n, _ := newNotifierUnderTest(t, fake, notifierInstallationRow(t, "active"))

	err := n.Send(context.Background(), notifierBinding(t), channel.Message{Text: "Brief ready. Brief: https://app.example/acme/company?brief=b1"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	calls := fake.sent()
	if len(calls) != 1 {
		t.Fatalf("sent %d messages, want 1", len(calls))
	}
	call := calls[0]
	if got := call.query.Get("receive_id_type"); got != "open_id" {
		t.Fatalf("receive_id_type = %q, want open_id", got)
	}
	if call.body["receive_id"] != notifierMemberOpenID || call.body["msg_type"] != "text" {
		t.Fatalf("body = %v, want the member open id and msg_type text", call.body)
	}
	var text map[string]string
	if err := json.Unmarshal([]byte(call.body["content"]), &text); err != nil || !strings.HasPrefix(text["text"], "Brief ready.") {
		t.Fatalf("text content = %q (%v), want the brief text", call.body["content"], err)
	}
	if call.auth != "Bearer t-brief" {
		t.Fatalf("Authorization = %q, want the tenant token", call.auth)
	}
}

func TestNotifierSendsACardWithDecisionButtons(t *testing.T) {
	fake := newNotifierFake(t, http.StatusOK, nil, notifierSendOKBody)
	n, _ := newNotifierUnderTest(t, fake, notifierInstallationRow(t, "active"))

	if err := n.Send(context.Background(), notifierBinding(t), briefMessageWithDecision()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	calls := fake.sent()
	if len(calls) != 1 || calls[0].body["msg_type"] != "interactive" {
		t.Fatalf("calls = %+v, want one interactive message", calls)
	}
	elements := cardElements(t, calls[0].body["content"])
	if elements[0]["tag"] != "markdown" {
		t.Fatalf("first element = %v, want the brief text as markdown", elements[0]["tag"])
	}
	rows := cardButtons(t, elements)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (brief link; approve and reject; snooze and revise)", len(rows))
	}
	if len(rows[0]) != 1 || len(rows[1]) != 2 || len(rows[2]) != 2 {
		t.Fatalf("row sizes = %d, %d, %d; want 1, 2, 2", len(rows[0]), len(rows[1]), len(rows[2]))
	}
	if got := behaviorOf(t, rows[0][0]); got["type"] != "open_url" {
		t.Fatalf("brief link behavior = %v, want open_url", got)
	}
	wantTokens := map[string]string{"Approve": "tok-approve", "Reject": "tok-reject", "Snooze": "tok-snooze"}
	callbacks := 0
	for _, row := range rows {
		for _, button := range row {
			behavior := behaviorOf(t, button)
			if behavior["type"] == "callback" {
				callbacks++
				value := behavior["value"].(map[string]any)
				label := button["text"].(map[string]any)["content"].(string)
				ok := false
				for prefix, token := range wantTokens {
					if strings.HasPrefix(label, prefix) && value["token"] == token {
						ok = true
					}
				}
				if !ok {
					t.Fatalf("callback button %q carries %v, not its signed token", label, value)
				}
			}
		}
	}
	if callbacks != 3 {
		t.Fatalf("callback buttons = %d, want 3 (approve, reject, snooze)", callbacks)
	}
	revise := behaviorOf(t, rows[2][1])
	if revise["type"] != "open_url" || revise["default_url"] != "https://app.example/acme/company?decision=p1" {
		t.Fatalf("revise behavior = %v, want the decision link", revise)
	}
}

// behaviorOf returns the first behavior of a button.
func behaviorOf(t *testing.T, button map[string]any) map[string]any {
	t.Helper()
	behaviors, ok := button["behaviors"].([]any)
	if !ok || len(behaviors) != 1 {
		t.Fatalf("button %v has no single behavior", button["text"])
	}
	return behaviors[0].(map[string]any)
}

func TestBriefCardGivesCallbackButtonsNoLinkFallback(t *testing.T) {
	// Lark takes callbacks over the long connection, so a callback button with a
	// LinkURL still renders as a callback, and the link is not read.
	card, err := briefCard(channel.Message{
		Text:    "Decide",
		Buttons: []channel.Button{{Label: "Approve", CallbackData: "tok", LinkURL: "https://app.example/x"}},
	})
	if err != nil {
		t.Fatalf("briefCard: %v", err)
	}
	if strings.Contains(card, "open_url") || strings.Contains(card, "app.example/x") {
		t.Fatalf("callback button carries a link fallback: %s", card)
	}
	if !strings.Contains(card, `"type":"callback"`) {
		t.Fatalf("callback button lost its callback: %s", card)
	}
}

func TestBriefCardRefusesAButtonWithNeitherOrBothAddresses(t *testing.T) {
	for name, button := range map[string]channel.Button{
		"neither": {Label: "Empty"},
		"both":    {Label: "Both", URL: "https://app.example/x", CallbackData: "tok"},
	} {
		if _, err := briefCard(channel.Message{Text: "x", Buttons: []channel.Button{button}}); err == nil {
			t.Fatalf("%s: briefCard accepted a button that must set exactly one address", name)
		}
	}
}

func TestNotifierSendErrorsClassifyForRetry(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		header     map[string]string
		reply      string
		permanent  bool
		retryAfter time.Duration
	}{
		{name: "no availability in a 200 envelope", status: http.StatusOK, reply: `{"code":230013,"msg":"Bot has NO availability to this user."}`, permanent: true},
		{name: "no availability in a 400 reply", status: http.StatusBadRequest, reply: notifierNoAvailabilityBody, permanent: true},
		{name: "rate limit with Retry-After", status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "30"}, reply: notifierRateLimitedBody, retryAfter: 30 * time.Second},
		{name: "rate limit code without a header", status: http.StatusBadRequest, reply: notifierRateLimitedBody},
		{name: "server error is retryable", status: http.StatusInternalServerError, reply: notifierServerErrorBody},
		{name: "unknown business code is retryable", status: http.StatusOK, reply: `{"code":99991672,"msg":"app scope not enabled"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newNotifierFake(t, tc.status, tc.header, tc.reply)
			n, _ := newNotifierUnderTest(t, fake, notifierInstallationRow(t, "active"))

			err := n.Send(context.Background(), notifierBinding(t), channel.Message{Text: "Brief ready."})
			if err == nil {
				t.Fatalf("Send succeeded on a failed reply")
			}
			if got := channel.IsPermanentDeliveryError(err); got != tc.permanent {
				t.Fatalf("permanent = %v, want %v (err %v)", got, tc.permanent, err)
			}
			if got := channel.DeliveryRetryAfter(err); got != tc.retryAfter {
				t.Fatalf("retry after = %v, want %v", got, tc.retryAfter)
			}
		})
	}
}

func TestNotifierSuccessIsNotAnError(t *testing.T) {
	fake := newNotifierFake(t, http.StatusOK, nil, notifierSendOKBody)
	n, _ := newNotifierUnderTest(t, fake, notifierInstallationRow(t, "active"))
	if err := n.Send(context.Background(), notifierBinding(t), channel.Message{Text: "Brief ready."}); err != nil {
		t.Fatalf("code 0 reply = %v, want nil", err)
	}
}

func TestNotifierRefusesBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name    string
		row     func(*testing.T) db.ChannelInstallation
		binding func(*testing.T) channel.Binding
		msg     channel.Message
	}{
		{
			name:    "another chat app's binding",
			row:     func(t *testing.T) db.ChannelInstallation { return notifierInstallationRow(t, "active") },
			binding: func(t *testing.T) channel.Binding { b := notifierBinding(t); b.ChannelType = "telegram"; return b },
			msg:     channel.Message{Text: "x"},
		},
		{
			name:    "binding without an open id",
			row:     func(t *testing.T) db.ChannelInstallation { return notifierInstallationRow(t, "active") },
			binding: func(t *testing.T) channel.Binding { b := notifierBinding(t); b.ChannelUserID = ""; return b },
			msg:     channel.Message{Text: "x"},
		},
		{
			name:    "revoked installation",
			row:     func(t *testing.T) db.ChannelInstallation { return notifierInstallationRow(t, "revoked") },
			binding: notifierBinding,
			msg:     channel.Message{Text: "x"},
		},
		{
			name:    "empty message",
			row:     func(t *testing.T) db.ChannelInstallation { return notifierInstallationRow(t, "active") },
			binding: notifierBinding,
			msg:     channel.Message{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newNotifierFake(t, http.StatusOK, nil, notifierSendOKBody)
			n, _ := newNotifierUnderTest(t, fake, tc.row(t))

			err := n.Send(context.Background(), tc.binding(t), tc.msg)
			if !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("err = %v, want a permanent refusal", err)
			}
			if got := len(fake.sent()); got != 0 {
				t.Fatalf("sent %d messages for a refused binding", got)
			}
		})
	}
}

func TestNotifierMissingInstallationIsPermanent(t *testing.T) {
	fake := newNotifierFake(t, http.StatusOK, nil, notifierSendOKBody)
	row := notifierInstallationRow(t, "active")
	n, q := newNotifierUnderTest(t, fake, row)
	b := notifierBinding(t)
	b.InstallationID = uuidFromString(t, "22222222-2222-4222-8222-222222222222")

	err := n.Send(context.Background(), b, channel.Message{Text: "x"})
	if !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("err = %v, want a permanent refusal for an unknown installation", err)
	}
	if q.calls != 1 || len(fake.sent()) != 0 {
		t.Fatalf("queries = %d, sent = %d; want one lookup and no message", q.calls, len(fake.sent()))
	}
}
