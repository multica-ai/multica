package weixin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestReplyHelpSendsPlainTextToBoundAccount(t *testing.T) {
	x := newDatabaseFixture(t)
	inst := x.connect(t)
	info := DecodePublicConfig(inst.Config)
	ctx := context.Background()
	sealed, err := x.svc.box.Seal([]byte("reply-context"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.q.SaveWeixinContext(ctx, db.SaveWeixinContextParams{
		InstallationID: inst.ID, BotID: info.BotID, UserID: info.UserID,
		ContextTokenEncrypted: sealed, ContextTimeMs: 123,
	}); err != nil {
		t.Fatal(err)
	}

	replies := make(chan message, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
		var body struct {
			Msg message `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		replies <- body.Msg
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	o := NewOutbound(x.q, x.svc.box, nil)
	o.client = providerClient(server)
	o.Reply(ctx, engine.ResolvedInstallation{ID: inst.ID, Platform: inst}, channel.InboundMessage{
		Source: channel.Source{ChatID: info.UserID, SenderID: info.UserID},
	}, engine.Result{Outcome: engine.OutcomeHelp})

	if len(replies) != 1 {
		t.Fatalf("got %d help replies, want 1", len(replies))
	}
	got := <-replies
	if got.To != info.UserID || got.ContextToken != "reply-context" {
		t.Fatalf("wrong help destination or reply context: %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].Text == nil {
		t.Fatalf("expected one text item, got %+v", got.Items)
	}
	if want := strings.ReplaceAll(engine.HelpTextEnglish, "`", ""); got.Items[0].Text.Text != want {
		t.Fatalf("help text = %q, want %q", got.Items[0].Text.Text, want)
	}
}
