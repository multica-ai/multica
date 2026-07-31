package lark

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const projectBindingCodeTTL = 10 * time.Minute

var (
	ErrProjectSyncNotFound     = errors.New("project sync binding not found")
	ErrProjectSyncForbidden    = errors.New("project sync permission denied")
	ErrProjectSyncConflict     = errors.New("project sync binding conflict")
	ErrProjectSyncInvalidInput = errors.New("invalid project sync input")
	ErrIssueTopicManualUnbound = errors.New("issue topic manually unbound")
)

// ChatInfo is the non-sensitive chat metadata persisted on a project binding.
type ChatInfo struct {
	ID   string
	Name string
}

// ChatInfoClient is an optional extension implemented by the production HTTP
// client. Keeping it separate from APIClient avoids widening every existing
// test fake that only sends messages.
type ChatInfoClient interface {
	GetChatInfo(ctx context.Context, creds InstallationCredentials, chatID ChatID) (ChatInfo, error)
}

type ProjectSyncService struct {
	store       *projectSyncStore
	queries     *db.Queries
	issues      *service.IssueService
	tasks       *service.TaskService
	client      APIClient
	credentials CredentialsResolver
	appURL      string
	logger      *slog.Logger
}

type ProjectSyncServiceConfig struct {
	Pool        *pgxpool.Pool
	Queries     *db.Queries
	Issues      *service.IssueService
	Tasks       *service.TaskService
	APIClient   APIClient
	Credentials CredentialsResolver
	AppURL      string
	Logger      *slog.Logger
}

func NewProjectSyncService(cfg ProjectSyncServiceConfig) (*ProjectSyncService, error) {
	if cfg.Pool == nil || cfg.Queries == nil || cfg.Issues == nil || cfg.Tasks == nil {
		return nil, errors.New("lark project sync: pool, queries, issue service and task service are required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &ProjectSyncService{
		store:       newProjectSyncStore(cfg.Pool),
		queries:     cfg.Queries,
		issues:      cfg.Issues,
		tasks:       cfg.Tasks,
		client:      cfg.APIClient,
		credentials: cfg.Credentials,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		logger:      cfg.Logger,
	}, nil
}

func (s *ProjectSyncService) HandleProjectCommand(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	switch p.Command.Resource {
	case "help":
		return commandReply(projectCommandHelp()), nil
	case "project":
		return s.handleProjectCommand(ctx, p)
	case "issue":
		return s.handleIssueCommand(ctx, p)
	default:
		return commandReply(projectCommandHelp()), nil
	}
}

func (s *ProjectSyncService) handleProjectCommand(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	switch p.Command.Action {
	case "bind":
		return s.commandProjectBind(ctx, p)
	case "status":
		return s.commandProjectStatus(ctx, p)
	case "list":
		return s.commandProjectList(ctx, p)
	case "unbind":
		return s.commandProjectUnbind(ctx, p)
	case "help":
		return commandReply(projectCommandHelp()), nil
	default:
		return commandReply("Unknown /project action.\n\n" + projectCommandHelp()), nil
	}
}

func (s *ProjectSyncService) handleIssueCommand(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	switch p.Command.Action {
	case "create":
		return s.commandIssueCreate(ctx, p)
	case "bind":
		return s.commandIssueBind(ctx, p)
	case "unbind":
		return s.commandIssueUnbind(ctx, p)
	case "status":
		return s.commandIssueStatus(ctx, p)
	case "stop":
		return s.commandIssueStop(ctx, p)
	case "show":
		return s.commandIssueShow(ctx, p)
	case "help":
		return commandReply(issueCommandHelp()), nil
	default:
		return commandReply("Unknown /issue action.\n\n" + issueCommandHelp()), nil
	}
}

func (s *ProjectSyncService) commandProjectBind(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	if p.Message.Source.ChatType == channel.ChatTypeP2P {
		if len(p.Command.Arguments) == 0 {
			text, err := s.availableProjectsText(ctx, p.Installation.WorkspaceID)
			if err != nil {
				return engine.ProjectCommandResult{}, err
			}
			return commandReply(text + "\n\nRun /project bind <project UUID or exact title>."), nil
		}
		project, reply, err := s.resolveProjectForCommand(ctx, p.Installation.WorkspaceID, strings.Join(p.Command.Arguments, " "))
		if err != nil || reply != "" {
			return commandReply(reply), err
		}
		if !s.canManageWorkspace(ctx, p.Installation.WorkspaceID, p.UserID) {
			return commandReply("Only workspace owners and admins can bind Project synchronization."), nil
		}
		binding, code, err := s.BeginProjectBinding(ctx, p.Installation.WorkspaceID, project.ID, p.Installation.ID, p.UserID)
		if err != nil {
			return commandReply(projectSyncErrorText(err)), nil
		}
		_ = binding
		return commandReply(fmt.Sprintf(
			"Project %s is waiting for a Feishu group.\n\nAdd this Bot to the target group, then run:\n/project bind %s\n\nThe code expires in 10 minutes.",
			project.Title, code,
		)), nil
	}

	if len(p.Command.Arguments) == 0 {
		text, err := s.availableProjectsText(ctx, p.Installation.WorkspaceID)
		if err != nil {
			return engine.ProjectCommandResult{}, err
		}
		return commandReply(text + "\n\nRun /project bind <project UUID or exact title>."), nil
	}

	argument := strings.Join(p.Command.Arguments, " ")
	if binding, ok, err := s.confirmCodeInGroup(ctx, p, argument); err != nil {
		if ok && (errors.Is(err, ErrProjectSyncForbidden) || errors.Is(err, ErrProjectSyncConflict) || errors.Is(err, ErrProjectSyncInvalidInput)) {
			return commandReply(projectSyncErrorText(err)), nil
		}
		return engine.ProjectCommandResult{}, err
	} else if ok {
		return commandReply(fmt.Sprintf("Bound Project to this group.\nProject binding: %s", util.UUIDToString(binding.ID))), nil
	}

	project, reply, err := s.resolveProjectForCommand(ctx, p.Installation.WorkspaceID, argument)
	if err != nil || reply != "" {
		return commandReply(reply), err
	}
	if !s.canManageWorkspace(ctx, p.Installation.WorkspaceID, p.UserID) {
		return commandReply("Only workspace owners and admins can bind Project synchronization."), nil
	}
	binding, err := s.bindProjectInGroup(ctx, p, project)
	if err != nil {
		return commandReply(projectSyncErrorText(err)), nil
	}
	return commandReply(fmt.Sprintf(
		"Project %s is now synchronized with this group.\nBinding: %s",
		project.Title, util.UUIDToString(binding.ID),
	)), nil
}

func (s *ProjectSyncService) commandProjectStatus(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	if p.Message.Source.ChatType == channel.ChatTypeP2P {
		return s.commandProjectList(ctx, p)
	}
	binding, err := s.store.getActiveProjectBindingByGroup(ctx, s.store.pool, p.Installation.ID, p.Message.Source.ChatID)
	if isNoRows(err) {
		return commandReply("This Bot and group are not bound to a Multica Project."), nil
	}
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	project, err := s.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: binding.ProjectID, WorkspaceID: binding.WorkspaceID,
	})
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	return commandReply(fmt.Sprintf(
		"Project: %s\nGroup: %s\nState: %s\nBinding: %s",
		project.Title, binding.ChannelChatName.String, binding.State, util.UUIDToString(binding.ID),
	)), nil
}

