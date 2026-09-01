package mattermost

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// This file holds the translation from a Mattermost "posted" event to the
// engine's normalized channel.InboundMessage. Free functions parameterized by
// the bot identity, mirroring slack/inbound.go and telegram/inbound.go, so the
// per-installation read loop threads in its own bot's id and username.

// mattermostRawEvent carries the Mattermost-specific fields the cross-platform
// envelope does not — read back only inside the Mattermost resolvers.
type mattermostRawEvent struct {
	// AppID routes the message to its installation
	// (config->>'app_id'; see installationKey).
	AppID string `json:"app_id"`
	// EventType is a coarse label for drop audits ("posted").
	EventType string `json:"event_type"`
	// SenderName is the poster's display name, carried for group attribution.
	SenderName string `json:"sender_name,omitempty"`
	// TeamID is the Mattermost team the post belongs to. Empty for DMs and
	// group DMs, which are team-independent.
	TeamID string `json:"team_id,omitempty"`
}

// inboundParams is everything inboundFromPosted needs beyond the event itself.
// rootAuthoredByBot is resolved by the caller (it may cost a REST call), so
// this function stays pure and unit-testable without a network.
type inboundParams struct {
	appID             string
	botUserID         string
	botUsername       string
	rootAuthoredByBot bool
}

// inboundFromPosted normalizes one Mattermost "posted" event. ok=false means
// the event must not reach the core: the bot's own posts, other bots' and
// webhooks' posts (loop guard), system messages, and posts from channels whose
// type the adapter does not ingest.
//
// Group addressing policy: a channel or group-DM message is addressed to the
// bot when it carries an explicit @botusername, or when it is a reply inside a
// thread the bot itself rooted. Direct messages are always addressed.
func inboundFromPosted(post Post, data postedData, p inboundParams) (channel.InboundMessage, bool) {
	if post.UserID == "" || post.UserID == p.botUserID {
		return channel.InboundMessage{}, false
	}
	if isMachinePost(post) {
		return channel.InboundMessage{}, false
	}
	// A non-empty Type is a system event (system_join_channel,
	// system_add_to_team, …). Only ordinary user posts are ingested.
	if post.Type != "" {
		return channel.InboundMessage{}, false
	}
	chatType, ok := mattermostChatType(data.ChannelType)
	if !ok {
		return channel.InboundMessage{}, false
	}

	msgType := classifyPost(post)
	mentioned := mentionsBot(post.Message, p.botUsername)
	addressed := chatType == channel.ChatTypeP2P || mentioned || p.rootAuthoredByBot

	cleaned := strings.TrimSpace(stripBotMentions(post.Message, p.botUsername))
	commandText := cleaned
	forceFresh := false
	if control, ok := engine.ParseControlCommand(cleaned); ok {
		cleaned = control.Body
		forceFresh = control.Kind == engine.ControlCommandFreshSession
	}
	agentText := cleaned

	raw, _ := json.Marshal(mattermostRawEvent{
		AppID:      p.appID,
		EventType:  eventPosted,
		SenderName: senderDisplayName(data),
		TeamID:     data.TeamID,
	})

	var reply *channel.ReplyCtx
	if post.RootID != "" {
		reply = &channel.ReplyCtx{MessageID: post.RootID, RootID: post.RootID}
	}

	return channel.InboundMessage{
		// Mattermost post ids are unique across the server, so the event id and
		// the dedup key are the same value — no compositing, unlike Telegram's
		// per-chat message ids.
		EventID:        post.ID,
		MessageID:      post.ID,
		Type:           msgType,
		Text:           agentText,
		CommandText:    commandText,
		ReplyTo:        reply,
		AddressedToBot: addressed,
		ForceFresh:     forceFresh,
		Source: channel.Source{
			ChannelType: TypeMattermost,
			ChatID:      post.ChannelID,
			ChatType:    chatType,
			SenderID:    post.UserID,
			// Mattermost user ids are unique per server, and one installation is
			// one server, so the per-installation id doubles as the stable id.
			SenderStableID: post.UserID,
			ThreadID:       post.RootID,
		},
		Raw: raw,
	}, true
}

// isMachinePost reports whether a post came from a bot account, an incoming
// webhook, or a slash-command response. Mattermost marks all three in props;
// ingesting them is how two bots in one channel talk each other into a loop.
func isMachinePost(post Post) bool {
	for _, key := range []string{"from_bot", "from_webhook", "from_oauth_app"} {
		if propIsTrue(post.Props[key]) {
			return true
		}
	}
	return false
}

// propIsTrue reads a Mattermost prop flag. They arrive as the STRING "true",
// not a JSON boolean, but tolerate both rather than depend on that.
func propIsTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

