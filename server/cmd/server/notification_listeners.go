package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mention represents a parsed @mention from markdown content (local alias).
type mention struct {
	Type string // "member", "agent", "issue", or "all"
	ID   string // user_id, agent_id, issue_id, or "all"
}

var emptyDetails = []byte("{}")

// parseMentions extracts mentions from markdown content.
// Delegates to the shared util.ParseMentions and converts to the local type.
func parseMentions(content string) []mention {
	parsed := util.ParseMentions(content)
	result := make([]mention, len(parsed))
	for i, m := range parsed {
		result[i] = mention{Type: m.Type, ID: m.ID}
	}
	return result
}

// parentBubbleNotifTypes is the allowlist of inbox notification types that
// bubble up from a sub-issue to subscribers of its parent. Other event types
// only notify subscribers of the sub-issue itself, to keep parent watchers'
// inboxes focused on the signal that matters most: status transitions.
var parentBubbleNotifTypes = map[string]bool{
	"status_changed": true,
}

// delegatedAlwaysNotifTypes are the events a DELEGATED subscriber (reason=
// 'delegated' — an agent created this issue on their behalf, MUL-5483) receives
// unconditionally: they are either addressed at the human directly, or they are
// exceptions that stall the work until a human looks.
var delegatedAlwaysNotifTypes = map[string]bool{
	"mentioned":     true,
	"task_failed":   true,
	"agent_blocked": true,
}

// issueStatusIsHandoff reports whether ARRIVING on this status hands the issue
// back to a human. It is the one predicate behind both the delegated-subscriber
// tier and the stale task_failed dismissal, which used to be two hand-kept
// allowlists that drifted apart (MUL-7379).
//
// It decides on LIFECYCLE plus one exact key, the split MUL-7364 established:
//
//   - done / closed are terminal, so arriving there always ends someone's wait;
//   - started is a handoff EXCEPT on the fixed in_progress key. in_progress is
//     the one status the platform itself writes to mean "an agent is working"
//     — the brief tells agents to write it, and failure recovery, the sweeper
//     and the webhook repair all key off it — so arriving there is routine
//     forward progress, not a handoff;
//   - unstarted is never a handoff: nobody is waiting on queued or parked work.
//
// That makes a CUSTOM started status a handoff. This is deliberate. A workspace
// does not mint "Awaiting Response" or "Code Review" to describe an agent
// typing; it mints one to mark the point where a person has to look. Before
// MUL-7240 those statuses carried the in_review/blocked category and notified
// for that reason; collapsing to four categories silently stopped them, which
// is the regression this restores. A custom status that really is an
// in_progress synonym now notifies where it did not — an accepted cost, since a
// missed handoff strands the issue while a spare inbox row costs one dismissal.
//
// Triage never reaches this predicate: it is not a status but a column of its
// own (MUL-7213), so an entry waiting in Triage carries whatever status the
// triager has proposed and is answered on that. Suppressing notifications for
// Triage entries is a separate rule and belongs to MUL-7219.
//
// The error is preserved rather than swallowed so each caller can choose its
// own failure direction; built-ins answer without touching the catalog, so only
// custom statuses can fail here at all.
func issueStatusIsHandoff(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
	status string,
) (bool, error) {
	handoff, _, err := issueStatusHandoff(ctx, queries, workspaceID, status)
	return handoff, err
}

// issueStatusHandoff answers issueStatusIsHandoff and, with the same catalog
// read, whether the status WAITS on a person: a started status other than
// in_progress (in review, blocked, a custom gate). done / closed are a handoff
// that waits on no one, which is what separates the two answers. The Inbox
// files a status change under "Needs you" only when it waits (MUL-9215).
func issueStatusHandoff(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
	status string,
) (handoff, waitsOnHuman bool, err error) {
	category, err := issuestatus.CategoryWithError(ctx, queries, workspaceID, status)
	if err != nil {
		return false, false, err
	}
	switch category {
	case issuestatus.CategoryDone, issuestatus.CategoryClosed:
		return true, false, nil
	case issuestatus.CategoryStarted:
		waits := status != issuestatus.InProgress
		return waits, waits, nil
	default:
		return false, false, nil
	}
}

// deliverToSubscriber reports whether a subscriber row should receive this
// notification type. Direct subscriptions (creator / assignee / commenter /
// mentioned / manual / autopilot) are unchanged — they opted in to this issue,
// explicitly or by acting on it. Only the delegated tier is narrowed, and only
// to drop churn.
//
// A child finishing is NOT churn: "sub-issue X is ready for review" is exactly
// the signal a delegated watcher needs, and there is one per piece of real work.
// An earlier cut suppressed those and synthesized a single "the whole batch
// finished" roll-up from sibling state instead. That was both the wrong shape
// (see the MUL-5483 thread) and redundant: the human is subscribed to the PARENT
// as well, so when the agent moves the parent to in_review/done that transition
// delivers here — which is the natural "the tree is done" signal. Deriving it
// from children was reinventing a notification the platform already sends.
//
// statusIsHandoff is the resolved issueStatusIsHandoff answer for the issue's
// current status, so the catalog is read once per notification rather than once
// per subscriber row.
func deliverToSubscriber(reason, notifType string, statusIsHandoff bool) bool {
	if reason != "delegated" {
		return true
	}
	if delegatedAlwaysNotifTypes[notifType] {
		return true
	}
	if notifType != "status_changed" {
		return false
	}
	return statusIsHandoff
}

