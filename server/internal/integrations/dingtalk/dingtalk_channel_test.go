package dingtalk

import (
	"context"
	"encoding/base64"
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
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

func noopHandler(_ context.Context, _ channel.InboundMessage) error { return nil }

func TestNewDingTalkFactory_DecryptsAndBuilds(t *testing.T) {
	box := testBox(t)
	sealed, _ := box.Seal([]byte("plain-secret"))
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-1",
		RobotCode:          "appkey-1",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})

	factory := newDingTalkFactory(ChannelDeps{Decrypt: box.Open})
	built, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	dc, ok := built.(*dingtalkChannel)
	if !ok {
		t.Fatalf("built channel is not a *dingtalkChannel: %T", built)
	}
	if dc.appKey != "appkey-1" || dc.appID != "appkey-1" || dc.robotCode != "appkey-1" {
		t.Errorf("identity fields wrong: %+v", dc)
	}
	if dc.appSecret != "plain-secret" {
		t.Errorf("app secret = %q, want decrypted plain-secret", dc.appSecret)
	}
	if dc.Type() != TypeDingTalk {
		t.Errorf("Type() = %q", dc.Type())
	}
	if dc.Capabilities()&channel.CapText == 0 {
		t.Error("channel must declare text capability")
	}
}

// A handler (engine dispatch) error on an addressed /issue command must post
// the internal-error notice: the frame is already ACKed and never redelivered,
// so silence would lose the command with no signal to the user.
func TestOnMessage_HandlerError_IssueCommandGetsErrorReply(t *testing.T) {
	sent := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		select {
		case sent <- string(body):
		default:
		}
		_, _ = w.Write([]byte(`{"processQueryKey":"pqk-1"}`))
	}))
	defer srv.Close()

	c := &dingtalkChannel{
		appID:     "appkey-1",
		robotCode: "robot-1",
		appKey:    "ak",
		appSecret: "as",
		client:    NewClient(nil, srv.URL),
		handler: func(_ context.Context, _ channel.InboundMessage) error {
			return errors.New("resolve sender: transient db error")
		},
		logger: slog.Default(),
	}
	c.dispatch = newDispatcher(c.runInbound, c.logger)
	data := &botCallbackData{
		ConversationId:   "cid-1",
		ConversationType: convTypeP2P,
		SenderStaffId:    "staff-1",
		MsgId:            "msg-1",
		Msgtype:          "text",
		Text:             botCallbackText{Content: "/issue login broken"},
	}
	if err := c.onMessage(context.Background(), data); err != nil {
		t.Fatalf("onMessage: %v", err)
	}
	select {
	case body := <-sent:
		if !strings.Contains(body, engine.IssueQueueFailedText) {
			t.Fatalf("reply body = %q, want the internal-error notice", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no error reply was sent for the failed /issue dispatch")
	}
}

// onMessage must return immediately even when the pipeline is slow: the
// socket read loop ACKs right after it, and a blocked loop would starve
// ping/system frames and get the connection torn down.
func TestOnMessage_ReturnsWithoutWaitingForHandler(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	c := &dingtalkChannel{appID: "appkey-1", logger: slog.Default(), handler: func(_ context.Context, _ channel.InboundMessage) error {
		close(started)
		<-release
		return nil
	}}
	c.dispatch = newDispatcher(c.runInbound, c.logger)
	defer close(release)

	data := &botCallbackData{
		ConversationId:   "cid-1",
		ConversationType: convTypeP2P,
		SenderStaffId:    "staff-1",
		MsgId:            "msg-1",
		Msgtype:          "text",
		Text:             botCallbackText{Content: "hello"},
	}
	done := make(chan struct{})
	go func() {
		_ = c.onMessage(context.Background(), data)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("onMessage blocked on the handler")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("the queued job never ran")
	}
}

// A handler error on a plain chat turn stays silent — only /issue commands
// warrant the dispatch-error notice.
func TestNotifyIssueDispatchError_GatesOnIssueCommand(t *testing.T) {
	c := &dingtalkChannel{logger: slog.Default()}
	// Non-issue text and unaddressed messages must return before spawning the
	// send goroutine; a nil client would panic if they did not.
	c.notifyIssueDispatchError(channel.InboundMessage{Text: "hello", AddressedToBot: true})
	c.notifyIssueDispatchError(channel.InboundMessage{Text: "/issue x", AddressedToBot: false})
}

func TestNewDingTalkFactory_RejectsMissingSecret(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{AppID: "appkey-1"})
	factory := newDingTalkFactory(ChannelDeps{Decrypt: testBox(t).Open})
	if _, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg}); err == nil {
		t.Error("an installation with no app secret must fail to build")
	}
}
