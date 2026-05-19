package inbound

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	replyIssueCreated       = "ISSUE_CREATED"
	replyCommentAdded       = "COMMENT_ADDED"
	replyStatusChanged      = "STATUS_CHANGED"
	replyAssigneeChanged    = "ASSIGNEE_CHANGED"
	replyPriorityChanged    = "PRIORITY_CHANGED"
	replyLabelAdded         = "LABEL_ADDED"
	replyLabelRemoved       = "LABEL_REMOVED"
	replyActionProposed     = "ACTION_PROPOSED"
	replyActionConfirmed    = "ACTION_CONFIRMED"
	replyActionCancelled    = "ACTION_CANCELLED"
	replyActionExpired      = "ACTION_EXPIRED"
	replyUnsupportedOp      = "UNSUPPORTED_OP"
	replyUnknown            = "UNKNOWN"
	replyAskClarify         = "ASK_CLARIFY"
	replyIgnoredSuffix      = "IGNORED_SUFFIX"
	replyMissingParam       = "MISSING_PARAM"
	replyIssueNotFound      = "ISSUE_NOT_FOUND"
	replyInternalError      = "INTERNAL_ERROR"
	replyPrivateUnsupported = "PRIVATE_UNSUPPORTED"
	replyMessageRecalled    = "MESSAGE_RECALLED"

	contextMessageIDIntentParam = "_channel_context_message_id"
)

type ChatBindingLookup interface {
	LookupWorkspaceID(ctx context.Context, channelName, chatID string) (pgtype.UUID, error)
}

type UserInfoResolver interface {
	Resolve(ctx context.Context, channelName, externalUserID string) (ResolvedUser, error)
}

type ProjectWorkspaceValidator interface {
	ValidateProjectInWorkspace(ctx context.Context, workspaceID, projectID pgtype.UUID) error
}

type ResolvedUser struct {
	MulticaUserID pgtype.UUID
	DisplayName   string
}

type DispatchConfig struct {
	IssueFacade       facade.IssueFacade
	IssueDigestFacade facade.IssueDigestFacade
	CommentFacade     facade.CommentFacade
	ReplySink         ChannelReplySink
	ChatBinding       ChatBindingLookup
	UserResolver      UserInfoResolver
	ProjectValidator  ProjectWorkspaceValidator
	DispatchStore     DispatchCompletionStore
	ProposalStore     ActionProposalStore
}

type dispatchStep struct {
	cfg DispatchConfig
}

func NewDispatchStep(cfg DispatchConfig) Step {
	return &dispatchStep{cfg: cfg}
}

func (dispatchStep) Name() string { return "dispatch" }

func (d *dispatchStep) Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error) {
	if d.cfg.DispatchStore != nil && evt.RuntimeEventID != "" {
		reply, ok, err := d.cfg.DispatchStore.GetDispatchCompletion(ctx, evt.RuntimeEventID)
		if err != nil {
			return evt, DecisionContinue, fmt.Errorf("load dispatch completion: %w", err)
		}
		if ok {
			if err := d.sendReply(ctx, evt, reply); err != nil {
				return evt, DecisionContinue, fmt.Errorf("send completed dispatch reply: %w", err)
			}
			return evt, DecisionContinue, nil
		}
	}

	// PRD E6: recall events are annotated in the chat thread but never
	// mutate any Issue or Comment. They bypass intent recognition entirely.
	if evt.Type == port.EventTypeMessageRecalled {
		reply := fmt.Sprintf("[%s] 上游消息已撤回 (message_id: %s)", replyMessageRecalled, evt.MessageID)
		if err := d.persistDispatchCompletion(ctx, evt, reply); err != nil {
			return evt, DecisionContinue, err
		}
		if sendErr := d.sendReply(ctx, evt, reply); sendErr != nil {
			return evt, DecisionContinue, fmt.Errorf("send recall annotation: %w", sendErr)
		}
		return evt, DecisionContinue, nil
	}

	intent := evt.Intent

	slog.Debug("dispatch: routing intent",
		"intent", string(intent.Kind),
		"confidence", intent.Confidence,
		"source", string(intent.Source),
		"channel", evt.ChannelName,
		"chat_id", evt.ChatID,
	)

	var reply string
	var rich *port.OutboundRichMessage
	var err error

	switch intent.Kind {
	case port.IntentCreateIssue, port.IntentAddComment, port.IntentSetStatus, port.IntentSetAssignee, port.IntentSetPriority, port.IntentSetLabel:
		reply, err = d.handleMutationIntent(ctx, evt)
	case port.IntentQueryProgress:
		reply, rich, err = d.handleQueryProgress(ctx, evt)
	case port.IntentQueryIssue:
		reply, rich, err = d.handleQueryIssue(ctx, evt)
	case port.IntentIssueDetail:
		reply, rich, err = d.handleIssueDetail(ctx, evt)
	case port.IntentIssueTimeline:
		reply, rich, err = d.handleIssueTimeline(ctx, evt)
	case port.IntentIssueLogs:
		reply, rich, err = d.handleIssueLogs(ctx, evt)
	case port.IntentConfirmAction:
		reply, err = d.handleConfirmAction(ctx, evt)
	case port.IntentCancelAction:
		reply, err = d.handleCancelAction(ctx, evt)
	case port.IntentUnsupported:
		reply = fmt.Sprintf("[%s] 此操作不支持在群内执行，请回 Web 端操作。", replyUnsupportedOp)
	case port.IntentUnknown:
		reply, err = d.handleUnknown(ctx, evt)
	case port.IntentASKClarify:
		reply, err = d.handleAskClarify(ctx, evt)
	default:
		reply = "我可以查进展、创建 Issue，或把你的回复写回某个 Issue。你可以直接说要查哪个 issue，或者要创建什么。"
	}

	if err != nil {
		slog.Error("dispatch: handler error", "intent", string(intent.Kind), "error", err)
		return evt, DecisionContinue, err
	}

	if _, hasSuffix := intent.Params["_ignored_suffix"]; hasSuffix {
		reply += fmt.Sprintf("\n[%s] 消息中包含多个意图，已忽略附加部分。", replyIgnoredSuffix)
	}

	// This checkpoint replays already persisted replies. Query replies are
	// intentionally at-least-current if a worker crashes before this write.
	if err := d.persistDispatchCompletion(ctx, evt, reply); err != nil {
		return evt, DecisionContinue, err
	}

	if rich != nil {
		if sendErr := d.sendRichReply(ctx, evt, *rich); sendErr != nil {
			return evt, DecisionContinue, fmt.Errorf("send dispatch reply: %w", sendErr)
		}
		return evt, DecisionContinue, nil
	}
	if sendErr := d.sendReply(ctx, evt, reply); sendErr != nil {
		return evt, DecisionContinue, fmt.Errorf("send dispatch reply: %w", sendErr)
	}
	return evt, DecisionContinue, nil
}

