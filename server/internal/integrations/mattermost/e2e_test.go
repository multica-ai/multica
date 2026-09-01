//go:build mattermoste2e

// This suite runs the Mattermost adapter against a REAL Mattermost server.
//
// It exists because everything else in this package tests the adapter against
// fixtures the adapter's own author wrote. That proves the code does what he
// thought Mattermost does; it cannot catch him being wrong about Mattermost.
// The wire details most likely to be wrong — the double-encoded post payload,
// the authentication-challenge reply frame, which channel_type a DM reports,
// how a bot account is flagged — are exactly the ones only a live server can
// settle.
//
// It is behind the `mattermoste2e` build tag AND an env gate, so `make test`
// never reaches the network.
//
// Run it:
//
//	eval "$(./scripts/mattermost-e2e-up.sh)"
//	(cd server && go test -tags=mattermoste2e ./internal/integrations/mattermost/ -run E2E -v -count=1)
//	./scripts/mattermost-e2e-down.sh

package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// e2eEnv is the connection block scripts/mattermost-e2e-up.sh prints.
type e2eEnv struct {
	URL         string
	BotToken    string
	BotUserID   string
	BotUsername string
	HumanToken  string
	HumanUserID string
	ChannelID   string
	DMChannelID string
	AdminToken  string
}

func requireE2E(t *testing.T) e2eEnv {
	t.Helper()
	if os.Getenv("MULTICA_MM_E2E") != "1" {
		t.Skip("set MULTICA_MM_E2E=1 (see scripts/mattermost-e2e-up.sh) to run the live suite")
	}
	env := e2eEnv{
		URL:         os.Getenv("MULTICA_MM_E2E_URL"),
		BotToken:    os.Getenv("MULTICA_MM_E2E_BOT_TOKEN"),
		BotUserID:   os.Getenv("MULTICA_MM_E2E_BOT_USER_ID"),
		BotUsername: os.Getenv("MULTICA_MM_E2E_BOT_USERNAME"),
		HumanToken:  os.Getenv("MULTICA_MM_E2E_HUMAN_TOKEN"),
		HumanUserID: os.Getenv("MULTICA_MM_E2E_HUMAN_USER_ID"),
		ChannelID:   os.Getenv("MULTICA_MM_E2E_CHANNEL_ID"),
		DMChannelID: os.Getenv("MULTICA_MM_E2E_DM_CHANNEL_ID"),
		AdminToken:  os.Getenv("MULTICA_MM_E2E_ADMIN_TOKEN"),
	}
	for name, value := range map[string]string{
		"MULTICA_MM_E2E_URL":           env.URL,
		"MULTICA_MM_E2E_BOT_TOKEN":     env.BotToken,
		"MULTICA_MM_E2E_BOT_USER_ID":   env.BotUserID,
		"MULTICA_MM_E2E_BOT_USERNAME":  env.BotUsername,
		"MULTICA_MM_E2E_HUMAN_TOKEN":   env.HumanToken,
		"MULTICA_MM_E2E_CHANNEL_ID":    env.ChannelID,
		"MULTICA_MM_E2E_DM_CHANNEL_ID": env.DMChannelID,
	} {
		if value == "" {
			t.Fatalf("%s is unset — re-run scripts/mattermost-e2e-up.sh", name)
		}
	}
	return env
}

// postAs publishes a post to the live server using an arbitrary token, so the
// suite can speak as the human user rather than as the bot.
func postAs(t *testing.T, env e2eEnv, token string, p Post) Post {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, env.URL+apiPath+"/posts", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post as user: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		t.Fatalf("post as user: http %d: %s", resp.StatusCode, raw)
	}
	var created Post
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode created post: %v", err)
	}
	return created
}

// liveChannel wires the real adapter to the real server and collects every
// InboundMessage the engine handler would have received.
type liveChannel struct {
	ch  channel.Channel
	mu  sync.Mutex
	got []channel.InboundMessage
	// connErr carries Connect's return value once the run context is cancelled.
	connErr chan error
	cancel  context.CancelFunc
}