// notifTypeToGroup maps each InboxItemType to the preference group that can
// switch it off. Types not in this map are always delivered (not configurable).
//
// A group only ever silences updates: see isNotifMuted. Mentions are not here,
// since their one choice is where a group mention is filed
// (groupMentionsAreUpdates).
var notifTypeToGroup = map[string]string{
	"issue_assigned":     "assignments",
	"unassigned":         "assignments",
	"assignee_changed":   "assignments",
	"status_changed":     "status_changes",
	"new_comment":        "comments",
	"priority_changed":   "updates",
	"start_date_changed": "updates",
	"due_date_changed":   "updates",
	"task_completed":     "agent_activity",
	"task_failed":        "agent_activity",
	"agent_blocked":      "agent_activity",
	"agent_completed":    "agent_activity",
	"issue_created":      "new_issues",
}

// isNotifMuted returns true if the user switched this notification off, based
// on their parsed preferences map.
//
// Only an update can be switched off (MUL-9215). A row filed under "Needs you"
// is addressed to the user: a mention, an assignment, a run they started, or
// activity on an issue they created or are assigned. Dropping one would leave
// the issue waiting on them with nothing in their Inbox to say so.
func isNotifMuted(prefs map[string]string, notifType, section string) bool {
	if section != inbox.SectionUpdates {
		return false
	}
	group, ok := notifTypeToGroup[notifType]
	if !ok {
		return false // unconfigurable types are always delivered
	}
	return prefs[group] == "muted"
}

// groupMentionsAreUpdates reports whether the user asked for @all and @squad
// mentions to be filed as updates instead of under "Needs you". A mention by
// name is always addressed to them.
//
// The choice is stored under the `mentions` key, which used to switch every
// mention off. Whoever muted it keeps the quieter Inbox they asked for, and
// gets the mentions by name back.
func groupMentionsAreUpdates(prefs map[string]string) bool {
	return prefs["mentions"] == "muted"
}

// loadUserPrefs loads notification preferences for a set of user IDs in a
// workspace. Returns a map from user_id string to parsed preferences.
func loadUserPrefs(
	ctx context.Context,
	queries *db.Queries,
	workspaceID string,
	userIDs []string,
) map[string]map[string]string {
	if len(userIDs) == 0 {
		return nil
	}

	uuids := make([]pgtype.UUID, len(userIDs))
	for i, id := range userIDs {
		uuids[i] = parseUUID(id)
	}

	rows, err := queries.ListNotificationPreferencesByUsers(ctx, db.ListNotificationPreferencesByUsersParams{
		WorkspaceID: parseUUID(workspaceID),
		UserIds:     uuids,
	})
	if err != nil {
		slog.Error("failed to load notification preferences", "error", err)
		return nil
	}

	result := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		var prefs map[string]string
		if err := json.Unmarshal(row.Preferences, &prefs); err != nil {
			continue
		}
		result[util.UUIDToString(row.UserID)] = prefs
	}
	return result
}

// archiveStaleTaskFailedInbox archives all task_failed inbox rows for the
// given issue and notifies each affected member recipient via
// inbox:batch-archived so connected clients self-heal.
func archiveStaleTaskFailedInbox(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	workspaceID string,
	issueID string,
) {
	rows, err := queries.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
		Type:        "task_failed",
	})
	if err != nil {
		slog.Error("auto-archive task_failed inbox: query failed",
			"workspace_id", workspaceID, "issue_id", issueID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Dedupe recipients: the listener creates one row per failure event per
	// subscriber, so a long-running issue can yield several rows for the
	// same recipient.
	counts := map[string]int{}
	for _, row := range rows {
		// Inbox rows for task_failed only target member recipients today
		// (notifySubscribers skips agent subscribers), but defend the WS
		// layer against future widening — only members get a personal feed.
		if row.RecipientType != "member" {
			continue
		}
		counts[util.UUIDToString(row.RecipientID)]++
	}

	for recipientID, count := range counts {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxBatchArchived,
			WorkspaceID: workspaceID,
			Payload: map[string]any{
				"recipient_id": recipientID,
				"count":        int64(count),
				"issue_id":     issueID,
				"reason":       "issue_status_terminal",
			},
		})
	}

	slog.Info("auto-archive task_failed inbox: archived stale rows",
		"workspace_id", workspaceID, "issue_id", issueID,
		"row_count", len(rows), "recipient_count", len(counts))
}

// publishInboxArchived tells each member whose rows were just archived, so
// connected clients drop the issue without a reload.
func publishInboxArchived(bus *events.Bus, workspaceID, issueID, reason string, counts map[string]int64) {
	for recipientID, count := range counts {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxBatchArchived,
			WorkspaceID: workspaceID,
			Payload: map[string]any{
				"recipient_id": recipientID,
				"count":        count,
				"issue_id":     issueID,
				"reason":       reason,
			},
		})
	}
}

// memberStartedRun reports the member whose own action started this run, the
// one the Inbox lists it under "Working" for. Runs nobody started by hand
// (delegation, a reaction to an agent's comment, an autopilot) have none.
func memberStartedRun(task db.AgentTaskQueue) (string, bool) {
	if !task.OriginatorUserID.Valid || task.OriginatorSource.String != string(attribution.SourceDirectHuman) {
		return "", false
	}
	return util.UUIDToString(task.OriginatorUserID), true
}

