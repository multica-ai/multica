package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// The response bodies below are the shapes the Web API returns for each case,
// recorded from its documented envelope. None of these tests reach the real
// Slack host.
const (
	slackNotifierOKBody          = `{"ok":true,"channel":"D0BRIEF","ts":"1767225600.000100","message":{"type":"message","text":"Brief ready","user":"UBOT","ts":"1767225600.000100"}}`
	slackNotifierChannelNotFound = `{"ok":false,"error":"channel_not_found"}`
	slackNotifierInternalError   = `{"ok":false,"error":"internal_error"}`
	slackNotifierRateLimited     = `{"ok":false,"error":"ratelimited"}`
	slackNotifierBotToken        = "xoxb-notifier-test-token"
	slackNotifierMemberUser      = "UMEMBER1"
)

// slackNotifierInstallation is the installation row the notifier reads. The
// stored token is base64 of the plaintext because the tests decrypt with nil.
func slackNotifierInstallation(status string) db.ChannelInstallation {
	cfg, _ := json.Marshal(map[string]string{
		"app_id":              "A1",
		"team_id":             "T1",
		"bot_user_id":         "UBOT",
		"bot_token_encrypted": base64.StdEncoding.EncodeToString([]byte(slackNotifierBotToken)),
	})
	return db.ChannelInstallation{ID: uid(1), ChannelType: string(TypeSlack), Status: status, Config: cfg}
}

type fakeSlackNotifierQueries struct {
	inst  db.ChannelInstallation
	err   error
	calls int
}

func (f *fakeSlackNotifierQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	f.calls++
	if arg.ChannelType != string(TypeSlack) {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return f.inst, f.err
}

// slackNotifierRequest is one call the notifier made: the Web API method and
// the form fields, which carry the bot token as "token".
type slackNotifierRequest struct {
	method string
	form   map[string]string
}

// slackNotifierServer records every Web API call and answers with status, body
// and any extra headers.
type slackNotifierServer struct {
	mu       sync.Mutex
	status   int
	body     string
	header   http.Header
	requests []slackNotifierRequest
}

func (s *slackNotifierServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		s.mu.Lock()
		s.requests = append(s.requests, slackNotifierRequest{
			method: strings.TrimPrefix(r.URL.Path, "/"),
			form:   form,
		})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		for k, v := range s.header {
			w.Header()[k] = v
		}
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.body)
	}
}

func newSlackNotifierFixture(t *testing.T, status int, body string, header http.Header) (*Notifier, *slackNotifierServer, *fakeSlackNotifierQueries) {
	t.Helper()
	srv := &slackNotifierServer{status: status, body: body, header: header}
	ts := httptest.NewServer(srv.handler(t))
	t.Cleanup(ts.Close)
	q := &fakeSlackNotifierQueries{inst: slackNotifierInstallation("active")}
	return NewNotifier(q, nil, ts.URL, ts.Client()), srv, q
}

func slackNotifierBinding() channel.Binding {
	return channel.Binding{
		ID:             uid(2),
		WorkspaceID:    uid(3),
		MulticaUserID:  uid(4),
		InstallationID: uid(1),
		ChannelType:    TypeSlack,
		ChannelUserID:  slackNotifierMemberUser,
	}
}

// slackNotifierMessage is a brief push with two decisions' worth of buttons in
// the shape renderBriefPush builds: the brief link, then Approve and Reject on
// one row, Snooze and Revise on the next.
func slackNotifierMessage() channel.Message {
	decision := "https://app.example/acme/company?decision=p1"
	return channel.Message{
		Text: "Hire a designer for the launch\n1 decision needs you:\n- Hire a designer " + decision,
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: "https://app.example/acme/company?brief=b1"},
			{Label: "Approve: Hire a designer", CallbackData: "tok-approve", LinkURL: decision},
			{Label: "Reject: Hire a designer", CallbackData: "tok-reject", LinkURL: decision, SameRow: true},
			{Label: "Snooze 7 days: Hire a designer", CallbackData: "tok-snooze", LinkURL: decision},
			{Label: "Revise: Hire a designer", URL: decision, SameRow: true},
		},
	}
}

// blocksOf decodes the blocks parameter the notifier sent.
func blocksOf(t *testing.T, req slackNotifierRequest) []map[string]any {
	t.Helper()
	raw, ok := req.form["blocks"]
	if !ok {
		t.Fatalf("blocks not sent: %v", req.form)
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
		t.Fatalf("blocks is not JSON: %v", err)
	}
	return blocks
}

// elementsOf returns the button elements of an actions block.
func elementsOf(t *testing.T, block map[string]any) []map[string]any {
	t.Helper()
	raw, _ := block["elements"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		out = append(out, e.(map[string]any))
	}
	return out
}

