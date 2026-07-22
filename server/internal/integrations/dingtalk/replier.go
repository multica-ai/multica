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
//   - Ingested: /issue acks (queued / usage / queue-failed) and the bare /new reset confirmation; plain chat turns stay silent.

const (
	agentOfflineText  = "⚠️ The agent is offline, so this message won't be processed automatically."
	agentArchivedText = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	freshResetText    = "🆕 Started a fresh session. Send your next message to begin."
	// Refusals for dropped /issue commands, carried over from the deleted
	// pre-engine IssueCommandProcessor: without them the user's command
	// vanishes with no signal that it will never be handled.
	issueNotMemberText = "You're not a member of this Multica workspace, so I can't file an issue for you. Ask a workspace admin to invite you, then send the command again."
	issueDisabledText  = "This DingTalk robot isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
	// Inbound-media refusals. mediaFailedText fires when the image pipeline
	// could not fetch/stage the message's pictures (nothing was recorded, so
	// silence would read as acceptance); unsupportedKindText fires for
	// audio/video/file/unknown kinds the bot cannot read.
	mediaFailedText     = "⚠️ I couldn't process the image(s) in that message — download failed, unsupported format (PNG, JPEG, GIF, WebP or BMP only), or over the limit (10 images, 20 MB each). Please send them again."
	unsupportedKindText = "I can only read text and images here. For files or voice, please describe it in text or attach it on the web app."
	// mediaUnsupportedText fires when this workspace has no inbound-image support
	// at all (object storage unconfigured). It is a permanent capability gap, so
	// — unlike mediaFailedText — it must NOT tell the user to resend, which no
	// resend could satisfy.
	mediaUnsupportedText = "I can't accept images in this chat. Please describe it in text, or attach the image on the Multica web app."
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
// offline/archived/issue notices still fire).
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
	case engine.OutcomeIngested:
		// /issue acks and the bare /new reset each warrant a short
		// confirmation; a plain chat message stays silent (the agent's own
		// reply lands via the outbound subscribers).
		if text := ingestedReplyText(res); text != "" {
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.logger.WarnContext(ctx, "dingtalk replier: ingest confirmation failed",
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
	_, ok := engine.ParseIssueCommand(msg.Text)
	return ok
}

// droppedReplyText maps an OutcomeDropped result to a user-facing refusal.
func droppedReplyText(res engine.Result, msg channel.InboundMessage) string {
	// Media/kind refusals apply to ANY addressed turn, not just /issue: the
	// sender deliberately sent content the pipeline refused, so silence would
	// read as acceptance.
	switch res.DropReason {
	case engine.DropReasonMediaFetchFailed:
		return mediaFailedText
	case engine.DropReasonMediaUnsupported:
		return mediaUnsupportedText
	case engine.DropReasonUnsupportedKind:
		return unsupportedKindText
	}
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

// ingestedReplyText maps an OutcomeIngested result to its confirmation text.
// Empty means stay silent: a plain chat turn's reply arrives later via the
// EventChatDone / EventQuickCreateDone outbound, not from the replier.
func ingestedReplyText(res engine.Result) string {
	switch {
	case res.IssueQueued:
		return engine.IssueQueuedAckText
	case res.IssueUsage:
		return engine.IssueUsageText
	case res.IssueQueueFailed:
		return engine.IssueQueueFailedText
	case res.FreshReset:
		return freshResetText
	default:
		return ""
	}
}
