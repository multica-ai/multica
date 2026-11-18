package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chatPoster delivers chat subscription updates as plain Lark text, replying
// in the topic under the subscription's anchor message. Subscriptions without
// an anchor post at the chat level.
type chatPoster struct {
	client      APIClient
	credentials CredentialsResolver
	log         *slog.Logger
}

// NewChatPoster returns the Feishu engine.ChatPoster. It is an
// engine.TopicPoster: replies in thread under a chat-level message form a
// topic, so a followed project gets one topic per issue.
func NewChatPoster(client APIClient, credentials CredentialsResolver, log *slog.Logger) engine.TopicPoster {
	if log == nil {
		log = slog.Default()
	}
	return &chatPoster{client: client, credentials: credentials, log: log}
}

func (p *chatPoster) PostToChat(ctx context.Context, row db.ChannelInstallation, target engine.ChatTarget, text string) error {
	if target.ChatID == "" {
		return errors.New("lark chat poster: missing chat_id")
	}
	inst, err := installationFromRow(row)
	if err != nil {
		return fmt.Errorf("decode feishu installation: %w", err)
	}
	creds, err := installationCredentialsFor(inst, p.credentials)
	if err != nil {
		return err
	}
	// Updates go into the topic under the message that subscribed: the topic
	// it was sent in, or the one the command reply opened under it.
	reply := ReplyTarget{}
	if target.AnchorMessageID != "" {
		reply = ReplyTarget{MessageID: target.AnchorMessageID, InThread: true}
	}
	return sendWithReplyFallback(p.log, "send issue subscription update", reply, func(t ReplyTarget) error {
		_, err := p.client.SendTextMessage(ctx, SendTextParams{
			InstallationID: creds,
			ChatID:         ChatID(target.ChatID),
			Text:           text,
			ReplyTarget:    t,
		})
		return err
	})
}

// OpenTopic posts text at chat level and returns its message id, the anchor
// later updates reply under to stay in that message's topic.
func (p *chatPoster) OpenTopic(ctx context.Context, row db.ChannelInstallation, chatID, text string) (string, error) {
	if chatID == "" {
		return "", errors.New("lark chat poster: missing chat_id")
	}
	inst, err := installationFromRow(row)
	if err != nil {
		return "", fmt.Errorf("decode feishu installation: %w", err)
	}
	creds, err := installationCredentialsFor(inst, p.credentials)
	if err != nil {
		return "", err
	}
	return p.client.SendTextMessage(ctx, SendTextParams{InstallationID: creds, ChatID: ChatID(chatID), Text: text})
}
