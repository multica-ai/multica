package infoflow

// card.go — rich-card delivery: the adapter's single entry point for the
// platform's audited "dynamic card" (AI 动态卡片, scheme=1) with a Markdown
// message as the fallback.
//
// Why a unified entry point at all. The platform offers two ways to render a
// formatted answer:
//
//   - the AI dynamic card (client.Bot.IM.DynamicCard): a real card with a
//     component tree the client renders, editable afterwards, and — because it
//     carries an app's interactive surface — gated behind the platform's
//     audit/enable step for that robot. A robot whose card capability has not
//     been approved, or a deployment where it is not enabled, gets a hard
//     rejection on send.
//   - a Markdown message (msgtype "md" / MD group body): no audit, available
//     to every robot, and the richest thing some group envelopes accept.
//
// Callers here should not have to know which one is live. sendReply decides, and
// if the card is refused it degrades to the Markdown message rather than losing
// the answer. That is exactly the shape channel.Capability documents:
// CapRichCard is a declaration, and the adapter is what makes it true.
//
// The card is deliberately a *snapshot*, not a stream. A streaming card
// (im.StreamingCardSession) needs a token round trip plus a flush per delta and
// buys the user a live-typing view; the engine already publishes the answer once,
// complete, and an interrupted stream leaves a half-rendered card with nothing
// to distinguish it from a real answer. So the card is sent whole, and the
// capability that would need a stream — an edit after send — is not declared.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dolfly/infoflow"
	"github.com/dolfly/infoflow/bot/dynamiccard"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// cardRoute records which wire route a card actually took. It travels back to
// the caller and into the log line, so "why did this answer render as plain
// markdown?" is answerable from the log rather than from a guess.
type cardRoute string

const (
	// cardRouteDynamic is the AI dynamic card (scheme=1).
	cardRouteDynamic cardRoute = "dynamic_card"
	// cardRouteMarkdown is the Markdown message fallback.
	cardRouteMarkdown cardRoute = "markdown"
)

// cardMarkdownComponentID is the id the card's single Markdown component carries.
// It is fixed because the card is a snapshot: nothing ever updates one component
// in place, so the id only has to be stable within the card.
const cardMarkdownComponentID = "multica-answer"

// cardDelivery is what happened: the route, and the platform's handle for the
// message that route created.
type cardDelivery struct {
	Route     cardRoute
	CardID    string
	MessageID string
}

// cardSender is the unified entry point. sendReply calls it; a test substitutes
// it to assert which route a given answer took without a network.
type cardSender interface {
	Send(ctx context.Context, target sendTarget, markdown string) (cardDelivery, error)
}

// sdkCardSender is the production cardSender. Its two routes are function
// fields rather than direct calls so the fallback is a fact about the data
// rather than something a test has to reach a network to observe.
type sdkCardSender struct {
	sendDynamic  func(ctx context.Context, target sendTarget, markdown string) (dynamiccard.CardRef, error)
	sendMarkdown func(ctx context.Context, target sendTarget, markdown string) (channel.SendResult, error)
	logger       *slog.Logger
}

var _ cardSender = sdkCardSender{}

// newSDKCardSender builds the production routes over one client.
func newSDKCardSender(client *infoflow.Client, logger *slog.Logger) sdkCardSender {
	if logger == nil {
		logger = slog.Default()
	}
	return sdkCardSender{
		sendDynamic: func(ctx context.Context, target sendTarget, markdown string) (dynamiccard.CardRef, error) {
			return sendDynamicCard(ctx, client, target, markdown)
		},
		sendMarkdown: func(ctx context.Context, target sendTarget, markdown string) (channel.SendResult, error) {
			return sendMarkdown(ctx, client, target, markdown)
		},
		logger: logger,
	}
}

// newCardSenderFor builds the production card sender from decrypted credentials.
func newCardSenderFor(creds credentials, logger *slog.Logger) (cardSender, error) {
	client, err := newSDKClient(creds)
	if err != nil {
		return nil, err
	}
	return newSDKCardSender(client, logger), nil
}