func (d *dispatchStep) persistDispatchCompletion(ctx context.Context, evt port.InboundEvent, reply string) error {
	if d.cfg.DispatchStore == nil || evt.RuntimeEventID == "" {
		return nil
	}
	if err := d.cfg.DispatchStore.MarkDispatchCompleted(ctx, evt.RuntimeEventID, reply); err != nil {
		return fmt.Errorf("mark dispatch completed: %w", err)
	}
	return nil
}

func (d *dispatchStep) lookupWorkspaceID(ctx context.Context, evt port.InboundEvent) (pgtype.UUID, error) {
	return d.cfg.ChatBinding.LookupWorkspaceID(ctx, evt.ConnectionID(), evt.ChatID)
}

func (d *dispatchStep) handleCreateIssue(ctx context.Context, evt port.InboundEvent) (string, error) {
	title, _ := evt.Intent.Params["title"]
	if title == "" {
		return fmt.Sprintf("[%s] 缺少 Issue 标题，请提供要创建的内容。", replyMissingParam), nil
	}

	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return "", fmt.Errorf("lookup workspace: %w", err)
	}

	user, err := d.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID)
	if err != nil {
		return "", fmt.Errorf("resolve user: %w", err)
	}

	var projectID pgtype.UUID
	if rawProjectID := evt.Intent.Params["project_id"]; rawProjectID != "" {
		parsed, err := util.ParseUUID(rawProjectID)
		if err != nil {
			return fmt.Sprintf("[%s] project_id 格式不正确。", replyMissingParam), nil
		}
		projectID = parsed
		if d.cfg.ProjectValidator == nil {
			return "", errors.New("validate project: project validator is not configured")
		}
		if err := d.cfg.ProjectValidator.ValidateProjectInWorkspace(ctx, wsID, projectID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Sprintf("[%s] project_id 不属于当前 workspace。", replyMissingParam), nil
			}
			return "", fmt.Errorf("validate project: %w", err)
		}
	}

	issue, err := d.cfg.IssueFacade.CreateIssue(ctx, facade.CreateIssueReq{
		WorkspaceID:        wsID,
		ActorID:            user.MulticaUserID,
		ProjectID:          projectID,
		InboundEventID:     parseRuntimeEventID(evt.RuntimeEventID),
		Title:              title,
		Description:        evt.Intent.Params["description"],
		AssigneeIdentifier: strings.TrimSpace(evt.Intent.Params["assignee"]),
	})
	if err != nil {
		return "", fmt.Errorf("create issue: %w", err)
	}

	return fmt.Sprintf("[%s] 已创建 Issue %s：%s", replyIssueCreated, issue.Identifier, issue.Title), nil
}

func (d *dispatchStep) handleQueryProgress(ctx context.Context, evt port.InboundEvent) (string, *port.OutboundRichMessage, error) {
	scope := strings.TrimSpace(evt.Intent.Params["scope"])
	if scope == "" && strings.TrimSpace(evt.Intent.Params["issue_key"]) != "" {
		scope = "issue"
	}
	if scope == "" {
		scope = "projects"
	}

	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return "", nil, fmt.Errorf("lookup workspace: %w", err)
	}
	if d.cfg.IssueDigestFacade == nil {
		return fmt.Sprintf("[%s] 进展查询服务未配置。", replyInternalError), nil, nil
	}

	switch scope {
	case "projects":
		items, err := d.cfg.IssueDigestFacade.ListProjectProgress(ctx, wsID)
		if err != nil {
			return "", nil, fmt.Errorf("list project progress: %w", err)
		}
		body := FormatProjectProgress(items)
		return body, &port.OutboundRichMessage{Target: port.TargetChat(evt.ChatID), Title: "项目进展", Body: body}, nil
	case "my_todos":
		return d.handleQueryIssue(ctx, evt)
	case "issue":
		issueKey := evt.Intent.Params["issue_key"]
		if !ValidIdentifierFormat(issueKey) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), nil, nil
		}
		progress, err := d.cfg.IssueDigestFacade.GetIssueProgress(ctx, wsID, issueKey)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
			}
			return "", nil, fmt.Errorf("get issue progress: %w", err)
		}
		body := FormatIssueProgress(progress)
		return body, &port.OutboundRichMessage{
			Target:  port.TargetChat(evt.ChatID),
			Title:   fmt.Sprintf("%s 进展", progress.Digest.Issue.Identifier),
			Body:    body,
			Actions: digestActions(progress.Digest),
		}, nil
	default:
		return fmt.Sprintf("[%s] 你是想查某个 Issue，还是看所有项目进展？", replyAskClarify), nil, nil
	}
}

func (d *dispatchStep) handleAddComment(ctx context.Context, evt port.InboundEvent) (string, error) {
	issueKey, _ := evt.Intent.Params["issue_key"]
	comment, _ := evt.Intent.Params["comment"]
	if issueKey == "" || comment == "" {
		return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和评论内容。", replyMissingParam), nil
	}

	if !ValidIdentifierFormat(issueKey) {
		return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), nil
	}

	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return "", fmt.Errorf("lookup workspace: %w", err)
	}

	issue, err := d.cfg.IssueFacade.GetIssueByIdentifier(ctx, wsID, issueKey)
	if err != nil {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil
	}

	user, err := d.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID)
	if err != nil {
		return "", fmt.Errorf("resolve user: %w", err)
	}

	if _, err := d.cfg.CommentFacade.AddComment(ctx, facade.AddCommentReq{
		IssueID:        issue.ID,
		ActorID:        user.MulticaUserID,
		InboundEventID: parseRuntimeEventID(evt.RuntimeEventID),
		Content:        comment,
	}); err != nil {
		return "", fmt.Errorf("add comment: %w", err)
	}

	return fmt.Sprintf("[%s] 已在 %s 上添加评论。", replyCommentAdded, issueKey), nil
}