func (s *ProjectSyncService) commandProjectList(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	items, err := s.store.listProjectBindingsByInstallation(ctx, s.store.pool, p.Installation.WorkspaceID, p.Installation.ID)
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	if len(items) == 0 {
		return commandReply("This Bot has no Project bindings."), nil
	}
	var b strings.Builder
	b.WriteString("Project bindings:\n")
	for _, item := range items {
		group := "waiting for group confirmation"
		if item.Binding.ChannelChatName.Valid {
			group = item.Binding.ChannelChatName.String
		}
		fmt.Fprintf(&b, "• %s → %s (%s)\n", item.ProjectTitle, group, item.Binding.State)
	}
	return commandReply(strings.TrimSpace(b.String())), nil
}

func (s *ProjectSyncService) commandProjectUnbind(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	if !s.canManageWorkspace(ctx, p.Installation.WorkspaceID, p.UserID) {
		return commandReply("Only workspace owners and admins can unbind Project synchronization."), nil
	}

	var binding ChannelProjectBinding
	var err error
	if p.Message.Source.ChatType == channel.ChatTypeGroup {
		binding, err = s.store.getActiveProjectBindingByGroup(ctx, s.store.pool, p.Installation.ID, p.Message.Source.ChatID)
	} else {
		if len(p.Command.Arguments) == 0 {
			return commandReply("Run /project unbind <project UUID or exact title>."), nil
		}
		project, reply, resolveErr := s.resolveProjectForCommand(ctx, p.Installation.WorkspaceID, strings.Join(p.Command.Arguments, " "))
		if resolveErr != nil || reply != "" {
			return commandReply(reply), resolveErr
		}
		binding, err = s.store.getCurrentProjectBinding(ctx, s.store.pool, p.Installation.WorkspaceID, project.ID)
	}
	if isNoRows(err) {
		return commandReply("No active Project binding was found."), nil
	}
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	if err := s.UnbindProject(ctx, binding.WorkspaceID, binding.ID, p.UserID); err != nil {
		return commandReply(projectSyncErrorText(err)), nil
	}
	return commandReply("Project synchronization was unbound. Historical Feishu topics were preserved."), nil
}

