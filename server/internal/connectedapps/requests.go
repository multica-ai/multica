package connectedapps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	"github.com/multica-ai/multica/server/internal/util"
	sdk "github.com/multica-ai/multica/server/pkg/composio"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// EventRequestChanged tells clients to refetch the requests of an issue or
// chat. The payload carries ids and status only; the request body is read
// through the authorized API.
const EventRequestChanged = "app_request:changed"

// EventCallCreated tells clients an issue's runs made another app call, so
// its call chips and sidebar refetch.
const EventCallCreated = "app_call:created"

// Inbox notification types.
const (
	NotifActionRequested  = "app_action_requested"
	NotifActionReminder   = "app_action_reminder"
	NotifConnectRequested = "app_connect_requested"
	NotifActionResult     = "app_action_result"
)

// Request kinds.
const (
	KindAction  = "action"
	KindConnect = "connect"
)

func (s *Service) submit(ctx context.Context, rn *run, app db.AgentApp, tool *sdk.Tool, class string, args map[string]any, summary string) (db.AppActionRequest, error) {
	return s.createRequest(ctx, rn, KindAction, app, tool.Slug, class, args, summary)
}

func (s *Service) createRequest(ctx context.Context, rn *run, kind string, app db.AgentApp, toolSlug, class string, args map[string]any, summary string) (db.AppActionRequest, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return db.AppActionRequest{}, err
	}
	slug := NormalizeSlug(app.ToolkitSlug)
	if existing, err := s.q.FindPendingAppActionRequest(ctx, db.FindPendingAppActionRequestParams{
		TaskID: rn.task.ID, Kind: kind, ToolkitSlug: slug, ToolSlug: toolSlug, Arguments: raw,
	}); err == nil {
		return existing, nil
	} else if !isNoRows(err) {
		return db.AppActionRequest{}, err
	}
	if class == "" {
		class = ClassSend
	}
	req, err := s.q.CreateAppActionRequest(ctx, db.CreateAppActionRequestParams{
		WorkspaceID:    rn.workspaceID,
		Kind:           kind,
		TaskID:         rn.task.ID,
		AgentID:        rn.agent.ID,
		IssueID:        rn.task.IssueID,
		ChatSessionID:  rn.task.ChatSessionID,
		ApproverUserID: rn.triggeredBy,
		AccountScope:   app.AccountScope,
		ToolkitSlug:    slug,
		ToolSlug:       toolSlug,
		ActionClass:    class,
		Arguments:      raw,
		Summary:        truncate(summary, 500),
		ExpiresAt:      pgtype.Timestamptz{Time: s.now().Add(requestTTL(kind)), Valid: true},
	})
	if err != nil {
		return db.AppActionRequest{}, err
	}
	notif := NotifActionRequested
	if kind == KindConnect {
		notif = NotifConnectRequested
	}
	s.notify(ctx, req, notif)
	s.publishChange(req)
	return req, nil
}

// Approve executes an approved request with args (nil keeps the agent's) and
// resumes the agent with the outcome. Only the approver may approve.
func (s *Service) Approve(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID, args map[string]any) (db.AppActionRequest, error) {
	req, err := s.pendingForApprover(ctx, workspaceID, actorID, requestID)
	if err != nil {
		return db.AppActionRequest{}, err
	}
	if req.Kind == KindConnect {
		if _, err := s.resolveAccount(ctx, workspaceID, actorID, req.ToolkitSlug, req.AccountScope); err != nil {
			return db.AppActionRequest{}, ErrAccountMissing
		}
	}
	approvedArgs := req.Arguments
	if args != nil && req.Kind == KindAction {
		if approvedArgs, err = json.Marshal(args); err != nil {
			return db.AppActionRequest{}, err
		}
	}
	decided, err := s.q.DecideAppActionRequest(ctx, db.DecideAppActionRequestParams{
		ID: requestID, WorkspaceID: workspaceID, Status: "approved", DecidedBy: actorID,
		ApprovedArguments: approvedArgs,
	})
	if isNoRows(err) {
		return db.AppActionRequest{}, ErrNotPending
	}
	if err != nil {
		return db.AppActionRequest{}, err
	}
	if decided.Kind == KindConnect {
		s.publishChange(decided)
		s.resume(ctx, decided, actorID, fmt.Sprintf(
			"The %s account you asked for (request %s) is now connected. Continue the task and use the app.",
			appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID)))
		return decided, nil
	}

	final, note := s.executeApproved(ctx, decided)
	s.publishChange(final)
	s.resume(ctx, final, actorID, note)
	return final, nil
}

