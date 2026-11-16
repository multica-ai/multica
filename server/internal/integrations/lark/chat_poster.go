package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chatPoster delivers chat issue subscription updates as plain Lark text. A
// subscription made inside a topic replies in that topic through its anchor
// message; others post at the chat level.
type chatPoster struct {
	client      APIClient
	credentials CredentialsResolver
	log         *slog.Logger
}

// NewChatPoster returns the Feishu engine.ChatPoster.
func NewChatPoster(client APIClient, credentials CredentialsResolver, log *slog.Logger) engine.ChatPoster {
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
	reply := ReplyTarget{}
	if target.ThreadID != "" && target.AnchorMessageID != "" {
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
