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

	botNames := NewBotNameResolver(nil, nil)
	factory := newDingTalkFactory(ChannelDeps{Decrypt: box.Open, BotNames: botNames})
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
	if dc.botNames != botNames {
		t.Fatal("factory did not wire the shared BotNameResolver into the channel")
	}
	if dc.Type() != TypeDingTalk {
		t.Errorf("Type() = %q", dc.Type())
	}
	if dc.Capabilities()&channel.CapText == 0 {
		t.Error("channel must declare text capability")
	}
}

func TestNewDingTalkFactory_ReusesDispatcherAcrossReconnects(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-reconnect",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	factory := newDingTalkFactory(ChannelDeps{})

	firstBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	secondBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	first := firstBuilt.(*dingtalkChannel)
	second := secondBuilt.(*dingtalkChannel)
	if first.dispatch != second.dispatch {
		t.Fatal("reconnect build replaced the per-installation dispatcher")
	}
}

func TestDingTalkChannel_DisconnectDoesNotCloseDispatcherAfterNormalRedial(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-redial",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	factory := newDingTalkFactory(ChannelDeps{})
	built, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := built.(*dingtalkChannel)
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if c.dispatch.isClosed() {
		t.Fatal("normal redial closed the reusable dispatcher")
	}
}

func TestDingTalkChannel_DisconnectClosesOnlyCurrentCancelledGeneration(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-rotation",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	factory := newDingTalkFactory(ChannelDeps{})
	firstBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	secondBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	first := firstBuilt.(*dingtalkChannel)
	second := secondBuilt.(*dingtalkChannel)
	first.stopDispatch.Store(true)
	if err := first.Disconnect(context.Background()); err != nil {
		t.Fatalf("old generation disconnect: %v", err)
	}
	if second.dispatch.isClosed() {
		t.Fatal("old generation closed the replacement's dispatcher")
	}

	second.stopDispatch.Store(true)
	if err := second.Disconnect(context.Background()); err != nil {
		t.Fatalf("current generation disconnect: %v", err)
	}
	if !second.dispatch.isClosed() {
		t.Fatal("current cancelled generation left its dispatcher open")
	}

	thirdBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("third build: %v", err)
	}
	third := thirdBuilt.(*dingtalkChannel)
	if third.dispatch == second.dispatch {
		t.Fatal("factory reused a closed dispatcher")
	}
}

func TestDingTalkChannel_DisconnectReleasesClosedDispatcherSlot(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-release",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	slots := newDispatchSlotRegistry()
	factory := newDingTalkFactoryWithRegistry(ChannelDeps{}, slots)
	built, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := slots.size(); got != 1 {
		t.Fatalf("registered dispatcher slots = %d, want 1", got)
	}

	c := built.(*dingtalkChannel)
	c.stopDispatch.Store(true)
	if err := c.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if got := slots.size(); got != 0 {
		t.Fatalf("registered dispatcher slots after lifecycle stop = %d, want 0", got)
	}
}

