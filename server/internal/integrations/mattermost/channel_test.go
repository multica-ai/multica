package mattermost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// fakeMattermost serves both the WebSocket event stream and the REST endpoints
// the adapter reaches for, so a channel test exercises the real Connect path.
type fakeMattermost struct {
	server *httptest.Server

	mu          sync.Mutex
	authToken   string // token received on the authentication challenge
	authOK      bool   // reply the challenge with OK (false = FAIL)
	created     []Post
	postsByID   map[string]Post
	sendOnReady []any // frames to push once authenticated
}

func newFakeMattermost(t *testing.T) *fakeMattermost {
	t.Helper()
	f := &fakeMattermost{authOK: true, postsByID: map[string]Post{}}
	mux := http.NewServeMux()
	mux.HandleFunc(websocketPath, f.serveWS)
	mux.HandleFunc(apiPath+"/posts", f.servePosts)
	mux.HandleFunc(apiPath+"/posts/", f.servePostByID)
	mux.HandleFunc(apiPath+"/users/", f.serveUserByID)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMattermost) serveWS(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()

	var challenge wsAction
	if err := conn.ReadJSON(&challenge); err != nil {
		return
	}
	f.mu.Lock()
	if token, ok := challenge.Data["token"].(string); ok {
		f.authToken = token
	}
	ok := f.authOK
	pending := f.sendOnReady
	f.mu.Unlock()

	if !ok {
		_ = conn.WriteJSON(map[string]any{
			"status":    "FAIL",
			"seq_reply": challenge.Seq,
			"error":     map[string]any{"id": "api.web_socket.auth", "message": "token rejected"},
		})
		// Hold the socket open: the adapter must fail on the reply, not on EOF.
		time.Sleep(2 * time.Second)
		return
	}
	_ = conn.WriteJSON(map[string]any{"status": statusOK, "seq_reply": challenge.Seq})
	_ = conn.WriteJSON(map[string]any{"event": eventHello, "data": map[string]any{}, "seq": 0})
	for i, frame := range pending {
		_ = conn.WriteJSON(frame)
		_ = i
	}
	// Keep the connection alive so the adapter's read loop stays blocked until
	// the test cancels it.
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (f *fakeMattermost) servePosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		return
	}
	var in Post
	_ = json.NewDecoder(r.Body).Decode(&in)
	f.mu.Lock()
	in.ID = "created"
	f.created = append(f.created, in)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(in)
}

func (f *fakeMattermost) servePostByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, apiPath+"/posts/")
	f.mu.Lock()
	post, ok := f.postsByID[id]
	f.mu.Unlock()
	if !ok {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(post)
}

func (f *fakeMattermost) serveUserByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, apiPath+"/users/")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(User{ID: id, Username: "alice", FirstName: "Alice"})
}

func (f *fakeMattermost) queue(frames ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendOnReady = append(f.sendOnReady, frames...)
}

func (f *fakeMattermost) createdPosts() []Post {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Post(nil), f.created...)
}

// postedFrame builds a "posted" event frame in Mattermost's wire shape.
func postedFrame(post Post, channelType, senderName string) map[string]any {
	encoded, _ := json.Marshal(post)
	return map[string]any{
		"event": eventPosted,
		"data": map[string]any{
			"post":         string(encoded),
			"channel_type": channelType,
			"sender_name":  senderName,
		},
		"seq": 1,
	}
}