// archiveInboxOnRunStart takes an issue out of a member's "Needs you" when
// their own action starts an agent run on it: the issue is with the agent now.
// The Inbox lists it under "Working" from the run itself, and it comes back
// through notifyRunEnded or the task:failed listener when the run ends. Runs
// the member did not start themselves leave the Inbox alone, and so does the
// retry of an earlier attempt, which is the same hand-over continuing.
func archiveInboxOnRunStart(ctx context.Context, queries *db.Queries, bus *events.Bus, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	issueID, _ := payload["issue_id"].(string)
	taskID, _ := payload["task_id"].(string)
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return
	}
	task, err := queries.GetAgentTask(ctx, taskUUID)
	if err != nil {
		slog.Error("archive handed-over inbox: failed to get task", "task_id", taskID, "error", err)
		return
	}
	originator, ok := memberStartedRun(task)
	if !ok || task.RetryOfTaskID.Valid {
		return
	}
	count, err := queries.ArchiveInboxByIssue(ctx, db.ArchiveInboxByIssueParams{
		WorkspaceID:   parseUUID(e.WorkspaceID),
		RecipientType: "member",
		RecipientID:   task.OriginatorUserID,
		IssueID:       issueUUID,
	})
	if err != nil {
		slog.Error("archive handed-over inbox: query failed",
			"workspace_id", e.WorkspaceID, "issue_id", issueID, "member_id", originator, "error", err)
		return
	}
	if count == 0 {
		return
	}
	publishInboxArchived(bus, e.WorkspaceID, issueID, "handed_to_agent", map[string]int64{originator: count})
}

// archiveInboxForClosedIssue takes an issue out of every Inbox once it reaches
// a done or cancelled status, whoever moved it there: nothing on it waits on
// anyone. The caller records the status change after this, so followers still
// get the one update saying the issue closed.
//
// FAIL CLOSED: an unresolved custom status keeps the rows, since hiding what
// waits on someone is worse than leaving it listed.
func archiveInboxForClosedIssue(ctx context.Context, queries *db.Queries, bus *events.Bus, workspaceID, issueID, status string) {
	category, err := issuestatus.CategoryWithError(ctx, queries, parseUUID(workspaceID), status)
	if err != nil {
		slog.Warn("resolve issue status category for inbox archive; keeping rows",
			"issue_id", issueID, "status", status, "error", err)
		return
	}
	if category != issuestatus.CategoryDone && category != issuestatus.CategoryClosed {
		return
	}
	rows, err := queries.ArchiveInboxByIssueForAll(ctx, db.ArchiveInboxByIssueForAllParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		slog.Error("archive closed-issue inbox: query failed",
			"workspace_id", workspaceID, "issue_id", issueID, "error", err)
		return
	}
	counts := map[string]int64{}
	for _, row := range rows {
		if row.RecipientType != "member" {
			continue
		}
		counts[util.UUIDToString(row.RecipientID)]++
	}
	publishInboxArchived(bus, workspaceID, issueID, "issue_closed", counts)
}

// runDetails names the run an inbox row reports.
func runDetails(taskID string) []byte {
	if taskID == "" {
		return emptyDetails
	}
	details, _ := json.Marshal(map[string]string{"task_id": taskID})
	return details
}

// notifyRunEnded hands an issue back to the member whose own action started
// the run, when the run completes: the row lands in their "Needs you" whether
// or not the agent mentioned them. Runs nobody started by hand leave people
// alone; whoever answers for the issue hears through the agent's comment or
// the status it sets.
func notifyRunEnded(ctx context.Context, queries *db.Queries, bus *events.Bus, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	issueID, _ := payload["issue_id"].(string)
	taskID, _ := payload["task_id"].(string)
	agentID, _ := payload["agent_id"].(string)
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return
	}
	task, err := queries.GetAgentTask(ctx, taskUUID)
	if err != nil {
		slog.Error("run ended notification: failed to get task", "task_id", taskID, "error", err)
		return
	}
	originator, ok := memberStartedRun(task)
	if !ok {
		return
	}
	issue, err := queries.GetIssue(ctx, issueUUID)
	if err != nil {
		slog.Error("run ended notification: failed to get issue", "issue_id", issueID, "error", err)
		return
	}
	// A run that closed the issue has nothing to hand back: the status change
	// already told everyone. An unresolved custom status still hands back,
	// since a spare row costs one archive and a missing one strands the issue.
	if category, err := issuestatus.CategoryWithError(ctx, queries, issue.WorkspaceID, issue.Status); err == nil &&
		(category == issuestatus.CategoryDone || category == issuestatus.CategoryClosed) {
		return
	}
	notifyDirect(ctx, queries, bus,
		"member", originator,
		e.WorkspaceID,
		events.Event{Type: e.Type, WorkspaceID: e.WorkspaceID, ActorType: "agent", ActorID: agentID},
		issueID, issue.Status,
		"agent_completed", "action_required",
		issue.Title, "",
		runDetails(taskID),
	)
}

