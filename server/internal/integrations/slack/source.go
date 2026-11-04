package slack

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const sourceMetadataTimeout = 500 * time.Millisecond

type sourceClient interface {
	GetConversationInfoContext(context.Context, *slack.GetConversationInfoInput) (*slack.Channel, error)
	GetPermalinkContext(context.Context, *slack.PermalinkParameters) (string, error)
}

// sourceMetadata makes at most two optional calls under one deadline. A 429
// stops enrichment immediately; errors and installation secrets never leave
// this adapter. No new scopes or retries are needed to read available fields.
func sourceMetadata(ctx context.Context, client sourceClient, channelID, threadID, messageID string) *protocol.ChatSource {
	source := protocol.SafeSlackSource(&protocol.ChatSource{ChannelID: channelID, ThreadID: threadID})
	if source == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, sourceMetadataTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return source
	}
	if messageID != "" {
		link, err := client.GetPermalinkContext(ctx, &slack.PermalinkParameters{Channel: channelID, Ts: messageID})
		if err == nil {
			source.Permalink = link
		}
		if !canContinueSourceMetadata(err) || ctx.Err() != nil {
			return protocol.SafeSlackSource(source)
		}
	}
	info, err := client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{ChannelID: channelID})
	if err == nil && info != nil && info.ID == channelID {
		source.ChannelName = info.Name
	}
	return protocol.SafeSlackSource(source)
}

// Only known API permission or missing-message responses allow another optional
// request. The SDK can return status or parsing errors for HTTP 429 depending on
// Retry-After, so unknown errors (including transport failures) must stop here.
func canContinueSourceMetadata(err error) bool {
	if err == nil {
		return true
	}
	var apiError slack.SlackErrorResponse
	if errors.As(err, &apiError) {
		switch apiError.Err {
		case "missing_scope", "access_denied", "message_not_found", "channel_not_found":
			return true
		}
	}
	return false
}

// TaskSource uses only the immutable delivery for this task, including on a
// retry after newer messages have changed the session's binding.
func (h *History) TaskSource(ctx context.Context, delivery db.ChannelTaskDelivery, workspaceID pgtype.UUID) *protocol.ChatSource {
	if delivery.ChannelType != string(TypeSlack) || !delivery.InstallationID.Valid || !workspaceID.Valid {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, sourceMetadataTimeout)
	defer cancel()
	inst, err := h.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: delivery.InstallationID, ChannelType: string(TypeSlack)})
	if err != nil || inst.Status != "active" || inst.ID != delivery.InstallationID || inst.WorkspaceID != workspaceID || inst.ChannelType != string(TypeSlack) {
		return nil
	}
	creds, err := decodeCredentials(inst.Config, h.decrypt)
	if err != nil {
		return nil
	}
	channelID, threadID := historyTarget(slackBindingFromTaskDelivery(delivery))
	messageID := ""
	if delivery.ChannelMessageID.Valid {
		messageID = delivery.ChannelMessageID.String
	}
	if threadID != "" {
		messageID = threadID
	}
	return sourceMetadata(ctx, h.newClient(creds.BotToken), channelID, threadID, messageID)
}
