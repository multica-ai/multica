package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// mattermostChannel is ONE installation's WebSocket connection. Mattermost
// exposes a push event stream at /api/v4/websocket, so this is the same shape
// as Feishu's long-conn and Slack's Socket Mode: engine.Supervisor builds one
// per active installation and owns the lease and reconnect lifecycle; Connect
// blocks on the receive loop until the link ends.
type mattermostChannel struct {
	appID       string
	serverURL   string
	botUserID   string
	botUsername string
	token       string
	rest        *restClient
	dialer      *websocket.Dialer
	handler     channel.InboundHandler
	logger      *slog.Logger

	// rootAuthors caches "did the bot author this thread root", so a busy
	// channel costs at most one REST call per thread rather than one per reply.
	rootAuthors *rootAuthorCache
}

const (
	// authChallengeSeq is the sequence number of the one action this client
	// sends. Replies echo it back in seq_reply.
	authChallengeSeq = 1
	// wsHandshakeTimeout bounds the WebSocket upgrade.
	wsHandshakeTimeout = 15 * time.Second
	// wsReadLimit caps one inbound frame. Mattermost posts are far smaller;
	// the limit stops a hostile or broken peer from exhausting memory.
	wsReadLimit = 1 << 20
	// wsPingInterval is how often this client pings. Mattermost's own server
	// pings too, but an outbound ping is what detects a silently dead link
	// (a NAT timeout, a dropped VPN) rather than waiting forever on a read.
	wsPingInterval = 30 * time.Second
	// wsReadTimeout must exceed wsPingInterval so an idle-but-healthy link is
	// never torn down; a missed pong trips it on the next cycle.
	wsReadTimeout = 90 * time.Second
	// wsWriteTimeout bounds a single frame write.
	wsWriteTimeout = 10 * time.Second
	// rootLookupTimeout bounds the thread-root authorship check. It runs
	// inline on the receive loop, so it must be short: a slow answer costs
	// message latency, and the fallback ("not addressed") is the safe verdict.
	rootLookupTimeout = 3 * time.Second
)

// ErrAuthRejected means Mattermost refused the access token on the WebSocket
// authentication challenge. Backoff cannot fix it, but the operator can, so it
// is surfaced distinctly instead of reading as a generic socket failure.
var ErrAuthRejected = errors.New("mattermost: server rejected the bot access token")

func (c *mattermostChannel) Type() channel.Type { return TypeMattermost }

// Capabilities declares the v1 surface. No CapTypingIndicator: Mattermost's
// typing signal is a WebSocket action with no REST equivalent, so delivering it
// would need a cross-replica sender registry (see the design note in
// docs/superpowers/specs/2026-09-01-mattermost-integration-design.md).
// No CapAttachment: v1 ingests and sends text.
func (c *mattermostChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapThreadReply
}