func (d *dispatchStep) handleUnknown(ctx context.Context, evt port.InboundEvent) (string, error) {
	if draft := strings.TrimSpace(evt.Intent.Params["_user_reply_draft"]); draft != "" {
		return draft, nil
	}
	return "我还没抓到你想推进哪件事。你可以直接问“各项目进展怎么样”，或者说“创建一个 issue：...”", nil
}

func (d *dispatchStep) handleAskClarify(ctx context.Context, evt port.InboundEvent) (string, error) {
	if draft := strings.TrimSpace(evt.Intent.Params["_user_reply_draft"]); draft != "" {
		return draft, nil
	}
	return "我还需要一点上下文：是要查某个 issue、看项目进展，还是创建/回复一个 issue？", nil
}

func (d *dispatchStep) handleMutationIntent(ctx context.Context, evt port.InboundEvent) (string, error) {
	if isContextReplyCommentIntent(evt.Intent) {
		if msg, ok := validateMutationIntent(evt.Intent); !ok {
			return msg, nil
		}
		return d.executeMutationIntent(ctx, evt)
	}
	if d.cfg.ProposalStore == nil {
		return d.executeMutationIntent(ctx, evt)
	}
	if msg, ok := validateMutationIntent(evt.Intent); !ok {
		return msg, nil
	}
	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return "", fmt.Errorf("lookup workspace: %w", err)
	}
	proposal, err := d.cfg.ProposalStore.CreateActionProposal(ctx, ActionProposalCreateRequest{
		ConnectionID:   evt.ConnectionID(),
		ChatID:         evt.ChatID,
		SenderID:       evt.SenderID,
		WorkspaceID:    wsID,
		InboundEventID: parseRuntimeEventID(evt.RuntimeEventID),
		Intent:         evt.Intent,
		ExpiresAt:      time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		return "", fmt.Errorf("create action proposal: %w", err)
	}
	return formatProposalReply(proposal, d.describeIntentWithCurrent(ctx, wsID, proposal.Intent)), nil
}

func isContextReplyCommentIntent(intent port.InboundIntent) bool {
	if intent.Kind != port.IntentAddComment {
		return false
	}
	if intent.Source != port.SourceRule {
		return false
	}
	return strings.TrimSpace(intent.Params[contextMessageIDIntentParam]) != ""
}

func (d *dispatchStep) executeMutationIntent(ctx context.Context, evt port.InboundEvent) (string, error) {
	switch evt.Intent.Kind {
	case port.IntentCreateIssue:
		return d.handleCreateIssue(ctx, evt)
	case port.IntentAddComment:
		return d.handleAddComment(ctx, evt)
	case port.IntentSetStatus:
		return d.handleSetStatus(ctx, evt)
	case port.IntentSetAssignee:
		return d.handleSetAssignee(ctx, evt)
	case port.IntentSetPriority:
		return d.handleSetPriority(ctx, evt)
	case port.IntentSetLabel:
		return d.handleSetLabel(ctx, evt)
	default:
		return fmt.Sprintf("[%s] 没有可执行的变更。", replyUnknown), nil
	}
}

func (d *dispatchStep) handleConfirmAction(ctx context.Context, evt port.InboundEvent) (string, error) {
	proposal, reply, ok, err := d.loadConfirmableProposal(ctx, evt)
	if err != nil || !ok {
		return reply, err
	}
	execEvt := evt
	execEvt.Intent = proposal.Intent
	execEvt.RuntimeEventID = util.UUIDToString(proposal.InboundEventID)
	execReply, err := d.executeMutationIntent(ctx, execEvt)
	if err != nil {
		return "", err
	}
	if err := d.cfg.ProposalStore.MarkActionProposalStatus(ctx, proposal.ID, proposalStatusConfirmed); err != nil {
		return "", fmt.Errorf("mark proposal confirmed: %w", err)
	}
	return fmt.Sprintf("[%s] 已确认并执行。\n%s", replyActionConfirmed, execReply), nil
}

func (d *dispatchStep) handleCancelAction(ctx context.Context, evt port.InboundEvent) (string, error) {
	proposal, reply, ok, err := d.loadConfirmableProposal(ctx, evt)
	if err != nil || !ok {
		return reply, err
	}
	if err := d.cfg.ProposalStore.MarkActionProposalStatus(ctx, proposal.ID, proposalStatusCancelled); err != nil {
		return "", fmt.Errorf("mark proposal cancelled: %w", err)
	}
	return fmt.Sprintf("[%s] 已取消：%s", replyActionCancelled, describeIntent(proposal.Intent)), nil
}

func (d *dispatchStep) loadConfirmableProposal(ctx context.Context, evt port.InboundEvent) (ActionProposal, string, bool, error) {
	if d.cfg.ProposalStore == nil {
		return ActionProposal{}, "", false, fmt.Errorf("proposal store is not configured")
	}
	code := strings.ToUpper(strings.TrimSpace(evt.Intent.Params["code"]))
	if code == "" {
		return ActionProposal{}, fmt.Sprintf("[%s] 缺少确认码。", replyMissingParam), false, nil
	}
	proposal, err := d.cfg.ProposalStore.FindActionProposal(ctx, evt.ConnectionID(), evt.ChatID, evt.SenderID, code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ActionProposal{}, fmt.Sprintf("[%s] 找不到待确认动作 %s。", replyIssueNotFound, code), false, nil
		}
		return ActionProposal{}, "", false, fmt.Errorf("find action proposal: %w", err)
	}
	if proposal.Status == proposalStatusConfirmed {
		return ActionProposal{}, fmt.Sprintf("[%s] 动作 %s 已执行过。", replyActionConfirmed, proposal.Code), false, nil
	}
	if proposal.Status == proposalStatusCancelled {
		return ActionProposal{}, fmt.Sprintf("[%s] 动作 %s 已取消。", replyActionCancelled, proposal.Code), false, nil
	}
	if proposal.Status == proposalStatusExpired || time.Now().After(proposal.ExpiresAt) {
		_ = d.cfg.ProposalStore.MarkActionProposalStatus(ctx, proposal.ID, proposalStatusExpired)
		return ActionProposal{}, fmt.Sprintf("[%s] 动作 %s 已过期，请重新发起。", replyActionExpired, proposal.Code), false, nil
	}
	return proposal, "", true, nil
}