// buildChannel constructs the adapter against the fake server through the real
// Factory, so the config decode path is covered too.
func buildChannel(t *testing.T, f *fakeMattermost, handler channel.InboundHandler) channel.Channel {
	t.Helper()
	cfg, err := json.Marshal(installConfig{
		AppID:                testAppID,
		ServerURL:            f.server.URL,
		BotUserID:            testBotID,
		BotUsername:          testBotUsername,
		AccessTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("tok123")),
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := newMattermostFactory(ChannelDeps{HTTPClient: f.server.Client()})(channel.Config{
		Type:    TypeMattermost,
		Raw:     cfg,
		Handler: handler,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return ch
}

func TestChannelConnectDeliversPosts(t *testing.T) {
	f := newFakeMattermost(t)
	f.queue(postedFrame(Post{
		ID:        "p1",
		UserID:    "humanuser000000000000000",
		ChannelID: "chan1",
		Message:   "@multica hello there",
	}, "O", "@alice"))

	received := make(chan channel.InboundMessage, 4)
	ch := buildChannel(t, f, func(_ context.Context, msg channel.InboundMessage) error {
		received <- msg
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connErr := make(chan error, 1)
	go func() { connErr <- ch.Connect(ctx) }()

	select {
	case msg := <-received:
		if msg.MessageID != "p1" {
			t.Errorf("MessageID = %q, want p1", msg.MessageID)
		}
		if !msg.AddressedToBot {
			t.Error("AddressedToBot = false, want true for an explicit mention")
		}
		if msg.Text != "hello there" {
			t.Errorf("Text = %q, want the mention stripped", msg.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no inbound message within 5s")
	}

	// The challenge must have carried the decrypted token.
	f.mu.Lock()
	gotToken := f.authToken
	f.mu.Unlock()
	if gotToken != "tok123" {
		t.Errorf("authentication challenge token = %q, want tok123", gotToken)
	}

	// Cancelling is a graceful stop, not a failure the Supervisor should back
	// off from.
	cancel()
	select {
	case err := <-connErr:
		if err != nil {
			t.Errorf("Connect after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return within 5s of cancellation")
	}
}

func TestChannelConnectRejectedToken(t *testing.T) {
	f := newFakeMattermost(t)
	f.mu.Lock()
	f.authOK = false
	f.mu.Unlock()

	ch := buildChannel(t, f, func(context.Context, channel.InboundMessage) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := ch.Connect(ctx)
	if err == nil {
		t.Fatal("Connect succeeded, want the rejection reported")
	}
	// The operator has to be able to tell "rotate the token" from "the socket
	// dropped"; a generic error would send them to the wrong fix.
	if !errors.Is(err, ErrAuthRejected) {
		t.Fatalf("error = %v, want ErrAuthRejected", err)
	}
	if !strings.Contains(err.Error(), "token rejected") {
		t.Errorf("error = %v, want the server's detail included", err)
	}
}

// A handler error is infrastructure failure: it must end the connection so the
// Supervisor reconnects, rather than being swallowed.
func TestChannelConnectPropagatesHandlerError(t *testing.T) {
	f := newFakeMattermost(t)
	f.queue(postedFrame(Post{
		ID: "p1", UserID: "humanuser000000000000000", ChannelID: "chan1", Message: "@multica hi",
	}, "D", "@alice"))

	boom := errors.New("database unavailable")
	ch := buildChannel(t, f, func(context.Context, channel.InboundMessage) error { return boom })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ch.Connect(ctx); !errors.Is(err, boom) {
		t.Fatalf("Connect = %v, want the handler error propagated", err)
	}
}

// A product drop (unaddressed group chatter) must not reach the handler and
// must not end the connection.
func TestChannelConnectIgnoresUnaddressedChatter(t *testing.T) {
	f := newFakeMattermost(t)
	f.queue(
		postedFrame(Post{ID: "p1", UserID: "u1", ChannelID: "chan1", Message: "unrelated chatter"}, "O", "@alice"),
		postedFrame(Post{ID: "p2", UserID: "u1", ChannelID: "chan1", Message: "@multica now for me"}, "O", "@alice"),
	)

	received := make(chan channel.InboundMessage, 4)
	ch := buildChannel(t, f, func(_ context.Context, msg channel.InboundMessage) error {
		received <- msg
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ch.Connect(ctx) }()

	// Both posts are ingested (the engine decides what to do with unaddressed
	// ones), but only the second is addressed.
	var addressed []string
	deadline := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case msg := <-received:
			if msg.AddressedToBot {
				addressed = append(addressed, msg.MessageID)
			}
		case <-deadline:
			t.Fatalf("only received %d of 2 messages", i)
		}
	}
	if len(addressed) != 1 || addressed[0] != "p2" {
		t.Fatalf("addressed = %v, want only p2", addressed)
	}
}

// A mention-free reply inside a thread the bot rooted is a continuation, so it
// counts as addressed. This is the one addressing rule that costs a REST call.
func TestChannelConnectThreadRootedByBotIsAddressed(t *testing.T) {
	f := newFakeMattermost(t)
	f.mu.Lock()
	f.postsByID["root1"] = Post{ID: "root1", UserID: testBotID, Message: "here is what I found"}
	f.mu.Unlock()
	f.queue(postedFrame(Post{
		ID: "p1", UserID: "u1", ChannelID: "chan1", RootID: "root1", Message: "can you expand on that?",
	}, "O", "@alice"))

	received := make(chan channel.InboundMessage, 2)
	ch := buildChannel(t, f, func(_ context.Context, msg channel.InboundMessage) error {
		received <- msg
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ch.Connect(ctx) }()

	select {
	case msg := <-received:
		if !msg.AddressedToBot {
			t.Fatal("AddressedToBot = false, want true for a reply in a bot-rooted thread")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no inbound message within 5s")
	}
}

// The same reply in a thread rooted by a human is NOT addressed.
func TestChannelConnectThreadRootedByHumanIsNotAddressed(t *testing.T) {
	f := newFakeMattermost(t)
	f.mu.Lock()
	f.postsByID["root1"] = Post{ID: "root1", UserID: "someoneelse", Message: "team discussion"}
	f.mu.Unlock()
	f.queue(postedFrame(Post{
		ID: "p1", UserID: "u1", ChannelID: "chan1", RootID: "root1", Message: "agreed",
	}, "O", "@alice"))

	received := make(chan channel.InboundMessage, 2)
	ch := buildChannel(t, f, func(_ context.Context, msg channel.InboundMessage) error {
		received <- msg
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ch.Connect(ctx) }()

	select {
	case msg := <-received:
		if msg.AddressedToBot {
			t.Fatal("AddressedToBot = true for a human-rooted thread, want false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no inbound message within 5s")
	}
}

// A media-only DM gets a spoken "cannot handle this yet" rather than silence.
func TestChannelConnectAnswersUnsupportedMediaInDM(t *testing.T) {
	f := newFakeMattermost(t)
	f.queue(postedFrame(Post{
		ID: "p1", UserID: "u1", ChannelID: "dm1", FileIDs: []string{"file1"},
	}, "D", "@alice"))

	handled := make(chan struct{}, 1)
	ch := buildChannel(t, f, func(context.Context, channel.InboundMessage) error {
		handled <- struct{}{}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = ch.Connect(ctx) }()

	deadline := time.After(5 * time.Second)
	for {
		if posts := f.createdPosts(); len(posts) > 0 {
			if posts[0].Message != msgUnsupportedType {
				t.Fatalf("notice = %q, want the unsupported-type copy", posts[0].Message)
			}
			// The notice threads under the offending post so it stays attached.
			if posts[0].RootID != "p1" {
				t.Errorf("notice RootID = %q, want p1", posts[0].RootID)
			}
			break
		}
		select {
		case <-handled:
			t.Fatal("a media-only post reached the engine, want it stopped at the adapter")
		case <-deadline:
			t.Fatal("no unsupported-type notice within 5s")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func TestChannelCapabilitiesAndType(t *testing.T) {
	f := newFakeMattermost(t)
	ch := buildChannel(t, f, nil)
	if ch.Type() != TypeMattermost {
		t.Errorf("Type = %q, want %q", ch.Type(), TypeMattermost)
	}
	caps := ch.Capabilities()
	if !caps.Has(channel.CapText) || !caps.Has(channel.CapThreadReply) {
		t.Errorf("capabilities = %s, want text and thread_reply", caps)
	}
	// v1 declares no typing indicator: Mattermost has no REST endpoint for it,
	// so declaring it would promise something the adapter cannot deliver.
	if caps.Has(channel.CapTypingIndicator) {
		t.Error("CapTypingIndicator declared, want it absent in v1")
	}
	if caps.Has(channel.CapAttachment) {
		t.Error("CapAttachment declared, want it absent in v1")
	}
	if err := ch.Disconnect(context.Background()); err != nil {
		t.Errorf("Disconnect = %v, want nil", err)
	}
}

func TestChannelConnectRequiresHandler(t *testing.T) {
	f := newFakeMattermost(t)
	ch := buildChannel(t, f, nil)
	if err := ch.Connect(context.Background()); err == nil {
		t.Fatal("Connect without a handler succeeded, want an error")
	}
}

func TestFactoryRejectsIncompleteConfig(t *testing.T) {
	factory := newMattermostFactory(ChannelDeps{})
	tests := []struct {
		name string
		cfg  installConfig
	}{
		{name: "no token", cfg: installConfig{ServerURL: "https://mm.example.com", BotUserID: "b"}},
		{
			name: "no server url",
			cfg:  installConfig{BotUserID: "b", AccessTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("t"))},
		},
		{
			name: "no bot user id",
			cfg: installConfig{
				ServerURL:            "https://mm.example.com",
				AccessTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("t")),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := factory(channel.Config{Type: TypeMattermost, Raw: raw}); err == nil {
				t.Fatal("factory accepted an incomplete config, want an error")
			}
		})
	}
	if _, err := factory(channel.Config{Type: TypeMattermost, Raw: json.RawMessage("{broken")}); err == nil {
		t.Fatal("factory accepted malformed JSON, want an error")
	}
}

func TestRegisterMattermost(t *testing.T) {
	reg := channel.NewRegistry()
	RegisterMattermost(reg, ChannelDeps{})
	if _, ok := reg.Lookup(TypeMattermost); !ok {
		t.Fatal("factory not registered under TypeMattermost")
	}
}

func TestRootAuthorCache(t *testing.T) {
	c := newRootAuthorCache(2)
	if _, ok := c.get("missing"); ok {
		t.Error("empty cache reported a hit")
	}
	c.put("a", true)
	if v, ok := c.get("a"); !ok || !v {
		t.Errorf("get(a) = %v, %v; want true, true", v, ok)
	}
	// Overflow drops the map wholesale — a miss only costs one REST call, so
	// LRU bookkeeping would buy nothing measurable.
	c.put("b", false)
	c.put("c", true)
	if len(c.entries) > 2 {
		t.Errorf("cache holds %d entries, want the limit respected", len(c.entries))
	}
	// A nil cache is safe to use, so the channel can be constructed without one.
	var nilCache *rootAuthorCache
	if _, ok := nilCache.get("x"); ok {
		t.Error("nil cache reported a hit")
	}
	nilCache.put("x", true)
}

func TestReplyRoot(t *testing.T) {
	if got := replyRoot("root1", "post1"); got != "root1" {
		t.Errorf("replyRoot with a thread = %q, want root1", got)
	}
	// A top-level post becomes its own thread root, so the reply starts a
	// thread under it rather than landing loose in the channel.
	if got := replyRoot("", "post1"); got != "post1" {
		t.Errorf("replyRoot without a thread = %q, want post1", got)
	}
}
