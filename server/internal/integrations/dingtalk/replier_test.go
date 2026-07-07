package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type fakeMinter struct{ lastUserID string }

func (f *fakeMinter) Mint(_ context.Context, _, _ pgtype.UUID, userID string) (BindingToken, error) {
	f.lastUserID = userID
	return BindingToken{Raw: "tok_raw"}, nil
}

// sessionWebhookRecorder captures webhook posts.
type sessionWebhookRecorder struct {
	mu     chan struct{}
	bodies []map[string]any
}

func newWebhookServer(t *testing.T) (*sessionWebhookRecorder, *httptest.Server) {
	rec := &sessionWebhookRecorder{mu: make(chan struct{}, 1)}
	rec.mu <- struct{}{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		<-rec.mu
		rec.bodies = append(rec.bodies, body)
		rec.mu <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return rec, srv
}

func inboundWithWebhook(t *testing.T, webhook string) channel.InboundMessage {
	t.Helper()
	msg, ok := inboundFromBotCallback(botCallbackData{
		ConversationID:   "cid",
		MsgID:            "m1",
		SenderStaffID:    "staff1",
		ConversationType: "1",
		Msgtype:          "text",
		SessionWebhook:   webhook,
	}, "ding_client")
	if !ok {
		t.Fatal("inbound mapping failed")
	}
	return msg
}

func TestReplierBindingPromptPostsSessionWebhook(t *testing.T) {
	rec, srv := newWebhookServer(t)
	minter := &fakeMinter{}
	r := NewOutboundReplier(OutboundReplierConfig{
		Binding: minter,
		AppURL:  "https://app.example",
	})
	msg := inboundWithWebhook(t, srv.URL)
	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome: engine.OutcomeNeedsBinding,
		Sender:  "staff1",
	})
	if minter.lastUserID != "staff1" {
		t.Errorf("minted for %q", minter.lastUserID)
	}
	if len(rec.bodies) != 1 {
		t.Fatalf("webhook posts = %d, want 1", len(rec.bodies))
	}
	body := rec.bodies[0]
	if body["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v", body["msgtype"])
	}
	md, _ := body["markdown"].(map[string]any)
	text, _ := md["text"].(string)
	if !strings.Contains(text, "https://app.example/dingtalk/bind?token=tok_raw") {
		t.Errorf("binding prompt text = %q", text)
	}
}

func TestReplierIssueCreatedConfirmation(t *testing.T) {
	rec, srv := newWebhookServer(t)
	r := NewOutboundReplier(OutboundReplierConfig{AppURL: "https://app.example"})
	msg := inboundWithWebhook(t, srv.URL)
	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome:         engine.OutcomeIngested,
		IssueID:         pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		IssueIdentifier: "MUL-42",
		IssueTitle:      "修复登录",
	})
	if len(rec.bodies) != 1 {
		t.Fatalf("webhook posts = %d, want 1", len(rec.bodies))
	}
	md, _ := rec.bodies[0]["markdown"].(map[string]any)
	text, _ := md["text"].(string)
	if !strings.Contains(text, "MUL-42") || !strings.Contains(text, "修复登录") {
		t.Errorf("issue confirmation = %q", text)
	}
}

func TestReplierPlainIngestStaysSilent(t *testing.T) {
	rec, srv := newWebhookServer(t)
	r := NewOutboundReplier(OutboundReplierConfig{AppURL: "https://app.example"})
	msg := inboundWithWebhook(t, srv.URL)
	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome: engine.OutcomeIngested,
	})
	if len(rec.bodies) != 0 {
		t.Fatalf("webhook posts = %d, want 0 (agent reply lands via EventChatDone)", len(rec.bodies))
	}
}

func TestPostSessionWebhookSurfacesEnvelopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"keywords not in content"}`))
	}))
	t.Cleanup(srv.Close)
	err := postSessionWebhook(context.Background(), srv.Client(), srv.URL, "hello")
	if err == nil || !strings.Contains(err.Error(), "errcode_310000") {
		t.Errorf("err = %v, want errcode envelope error", err)
	}
}
