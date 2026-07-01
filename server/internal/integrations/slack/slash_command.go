package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file implements the Slack `/issue` SLASH COMMAND. It is deliberately
// separate from the message-based `/issue` (engine ParseIssueCommand): on Slack
// a message whose first character is `/` is intercepted by the client as a
// slash command and never delivered to the app, so the message-prefix form of
// `/issue` cannot work here at all (MUL-3908). Registering `/issue` as a real
// slash command in the app manifest is what makes it reach us — as an
// `EventTypeSlashCommand` over the same Socket Mode connection.
//
// Unlike the message path (chat session + dedup + debounced chat run), a slash
// command creates no channel message and starts no chat session / chat run: it
// is a one-shot issue creation with a PRIVATE (ephemeral) confirmation back to
// the invoker via the command's response_url. It reuses the same installation
// routing, identity + membership checks, and the shared IssueService, so a
// slash-command issue shares the counter, dup guard, project boundary,
// broadcast, analytics and agent-enqueue with every other create path — i.e. a
// `todo` issue assigned to the agent still triggers the agent through the normal
// issue-assignment path (maybeEnqueueOnAssign), exactly like the message /issue.

const issueSlashCommand = "/issue"

// User-facing ephemeral replies. Kept terse; only the invoker sees them.
const (
	slashUsageText           = "Please give the issue a title, e.g. `/issue Fix the login redirect`."
	slashNotMemberText       = "You're not a member of this Multica workspace, so I can't file an issue for you."
	slashLinkAccountFallback = "Link your Slack account to Multica first, then try `/issue` again."
	slashInternalErrorText   = "⚠️ Something went wrong creating the issue. Please try again."
	slashDisabledText        = "This Slack app isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
)

// slashQueries is the narrow slice of generated queries the slash-command
// processor needs. *db.Queries satisfies it; tests supply a fake. The
// installation / member resolution mirrors the message-path resolvers
// (resolvers.go) but is kept local so the proven inbound pipeline is untouched.
type slashQueries interface {
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// SlashCommandProcessor handles the Slack `/issue` slash command end to end.
type SlashCommandProcessor struct {
	q           slashQueries
	issues      engine.IssueCreator
	binding     bindingMinter
	appURL      string
	bindingPath string
	logger      *slog.Logger
	// respond posts an ephemeral reply to the command's response_url. Injected
	// so tests can capture the reply without hitting Slack.
	respond func(ctx context.Context, responseURL, text string) error
}

// SlashCommandConfig configures the processor. Binding + AppURL are required for
// the unbound-user "link your account" reply; without them that case falls back
// to a plain instruction. Issues + Queries are required for the command to do
// anything.
type SlashCommandConfig struct {
	Queries     *db.Queries
	Issues      engine.IssueCreator
	Binding     bindingMinter
	AppURL      string
	BindingPath string // default "/slack/bind"
	Logger      *slog.Logger
}

// NewSlashCommandProcessor builds the processor. The default responder POSTs an
// ephemeral message to the command's response_url (a signed webhook — no bot
// token required).
func NewSlashCommandProcessor(cfg SlashCommandConfig) *SlashCommandProcessor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/slack/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	p := &SlashCommandProcessor{
		q:           cfg.Queries,
		issues:      cfg.Issues,
		binding:     cfg.Binding,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
	p.respond = func(ctx context.Context, responseURL, text string) error {
		return slack.PostWebhookContext(ctx, responseURL, &slack.WebhookMessage{
			ResponseType: slack.ResponseTypeEphemeral,
			Text:         text,
		})
	}
	return p
}

// Handle processes one slash command and delivers the ephemeral reply. It is
// called from a detached goroutine (the socket receive loop has already ACKed),
// so it never returns an error — every outcome is a user-facing message.
func (p *SlashCommandProcessor) Handle(ctx context.Context, cmd slack.SlashCommand) {
	// Only /issue is registered in the manifest; ignore anything else defensively.
	if !strings.EqualFold(strings.TrimSpace(cmd.Command), issueSlashCommand) {
		return
	}
	text := p.process(ctx, cmd)
	if text == "" || cmd.ResponseURL == "" {
		return
	}
	if err := p.respond(ctx, cmd.ResponseURL, text); err != nil {
		p.logger.WarnContext(ctx, "slack slash command: response_url reply failed",
			"app_id", cmd.APIAppID, "error", err)
	}
}

// process runs the command and returns the ephemeral text to reply with.
func (p *SlashCommandProcessor) process(ctx context.Context, cmd slack.SlashCommand) string {
	title, description := splitIssueText(cmd.Text)
	if title == "" {
		return slashUsageText
	}

	inst, err := p.resolveInstallation(ctx, cmd.APIAppID, cmd.TeamID)
	if err != nil {
		if !errors.Is(err, engine.ErrInstallationNotFound) {
			p.logger.WarnContext(ctx, "slack slash command: resolve installation failed",
				"app_id", cmd.APIAppID, "error", err)
			return slashInternalErrorText
		}
		return slashDisabledText
	}
	if !inst.Active {
		return slashDisabledText
	}

	userID, err := p.resolveUser(ctx, inst, cmd.UserID)
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSenderUnbound):
			return p.bindingText(ctx, inst, cmd.UserID)
		case errors.Is(err, engine.ErrSenderNotMember):
			return slashNotMemberText
		default:
			p.logger.WarnContext(ctx, "slack slash command: resolve user failed",
				"app_id", cmd.APIAppID, "error", err)
			return slashInternalErrorText
		}
	}

	res, err := p.issues.Create(ctx, service.IssueCreateParams{
		WorkspaceID:  inst.WorkspaceID,
		Title:        title,
		Description:  pgtype.Text{String: description, Valid: description != ""},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   inst.AgentID,
		CreatorType:  "member",
		CreatorID:    userID,
		OriginType:   pgtype.Text{String: originSlackChat, Valid: true},
		// No chat session backs a slash command, so OriginID stays NULL.
	}, service.IssueCreateOpts{})
	if err != nil {
		p.logger.WarnContext(ctx, "slack slash command: create issue failed",
			"app_id", cmd.APIAppID, "error", err)
		return slashInternalErrorText
	}
	return p.issueCreatedText(ctx, inst.WorkspaceID, res.Issue)
}