// mattermostChatType maps Mattermost's channel type. "D" is a direct message;
// "O" (public), "P" (private) and "G" (group DM) are all multi-party rooms
// where the bot must be addressed explicitly.
func mattermostChatType(t string) (channel.ChatType, bool) {
	switch t {
	case "D":
		return channel.ChatTypeP2P, true
	case "O", "P", "G":
		return channel.ChatTypeGroup, true
	default:
		return "", false
	}
}

// classifyPost maps the post payload to the normalized MsgType. Only text is
// actionable in v1 (aligned with Slack and Telegram). A post that carries both
// text and files is text: the message is what the user wrote, and dropping it
// because an image rode along would be worse than ignoring the image.
func classifyPost(post Post) channel.MsgType {
	if strings.TrimSpace(post.Message) != "" {
		return channel.MsgTypeText
	}
	if len(post.FileIDs) > 0 {
		return channel.MsgTypeFile
	}
	return channel.MsgTypeUnknown
}

// isMentionByte reports whether b can appear inside a Mattermost username.
// Usernames are lower-case alphanumerics plus period, dash and underscore.
func isMentionByte(b byte) bool {
	return b >= 'a' && b <= 'z' ||
		b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' ||
		b == '.' || b == '-' || b == '_'
}

// mentionSpan finds the username token starting at the '@' located at index
// at. It returns the token and the index just past it, or ok=false when the
// '@' begins no username.
//
// Trailing periods, dashes and underscores are trimmed from the token, because
// a username cannot end in one and "@bot." at the end of a sentence is the
// common case. The returned end index still covers the untrimmed run, so the
// caller removing a mention removes the punctuation it consumed.
func mentionSpan(s string, at int) (token string, end int, ok bool) {
	if at < 0 || at >= len(s) || s[at] != '@' {
		return "", 0, false
	}
	// A mention must start the string or follow a non-username byte, so
	// "email@example.com" never reads as a mention of "example.com".
	if at > 0 && isMentionByte(s[at-1]) {
		return "", 0, false
	}
	i := at + 1
	for i < len(s) && isMentionByte(s[i]) {
		i++
	}
	token = strings.TrimRight(s[at+1:i], "._-")
	if token == "" {
		return "", 0, false
	}
	return token, i, true
}

// mentionsBot reports whether the text contains an explicit @botusername.
//
// Matching the literal token rather than the event's "mentions" array is
// deliberate: Mattermost puts every notified user in that array, so @channel
// and @all would make the bot answer messages nobody addressed to it.
func mentionsBot(text, botUsername string) bool {
	if botUsername == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		token, end, ok := mentionSpan(text, i)
		if !ok {
			continue
		}
		if strings.EqualFold(token, botUsername) {
			return true
		}
		i = end - 1
	}
	return false
}

// stripBotMentions removes @botusername tokens so the agent sees the
// instruction rather than its own name. Shared commands such as /clear and
// /issue survive untouched for the engine's command parser.
func stripBotMentions(text, botUsername string) string {
	if botUsername == "" {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	last := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		token, end, ok := mentionSpan(text, i)
		if !ok {
			continue
		}
		if strings.EqualFold(token, botUsername) {
			out.WriteString(text[last:i])
			last = end
		}
		i = end - 1
	}
	out.WriteString(text[last:])
	// Collapse the double space a removed mid-sentence mention leaves behind.
	return strings.Join(strings.Fields(out.String()), " ")
}

// enrichWithQuotedPost prepends the post a group member explicitly selected by
// replying to it while mentioning the bot. Ambient channel history never
// enters the agent context — only the one message the sender pointed at.
// CommandText stays the sender's own cleaned instruction, so a command inside
// the quoted message stays historical.
func enrichWithQuotedPost(instruction string, quoted Post, quotedSender string) string {
	quotedText := strings.TrimSpace(quoted.Message)
	if quotedText == "" {
		quotedText = "[empty or non-text message]"
	}
	sender := strings.TrimSpace(quotedSender)
	if sender == "" {
		sender = "Unknown user"
	}
	block := fmt.Sprintf("<quoted_message message_id=%q sender=%q type=%q>\n%s\n</quoted_message>",
		quoted.ID, sender, classifyPost(quoted), quotedText)
	if instruction == "" {
		return block
	}
	return block + "\n\n" + instruction
}

// isAddressedIssueCommand reports whether an inbound message is an /issue
// command aimed at the bot. Used to decide whether a drop deserves a spoken
// refusal instead of silence.
func isAddressedIssueCommand(msg channel.InboundMessage) bool {
	if !msg.AddressedToBot {
		return false
	}
	source := msg.CommandText
	if source == "" {
		source = msg.Text
	}
	_, ok := engine.ParseIssueCommand(source)
	return ok
}