// Send delivers markdown as a rich card. The dynamic card goes first; a
// rejection — the robot has not been audited for the card capability, the
// deployment has it disabled, or the content is past the platform's card budget
// — degrades to the Markdown message, which every robot can send.
//
// The returned error is therefore only about the fallback: if the Markdown
// message also failed, the answer is genuinely undelivered and the caller must
// report it.
func (s sdkCardSender) Send(ctx context.Context, target sendTarget, markdown string) (cardDelivery, error) {
	if strings.TrimSpace(markdown) == "" {
		return cardDelivery{}, fmt.Errorf("infoflow: empty card content")
	}
	if s.sendDynamic == nil || s.sendMarkdown == nil {
		return cardDelivery{}, fmt.Errorf("infoflow: card sender is not configured")
	}
	ref, cardErr := s.sendDynamic(ctx, target, markdown)
	if cardErr == nil {
		return cardDelivery{Route: cardRouteDynamic, CardID: ref.CardID, MessageID: ref.MessageID}, nil
	}
	if s.logger != nil {
		// The card's own error text is platform detail; log it as the reason the
		// fallback was used, and never let it fail the reply.
		s.logger.Warn("infoflow: dynamic card refused; falling back to a markdown message",
			"chat_id", target.ChatID, "error", cardErr)
	}
	sent, err := s.sendMarkdown(ctx, target, markdown)
	if err != nil {
		return cardDelivery{}, fmt.Errorf("send markdown message (dynamic card failed: %v): %w", cardErr, err)
	}
	return cardDelivery{Route: cardRouteMarkdown, MessageID: sent.MessageID}, nil
}

// sendDynamicCard posts one scheme=1 dynamic card whose body is the answer.
func sendDynamicCard(ctx context.Context, client *infoflow.Client, target sendTarget, markdown string) (dynamiccard.CardRef, error) {
	if client == nil {
		return dynamiccard.CardRef{}, fmt.Errorf("infoflow client is not configured")
	}
	content := dynamiccard.NewContent(dynamiccard.NewMarkdown(cardMarkdownComponentID, markdown))
	request := dynamiccard.SendRequest{Content: content}
	switch target.ChatType {
	case channel.ChatTypeGroup:
		request.GroupID = target.ChatID
	default:
		request.ToUser = target.ChatID
	}
	return client.Bot.IM.DynamicCard.Send(ctx, request)
}

// replySender is the seam outbound.go and the OutboundReplier share, so every
// place that posts an answer to a user makes the same card-or-text decision.
// It holds one client: a card and its fallback are the same robot talking.
type replySender struct {
	text  func(ctx context.Context, target sendTarget, content string) (channel.SendResult, error)
	cards cardSender
}

func newReplySender(creds credentials, logger *slog.Logger) (replySender, error) {
	client, err := newSDKClient(creds)
	if err != nil {
		return replySender{}, err
	}
	return replySender{
		text: func(ctx context.Context, target sendTarget, content string) (channel.SendResult, error) {
			return sendText(ctx, client, target, content)
		},
		cards: newSDKCardSender(client, logger),
	}, nil
}

// send delivers one answer: a rich card when the content has Markdown worth
// rendering, a plain text message otherwise.
func (r replySender) send(ctx context.Context, target sendTarget, content string) (channel.SendResult, error) {
	if strings.TrimSpace(content) == "" {
		return channel.SendResult{}, fmt.Errorf("infoflow: empty reply")
	}
	if !wantsCard(content) || r.cards == nil {
		if r.text == nil {
			return channel.SendResult{}, fmt.Errorf("infoflow: text sender is not configured")
		}
		return r.text(ctx, target, content)
	}
	delivery, err := r.cards.Send(ctx, target, content)
	if err != nil {
		return channel.SendResult{}, err
	}
	return channel.SendResult{MessageID: delivery.MessageID}, nil
}

// wantsCard reports whether an answer carries Markdown that a card would render
// and a plain text message would not. The list is deliberately conservative: a
// false positive costs a card (and, for a robot without the capability, one
// rejected send), while a false negative only costs the user the formatting.
func wantsCard(content string) bool {
	if strings.Contains(content, "```") {
		return true
	}
	if strings.Contains(content, "**") || strings.Contains(content, "__") {
		return true
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"), // heading
			strings.HasPrefix(trimmed, "> "), // quote
			strings.HasPrefix(trimmed, "- "), // bullet
			strings.HasPrefix(trimmed, "* "), // bullet
			strings.HasPrefix(trimmed, "+ "): // bullet
			return true
		}
		// A table row is only a table in company; a single "|" line is prose.
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			return true
		}
		// Ordered list, "1. " at the start of a line.
		if len(trimmed) > 3 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if dot := strings.Index(trimmed, ". "); dot > 0 && dot <= 3 {
				return true
			}
		}
		// A Markdown link.
		if strings.Contains(trimmed, "](") && strings.Contains(trimmed, ")") {
			return true
		}
	}
	return false
}
