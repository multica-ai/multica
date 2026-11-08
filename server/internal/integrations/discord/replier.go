package discord

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

// This file is the Discord OutboundReplier — the engine seam that delivers a
// verdict-driven reply back to the user, mirroring slack/replier.go and
// telegram/replier.go:
//   - NeedsBinding: mint a single-use binding token, reply with the link to
//     the in-product redeem page (/discord/bind). A guild channel never gets
//     the bearer link: the sender is asked to DM the bot instead.
//   - AgentOffline / AgentArchived: a status notice.
//   - FreshPending / ChatStarted / IssueUsage: command confirmation or
//     corrective guidance.
//   - Ingested with an /issue result: creation or duplicate confirmation.

const (
	msgAgentOffline   = "⚠️ The agent is offline right now. Your message was received and will be handled once it's back online."
	msgAgentArchived  = "⚠️ This agent has been archived and can't respond. Please contact your workspace admin."
	msgFreshPending   = "✅ Fresh start ready. Your next chat message will run without previous context."
	msgChatStarted    = "✅ Started a new Multica chat. Your next message will enter it."
	msgIssueUsage     = "Please include an issue title. Use:\n\n`/issue <title>`\n`[description]` (optional)"
	msgBindingGroup   = "👋 To link your Discord account to Multica, send me a direct message first. I'll reply there with the link."
	msgIssueNotMember = "You're not a member of this Multica workspace, so I can't file an issue for you. Ask a workspace admin to invite you, then send the command again."
	msgIssueDisabled  = "This Discord bot isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, discordUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for Discord.
type OutboundReplier struct {
	binding     bindingMinter
	decrypt     Decrypter
	appURL      string
	bindingPath string
	apiBase     string
	logger      *slog.Logger
}

// OutboundReplierConfig configures the replier. Binding + AppURL are required
// for the NeedsBinding prompt; without them the prompt is skipped (other
// notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	Decrypt Decrypter
	// AppURL is the Multica web app host for the redeem link, same sourcing as
	// the Slack/Telegram repliers (MULTICA_APP_URL ?? FRONTEND_ORIGIN).
	AppURL      string
	BindingPath string // default "/discord/bind"
	APIBase     string
	Logger      *slog.Logger
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
		bindingPath = "/discord/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		decrypt:     cfg.Decrypt,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		apiBase:     cfg.APIBase,
		logger:      logger,
	}
}

// Reply routes each outcome to its user-visible message. Errors are logged,
// not propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	var text string
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "discord replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
		return
	case engine.OutcomeAgentOffline:
		text = msgAgentOffline
	case engine.OutcomeAgentArchived:
		text = msgAgentArchived
	case engine.OutcomeFreshPending:
		text = msgFreshPending
	case engine.OutcomeChatStarted:
		text = msgChatStarted
	case engine.OutcomeIssueUsage:
		text = msgIssueUsage
	case engine.OutcomeIngested:
		// Only an /issue product result warrants an immediate reply; a plain
		// chat message stays silent (the agent's own reply lands via ChatDone).
		if !res.IssueID.Valid {
			return
		}
		text = issueCreatedText(res)
		if res.IssueDuplicate {
			text = issueDuplicateText(res)
		}
	case engine.OutcomeDropped:
		text = droppedReplyText(res, msg)
	default:
		return
	}
	if text == "" {
		return
	}
	if err := r.post(ctx, inst, msg, text); err != nil {
		r.logger.WarnContext(ctx, "discord replier: reply failed",
			"installation_id", util.UUIDToString(inst.ID), "outcome", string(res.Outcome), "error", err)
	}
}

func (r *OutboundReplier) sendBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) error {
	// A guild-channel bearer link can be redeemed by another channel member and
	// would bind the original sender's Discord identity to the wrong Multica
	// user. Ask the sender to DM the bot first; only DM prompts carry a token.
	if msg.Source.ChatType == channel.ChatTypeGroup {
		return r.post(ctx, inst, msg, msgBindingGroup)
	}
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
	text := "👋 To start chatting with me, link your Discord account to Multica:\n" + bindURL +
		"\n(This link expires in 15 minutes.)"
	return r.post(ctx, inst, msg, text)
}

// post resolves the installation's bot token from the carried platform row
// and sends plain text back into the originating channel.
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, r.decrypt)
	if err != nil {
		return fmt.Errorf("decode credentials: %w", err)
	}
	_, err = sendText(ctx, newAPIClient(r.apiBase, creds.BotToken, nil), msg.Source.ChatID, text, msg.MessageID)
	if err != nil {
		return fmt.Errorf("post discord reply: %w", err)
	}
	return nil
}

func issueCreatedText(res engine.Result) string {
	id := issueResultIdentifier(res)
	title := strings.TrimSpace(res.IssueTitle)
	if title == "" {
		return "✅ Created " + id
	}
	return "✅ Created " + id + " — " + title
}

func issueDuplicateText(res engine.Result) string {
	id := issueResultIdentifier(res)
	title := strings.TrimSpace(res.IssueTitle)
	if title == "" {
		return "⚠️ Not created — active issue " + id + " already exists."
	}
	return "⚠️ Not created — active issue " + id + " already exists: " + title
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
		return msgIssueNotMember
	case engine.DropReasonRevokedInstallation:
		return msgIssueDisabled
	default:
		return ""
	}
}