// ResolveConnectRequests completes the connection requests a member was
// asked to answer for slug, once they connected it, and resumes each agent.
// It runs from the OAuth callback, so nobody has to come back and confirm.
func (s *Service) ResolveConnectRequests(ctx context.Context, userID pgtype.UUID, slug string) {
	rows, err := s.q.ListPendingConnectRequestsForUser(ctx, db.ListPendingConnectRequestsForUserParams{
		ApproverUserID: userID, ToolkitSlug: NormalizeSlug(slug),
	})
	if err != nil {
		slog.Warn("connectedapps: list connect requests failed", "toolkit", slug, "error", err)
		return
	}
	for _, r := range rows {
		if _, err := s.Approve(ctx, r.WorkspaceID, userID, r.ID, nil); err != nil && !errors.Is(err, ErrNotPending) {
			slog.Warn("connectedapps: resolve connect request failed", "request_id", util.UUIDToString(r.ID), "error", err)
		}
	}
}

// Reject declines a request and resumes the agent so it can adapt.
func (s *Service) Reject(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID, reason string) (db.AppActionRequest, error) {
	if _, err := s.pendingForApprover(ctx, workspaceID, actorID, requestID); err != nil {
		return db.AppActionRequest{}, err
	}
	reason = truncate(strings.TrimSpace(reason), 1000)
	decided, err := s.q.DecideAppActionRequest(ctx, db.DecideAppActionRequestParams{
		ID: requestID, WorkspaceID: workspaceID, Status: "rejected", DecidedBy: actorID,
		Reason: pgtype.Text{String: reason, Valid: reason != ""},
	})
	if isNoRows(err) {
		return db.AppActionRequest{}, ErrNotPending
	}
	if err != nil {
		return db.AppActionRequest{}, err
	}
	if decided.Kind == KindAction {
		s.recordCall(ctx, toolCall{
			workspaceID: decided.WorkspaceID, taskID: decided.TaskID, agentID: decided.AgentID,
			issueID: decided.IssueID, chatSessionID: decided.ChatSessionID, triggeredBy: decided.ApproverUserID,
			acct:    account{scope: decided.AccountScope, owner: personalOwner(decided)},
			toolkit: decided.ToolkitSlug, tool: decided.ToolSlug, class: decided.ActionClass,
			outcome: "rejected", requestID: decided.ID,
		})
	}
	s.publishChange(decided)
	note := fmt.Sprintf("The person declined your %s request %s (%s).", appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID), decided.ToolSlug)
	if decided.Kind == KindConnect {
		note = fmt.Sprintf("The person declined to connect %s (request %s).", appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
	}
	if reason != "" {
		note += " Their reason: " + reason
	}
	note += " Do not retry the same call. Continue without it, or adjust your plan and explain in your reply."
	s.resume(ctx, decided, actorID, note)
	return decided, nil
}

func (s *Service) pendingForApprover(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID) (db.AppActionRequest, error) {
	req, err := s.q.GetAppActionRequest(ctx, db.GetAppActionRequestParams{ID: requestID, WorkspaceID: workspaceID})
	if err != nil {
		return db.AppActionRequest{}, ErrRequestNotFound
	}
	if req.ApproverUserID != actorID {
		return db.AppActionRequest{}, ErrNotApprover
	}
	if req.Status != "pending" {
		return db.AppActionRequest{}, ErrNotPending
	}
	return req, nil
}