func (d *dispatchStep) handleQueryIssue(ctx context.Context, evt port.InboundEvent) (string, *port.OutboundRichMessage, error) {
	issueKey, hasKey := evt.Intent.Params["issue_key"]

	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return "", nil, fmt.Errorf("lookup workspace: %w", err)
	}

	if !hasKey || issueKey == "" {
		user, err := d.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID)
		if err != nil {
			return "", nil, fmt.Errorf("resolve user: %w", err)
		}

		issues, err := d.cfg.IssueFacade.ListMyTodos(ctx, wsID, user.MulticaUserID)
		if err != nil {
			return "", nil, fmt.Errorf("list todos: %w", err)
		}
		if len(issues) == 0 {
			return "你没有待办的 Issue。", nil, nil
		}
		msg := "你的待办：\n"
		for i, iss := range issues {
			if i >= 10 {
				msg += fmt.Sprintf("... 还有 %d 条\n", len(issues)-10)
				break
			}
			msg += fmt.Sprintf("  %s [%s] %s\n", iss.Identifier, iss.Status, iss.Title)
		}
		return msg, nil, nil
	}

	if !ValidIdentifierFormat(issueKey) {
		return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), nil, nil
	}

	if d.cfg.IssueDigestFacade != nil {
		digest, err := d.cfg.IssueDigestFacade.GetIssueDigest(ctx, wsID, issueKey)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
			}
			return "", nil, fmt.Errorf("get issue digest: %w", err)
		}
		body := FormatIssueDigest(digest)
		rich := port.OutboundRichMessage{
			Target:  port.TargetChat(evt.ChatID),
			Title:   fmt.Sprintf("%s 工作摘要", digest.Issue.Identifier),
			Body:    body,
			Actions: digestActions(digest),
		}
		return body, &rich, nil
	}

	issue, err := d.cfg.IssueFacade.GetIssueByIdentifier(ctx, wsID, issueKey)
	if err != nil {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
	}

	msg := fmt.Sprintf("📋 %s [%s] %s",
		issue.Identifier, issue.Status, issue.Title)

	if user, err := d.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID); err == nil && user.DisplayName != "" {
		msg += fmt.Sprintf("\n查询者: %s", user.DisplayName)
	}

	return msg, nil, nil
}

func (d *dispatchStep) handleIssueDetail(ctx context.Context, evt port.InboundEvent) (string, *port.OutboundRichMessage, error) {
	issueKey, wsID, msg, ok, err := d.resolveReadOnlyIssueKey(ctx, evt)
	if err != nil || !ok {
		return msg, nil, err
	}
	if d.cfg.IssueDigestFacade == nil {
		return fmt.Sprintf("[%s] 详情查询服务未配置。", replyInternalError), nil, nil
	}
	detail, err := d.cfg.IssueDigestFacade.GetIssueDetail(ctx, wsID, issueKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
		}
		return "", nil, fmt.Errorf("get issue detail: %w", err)
	}
	body := FormatIssueDetail(detail)
	rich := port.OutboundRichMessage{
		Target:  port.TargetChat(evt.ChatID),
		Title:   fmt.Sprintf("%s 详情", detail.Digest.Issue.Identifier),
		Body:    body,
		Actions: digestActions(detail.Digest),
	}
	return body, &rich, nil
}

func (d *dispatchStep) handleIssueTimeline(ctx context.Context, evt port.InboundEvent) (string, *port.OutboundRichMessage, error) {
	issueKey, wsID, msg, ok, err := d.resolveReadOnlyIssueKey(ctx, evt)
	if err != nil || !ok {
		return msg, nil, err
	}
	if d.cfg.IssueDigestFacade == nil {
		return fmt.Sprintf("[%s] 动态查询服务未配置。", replyInternalError), nil, nil
	}
	page := pageParam(evt.Intent.Params["page"])
	timeline, err := d.cfg.IssueDigestFacade.GetIssueTimeline(ctx, wsID, issueKey, page, 5)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
		}
		return "", nil, fmt.Errorf("get issue timeline: %w", err)
	}
	body := FormatIssueTimeline(timeline)
	rich := port.OutboundRichMessage{
		Target: port.TargetChat(evt.ChatID),
		Title:  fmt.Sprintf("%s 动态", timeline.Issue.Identifier),
		Body:   body,
	}
	return body, &rich, nil
}

func (d *dispatchStep) handleIssueLogs(ctx context.Context, evt port.InboundEvent) (string, *port.OutboundRichMessage, error) {
	issueKey, wsID, msg, ok, err := d.resolveReadOnlyIssueKey(ctx, evt)
	if err != nil || !ok {
		return msg, nil, err
	}
	if d.cfg.IssueDigestFacade == nil {
		return fmt.Sprintf("[%s] 日志查询服务未配置。", replyInternalError), nil, nil
	}
	page := pageParam(evt.Intent.Params["page"])
	logs, err := d.cfg.IssueDigestFacade.GetIssueLogs(ctx, wsID, issueKey, page, 8)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil, nil
		}
		return "", nil, fmt.Errorf("get issue logs: %w", err)
	}
	body := FormatIssueLogs(logs)
	rich := port.OutboundRichMessage{
		Target: port.TargetChat(evt.ChatID),
		Title:  fmt.Sprintf("%s 执行日志", logs.Issue.Identifier),
		Body:   body,
	}
	return body, &rich, nil
}

func (d *dispatchStep) resolveReadOnlyIssueKey(ctx context.Context, evt port.InboundEvent) (string, pgtype.UUID, string, bool, error) {
	issueKey := evt.Intent.Params["issue_key"]
	if !ValidIdentifierFormat(issueKey) {
		return issueKey, pgtype.UUID{}, fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false, nil
	}
	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return issueKey, pgtype.UUID{}, "", false, fmt.Errorf("lookup workspace: %w", err)
	}
	return issueKey, wsID, "", true, nil
}