// notifyRunCancelled hands an issue back to the member whose own action
// started a run that was then cancelled. Without it the issue leaves "Working"
// and appears nowhere: the run's start archived what the member had on it, and
// a cancelled run reports nothing else.
//
// The row is read when the member stopped the run themselves, since it tells
// them nothing new, and unread when someone else or the system did. It names
// who in its details.
//
// Nothing is handed back when the issue is not coming back to the member: it
// is being deleted, it is closed, or another run they started on it is still
// active. A rerun or an edited comment cancels the pending run before queueing
// its replacement, so the row is written and the replacement's task:queued
// archives it again (archiveInboxOnRunStart).
func notifyRunCancelled(ctx context.Context, queries *db.Queries, bus *events.Bus, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	// A replay re-announces a run that was already handed back, and a deleted
	// issue takes its Inbox rows with it.
	if replay, _ := payload["replay"].(bool); replay {
		return
	}
	if deleted, _ := payload["issue_deleted"].(bool); deleted {
		return
	}
	issueID, _ := payload["issue_id"].(string)
	taskID, _ := payload["task_id"].(string)
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return
	}
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return
	}
	task, err := queries.GetAgentTask(ctx, taskUUID)
	if err != nil {
		slog.Error("run cancelled notification: failed to get task", "task_id", taskID, "error", err)
		return
	}
	originator, ok := memberStartedRun(task)
	if !ok {
		return
	}
	issue, err := queries.GetIssue(ctx, issueUUID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("run cancelled notification: failed to get issue", "issue_id", issueID, "error", err)
		}
		return
	}
	// Same rule as notifyRunEnded: an unresolved custom status still hands back.
	if category, err := issuestatus.CategoryWithError(ctx, queries, issue.WorkspaceID, issue.Status); err == nil &&
		(category == issuestatus.CategoryDone || category == issuestatus.CategoryClosed) {
		return
	}
	stillWorking, err := queries.HasOtherActiveRunStartedByMember(ctx, db.HasOtherActiveRunStartedByMemberParams{
		IssueID:          issueUUID,
		TaskID:           taskUUID,
		OriginatorUserID: task.OriginatorUserID,
	})
	if err != nil {
		slog.Error("run cancelled notification: failed to check active runs", "issue_id", issueID, "error", err)
		return
	}
	if stillWorking {
		return
	}

	details := map[string]string{"task_id": taskID}
	cancelledByID := ""
	if task.CancelledByType.Valid {
		details["cancelled_by_type"] = task.CancelledByType.String
	}
	if task.CancelledByID.Valid {
		cancelledByID = util.UUIDToString(task.CancelledByID)
		details["cancelled_by_id"] = cancelledByID
	}
	if task.CancelledByName.Valid {
		details["cancelled_by_name"] = task.CancelledByName.String
	}
	detailsJSON, _ := json.Marshal(details)
	stoppedByMember := task.CancelledByType.String == "member" && cancelledByID == originator

	agentID := util.UUIDToString(task.AgentID)
	item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   parseUUID(e.WorkspaceID),
		RecipientType: "member",
		RecipientID:   task.OriginatorUserID,
		Type:          "task_cancelled",
		Severity:      "info",
		IssueID:       issueUUID,
		Title:         issue.Title,
		ActorType:     util.StrToText("agent"),
		ActorID:       optionalUUID(agentID),
		Details:       detailsJSON,
		Read:          pgtype.Bool{Bool: stoppedByMember, Valid: true},
	})
	if err != nil {
		slog.Error("run cancelled notification: creation failed",
			"issue_id", issueID, "recipient_id", originator, "error", err)
		return
	}
	if !inserted {
		return
	}
	resp := inboxItemToResponse(item)
	resp["issue_status"] = issue.Status
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: e.WorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload:     map[string]any{"item": resp},
	})
}

// memberAnswersForIssue reports whether the member created the issue or has it
// assigned: the two relations that make what happens on it theirs to handle.
func memberAnswersForIssue(issue db.Issue, memberID string) bool {
	if issue.CreatorType == "member" && util.UUIDToString(issue.CreatorID) == memberID {
		return true
	}
	return issue.AssigneeType.String == "member" && issue.AssigneeID.Valid &&
		util.UUIDToString(issue.AssigneeID) == memberID
}

// notifySubscribers queries the subscriber table for an issue, excludes the
// actor and any extra IDs, and creates inbox items for each remaining member
// subscriber. Publishes an inbox:new event for each notification.
// If the issue has a parent and the notification type is in the bubble
// allowlist, parent issue subscribers are also notified (deduplicated
// against direct subscribers).
func notifySubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	notified := notifyIssueAndParentSubscribers(ctx, queries, bus, issueID, issueStatus,
		workspaceID, e, exclude, notifType, severity, title, body, details)

	// Project watchers hear about every issue in the project, after the
	// issue-level fanout so nobody receives the same event twice.
	projectExclude := make(map[string]bool, len(exclude)+len(notified))
	for id := range exclude {
		projectExclude[id] = true
	}
	for id := range notified {
		projectExclude[id] = true
	}
	notifyProjectSubscribers(ctx, queries, bus, issueID, issueStatus, workspaceID, e,
		projectExclude, notifType, severity, title, body, details)
}

// notifyIssueAndParentSubscribers is the issue-level half of notifySubscribers:
// the issue's own subscribers, then — for bubbling types — its parent's.
// Returns every member it notified.
func notifyIssueAndParentSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) map[string]bool {
	notified, tierSuppressed := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details)

	// Only a small allowlist of event types bubbles to parent subscribers.
	if !parentBubbleNotifTypes[notifType] {
		return notified
	}

	// Also notify parent issue subscribers if this is a sub-issue.
	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("failed to get issue for parent notification",
			"issue_id", issueID, "error", err)
		return notified
	}
	if !issue.ParentIssueID.Valid {
		return notified
	}

	// Merge already-notified IDs into exclude set for parent subscribers.
	parentExclude := make(map[string]bool, len(exclude)+len(notified)+len(tierSuppressed))
	for id := range exclude {
		parentExclude[id] = true
	}
	for id := range notified {
		parentExclude[id] = true
	}
	// Recipients the delegated tier just filtered out for THIS child must not
	// get the same event smuggled back in through the parent's subscriber list.
	// The common shape is exactly that: the human directly created the parent
	// (reason='creator', full delivery) while their agent filed the children
	// (reason='delegated', reduced). Without this the tier suppresses nothing
	// in the one case it exists for — an agent-built tree under a parent the
	// human is watching (MUL-5483).
	for id := range tierSuppressed {
		parentExclude[id] = true
	}

	// Query subscribers from the parent issue, but the inbox item still
	// points to the sub-issue so the user navigates to the actual change.
	parentID := util.UUIDToString(issue.ParentIssueID)
	parentNotified, _ := notifyIssueSubscribers(ctx, queries, bus,
		parentID, issueID, issueStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, details)
	for id := range parentNotified {
		notified[id] = true
	}
	return notified
}