func personalOwner(req db.AppActionRequest) pgtype.UUID {
	if req.AccountScope == ScopePersonal {
		return req.ApproverUserID
	}
	return pgtype.UUID{}
}

// executeApproved runs an approved request. The agent's app, the account and
// the rules are checked again: anything that changed while the request waited
// and now forbids the call fails the request instead of running it.
func (s *Service) executeApproved(ctx context.Context, req db.AppActionRequest) (db.AppActionRequest, string) {
	label := fmt.Sprintf("%s request %s (%s)", appLabel(req.ToolkitSlug), util.UUIDToString(req.ID), req.ToolSlug)
	call := toolCall{
		workspaceID: req.WorkspaceID, taskID: req.TaskID, agentID: req.AgentID,
		issueID: req.IssueID, chatSessionID: req.ChatSessionID, triggeredBy: req.ApproverUserID,
		acct:    account{scope: req.AccountScope, owner: personalOwner(req)},
		toolkit: req.ToolkitSlug, tool: req.ToolSlug, class: req.ActionClass, requestID: req.ID,
	}
	fail := func(msg string) (db.AppActionRequest, string) {
		call.outcome, call.err = "error", truncate(msg, 500)
		s.recordCall(ctx, call)
		row, err := s.q.SetAppActionRequestOutcome(ctx, db.SetAppActionRequestOutcomeParams{
			ID: req.ID, WorkspaceID: req.WorkspaceID, Status: "failed",
			Error: pgtype.Text{String: truncate(msg, 2000), Valid: true},
		})
		if err != nil {
			row = req
		}
		return row, fmt.Sprintf("Your %s was approved, but running it failed: %s. Decide how to continue; do not resubmit the same call unless the failure was transient.", label, msg)
	}

	app, ok := s.agentApp(ctx, req.AgentID, req.ToolkitSlug)
	if !ok {
		return fail("the app is no longer attached to the agent")
	}
	if !s.Catalog(ctx, req.WorkspaceID).Allows(req.ToolkitSlug) {
		return fail("the workspace no longer allows this app")
	}
	acct, err := s.resolveAccount(ctx, req.WorkspaceID, req.ApproverUserID, req.ToolkitSlug, req.AccountScope)
	if err != nil {
		return fail("the account is no longer connected")
	}
	call.acct = acct
	tool, err := s.composio.GetTool(ctx, req.ToolkitSlug, req.ToolSlug)
	if err != nil {
		return fail("the tool could not be loaded")
	}
	class := s.classify(ctx, req.WorkspaceID, req.ToolkitSlug, tool)
	rule, err := s.accountRule(ctx, req.WorkspaceID, req.ToolkitSlug, acct, class)
	if err != nil {
		return fail("the rules could not be read")
	}
	if Decide(req.ToolkitSlug, app.Level, class, rule) == PolicyBlock {
		return fail("a rule now blocks this call")
	}
	var args map[string]any
	if err := json.Unmarshal(req.ApprovedArguments, &args); err != nil {
		return fail("the approved arguments are invalid")
	}
	resp, err := s.composio.ExecuteTool(ctx, acct.connectedAccountID, *tool, args)
	s.touch(ctx, req.WorkspaceID, acct)
	if err != nil {
		return fail(truncate(err.Error(), 500))
	}
	if !resp.Successful {
		return fail(truncate(resp.Error, 500))
	}
	result, err := json.Marshal(resp.Data)
	if err != nil || len(result) > maxResultBytes {
		result = []byte(`{"truncated":true}`)
	}
	call.outcome = "approved"
	s.recordCall(ctx, call)
	row, err := s.q.SetAppActionRequestOutcome(ctx, db.SetAppActionRequestOutcomeParams{
		ID: req.ID, WorkspaceID: req.WorkspaceID, Status: "approved", Result: result,
	})
	if err != nil {
		row = req
	}
	edited := ""
	if string(req.ApprovedArguments) != string(req.Arguments) {
		edited = " The person edited the arguments before approving; the result reflects their version."
	}
	return row, fmt.Sprintf("Your %s was approved and Multica ran it.%s Result:\n```json\n%s\n```\nContinue the task from here. Do not call the tool again for this request.", label, edited, string(result))
}