// resolveInstallation maps the command's api_app_id (+ event team) to its
// installation, applying the same team-scoping guard as inbound routing.
func (p *SlashCommandProcessor) resolveInstallation(ctx context.Context, appID, teamID string) (engine.ResolvedInstallation, error) {
	inst, err := p.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       appID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
		}
		return engine.ResolvedInstallation{}, err
	}
	if !installationServesTeam(inst.Config, teamID) {
		return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
	}
	return engine.ResolvedInstallation{
		ID:              inst.ID,
		WorkspaceID:     inst.WorkspaceID,
		AgentID:         inst.AgentID,
		InstallerUserID: inst.InstallerUserID,
		Active:          inst.Status == "active",
		Platform:        inst,
	}, nil
}

// resolveUser maps the Slack user id to the bound Multica user, re-checking
// workspace membership (no binding→member FK). Returns engine.ErrSenderUnbound
// or engine.ErrSenderNotMember for the product cases.
func (p *SlashCommandProcessor) resolveUser(ctx context.Context, inst engine.ResolvedInstallation, slackUserID string) (pgtype.UUID, error) {
	binding, err := p.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  slackUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrSenderUnbound
		}
		return pgtype.UUID{}, err
	}
	if _, err := p.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.MulticaUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrSenderNotMember
		}
		return pgtype.UUID{}, err
	}
	return binding.MulticaUserID, nil
}

// bindingText mints a single-use binding token and returns a "link your account"
// prompt, mirroring the outbound replier's NeedsBinding message. Falls back to a
// plain instruction when the binding service / app URL are not configured.
func (p *SlashCommandProcessor) bindingText(ctx context.Context, inst engine.ResolvedInstallation, slackUserID string) string {
	if p.binding == nil || p.appURL == "" {
		return slashLinkAccountFallback
	}
	token, err := p.binding.Mint(ctx, inst.WorkspaceID, inst.ID, slackUserID)
	if err != nil {
		p.logger.WarnContext(ctx, "slack slash command: mint binding token failed",
			"installation_id", inst.ID, "error", err)
		return slashLinkAccountFallback
	}
	bindURL := p.appURL + p.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	// Wrap the URL as an explicit Slack link so the base64url token's `_`/`-`
	// are not mangled by mrkdwn (same reasoning as the replier).
	return "👋 To file issues, link your Slack account to Multica: <" +
		bindURL + "|link your account>\n(This link expires in 15 minutes.)"
}

// issueCreatedText renders the success confirmation with the workspace-prefixed
// identifier (falling back to #<number> when no prefix is set).
func (p *SlashCommandProcessor) issueCreatedText(ctx context.Context, workspaceID pgtype.UUID, issue db.Issue) string {
	identifier := fmt.Sprintf("#%d", issue.Number)
	if ws, err := p.q.GetWorkspace(ctx, workspaceID); err == nil && ws.IssuePrefix != "" {
		identifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
	}
	title := strings.TrimSpace(issue.Title)
	if title == "" {
		return "✅ Created " + identifier
	}
	return "✅ Created " + identifier + " — " + title
}

// splitIssueText splits the slash-command argument string into a title (first
// non-empty line) and an optional description (the remaining lines). Slack
// slash-command input is normally single-line, so the description is usually
// empty.
func splitIssueText(text string) (title, description string) {
	lines := strings.Split(text, "\n")
	first := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = i
			break
		}
	}
	if first == -1 {
		return "", ""
	}
	title = strings.TrimSpace(lines[first])
	if first+1 < len(lines) {
		description = strings.TrimRight(strings.Join(lines[first+1:], "\n"), " \t\n")
	}
	return title, description
}