func (s *ProjectSyncService) commandIssueCreate(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	var binding ChannelProjectBinding
	var project db.Project
	var title string
	var err error

	if p.Message.Source.ChatType == channel.ChatTypeGroup {
		binding, err = s.store.getActiveProjectBindingByGroup(ctx, s.store.pool, p.Installation.ID, p.Message.Source.ChatID)
		if isNoRows(err) {
			return commandReply("Bind this Bot and group to a Project first with /project bind."), nil
		}
		if err != nil {
			return engine.ProjectCommandResult{}, err
		}
		project, err = s.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID: binding.ProjectID, WorkspaceID: binding.WorkspaceID,
		})
		if err != nil {
			return engine.ProjectCommandResult{}, err
		}
		title = strings.TrimSpace(p.Command.RawArguments)
	} else {
		projectToken, issueTitle, ok := splitPrivateCreateArguments(p.Command.RawArguments)
		if !ok {
			text, listErr := s.availableProjectsText(ctx, p.Installation.WorkspaceID)
			if listErr != nil {
				return engine.ProjectCommandResult{}, listErr
			}
			return commandReply(text + "\n\nPrivate-chat syntax:\n/issue create <project UUID> -- <title>"), nil
		}
		project, _, err = s.resolveProjectForCommand(ctx, p.Installation.WorkspaceID, projectToken)
		if err != nil {
			return engine.ProjectCommandResult{}, err
		}
		if !project.ID.Valid {
			return commandReply("Project not found."), nil
		}
		title = issueTitle
		binding, err = s.store.getCurrentProjectBinding(ctx, s.store.pool, p.Installation.WorkspaceID, project.ID)
		if err != nil && !isNoRows(err) {
			return engine.ProjectCommandResult{}, err
		}
	}

	if title == "" {
		return commandReply("Issue title is required.\nUsage: /issue create <title>"), nil
	}

	topicRoot := topicRootMessageID(p.Message)
	opts := service.IssueCreateOpts{Platform: "lark"}
	if p.Message.Source.ChatType == channel.ChatTypeGroup && topicRoot != "" {
		opts.WithinTransaction = func(hookCtx context.Context, tx pgx.Tx, issue db.Issue) error {
			_, err := s.store.createIssueTopic(
				hookCtx, tx, binding.WorkspaceID, binding.InstallationID, binding.ID, binding.ProjectID,
				issue.ID, p.Message.Source.ChatID, topicRoot,
				p.Message.Source.ThreadID, "issue_created_in_topic", p.UserID,
			)
			return err
		}
	}

	result, err := s.issues.Create(ctx, service.IssueCreateParams{
		WorkspaceID:  p.Installation.WorkspaceID,
		Title:        title,
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   p.Installation.AgentID,
		CreatorType:  "member",
		CreatorID:    p.UserID,
		ProjectID:    project.ID,
		OriginType:   pgtype.Text{String: originFeishuChat, Valid: true},
	}, opts)
	if err != nil {
		if errors.Is(err, service.ErrActiveDuplicate) && result.DuplicateIssue != nil {
			return commandReply(fmt.Sprintf("An active duplicate already exists: %s", s.issueIdentifier(ctx, *result.DuplicateIssue))), nil
		}
		return engine.ProjectCommandResult{}, err
	}
	identifier := s.issueIdentifier(ctx, result.Issue)
	reply := fmt.Sprintf("Created %s — %s\nProject: %s\nStatus: %s", identifier, result.Issue.Title, project.Title, result.Issue.Status)
	if p.Message.Source.ChatType == channel.ChatTypeP2P && !binding.ID.Valid {
		reply += "\n\nThe Issue was created, but this Project has no active Feishu group synchronization."
	}
	return engine.ProjectCommandResult{
		ReplyText:       reply,
		IssueID:         result.Issue.ID,
		IssueNumber:     result.Issue.Number,
		IssueIdentifier: identifier,
		IssueTitle:      result.Issue.Title,
	}, nil
}

func (s *ProjectSyncService) commandIssueBind(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	if p.Message.Source.ChatType != channel.ChatTypeGroup {
		return commandReply("/issue bind must be run inside the target Feishu topic."), nil
	}
	if !s.canManageWorkspace(ctx, p.Installation.WorkspaceID, p.UserID) {
		return commandReply("Only workspace owners and admins can bind an Issue topic."), nil
	}
	rootID := topicRootMessageID(p.Message)
	if rootID == "" {
		return commandReply("Run /issue bind inside a Feishu topic."), nil
	}
	if len(p.Command.Arguments) == 0 {
		return commandReply("Usage: /issue bind MUL-123"), nil
	}
	issue, reply, err := s.resolveIssueForCommand(ctx, p.Installation.WorkspaceID, p.Command.Arguments[0])
	if err != nil || reply != "" {
		return commandReply(reply), err
	}

	// A manual /issue bind is an installation-scoped direct route. Keep it
	// independent from any Project binding in the group so Project rebinds or
	// unbinds cannot silently tear down the explicitly selected Issue topic.
	if err := s.bindIssueTopic(
		ctx, p.Installation.WorkspaceID, p.Installation.ID, pgtype.UUID{}, issue.ProjectID,
		p.Message.Source.ChatID, issue, rootID, p.Message.Source.ThreadID, p.UserID,
	); err != nil {
		return commandReply(projectSyncErrorText(err)), nil
	}
	return commandReply(fmt.Sprintf("Bound this topic to %s — %s.", s.issueIdentifier(ctx, issue), issue.Title)), nil
}

func (s *ProjectSyncService) commandIssueUnbind(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	if p.Message.Source.ChatType != channel.ChatTypeGroup {
		return commandReply("/issue unbind must be run inside the bound Feishu topic."), nil
	}
	if !s.canManageWorkspace(ctx, p.Installation.WorkspaceID, p.UserID) {
		return commandReply("Only workspace owners and admins can unbind an Issue topic."), nil
	}
	rootID := topicRootMessageID(p.Message)
	if rootID == "" {
		return commandReply("Run /issue unbind inside a Feishu topic."), nil
	}
	topic, err := s.store.getActiveIssueTopicByRoot(
		ctx, s.store.pool, p.Installation.WorkspaceID, p.Installation.ID,
		p.Message.Source.ChatID, rootID,
	)
	if isNoRows(err) {
		return commandReply("This topic is not bound to an Issue."), nil
	}
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	_, err = s.store.manualUnbindIssueTopic(ctx, s.store.pool, p.Installation.WorkspaceID, topic.ID, p.UserID)
	if isNoRows(err) {
		return commandReply("This topic is not bound to an Issue."), nil
	}
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	return commandReply("Issue topic synchronization was disabled. It will not be recreated automatically."), nil
}

