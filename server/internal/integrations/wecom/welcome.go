package wecom

// welcome.go — what the bot says when somebody opens the chat for the first
// time.
//
// WeCom pushes an `enter_chat` event the moment a user opens the bot's
// conversation, and expects an `aibot_respond_welcome_msg` echoing that
// frame's req_id. The event type was already named here (eventEnterChat) but
// nothing acted on it, so it fell through to a log line: a first-time user
// opened the bot and got an empty window — no statement of what the bot is
// for, and no hint that they have to link an account before it will answer
// them at all. Every peer platform greets: Slack posts an App Home, Lark
// sends a welcome card.
//
// Two things make this path different from every other outbound write:
//
//   - It is a REPLY, addressed by the req_id of the frame that triggered it.
//     A req_id is meaningless on the next connection, so a greeting that
//     missed its window is not late, it is void — there is nothing worth
//     holding for a reconnect.
//   - The platform gives it about five seconds, after which the chat window
//     is already drawn and the user is typing. It therefore gets its own
//     worker and its own budget rather than queueing behind message
//     callbacks, any one of which can take longer than that on its own.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cmdRespondWelcome is the reply to an enter_chat event.
const cmdRespondWelcome = "aibot_respond_welcome_msg"

// welcomeDeadline is this handler's own budget for the whole greeting —
// lookup, mint and write — held comfortably inside the platform's five
// seconds so a slow database produces no greeting rather than a frame WeCom
// refuses for arriving late.
const welcomeDeadline = 4 * time.Second

// welcomeQueueDepth is small on purpose, and the queue behind it DROPS when
// full rather than blocking. See the routing comment in Connect: a greeting
// that has been waiting is already past its window, so a deep backlog of them
// buys nothing and a shallow one keeps a burst off the read loop.
const welcomeQueueDepth = 8

// The greeting itself. Hardcoded Chinese, the same way every other
// user-visible string in this adapter is (replier.go, the text-only receipt in
// dispatchFrame) — WeCom deployments are China-only and the package has no
// locale layer.
const (
	// welcomeBoundText is the greeting for somebody already linked, and the
	// fallback whenever we cannot establish that they are not. Offering a bind
	// link to a linked user is the confusing outcome, so on doubt we do not.
	welcomeBoundText = "👋 你好，我是 Multica 智能助手。有事直接发消息给我，或者用 “/issue 标题” 建一条任务。（目前只能处理文字消息）"

	// welcomeUnboundPrefix / Suffix wrap the bind URL. The wording matches the
	// needs_binding prompt in replier.go on purpose — a user who sees both
	// should not have to work out whether they are the same link.
	welcomeUnboundPrefix = "👋 你好，我是 Multica 智能助手。请先绑定你的 Multica 账号，才能与我对话：\n"
	welcomeUnboundSuffix = "\n（链接 15 分钟内有效）"
)

// defaultBindingPath is where the web app serves the bind page.
const defaultBindingPath = "/wecom/bind"

