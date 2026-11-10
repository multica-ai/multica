package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
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
	category, err := issuestatus.CategoryWithError(ctx, queries, workspaceID, status)
	if err != nil {
		return false, err
	}
	switch category {
	case issuestatus.CategoryDone, issuestatus.CategoryClosed:
		return true, nil
	case issuestatus.CategoryStarted:
		return status != issuestatus.InProgress, nil
	default:
		return false, nil
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

// notifTypeToGroup maps each InboxItemType to a user-configurable preference
// group. Types not in this map are always delivered (not configurable).
var notifTypeToGroup = map[string]string{
	"issue_assigned":     "assignments",
	"unassigned":         "assignments",
	"assignee_changed":   "assignments",
	"status_changed":     "status_changes",
	"new_comment":        "comments",
	"mentioned":          "mentions",
	"priority_changed":   "updates",
	"start_date_changed": "updates",
	"due_date_changed":   "updates",
	"task_completed":     "agent_activity",
	"task_failed":        "agent_activity",
	"agent_blocked":      "agent_activity",
	"agent_completed":    "agent_activity",
	"issue_created":      "new_issues",
}

// isNotifMuted returns true if the given notification type is muted for a user
// based on their parsed preferences map.
func isNotifMuted(prefs map[string]string, notifType string) bool {
	group, ok := notifTypeToGroup[notifType]
	if !ok {
		return false // unconfigurable types are always delivered
	}
	return prefs[group] == "muted"
}

// Focused inbox preference keys. Absent keys take the defaults: mode "all",
// every rule "on".
const (
	prefInboxMode            = "inbox_mode"
	inboxModeFocused         = "focused"
	prefFocusedAssigned      = "focused_assigned"
	prefFocusedBlocked       = "focused_blocked"
	prefFocusedMentioned     = "focused_mentioned"
	prefFocusedThreadReply   = "focused_thread_reply"
	prefFocusedAgentActivity = "focused_agent_activity"
)

// inboxFocusFacts answers the questions focused mode asks about one
// notification. Implementations may query lazily; all mode never calls them.
type inboxFocusFacts interface {
	targetAssignee() (assigneeType, assigneeID string)
	newStatus() string
	actorType() string
	commentAuthorType() string
	threadRootAuthor() (authorType, authorID string, isReply bool)
	directCommentMention(recipientID string) bool
}

// shouldDeliverInbox decides whether a member recipient gets an inbox row.
// All mode is exactly isNotifMuted. Focused mode ignores the group mutes and
// delivers a filtered type only when one enabled rule matches. Cheap facts are
// checked before the assignee so a lazy implementation skips the issue read
// when no rule can match anyway.
func shouldDeliverInbox(prefs map[string]string, notifType, recipientID string, facts inboxFocusFacts) bool {
	if prefs[prefInboxMode] != inboxModeFocused {
		return !isNotifMuted(prefs, notifType)
	}
	if _, filtered := notifTypeToGroup[notifType]; !filtered && notifType != "reaction_added" {
		return true
	}

	ruleOn := func(key string) bool { return prefs[key] != "off" }
	isAssignee := func() bool {
		assigneeType, assigneeID := facts.targetAssignee()
		return assigneeType == "member" && assigneeID == recipientID
	}

	switch notifType {
	case "issue_assigned":
		return ruleOn(prefFocusedAssigned)
	case "mentioned":
		return ruleOn(prefFocusedMentioned) && facts.directCommentMention(recipientID)
	case "status_changed":
		blocked := ruleOn(prefFocusedBlocked) && facts.newStatus() == issuestatus.Blocked
		byAgent := ruleOn(prefFocusedAgentActivity) && facts.actorType() == "agent"
		return (blocked || byAgent) && isAssignee()
	case "new_comment":
		if ruleOn(prefFocusedThreadReply) {
			authorType, authorID, isReply := facts.threadRootAuthor()
			if isReply && authorType == "member" && authorID == recipientID {
				return true
			}
		}
		return ruleOn(prefFocusedAgentActivity) && facts.commentAuthorType() == "agent" && isAssignee()
	}
	return false
}

// lazyFocusFacts resolves inboxFocusFacts from the triggering event, reading
// the target issue and thread root at most once each and only when asked.
type lazyFocusFacts struct {
	ctx           context.Context
	queries       *db.Queries
	event         events.Event
	targetIssueID string
	issueStatus   string

	// Mention origin; set only on the mention path.
	fromComment    bool
	directMentions map[string]bool

	assigneeLoaded bool
	assigneeType   string
	assigneeID     string

	rootLoaded     bool
	rootAuthorType string
	rootAuthorID   string
	isReply        bool
}

func newLazyFocusFacts(
	ctx context.Context,
	queries *db.Queries,
	e events.Event,
	targetIssueID string,
	issueStatus string,
) *lazyFocusFacts {
	return &lazyFocusFacts{
		ctx:           ctx,
		queries:       queries,
		event:         e,
		targetIssueID: targetIssueID,
		issueStatus:   issueStatus,
	}
}

func (f *lazyFocusFacts) newStatus() string { return f.issueStatus }

func (f *lazyFocusFacts) actorType() string { return f.event.ActorType }

func (f *lazyFocusFacts) directCommentMention(recipientID string) bool {
	return f.fromComment && f.directMentions[recipientID]
}

func (f *lazyFocusFacts) commentAuthorType() string {
	c, _ := commentFromEvent(f.event)
	return c.AuthorType
}

func (f *lazyFocusFacts) targetAssignee() (string, string) {
	if f.assigneeLoaded {
		return f.assigneeType, f.assigneeID
	}
	f.assigneeLoaded = true

	// issue:updated already carries the post-update issue.
	if payload, ok := f.event.Payload.(map[string]any); ok {
		if issue, ok := payload["issue"].(handler.IssueResponse); ok && issue.ID == f.targetIssueID {
			if issue.AssigneeType != nil && issue.AssigneeID != nil {
				f.assigneeType, f.assigneeID = *issue.AssigneeType, *issue.AssigneeID
			}
			return f.assigneeType, f.assigneeID
		}
	}

	issue, err := f.queries.GetIssue(f.ctx, parseUUID(f.targetIssueID))
	if err != nil {
		slog.Warn("focused inbox: load target issue", "issue_id", f.targetIssueID, "error", err)
		return "", ""
	}
	if issue.AssigneeType.Valid && issue.AssigneeID.Valid {
		f.assigneeType = issue.AssigneeType.String
		f.assigneeID = util.UUIDToString(issue.AssigneeID)
	}
	return f.assigneeType, f.assigneeID
}

func (f *lazyFocusFacts) threadRootAuthor() (string, string, bool) {
	if f.rootLoaded {
		return f.rootAuthorType, f.rootAuthorID, f.isReply
	}
	f.rootLoaded = true

	c, ok := commentFromEvent(f.event)
	if !ok || c.ID == "" || (c.ParentKnown && c.ParentID == "") {
		return "", "", false
	}
	root, err := f.queries.GetThreadRoot(f.ctx, db.GetThreadRootParams{
		CommentID:   parseUUID(c.ID),
		WorkspaceID: parseUUID(f.event.WorkspaceID),
	})
	if err != nil {
		slog.Warn("focused inbox: load thread root", "comment_id", c.ID, "error", err)
		return "", "", false
	}
	if util.UUIDToString(root.ID) == c.ID {
		return "", "", false
	}
	f.rootAuthorType = root.AuthorType
	f.rootAuthorID = util.UUIDToString(root.AuthorID)
	f.isReply = true
	return f.rootAuthorType, f.rootAuthorID, f.isReply
}

// eventComment is the part of a comment:created payload the listeners read.
type eventComment struct {
	ID         string
	IssueID    string
	Content    string
	AuthorType string
	ParentID   string
	// ParentKnown is false when the payload omits parent_id entirely.
	ParentKnown bool
}

// commentFromEvent reads the comment from a comment:created payload, which is
// handler.CommentResponse from HTTP handlers and map[string]any from service
// paths such as TaskService.createAgentComment.
func commentFromEvent(e events.Event) (eventComment, bool) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return eventComment{}, false
	}
	switch c := payload["comment"].(type) {
	case handler.CommentResponse:
		out := eventComment{
			ID:          c.ID,
			IssueID:     c.IssueID,
			Content:     c.Content,
			AuthorType:  c.AuthorType,
			ParentKnown: true,
		}
		if c.ParentID != nil {
			out.ParentID = *c.ParentID
		}
		return out, true
	case map[string]any:
		out := eventComment{}
		out.IssueID, _ = c["issue_id"].(string)
		out.ID, _ = c["id"].(string)
		out.Content, _ = c["content"].(string)
		out.AuthorType, _ = c["author_type"].(string)
		if raw, present := c["parent_id"]; present {
			out.ParentKnown = true
			switch parent := raw.(type) {
			case *string:
				if parent != nil {
					out.ParentID = *parent
				}
			case string:
				out.ParentID = parent
			}
		}
		return out, true
	default:
		return eventComment{}, false
	}
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
	// One resolver for every pass: the inbox row always targets this issue.
	facts := newLazyFocusFacts(ctx, queries, e, issueID, issueStatus)
	notified := notifyIssueAndParentSubscribers(ctx, queries, bus, issueID, issueStatus,
		workspaceID, e, exclude, notifType, severity, title, body, details, facts)

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
		projectExclude, notifType, severity, title, body, details, facts)
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
	facts inboxFocusFacts,
) map[string]bool {
	notified, tierSuppressed := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details, facts)

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
		notifType, severity, title, body, details, facts)
	for id := range parentNotified {
		notified[id] = true
	}
	return notified
}