func (s *ProjectSyncService) commandIssueStatus(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	issue, status, reply, err := s.issueAndTrailingArgument(ctx, p, "status")
	if err != nil || reply != "" {
		return commandReply(reply), err
	}
	switch status {
	case "backlog", "todo", "in_progress", "done", "cancelled":
	default:
		return commandReply("Invalid Issue status. Use backlog, todo, in_progress, done, or cancelled."), nil
	}
	previous := issue.Status
	updated, err := s.queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: issue.ID, Status: status, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	if s.issues.Bus != nil && previous != updated.Status {
		s.issues.Bus.Publish(events.Event{
			Type: protocol.EventIssueUpdated, WorkspaceID: util.UUIDToString(updated.WorkspaceID),
			ActorType: "member", ActorID: util.UUIDToString(p.UserID),
			Payload: map[string]any{
				"issue_id":       util.UUIDToString(updated.ID),
				"status":         updated.Status,
				"prev_status":    previous,
				"status_changed": true,
			},
		})
	}
	return commandReply(fmt.Sprintf("%s status: %s → %s", s.issueIdentifier(ctx, updated), previous, updated.Status)), nil
}

func (s *ProjectSyncService) commandIssueStop(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	issue, reply, err := s.issueFromOptionalArgumentOrTopic(ctx, p)
	if err != nil || reply != "" {
		return commandReply(reply), err
	}
	tasks, err := s.queries.ListActiveTasksByIssue(ctx, issue.ID)
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	if len(tasks) == 0 {
		return commandReply(fmt.Sprintf("%s has no active Task to stop.", s.issueIdentifier(ctx, issue))), nil
	}
	task, err := s.tasks.CancelTask(ctx, tasks[0].ID)
	if err != nil {
		return engine.ProjectCommandResult{}, err
	}
	return commandReply(fmt.Sprintf(
		"Stopped Task %s for %s.\nIssue status remains %s.",
		util.UUIDToString(task.ID), s.issueIdentifier(ctx, issue), issue.Status,
	)), nil
}

func (s *ProjectSyncService) commandIssueShow(ctx context.Context, p engine.ProjectCommandContext) (engine.ProjectCommandResult, error) {
	issue, reply, err := s.issueFromOptionalArgumentOrTopic(ctx, p)
	if err != nil || reply != "" {
		return commandReply(reply), err
	}
	text := fmt.Sprintf("%s — %s\nStatus: %s\nPriority: %s",
		s.issueIdentifier(ctx, issue), issue.Title, issue.Status, issue.Priority)
	if s.appURL != "" {
		text += "\n" + s.appURL + "/issues/" + util.UUIDToString(issue.ID)
	}
	return commandReply(text), nil
}

