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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
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
	switch {
	case isTeamConnect(req):
		s.notifyAdmins(ctx, req)
	case kind == KindConnect:
		s.notify(ctx, req, NotifConnectRequested)
	default:
		s.notify(ctx, req, NotifActionRequested)
	}
	s.publishChange(req)
	return req, nil
}

// isTeamConnect reports a request for the workspace's team account. Any
// admin may answer it, not only its approver, who is the person waiting.
func isTeamConnect(req db.AppActionRequest) bool {
	return req.Kind == KindConnect && req.AccountScope == ScopeTeam
}

// AskTeamAccount records a member asking the admins for a team account for
// slug, outside any run. Asking again while the ask is open changes nothing.
func (s *Service) AskTeamAccount(ctx context.Context, workspaceID, memberID pgtype.UUID, slug string, agentID pgtype.UUID) (db.AppActionRequest, error) {
	slug = NormalizeSlug(slug)
	if existing, err := s.q.FindPendingTeamAsk(ctx, db.FindPendingTeamAskParams{
		WorkspaceID: workspaceID, ToolkitSlug: slug, ApproverUserID: memberID,
	}); err == nil {
		return existing, nil
	} else if !isNoRows(err) {
		return db.AppActionRequest{}, err
	}
	req, err := s.q.CreateAppActionRequest(ctx, db.CreateAppActionRequestParams{
		WorkspaceID:    workspaceID,
		Kind:           KindConnect,
		AgentID:        agentID,
		ApproverUserID: memberID,
		AccountScope:   ScopeTeam,
		ToolkitSlug:    slug,
		ActionClass:    ClassSend,
		Arguments:      []byte("{}"),
		ExpiresAt:      pgtype.Timestamptz{Time: s.now().Add(TeamAskTTL), Valid: true},
	})
	if err != nil {
		return db.AppActionRequest{}, err
	}
	s.notifyAdmins(ctx, req)
	s.publishChange(req)
	return req, nil
}

// WithdrawTeamAsk cancels the member's own open ask for slug's team account.
func (s *Service) WithdrawTeamAsk(ctx context.Context, workspaceID, memberID pgtype.UUID, slug string) error {
	req, err := s.q.FindPendingTeamAsk(ctx, db.FindPendingTeamAskParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug), ApproverUserID: memberID,
	})
	if isNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	s.withdraw(ctx, req)
	s.archiveAdminNotifications(ctx, req, pgtype.UUID{}, "app_request_withdrawn")
	return nil
}

// archiveAdminNotifications archives the admins' open notifications for a
// team account request nobody needs to answer any more, except keep's: the
// admin who decided it keeps theirs where they acted on it.
func (s *Service) archiveAdminNotifications(ctx context.Context, req db.AppActionRequest, keep pgtype.UUID, reason string) {
	rows, err := s.q.ArchiveAppRequestInbox(ctx, db.ArchiveAppRequestInboxParams{
		WorkspaceID: req.WorkspaceID, RequestID: util.UUIDToString(req.ID), KeepRecipientID: keep,
	})
	if err != nil {
		slog.Warn("connectedapps: archive team request notifications failed", "request_id", util.UUIDToString(req.ID), "error", err)
		return
	}
	if s.bus == nil {
		return
	}
	for _, row := range rows {
		if row.RecipientType != "member" {
			continue
		}
		s.bus.Publish(events.Event{
			Type:        protocol.EventInboxBatchArchived,
			WorkspaceID: util.UUIDToString(req.WorkspaceID),
			Payload: map[string]any{
				"recipient_id": util.UUIDToString(row.RecipientID),
				"count":        int64(1),
				"reason":       reason,
			},
		})
	}
}

