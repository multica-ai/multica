package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file owns the Slack failure card (posted by Outbound on a terminal chat
// task failure) and its interactive Retry button, delivered as a block_actions
// payload over the installation's Socket Mode connection. A click never
// bypasses Multica authorization: the clicker must be a bound workspace member
// of the installation that posted the card, and the retry is refused once the
// failed turn is no longer the session's latest (stale card or double click).

const (
	retryTaskActionID    = "multica_retry_task"
	openChatActionID     = "multica_open_chat"
	failureActionsBlock  = "multica_failure_actions"
	maxFailureErrorRunes = 2500 // section text caps at 3000 characters
	failureCardFallback  = "⚠️ The agent run failed."
)

const (
	retryQueuedText = "🔁 <@%s> retried this run."
	retryStaleText  = "This run can no longer be retried: it was already retried or the conversation has moved on."
	retryErrorText  = "⚠️ Something went wrong retrying this run. Please try again."
)

type chatRetrier interface {
	RetryFailedChatTask(ctx context.Context, parentID, actorUserID pgtype.UUID) (db.AgentTaskQueue, error)
}

func failureCardBlocks(taskID pgtype.UUID, errText, chatURL string) []slack.Block {
	section := slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType,
		"⚠️ *The agent run failed*\n>"+strings.ReplaceAll(escapeMrkdwn(errText), "\n", "\n>"), false, false), nil, nil)
	retry := slack.NewButtonBlockElement(retryTaskActionID, util.UUIDToString(taskID),
		slack.NewTextBlockObject(slack.PlainTextType, "Retry", false, false))
	retry.Style = slack.StylePrimary
	elements := []slack.BlockElement{retry}
	if chatURL != "" {
		open := slack.NewButtonBlockElement(openChatActionID, "",
			slack.NewTextBlockObject(slack.PlainTextType, "Open in Multica", false, false))
		open.URL = chatURL
		elements = append(elements, open)
	}
	return []slack.Block{section, slack.NewActionBlock(failureActionsBlock, elements...)}
}

// escapeMrkdwn escapes the three control characters Slack requires escaped in
// message text, so agent error output cannot inject mentions or links.
func escapeMrkdwn(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// HandleBlockAction processes an already-ACKed block_actions payload. Only the
// Retry button carries work; the URL button needs no server action.
func (p *SlashCommandProcessor) HandleBlockAction(ctx context.Context, cb slack.InteractionCallback) {
	var taskValue string
	found := false
	for _, a := range cb.ActionCallback.BlockActions {
		if a != nil && a.ActionID == retryTaskActionID {
			taskValue, found = a.Value, true
			break
		}
	}
	if !found || cb.ResponseURL == "" {
		return
	}
	text, retried := p.processRetry(ctx, cb, taskValue)
	var err error
	if retried {
		err = p.replaceCard(ctx, cb.ResponseURL, retriedCardBlocks(cb.Message.Blocks.BlockSet, text), text)
	} else {
		err = p.respond(ctx, cb.ResponseURL, text)
	}
	if err != nil {
		p.logger.WarnContext(ctx, "slack retry action: response_url reply failed",
			"app_id", cb.APIAppID, "error", err)
	}
}

func (p *SlashCommandProcessor) processRetry(ctx context.Context, cb slack.InteractionCallback, taskValue string) (string, bool) {
	inst, err := p.resolveInstallation(ctx, cb.APIAppID, cb.Team.ID)
	if err != nil {
		if errors.Is(err, engine.ErrInstallationNotFound) {
			return slashDisabledText, false
		}
		return retryErrorText, false
	}
	if !inst.Active {
		return slashDisabledText, false
	}
	userID, err := p.resolveUser(ctx, inst, cb.User.ID)
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSenderUnbound):
			return p.bindingText(ctx, inst, cb.User.ID), false
		case errors.Is(err, engine.ErrSenderNotMember):
			return slashNotMemberText, false
		default:
			return retryErrorText, false
		}
	}
	taskID, err := util.ParseUUID(taskValue)
	if err != nil || !taskID.Valid || p.retry == nil {
		return retryStaleText, false
	}
	// The card's task must belong to this installation and channel; anything
	// else is a forged or cross-installation payload and is treated as stale.
	delivery, err := p.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return retryStaleText, false
		}
		return retryErrorText, false
	}
	channelID, _ := outboundTarget(slackBindingFromTaskDelivery(delivery))
	if delivery.ChannelType != string(TypeSlack) || delivery.InstallationID != inst.ID || channelID != cb.Channel.ID {
		return retryStaleText, false
	}
	if _, err := p.retry.RetryFailedChatTask(ctx, taskID, userID); err != nil {
		if errors.Is(err, service.ErrChatRetryStale) {
			return retryStaleText, false
		}
		p.logger.WarnContext(ctx, "slack retry action: retry failed",
			"installation_id", util.UUIDToString(inst.ID), "task_id", taskValue, "error", err)
		return retryErrorText, false
	}
	return fmt.Sprintf(retryQueuedText, cb.User.ID), true
}

// retriedCardBlocks keeps the failure summary, drops the buttons, and records
// who retried, so the thread shows the outcome and the card cannot be reused.
func retriedCardBlocks(original []slack.Block, note string) []slack.Block {
	blocks := make([]slack.Block, 0, len(original)+1)
	for _, b := range original {
		if b.BlockType() != slack.MBTAction {
			blocks = append(blocks, b)
		}
	}
	return append(blocks, slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, note, false, false)))
}