func (s *ProjectSyncService) bindProjectInGroup(ctx context.Context, p engine.ProjectCommandContext, project db.Project) (ChannelProjectBinding, error) {
	if p.Installation.WorkspaceID != project.WorkspaceID {
		return ChannelProjectBinding{}, ErrProjectSyncInvalidInput
	}
	if !s.canManageWorkspace(ctx, project.WorkspaceID, p.UserID) {
		return ChannelProjectBinding{}, ErrProjectSyncForbidden
	}
	inst, ok := p.Installation.Platform.(Installation)
	if !ok || InstallationStatus(inst.Status) != InstallationActive {
		return ChannelProjectBinding{}, ErrProjectSyncInvalidInput
	}
	chatName, err := s.chatName(ctx, inst, p.Message.Source.ChatID)
	if err != nil {
		return ChannelProjectBinding{}, err
	}
	tx, err := s.store.begin(ctx)
	if err != nil {
		return ChannelProjectBinding{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := s.store.getCurrentProjectBinding(ctx, tx, project.WorkspaceID, project.ID); err == nil {
		return ChannelProjectBinding{}, ErrProjectSyncConflict
	} else if !isNoRows(err) {
		return ChannelProjectBinding{}, err
	}
	if _, err := s.store.getActiveProjectBindingByGroup(ctx, tx, p.Installation.ID, p.Message.Source.ChatID); err == nil {
		return ChannelProjectBinding{}, ErrProjectSyncConflict
	} else if !isNoRows(err) {
		return ChannelProjectBinding{}, err
	}
	binding, err := s.store.createActiveProjectBinding(
		ctx, tx, project.WorkspaceID, project.ID, p.Installation.ID,
		p.Message.Source.ChatID, chatName, p.UserID,
	)
	if err != nil {
		return ChannelProjectBinding{}, translateProjectSyncConstraint(err)
	}
	if err := s.store.enqueueProjectBackfill(ctx, tx, binding); err != nil {
		return ChannelProjectBinding{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelProjectBinding{}, err
	}
	return binding, nil
}

func (s *ProjectSyncService) confirmCodeInGroup(ctx context.Context, p engine.ProjectCommandContext, code string) (ChannelProjectBinding, bool, error) {
	hash := projectBindingCodeHash(code)
	tx, err := s.store.begin(ctx)
	if err != nil {
		return ChannelProjectBinding{}, false, err
	}
	defer tx.Rollback(ctx)
	pending, err := s.store.getPendingProjectBindingByToken(ctx, tx, p.Installation.ID, hash)
	if isNoRows(err) {
		return ChannelProjectBinding{}, false, nil
	}
	if err != nil {
		return ChannelProjectBinding{}, false, err
	}
	if !s.canManageWorkspace(ctx, pending.WorkspaceID, p.UserID) {
		return ChannelProjectBinding{}, true, ErrProjectSyncForbidden
	}
	inst, ok := p.Installation.Platform.(Installation)
	if !ok {
		return ChannelProjectBinding{}, true, ErrProjectSyncInvalidInput
	}
	chatName, err := s.chatName(ctx, inst, p.Message.Source.ChatID)
	if err != nil {
		return ChannelProjectBinding{}, true, err
	}
	if _, err := s.store.getActiveProjectBindingByGroup(ctx, tx, p.Installation.ID, p.Message.Source.ChatID); err == nil {
		return ChannelProjectBinding{}, true, ErrProjectSyncConflict
	} else if !isNoRows(err) {
		return ChannelProjectBinding{}, true, err
	}
	binding, err := s.store.confirmProjectBinding(ctx, tx, pending, p.Message.Source.ChatID, chatName, p.UserID)
	if err != nil {
		return ChannelProjectBinding{}, true, translateProjectSyncConstraint(err)
	}
	if err := s.store.enqueueProjectBackfill(ctx, tx, binding); err != nil {
		return ChannelProjectBinding{}, true, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelProjectBinding{}, true, err
	}
	return binding, true, nil
}

func (s *ProjectSyncService) BeginProjectBinding(ctx context.Context, workspaceID, projectID, installationID, userID pgtype.UUID) (ChannelProjectBinding, string, error) {
	if !s.canManageWorkspace(ctx, workspaceID, userID) {
		return ChannelProjectBinding{}, "", ErrProjectSyncForbidden
	}
	project, err := s.queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		return ChannelProjectBinding{}, "", ErrProjectSyncNotFound
	}
	_ = project
	inst, err := NewChannelStore(s.queries).GetLarkInstallationInWorkspace(ctx, GetInstallationInWorkspaceParams{
		ID: installationID, WorkspaceID: workspaceID,
	})
	if err != nil || InstallationStatus(inst.Status) != InstallationActive {
		return ChannelProjectBinding{}, "", ErrProjectSyncInvalidInput
	}
	code, err := newProjectBindingCode()
	if err != nil {
		return ChannelProjectBinding{}, "", err
	}
	tx, err := s.store.begin(ctx)
	if err != nil {
		return ChannelProjectBinding{}, "", err
	}
	defer tx.Rollback(ctx)
	current, err := s.store.getCurrentProjectBinding(ctx, tx, workspaceID, projectID)
	if err == nil {
		if current.InstallationID == installationID {
			return current, "", nil
		}
		replaced, unbindErr := s.store.unbindProject(ctx, tx, workspaceID, current.ID, userID)
		if unbindErr != nil {
			return ChannelProjectBinding{}, "", unbindErr
		}
		if err := s.store.finishProjectUnbind(ctx, tx, replaced, "project_binding_replaced"); err != nil {
			return ChannelProjectBinding{}, "", err
		}
	} else if !isNoRows(err) {
		return ChannelProjectBinding{}, "", err
	}
	binding, err := s.store.createPendingProjectBinding(
		ctx, tx, workspaceID, projectID, installationID,
		projectBindingCodeHash(code), time.Now().Add(projectBindingCodeTTL), userID,
	)
	if err != nil {
		return ChannelProjectBinding{}, "", translateProjectSyncConstraint(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelProjectBinding{}, "", err
	}
	return binding, code, nil
}

func (s *ProjectSyncService) UnbindProject(ctx context.Context, workspaceID, bindingID, userID pgtype.UUID) error {
	if !s.canManageWorkspace(ctx, workspaceID, userID) {
		return ErrProjectSyncForbidden
	}
	tx, err := s.store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	binding, err := s.store.unbindProject(ctx, tx, workspaceID, bindingID, userID)
	if isNoRows(err) {
		return ErrProjectSyncNotFound
	}
	if err != nil {
		return err
	}
	if err := s.store.finishProjectUnbind(ctx, tx, binding, "project_unbound"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ProjectSyncService) RetryProjectTopics(ctx context.Context, workspaceID, projectID, userID pgtype.UUID) (int64, error) {
	if !s.canManageWorkspace(ctx, workspaceID, userID) {
		return 0, ErrProjectSyncForbidden
	}
	binding, err := s.store.getCurrentProjectBinding(ctx, s.store.pool, workspaceID, projectID)
	if err != nil || binding.State != "active" {
		return 0, ErrProjectSyncNotFound
	}
	tx, err := s.store.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err := s.store.enqueueProjectBackfill(ctx, tx, binding); err != nil {
		return 0, err
	}
	retried, err := s.store.retryDeadNotifications(ctx, tx, workspaceID, projectID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return retried, nil
}

func (s *ProjectSyncService) ProjectSummary(ctx context.Context, workspaceID, projectID pgtype.UUID) (ChannelProjectSyncSummary, error) {
	summary, err := s.store.getProjectSyncSummary(ctx, workspaceID, projectID)
	if isNoRows(err) {
		return ChannelProjectSyncSummary{}, ErrProjectSyncNotFound
	}
	return summary, err
}

func (s *ProjectSyncService) ProjectSummaries(ctx context.Context, workspaceID pgtype.UUID) ([]ChannelProjectSyncSummary, error) {
	return s.store.listProjectSyncSummaries(ctx, workspaceID)
}

// IssueCreateTopicHookForAgentTask resolves the Feishu topic that triggered an
// agent task and returns an IssueService transaction hook for binding any Issue
// created by that task back to the same topic. Tasks originating outside a
// Feishu group topic intentionally return a nil hook.
func (s *ProjectSyncService) IssueCreateTopicHookForAgentTask(
	ctx context.Context,
	workspaceID, taskID pgtype.UUID,
) (func(context.Context, pgx.Tx, db.Issue) error, error) {
	task, err := s.queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{
		ID: taskID, WorkspaceID: workspaceID,
	})
	if isNoRows(err) || (err == nil && !task.ChatSessionID.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	binding, err := s.queries.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: task.ChatSessionID,
		ChannelType:   "feishu",
	})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if binding.ChatType != string(channel.ChatTypeGroup) ||
		!binding.LastThreadID.Valid || strings.TrimSpace(binding.LastThreadID.String) == "" {
		return nil, nil
	}

	installation, err := s.queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID: binding.InstallationID, WorkspaceID: workspaceID, ChannelType: "feishu",
	})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if InstallationStatus(installation.Status) != InstallationActive || installation.AgentID != task.AgentID {
		return nil, nil
	}

	var config larkBindingConfig
	if len(binding.Config) > 0 {
		if err := json.Unmarshal(binding.Config, &config); err != nil {
			return nil, fmt.Errorf("decode Feishu chat binding config: %w", err)
		}
	}
	threadID := strings.TrimSpace(binding.LastThreadID.String)
	chatID := strings.TrimSpace(config.ChatID)
	if chatID == "" {
		chatID = strings.TrimSuffix(binding.ChannelChatID, ":"+threadID)
	}
	rootID := strings.TrimSpace(config.TopicRootMessageID)
	if rootID == "" && binding.LastMessageID.Valid {
		// Compatibility for topic sessions created before the root id was
		// persisted in config. The trigger message is the best available root.
		rootID = strings.TrimSpace(binding.LastMessageID.String)
	}
	if chatID == "" || rootID == "" {
		return nil, nil
	}

	createdBy := task.InitiatorUserID
	if !createdBy.Valid {
		createdBy = task.OriginatorUserID
	}
	installationID := binding.InstallationID
	return func(hookCtx context.Context, tx pgx.Tx, issue db.Issue) error {
		// Resolve the active project binding so the topic binding
		// is linked to its parent project binding. If no active
		// binding exists (e.g. project was already unbound), fall
		// back to NULL — finishProjectUnbind will clean it up later.
		projectBindingID := pgtype.UUID{}
		if pb, pbErr := s.store.getActiveProjectBindingByGroup(
			hookCtx, tx, installationID, chatID,
		); pbErr == nil {
			projectBindingID = pb.ID
		}
		if _, err := s.store.createIssueTopic(
			hookCtx, tx, issue.WorkspaceID, installationID, projectBindingID, issue.ProjectID,
			issue.ID, chatID, rootID, threadID, "issue_created_in_topic", createdBy,
		); err != nil {
			return translateProjectSyncConstraint(err)
		}
		// The Issue INSERT trigger runs before this hook and may have queued a
		// Project-level issue_created notification. The direct topic route is
		// authoritative, so remove that now-stale notification in the same
		// transaction instead of letting the worker later reject it as a route
		// conflict.
		_, err := tx.Exec(hookCtx, `
			DELETE FROM channel_notification_outbox
			WHERE workspace_id = $1 AND issue_id = $2
			  AND event_type = 'issue_created'
			  AND issue_topic_binding_id IS NULL`,
			issue.WorkspaceID, issue.ID)
		return err
	}, nil
}

func (s *ProjectSyncService) ListInstallationProjects(ctx context.Context, workspaceID, installationID pgtype.UUID) ([]ChannelProjectBindingListItem, error) {
	return s.store.listProjectBindingsByInstallation(ctx, s.store.pool, workspaceID, installationID)
}

// RevokeInstallationBindings stops every Project route owned by a shared Bot
// without deleting the installation or any historical Feishu message.
func (s *ProjectSyncService) RevokeInstallationBindings(ctx context.Context, workspaceID, installationID pgtype.UUID) error {
	tx, err := s.store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	items, err := s.store.listProjectBindingsByInstallation(ctx, tx, workspaceID, installationID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
			UPDATE channel_project_binding
			SET state = 'bot_revoked', unbound_at = now(), updated_at = now(),
			    bind_token_hash = NULL, bind_token_expires_at = NULL
			WHERE id = $1 AND workspace_id = $2
			  AND state IN ('pending_group', 'active')`,
			item.Binding.ID, workspaceID); err != nil {
			return err
		}
		if err := s.store.finishProjectUnbind(ctx, tx, item.Binding, "bot_revoked"); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		WITH revoked_topics AS (
			UPDATE channel_issue_topic_binding
			SET state = 'bot_revoked', unbound_at = now(), updated_at = now()
			WHERE workspace_id = $1 AND installation_id = $2 AND state = 'active'
			RETURNING id
		)
		UPDATE channel_notification_outbox
		SET status = 'dead', last_error = 'bot_revoked',
		    locked_at = NULL, locked_by = NULL
		WHERE workspace_id = $1
		  AND issue_topic_binding_id IN (SELECT id FROM revoked_topics)
		  AND status IN ('pending', 'sending')`,
		workspaceID, installationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *ProjectSyncService) IssueTopic(ctx context.Context, workspaceID, issueID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	binding, err := s.store.getLatestIssueTopicByIssue(ctx, s.store.pool, workspaceID, issueID)
	if isNoRows(err) {
		return ChannelIssueTopicBinding{}, ErrProjectSyncNotFound
	}
	return binding, err
}

func (s *ProjectSyncService) UnbindIssueTopic(ctx context.Context, workspaceID, issueID, userID pgtype.UUID) error {
	if !s.canManageWorkspace(ctx, workspaceID, userID) {
		return ErrProjectSyncForbidden
	}
	active, err := s.store.getActiveIssueTopicByIssue(ctx, s.store.pool, workspaceID, issueID)
	if isNoRows(err) {
		return ErrProjectSyncNotFound
	}
	if err != nil {
		return err
	}
	_, err = s.store.manualUnbindIssueTopic(
		ctx, s.store.pool, workspaceID, active.ID, userID,
	)
	return err
}

func (s *ProjectSyncService) EnableIssueTopic(ctx context.Context, workspaceID, issueID, userID pgtype.UUID) (ChannelIssueTopicBinding, error) {
	if !s.canManageWorkspace(ctx, workspaceID, userID) {
		return ChannelIssueTopicBinding{}, ErrProjectSyncForbidden
	}
	latest, err := s.store.getLatestIssueTopicByIssue(ctx, s.store.pool, workspaceID, issueID)
	if err != nil {
		return ChannelIssueTopicBinding{}, ErrProjectSyncNotFound
	}
	if latest.State != "manual_unbound" {
		return ChannelIssueTopicBinding{}, ErrProjectSyncConflict
	}
	tx, err := s.store.begin(ctx)
	if err != nil {
		return ChannelIssueTopicBinding{}, err
	}
	defer tx.Rollback(ctx)
	binding, err := s.store.createIssueTopic(
		ctx, tx, latest.WorkspaceID, latest.InstallationID, latest.ProjectBindingID, latest.ProjectID,
		latest.IssueID, latest.ChannelChatID, latest.TopicRootMessageID,
		latest.ChannelThreadID.String, "manual_topic_bind", userID,
	)
	if err != nil {
		return ChannelIssueTopicBinding{}, translateProjectSyncConstraint(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ChannelIssueTopicBinding{}, err
	}
	return binding, nil
}

func (s *ProjectSyncService) bindIssueTopic(
	ctx context.Context,
	workspaceID, installationID, projectBindingID, projectID pgtype.UUID,
	chatID string,
	issue db.Issue,
	rootID, threadID string,
	userID pgtype.UUID,
) error {
	tx, err := s.store.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if existing, err := s.store.getActiveIssueTopicByRoot(
		ctx, tx, workspaceID, installationID, chatID, rootID,
	); err == nil {
		if existing.IssueID == issue.ID {
			return tx.Commit(ctx)
		}
		return ErrProjectSyncConflict
	} else if !isNoRows(err) {
		return err
	}
	if existing, err := s.store.getActiveIssueTopicByIssue(ctx, tx, workspaceID, issue.ID); err == nil {
		if existing.InstallationID == installationID &&
			existing.ChannelChatID == chatID &&
			existing.TopicRootMessageID == rootID {
			return tx.Commit(ctx)
		}
		if err := s.store.replaceActiveIssueTopic(ctx, tx, workspaceID, issue.ID, userID); err != nil {
			return err
		}
	} else if !isNoRows(err) {
		return err
	}
	if _, err := s.store.createIssueTopic(
		ctx, tx, workspaceID, installationID, projectBindingID, projectID, issue.ID,
		chatID, rootID, threadID, "manual_topic_bind", userID,
	); err != nil {
		return translateProjectSyncConstraint(err)
	}
	return tx.Commit(ctx)
}

func (s *ProjectSyncService) issueAndTrailingArgument(ctx context.Context, p engine.ProjectCommandContext, argumentName string) (db.Issue, string, string, error) {
	args := p.Command.Arguments
	rootID := topicRootMessageID(p.Message)
	if len(args) >= 2 {
		issue, reply, err := s.resolveIssueForCommand(ctx, p.Installation.WorkspaceID, args[0])
		return issue, args[1], reply, err
	}
	if len(args) == 1 && rootID != "" {
		issue, reply, err := s.issueFromTopic(ctx, p)
		return issue, args[0], reply, err
	}
	return db.Issue{}, "", fmt.Sprintf("Usage: /issue %s [MUL-123] <%s>", argumentName, argumentName), nil
}

func (s *ProjectSyncService) issueFromOptionalArgumentOrTopic(ctx context.Context, p engine.ProjectCommandContext) (db.Issue, string, error) {
	if len(p.Command.Arguments) > 0 {
		return s.resolveIssueForCommand(ctx, p.Installation.WorkspaceID, p.Command.Arguments[0])
	}
	return s.issueFromTopic(ctx, p)
}

func (s *ProjectSyncService) issueFromTopic(ctx context.Context, p engine.ProjectCommandContext) (db.Issue, string, error) {
	if p.Message.Source.ChatType != channel.ChatTypeGroup {
		return db.Issue{}, "Specify an Issue identifier in private chat.", nil
	}
	rootID := topicRootMessageID(p.Message)
	if rootID == "" {
		return db.Issue{}, "Run this command inside a bound Feishu topic or specify an Issue identifier.", nil
	}
	topic, err := s.store.getActiveIssueTopicByRoot(
		ctx, s.store.pool, p.Installation.WorkspaceID, p.Installation.ID,
		p.Message.Source.ChatID, rootID,
	)
	if isNoRows(err) {
		return db.Issue{}, "This topic is not bound to an Issue.", nil
	}
	if err != nil {
		return db.Issue{}, "", err
	}
	issue, err := s.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: topic.IssueID, WorkspaceID: topic.WorkspaceID,
	})
	if err != nil {
		return db.Issue{}, "", err
	}
	return issue, "", nil
}

func (s *ProjectSyncService) resolveProjectForCommand(ctx context.Context, workspaceID pgtype.UUID, identifier string) (db.Project, string, error) {
	projects, err := s.queries.ListProjects(ctx, db.ListProjectsParams{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.Project{}, "", err
	}
	identifier = strings.TrimSpace(identifier)
	var matches []db.Project
	for _, project := range projects {
		if util.UUIDToString(project.ID) == identifier || strings.EqualFold(project.Title, identifier) {
			matches = append(matches, project)
		}
	}
	switch len(matches) {
	case 0:
		return db.Project{}, "Project not found. Use its UUID or exact title.", nil
	case 1:
		return matches[0], "", nil
	default:
		return db.Project{}, "More than one Project has that title. Use the Project UUID.", nil
	}
}

func (s *ProjectSyncService) resolveIssueForCommand(ctx context.Context, workspaceID pgtype.UUID, identifier string) (db.Issue, string, error) {
	identifier = strings.TrimSpace(identifier)
	if id, err := util.ParseUUID(identifier); err == nil {
		issue, err := s.queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
			ID: id, WorkspaceID: workspaceID,
		})
		if isNoRows(err) {
			return db.Issue{}, "Issue not found.", nil
		}
		return issue, "", err
	}
	dash := strings.LastIndex(identifier, "-")
	if dash <= 0 || dash == len(identifier)-1 {
		return db.Issue{}, "Invalid Issue identifier. Use a value such as MUL-123.", nil
	}
	workspace, err := s.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return db.Issue{}, "", err
	}
	if !strings.EqualFold(identifier[:dash], workspace.IssuePrefix) {
		return db.Issue{}, "That Issue identifier belongs to a different workspace.", nil
	}
	n, err := strconv.ParseInt(identifier[dash+1:], 10, 32)
	if err != nil || n <= 0 {
		return db.Issue{}, "Invalid Issue identifier. Use a value such as MUL-123.", nil
	}
	issue, err := s.queries.GetIssueByNumber(ctx, db.GetIssueByNumberParams{
		WorkspaceID: workspaceID, Number: int32(n),
	})
	if isNoRows(err) {
		return db.Issue{}, "Issue not found.", nil
	}
	return issue, "", err
}