// Approve executes an approved request with args (nil keeps the agent's) and
// resumes the agent with the outcome. Only the approver may approve.
func (s *Service) Approve(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID, args map[string]any) (db.AppActionRequest, error) {
	req, asAdmin, err := s.pendingForDecider(ctx, workspaceID, actorID, requestID)
	if err != nil {
		return db.AppActionRequest{}, err
	}
	if !s.mayAct(ctx, req) {
		s.withdraw(ctx, req)
		return db.AppActionRequest{}, ErrNoLongerAllowed
	}
	addsApp := false
	if req.Kind == KindConnect {
		if _, has := s.agentApp(ctx, req.AgentID, req.ToolkitSlug); req.TaskID.Valid && !has {
			if req.AccountScope != ScopePersonal || !s.mayManage(ctx, req.WorkspaceID, req.AgentID, actorID) ||
				!s.Catalog(ctx, workspaceID).Allows(req.ToolkitSlug) {
				s.withdraw(ctx, req)
				return db.AppActionRequest{}, ErrCannotAddApp
			}
			addsApp = true
		}
		if _, err := s.resolveAccount(ctx, workspaceID, req.ApproverUserID, req.ToolkitSlug, req.AccountScope); err != nil {
			return db.AppActionRequest{}, ErrAccountMissing
		}
	} else if acct, err := s.resolveAccount(ctx, workspaceID, actorID, req.ToolkitSlug, req.AccountScope); err == nil &&
		s.overLimit(ctx, workspaceID, req.ToolkitSlug, acct) {
		// Checked before deciding, so the request stays pending and retryable.
		return db.AppActionRequest{}, ErrRateLimited
	}
	approvedArgs := req.Arguments
	if args != nil && req.Kind == KindAction {
		if approvedArgs, err = json.Marshal(args); err != nil {
			return db.AppActionRequest{}, err
		}
	}
	decided, err := s.q.DecideAppActionRequest(ctx, db.DecideAppActionRequestParams{
		ID: requestID, WorkspaceID: workspaceID, Status: "approved", DecidedBy: actorID,
		ApprovedArguments: approvedArgs, AsAdmin: asAdmin,
	})
	if isNoRows(err) {
		return db.AppActionRequest{}, ErrNotPending
	}
	if err != nil {
		return db.AppActionRequest{}, err
	}
	if decided.Kind == KindConnect {
		if isTeamConnect(decided) {
			s.archiveAdminNotifications(ctx, decided, actorID, "app_request_decided")
		}
		if !decided.TaskID.Valid {
			s.publishChange(decided)
			s.notify(ctx, decided, NotifActionResult)
			return decided, nil
		}
		note := fmt.Sprintf("The %s account you asked for (request %s) is now connected. Continue the task and use the app.",
			appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
		if isTeamConnect(decided) {
			note = fmt.Sprintf("A workspace admin connected the team %s account you asked for (request %s). Continue the task and use the app.",
				appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
		}
		if addsApp {
			if err := s.addPersonalApp(ctx, decided, actorID); err != nil {
				slog.Warn("connectedapps: add app on approval failed", "request_id", util.UUIDToString(decided.ID), "error", err)
				if row, serr := s.q.SetAppActionRequestOutcome(ctx, db.SetAppActionRequestOutcomeParams{
					ID: decided.ID, WorkspaceID: decided.WorkspaceID, Status: "failed",
					Error: pgtype.Text{String: "the app could not be added to the agent", Valid: true},
				}); serr == nil {
					decided = row
				}
				s.publishChange(decided)
				s.resume(ctx, decided, actorID, fmt.Sprintf(
					"The person approved adding %s to you (request %s), but adding it failed. Continue without it and explain in your reply.",
					appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID)))
				return decided, nil
			}
			note = fmt.Sprintf("The person added %s to your apps and connected their account (request %s). Continue the task and use the app.",
				appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
		}
		s.publishChange(decided)
		s.resume(ctx, decided, actorID, note)
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
func (s *Service) ResolveConnectRequests(ctx context.Context, workspaceID, userID pgtype.UUID, slug string) {
	rows, err := s.q.ListPendingConnectRequestsForUser(ctx, db.ListPendingConnectRequestsForUserParams{
		WorkspaceID: workspaceID, ApproverUserID: userID, ToolkitSlug: NormalizeSlug(slug),
	})
	if err != nil {
		slog.Warn("connectedapps: list connect requests failed", "toolkit", slug, "error", err)
		return
	}
	for _, r := range rows {
		if _, err := s.Approve(ctx, r.WorkspaceID, userID, r.ID, nil); err != nil &&
			!errors.Is(err, ErrNotPending) && !errors.Is(err, ErrNoLongerAllowed) && !errors.Is(err, ErrCannotAddApp) {
			slog.Warn("connectedapps: resolve connect request failed", "request_id", util.UUIDToString(r.ID), "error", err)
		}
	}
}

// ResolveTeamConnectRequests answers everything waiting for the workspace's
// team account for slug once an admin connected it: agents resume, members
// who asked are told.
func (s *Service) ResolveTeamConnectRequests(ctx context.Context, workspaceID, adminID pgtype.UUID, slug string) {
	rows, err := s.q.ListPendingTeamConnectRequests(ctx, db.ListPendingTeamConnectRequestsParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug),
	})
	if err != nil {
		slog.Warn("connectedapps: list team connect requests failed", "toolkit", slug, "error", err)
		return
	}
	for _, r := range rows {
		if _, err := s.Approve(ctx, workspaceID, adminID, r.ID, nil); err != nil &&
			!errors.Is(err, ErrNotPending) && !errors.Is(err, ErrNoLongerAllowed) && !errors.Is(err, ErrCannotAddApp) {
			slog.Warn("connectedapps: resolve team connect request failed", "request_id", util.UUIDToString(r.ID), "error", err)
		}
	}
}

// DeclineTeamConnectRequests is an admin declining everything waiting for
// the team account for slug.
func (s *Service) DeclineTeamConnectRequests(ctx context.Context, workspaceID, adminID pgtype.UUID, slug string) error {
	rows, err := s.q.ListPendingTeamConnectRequests(ctx, db.ListPendingTeamConnectRequestsParams{
		WorkspaceID: workspaceID, ToolkitSlug: NormalizeSlug(slug),
	})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := s.Reject(ctx, workspaceID, adminID, r.ID, ""); err != nil && !errors.Is(err, ErrNotPending) {
			return err
		}
	}
	return nil
}

// Reject declines a request and resumes the agent so it can adapt.
func (s *Service) Reject(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID, reason string) (db.AppActionRequest, error) {
	_, asAdmin, err := s.pendingForDecider(ctx, workspaceID, actorID, requestID)
	if err != nil {
		return db.AppActionRequest{}, err
	}
	reason = truncate(strings.TrimSpace(reason), 1000)
	decided, err := s.q.DecideAppActionRequest(ctx, db.DecideAppActionRequestParams{
		ID: requestID, WorkspaceID: workspaceID, Status: "rejected", DecidedBy: actorID,
		Reason: pgtype.Text{String: reason, Valid: reason != ""}, AsAdmin: asAdmin,
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
	if isTeamConnect(decided) {
		s.archiveAdminNotifications(ctx, decided, actorID, "app_request_decided")
	}
	s.publishChange(decided)
	if !decided.TaskID.Valid {
		if decided.DecidedBy != decided.ApproverUserID {
			s.notify(ctx, decided, NotifActionResult)
		}
		return decided, nil
	}
	note := fmt.Sprintf("The person declined your %s request %s (%s).", appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID), decided.ToolSlug)
	switch {
	case isTeamConnect(decided) && decided.DecidedBy != decided.ApproverUserID:
		note = fmt.Sprintf("A workspace admin declined to connect a team %s account (request %s).", appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
	case decided.Kind == KindConnect:
		note = fmt.Sprintf("The person declined to connect %s (request %s).", appLabel(decided.ToolkitSlug), util.UUIDToString(decided.ID))
	}
	if reason != "" {
		note += " Their reason: " + reason
	}
	note += " Do not retry the same call. Continue without it, or adjust your plan and explain in your reply."
	s.resume(ctx, decided, actorID, note)
	return decided, nil
}

// pendingForDecider loads a pending request actorID may decide: its
// approver's, or, for a team account request, any admin's. asAdmin is set
// when the actor decides as an admin rather than as the approver.
func (s *Service) pendingForDecider(ctx context.Context, workspaceID, actorID, requestID pgtype.UUID) (req db.AppActionRequest, asAdmin bool, err error) {
	req, err = s.q.GetAppActionRequest(ctx, db.GetAppActionRequestParams{ID: requestID, WorkspaceID: workspaceID})
	if err != nil {
		return db.AppActionRequest{}, false, ErrRequestNotFound
	}
	if req.ApproverUserID != actorID {
		if !isTeamConnect(req) || !s.IsAdmin(ctx, workspaceID, actorID) {
			return db.AppActionRequest{}, false, ErrNotApprover
		}
		asAdmin = true
	}
	if req.Status != "pending" {
		return db.AppActionRequest{}, false, ErrNotPending
	}
	return req, asAdmin, nil
}

// IsAdmin reports whether userID owns or administers the workspace.
func (s *Service) IsAdmin(ctx context.Context, workspaceID, userID pgtype.UUID) bool {
	member, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: userID, WorkspaceID: workspaceID})
	return err == nil && (member.Role == "owner" || member.Role == "admin")
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
	failAs := func(outcome, msg string) (db.AppActionRequest, string) {
		call.outcome, call.err = outcome, truncate(msg, 500)
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
	fail := func(msg string) (db.AppActionRequest, string) { return failAs("error", msg) }

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
	var args map[string]any
	if err := json.Unmarshal(req.ApprovedArguments, &args); err != nil {
		return fail("the approved arguments are invalid")
	}
	class := ClassifyCall(s.classify(ctx, req.WorkspaceID, req.ToolkitSlug, tool), tool.Slug, args)
	rule, err := s.accountRule(ctx, req.WorkspaceID, req.ToolkitSlug, acct, class)
	if err != nil {
		return fail("the rules could not be read")
	}
	if Decide(req.ToolkitSlug, app.Level, class, rule) == PolicyBlock {
		return fail("a rule now blocks this call")
	}
	// Last: it counts the call, which runs right after.
	if s.quota != nil {
		countedIn, exceeded := s.quota.Admit(ctx, req.WorkspaceID)
		if exceeded != nil {
			return failAs("quota_exceeded", exceeded.Message())
		}
		call.meteredPeriod = countedIn
	}
	resp, err := s.composio.ExecuteTool(ctx, acct.connectedAccountID, acct.composioUserID, *tool, args)
	s.touch(ctx, req.WorkspaceID, acct)
	if err != nil {
		logExecutionFailure(ctx, call, "request_failed")
		return fail(truncate(err.Error(), 500))
	}
	if !resp.Successful {
		logExecutionFailure(ctx, call, "tool_unsuccessful")
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

// addPersonalApp attaches the requested app to the agent through the personal
// account of whoever triggers it, keeping the legacy allowlist in step. An
// app attached meanwhile keeps its account and level.
func (s *Service) addPersonalApp(ctx context.Context, req db.AppActionRequest, actorID pgtype.UUID) error {
	if s.tx == nil {
		return errors.New("connectedapps: no transaction starter configured")
	}
	agent, err := s.q.GetAgent(ctx, req.AgentID)
	if err != nil {
		return err
	}
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	apps, err := qtx.ListAgentApps(ctx, agent.ID)
	if err != nil {
		return err
	}
	slug := NormalizeSlug(req.ToolkitSlug)
	slugs := make([]string, 0, len(apps)+1)
	has := false
	for _, app := range apps {
		slugs = append(slugs, app.ToolkitSlug)
		has = has || NormalizeSlug(app.ToolkitSlug) == slug
	}
	if !has {
		if _, err := qtx.UpsertAgentApp(ctx, db.UpsertAgentAppParams{
			WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, ToolkitSlug: slug,
			AccountScope: ScopePersonal, Level: DefaultLevel(agent.PermissionMode), CreatedBy: actorID,
		}); err != nil {
			return err
		}
		slugs = append(slugs, slug)
	}
	if err := qtx.SetAgentComposioToolkitAllowlist(ctx, db.SetAgentComposioToolkitAllowlistParams{ID: agent.ID, Slugs: slugs}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) mayManage(ctx context.Context, workspaceID, agentID, userID pgtype.UUID) bool {
	return s.resumer != nil && s.resumer.CanManageAgent(ctx, workspaceID, agentID, userID)
}

// mayAct reports whether the approver is still a member who may run the
// agent. Membership and agent access can change while a request waits.
func (s *Service) mayAct(ctx context.Context, req db.AppActionRequest) bool {
	if !req.TaskID.Valid {
		_, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID: req.ApproverUserID, WorkspaceID: req.WorkspaceID,
		})
		return err == nil
	}
	return s.mayUseAgent(ctx, req.WorkspaceID, req.AgentID, req.ApproverUserID)
}

func (s *Service) mayUseAgent(ctx context.Context, workspaceID, agentID, userID pgtype.UUID) bool {
	if _, err := s.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: userID, WorkspaceID: workspaceID,
	}); err != nil {
		return false
	}
	return s.resumer == nil || s.resumer.CanActFor(ctx, workspaceID, agentID, userID)
}

func (s *Service) withdraw(ctx context.Context, req db.AppActionRequest) {
	row, err := s.q.CancelAppActionRequest(ctx, db.CancelAppActionRequestParams{ID: req.ID, WorkspaceID: req.WorkspaceID})
	if err != nil {
		return
	}
	s.publishChange(row)
}

func (s *Service) resume(ctx context.Context, req db.AppActionRequest, actorID pgtype.UUID, note string) {
	if s.resumer == nil {
		return
	}
	if !s.mayAct(ctx, req) {
		slog.Info("connectedapps: approver can no longer run the agent; not resuming", "request_id", util.UUIDToString(req.ID))
		return
	}
	// The run continues as the person who started it, whoever decided.
	if err := s.resumer.ResumeAfterDecision(ctx, req, req.ApproverUserID, note); err != nil {
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
	if app, ok := Curated(slug); ok {
		return app.Name
	}
	if name := runtimeapps.DisplayNameForToolkitSlug(slug); name != "" {
		return name
	}
	return slug
}

// notify writes the approver's inbox item. Confirmation and connection
// requests are always delivered: the agent is stopped until the person
// answers. Results follow the recipient's agent-activity preference; the
// inbox type registry owns both rules.
func (s *Service) notify(ctx context.Context, req db.AppActionRequest, notifType string) {
	s.deliver(ctx, req, notifType, req.ApproverUserID)
}

// notifyAdmins tells the admins a team account is wanted, but only the ones
// it concerns: the person who started the run when they are an admin, else
// the agent's owner when they are an admin, else every admin. An admin who
// already holds an open notification for this app is not told again: they
// answer every request for it at once.
func (s *Service) notifyAdmins(ctx context.Context, req db.AppActionRequest) {
	recipients := s.teamConnectRecipients(ctx, req)
	if len(recipients) == 0 {
		return
	}
	notified, err := s.q.ListTeamConnectNotifiedRecipients(ctx, db.ListTeamConnectNotifiedRecipientsParams{
		WorkspaceID: req.WorkspaceID, ToolkitSlug: req.ToolkitSlug, RecipientIds: recipients,
	})
	if err != nil {
		slog.Warn("connectedapps: list notified admins for team request failed", "toolkit", req.ToolkitSlug, "error", err)
	}
	told := make(map[pgtype.UUID]bool, len(notified))
	for _, id := range notified {
		told[id] = true
	}
	for _, admin := range recipients {
		if !told[admin] {
			s.deliver(ctx, req, NotifConnectRequested, admin)
		}
	}
}

// teamConnectRecipients picks who is asked to connect a team account. A
// member asking outside any run is never asked themselves.
func (s *Service) teamConnectRecipients(ctx context.Context, req db.AppActionRequest) []pgtype.UUID {
	eligible := func(id pgtype.UUID) bool {
		if !id.Valid || (id == req.ApproverUserID && !req.TaskID.Valid) {
			return false
		}
		return s.IsAdmin(ctx, req.WorkspaceID, id)
	}
	if req.TaskID.Valid && eligible(req.ApproverUserID) {
		return []pgtype.UUID{req.ApproverUserID}
	}
	if req.AgentID.Valid {
		if agent, err := s.q.GetAgent(ctx, req.AgentID); err == nil && eligible(agent.OwnerID) {
			return []pgtype.UUID{agent.OwnerID}
		}
	}
	admins, err := s.q.ListWorkspaceManagerUserIDs(ctx, req.WorkspaceID)
	if err != nil {
		slog.Warn("connectedapps: list admins for team request failed", "toolkit", req.ToolkitSlug, "error", err)
		return nil
	}
	out := make([]pgtype.UUID, 0, len(admins))
	for _, admin := range admins {
		if admin != req.ApproverUserID || req.TaskID.Valid {
			out = append(out, admin)
		}
	}
	return out
}

func (s *Service) deliver(ctx context.Context, req db.AppActionRequest, notifType string, recipient pgtype.UUID) {
	actorType := pgtype.Text{String: "agent", Valid: true}
	actorID := req.AgentID
	agentName := "Agent"
	if agent, err := s.q.GetAgent(ctx, req.AgentID); err == nil {
		agentName = agent.Name
	}
	if !req.TaskID.Valid {
		actorType, actorID, agentName = pgtype.Text{String: "member", Valid: true}, req.ApproverUserID, "A member"
		if user, err := s.q.GetUser(ctx, req.ApproverUserID); err == nil && user.Name != "" {
			agentName = user.Name
		}
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
	} else if !req.TaskID.Valid {
		title = appLabel(req.ToolkitSlug)
	}
	var body string
	severity := "action_required"
	switch notifType {
	case NotifActionResult:
		severity = "info"
		switch {
		case !req.TaskID.Valid && req.Status == "approved":
			body = fmt.Sprintf("An admin connected the team %s account you asked for", appLabel(req.ToolkitSlug))
		case !req.TaskID.Valid && req.Status == "rejected":
			body = fmt.Sprintf("An admin declined your request for a team %s account", appLabel(req.ToolkitSlug))
		case !req.TaskID.Valid:
			body = fmt.Sprintf("Your request for a team %s account expired without an answer", appLabel(req.ToolkitSlug))
		case req.Status == "blocked":
			body = fmt.Sprintf("A rule blocked %s from running %s · %s", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
		case req.Kind == KindConnect:
			body = fmt.Sprintf("%s's request to connect %s expired without an answer", agentName, appLabel(req.ToolkitSlug))
		default:
			body = fmt.Sprintf("%s's request to run %s · %s expired without an answer", agentName, appLabel(req.ToolkitSlug), req.ToolSlug)
		}
	case NotifConnectRequested:
		body = fmt.Sprintf("%s asks you to connect %s", agentName, appLabel(req.ToolkitSlug))
		if isTeamConnect(req) {
			body = fmt.Sprintf("%s asks for a team %s account", agentName, appLabel(req.ToolkitSlug))
		} else if _, has := s.agentApp(ctx, req.AgentID, req.ToolkitSlug); !has {
			body = fmt.Sprintf("%s asks you to add %s and connect your account", agentName, appLabel(req.ToolkitSlug))
		}
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
		"agent_id":        util.UUIDToPtr(req.AgentID),
		"account_scope":   req.AccountScope,
		"chat_session_id": util.UUIDToPtr(req.ChatSessionID),
		"summary":         req.Summary,
		"expires_at":      util.TimestampToString(req.ExpiresAt),
	})
	_, _, err := inbox.Deliver(ctx, s.q, s.bus, inbox.Notification{
		Params: db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   req.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipient,
			Type:          notifType,
			Severity:      severity,
			IssueID:       req.IssueID,
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     actorType,
			ActorID:       actorID,
			Details:       details,
		},
		IssueStatus: &issueStatus,
	})
	if err != nil {
		slog.Warn("connectedapps: create inbox item failed", "request_id", util.UUIDToString(req.ID), "error", err)
	}
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