// notifyProjectSubscribers delivers an issue event to the members watching the
// issue's project. A project subscription is an explicit request to hear about
// every issue in it, so the delegated delivery tier does not narrow it; the
// actor, the exclude set, the recipient's inbox preferences and per-issue
// opt-outs (ListProjectSubscribersForIssue) still apply.
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
	facts inboxFocusFacts,
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
		if !shouldDeliverInbox(userPrefs[subID], notifType, subID, facts) {
			continue
		}
		createMemberInboxItem(ctx, queries, bus, subID, workspaceID, e, issueID, issueStatus,
			notifType, severity, title, body, details)
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
	facts inboxFocusFacts,
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
	statusIsHandoff, err := issueStatusIsHandoff(ctx, queries, parseUUID(workspaceID), issueStatus)
	if err != nil {
		slog.Warn("resolve issue status handoff for delegated delivery; delivering",
			"issue_id", subscriberIssueID, "status", issueStatus, "error", err)
		statusIsHandoff = true
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

		if !shouldDeliverInbox(userPrefs[subID], notifType, subID, facts) {
			continue
		}

		// Delegated subscriptions deliver a narrower event set than direct
		// ones — see deliverToSubscriber.
		if !deliverToSubscriber(sub.Reason, notifType, statusIsHandoff) {
			tierSuppressed[subID] = true
			continue
		}

		if createMemberInboxItem(ctx, queries, bus, subID, workspaceID, e, targetIssueID, issueStatus,
			notifType, severity, title, body, details) {
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

	// Check notification preferences for member recipients.
	if recipientType == "member" {
		prefs := loadUserPrefs(ctx, queries, workspaceID, []string{recipientID})
		facts := newLazyFocusFacts(ctx, queries, e, issueID, issueStatus)
		if !shouldDeliverInbox(prefs[recipientID], notifType, recipientID, facts) {
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
	// Collect the set of member IDs to notify. directIDs keeps the ones named
	// individually; focused mode ignores @all and @squad expansion.
	recipientIDs := map[string]bool{}
	directIDs := map[string]bool{}

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
			directIDs[m.ID] = true
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
			if sm.MemberType == "member" {
				recipientIDs[util.UUIDToString(sm.MemberID)] = true
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
				recipientIDs[util.UUIDToString(m.UserID)] = true
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
	facts := newLazyFocusFacts(context.Background(), queries, e, issueID, issueStatus)
	facts.fromComment = e.Type == protocol.EventCommentCreated
	facts.directMentions = directIDs

	for id := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		// Skip if mentions are muted by this user. This is deliberately a
		// different group from `comments`: muting comment volume must not
		// silence someone asking for you by name.
		if !shouldDeliverInbox(mentionPrefs[id], "mentioned", id, facts) {
			continue
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
				emptyDetails, newLazyFocusFacts(ctx, queries, e, issue.ID, issue.Status))
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

		comment, ok := commentFromEvent(e)
		if !ok {
			return
		}
		issueID, commentID := comment.IssueID, comment.ID
		commentContent, authorType := comment.Content, comment.AuthorType

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

	// task:completed — no inbox notification (completion is visible from status change)

	// task:failed — notify all subscribers except the agent
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
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

		notifySubscribers(ctx, queries, bus, issueID, issue.Status, e.WorkspaceID,
			events.Event{
				Type:        e.Type,
				WorkspaceID: e.WorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
			},
			exclude, "task_failed", "action_required",
			issue.Title, "",
			emptyDetails)
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
	}
}