// normalizeBindingPath applies the one default, so the greeting and the
// needs_binding prompt cannot drift onto two different bind pages.
func normalizeBindingPath(p string) string {
	if p == "" {
		return defaultBindingPath
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// aibotEnterChat is the enter_chat body: the event envelope plus the fields
// that say who opened which conversation.
type aibotEnterChat struct {
	MsgID    string `json:"msgid"`
	AIBotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
}

// welcomeLookup is the pair of reads the greeting needs: is this person
// already linked, and if not, which workspace is the link for. channel.Config
// carries no workspace id, so the installation row is where that comes from.
// *db.Queries satisfies it.
type welcomeLookup interface {
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// isWelcomeFrame reports whether this frame is an enter_chat event — the one
// event callback that is answered rather than logged, and the one inbound
// frame with a deadline measured in seconds.
func isWelcomeFrame(env frameEnvelope) bool {
	if env.Cmd != cmdEventCallback {
		return false
	}
	var ec aibotEventCallback
	if err := json.Unmarshal(env.Body, &ec); err != nil {
		return false
	}
	return ec.Event.EventType == eventEnterChat
}

// handleEnterChat answers one enter_chat event. It never reports failure: a
// greeting that could not be sent is void, not retriable, and nothing about it
// is worth tearing the connection down for.
//
// GROUP CHATS GET NOTHING, deliberately. Two reasons, either sufficient:
//
//   - Greeting a room is a different act from greeting a person. The event
//     names whoever just walked in, so a greeting in a group is an
//     announcement about that person to an audience, which is not what
//     opening a chat asked for.
//   - The greeting's whole job for an unlinked user is to hand them a bind
//     link, and a binding token is a bearer credential: posted into a room,
//     the first colleague to click owns it, and every later message from the
//     person it was minted for resolves to the hijacker. That is the same
//     reasoning sendBindingPrompt spells out in replier.go, and here it is
//     simpler to obey — a room nobody has spoken in yet has nothing to be
//     told.
func (c *wecomChannel) handleEnterChat(ctx context.Context, env frameEnvelope, sender *wsSender, log *slog.Logger) {
	if ctx.Err() != nil {
		// The connection is going away. This req_id dies with it, so there is
		// nothing to send and nothing to wait for.
		return
	}
	var ev aibotEnterChat
	if err := json.Unmarshal(env.Body, &ev); err != nil {
		log.Warn("wecom: bad enter_chat body", "error", err)
		return
	}
	if ev.ChatType != "" && ev.ChatType != "single" {
		log.Debug("wecom: enter_chat in a group, no greeting", "chat_type", ev.ChatType)
		return
	}
	if ev.From.UserID == "" {
		log.Warn("wecom: enter_chat with no sender", "msg_id", ev.MsgID)
		return
	}
	if env.Headers.ReqID == "" {
		// Nothing to address the reply to. WeCom always sets it; a frame
		// without one is not one we can answer.
		log.Warn("wecom: enter_chat with no req_id", "msg_id", ev.MsgID)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, welcomeDeadline)
	defer cancel()

	text := c.welcomeText(ctx, ev.From.UserID, log)
	if text == "" {
		return
	}
	if err := sender.respondWelcome(ctx, env.Headers.ReqID, text); err != nil {
		// Never retried. The req_id is spent either way, and the user's next
		// message gets the ordinary binding prompt.
		log.Warn("wecom: welcome reply failed", "error", err, "msg_id", ev.MsgID)
	}
}

// welcomeText builds the greeting for one person, or "" when there is nothing
// worth saying.
func (c *wecomChannel) welcomeText(ctx context.Context, wecomUserID string, log *slog.Logger) string {
	if c.welcome == nil || !c.installationID.Valid {
		// No lookup wired — a deployment without the binding surface. Greet
		// without offering a link we cannot mint. Degrading to silence would
		// make the bot look broken over a feature it does not have.
		return welcomeBoundText
	}

	_, err := c.welcome.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: c.installationID,
		ChannelUserID:  wecomUserID,
	})
	switch {
	case err == nil:
		// Already linked: say hello and what to do, no link.
		return welcomeBoundText
	case !errors.Is(err, pgx.ErrNoRows):
		// Could not tell. Offering a link to somebody already linked reads as
		// the bot having lost their account, so on doubt we do not — and we do
		// not mint a token for a user we cannot prove needs one.
		log.WarnContext(ctx, "wecom: welcome binding lookup failed", "error", err)
		return welcomeBoundText
	}

	// Unlinked, and this is a 1:1 chat, so the link goes to the one person it
	// is about.
	if c.binding == nil || c.appURL == "" {
		return welcomeBoundText
	}
	row, err := c.welcome.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          c.installationID,
		ChannelType: channelTypeWecom,
	})
	if err != nil {
		log.WarnContext(ctx, "wecom: welcome could not load its installation", "error", err)
		return welcomeBoundText
	}
	token, err := c.binding.Mint(ctx, row.WorkspaceID, c.installationID, wecomUserID)
	if err != nil {
		log.WarnContext(ctx, "wecom: welcome could not mint a binding token", "error", err)
		return welcomeBoundText
	}
	bindURL := c.appURL + c.bindingPath() + "?token=" + url.QueryEscape(token.Raw)
	return welcomeUnboundPrefix + bindURL + welcomeUnboundSuffix
}

// bindingPath is where the web app serves the bind page. Read through a method
// so a wecomChannel assembled without one still produces a usable URL rather
// than one missing its path entirely.
func (c *wecomChannel) bindingPath() string {
	return normalizeBindingPath(c.bindPath)
}

// respondWelcome writes the reply to an enter_chat frame.
//
// It goes straight to the socket rather than through sendersRegistry: this is
// addressed by req_id, and a req_id means nothing on the next connection, so
// there is no version of this message worth holding for a reconnect.
func (s *wsSender) respondWelcome(ctx context.Context, reqID, content string) error {
	if reqID == "" {
		return errors.New("wecom: respond_welcome requires a req_id")
	}
	if content == "" {
		return errors.New("wecom: respond_welcome requires content")
	}
	_, err := s.requestWithID(ctx, reqID, cmdRespondWelcome, map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	})
	return err
}
