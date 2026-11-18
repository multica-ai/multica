package infoflow

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
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the InfoFlow OutboundReplier — the engine seam that delivers a
// verdict-driven reply back to the user. It posts through the SDK client built
// from the installation's own credentials, exactly like the EventChatDone
// subscriber (outbound.go).
//
// Outcomes handled:
//   - NeedsBinding: the sender is unbound. Mint a single-use binding token and
//     reply with a "link your account" prompt pointing at the in-product redeem
//     page, delivered privately to that sender.
//   - AgentOffline / AgentArchived: a status notice so the user is not left
//     wondering why nothing happened.
//   - FreshPending / ChatStarted / IssueUsage: command confirmations.
//   - Ingested with a synchronously-created /issue: a confirmation carrying the
//     issue identifier and title. Plain chat turns stay silent.

const (
	agentOfflineText  = "⚠️ The agent is offline, so this message won't be processed automatically."
	agentArchivedText = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	freshPendingText  = "✅ Fresh start ready. Your next chat message will run without previous context."
	chatStartedText   = "✅ Started a new Multica chat. Your next message will enter it."
	issueUsageText    = "Please include an issue title. Use:\n\n`/issue <title>`\n\n`[description]` (optional)"
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, infoflowUserID string) (BindingToken, error)
}

// OutboundReplierConfig configures the replier. Binding + AppURL are required
// for the NeedsBinding prompt to work; without them the prompt is skipped (the
// status and command notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	Decrypt Decrypter
	Box     *secretbox.Box
	AppURL  string
	// BindingPath is the in-product redeem page. Default "/infoflow/bind".
	BindingPath string
	Logger      *slog.Logger
}

// OutboundReplier implements engine.OutboundReplier for InfoFlow.
type OutboundReplier struct {
	binding     bindingMinter
	box         *secretbox.Box
	appURL      string
	bindingPath string
	logger      *slog.Logger
}

var _ engine.OutboundReplier = (*OutboundReplier)(nil)

// NewOutboundReplier builds the replier.
func NewOutboundReplier(cfg OutboundReplierConfig) *OutboundReplier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/infoflow/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		box:         cfg.Box,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
}

// Reply routes each outcome to its user-visible message. Errors are logged, not
// propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	// These notices may refer to recovered pending input. The live callback can
	// belong to another generation, so use only its stable conversation route.
	if res.Outcome == engine.OutcomeAgentOffline || res.Outcome == engine.OutcomeAgentArchived {
		msg = channel.InboundMessage{Source: msg.Source}
		if msg.Source.ChatType == channel.ChatTypeGroup {
			msg.Source.SenderID = ""
		}
	}
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.warn(ctx, "binding prompt failed", inst.ID, err)
		}
	case engine.OutcomeAgentOffline:
		r.post(ctx, inst, msg, agentOfflineText, "offline notice failed")
	case engine.OutcomeAgentArchived:
		r.post(ctx, inst, msg, agentArchivedText, "archived notice failed")
	case engine.OutcomeFreshPending:
		r.post(ctx, inst, msg, freshPendingText, "fresh-start confirmation failed")
	case engine.OutcomeChatStarted:
		r.post(ctx, inst, msg, chatStartedText, "new-chat confirmation failed")
	case engine.OutcomeIssueUsage:
		r.post(ctx, inst, msg, issueUsageText, "issue usage reply failed")
	case engine.OutcomeIngested:
		if res.IssueID.Valid {
			r.post(ctx, inst, msg, issueCreatedText(res, r.appURL), "issue outcome reply failed")
		}
	case engine.OutcomeDropped:
		if text := droppedReplyText(res, msg); text != "" {
			r.post(ctx, inst, msg, text, "drop refusal failed")
		}
	}
}

func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text, what string) {
	if _, err := sendInstallationText(ctx, r.box, inst, targetFromChatID(msg.Source.ChatID), text); err != nil {
		r.warn(ctx, what, inst.ID, err)
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
	text := "👋 To start chatting with me, link your InfoFlow account to Multica: [link your account](" +
		bindURL + ")\n\n(This link expires in 15 minutes.)"
	// Deliver the single-use binding link privately to the sender, never into the
	// originating chat: in a group that would post the token where any other
	// member could redeem it and bind the sender's id to their own account.
	target := sendTarget{ChatType: channel.ChatTypeP2P, ChatID: sender}
	if _, err := sendInstallationText(ctx, r.box, inst, target, text); err != nil {
		return fmt.Errorf("post infoflow binding prompt: %w", err)
	}
	return nil
}

func (r *OutboundReplier) warn(ctx context.Context, what string, instID pgtype.UUID, err error) {
	logger := r.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "infoflow replier: "+what, "installation_id", util.UUIDToString(instID), "error", err)
}

// sendInstallationText decodes the installation's credentials and sends text
// into target. Shared by the OutboundReplier so every proactive send decodes
// credentials identically.
func sendInstallationText(ctx context.Context, box *secretbox.Box, inst engine.ResolvedInstallation, target sendTarget, text string) (string, error) {
	if box == nil {
		return "", errors.New("infoflow: encryption box is required")
	}
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return "", errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, box.Open)
	if err != nil {
		return "", fmt.Errorf("decode credentials: %w", err)
	}
	client, err := newSDKClient(creds)
	if err != nil {
		return "", err
	}
	res, err := sendText(ctx, client, target, text)
	if err != nil {
		return "", err
	}
	return res.MessageID, nil
}

// issueCreatedText builds the confirmation for a synchronously-created issue.
func issueCreatedText(res engine.Result, appURL string) string {
	identifier := res.IssueIdentifier
	if identifier == "" {
		return "✅ Created your issue."
	}
	text := "✅ Created " + identifier
	if res.IssueTitle != "" {
		text += " — " + res.IssueTitle
	}
	if appURL != "" && res.IssueID.Valid {
		text += "\n" + appURL + "/issues/" + util.UUIDToString(res.IssueID)
	}
	return text
}

// droppedReplyText maps a dropped /issue command to a refusal. Every other drop
// (duplicates, unaddressed group chatter) stays silent.
func droppedReplyText(res engine.Result, msg channel.InboundMessage) string {
	switch res.DropReason {
	case engine.DropReasonNonWorkspaceMember:
		return "You're not a member of this Multica workspace, so I can't run an issue command for you. Ask a workspace admin to invite you, then send the command again."
	default:
		return ""
	}
}
