package dingtalk

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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk OutboundReplier — the engine seam that delivers a
// verdict-driven reply back to the user. It posts through the same sender as the
// EventChatDone subscriber.
//
// Outcomes handled:
//   - NeedsBinding: the sender is unbound. Mint a single-use binding token and
//     reply with a "link your account" prompt pointing at the in-product redeem
//     page. After they bind, their next message reaches the agent.
//   - AgentOffline / AgentArchived: a status notice so the user is not left
//     wondering why nothing happened.
//   - FreshPending / IssueUsage: command confirmation or corrective guidance.
//   - Ingested with a synchronously-created /issue: a confirmation carrying the
//     issue identifier and title. Plain chat turns stay silent.

const (
	agentOfflineText        = "⚠️ The agent is offline, so this message won't be processed automatically."
	agentArchivedText       = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	freshPendingText        = "✅ Fresh start ready. Your next chat message will run without previous context."
	issueUsageText          = "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)"
	issueUsageWithMediaText = "Please add a title and resend with the image (*image can come before or after the command*):\n\n`/issue <title>`\n\n`[description]` (optional)"
	// Refusals for dropped /issue commands, carried over from the deleted
	// pre-engine IssueCommandProcessor: without them the user's command
	// vanishes with no signal that it will never be handled.
	issueNotMemberText = "You're not a member of this Multica workspace, so I can't file an issue for you. Ask a workspace admin to invite you, then send the command again."
	issueDisabledText  = "This DingTalk robot isn't connected to Multica (or was disconnected). Ask the agent owner or a workspace owner/admin to reconnect it."
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, dingtalkUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for DingTalk.
type OutboundReplier struct {
	binding     bindingMinter
	decrypt     Decrypter
	client      *Client
	appURL      string
	bindingPath string
	logger      *slog.Logger
}

// OutboundReplierConfig configures the replier. Binding + AppURL are required for
// the NeedsBinding prompt to work; without them the prompt is skipped (the
// status and command notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	Decrypt Decrypter
	Client  *Client
	// AppURL is the Multica web app host the user clicks into to redeem the
	// binding token (e.g. https://multica.example). The bind page (/dingtalk/bind)
	// is served by the web app, so the link must point at the app host, not the
	// API host. Mirrors the Slack replier's AppURL.
	AppURL      string
	BindingPath string // default "/dingtalk/bind"
	Logger      *slog.Logger
}

var _ engine.OutboundReplier = (*OutboundReplier)(nil)

// NewOutboundReplier builds the replier.
func NewOutboundReplier(cfg OutboundReplierConfig) *OutboundReplier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := cfg.Client
	if client == nil {
		client = NewClient(nil, "")
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/dingtalk/bind"
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

// Reply routes each outcome to its user-visible message. Errors are logged, not
// propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, inst, msg, agentOfflineText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: offline notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, inst, msg, agentArchivedText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: archived notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeFreshPending:
		if err := r.post(ctx, inst, msg, freshPendingText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: fresh-start confirmation failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIssueUsage:
		text := issueUsageText
		if res.IssueUsageHadMedia {
			text = issueUsageWithMediaText
		}
		if err := r.post(ctx, inst, msg, text); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: issue usage reply failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIngested:
		if res.IssueID.Valid {
			text := issueCreatedText(res)
			if res.IssueDuplicate {
				text = issueDuplicateText(res)
			}
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.logger.WarnContext(ctx, "dingtalk replier: issue outcome reply failed",
					"installation_id", util.UUIDToString(inst.ID), "error", err)
			}
		}
	case engine.OutcomeDropped:
		// Dropped /issue commands get a refusal so the sender is not left
		// waiting for an issue that will never be created; every other drop
		// (duplicates, unaddressed group chatter) stays silent.
		if text := droppedReplyText(res, msg); text != "" {
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.logger.WarnContext(ctx, "dingtalk replier: drop refusal failed",
					"installation_id", util.UUIDToString(inst.ID), "error", err)
			}
		}
	}
}

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
	token, err := r.binding.Mint(ctx, inst.WorkspaceID, inst.ID, sender)
	if err != nil {
		return fmt.Errorf("mint binding token: %w", err)
	}
	bindURL := r.appURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	text := "👋 To start chatting with me, link your DingTalk account to Multica: [link your account](" +
		bindURL + ")\n\n(This link expires in 15 minutes.)"
	// Deliver the single-use binding link privately (1:1) to the sender, never
	// via targetFromMessage: in a group that would post the token into the whole
	// chat, where any other workspace member could redeem it and bind the
	// sender's DingTalk id to their own account (identity misbinding).
	target := sendTarget{ConversationType: convTypeP2P, StaffID: sender}
	if _, err := sendInstallationText(ctx, r.client, r.decrypt, inst, target, text); err != nil {
		return fmt.Errorf("post dingtalk binding prompt: %w", err)
	}
	return nil
}

// post resolves the installation's credentials from the carried platform row and
// sends text back into the originating conversation.
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	if _, err := sendInstallationText(ctx, r.client, r.decrypt, inst, targetFromMessage(msg), text); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	return nil
}

// sendInstallationText resolves an installation's credentials from the carried
// platform row and sends text into target. Shared by the OutboundReplier and the
// ack notifier so both proactive-send paths decode credentials identically.
func sendInstallationText(ctx context.Context, client *Client, decrypt Decrypter, inst engine.ResolvedInstallation, target sendTarget, text string) (string, error) {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return "", errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, decrypt)
	if err != nil {
		return "", fmt.Errorf("decode credentials: %w", err)
	}
	s := &sender{client: client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	return s.send(ctx, target, text)
}

// targetFromMessage builds the reply target from the inbound message's own
// routing identity (used for the immediate binding/status replies, before any
// chat binding exists).
func targetFromMessage(msg channel.InboundMessage) sendTarget {
	t := sendTarget{ConversationType: convTypeGroup, ConversationID: msg.Source.ChatID}
	if msg.Source.ChatType == channel.ChatTypeP2P {
		t.ConversationType = convTypeP2P
		t.StaffID = msg.Source.SenderID
	}
	return t
}

// isAddressedIssueCommand reports whether msg is an /issue command explicitly
// addressed to the bot — the gating the deleted pre-engine divert used. Only
// such messages warrant an error/refusal reply: the sender asked for an
// action, so silence would read as acceptance.
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

// droppedReplyText maps an OutcomeDropped result to a user-facing refusal.
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
