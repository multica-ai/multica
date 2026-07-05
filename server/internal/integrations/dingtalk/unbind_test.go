package dingtalk

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeUnbindQueries struct {
	deleted  int64
	err      error
	lastArgs db.DeleteChannelUserBindingParams
}

func (f *fakeUnbindQueries) DeleteChannelUserBinding(_ context.Context, arg db.DeleteChannelUserBindingParams) (int64, error) {
	f.lastArgs = arg
	return f.deleted, f.err
}

func TestUnbinderDeletesSenderBinding(t *testing.T) {
	q := &fakeUnbindQueries{deleted: 1}
	u := &unbinder{q: q}

	inst := autoBindInstallation(t)
	existed, err := u.UnbindSender(context.Background(), inst, autoBindInbound(t, "staff_1"))
	if err != nil {
		t.Fatalf("UnbindSender: %v", err)
	}
	if !existed {
		t.Fatalf("expected existed=true when a row was deleted")
	}
	if q.lastArgs.InstallationID != inst.ID || q.lastArgs.ChannelUserID != "staff_1" {
		t.Fatalf("unexpected delete args: %+v", q.lastArgs)
	}
}

func TestUnbinderReportsMissingBinding(t *testing.T) {
	u := &unbinder{q: &fakeUnbindQueries{deleted: 0}}
	existed, err := u.UnbindSender(context.Background(), autoBindInstallation(t), autoBindInbound(t, "staff_1"))
	if err != nil {
		t.Fatalf("UnbindSender: %v", err)
	}
	if existed {
		t.Fatalf("expected existed=false when no row matched")
	}
}

func replierText(t *testing.T, rec *sessionWebhookRecorder) string {
	t.Helper()
	if len(rec.bodies) != 1 {
		t.Fatalf("webhook posts = %d, want 1", len(rec.bodies))
	}
	md, _ := rec.bodies[0]["markdown"].(map[string]any)
	text, _ := md["text"].(string)
	return text
}

func TestReplierUnboundConfirmation(t *testing.T) {
	rec, srv := newWebhookServer(t)
	r := NewOutboundReplier(OutboundReplierConfig{AppURL: "https://app.example"})
	msg := inboundWithWebhook(t, srv.URL)

	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome:       engine.OutcomeUnbound,
		UnbindExisted: true,
	})
	if text := replierText(t, rec); !strings.Contains(text, "已解除") {
		t.Errorf("unbind confirmation text = %q", text)
	}
}

func TestReplierUnboundWithoutBinding(t *testing.T) {
	rec, srv := newWebhookServer(t)
	r := NewOutboundReplier(OutboundReplierConfig{AppURL: "https://app.example"})
	msg := inboundWithWebhook(t, srv.URL)

	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome:       engine.OutcomeUnbound,
		UnbindExisted: false,
	})
	if text := replierText(t, rec); !strings.Contains(text, "没有绑定记录") {
		t.Errorf("unbind miss text = %q", text)
	}
}

func TestReplierAgentBusyNotice(t *testing.T) {
	rec, srv := newWebhookServer(t)
	r := NewOutboundReplier(OutboundReplierConfig{AppURL: "https://app.example"})
	msg := inboundWithWebhook(t, srv.URL)

	r.Reply(context.Background(), engine.ResolvedInstallation{}, msg, engine.Result{
		Outcome: engine.OutcomeAgentBusy,
	})
	if text := replierText(t, rec); !strings.Contains(text, "已排队") {
		t.Errorf("busy notice text = %q", text)
	}
}