// Disconnect is a no-op: the connection's whole lifetime is scoped to Connect,
// which returns when the run context is cancelled. Mirrors slackChannel and
// telegramChannel.
func (c *mattermostChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts an outbound reply with this installation's token, reusing the
// shared sender (chunking, threading).
func (c *mattermostChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return newSender(c.rest, c.logger).Send(ctx, out)
}

// Connect dials the event stream, authenticates, and runs the receive loop
// until ctx is cancelled (returns nil — graceful stop) or the link fails
// (returns the error — the Supervisor reconnects under backoff).
func (c *mattermostChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("mattermost: inbound handler not configured")
	}
	if c.token == "" {
		return errors.New("mattermost: installation has no access token")
	}

	dialer := c.dialer
	if dialer == nil {
		dialer = &websocket.Dialer{HandshakeTimeout: wsHandshakeTimeout}
	}
	conn, resp, err := dialer.DialContext(ctx, websocketURL(c.serverURL), nil)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("%w (handshake): %v", ErrAuthRejected, err)
			}
			return fmt.Errorf("mattermost: websocket dial: http %d: %w", resp.StatusCode, err)
		}
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("mattermost: websocket dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(wsReadLimit)
	// Every frame and every pong extends the deadline: the link is alive as
	// long as it is saying anything at all.
	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	})

	// Writes are serialized through this mutex: gorilla forbids concurrent
	// writers, and the keepalive pinger writes from its own goroutine.
	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
		return conn.WriteJSON(v)
	}

	// Mattermost's own client authenticates after the upgrade rather than on
	// it, and a bot access token is accepted either way; the challenge is the
	// documented path, so that is the one used here.
	if err := writeJSON(wsAction{
		Seq:    authChallengeSeq,
		Action: "authentication_challenge",
		Data:   map[string]any{"token": c.token},
	}); err != nil {
		return fmt.Errorf("mattermost: send authentication challenge: %w", err)
	}

	// Closing the socket is what unblocks the reader; the pinger exits on its
	// own context and is waited for so no goroutine outlives Connect.
	pingCtx, stopPing := context.WithCancel(ctx)
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	defer func() {
		stopPing()
		_ = conn.Close()
		<-pingDone
	}()

	// Cancellation reaches a blocked ReadJSON only by closing the socket.
	readerDone := make(chan struct{})
	defer close(readerDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-readerDone:
		}
	}()

	for {
		var frame wsFrame
		if err := conn.ReadJSON(&frame); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("mattermost: websocket read: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		if err := c.handleFrame(ctx, frame); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// handleFrame routes one decoded frame. A returned error ends the connection;
// the Supervisor reconnects under backoff.
func (c *mattermostChannel) handleFrame(ctx context.Context, frame wsFrame) error {
	if frame.isReply() {
		if frame.SeqReply == authChallengeSeq && !strings.EqualFold(frame.Status, statusOK) {
			detail := "no detail"
			if frame.Error != nil && frame.Error.Message != "" {
				detail = frame.Error.Message
			}
			return fmt.Errorf("%w: %s", ErrAuthRejected, detail)
		}
		return nil
	}
	switch frame.Event {
	case eventHello:
		c.logger.DebugContext(ctx, "mattermost: connected", "app_id", c.appID)
		return nil
	case eventPosted:
		return c.dispatchPosted(ctx, frame)
	default:
		return nil
	}
}

// dispatchPosted translates one post and hands it to the engine. A non-nil
// handler error is an infrastructure failure and propagates (the Supervisor
// reconnects; Mattermost does not redeliver, but the engine's dedup makes a
// repeat harmless if it ever does). Product drops return nil. Unsupported
// media in a DM, or addressed to the bot in a channel, gets a courteous notice.
func (c *mattermostChannel) dispatchPosted(ctx context.Context, frame wsFrame) error {
	post, data, ok := decodePosted(frame.Data)
	if !ok {
		return nil
	}
	// Resolved before normalization because it decides "addressed", and it may
	// cost a REST call. Skipped entirely for DMs and for posts that already
	// mention the bot.
	rootByBot := false
	if post.RootID != "" && data.ChannelType != "D" && !mentionsBot(post.Message, c.botUsername) {
		rootByBot = c.rootAuthoredByBot(ctx, post.RootID)
	}

	msg, ok := inboundFromPosted(post, data, inboundParams{
		appID:             c.appID,
		botUserID:         c.botUserID,
		botUsername:       c.botUsername,
		rootAuthoredByBot: rootByBot,
	})
	if !ok {
		return nil
	}
	if msg.Type != channel.MsgTypeText {
		if msg.Source.ChatType == channel.ChatTypeP2P || msg.AddressedToBot {
			c.notifyUnsupported(ctx, post)
		}
		return nil
	}
	if msg.Text == "" {
		return nil
	}
	// A group member who replies to someone else AND mentions the bot is
	// pointing at that message on purpose, so it rides along as context. A
	// reply into a bot-rooted thread is a continuation, not a quote, and needs
	// no enrichment.
	if msg.Source.ChatType == channel.ChatTypeGroup && post.RootID != "" && mentionsBot(post.Message, c.botUsername) {
		if quoted, sender, ok := c.lookupQuoted(ctx, post.RootID); ok && quoted.UserID != c.botUserID {
			msg.Text = enrichWithQuotedPost(msg.Text, quoted, sender)
		}
	}
	if err := c.handler(ctx, msg); err != nil {
		c.notifyIssueDispatchError(msg)
		return err
	}
	return nil
}

const (
	issueErrorReplyTimeout  = 5 * time.Second
	issueDispatchFailedText = ":warning: I couldn't create that issue because an internal error occurred. Please try again."
)

// notifyIssueDispatchError keeps an addressed /issue command from failing
// silently when the engine returns an infrastructure error before it can
// produce a normal outcome. Detached from the receive loop so the Supervisor
// can reconnect without waiting on a best-effort notice.
func (c *mattermostChannel) notifyIssueDispatchError(msg channel.InboundMessage) {
	if !isAddressedIssueCommand(msg) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), issueErrorReplyTimeout)
		defer cancel()
		if _, err := c.rest.CreatePost(ctx, Post{
			ChannelID: msg.Source.ChatID,
			RootID:    replyRoot(msg.Source.ThreadID, msg.MessageID),
			Message:   issueDispatchFailedText,
		}); err != nil {
			c.logger.WarnContext(ctx, "mattermost: issue dispatch-error reply failed", "error", err)
		}
	}()
}

// notifyUnsupported tells an interacting sender their non-text post is not
// handled yet, threaded so the notice stays attached in a busy channel.
// Best-effort; failures are logged only.
func (c *mattermostChannel) notifyUnsupported(ctx context.Context, post Post) {
	if _, err := c.rest.CreatePost(ctx, Post{
		ChannelID: post.ChannelID,
		RootID:    replyRoot(post.RootID, post.ID),
		Message:   msgUnsupportedType,
	}); err != nil {
		c.logger.WarnContext(ctx, "mattermost: unsupported-type notice failed", "error", err)
	}
}