func (d *dispatchStep) handleSetStatus(ctx context.Context, evt port.InboundEvent) (string, error) {
	issueKey, status := evt.Intent.Params["issue_key"], evt.Intent.Params["status"]
	if issueKey == "" || status == "" {
		return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和目标状态。", replyMissingParam), nil
	}
	issue, user, _, err := d.resolveIssueAndUser(ctx, evt, issueKey)
	if err != nil {
		return "", err
	}
	if issue.ID == (pgtype.UUID{}) {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil
	}
	if err := d.cfg.IssueFacade.SetIssueStatus(ctx, issue.ID, user.MulticaUserID, status, facade.ChannelMutationContext{InboundEventID: parseRuntimeEventID(evt.RuntimeEventID)}); err != nil {
		return "", fmt.Errorf("set status: %w", err)
	}
	return fmt.Sprintf("[%s] 已将 %s 状态改为 %s。", replyStatusChanged, issueKey, status), nil
}

func (d *dispatchStep) handleSetAssignee(ctx context.Context, evt port.InboundEvent) (string, error) {
	issueKey, assignee := evt.Intent.Params["issue_key"], evt.Intent.Params["assignee"]
	if issueKey == "" || assignee == "" {
		return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和指派人。", replyMissingParam), nil
	}
	issue, user, _, err := d.resolveIssueAndUser(ctx, evt, issueKey)
	if err != nil {
		return "", err
	}
	if issue.ID == (pgtype.UUID{}) {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil
	}
	if err := d.cfg.IssueFacade.SetIssueAssignee(ctx, issue.ID, user.MulticaUserID, assignee, facade.ChannelMutationContext{InboundEventID: parseRuntimeEventID(evt.RuntimeEventID)}); err != nil {
		return "", fmt.Errorf("set assignee: %w", err)
	}
	return fmt.Sprintf("[%s] 已将 %s 的指派人改为 %s。", replyAssigneeChanged, issueKey, assignee), nil
}

func (d *dispatchStep) handleSetPriority(ctx context.Context, evt port.InboundEvent) (string, error) {
	issueKey, priority := evt.Intent.Params["issue_key"], evt.Intent.Params["priority"]
	if issueKey == "" || priority == "" {
		return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和目标优先级。", replyMissingParam), nil
	}
	issue, user, _, err := d.resolveIssueAndUser(ctx, evt, issueKey)
	if err != nil {
		return "", err
	}
	if issue.ID == (pgtype.UUID{}) {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil
	}
	if err := d.cfg.IssueFacade.SetIssuePriority(ctx, issue.ID, user.MulticaUserID, priority, facade.ChannelMutationContext{InboundEventID: parseRuntimeEventID(evt.RuntimeEventID)}); err != nil {
		return "", fmt.Errorf("set priority: %w", err)
	}
	return fmt.Sprintf("[%s] 已将 %s 的优先级改为 %s。", replyPriorityChanged, issueKey, priority), nil
}

func (d *dispatchStep) handleSetLabel(ctx context.Context, evt port.InboundEvent) (string, error) {
	issueKey, label, op := evt.Intent.Params["issue_key"], evt.Intent.Params["label"], evt.Intent.Params["op"]
	if issueKey == "" || label == "" {
		return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和标签名。", replyMissingParam), nil
	}
	issue, user, _, err := d.resolveIssueAndUser(ctx, evt, issueKey)
	if err != nil {
		return "", err
	}
	if issue.ID == (pgtype.UUID{}) {
		return fmt.Sprintf("[%s] 找不到 Issue %s。", replyIssueNotFound, issueKey), nil
	}
	if op == "remove" {
		if err := d.cfg.IssueFacade.RemoveIssueLabel(ctx, issue.ID, user.MulticaUserID, label, facade.ChannelMutationContext{InboundEventID: parseRuntimeEventID(evt.RuntimeEventID)}); err != nil {
			return "", fmt.Errorf("remove label: %w", err)
		}
		return fmt.Sprintf("[%s] 已从 %s 去掉标签 %s。", replyLabelRemoved, issueKey, label), nil
	}
	if err := d.cfg.IssueFacade.AddIssueLabel(ctx, issue.ID, user.MulticaUserID, label, facade.ChannelMutationContext{InboundEventID: parseRuntimeEventID(evt.RuntimeEventID)}); err != nil {
		return "", fmt.Errorf("add label: %w", err)
	}
	return fmt.Sprintf("[%s] 已为 %s 添加标签 %s。", replyLabelAdded, issueKey, label), nil
}

func (d *dispatchStep) resolveIssueAndUser(ctx context.Context, evt port.InboundEvent, issueKey string) (facade.Issue, ResolvedUser, pgtype.UUID, error) {
	if !ValidIdentifierFormat(issueKey) {
		return facade.Issue{}, ResolvedUser{}, pgtype.UUID{}, fmt.Errorf("[%s] Issue 编号格式不正确。", replyIssueNotFound)
	}
	wsID, err := d.lookupWorkspaceID(ctx, evt)
	if err != nil {
		return facade.Issue{}, ResolvedUser{}, pgtype.UUID{}, fmt.Errorf("lookup workspace: %w", err)
	}
	issue, err := d.cfg.IssueFacade.GetIssueByIdentifier(ctx, wsID, issueKey)
	if err != nil {
		return facade.Issue{}, ResolvedUser{}, wsID, nil // not found — caller formats reply
	}
	user, err := d.cfg.UserResolver.Resolve(ctx, evt.ConnectionID(), evt.SenderID)
	if err != nil {
		return facade.Issue{}, ResolvedUser{}, wsID, fmt.Errorf("resolve user: %w", err)
	}
	return issue, user, wsID, nil
}

func (d *dispatchStep) sendReply(ctx context.Context, evt port.InboundEvent, text string) error {
	if d.cfg.ReplySink == nil {
		return nil
	}
	err := d.cfg.ReplySink.SendText(ctx, evt, port.OutboundMessage{
		Target: port.TargetChat(evt.ChatID),
		Text:   text,
	})
	return err
}