func startLiveChannel(t *testing.T, env e2eEnv) *liveChannel {
	t.Helper()
	lc := &liveChannel{connErr: make(chan error, 1)}

	// Build through the real Factory so the config-decode path is covered too.
	// The token is stored plaintext here because deps.Decrypt is nil; the
	// production path seals it with secretbox (see install.go).
	cfg, err := json.Marshal(installConfig{
		AppID:                installationKey(env.URL, env.BotUserID),
		ServerURL:            env.URL,
		BotUserID:            env.BotUserID,
		BotUsername:          env.BotUsername,
		AccessTokenEncrypted: base64Std(env.BotToken),
	})
	if err != nil {
		t.Fatal(err)
	}
	built, err := newMattermostFactory(ChannelDeps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})(channel.Config{
		Type: TypeMattermost,
		Raw:  cfg,
		Handler: func(_ context.Context, msg channel.InboundMessage) error {
			lc.mu.Lock()
			lc.got = append(lc.got, msg)
			lc.mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	lc.ch = built

	ctx, cancel := context.WithCancel(context.Background())
	lc.cancel = cancel
	go func() { lc.connErr <- built.Connect(ctx) }()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-lc.connErr:
			// Cancellation is a graceful stop; anything else is a real failure
			// worth surfacing even during teardown.
			if err != nil {
				t.Errorf("Connect returned %v after cancellation, want nil", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("Connect did not return within 15s of cancellation")
		}
	})

	// Block until the socket has actually authenticated and is delivering.
	//
	// Connect dials and sends the authentication challenge asynchronously, so a
	// post published straight after this function returns can be produced before
	// the server has subscribed this client — and Mattermost does not replay it.
	// On localhost the connection usually wins that race, which is worse than
	// losing it: the suite would pass here and flake in CI. Posting a probe and
	// waiting for it to come back proves the whole receive path is live.
	probe := postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		Message:   fmt.Sprintf("@%s readiness probe", env.BotUsername),
	})
	if _, ok := lc.await(t, probe.ID, 60*time.Second); !ok {
		t.Fatal("the websocket never delivered the readiness probe; the connection is not usable")
	}

	return lc
}

func base64Std(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	src := []byte(s)
	var out strings.Builder
	for i := 0; i < len(src); i += 3 {
		var buf [3]byte
		n := copy(buf[:], src[i:])
		out.WriteByte(alphabet[buf[0]>>2])
		out.WriteByte(alphabet[(buf[0]&0x03)<<4|buf[1]>>4])
		if n > 1 {
			out.WriteByte(alphabet[(buf[1]&0x0f)<<2|buf[2]>>6])
		} else {
			out.WriteByte('=')
		}
		if n > 2 {
			out.WriteByte(alphabet[buf[2]&0x3f])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

// await polls the collected messages for the post id, so a test states what it
// is waiting for rather than sleeping a guessed interval.
func (lc *liveChannel) await(t *testing.T, postID string, timeout time.Duration) (channel.InboundMessage, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		lc.mu.Lock()
		for _, m := range lc.got {
			if m.MessageID == postID {
				lc.mu.Unlock()
				return m, true
			}
		}
		lc.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
	}
	return channel.InboundMessage{}, false
}

// seen reports whether a post id ever reached the handler. Used for the
// negative cases, where the answer is "it must not".
func (lc *liveChannel) seen(postID string) bool {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, m := range lc.got {
		if m.MessageID == postID {
			return true
		}
	}
	return false
}

// ---- install-time validation ------------------------------------------------

func TestE2EGetMeIdentifiesTheBot(t *testing.T) {
	env := requireE2E(t)

	me, err := newRESTClient(env.URL, env.BotToken, nil).GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe against the live server: %v", err)
	}
	if me.ID != env.BotUserID {
		t.Errorf("bot user id = %q, want %q", me.ID, env.BotUserID)
	}
	if me.Username != env.BotUsername {
		t.Errorf("bot username = %q, want %q", me.Username, env.BotUsername)
	}
	// The whole "is this a bot account" install gate rests on this flag being
	// set for a real bot. If Mattermost ever stopped setting it, install would
	// reject every legitimate bot — so assert it against the real server.
	if !me.IsBot {
		t.Error("IsBot = false for a real bot account, want true")
	}
}

// A personal access token must be refused: the account's own posts would come
// back in as inbound and the bot would answer itself.
func TestE2EPersonalTokenIsNotABotAccount(t *testing.T) {
	env := requireE2E(t)

	me, err := newRESTClient(env.URL, env.HumanToken, nil).GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe with a human session token: %v", err)
	}
	if me.IsBot {
		t.Fatalf("IsBot = true for the human user %q, want false", me.Username)
	}
}

