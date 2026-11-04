package dingtalk

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

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The bodies below are the shapes the DingTalk Open API returns, written from the
// documented envelopes. No test reaches the real host: the client points at an
// httptest server.
const (
	notifierTokenBody  = `{"accessToken":"notifier-token","expireIn":7200}`
	notifierSendBody   = `{"processQueryKey":"pqk-1"}`
	notifierBadRequest = `{"code":"invalidParameter","message":"staffId is invalid"}`
	notifierServerErr  = `{"code":"InternalError","message":"try later"}`
	notifierStaffID    = "staff-brief-1"
)

func notifierInstallation(status string) db.ChannelInstallation {
	cfg, _ := json.Marshal(map[string]string{
		"app_id":               "app-key",
		"robot_code":           "robot-1",
		"app_secret_encrypted": base64.StdEncoding.EncodeToString([]byte("app-secret")),
	})
	return db.ChannelInstallation{ID: sessionUUID(5), ChannelType: string(TypeDingTalk), Status: status, Config: cfg}
}

type fakeNotifierQueries struct {
	inst db.ChannelInstallation
	err  error
}

func (f *fakeNotifierQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if arg.ChannelType != string(TypeDingTalk) {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	return f.inst, f.err
}

// notifierRequest is one call the notifier made to the robot API.
type notifierRequest struct {
	path  string
	token string
	body  map[string]any
}

// notifierServer answers the token mint and the send, and records every call.
// sendStatus and sendBody are what each send returns, in order; the last one
// repeats.
type notifierServer struct {
	mu         sync.Mutex
	sendStatus []int
	sendBody   []string
	tokenMints int
	requests   []notifierRequest
}

func (s *notifierServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path == accessTokenPath {
			s.tokenMints++
			_, _ = w.Write([]byte(notifierTokenBody))
			return
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("send body is not JSON: %v", err)
		}
		s.requests = append(s.requests, notifierRequest{path: r.URL.Path, token: r.Header.Get("x-acs-dingtalk-access-token"), body: body})
		i := len(s.requests) - 1
		status, resp := http.StatusOK, notifierSendBody
		if len(s.sendStatus) > 0 {
			idx := min(i, len(s.sendStatus)-1)
			status = s.sendStatus[idx]
			resp = s.sendBody[idx]
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}
}

func newNotifierForTest(t *testing.T, srv *notifierServer, q notifierQueries) *Notifier {
	t.Helper()
	ts := httptest.NewServer(srv.handler(t))
	t.Cleanup(ts.Close)
	client := NewClient(ts.Client(), ts.URL)
	identity := func(b []byte) ([]byte, error) { return b, nil }
	return NewNotifier(q, identity, client)
}

func notifierBinding() channel.Binding {
	return channel.Binding{
		ChannelType:    TypeDingTalk,
		InstallationID: sessionUUID(5),
		ChannelUserID:  notifierStaffID,
	}
}

func TestNotifierSendsMarkdownWithoutButtons(t *testing.T) {
	srv := &notifierServer{}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})

	if err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "Brief ready\nTwo decisions"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(srv.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(srv.requests))
	}
	req := srv.requests[0]
	if req.path != pathSendP2P {
		t.Fatalf("path = %q, want the one-to-one send", req.path)
	}
	if req.token != "notifier-token" {
		t.Fatalf("access token header = %q", req.token)
	}
	if req.body["robotCode"] != "robot-1" || req.body["msgKey"] != msgKeyMarkdown {
		t.Fatalf("body = %+v, want robot-1 and sampleMarkdown", req.body)
	}
	if users, _ := req.body["userIds"].([]any); len(users) != 1 || users[0] != notifierStaffID {
		t.Fatalf("userIds = %v, want the binding's staff id", req.body["userIds"])
	}
	var param markdownParam
	if err := json.Unmarshal([]byte(req.body["msgParam"].(string)), &param); err != nil {
		t.Fatalf("msgParam: %v", err)
	}
	if param.Text != "Brief ready\nTwo decisions" || param.Title == "" {
		t.Fatalf("msgParam = %+v", param)
	}
}

func TestNotifierSendsActionCardWithLinkButtons(t *testing.T) {
	srv := &notifierServer{}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})

	msg := channel.Message{
		Text: "Brief ready\n1 decision needs you:",
		Buttons: []channel.Button{
			{Label: "Open the brief", URL: "https://app.example/acme/office?brief=b1"},
			// A callback button has no place on DingTalk: it becomes a link to its
			// LinkURL, and is dropped when it has none.
			{Label: "Approve: Hire a designer", CallbackData: "signed-token", LinkURL: "https://app.example/acme/office?decision=p1"},
			{Label: "Approve: no link", CallbackData: "signed-token-2"},
		},
	}
	if err := n.Send(context.Background(), notifierBinding(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	req := srv.requests[0]
	if req.body["msgKey"] != msgKeyActionCard {
		t.Fatalf("msgKey = %v, want %s", req.body["msgKey"], msgKeyActionCard)
	}
	var param actionCardParam
	if err := json.Unmarshal([]byte(req.body["msgParam"].(string)), &param); err != nil {
		t.Fatalf("msgParam: %v", err)
	}
	if len(param.Btns) != 2 {
		t.Fatalf("buttons = %+v, want the brief link and the decision link", param.Btns)
	}
	if param.Btns[0].ActionURL != "https://app.example/acme/office?brief=b1" {
		t.Fatalf("first button = %+v, want the brief link", param.Btns[0])
	}
	if param.Btns[1].ActionURL != "https://app.example/acme/office?decision=p1" {
		t.Fatalf("second button = %+v, want the callback button's link", param.Btns[1])
	}
	if strings.Contains(req.body["msgParam"].(string), "signed-token") {
		t.Fatalf("a signed token reached DingTalk: %s", req.body["msgParam"])
	}
}

func TestNotifierTruncatesTheButtonsToTheCardLimit(t *testing.T) {
	srv := &notifierServer{}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})

	var buttons []channel.Button
	for i := 0; i < maxActionCardButtons+3; i++ {
		buttons = append(buttons, channel.Button{Label: "Open", URL: "https://app.example/x"})
	}
	if err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "t", Buttons: buttons}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var param actionCardParam
	_ = json.Unmarshal([]byte(srv.requests[0].body["msgParam"].(string)), &param)
	if len(param.Btns) != maxActionCardButtons {
		t.Fatalf("buttons = %d, want %d", len(param.Btns), maxActionCardButtons)
	}
}

func TestNotifierRefusesBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name    string
		binding channel.Binding
		msg     channel.Message
	}{
		{"another channel's binding", channel.Binding{ChannelType: "slack", InstallationID: sessionUUID(5), ChannelUserID: notifierStaffID}, channel.Message{Text: "x"}},
		{"no staff id", channel.Binding{ChannelType: TypeDingTalk, InstallationID: sessionUUID(5)}, channel.Message{Text: "x"}},
		{"no text", notifierBinding(), channel.Message{}},
		{"button with neither address", notifierBinding(), channel.Message{Text: "x", Buttons: []channel.Button{{Label: "Bad"}}}},
		{"button with both addresses", notifierBinding(), channel.Message{Text: "x", Buttons: []channel.Button{{Label: "Bad", URL: "https://a", CallbackData: "t"}}}},
		{"oversized message", notifierBinding(), channel.Message{Text: strings.Repeat("a", markdownPayloadByteBudget)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &notifierServer{}
			n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})
			err := n.Send(context.Background(), tc.binding, tc.msg)
			if !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("Send error = %v, want a permanent failure", err)
			}
			if srv.tokenMints != 0 || len(srv.requests) != 0 {
				t.Fatalf("a refused message reached DingTalk: mints=%d requests=%d", srv.tokenMints, len(srv.requests))
			}
		})
	}
}

func TestNotifierInstallationProblemsArePermanent(t *testing.T) {
	cases := []struct {
		name string
		q    *fakeNotifierQueries
	}{
		{"installation missing", &fakeNotifierQueries{err: pgx.ErrNoRows}},
		{"installation inactive", &fakeNotifierQueries{inst: notifierInstallation("revoked")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &notifierServer{}
			n := newNotifierForTest(t, srv, tc.q)
			err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "x"})
			if !channel.IsPermanentDeliveryError(err) {
				t.Fatalf("Send error = %v, want a permanent failure", err)
			}
			if len(srv.requests) != 0 {
				t.Fatalf("a send went out for a dead installation")
			}
		})
	}
}

func TestNotifierUnavailableInstallationLookupIsRetryable(t *testing.T) {
	srv := &notifierServer{}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{err: errors.New("connection reset")})
	err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "x"})
	if err == nil || channel.IsPermanentDeliveryError(err) {
		t.Fatalf("Send error = %v, want a retryable failure", err)
	}
}

func TestNotifierClassifiesDingTalkErrorsForRetry(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{"invalid staff id", http.StatusBadRequest, notifierBadRequest, true},
		{"forbidden to this user", http.StatusForbidden, `{"code":"forbidden"}`, true},
		{"not found", http.StatusNotFound, `{"code":"notFound"}`, true},
		{"request timeout", http.StatusRequestTimeout, `{"code":"timeout"}`, false},
		{"rate limited", http.StatusTooManyRequests, `{"code":"throttled"}`, false},
		{"server error", http.StatusInternalServerError, notifierServerErr, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &notifierServer{sendStatus: []int{tc.status}, sendBody: []string{tc.body}}
			n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})
			err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "x"})
			if err == nil {
				t.Fatalf("Send succeeded on HTTP %d", tc.status)
			}
			if got := channel.IsPermanentDeliveryError(err); got != tc.permanent {
				t.Fatalf("permanent = %v, want %v (err: %v)", got, tc.permanent, err)
			}
		})
	}
}

// A 401 mints a fresh token and sends once more: the expired-token path
// is the same as the reply path's.
func TestNotifierRefreshesTheTokenOnceOn401(t *testing.T) {
	srv := &notifierServer{sendStatus: []int{http.StatusUnauthorized, http.StatusOK}, sendBody: []string{`{}`, notifierSendBody}}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})

	if err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "x"}); err != nil {
		t.Fatalf("Send after a 401: %v", err)
	}
	if srv.tokenMints != 2 {
		t.Fatalf("token mints = %d, want 2 (one refresh)", srv.tokenMints)
	}
}

func TestNotifierSuccessIsNotAnError(t *testing.T) {
	srv := &notifierServer{}
	n := newNotifierForTest(t, srv, &fakeNotifierQueries{inst: notifierInstallation("active")})
	if err := n.Send(context.Background(), notifierBinding(), channel.Message{Text: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
}