func (d *dispatchStep) sendRichReply(ctx context.Context, evt port.InboundEvent, msg port.OutboundRichMessage) error {
	if d.cfg.ReplySink == nil {
		return nil
	}
	if msg.Body == "" {
		msg.Body = msg.Title
	}
	if msg.Title == "" {
		msg.Title = "Multica"
	}
	if msg.Target.ID == "" {
		msg.Target = port.TargetChat(evt.ChatID)
	}
	return d.cfg.ReplySink.SendRich(ctx, evt, msg)
}

func validateMutationIntent(intent port.InboundIntent) (string, bool) {
	switch intent.Kind {
	case port.IntentCreateIssue:
		if strings.TrimSpace(intent.Params["title"]) == "" {
			return fmt.Sprintf("[%s] 缺少 Issue 标题，请提供要创建的内容。", replyMissingParam), false
		}
	case port.IntentAddComment:
		if strings.TrimSpace(intent.Params["issue_key"]) == "" || strings.TrimSpace(intent.Params["comment"]) == "" {
			return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和评论内容。", replyMissingParam), false
		}
		if !ValidIdentifierFormat(intent.Params["issue_key"]) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false
		}
	case port.IntentSetStatus:
		if strings.TrimSpace(intent.Params["issue_key"]) == "" || strings.TrimSpace(intent.Params["status"]) == "" {
			return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和目标状态。", replyMissingParam), false
		}
		if !ValidIdentifierFormat(intent.Params["issue_key"]) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false
		}
	case port.IntentSetAssignee:
		if strings.TrimSpace(intent.Params["issue_key"]) == "" || strings.TrimSpace(intent.Params["assignee"]) == "" {
			return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和指派人。", replyMissingParam), false
		}
		if !ValidIdentifierFormat(intent.Params["issue_key"]) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false
		}
	case port.IntentSetPriority:
		if strings.TrimSpace(intent.Params["issue_key"]) == "" || strings.TrimSpace(intent.Params["priority"]) == "" {
			return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和目标优先级。", replyMissingParam), false
		}
		if !ValidIdentifierFormat(intent.Params["issue_key"]) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false
		}
	case port.IntentSetLabel:
		if strings.TrimSpace(intent.Params["issue_key"]) == "" || strings.TrimSpace(intent.Params["label"]) == "" {
			return fmt.Sprintf("[%s] 缺少参数：需要 Issue 编号和标签名。", replyMissingParam), false
		}
		if !ValidIdentifierFormat(intent.Params["issue_key"]) {
			return fmt.Sprintf("[%s] Issue 编号格式不正确。", replyIssueNotFound), false
		}
	}
	return "", true
}

func formatProposalReply(proposal ActionProposal, description string) string {
	if strings.TrimSpace(description) == "" {
		description = describeIntent(proposal.Intent)
	}
	return fmt.Sprintf("[%s] 待确认动作，不会立即执行。\n将要执行：%s\n影响对象：%s\n确认：/confirm %s\n取消：/cancel %s\n有效期至：%s",
		replyActionProposed,
		description,
		emptyAs(proposal.Intent.Params["issue_key"], "新 Issue"),
		proposal.Code,
		proposal.Code,
		proposal.ExpiresAt.Format("15:04:05"),
	)
}

func (d *dispatchStep) describeIntentWithCurrent(ctx context.Context, wsID pgtype.UUID, intent port.InboundIntent) string {
	issueKey := intent.Params["issue_key"]
	if d.cfg.IssueDigestFacade == nil || !ValidIdentifierFormat(issueKey) {
		return describeIntent(intent)
	}
	digest, err := d.cfg.IssueDigestFacade.GetIssueDigest(ctx, wsID, issueKey)
	if err != nil {
		return describeIntent(intent)
	}
	switch intent.Kind {
	case port.IntentSetStatus:
		return fmt.Sprintf("把 %s 状态从 %s 改为 %s", issueKey, emptyAs(digest.Issue.Status, "未知"), intent.Params["status"])
	case port.IntentSetPriority:
		return fmt.Sprintf("把 %s 优先级从 %s 改为 %s", issueKey, emptyAs(digest.Issue.Priority, "none"), intent.Params["priority"])
	case port.IntentSetAssignee:
		return fmt.Sprintf("把 %s 负责人从 %s 改为 %s", issueKey, emptyAs(digest.AssigneeName, "未指派"), intent.Params["assignee"])
	default:
		return describeIntent(intent)
	}
}

func describeIntent(intent port.InboundIntent) string {
	switch intent.Kind {
	case port.IntentCreateIssue:
		return fmt.Sprintf("创建 Issue「%s」", intent.Params["title"])
	case port.IntentAddComment:
		return fmt.Sprintf("在 %s 添加评论「%s」", intent.Params["issue_key"], truncateDisplay(intent.Params["comment"], 80))
	case port.IntentSetStatus:
		return fmt.Sprintf("把 %s 状态改为 %s", intent.Params["issue_key"], intent.Params["status"])
	case port.IntentSetAssignee:
		return fmt.Sprintf("把 %s 指派给 %s", intent.Params["issue_key"], intent.Params["assignee"])
	case port.IntentSetPriority:
		return fmt.Sprintf("把 %s 优先级改为 %s", intent.Params["issue_key"], intent.Params["priority"])
	case port.IntentSetLabel:
		if intent.Params["op"] == "remove" {
			return fmt.Sprintf("从 %s 去掉标签 %s", intent.Params["issue_key"], intent.Params["label"])
		}
		return fmt.Sprintf("给 %s 添加标签 %s", intent.Params["issue_key"], intent.Params["label"])
	default:
		return "未知动作"
	}
}