func TestE2EBadTokenIsRejectedNotUnverifiable(t *testing.T) {
	env := requireE2E(t)

	_, err := newRESTClient(env.URL, "definitely-not-a-real-token", nil).GetMe(context.Background())
	if err == nil {
		t.Fatal("GetMe with a bogus token succeeded, want an error")
	}
	// The install handler tells the operator to rotate the credential only on
	// this classification; anything else says "check your network" instead.
	classified := classifyCredentialVerificationError(err)
	if !strings.Contains(classified.Error(), ErrCredentialsRejected.Error()) {
		t.Fatalf("classified as %v, want a credentials-rejected error (status was %d)",
			classified, statusOf(err))
	}
}

func TestE2EUnreachableServerIsUnverifiable(t *testing.T) {
	requireE2E(t)

	// Port 1 is reserved and never listening: a connection error, not an HTTP
	// status, which must classify as "could not reach", never as "bad token".
	_, err := newRESTClient("http://127.0.0.1:1", "tok", nil).GetMe(context.Background())
	if err == nil {
		t.Fatal("GetMe against a dead port succeeded, want an error")
	}
	classified := classifyCredentialVerificationError(err)
	if !strings.Contains(classified.Error(), ErrCredentialsUnverifiable.Error()) {
		t.Fatalf("classified as %v, want a credentials-unverifiable error", classified)
	}
}

// ---- inbound over the real WebSocket ---------------------------------------

func TestE2EChannelMentionIsAddressed(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	body := fmt.Sprintf("@%s please look at this", env.BotUsername)
	posted := postAs(t, env, env.HumanToken, Post{ChannelID: env.ChannelID, Message: body})

	msg, ok := lc.await(t, posted.ID, 30*time.Second)
	if !ok {
		t.Fatal("the mention never reached the engine handler")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("ChatType = %q, want group for a public channel", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Error("AddressedToBot = false for an explicit mention, want true")
	}
	if msg.Text != "please look at this" {
		t.Errorf("Text = %q, want the mention stripped", msg.Text)
	}
	if msg.Source.ChatID != env.ChannelID {
		t.Errorf("ChatID = %q, want %q", msg.Source.ChatID, env.ChannelID)
	}
	if msg.Source.SenderID != env.HumanUserID {
		t.Errorf("SenderID = %q, want the human %q", msg.Source.SenderID, env.HumanUserID)
	}
	// Raw carries the routing key the installation resolver reads back.
	var raw mattermostRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatalf("Raw is not decodable: %v", err)
	}
	if want := installationKey(env.URL, env.BotUserID); raw.AppID != want {
		t.Errorf("Raw.AppID = %q, want %q", raw.AppID, want)
	}
	if raw.SenderName == "" {
		t.Error("Raw.SenderName is empty; the live server does send sender_name")
	}
	if strings.HasPrefix(raw.SenderName, "@") {
		t.Errorf("Raw.SenderName = %q, want the leading @ stripped", raw.SenderName)
	}
}

func TestE2EDirectMessageNeedsNoMention(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	posted := postAs(t, env, env.HumanToken, Post{ChannelID: env.DMChannelID, Message: "hello with no mention"})

	msg, ok := lc.await(t, posted.ID, 30*time.Second)
	if !ok {
		t.Fatal("the direct message never reached the engine handler")
	}
	// The DM discriminator is "D" — this is the assertion that would catch the
	// adapter guessing the wrong letter.
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("ChatType = %q, want p2p for a direct message", msg.Source.ChatType)
	}
	if !msg.AddressedToBot {
		t.Error("AddressedToBot = false in a DM, want true")
	}
	if msg.Text != "hello with no mention" {
		t.Errorf("Text = %q, want it unchanged", msg.Text)
	}
}

