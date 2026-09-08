package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CredentialsFunc resolves an installation's API credentials.
type CredentialsFunc func(installationID pgtype.UUID) (InstallationCredentials, error)

type dmDeliverer struct {
	client APIClient
	creds  CredentialsFunc
	logger *slog.Logger
}

var _ notify.DMDeliverer = (*dmDeliverer)(nil)

// NewDMDeliverer adapts the Lark client to the shared push layer.
//
// Lark is the reference implementation of a replyable push: it hands back a
// per-message id on send and reports a per-message ReplyCtx on inbound, so a
// reply to a push can be traced to the issue it was about. WeCom does
// neither.
func NewDMDeliverer(client APIClient, creds CredentialsFunc, logger *slog.Logger) notify.DMDeliverer {
	if logger == nil {
		logger = slog.Default()
	}
	return &dmDeliverer{client: client, creds: creds, logger: logger}
}

func (d *dmDeliverer) DeliverDM(ctx context.Context, ref notify.PushRef, binding db.ChannelUserBinding, text string) (notify.DeliverResult, error) {
	if text == "" || binding.ChannelUserID == "" {
		return notify.DeliverResult{}, nil
	}
	creds, err := d.creds(binding.InstallationID)
	if err != nil {
		return notify.DeliverResult{}, err
	}
	params := SendDirectParams{
		InstallationID: creds,
		OpenID:         OpenID(binding.ChannelUserID),
	}
	plainText := notify.PlainHead(text)
	if ref.DesktopURL != "" {
		cardJSON, cardErr := issuePushCard(plainText, ref.WebURL, ref.DesktopURL)
		if cardErr != nil {
			return notify.DeliverResult{}, cardErr
		}
		params.CardJSON = cardJSON
	} else {
		params.Text = plainText
	}
	messageID, err := d.client.SendDirectMessage(ctx, params)
	if err != nil {
		return notify.DeliverResult{}, err
	}
	if messageID == "" {
		// Without an id the push is unaddressable, so a reply to it would
		// fall through to the ordinary chat path and confuse the user.
		// Surfacing this as a failure keeps the metric honest.
		return notify.DeliverResult{}, errors.New("lark: send returned no message_id")
	}
	if ref.StartTopic {
		if _, topicErr := d.client.SendTextMessage(ctx, SendTextParams{
			InstallationID: creds,
			Text:           "请在本话题中回复处理意见。",
			ReplyTarget:    ReplyTarget{MessageID: messageID, InThread: true},
		}); topicErr != nil {
			// The root push is already visible and its message id is required for
			// inbound attribution. Treat topic creation as a graceful degradation
			// rather than returning an error that would leave the root untracked.
			d.logger.Warn("lark: create push topic", "message_id", messageID, "err", topicErr)
		}
	}
	return notify.DeliverResult{State: notify.StateDelivered, MessageID: messageID}, nil
}

func issuePushCard(text, webURL, desktopURL string) (string, error) {
	defaultURL := webURL
	if defaultURL == "" {
		defaultURL = desktopURL
	}
	doc := map[string]any{
		"schema": "2.0",
		"body": map[string]any{
			"elements": []any{
				map[string]any{
					"tag":     "markdown",
					"content": escapeCardMarkdown(text),
				},
				map[string]any{
					"tag":  "button",
					"type": "primary",
					"text": map[string]any{
						"tag":     "plain_text",
						"content": "在 Multica 中查看",
					},
					"behaviors": []any{
						map[string]any{
							"type":        "open_url",
							"default_url": defaultURL,
							"pc_url":      desktopURL,
						},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("lark: encode issue push card: %w", err)
	}
	return string(raw), nil
}

// escapeCardMarkdown preserves the push as literal text inside Lark's
// schema-2.0 markdown element. The shared renderer may contain user-authored
// content, so allowing its markdown tokens through would let a task reshape
// the notification card around the trusted action button.
func escapeCardMarkdown(text string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"`", "\\`",
		"*", "\\*",
		"_", "\\_",
		"~", "\\~",
		"#", "\\#",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"<", "\\<",
		">", "\\>",
	)
	return replacer.Replace(text)
}

// AcceptsReplies is true for Lark: SendDirectMessage returns a real
// message_id on send, and Lark's inbound delivery reports a per-message
// reply context, so a reply to a push can be attributed back to the issue
// it was about. Contrast WeCom (internal/integrations/wecom/notify_dm.go),
// which returns false — its send ack carries no message id and its inbound
// callback carries no reply context, so neither half of the loop exists
// there.
func (d *dmDeliverer) AcceptsReplies() bool { return true }