func FormatIssueDigest(digest facade.IssueDigest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 %s [%s] %s\n", digest.Issue.Identifier, digest.Issue.Status, digest.Issue.Title)
	fmt.Fprintf(&b, "优先级：%s", emptyAs(digest.Issue.Priority, "none"))
	if digest.ProjectName != "" {
		fmt.Fprintf(&b, " · 项目：%s", digest.ProjectName)
	}
	if digest.AssigneeName != "" {
		fmt.Fprintf(&b, " · 负责人：%s", digest.AssigneeName)
	} else if digest.AssigneeType != "" {
		fmt.Fprintf(&b, " · 负责人：%s", digest.AssigneeType)
	}
	if !digest.Issue.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, " · 更新：%s", digest.Issue.UpdatedAt.Format("01-02 15:04"))
	}
	b.WriteString("\n")
	if len(digest.RecentEvents) > 0 {
		b.WriteString("\n最近动态：")
		for _, event := range digest.RecentEvents {
			actor := emptyAs(event.ActorName, "系统")
			fmt.Fprintf(&b, "\n- %s：%s", actor, event.Summary)
		}
	}
	if digest.AgentSummary != nil {
		b.WriteString("\n\nAgent：")
		agent := emptyAs(digest.AgentSummary.AgentName, "agent")
		fmt.Fprintf(&b, "%s [%s]", agent, digest.AgentSummary.Status)
		if digest.AgentSummary.Progress != "" {
			fmt.Fprintf(&b, "\n- 进度：%s", digest.AgentSummary.Progress)
		}
		if digest.AgentSummary.ResultSummary != "" {
			fmt.Fprintf(&b, "\n- 输出：%s", digest.AgentSummary.ResultSummary)
		}
		if digest.AgentSummary.FailureReason != "" {
			fmt.Fprintf(&b, "\n- 失败原因：%s", digest.AgentSummary.FailureReason)
		}
	} else {
		b.WriteString("\n\nAgent：暂无执行记录")
	}
	fmt.Fprintf(&b, "\n\n下一步：%s", nextStepForDigest(digest))
	fmt.Fprintf(&b, "\n\n展开：/detail %s · /timeline %s · /logs %s", digest.Issue.Identifier, digest.Issue.Identifier, digest.Issue.Identifier)
	return b.String()
}

func FormatIssueProgress(progress facade.IssueProgress) string {
	digest := progress.Digest
	var b strings.Builder
	fmt.Fprintf(&b, "%s 目前是 %s：%s", digest.Issue.Identifier, digest.Issue.Status, digest.Issue.Title)
	if digest.AssigneeName != "" {
		fmt.Fprintf(&b, "\n负责人：%s", digest.AssigneeName)
	} else {
		b.WriteString("\n负责人：未指派")
	}
	if progress.LatestStatus != nil {
		fmt.Fprintf(&b, "\n最近状态：%s", progress.LatestStatus.Summary)
	}
	if progress.LatestReply != nil && progress.LatestReply.Content != "" {
		author := emptyAs(progress.LatestReply.AuthorName, progress.LatestReply.AuthorType)
		fmt.Fprintf(&b, "\n\n最新回复（%s，%s）：\n%s",
			emptyAs(author, "未知"),
			progress.LatestReply.CreatedAt.Format("01-02 15:04"),
			progress.LatestReply.Content,
		)
	} else if digest.AgentSummary != nil && digest.AgentSummary.ResultSummary != "" {
		fmt.Fprintf(&b, "\n\n最新输出：\n%s", digest.AgentSummary.ResultSummary)
	} else if len(digest.RecentEvents) > 0 {
		b.WriteString("\n\n最近动态：")
		for _, event := range digest.RecentEvents {
			fmt.Fprintf(&b, "\n- %s：%s", emptyAs(event.ActorName, "系统"), event.Summary)
		}
	} else {
		b.WriteString("\n\n还没有实质回复。")
	}
	next := progress.RecommendedNext
	if next == "" {
		next = nextStepForDigest(digest)
	}
	fmt.Fprintf(&b, "\n\n下一步：%s", next)
	fmt.Fprintf(&b, "\n\n展开：/detail %s · /timeline %s · /logs %s", digest.Issue.Identifier, digest.Issue.Identifier, digest.Issue.Identifier)
	return b.String()
}

func FormatProjectProgress(items []facade.ProjectProgress) string {
	if len(items) == 0 {
		return "当前 workspace 还没有项目。"
	}
	var b strings.Builder
	b.WriteString("项目进展：")
	for _, item := range items {
		fmt.Fprintf(&b, "\n\n%s：开放 %d / 总计 %d", item.ProjectName, item.Open, item.Total)
		if item.Blocked > 0 || item.InReview > 0 || item.InProgress > 0 {
			fmt.Fprintf(&b, "\n- 处理中 %d · Review %d · 阻塞 %d", item.InProgress, item.InReview, item.Blocked)
		}
		if len(item.FocusIssues) > 0 {
			b.WriteString("\n- 关注：")
			for _, issue := range item.FocusIssues {
				assignee := ""
				if issue.Assignee != "" {
					assignee = " @" + issue.Assignee
				}
				fmt.Fprintf(&b, "\n  %s [%s]%s %s", issue.Identifier, issue.Status, assignee, issue.Title)
			}
		}
	}
	return b.String()
}

func FormatIssueDetail(detail facade.IssueDetail) string {
	digest := detail.Digest
	var b strings.Builder
	fmt.Fprintf(&b, "📌 %s 详情\n%s [%s]\n", digest.Issue.Identifier, digest.Issue.Title, digest.Issue.Status)
	fmt.Fprintf(&b, "优先级：%s", emptyAs(digest.Issue.Priority, "none"))
	if digest.ProjectName != "" {
		fmt.Fprintf(&b, " · 项目：%s", digest.ProjectName)
	}
	if digest.AssigneeName != "" {
		fmt.Fprintf(&b, " · 负责人：%s", digest.AssigneeName)
	} else {
		b.WriteString(" · 负责人：未指派")
	}
	if digest.CreatorName != "" {
		fmt.Fprintf(&b, " · 创建者：%s", digest.CreatorName)
	}
	if !digest.Issue.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "\n创建：%s", digest.Issue.CreatedAt.Format("01-02 15:04"))
	}
	if !digest.Issue.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, " · 更新：%s", digest.Issue.UpdatedAt.Format("01-02 15:04"))
	}
	if len(digest.Labels) > 0 {
		fmt.Fprintf(&b, "\n标签：%s", strings.Join(digest.Labels, ", "))
	}
	if digest.Issue.Description != "" {
		fmt.Fprintf(&b, "\n\n描述：\n%s", truncateDisplay(digest.Issue.Description, 600))
	} else {
		b.WriteString("\n\n描述：暂无")
	}
	if len(detail.StatusHistory) > 0 {
		b.WriteString("\n\n状态/指派历史：")
		for _, event := range detail.StatusHistory {
			fmt.Fprintf(&b, "\n- %s %s：%s", event.CreatedAt.Format("01-02 15:04"), emptyAs(event.ActorName, "系统"), event.Summary)
		}
	}
	if digest.AgentSummary != nil {
		fmt.Fprintf(&b, "\n\nAgent：%s [%s]", emptyAs(digest.AgentSummary.AgentName, "agent"), digest.AgentSummary.Status)
		if digest.AgentSummary.Progress != "" {
			fmt.Fprintf(&b, "\n- 最近进度：%s", digest.AgentSummary.Progress)
		}
		if digest.AgentSummary.ResultSummary != "" {
			fmt.Fprintf(&b, "\n- 输出：%s", digest.AgentSummary.ResultSummary)
		}
		if digest.AgentSummary.FailureReason != "" {
			fmt.Fprintf(&b, "\n- 失败原因：%s", digest.AgentSummary.FailureReason)
		}
	} else {
		b.WriteString("\n\nAgent：暂无执行记录")
	}
	fmt.Fprintf(&b, "\n\n下一步：%s", nextStepForDigest(digest))
	fmt.Fprintf(&b, "\n\n继续展开：/timeline %s · /logs %s", digest.Issue.Identifier, digest.Issue.Identifier)
	return b.String()
}