func TestSlackNotifierPostsBriefToTheMembersDirectMessage(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)

	if err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(srv.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(srv.requests))
	}
	req := srv.requests[0]
	if req.method != "chat.postMessage" {
		t.Fatalf("method = %q, want chat.postMessage", req.method)
	}
	if req.form["token"] != slackNotifierBotToken {
		t.Fatalf("token = %q, want the installation's bot token", req.form["token"])
	}
	if got := req.form["channel"]; got != slackNotifierMemberUser {
		t.Fatalf("channel = %q, want the member's Slack user id %q", got, slackNotifierMemberUser)
	}
	if !strings.HasPrefix(req.form["text"], "Hire a designer for the launch") {
		t.Fatalf("fallback text = %q, want the headline first", req.form["text"])
	}

	blocks := blocksOf(t, req)
	// One section, then one actions block per row: [Open the brief], [Approve, Reject],
	// [Snooze, Revise].
	if len(blocks) != 4 {
		t.Fatalf("blocks = %d, want 4 (section and three button rows): %v", len(blocks), blocks)
	}
	if blocks[0]["type"] != "section" {
		t.Fatalf("first block = %v, want the section", blocks[0]["type"])
	}
	wantRows := []int{1, 2, 2}
	for i, want := range wantRows {
		if blocks[i+1]["type"] != "actions" {
			t.Fatalf("block %d type = %v, want actions", i+1, blocks[i+1]["type"])
		}
		if got := len(elementsOf(t, blocks[i+1])); got != want {
			t.Fatalf("row %d has %d buttons, want %d", i, got, want)
		}
	}

	open := elementsOf(t, blocks[1])[0]
	if open["url"] != "https://app.example/acme/company?brief=b1" || open["value"] != nil {
		t.Fatalf("Open the brief = %v, want a link with no value", open)
	}

	approve := elementsOf(t, blocks[2])[0]
	reject := elementsOf(t, blocks[2])[1]
	if approve["value"] != "tok-approve" || reject["value"] != "tok-reject" {
		t.Fatalf("decision buttons carry the signed tokens: approve=%v reject=%v", approve["value"], reject["value"])
	}
	snooze := elementsOf(t, blocks[3])[0]
	revise := elementsOf(t, blocks[3])[1]
	if snooze["value"] != "tok-snooze" {
		t.Fatalf("snooze value = %v, want its token", snooze["value"])
	}
	// Revise needs a note, so it is a link into the app, not a press.
	if revise["url"] != "https://app.example/acme/company?decision=p1" || revise["value"] != nil {
		t.Fatalf("Revise = %v, want a link with no value", revise)
	}

	seen := map[string]bool{}
	for _, b := range blocks[1:] {
		for _, e := range elementsOf(t, b) {
			id, _ := e["action_id"].(string)
			if id == "" || seen[id] {
				t.Fatalf("action_id %q missing or repeated in one message", id)
			}
			seen[id] = true
		}
	}
}

func TestSlackNotifierMessageWithoutButtonsHasOnlyTheSection(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)

	if err := n.Send(context.Background(), slackNotifierBinding(), channel.Message{Text: "hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	blocks := blocksOf(t, srv.requests[0])
	if len(blocks) != 1 || blocks[0]["type"] != "section" {
		t.Fatalf("blocks = %v, want one section", blocks)
	}
}

// The text is sent as mrkdwn, so a headline with markup characters must not
// open a link or a mention.
func TestSlackNotifierEscapesMarkupInTheText(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)

	if err := n.Send(context.Background(), slackNotifierBinding(), channel.Message{Text: "Fix <b> & ship"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	blocks := blocksOf(t, srv.requests[0])
	section := blocks[0]["text"].(map[string]any)
	if got := section["text"]; got != "Fix &lt;b&gt; &amp; ship" {
		t.Fatalf("section text = %q, want the markup escaped", got)
	}
}

func TestSlackNotifierSucceedsOnAnOKResponse(t *testing.T) {
	t.Parallel()
	n, _, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)

	if err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage()); err != nil {
		t.Fatalf("Send on ok:true: %v", err)
	}
}

// A Web API error arrives as HTTP 200 with ok false. The permanent ones are
// the ones a retry cannot fix; the rest are retried.
func TestSlackNotifierClassifiesWebAPIErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		body      string
		code      string
		permanent bool
	}{
		{"channel not found", slackNotifierChannelNotFound, "channel_not_found", true},
		{"invalid token", `{"ok":false,"error":"invalid_auth"}`, "invalid_auth", true},
		{"missing scope", `{"ok":false,"error":"missing_scope"}`, "missing_scope", true},
		{"user removed", `{"ok":false,"error":"user_not_found"}`, "user_not_found", true},
		{"internal error", slackNotifierInternalError, "internal_error", false},
		{"fatal error", `{"ok":false,"error":"fatal_error"}`, "fatal_error", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, _, _ := newSlackNotifierFixture(t, http.StatusOK, tc.body, nil)
			err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage())
			if err == nil {
				t.Fatal("Send succeeded on ok:false")
			}
			if got := channel.IsPermanentDeliveryError(err); got != tc.permanent {
				t.Fatalf("permanent = %v, want %v (%v)", got, tc.permanent, err)
			}
			if !strings.Contains(err.Error(), tc.code) {
				t.Fatalf("error lost Slack's code %q: %v", tc.code, err)
			}
		})
	}
}