// notifyProjectSubscribers delivers an issue event to the members watching the
// issue's project. A project subscription is an explicit request to hear about
// every issue in it, so the delegated delivery tier does not narrow it; the
// actor, the exclude set, muted preference groups and per-issue opt-outs
// (ListProjectSubscribersForIssue) still apply.
func notifyProjectSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	subscribers, err := queries.ListProjectSubscribersForIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("failed to list project subscribers for notification",
			"issue_id", issueID, "error", err)
		return
	}
	if len(subscribers) == 0 {
		return
	}

	recipients := make([]string, 0, len(subscribers))
	for _, sub := range subscribers {
		subID := util.UUIDToString(sub.UserID)
		if subID == e.ActorID || exclude[subID] {
			continue
		}
		recipients = append(recipients, subID)
	}
	userPrefs := loadUserPrefs(ctx, queries, workspaceID, recipients)

	for _, subID := range recipients {
		if prefs, ok := userPrefs[subID]; ok && isNotifMuted(prefs, notifType, inbox.SectionUpdates) {
			continue
		}
		// Watching a project is following it: whoever answers for the issue
		// was reached through its own subscribers first and is excluded here.
		createMemberInboxItem(ctx, queries, bus, subID, workspaceID, e, issueID, issueStatus,
			notifType, severity, title, body, details, inbox.SectionUpdates)
	}
}

// createMemberInboxItem writes one member inbox row and announces it. Reports
// whether a row was actually inserted.
func createMemberInboxItem(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	recipientID string,
	workspaceID string,
	e events.Event,
	issueID string,
	issueStatus string,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
	section string,
) bool {
	item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(recipientID),
		Type:          notifType,
		Severity:      severity,
		IssueID:       parseUUID(issueID),
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText(e.ActorType),
		ActorID:       optionalUUID(e.ActorID),
		Details:       details,
		Section:       util.StrToText(section),
	})
	if err != nil {
		slog.Error("member notification creation failed",
			"recipient_id", recipientID, "type", notifType, "error", err)
		return false
	}
	if !inserted {
		return false
	}
	resp := inboxItemToResponse(item)
	resp["issue_status"] = issueStatus
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		Payload:     map[string]any{"item": resp},
	})
	return true
}

// notifyIssueSubscribers sends inbox notifications to subscribers of
// subscriberIssueID, but creates inbox items pointing to targetIssueID.
// This allows querying subscribers from a parent issue while the notification
// links to the sub-issue where the change actually occurred.
//
// Returns two sets of member IDs: those that were notified, and those the
// delegated delivery tier deliberately filtered out. The caller propagates the
// second set into the parent bubble so a suppressed event cannot be
// re-delivered through an ancestor subscription (see notifySubscribers).
func notifyIssueSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	subscriberIssueID string,
	targetIssueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) (map[string]bool, map[string]bool) {
	notified := map[string]bool{}
	tierSuppressed := map[string]bool{}

	// Resolve the handoff question once for the whole subscriber list, rather
	// than per row. A built-in key answers without a query, so the common path
	// still performs no I/O. (MUL-7379)
	//
	// FAIL OPEN. A catalog read can only fail for a CUSTOM status, and the two
	// outcomes are not symmetric: a spare inbox row costs one dismissal, while
	// a dropped one silently strands the issue on a status whose whole purpose
	// is to be waited on, with no other signal to the delegated subscriber.
	//
	// The same direction decides the section: on a failed read the status is
	// taken to wait on a person, so the row lands in "Needs you".
	statusIsHandoff, statusWaitsOnHuman, err := issueStatusHandoff(ctx, queries, parseUUID(workspaceID), issueStatus)
	if err != nil {
		slog.Warn("resolve issue status handoff for delegated delivery; delivering",
			"issue_id", subscriberIssueID, "status", issueStatus, "error", err)
		statusIsHandoff = true
		statusWaitsOnHuman = true
	}

	// Who answers for the issue the notification points at decides its Inbox
	// section. FAIL OPEN again: without the issue, every recipient is treated
	// as answering for it.
	targetIssue, targetIssueErr := queries.GetIssue(ctx, parseUUID(targetIssueID))
	if targetIssueErr != nil {
		slog.Warn("load issue for inbox section; filing under needs you",
			"issue_id", targetIssueID, "error", targetIssueErr)
	}

	subs, err := queries.ListIssueSubscribers(ctx, parseUUID(subscriberIssueID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"issue_id", subscriberIssueID, "error", err)
		return notified, tierSuppressed
	}

	// Batch-load notification preferences for all member subscribers.
	var memberIDs []string
	for _, sub := range subs {
		if sub.UserType == "member" {
			memberIDs = append(memberIDs, util.UUIDToString(sub.UserID))
		}
	}
	userPrefs := loadUserPrefs(ctx, queries, workspaceID, memberIDs)

	for _, sub := range subs {
		// Only notify member-type subscribers (not agents)
		if sub.UserType != "member" {
			continue
		}

		subID := util.UUIDToString(sub.UserID)

		// Skip the actor
		if subID == e.ActorID {
			continue
		}

		// Skip any extra excluded IDs
		if exclude[subID] {
			continue
		}

		// A delegated subscription means an agent filed the issue on this
		// member's behalf, so it is theirs as much as one they created.
		addressed := sub.Reason == "delegated" || targetIssueErr != nil ||
			memberAnswersForIssue(targetIssue, subID)
		section := inbox.SubscriberSection(notifType, addressed, statusWaitsOnHuman)

		// Skip if the user switched this kind of update off
		if prefs, ok := userPrefs[subID]; ok && isNotifMuted(prefs, notifType, section) {
			continue
		}

		// Delegated subscriptions deliver a narrower event set than direct
		// ones — see deliverToSubscriber.
		if !deliverToSubscriber(sub.Reason, notifType, statusIsHandoff) {
			tierSuppressed[subID] = true
			continue
		}

		if createMemberInboxItem(ctx, queries, bus, subID, workspaceID, e, targetIssueID, issueStatus,
			notifType, severity, title, body, details, section) {
			notified[subID] = true
		}
	}

	return notified, tierSuppressed
}

