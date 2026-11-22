package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Preference keys, one per action you take on an issue, that archive your own
// notifications on it. Completing means moving it to a done or closed status;
// triggering an agent means your action actually queued, coalesced, deferred
// to an active run, steered, or reran an agent on the issue. Completing and
// triggering an agent are on unless "muted"; other edits are off unless "all".
const (
	AutoArchiveCompletedPref    = "auto_archive_completed"
	AutoArchiveAgentTriggerPref = "auto_archive_on_agent_trigger"
	AutoArchiveCommentPref      = "auto_archive_on_comment"
	AutoArchiveStatusPref       = "auto_archive_on_status"
	AutoArchiveAssigneePref     = "auto_archive_on_assignee"
	AutoArchivePriorityPref     = "auto_archive_on_priority"
	AutoArchiveProjectPref      = "auto_archive_on_project"
	AutoArchiveDatesPref        = "auto_archive_on_dates"
	AutoArchiveLabelsPref       = "auto_archive_on_labels"
)

var autoArchiveOnByDefault = map[string]bool{
	AutoArchiveCompletedPref:    true,
	AutoArchiveAgentTriggerPref: true,
}

// Why the actor's notifications were archived, sent as `reason` on
// inbox:batch-archived so the client can tell its user's own action apart.
const (
	ArchiveReasonCompleted = "issue_status_terminal"
	ArchiveReasonActed     = "actor_acted"
)

// AutoArchivesOn reports whether prefs archive the actor's notifications when
// they take action. nil prefs, a member without a stored row, applies the
// defaults.
func AutoArchivesOn(prefs map[string]string, action string) bool {
	if autoArchiveOnByDefault[action] {
		return prefs[action] != "muted"
	}
	return prefs[action] == "all"
}

// ArchiveActorIssueInbox archives the actor's own notifications for an issue,
// read or not, when their preferences archive on one of the actions they just
// took on it. Completing archives the same set "Archive completed" clears by
// hand. Other recipients keep theirs: someone else acting on an issue is news
// they have not seen yet. A later notification brings the issue back.
func ArchiveActorIssueInbox(
	ctx context.Context,
	queries *db.Queries,
	pub Publisher,
	workspaceID string,
	issueID string,
	actorType string,
	actorID string,
	actions ...string,
) {
	if actorType != "member" || actorID == "" || issueID == "" || workspaceID == "" || len(actions) == 0 {
		return
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return
	}
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		return
	}
	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		return
	}

	// FAIL CLOSED: archiving on a guess hides notifications the user wanted.
	pref, err := queries.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: wsUUID,
		UserID:      actorUUID,
	})
	var prefs map[string]string
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		slog.Warn("auto-archive actor issue inbox: load preference failed; keeping rows",
			"workspace_id", workspaceID, "issue_id", issueID, "actions", actions, "error", err)
		return
	default:
		if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
			slog.Warn("auto-archive actor issue inbox: bad preference JSON; keeping rows",
				"workspace_id", workspaceID, "issue_id", issueID, "actions", actions, "error", err)
			return
		}
	}
	var archivedOn []string
	for _, action := range actions {
		if AutoArchivesOn(prefs, action) {
			archivedOn = append(archivedOn, action)
		}
	}
	if len(archivedOn) == 0 {
		return
	}
	reason := ArchiveReasonActed
	if slices.Contains(archivedOn, AutoArchiveCompletedPref) {
		reason = ArchiveReasonCompleted
	}

	count, err := queries.ArchiveInboxByIssue(ctx, db.ArchiveInboxByIssueParams{
		WorkspaceID:   wsUUID,
		RecipientType: "member",
		RecipientID:   actorUUID,
		IssueID:       issueUUID,
	})
	if err != nil {
		slog.Error("auto-archive actor issue inbox: query failed",
			"workspace_id", workspaceID, "issue_id", issueID, "reason", reason, "error", err)
		return
	}
	if count == 0 {
		return
	}
	publish(pub, events.Event{
		Type:        protocol.EventInboxBatchArchived,
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"recipient_id": actorID,
			"count":        count,
			"issue_id":     issueID,
			"reason":       reason,
		},
	})

	slog.Info("auto-archive actor issue inbox: archived rows",
		"workspace_id", workspaceID, "issue_id", issueID, "reason", reason, "row_count", count)
}
