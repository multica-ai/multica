package dingtalk

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file implements the DingTalk `/issue` command as a quick-create entry
// point, mirroring the Slack slash command. DingTalk has no native slash-command
// transport — `/issue` arrives as an ordinary text message — so it is intercepted
// in onMessage (dingtalk_channel.go) before the engine and diverted here.
//
// Like the web "quick create" modal, it does not create the issue itself: it
// enqueues the invoker's description as a quick-create task and the agent authors
// a well-formed issue in the background, attributed to the bound member. Creation
// is asynchronous, so the reply is an acknowledgement (no issue number) and the
// agent's completion surfaces as a Multica inbox notification. It starts no chat
// session or chat run.
//
// Installation routing and identity/membership checks are kept local (a narrow
// query interface) so the proven inbound pipeline is untouched.

// User-facing replies. Kept terse; only the invoker's conversation sees them.
const (
	issueUsageText     = "Tell me what to file, e.g. `/issue the login button does nothing on Safari`."
	issueQueuedText    = "👀 On it — I'm turning that into an issue. You'll get a Multica notification when it's ready."
	issueNotMemberText = "You're not a member of this Multica workspace, so I can't file an issue for you."
	issueInternalError = "⚠️ Something went wrong creating the issue. Please try again."
	issueDisabledText  = "This DingTalk robot isn't connected to Multica (or was disconnected). Ask a workspace admin to reconnect it."
)

// issueCommandQueries is the narrow slice of generated queries the processor
// needs. *db.Queries satisfies it; tests supply a fake.
type issueCommandQueries interface {
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

// quickCreateEnqueuer is the narrow slice of *service.TaskService the command
// needs to hand the invoker's prompt to the agent. *service.TaskService
// satisfies it; tests supply a fake.
type quickCreateEnqueuer interface {
	EnqueueQuickCreateTask(ctx context.Context, workspaceID, requesterID, agentID, squadID pgtype.UUID, prompt string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID) (db.AgentTaskQueue, error)
}

// issueCommandReplier posts the processor's user-facing messages back into the
// originating conversation. *OutboundReplier satisfies it — reusing its proven
// credential-decode + binding-token mint paths; tests supply a fake.
type issueCommandReplier interface {
	post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error
	sendBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) error
}

var _ issueCommandReplier = (*OutboundReplier)(nil)

// IssueCommandProcessor handles the DingTalk `/issue` command end to end.
type IssueCommandProcessor struct {
	q       issueCommandQueries
	tasks   quickCreateEnqueuer
	replier issueCommandReplier
	logger  *slog.Logger
}

// IssueCommandConfig configures the processor. All fields are required for the
// command to do anything.
type IssueCommandConfig struct {
	Queries *db.Queries
	Tasks   quickCreateEnqueuer
	Replier *OutboundReplier
	Logger  *slog.Logger
}

// NewIssueCommandProcessor builds the processor.
func NewIssueCommandProcessor(cfg IssueCommandConfig) *IssueCommandProcessor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &IssueCommandProcessor{
		q:       cfg.Queries,
		tasks:   cfg.Tasks,
		replier: cfg.Replier,
		logger:  logger,
	}
}

