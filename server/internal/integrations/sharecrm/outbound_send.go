package sharecrm

import (
	"context"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// sendTarget is always a chat_id for ShareCRM (p2p and group share the same API).
type sendTarget struct {
	ChatID string
}

func targetFromMessage(msg channel.InboundMessage) sendTarget {
	return sendTarget{ChatID: msg.Source.ChatID}
}

func sendInstallationText(ctx context.Context, client *Client, decrypt Decrypter, inst engine.ResolvedInstallation, target sendTarget, text string) (string, error) {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return "", errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, decrypt)
	if err != nil {
		return "", fmt.Errorf("decode credentials: %w", err)
	}
	if target.ChatID == "" {
		return "", errors.New("sharecrm: empty chat_id")
	}
	return client.SendMessage(ctx, creds.GatewayBaseURL, creds.AppID, creds.AppSecret, target.ChatID, stripMarkdown(text), nil)
}