func FormatIssueTimeline(page facade.IssueTimelinePage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🧾 %s 动态 第 %d 页\n%s [%s]", page.Issue.Identifier, page.Page, page.Issue.Title, page.Issue.Status)
	if len(page.Events) == 0 {
		b.WriteString("\n\n暂无更多动态。")
		return b.String()
	}
	for _, event := range page.Events {
		fmt.Fprintf(&b, "\n- %s %s：%s", event.CreatedAt.Format("01-02 15:04"), emptyAs(event.ActorName, "系统"), event.Summary)
	}
	if page.HasMore {
		fmt.Fprintf(&b, "\n\n更多：/timeline %s %d", page.Issue.Identifier, page.Page+1)
	}
	return b.String()
}

func FormatIssueLogs(page facade.IssueLogPage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🛠 %s 执行日志 第 %d 页\n%s [%s]", page.Issue.Identifier, page.Page, page.Issue.Title, page.Issue.Status)
	if page.TaskID == "" {
		b.WriteString("\n\n暂无 agent 执行记录。")
		return b.String()
	}
	fmt.Fprintf(&b, "\nAgent：%s [%s]", emptyAs(page.AgentName, "agent"), emptyAs(page.TaskStatus, "unknown"))
	if page.ResultSummary != "" {
		fmt.Fprintf(&b, "\n输出：%s", page.ResultSummary)
	}
	if page.FailureReason != "" {
		fmt.Fprintf(&b, "\n失败原因：%s", page.FailureReason)
	}
	if len(page.Messages) == 0 {
		b.WriteString("\n\n暂无更多日志。")
		return b.String()
	}
	b.WriteString("\n\n最近日志：")
	for _, msg := range page.Messages {
		label := msg.Type
		if msg.Tool != "" {
			label += "/" + msg.Tool
		}
		fmt.Fprintf(&b, "\n- #%d %s %s：%s", msg.Seq, msg.CreatedAt.Format("01-02 15:04"), emptyAs(label, "message"), msg.Content)
	}
	if page.HasMore {
		fmt.Fprintf(&b, "\n\n更多：/logs %s %d", page.Issue.Identifier, page.Page+1)
	}
	return b.String()
}

func nextStepForDigest(digest facade.IssueDigest) string {
	switch digest.Issue.Status {
	case "in_review":
		return "需要 reviewer 处理；如果已经通过，请确认后改为 done。"
	case "blocked":
		return "先补充阻塞原因或解除依赖，再继续推进。"
	case "todo", "backlog":
		if digest.AssigneeName == "" && digest.AgentSummary == nil {
			return "建议先指派负责人，或触发 agent 开始处理。"
		}
		return "建议确认负责人/agent 是否已经开始推进。"
	case "in_progress":
		if digest.AgentSummary != nil && (digest.AgentSummary.Status == "queued" || digest.AgentSummary.Status == "running" || digest.AgentSummary.Status == "dispatched") {
			return "agent 正在处理，关注执行日志和最新输出即可。"
		}
		return "关注最近动态；如果长时间无更新，建议追问负责人或补充上下文。"
	case "done":
		return "已完成；如结果不符合预期，补充评论后重新打开或重跑 agent。"
	default:
		return "查看最近动态后决定是否补充评论、指派负责人或触发 agent。"
	}
}

func digestActions(digest facade.IssueDigest) []port.OutboundAction {
	base := strings.TrimRight(os.Getenv("MULTICA_APP_URL"), "/")
	if !isPublicAppURL(base) || digest.WorkspaceSlug == "" || !digest.Issue.ID.Valid {
		return nil
	}
	issueURL := fmt.Sprintf("%s/%s/issues/%s", base, digest.WorkspaceSlug, util.UUIDToString(digest.Issue.ID))
	actions := []port.OutboundAction{{Label: "打开 Issue", URL: issueURL}}
	if digest.AgentSummary != nil && digest.AgentSummary.TaskID != "" {
		actions = append(actions, port.OutboundAction{Label: "查看执行日志", URL: issueURL + "?tab=activity"})
	}
	return actions
}

func isPublicAppURL(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "0.0.0.0" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

func pageParam(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	if page > 99 {
		return 99
	}
	return page
}

func emptyAs(s, fallback string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func truncateDisplay(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "..."
}

func parseRuntimeEventID(id string) pgtype.UUID {
	if id == "" {
		return pgtype.UUID{}
	}
	parsed, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.UUID{}
	}
	return parsed
}

// identifierRe matches valid issue identifiers like STA-39, MUL-123.
// Format: 2-5 uppercase letters, hyphen, positive integer (no leading zeros).
var identifierRe = regexp.MustCompile(`^[A-Z]{2,5}-[1-9][0-9]*$`)

// ValidIdentifierFormat checks if an issue identifier matches the expected
// format (e.g. STA-39, MUL-123). Exported for testing.
func ValidIdentifierFormat(key string) bool {
	return identifierRe.MatchString(key)
}
