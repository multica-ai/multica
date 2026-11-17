package weixin

import (
	"context"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Reply implements the shared engine's command/status notices. The scanner
// was already bound to the initiating Multica user during QR confirmation.
func (o *Outbound) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, result engine.Result) {
	var text string
	switch result.Outcome {
	case engine.OutcomeAgentOffline:
		text = "This agent is offline. Start its runtime in Multica, then send your message again."
	case engine.OutcomeAgentArchived:
		text = "This agent is archived. Restore it in Multica before continuing."
	case engine.OutcomeInvokeDenied:
		text = "You no longer have permission to run this agent. Check your workspace access in Multica."
	case engine.OutcomeNeedsBinding:
		text = "Reconnect WeChat in Multica to link this account."
	case engine.OutcomeFreshPending:
		text = "Your next message will start with fresh context."
	case engine.OutcomeChatStarted:
		text = "Started a new Multica chat. Send your next message here."
	case engine.OutcomeIssueUsage:
		text = "Use /issue <title> followed by an optional description."
	case engine.OutcomeIngested:
		if result.IssueID.Valid {
			text = "Created " + result.IssueIdentifier + ": " + result.IssueTitle
			if result.IssueDuplicate {
				text = "This issue already exists: " + result.IssueIdentifier
			}
		}
	case engine.OutcomeDropped:
		if result.DropReason == engine.DropReasonNonWorkspaceMember {
			text = "You are no longer a member of this Multica workspace."
		}
	}
	if text == "" {
		return
	}
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return
	}
	info := DecodePublicConfig(row.Config)
	api, token, err := o.sender(ctx, inst.ID, info.BotID, msg.Source.SenderID)
	if err == nil {
		_, err = api.Send(ctx, textMessage(msg.Source.ChatID, token, util.UUIDToString(dbid.NewV7()), text))
	}
	if err != nil {
		o.logger.WarnContext(ctx, "weixin: status reply failed", "error", err)
	}
}