func TestE2EUnaddressedChannelChatterIsNotAddressed(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	posted := postAs(t, env, env.HumanToken, Post{ChannelID: env.ChannelID, Message: "just talking to the team"})

	msg, ok := lc.await(t, posted.ID, 30*time.Second)
	if !ok {
		t.Fatal("the post never reached the engine handler")
	}
	// It is ingested (the engine decides what to do with it) but must not be
	// addressed, or the bot would answer every message in the channel.
	if msg.AddressedToBot {
		t.Error("AddressedToBot = true for unaddressed chatter, want false")
	}
}

// @channel notifies everyone. If addressing read Mattermost's `mentions` array
// instead of the message text, this would wake the bot.
func TestE2EBroadcastMentionDoesNotAddressTheBot(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	posted := postAs(t, env, env.HumanToken, Post{ChannelID: env.ChannelID, Message: "@channel standup in five"})

	msg, ok := lc.await(t, posted.ID, 30*time.Second)
	if !ok {
		t.Fatal("the broadcast post never reached the engine handler")
	}
	if msg.AddressedToBot {
		t.Error("AddressedToBot = true for @channel, want false")
	}
}

// A mention-free reply inside a thread the bot itself rooted must count as
// addressed. This is the one addressing rule that costs a REST lookup, and the
// only way to prove it is against a server that really threads posts.
func TestE2EReplyInBotRootedThreadIsAddressed(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	root := postAs(t, env, env.BotToken, Post{ChannelID: env.ChannelID, Message: "here is what I found"})
	reply := postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		RootID:    root.ID,
		Message:   "can you expand on that?",
	})

	msg, ok := lc.await(t, reply.ID, 30*time.Second)
	if !ok {
		t.Fatal("the thread reply never reached the engine handler")
	}
	if !msg.AddressedToBot {
		t.Error("AddressedToBot = false for a reply in a bot-rooted thread, want true")
	}
	if msg.Source.ThreadID != root.ID {
		t.Errorf("ThreadID = %q, want the root %q", msg.Source.ThreadID, root.ID)
	}
}

func TestE2EReplyInHumanRootedThreadIsNotAddressed(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	root := postAs(t, env, env.HumanToken, Post{ChannelID: env.ChannelID, Message: "team discussion"})
	reply := postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		RootID:    root.ID,
		Message:   "agreed",
	})

	msg, ok := lc.await(t, reply.ID, 30*time.Second)
	if !ok {
		t.Fatal("the thread reply never reached the engine handler")
	}
	if msg.AddressedToBot {
		t.Error("AddressedToBot = true for a human-rooted thread, want false")
	}
}

// The bot's own posts must never come back in, or every reply starts a new turn.
func TestE2EBotOwnPostIsSuppressed(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)

	own := postAs(t, env, env.BotToken, Post{ChannelID: env.ChannelID, Message: "a reply from the bot itself"})

	// Give the event a generous window to arrive and be (correctly) dropped,
	// then post a human message and wait for THAT: once the later post has been
	// delivered, the earlier one has definitively been processed.
	marker := postAs(t, env, env.HumanToken, Post{
		ChannelID: env.ChannelID,
		Message:   fmt.Sprintf("@%s marker", env.BotUsername),
	})
	if _, ok := lc.await(t, marker.ID, 30*time.Second); !ok {
		t.Fatal("the marker post never arrived, so the suppression check is inconclusive")
	}
	if lc.seen(own.ID) {
		t.Error("the bot's own post reached the engine handler, want it suppressed")
	}
}

// ---- outbound against the real server --------------------------------------