// Handle processes one `/issue` message and delivers the reply. It is called
// from a detached goroutine (the Stream frame has already been ACKed), so it
// never returns an error — every outcome is a user-facing message.
func (p *IssueCommandProcessor) Handle(ctx context.Context, msg channel.InboundMessage) {
	inst, ok := p.resolveInstallation(ctx, msg)
	if !ok {
		return
	}
	prompt := issuePrompt(msg.Text)
	if prompt == "" {
		p.reply(ctx, inst, msg, issueUsageText)
		return
	}
	userID, ok := p.resolveUser(ctx, inst, msg)
	if !ok {
		return // resolveUser already replied (binding prompt / not-member / error)
	}
	// Enqueue against the installation's own agent — no squad, project, parent,
	// or attachments.
	if _, err := p.tasks.EnqueueQuickCreateTask(
		ctx,
		inst.WorkspaceID,
		userID,
		inst.AgentID,
		pgtype.UUID{}, // no squad
		prompt,
		pgtype.UUID{}, // no project
		pgtype.UUID{}, // no parent issue
		nil,           // no attachments
	); err != nil {
		p.logger.WarnContext(ctx, "dingtalk /issue: enqueue quick-create failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		p.reply(ctx, inst, msg, issueInternalError)
		return
	}
	p.reply(ctx, inst, msg, issueQueuedText)
}

// resolveInstallation maps the inbound message's stamped AppKey to its
// installation, mirroring installationResolver. An unroutable event stays silent
// (there is nowhere to reply); an inactive installation replies "disconnected".
func (p *IssueCommandProcessor) resolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, bool) {
	raw, err := decodeDingTalkRaw(msg)
	if err != nil {
		p.logger.WarnContext(ctx, "dingtalk /issue: decode inbound raw failed", "error", err)
		return engine.ResolvedInstallation{}, false
	}
	row, err := p.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeDingTalk),
		AppID:       raw.AppID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			p.logger.WarnContext(ctx, "dingtalk /issue: resolve installation failed",
				"app_id", raw.AppID, "error", err)
		}
		return engine.ResolvedInstallation{}, false
	}
	inst := engine.ResolvedInstallation{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		AgentID:         row.AgentID,
		InstallerUserID: row.InstallerUserID,
		Active:          row.Status == "active",
		Platform:        row,
	}
	if !inst.Active {
		p.reply(ctx, inst, msg, issueDisabledText)
		return engine.ResolvedInstallation{}, false
	}
	return inst, true
}

// resolveUser maps the DingTalk sender to the bound Multica user, re-checking
// workspace membership. Unbound senders get a binding prompt; non-members get a
// refusal. Returns ok=false (having already replied) for every non-success case.
func (p *IssueCommandProcessor) resolveUser(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (pgtype.UUID, bool) {
	binding, err := p.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  msg.Source.SenderID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if berr := p.replier.sendBindingPrompt(ctx, inst, msg, engine.Result{Sender: msg.Source.SenderID}); berr != nil {
				p.logger.WarnContext(ctx, "dingtalk /issue: binding prompt failed",
					"installation_id", util.UUIDToString(inst.ID), "error", berr)
			}
			return pgtype.UUID{}, false
		}
		p.logger.WarnContext(ctx, "dingtalk /issue: resolve binding failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		p.reply(ctx, inst, msg, issueInternalError)
		return pgtype.UUID{}, false
	}
	// Binding existence no longer proves membership (no FK); re-check.
	if _, err := p.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.MulticaUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			p.reply(ctx, inst, msg, issueNotMemberText)
			return pgtype.UUID{}, false
		}
		p.logger.WarnContext(ctx, "dingtalk /issue: resolve membership failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
		p.reply(ctx, inst, msg, issueInternalError)
		return pgtype.UUID{}, false
	}
	return binding.MulticaUserID, true
}

// reply posts one user-facing message, logging (not surfacing) any send failure.
func (p *IssueCommandProcessor) reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) {
	if err := p.replier.post(ctx, inst, msg, text); err != nil {
		p.logger.WarnContext(ctx, "dingtalk /issue: reply failed",
			"installation_id", util.UUIDToString(inst.ID), "error", err)
	}
}

// issuePrompt strips the leading `/issue` token from a command body and returns
// the remaining natural-language text verbatim (first-line remainder joined with
// any following lines). Unlike the engine's ParseIssueCommand it does NOT split
// into title/description — quick-create hands the whole prompt to the agent,
// which authors the title itself. Returns "" for a bare `/issue`.
func issuePrompt(text string) string {
	cmd, ok := engine.ParseIssueCommand(text)
	if !ok {
		return ""
	}
	switch {
	case cmd.Description == "":
		return cmd.Title
	case cmd.Title == "":
		return cmd.Description
	default:
		return cmd.Title + "\n" + cmd.Description
	}
}
