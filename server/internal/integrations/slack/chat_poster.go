package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chatPoster delivers chat issue subscription updates into the channel, or
// into the thread the subscription was made in.
type chatPoster struct {
	decrypt Decrypter
	logger  *slog.Logger
}

// NewChatPoster returns the Slack engine.ChatPoster.
func NewChatPoster(decrypt Decrypter, logger *slog.Logger) engine.ChatPoster {
	if logger == nil {
		logger = slog.Default()
	}
	return &chatPoster{decrypt: decrypt, logger: logger}
}

func (p *chatPoster) PostToChat(ctx context.Context, row db.ChannelInstallation, target engine.ChatTarget, text string) error {
	if target.ChatID == "" {
		return errors.New("slack chat poster: missing channel id")
	}
	creds, err := decodeCredentials(row.Config, p.decrypt)
	if err != nil {
		return fmt.Errorf("decode credentials: %w", err)
	}
	// The update quotes member-authored titles and comments.
	_, err = newSlackSender(creds, slack.New(creds.BotToken), p.logger).Send(ctx, channel.OutboundMessage{
		ChatID: target.ChatID, Text: memberIssueTitle(text), ThreadID: target.ThreadID,
	})
	return err
}
