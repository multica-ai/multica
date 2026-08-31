package mattermost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the Mattermost OutboundReplier — the engine seam that delivers
// a verdict-driven reply back to the user, mirroring slack/replier.go and
// telegram/replier.go:
//   - NeedsBinding: mint a single-use binding token, reply with the link to
//     the in-product redeem page (/mattermost/bind).
//   - AgentOffline / AgentArchived: a status notice.
//   - FreshPending / ChatStarted / IssueUsage: command confirmation or
//     corrective guidance.
//   - Ingested with an /issue result: creation or duplicate confirmation.
//   - Dropped addressed /issue commands: an authorization/status refusal.

const (
	msgFreshPending   = ":white_check_mark: Fresh start ready. Your next chat message will run without previous context."
	msgChatStarted    = ":white_check_mark: Started a new Multica chat. Your next message will enter it."
	msgIssueUsage     = "Please include an issue title. Use:\n\n```\n/issue <title>\n[description] (optional)\n```"
	msgIssueNotMember = "You're not a member of this Multica workspace, so I can't file an issue for you. Ask a workspace admin to invite you, then send the command again."
	msgIssueDisabled  = "This Mattermost bot isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, mattermostUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for Mattermost.
type OutboundReplier struct {
	binding     bindingMinter
	decrypt     Decrypter
	appURL      string
	bindingPath string
	client      *http.Client
	logger      *slog.Logger
}

// OutboundReplierConfig configures the replier. Binding + AppURL are required
// for the NeedsBinding prompt; without them the prompt is skipped (other
// notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	Decrypt Decrypter
	// AppURL is the Multica web app host for the redeem link, same sourcing as
	// the Slack and Telegram repliers (MULTICA_APP_URL ?? FRONTEND_ORIGIN).
	AppURL      string
	BindingPath string // default "/mattermost/bind"
	HTTPClient  *http.Client
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
		bindingPath = "/mattermost/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		decrypt:     cfg.Decrypt,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		client:      cfg.HTTPClient,
		logger:      logger,
	}
}

// Reply routes each outcome to its user-visible message. Errors are logged,
// not propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.warn(ctx, "binding prompt failed", inst, err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, inst, msg, msgAgentOffline); err != nil {
			r.warn(ctx, "offline notice failed", inst, err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, inst, msg, msgAgentArchived); err != nil {
			r.warn(ctx, "archived notice failed", inst, err)
		}
	case engine.OutcomeFreshPending:
		if err := r.post(ctx, inst, msg, msgFreshPending); err != nil {
			r.warn(ctx, "fresh-start confirmation failed", inst, err)
		}
	case engine.OutcomeChatStarted:
		if err := r.post(ctx, inst, msg, msgChatStarted); err != nil {
			r.warn(ctx, "new-chat confirmation failed", inst, err)
		}
	case engine.OutcomeIssueUsage:
		if err := r.post(ctx, inst, msg, msgIssueUsage); err != nil {
			r.warn(ctx, "issue usage reply failed", inst, err)
		}
	case engine.OutcomeIngested:
		if res.IssueID.Valid {
			text := issueCreatedText(res)
			if res.IssueDuplicate {
				text = issueDuplicateText(res)
			}
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.warn(ctx, "issue outcome reply failed", inst, err)
			}
		}
	case engine.OutcomeDropped:
		if text := droppedReplyText(res, msg); text != "" {
			if err := r.post(ctx, inst, msg, text); err != nil {
				r.warn(ctx, "drop refusal failed", inst, err)
			}
		}
	}
}

func (r *OutboundReplier) warn(ctx context.Context, what string, inst engine.ResolvedInstallation, err error) {
	r.logger.WarnContext(ctx, "mattermost replier: "+what,
		"installation_id", util.UUIDToString(inst.ID), "error", err)
}

func (r *OutboundReplier) sendBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) error {
	// A channel-visible bearer link can be redeemed by another member and
	// would bind the original sender's Mattermost identity to the wrong
	// Multica user. Ask the sender to open a direct chat first; only DM
	// prompts carry a redeem token.
	if msg.Source.ChatType == channel.ChatTypeGroup {
		return r.post(ctx, inst, msg, msgBindingGroupHint)
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
	text := ":wave: To start chatting with me, link your Mattermost account to Multica:\n" +
		bindURL + "\n(This link expires in 15 minutes.)"
	return r.post(ctx, inst, msg, text)
}

// post resolves the installation's credentials from the carried platform row
// and sends plain text back into the originating channel / thread.
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, r.decrypt)
	if err != nil {
		return fmt.Errorf("decode credentials: %w", err)
	}
	if creds.ServerURL == "" {
		return errors.New("installation has no server url")
	}
	if _, err := newRESTClient(creds.ServerURL, creds.AccessToken, r.client).CreatePost(ctx, Post{
		ChannelID: msg.Source.ChatID,
		RootID:    replyRoot(msg.Source.ThreadID, msg.MessageID),
		Message:   text,
	}); err != nil {
		return fmt.Errorf("post mattermost reply: %w", err)
	}
	return nil
}

func issueCreatedText(res engine.Result) string {
	id := issueResultIdentifier(res)
	title := strings.TrimSpace(res.IssueTitle)
	if title == "" {
		return ":white_check_mark: Created " + id
	}
	return ":white_check_mark: Created " + id + " — " + title
}

func issueDuplicateText(res engine.Result) string {
	id := issueResultIdentifier(res)
	title := strings.TrimSpace(res.IssueTitle)
	if title == "" {
		return ":warning: Not created — active issue " + id + " already exists."
	}
	return ":warning: Not created — active issue " + id + " already exists: " + title
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