func (s *ProjectSyncService) availableProjectsText(ctx context.Context, workspaceID pgtype.UUID) (string, error) {
	projects, err := s.queries.ListProjects(ctx, db.ListProjectsParams{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return "", err
	}
	if len(projects) == 0 {
		return "No Projects are available in this workspace.", nil
	}
	var b strings.Builder
	b.WriteString("Projects:\n")
	for _, project := range projects {
		if _, err := s.store.getCurrentProjectBinding(ctx, s.store.pool, workspaceID, project.ID); isNoRows(err) {
			fmt.Fprintf(&b, "• %s — %s\n", project.Title, util.UUIDToString(project.ID))
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "Projects:" {
		return "Every Project already has an active or pending group binding.", nil
	}
	return text, nil
}

func (s *ProjectSyncService) canManageWorkspace(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	member, err := s.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: userID, WorkspaceID: workspaceID,
	})
	return err == nil && (member.Role == "owner" || member.Role == "admin")
}

func (s *ProjectSyncService) chatName(ctx context.Context, inst Installation, chatID string) (string, error) {
	getter, ok := s.client.(ChatInfoClient)
	if !ok {
		return chatID, nil
	}
	if s.credentials == nil {
		return "", errors.New("lark project sync: credentials resolver missing")
	}
	creds, err := installationCredentialsFor(inst, s.credentials)
	if err != nil {
		return "", err
	}
	info, err := getter.GetChatInfo(ctx, creds, ChatID(chatID))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(info.Name) == "" {
		return chatID, nil
	}
	return info.Name, nil
}

func (s *ProjectSyncService) issueIdentifier(ctx context.Context, issue db.Issue) string {
	workspace, err := s.queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil || workspace.IssuePrefix == "" {
		return fmt.Sprintf("#%d", issue.Number)
	}
	return fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)
}