func TestDingTalkChannel_DisconnectTimeoutReleasesSlotAfterWorkerExits(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-release-after-timeout",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	started := make(chan struct{})
	release := make(chan struct{})
	slots := newDispatchSlotRegistry()
	factory := newDingTalkFactoryWithRegistry(ChannelDeps{}, slots)
	built, err := factory(channel.Config{
		Type: TypeDingTalk,
		Raw:  cfg,
		Handler: func(context.Context, channel.InboundMessage) error {
			close(started)
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := built.(*dingtalkChannel)
	c.dispatch.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("dispatch worker did not start")
	}

	c.stopDispatch.Store(true)
	disconnectCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := c.Disconnect(disconnectCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("disconnect error = %v, want deadline exceeded", err)
	}
	if got := slots.size(); got != 1 {
		t.Fatalf("registered dispatcher slots before worker exit = %d, want 1", got)
	}
	close(release)

	deadline := time.Now().Add(time.Second)
	for slots.size() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := slots.size(); got != 0 {
		t.Fatalf("registered dispatcher slots after worker exit = %d, want 0", got)
	}
}

func TestDingTalkChannel_DisconnectDrainsQueuedMessagesBeforeReplacement(t *testing.T) {
	cfg, _ := json.Marshal(installConfig{
		AppID:              "appkey-drain",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString([]byte("secret")),
	})
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	handled := make(chan string, 2)
	factory := newDingTalkFactory(ChannelDeps{})
	built, err := factory(channel.Config{
		Type: TypeDingTalk,
		Raw:  cfg,
		Handler: func(_ context.Context, msg channel.InboundMessage) error {
			started <- struct{}{}
			<-release
			handled <- msg.MessageID
			return nil
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	first := built.(*dingtalkChannel)
	first.dispatch.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	first.dispatch.enqueue("conv-A", dispatchMsg("conv-A", "second"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first message did not start")
	}

	first.stopDispatch.Store(true)
	disconnected := make(chan error, 1)
	go func() { disconnected <- first.Disconnect(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for !first.dispatch.isClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !first.dispatch.isClosed() {
		t.Fatal("dispatcher did not start closing")
	}

	replacementBuilt, err := factory(channel.Config{Type: TypeDingTalk, Raw: cfg, Handler: noopHandler})
	if err != nil {
		t.Fatalf("replacement build: %v", err)
	}
	if replacementBuilt.(*dingtalkChannel).dispatch == first.dispatch {
		t.Fatal("replacement adopted a closing dispatcher")
	}
	close(release)

	select {
	case err := <-disconnected:
		if err != nil {
			t.Fatalf("disconnect: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not finish draining")
	}
	for _, want := range []string{"first", "second"} {
		select {
		case got := <-handled:
			if got != want {
				t.Fatalf("handled %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("queued message %q was dropped during drain", want)
		}
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
		if !strings.Contains(body, issueDispatchFailedText) {
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

func TestOnMessage_ReturnsBeforeBotNameLookupAndQueuesCallbackSnapshot(t *testing.T) {
	lookupStarted := make(chan struct{})
	releaseLookup := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == accessTokenPath {
			_, _ = w.Write([]byte(`{"accessToken":"tok","expireIn":7200}`))
			return
		}
		close(lookupStarted)
		<-releaseLookup
		_, _ = w.Write([]byte(`{"chatbotInstanceVOList":[{"robotCode":"robot-1","name":"Multica Bot - Local"}]}`))
	}))
	defer srv.Close()

	handled := make(chan channel.InboundMessage, 1)
	client := NewClient(nil, srv.URL)
	c := &dingtalkChannel{
		appID: "appkey-1", robotCode: "robot-1", appKey: "appkey-1", appSecret: "secret",
		client: client, botNames: NewBotNameResolver(client, nil), logger: slog.Default(),
		handler: func(_ context.Context, msg channel.InboundMessage) error {
			handled <- msg
			return nil
		},
	}
	c.dispatch = newDispatcher(c.runInbound, c.logger)
	data := &botCallbackData{
		ConversationId: "cid-1", ConversationType: convTypeGroup, IsInAtList: true,
		SenderStaffId: "staff-1", MsgId: "msg-1", Msgtype: "text",
		Text: botCallbackText{Content: "@Multica Bot - Local /new inspect this"},
	}
	returned := make(chan struct{})
	go func() {
		_ = c.onMessage(context.Background(), data)
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("onMessage waited for Bot-name resolution instead of returning for ACK")
	}
	select {
	case <-lookupStarted:
	case <-time.After(time.Second):
		t.Fatal("queued worker did not start Bot-name resolution")
	}

	// The Stream decoder owns data only for the callback. A queued job must not
	// retain aliases to mutable slices from that object.
	data.Text.Content = "mutated after enqueue"
	data.AtUsers = append(data.AtUsers, botCallbackAtUser{StaffId: "mutated"})
	data.Content = append(data.Content, []byte("mutated")...)
	close(releaseLookup)

	select {
	case msg := <-handled:
		if msg.Text != "/new inspect this" || msg.CommandText != msg.Text {
			t.Fatalf("queued callback snapshot normalized to %q/%q", msg.Text, msg.CommandText)
		}
	case <-time.After(time.Second):
		t.Fatal("queued callback was not handled after Bot-name resolution")
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