// notifyDirect creates an inbox item for a specific recipient. Skips if the
// recipient is the actor. Publishes an inbox:new event on success.
func notifyDirect(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	recipientType string,
	recipientID string,
	workspaceID string,
	e events.Event,
	issueID string,
	issueStatus string,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	// Skip if recipient is the actor
	if recipientID == e.ActorID {
		return
	}

	// Check notification preferences for member recipients. The row is filed
	// by its type (inbox.CreateIfEligible), and only an update can be muted.
	if section := inbox.DefaultSection(notifType); recipientType == "member" && section == inbox.SectionUpdates {
		prefs := loadUserPrefs(ctx, queries, workspaceID, []string{recipientID})
		if p, ok := prefs[recipientID]; ok && isNotifMuted(p, notifType, section) {
			return
		}
	}

	item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: recipientType,
		RecipientID:   parseUUID(recipientID),
		Type:          notifType,
		Severity:      severity,
		IssueID:       parseUUID(issueID),
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText(e.ActorType),
		ActorID:       optionalUUID(e.ActorID),
		Details:       details,
	})
	if err != nil {
		slog.Error("direct notification creation failed",
			"issue_id", issueID, "recipient_id", recipientID, "type", notifType, "error", err)
		return
	}
	if !inserted {
		return
	}

	resp := inboxItemToResponse(item)
	resp["issue_status"] = issueStatus
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		Payload:     map[string]any{"item": resp},
	})
}

// notifyMentionedMembers creates inbox items for each @mentioned member,
// excluding the actor and any IDs in the skip set. When an @all mention is
// present, all workspace members are notified (excluding agents).
func notifyMentionedMembers(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	mentions []mention,
	issueID string,
	issueTitle string,
	issueStatus string,
	title string,
	skip map[string]bool,
	details []byte,
) {
	// Collect the set of member IDs to notify. The value records whether the
	// member was mentioned by name, rather than only through @all or a squad.
	recipientIDs := map[string]bool{}

	hasAll := false
	var squadIDs []string
	for _, m := range mentions {
		if m.Type == "all" {
			hasAll = true
			continue
		}
		if m.Type == "member" {
			if _, err := util.ParseUUID(m.ID); err != nil {
				continue
			}
			recipientIDs[m.ID] = true
		}
		if m.Type == "squad" {
			squadIDs = append(squadIDs, m.ID)
		}
	}

	// Expand each @squad mention to its human members. Agent members of a
	// squad are reached via comment-trigger / assignment paths, not the
	// mention-inbox path, so we only seed member-typed recipients here.
	for _, sid := range squadIDs {
		squadUUID, err := util.ParseUUID(sid)
		if err != nil {
			continue
		}
		members, err := queries.ListSquadMembers(context.Background(), squadUUID)
		if err != nil {
			slog.Error("failed to list squad members for @squad mention", "squad_id", sid, "error", err)
			continue
		}
		for _, sm := range members {
			if id := util.UUIDToString(sm.MemberID); sm.MemberType == "member" && !recipientIDs[id] {
				recipientIDs[id] = false
			}
		}
	}

	// If @all is present, expand to all workspace members.
	if hasAll {
		members, err := queries.ListMembers(context.Background(), parseUUID(e.WorkspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", e.WorkspaceID, "error", err)
		} else {
			for _, m := range members {
				if id := util.UUIDToString(m.UserID); !recipientIDs[id] {
					recipientIDs[id] = false
				}
			}
		}
	}

	// Batch-load notification preferences for all mention recipients.
	var mentionUserIDs []string
	for id := range recipientIDs {
		if id != e.ActorID && !skip[id] {
			mentionUserIDs = append(mentionUserIDs, id)
		}
	}
	mentionPrefs := loadUserPrefs(context.Background(), queries, e.WorkspaceID, mentionUserIDs)

	for id, byName := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		// A mention is never dropped. One that reached the member only through
		// @all or a squad is an update when they asked for that; everything
		// else is addressed to them.
		section := inbox.SectionNeedsYou
		if p, ok := mentionPrefs[id]; ok && !byName && groupMentionsAreUpdates(p) {
			section = inbox.SectionUpdates
		}
		item, inserted, err := inbox.CreateIfEligible(context.Background(), queries, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(id),
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       parseUUID(issueID),
			Title:         title,
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
			Section:       util.StrToText(section),
		})
		if err != nil {
			slog.Error("mention inbox creation failed", "mentioned_id", id, "error", err)
			continue
		}
		if !inserted {
			continue
		}
		resp := inboxItemToResponse(item)
		resp["issue_status"] = issueStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: e.WorkspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}
}