func TestSlackNotifierRateLimitCarriesRetryAfter(t *testing.T) {
	t.Parallel()
	header := http.Header{"Retry-After": []string{"7"}}
	n, _, _ := newSlackNotifierFixture(t, http.StatusTooManyRequests, slackNotifierRateLimited, header)

	err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage())
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

func TestSlackNotifierRefusesWithoutCallingSlackWhenInstallationIsInactive(t *testing.T) {
	t.Parallel()
	n, srv, q := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	q.inst = slackNotifierInstallation("revoked")

	err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage())
	if !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("revoked installation not permanent: %v", err)
	}
	if len(srv.requests) != 0 {
		t.Fatalf("requests = %d, want none", len(srv.requests))
	}
}

func TestSlackNotifierMissingInstallationIsPermanent(t *testing.T) {
	t.Parallel()
	n, srv, q := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	q.err = pgx.ErrNoRows

	if err := n.Send(context.Background(), slackNotifierBinding(), slackNotifierMessage()); !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("missing installation not permanent: %v", err)
	}
	if len(srv.requests) != 0 {
		t.Fatalf("requests = %d, want none", len(srv.requests))
	}
}

func TestSlackNotifierRefusesAnotherChannelsBinding(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	b := slackNotifierBinding()
	b.ChannelType = "telegram"

	if err := n.Send(context.Background(), b, slackNotifierMessage()); !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("telegram binding not permanent: %v", err)
	}
	if len(srv.requests) != 0 {
		t.Fatalf("requests = %d, want none", len(srv.requests))
	}
}

func TestSlackNotifierRefusesABindingWithNoUser(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	b := slackNotifierBinding()
	b.ChannelUserID = ""

	if err := n.Send(context.Background(), b, slackNotifierMessage()); !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("binding with no user not permanent: %v", err)
	}
	if len(srv.requests) != 0 {
		t.Fatalf("requests = %d, want none", len(srv.requests))
	}
}

// A button sets exactly one of URL and CallbackData, as on Telegram. A broken
// button refuses the whole message before anything is sent.
func TestSlackNotifierRefusesAButtonWithoutExactlyOneAddress(t *testing.T) {
	t.Parallel()
	cases := map[string]channel.Button{
		"neither": {Label: "Approve"},
		"both":    {Label: "Approve", URL: "https://app.example", CallbackData: "tok"},
	}
	for name, button := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
			msg := channel.Message{Text: "hello", Buttons: []channel.Button{button}}

			if err := n.Send(context.Background(), slackNotifierBinding(), msg); !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("button %s not permanent: %v", name, err)
			}
			if len(srv.requests) != 0 {
				t.Fatalf("requests = %d, want none", len(srv.requests))
			}
		})
	}
}

func TestSlackNotifierRefusesAnOverlongMessage(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	long := strings.Repeat("x", maxSectionTextRunes+1)

	if err := n.Send(context.Background(), slackNotifierBinding(), channel.Message{Text: long}); !channel.IsPermanentDeliveryError(err) {
		t.Fatalf("overlong message not permanent: %v", err)
	}
	if len(srv.requests) != 0 {
		t.Fatalf("requests = %d, want none", len(srv.requests))
	}
}

// LinkURL is the fallback for a chat app that cannot take callbacks. Slack
// takes them, so a callback button stays an action button and LinkURL is unused.
func TestSlackNotifierKeepsCallbackButtonsAndIgnoresLinkURL(t *testing.T) {
	t.Parallel()
	n, srv, _ := newSlackNotifierFixture(t, http.StatusOK, slackNotifierOKBody, nil)
	msg := channel.Message{Text: "hello", Buttons: []channel.Button{
		{Label: "Approve", CallbackData: "tok-approve", LinkURL: "https://app.example/acme/company?decision=p1"},
	}}

	if err := n.Send(context.Background(), slackNotifierBinding(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	blocks := blocksOf(t, srv.requests[0])
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want section and one actions block", len(blocks))
	}
	button := elementsOf(t, blocks[1])[0]
	if button["value"] != "tok-approve" || button["url"] != nil {
		t.Fatalf("callback button = %v, want the token and no link", button)
	}
}
