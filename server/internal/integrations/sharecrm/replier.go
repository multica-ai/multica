package sharecrm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	agentOfflineText   = "⚠️ The agent is offline, so this message won't be processed automatically."
	agentArchivedText  = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	issueNotMemberText = "You're not a member of this Multica workspace, so I can't file an issue for you. Ask a workspace admin to invite you, then send the command again."
	issueDisabledText  = "This ShareCRM bot isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
	// bare /clear confirmation (MUL-6661 / engine OutcomeFreshPending).
	freshPendingText = "✅ Fresh start ready. Your next chat message will run without previous context."
	// bare /new confirmation (MUL-6661 / engine OutcomeChatStarted).
	chatStartedText = "✅ Started a new Multica chat. Your next message will enter it."
	// bare /issue usage (MUL-5873 / engine OutcomeIssueUsage). ShareCRM is
	// text-first so there is no media-with-title variant.
	issueUsageText = "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)"
	// bindingLinkAlreadySentText is posted when the mint throttle reuses a
	// live link (MUL-5880). Only the hash was stored, so we cannot rebuild a
	// URL — point the user at the earlier 1:1 message.
	bindingLinkAlreadySentText = "👋 The binding link was already sent above — please open it to finish linking."
)

type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, channelUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for ShareCRM.
type OutboundReplier struct {
	binding     bindingMinter
	decrypt     Decrypter
	client      *Client
	appURL      string
	bindingPath string
	logger      *slog.Logger
}

type OutboundReplierConfig struct {
	Binding     bindingMinter
	Decrypt     Decrypter
	Client      *Client
	AppURL      string
	BindingPath string
	Logger      *slog.Logger
}

var _ engine.OutboundReplier = (*OutboundReplier)(nil)

func NewOutboundReplier(cfg OutboundReplierConfig) *OutboundReplier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := cfg.Client
	if client == nil {
		client = NewClient(nil)
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/sharecrm/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		decrypt:     cfg.Decrypt,
		client:      client,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
}

func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, inst, msg, agentOfflineText); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: offline notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, inst, msg, agentArchivedText); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: archived notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeFreshPending:
		if err := r.post(ctx, inst, msg, freshPendingText); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: fresh-start confirmation failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeChatStarted:
		if err := r.post(ctx, inst, msg, chatStartedText); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: new-chat confirmation failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIssueUsage:
		if err := r.post(ctx, inst, msg, issueUsageText); err != nil {
			r.logger.WarnContext(ctx, "sharecrm replier: issue usage reply failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIngested:
		if res.IssueID.Valid {
			text := issueCreatedText(res)
			if res.IssueDuplicate {
				text = issueDuplicateText(res)
			}
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.logger.WarnContext(ctx, "sharecrm replier: issue outcome reply failed",
					"installation_id", util.UUIDToString(inst.ID), "error", err)
			}
		}
	case engine.OutcomeDropped:
		if text := droppedReplyText(res, msg); text != "" {
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.logger.WarnContext(ctx, "sharecrm replier: drop refusal failed",
					"installation_id", util.UUIDToString(inst.ID), "error", err)
			}
		}
	}
}

const (
	// groupBindingAckText is posted into a group when the sender still needs to
	// bind. It deliberately carries NO token: Redeem only checks that the
	// redeemer is a workspace member, and the bind page redeems as whoever is
	// signed in. A live link in a group would let any member click first and
	// attach the sender's ShareCRM id to their own Multica account (the same
	// identity-misbinding path WeCom/DingTalk closed). ShareCRM's send API is
	// chat_id-only (no staff-id / open-id private address), so we cannot DM
	// from a group event — ask the user to open a 1:1 and mint only there.
	groupBindingAckText = "👋 Please open a 1:1 chat with me and send any message to get your account-link. Binding links are never posted in groups."
)

func (r *OutboundReplier) sendBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) error {
	sender := res.Sender
	if sender == "" {
		sender = msg.Source.SenderID
	}
	if sender == "" {
		return errors.New("missing sender id")
	}
	if r.binding == nil {
		return errors.New("binding service not configured")
	}
	if r.appURL == "" {
		return errors.New("app url not configured")
	}

	// Group: never mint. A token is a bearer credential; the only safe room for
	// it is the sender's 1:1. Ask them to DM the bot so the next NeedsBinding
	// outcome lands in P2P and gets the real link.
	if msg.Source.ChatType == channel.ChatTypeGroup {
		return r.post(ctx, inst, msg, groupBindingAckText)
	}

	token, err := r.binding.Mint(ctx, inst.WorkspaceID, inst.ID, sender)
	if err != nil {
		return fmt.Errorf("mint binding token: %w", err)
	}
	// Throttle reused a live link: only its hash was stored, so there is no
	// URL to rebuild — point the user at the earlier 1:1 message.
	if token.Reused {
		return r.post(ctx, inst, msg, bindingLinkAlreadySentText)
	}
	bindURL := r.appURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	// ShareCRM is plain text — no markdown link syntax. Only delivered in 1:1.
	text := "👋 To start chatting with me, link your ShareCRM account to Multica:\n" +
		bindURL + "\n\n(This link expires in 15 minutes.)"
	return r.post(ctx, inst, msg, text)
}

func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	if _, err := sendInstallationText(ctx, r.client, r.decrypt, inst, targetFromMessage(msg), text); err != nil {
		return fmt.Errorf("post sharecrm reply: %w", err)
	}
	return nil
}

func isAddressedIssueCommand(msg channel.InboundMessage) bool {
	if !msg.AddressedToBot {
		return false
	}
	source := msg.CommandText
	if source == "" {
		source = msg.Text
	}
	_, ok := engine.ParseIssueCommand(source)
	return ok
}

func droppedReplyText(res engine.Result, msg channel.InboundMessage) string {
	if !isAddressedIssueCommand(msg) {
		return ""
	}
	switch res.DropReason {
	case engine.DropReasonNonWorkspaceMember:
		return issueNotMemberText
	case engine.DropReasonRevokedInstallation:
		return issueDisabledText
	default:
		return ""
	}
}

func issueCreatedText(res engine.Result) string {
	identifier := issueResultIdentifier(res)
	if res.IssueTitle == "" {
		return "✅ Created " + identifier
	}
	return "✅ Created " + identifier + " — " + res.IssueTitle
}

func issueDuplicateText(res engine.Result) string {
	identifier := issueResultIdentifier(res)
	if res.IssueTitle == "" {
		return "⚠️ Not created — active issue " + identifier + " already exists."
	}
	return "⚠️ Not created — active issue " + identifier + " already exists: " + res.IssueTitle
}

func issueResultIdentifier(res engine.Result) string {
	if res.IssueIdentifier != "" {
		return res.IssueIdentifier
	}
	if res.IssueNumber > 0 {
		return fmt.Sprintf("#%d", res.IssueNumber)
	}
	return util.UUIDToString(res.IssueID)
}