// replyRoot picks the thread to reply into: the post's existing thread when it
// has one, otherwise the post itself (which starts a thread under it).
func replyRoot(threadID, postID string) string {
	if threadID != "" {
		return threadID
	}
	return postID
}

// rootAuthoredByBot reports whether this bot wrote the thread root, which is
// what makes a mention-free reply inside a bot-started thread count as
// addressed. A lookup failure answers false: staying quiet is the safe verdict
// when the adapter cannot establish that it was spoken to.
func (c *mattermostChannel) rootAuthoredByBot(ctx context.Context, rootID string) bool {
	if cached, ok := c.rootAuthors.get(rootID); ok {
		return cached
	}
	lookupCtx, cancel := context.WithTimeout(ctx, rootLookupTimeout)
	defer cancel()
	root, err := c.rest.GetPost(lookupCtx, rootID)
	if err != nil {
		c.logger.DebugContext(ctx, "mattermost: thread root lookup failed", "error", err, "root_id", rootID)
		return false
	}
	authored := root.UserID == c.botUserID
	c.rootAuthors.put(rootID, authored)
	return authored
}

// lookupQuoted fetches the post a group member replied to, for quote
// enrichment. Failures are silent: the instruction still reaches the agent,
// just without the quoted context.
func (c *mattermostChannel) lookupQuoted(ctx context.Context, postID string) (Post, string, bool) {
	lookupCtx, cancel := context.WithTimeout(ctx, rootLookupTimeout)
	defer cancel()
	quoted, err := c.rest.GetPost(lookupCtx, postID)
	if err != nil {
		return Post{}, "", false
	}
	sender := ""
	if user, err := c.restUser(lookupCtx, quoted.UserID); err == nil {
		sender = user
	}
	return quoted, sender, true
}

// restUser resolves a user id to a display name for quoted-message
// attribution.
func (c *mattermostChannel) restUser(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", errors.New("mattermost: empty user id")
	}
	var u User
	if err := c.rest.do(ctx, http.MethodGet, "/users/"+userID, nil, &u); err != nil {
		return "", err
	}
	if name := strings.TrimSpace(u.FirstName + " " + u.LastName); name != "" {
		return name, nil
	}
	if u.Nickname != "" {
		return u.Nickname, nil
	}
	return u.Username, nil
}

// rootAuthorCache is a small bounded map of thread root -> "authored by this
// bot". Thread roots are immutable, so an entry never goes stale; the bound
// exists only to keep a long-lived connection in a busy server from growing
// without limit. On overflow the whole map is dropped rather than evicted
// one-by-one: the cost of a miss is one REST call, so LRU bookkeeping would
// buy accuracy nobody can measure.
type rootAuthorCache struct {
	mu      sync.Mutex
	entries map[string]bool
	limit   int
}

const defaultRootAuthorCacheLimit = 2048

func newRootAuthorCache(limit int) *rootAuthorCache {
	if limit <= 0 {
		limit = defaultRootAuthorCacheLimit
	}
	return &rootAuthorCache{entries: make(map[string]bool), limit: limit}
}

func (c *rootAuthorCache) get(key string) (bool, bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *rootAuthorCache) put(key string, value bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.limit {
		c.entries = make(map[string]bool, c.limit)
	}
	c.entries[key] = value
}

// ChannelDeps are the shared dependencies the Mattermost Factory closes over.
// The engine inbound handler is supplied per-build via channel.Config.Handler.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// HTTPClient overrides the REST client (tests). Nil uses a default that
	// refuses redirects.
	HTTPClient *http.Client
	// Dialer overrides the WebSocket dialer (tests). Nil uses a default.
	Dialer *websocket.Dialer
}

// RegisterMattermost registers the per-installation Factory so
// engine.Supervisor builds and supervises one WebSocket connection per active
// Mattermost installation. Same contract as lark.RegisterFeishu,
// slack.RegisterSlack and telegram.RegisterTelegram — no engine edit.
func RegisterMattermost(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeMattermost, newMattermostFactory(deps))
}

func newMattermostFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("mattermost: decode installation config: %w", err)
		}
		token, err := decryptToken(ic.AccessTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("mattermost: decrypt access token: %w", err)
		}
		if token == "" {
			return nil, errors.New("mattermost: installation has no access token")
		}
		if ic.ServerURL == "" {
			return nil, errors.New("mattermost: installation has no server URL")
		}
		if ic.BotUserID == "" {
			return nil, errors.New("mattermost: installation has no bot user id")
		}
		return &mattermostChannel{
			appID:       ic.AppID,
			serverURL:   ic.ServerURL,
			botUserID:   ic.BotUserID,
			botUsername: ic.BotUsername,
			token:       token,
			rest:        newRESTClient(ic.ServerURL, token, deps.HTTPClient),
			dialer:      deps.Dialer,
			handler:     cfg.Handler,
			logger:      logger,
			rootAuthors: newRootAuthorCache(defaultRootAuthorCacheLimit),
		}, nil
	}
}