func TestE2ESendPostsAndThreads(t *testing.T) {
	env := requireE2E(t)
	rest := newRESTClient(env.URL, env.BotToken, nil)
	s := newSender(rest, slog.New(slog.NewTextHandler(io.Discard, nil)))

	root := postAs(t, env, env.HumanToken, Post{ChannelID: env.ChannelID, Message: "a question"})

	result, err := s.Send(context.Background(), channel.OutboundMessage{
		ChatID:   env.ChannelID,
		Text:     "**bold** answer with `code`",
		ThreadID: root.ID,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.MessageID == "" {
		t.Fatal("Send returned no post id")
	}

	got, err := rest.GetPost(context.Background(), result.MessageID)
	if err != nil {
		t.Fatalf("GetPost: %v", err)
	}
	if got.RootID != root.ID {
		t.Errorf("RootID = %q, want the reply threaded under %q", got.RootID, root.ID)
	}
	// Mattermost renders Markdown natively, so the adapter sends it verbatim;
	// this asserts the server stored it unmangled.
	if got.Message != "**bold** answer with `code`" {
		t.Errorf("stored message = %q, want the Markdown verbatim", got.Message)
	}
	if got.UserID != env.BotUserID {
		t.Errorf("author = %q, want the bot %q", got.UserID, env.BotUserID)
	}
}

// A reply longer than one post must arrive as several posts in one thread,
// not be silently truncated or rejected by the server.
func TestE2ESendChunksLongReplyIntoOneThread(t *testing.T) {
	env := requireE2E(t)
	rest := newRESTClient(env.URL, env.BotToken, nil)
	s := newSender(rest, slog.New(slog.NewTextHandler(io.Discard, nil)))

	long := strings.TrimRight(strings.Repeat("word ", maxPostRunes/2), " ")
	result, err := s.Send(context.Background(), channel.OutboundMessage{ChatID: env.ChannelID, Text: long})
	if err != nil {
		t.Fatalf("Send long reply: %v", err)
	}
	if len(result.MessageIDs) < 2 {
		t.Fatalf("delivered %d posts, want the reply split across several", len(result.MessageIDs))
	}

	first, err := rest.GetPost(context.Background(), result.MessageIDs[0])
	if err != nil {
		t.Fatalf("GetPost first chunk: %v", err)
	}
	if first.RootID != "" {
		t.Errorf("first chunk RootID = %q, want empty (it anchors the thread)", first.RootID)
	}
	for _, id := range result.MessageIDs[1:] {
		p, err := rest.GetPost(context.Background(), id)
		if err != nil {
			t.Fatalf("GetPost chunk %s: %v", id, err)
		}
		if p.RootID != first.ID {
			t.Errorf("chunk %s RootID = %q, want it threaded under %q", id, p.RootID, first.ID)
		}
	}
}

// The unsupported-media notice is the adapter's own outbound path, invoked from
// the receive loop rather than from Send.
func TestE2EUnsupportedMediaGetsANoticeInDM(t *testing.T) {
	env := requireE2E(t)
	lc := startLiveChannel(t, env)
	rest := newRESTClient(env.URL, env.BotToken, nil)

	// A post with neither text nor files classifies as unknown, which is the
	// same branch a files-only post takes — and needs no upload to produce.
	posted := postAs(t, env, env.HumanToken, Post{ChannelID: env.DMChannelID, Message: ""})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if replied := findBotReplyUnder(t, rest, env, posted.ID); replied != "" {
			if replied != msgUnsupportedType {
				t.Fatalf("notice = %q, want the unsupported-type copy", replied)
			}
			if lc.seen(posted.ID) {
				t.Error("a non-text post reached the engine, want it stopped at the adapter")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("no unsupported-type notice appeared within 30s")
}

// findBotReplyUnder returns the bot's reply text in rootID's thread, or "".
func findBotReplyUnder(t *testing.T, rest *restClient, env e2eEnv, rootID string) string {
	t.Helper()
	var thread struct {
		Order []string        `json:"order"`
		Posts map[string]Post `json:"posts"`
	}
	if err := rest.do(context.Background(), http.MethodGet, "/posts/"+rootID+"/thread", nil, &thread); err != nil {
		return ""
	}
	for _, p := range thread.Posts {
		if p.ID != rootID && p.UserID == env.BotUserID {
			return p.Message
		}
	}
	return ""
}