// registerNotificationListeners wires up event bus listeners that create inbox
// notifications using the subscriber table. This replaces the old hardcoded
// notification logic from inbox_listeners.go.
//
// NOTE: uses context.Background() because the event bus dispatches synchronously
// within the HTTP request goroutine. Adding per-handler timeouts is a bus-level
// concern — see events.Bus for future improvements.
func registerNotificationListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// issue:created — Direct notification to assignee if assignee != actor
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		// Track who already got notified to avoid duplicates
		skip := map[string]bool{e.ActorID: true}

		// Direct notification to assignees that own an inbox.
		if issue.AssigneeType != nil && issue.AssigneeID != nil && isAssignmentRecipientType(*issue.AssigneeType) {
			skip[*issue.AssigneeID] = true
			notifyDirect(ctx, queries, bus,
				*issue.AssigneeType, *issue.AssigneeID,
				issue.WorkspaceID, e, issue.ID, issue.Status,
				"issue_assigned", "action_required",
				issue.Title,
				"",
				emptyDetails,
			)
		}

		// Notify @mentions in description
		if issue.Description != nil && *issue.Description != "" {
			mentions := parseMentions(*issue.Description)
			notifyMentionedMembers(bus, queries, e, mentions, issue.ID, issue.Title, issue.Status,
				issue.Title, skip, emptyDetails)
		}

		// Members watching the project hear about every new issue in it.
		if issue.ProjectID != nil {
			notifyProjectSubscribers(ctx, queries, bus, issue.ID, issue.Status, issue.WorkspaceID, e,
				skip, "issue_created", "info",
				issue.Title, "",
				emptyDetails)
		}
	})

	// issue:updated — handle assignee changes, status changes, priority, due date
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		statusChanged, _ := payload["status_changed"].(bool)
		descriptionChanged, _ := payload["description_changed"].(bool)
		prevAssigneeType, _ := payload["prev_assignee_type"].(*string)
		prevAssigneeID, _ := payload["prev_assignee_id"].(*string)
		prevDescription, _ := payload["prev_description"].(*string)

		if assigneeChanged {
			// Build structured details for assignee change.
			//
			// map[string]string, not map[string]any: every client parses inbox
			// `details` as a string->string map, and because the inbox endpoint
			// returns an ARRAY, one non-string value fails the whole parse and
			// blanks the entire list rather than one row. `any` let that be a
			// convention a reviewer had to notice; the concrete type makes it a
			// compile error. This is the only details map in this file that was
			// not already string-typed.
			detailsMap := map[string]string{}
			if prevAssigneeType != nil {
				detailsMap["prev_assignee_type"] = *prevAssigneeType
			}
			if prevAssigneeID != nil {
				detailsMap["prev_assignee_id"] = *prevAssigneeID
			}
			if issue.AssigneeType != nil {
				detailsMap["new_assignee_type"] = *issue.AssigneeType
			}
			if issue.AssigneeID != nil {
				detailsMap["new_assignee_id"] = *issue.AssigneeID
			}
			assigneeDetails, _ := json.Marshal(detailsMap)

			// Direct: notify new assignee about assignment when it owns an inbox.
			if issue.AssigneeType != nil && issue.AssigneeID != nil && isAssignmentRecipientType(*issue.AssigneeType) {
				notifyDirect(ctx, queries, bus,
					*issue.AssigneeType, *issue.AssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"issue_assigned", "action_required",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Direct: notify only a previous member assignee about unassignment.
			// This is intentionally narrower than isAssignmentRecipientType: agents
			// do not receive unassigned notifications.
			if prevAssigneeType != nil && prevAssigneeID != nil && *prevAssigneeType == "member" {
				notifyDirect(ctx, queries, bus,
					"member", *prevAssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"unassigned", "info",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Subscriber: notify remaining subscribers about assignee change,
			// excluding actor, old assignee, and new assignee
			exclude := map[string]bool{}
			if prevAssigneeID != nil {
				exclude[*prevAssigneeID] = true
			}
			if issue.AssigneeID != nil {
				exclude[*issue.AssigneeID] = true
			}
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				exclude, "assignee_changed", "info",
				issue.Title, "",
				assigneeDetails)
		}

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			statusDetails, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   issue.Status,
			})
			// A done or cancelled issue leaves every Inbox before the change is
			// recorded, so what remains is the one update saying it closed.
			archiveInboxForClosedIssue(ctx, queries, bus, e.WorkspaceID, issue.ID, issue.Status)
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "status_changed", "info",
				issue.Title, "",
				statusDetails)

			// When the issue progresses past the failure, retire any stale
			// task_failed inbox rows so the inbox reflects the current state of
			// the work, not its history. The activity log keeps the full
			// failure history for audit.
			//
			// Same handoff predicate as the delegated tier, minus Blocked:
			// blocked hands the issue to a human, but it hands over work that
			// is still stuck, so the past failures remain worth triaging.
			// Blocked is excluded by its exact key — a custom started status is
			// a review gate, not a stall. (MUL-7379)
			//
			// FAIL CLOSED here, unlike delivery: hiding a failure notice on a
			// guess is worse than leaving a stale one the user can dismiss.
			handoff, handoffErr := issueStatusIsHandoff(ctx, queries, parseUUID(e.WorkspaceID), issue.Status)
			switch {
			case handoffErr != nil:
				slog.Warn("resolve issue status handoff for task_failed dismissal; keeping rows",
					"issue_id", issue.ID, "status", issue.Status, "error", handoffErr)
			case handoff && issue.Status != issuestatus.Blocked:
				archiveStaleTaskFailedInbox(ctx, queries, bus, e.WorkspaceID, issue.ID)
			}
		}

		if priorityChanged, _ := payload["priority_changed"].(bool); priorityChanged {
			prevPriority, _ := payload["prev_priority"].(string)
			priorityDetails, _ := json.Marshal(map[string]string{
				"from": prevPriority,
				"to":   issue.Priority,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "priority_changed", "info",
				issue.Title, "",
				priorityDetails)
		}

		if startDateChanged, _ := payload["start_date_changed"].(bool); startDateChanged {
			prevStartDateStr := ""
			if prevStartDate, ok := payload["prev_start_date"].(*string); ok && prevStartDate != nil {
				prevStartDateStr = *prevStartDate
			}
			newStartDateStr := ""
			if issue.StartDate != nil {
				newStartDateStr = *issue.StartDate
			}
			startDateDetails, _ := json.Marshal(map[string]string{
				"from": prevStartDateStr,
				"to":   newStartDateStr,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "start_date_changed", "info",
				issue.Title, "",
				startDateDetails)
		}

		if dueDateChanged, _ := payload["due_date_changed"].(bool); dueDateChanged {
			prevDueDateStr := ""
			if prevDueDate, ok := payload["prev_due_date"].(*string); ok && prevDueDate != nil {
				prevDueDateStr = *prevDueDate
			}
			newDueDateStr := ""
			if issue.DueDate != nil {
				newDueDateStr = *issue.DueDate
			}
			dueDateDetails, _ := json.Marshal(map[string]string{
				"from": prevDueDateStr,
				"to":   newDueDateStr,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "due_date_changed", "info",
				issue.Title, "",
				dueDateDetails)
		}

		// Notify NEW @mentions in description
		if descriptionChanged && issue.Description != nil {
			newMentions := parseMentions(*issue.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				var added []mention
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						added = append(added, m)
					}
				}
				skip := map[string]bool{e.ActorID: true}
				notifyMentionedMembers(bus, queries, e, added, issue.ID, issue.Title, issue.Status,
					issue.Title, skip, emptyDetails)
			}
		}
	})

	// comment:created — notify all subscribers except the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// The comment payload can come as handler.CommentResponse from the
		// HTTP handler, or as map[string]any from the agent comment path in
		// task.go. Handle both.
		var issueID, commentID, commentContent, authorType string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentID = c.ID
			commentContent = c.Content
			authorType = c.AuthorType
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
			authorType, _ = c["author_type"].(string)
		default:
			return
		}

		// Platform-authored system comments (MUL-2538 child-done parent
		// notify) must NOT create inbox rows or parse mentions from their
		// body — the comment is a controlled platform signal, not a human
		// commenter. Mention parsing is the dangerous bit: if the body
		// transcluded a child title containing `mention://member/<uuid>`,
		// the parent's assignee inbox would light up via the generic path.
		// Skip the listener entirely; the WS broadcast still delivers the
		// comment to the issue timeline.
		if authorType == "system" {
			return
		}

		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		commentDetails := emptyDetails
		if commentID != "" {
			commentDetails, _ = json.Marshal(map[string]string{
				"comment_id": commentID,
			})
		}

		notifySubscribers(ctx, queries, bus, issueID, issueStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			issueTitle, commentContent,
			commentDetails)

		// Notify @mentions in comment content.
		mentions := parseMentions(commentContent)
		if len(mentions) > 0 {
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMembers(bus, queries, e, mentions, issueID, issueTitle, issueStatus,
				issueTitle, skip, commentDetails)
		}
	})

	// issue_reaction:added — notify the issue creator
	bus.Subscribe(protocol.EventIssueReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.IssueReactionResponse)
		if !ok {
			return
		}

		creatorType, _ := payload["creator_type"].(string)
		creatorID, _ := payload["creator_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if creatorType == "" || creatorID == "" {
			return
		}

		details, _ := json.Marshal(map[string]string{
			"emoji": reaction.Emoji,
		})

		notifyDirect(ctx, queries, bus,
			creatorType, creatorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// reaction:added — notify the comment author
	bus.Subscribe(protocol.EventReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.ReactionResponse)
		if !ok {
			return
		}

		commentAuthorType, _ := payload["comment_author_type"].(string)
		commentAuthorID, _ := payload["comment_author_id"].(string)
		commentID, _ := payload["comment_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if commentAuthorType == "" || commentAuthorID == "" {
			return
		}

		detailsMap := map[string]string{
			"emoji": reaction.Emoji,
		}
		if commentID != "" {
			detailsMap["comment_id"] = commentID
		}
		details, _ := json.Marshal(detailsMap)

		notifyDirect(ctx, queries, bus,
			commentAuthorType, commentAuthorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// task:queued — the member who started the run has handed the issue over
	bus.Subscribe(protocol.EventTaskQueued, func(e events.Event) {
		archiveInboxOnRunStart(ctx, queries, bus, e)
	})

	// task:completed — hand the issue back to the member who started the run
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		notifyRunEnded(ctx, queries, bus, e)
	})

	// task:cancelled — hand the issue back to the member who started the run
	bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		notifyRunCancelled(ctx, queries, bus, e)
	})

	// task:failed — notify all subscribers except the agent, once the failure
	// is final: an attempt with a retry pending has not ended the run.
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if retryPending, _ := payload["retry_pending"].(bool); retryPending {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		taskID, _ := payload["task_id"].(string)
		if issueID == "" {
			return
		}

		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			slog.Error("task:failed notification: failed to get issue", "issue_id", issueID, "error", err)
			return
		}

		exclude := map[string]bool{}
		if agentID != "" {
			exclude[agentID] = true
		}
		agentEvent := events.Event{
			Type:        e.Type,
			WorkspaceID: e.WorkspaceID,
			ActorType:   "agent",
			ActorID:     agentID,
		}
		details := runDetails(taskID)

		// The member who started the run gets it back in "Needs you" whatever
		// their relation to the issue. Everyone else is filed by that relation
		// in the subscriber fan-out.
		if taskUUID, err := util.ParseUUID(taskID); err == nil {
			if task, err := queries.GetAgentTask(ctx, taskUUID); err != nil {
				slog.Error("task:failed notification: failed to get task", "task_id", taskID, "error", err)
			} else if originator, ok := memberStartedRun(task); ok {
				notifyDirect(ctx, queries, bus,
					"member", originator,
					e.WorkspaceID, agentEvent, issueID, issue.Status,
					"task_failed", "action_required",
					issue.Title, "",
					details,
				)
				exclude[originator] = true
			}
		}

		notifySubscribers(ctx, queries, bus, issueID, issue.Status, e.WorkspaceID,
			agentEvent,
			exclude, "task_failed", "action_required",
			issue.Title, "",
			details)
	})
}

// inboxItemToResponse converts a db.InboxItem into a map suitable for
// JSON-serializable event payloads (mirrors handler.inboxToResponse fields).
func inboxItemToResponse(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"section":        inbox.EffectiveSection(item.Section, item.Type, item.Read),
		"snoozed_until":  util.TimestampToPtr(item.SnoozedUntil),
	}
}
