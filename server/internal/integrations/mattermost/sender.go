package mattermost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// User-facing copy the bot speaks in Mattermost. English, matching Slack,
// Telegram and DingTalk word for word wherever they say the same thing.
//
// The language is chosen per channel by who is on the other end (MUL-6509,
// #7349). WeCom and Lark are Chinese-market products and their adapters keep
// Chinese copy on purpose. Mattermost is a self-hosted product with a
// worldwide install base and no single dominant locale, so it follows Slack
// and Telegram into English.
//
// English rather than a locale lookup because this bot speaks before it knows
// who it is speaking to: the binding prompt goes to an unbound sender, and
// user.language is only ever written by the settings language switcher, so it
// is NULL for anyone who never opened it. English is the product default
// (DEFAULT_LOCALE in packages/core/i18n/types.ts).
const (
	msgAgentOffline     = ":warning: The agent is offline right now. Your message was received and will be handled once it's back online."
	msgAgentArchived    = ":warning: This agent has been archived and can't respond. Please contact your workspace admin."
	msgUnsupportedType  = "Sorry, I can't handle this kind of message yet. Please send text."
	msgBindingGroupHint = "Please message me in a direct chat first, then link your Multica account."
)

// maxPostRunes caps one outbound post. Mattermost's server-side MaxPostSize
// defaults to 4000 characters and an operator may lower it; 3800 leaves
// headroom without a round trip to read the server config, which a bot account
// is not guaranteed permission to do.
const maxPostRunes = 3800

// sender posts agent replies back to Mattermost. Outbound half only; the
// installation identity is resolved per message by the Router.
type sender struct {
	rest   *restClient
	logger *slog.Logger
}

func newSender(rest *restClient, logger *slog.Logger) *sender {
	if logger == nil {
		logger = slog.Default()
	}
	return &sender{rest: rest, logger: logger}
}

// Send delivers a text reply, chunked under the per-post cap. Mattermost
// renders Markdown natively, so the agent's output goes out verbatim — no
// conversion pass, and no conversion bug class either.
//
// The returned SendResult carries every post id, so the caller can record
// provenance for each one; MessageID holds the last, which is the anchor a
// follow-up would thread under.
func (s *sender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if s.rest == nil {
		return channel.SendResult{}, errors.New("mattermost: api client not configured")
	}
	if out.ChatID == "" {
		return channel.SendResult{}, errors.New("mattermost: outbound message has no channel id")
	}
	if strings.TrimSpace(out.Text) == "" {
		return channel.SendResult{}, errors.New("mattermost: refusing to post an empty message")
	}
	// ReplyTo threads under a specific post; ThreadID threads into an existing
	// root. Mattermost has one anchor for both, so ThreadID wins when set
	// (it is the session's established root) and ReplyTo is the fallback.
	root := out.ThreadID
	if root == "" {
		root = out.ReplyTo
	}

	var ids []string
	for _, chunk := range chunkMessage(out.Text, maxPostRunes) {
		created, err := s.rest.CreatePost(ctx, Post{
			ChannelID: out.ChatID,
			RootID:    root,
			Message:   chunk,
		})
		if err != nil {
			// Partial delivery is reported as failure with the ids that did
			// land, so the caller records provenance for what the user can
			// actually see rather than pretending nothing was posted.
			return channel.SendResult{MessageIDs: ids}, fmt.Errorf("mattermost: create post: %w", err)
		}
		ids = append(ids, created.ID)
		// Later chunks join the thread the first one anchored, so a multi-part
		// reply reads as one conversation instead of several loose posts.
		if root == "" {
			root = created.ID
		}
	}
	if len(ids) == 0 {
		return channel.SendResult{}, errors.New("mattermost: nothing to send")
	}
	return channel.SendResult{MessageID: ids[len(ids)-1], MessageIDs: ids}, nil
}

// chunkMessage splits text into pieces of at most maxRunes runes, preferring a
// newline break so paragraphs and fenced code blocks split cleanly. Mattermost
// counts characters, not UTF-16 code units, so this counts runes.
func chunkMessage(text string, maxRunes int) []string {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return []string{text}
	}
	var chunks []string
	for len(runes) > 0 {
		end := maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		// Prefer the last newline in the window, but only when it leaves a
		// substantial first chunk rather than a trail of fragments.
		if end < len(runes) {
			if i := lastIndexRune(runes[:end], '\n'); i > maxRunes/2 {
				end = i + 1
			}
		}
		chunks = append(chunks, strings.TrimRight(string(runes[:end]), "\n"))
		runes = runes[end:]
	}
	return chunks
}

func lastIndexRune(rs []rune, target rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i] == target {
			return i
		}
	}
	return -1
}