func topicRootMessageID(msg channel.InboundMessage) string {
	if msg.ReplyTo != nil {
		if msg.ReplyTo.RootID != "" {
			return msg.ReplyTo.RootID
		}
		if msg.Source.ThreadID != "" && msg.ReplyTo.MessageID != "" {
			return msg.ReplyTo.MessageID
		}
	}
	if msg.Source.ThreadID != "" {
		return msg.MessageID
	}
	return ""
}

func splitPrivateCreateArguments(raw string) (string, string, bool) {
	parts := strings.SplitN(raw, "--", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	project := strings.TrimSpace(parts[0])
	title := strings.TrimSpace(parts[1])
	return project, title, project != "" && title != ""
}

func newProjectBindingCode() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "=")[:10], nil
}

func projectBindingCodeHash(code string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}

func commandReply(text string) engine.ProjectCommandResult {
	return engine.ProjectCommandResult{ReplyText: text}
}

func projectCommandHelp() string {
	return strings.Join([]string{
		"Project commands:",
		"/project bind [project or confirmation code]",
		"/project status",
		"/project list",
		"/project unbind [project]",
	}, "\n")
}

func issueCommandHelp() string {
	return strings.Join([]string{
		"Issue commands:",
		"/issue create <title>",
		"/issue bind MUL-123",
		"/issue unbind",
		"/issue show [MUL-123]",
		"/issue status [MUL-123] <status>",
		"/issue stop [MUL-123]",
	}, "\n")
}

func projectSyncErrorText(err error) string {
	switch {
	case errors.Is(err, ErrProjectSyncForbidden):
		return "You do not have permission to manage this synchronization."
	case errors.Is(err, ErrProjectSyncConflict):
		return "This Project, Bot, group, Issue, or topic already has a conflicting active binding."
	case errors.Is(err, ErrProjectSyncNotFound):
		return "Synchronization binding not found."
	case errors.Is(err, ErrProjectSyncInvalidInput):
		return "The synchronization request is invalid or the Bot installation is inactive."
	default:
		return "Synchronization failed. Please retry or check the server logs."
	}
}

func translateProjectSyncConstraint(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrProjectSyncConflict
	}
	return err
}