func (s *Service) agentApp(ctx context.Context, agentID pgtype.UUID, slug string) (db.AgentApp, bool) {
	apps, err := s.q.ListAgentApps(ctx, agentID)
	if err != nil {
		return db.AgentApp{}, false
	}
	for _, app := range apps {
		if NormalizeSlug(app.ToolkitSlug) == NormalizeSlug(slug) {
			return app, true
		}
	}
	return db.AgentApp{}, false
}

func (s *Service) resume(ctx context.Context, req db.AppActionRequest, actorID pgtype.UUID, note string) {
	if s.resumer == nil {
		return
	}
	if err := s.resumer.ResumeAfterDecision(ctx, req, actorID, note); err != nil {
		slog.Warn("connectedapps: resume agent failed", "request_id", util.UUIDToString(req.ID), "error", err)
	}
}

// CancelTeamRequests cancels what was waiting on a team account that was
// just disconnected. The agents are not resumed.
func (s *Service) CancelTeamRequests(ctx context.Context, workspaceID pgtype.UUID, slug string) {
	rows, err := s.q.CancelPendingTeamAppActionRequests(ctx, db.CancelPendingTeamAppActionRequestsParams{WorkspaceID: workspaceID, ToolkitSlug: slug})
	if err != nil {
		slog.Warn("connectedapps: cancel team requests failed", "toolkit", slug, "error", err)
		return
	}
	for _, r := range rows {
		s.publishChange(r)
	}
}

// Sweep expires requests past their deadline and reminds approvers two hours
// before. Expiry does not resume the agent: the run already ended, and
// nothing happened. The approver gets a result notification instead.
func (s *Service) Sweep(ctx context.Context) {
	expired, err := s.q.ExpireDueAppActionRequests(ctx)
	if err != nil {
		slog.Warn("connectedapps: expire requests failed", "error", err)
	}
	for _, r := range expired {
		s.publishChange(r)
		s.notify(ctx, r, NotifActionResult)
	}
	due, err := s.q.ClaimAppActionRequestReminders(ctx)
	if err != nil {
		slog.Warn("connectedapps: claim reminders failed", "error", err)
	}
	for _, r := range due {
		s.notify(ctx, r, NotifActionReminder)
	}
}

func (s *Service) publishChange(req db.AppActionRequest) {
	if s.bus == nil {
		return
	}
	e := events.Event{
		Type:        EventRequestChanged,
		WorkspaceID: util.UUIDToString(req.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"request_id":      util.UUIDToString(req.ID),
			"issue_id":        util.UUIDToPtr(req.IssueID),
			"chat_session_id": util.UUIDToPtr(req.ChatSessionID),
			"status":          req.Status,
		},
	}
	if !req.IssueID.Valid {
		e.MemberPrivateUserID = util.UUIDToString(req.ApproverUserID)
	}
	s.bus.Publish(e)
}

func appLabel(slug string) string {
	if name := runtimeapps.DisplayNameForToolkitSlug(slug); name != "" {
		return name
	}
	return slug
}

// notify writes the approver's inbox item. Confirmation and connection
// requests are always delivered: the agent is stopped until the person
// answers. Results follow the recipient's agent-activity preference.
func (s *Service) notify(ctx context.Context, req db.AppActionRequest, notifType string) {
	if notifType == NotifActionResult && s.mutedAgentActivity(ctx, req) {
		return
	}
	actorType := pgtype.Text{String: "agent", Valid: true}
	agentName := "Agent"
	if agent, err := s.q.GetAgent(ctx, req.AgentID); err == nil {
		agentName = agent.Name
	}
	title, issueStatus := agentName, ""
	if req.IssueID.Valid {
		if issue, err := s.q.GetIssue(ctx, req.IssueID); err == nil {
			title, issueStatus = issue.Title, issue.Status
		}
	} else if req.ChatSessionID.Valid {
		if chat, err := s.q.GetChatSession(ctx, req.ChatSessionID); err == nil && chat.Title != "" {
			title = chat.Title
		}
	}
	var body string
	severity := "action_required"
	switch notifType {
	case NotifActionResult:
		severity = "info"
		switch {
		case req.Status == "blocked":
			body = fmt.Sprintf("A rule blocked %s from running %s · %s", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
		case req.Kind == KindConnect:
			body = fmt.Sprintf("%s's request to connect %s expired without an answer", agentName, appLabel(req.ToolkitSlug))
		default:
			body = fmt.Sprintf("%s's request to run %s · %s expired without an answer", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
		}
	case NotifConnectRequested:
		body = fmt.Sprintf("%s asks you to connect %s", agentName, appLabel(req.ToolkitSlug))
	case NotifActionReminder:
		body = fmt.Sprintf("%s is still waiting for your confirmation on %s · %s; it expires in 2 hours", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
	default:
		body = fmt.Sprintf("%s wants to run %s · %s", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
	}
	details, _ := json.Marshal(map[string]any{
		"request_id":      util.UUIDToPtr(req.ID),
		"task_id":         util.UUIDToPtr(req.TaskID),
		"status":          req.Status,
		"kind":            req.Kind,
		"toolkit_slug":    req.ToolkitSlug,
		"tool_slug":       req.ToolSlug,
		"action_class":    req.ActionClass,
		"agent_id":        util.UUIDToString(req.AgentID),
		"chat_session_id": util.UUIDToPtr(req.ChatSessionID),
		"summary":         req.Summary,
		"expires_at":      util.TimestampToString(req.ExpiresAt),
	})
	item, inserted, err := inbox.CreateIfEligible(ctx, s.q, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   req.WorkspaceID,
		RecipientType: "member",
		RecipientID:   req.ApproverUserID,
		Type:          notifType,
		Severity:      severity,
		IssueID:       req.IssueID,
		Title:         title,
		Body:          pgtype.Text{String: body, Valid: true},
		ActorType:     actorType,
		ActorID:       req.AgentID,
		Details:       details,
	})
	if err != nil {
		slog.Warn("connectedapps: create inbox item failed", "request_id", util.UUIDToString(req.ID), "error", err)
		return
	}
	if !inserted || s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{
		Type: "inbox:new", WorkspaceID: util.UUIDToString(item.WorkspaceID), ActorType: "agent", ActorID: util.UUIDToString(req.AgentID),
		Payload: map[string]any{"item": map[string]any{
			"id": util.UUIDToString(item.ID), "workspace_id": util.UUIDToString(item.WorkspaceID),
			"recipient_type": item.RecipientType, "recipient_id": util.UUIDToString(item.RecipientID),
			"type": item.Type, "severity": item.Severity, "issue_id": util.UUIDToPtr(item.IssueID),
			"title": item.Title, "body": util.TextToPtr(item.Body), "read": item.Read, "archived": item.Archived,
			"created_at": util.TimestampToString(item.CreatedAt), "actor_type": util.TextToPtr(item.ActorType),
			"actor_id": util.UUIDToPtr(item.ActorID), "details": json.RawMessage(item.Details), "issue_status": issueStatus,
		}},
	})
}

// mutedAgentActivity reports whether the recipient muted the agent-activity
// group, which app results belong to.
func (s *Service) mutedAgentActivity(ctx context.Context, req db.AppActionRequest) bool {
	pref, err := s.q.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: req.WorkspaceID, UserID: req.ApproverUserID,
	})
	if err != nil {
		return false
	}
	prefs := map[string]string{}
	_ = json.Unmarshal(pref.Preferences, &prefs)
	return prefs["agent_activity"] == "muted"
}

// RunSweeper runs Sweep every minute until ctx ends. Safe on every replica:
// both statements claim rows with SKIP LOCKED.
func (s *Service) RunSweeper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
