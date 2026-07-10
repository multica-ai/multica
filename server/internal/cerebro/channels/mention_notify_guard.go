package channels

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// channelMentionMembersOnlyFlagKey mirrors the client feature-flag key in
// packages/cerebro-feature-flags/registry.ts. FIR-2680.
const channelMentionMembersOnlyFlagKey = "cerebro_channel_mention_members_only"

// FilterChannelMentionRecipients drops @mentioned recipients who are not
// participants of a channel/group, so tagging a non-member no longer silently
// notifies them (FIR-2680). It is the server-side backstop for the client
// "add them to this channel?" prompt (Part B): the guard holds regardless of
// which client sent the comment.
//
// Behaviour, in order:
//   - Empty recipient set, or a lookup failure, returns the input unchanged
//     (fail-open — never block notification delivery on a transient error).
//   - kind ∉ {channel, group} returns the input unchanged (issue/task/dm keep
//     today's behaviour; dm promotion is a separate path that runs first).
//   - Feature flag OFF (an explicit workspace/personal override) returns the
//     input unchanged; with no override the flag defaults ON (FIR-2680).
//   - Otherwise, only recipients that are issue_subscriber rows (participants)
//     survive. This also scopes an @all mention to participants.
//
// recipientIDs keys are member UUIDs (collectMentionedMemberIDs only adds
// member ids and expands @all to members), so participation is checked with
// user_type "member".
func FilterChannelMentionRecipients(
	ctx context.Context,
	queries *db.Queries,
	cerebroQueries *cerebrodb.Queries,
	issueID pgtype.UUID,
	actorType, actorID string,
	recipientIDs map[string]bool,
) map[string]bool {
	if len(recipientIDs) == 0 || queries == nil {
		return recipientIDs
	}

	issue, err := queries.GetIssue(ctx, issueID)
	if err != nil {
		slog.Warn("channel mention guard: issue lookup failed", "error", err)
		return recipientIDs
	}
	if issue.Kind != "channel" && issue.Kind != "group" {
		return recipientIDs
	}
	if !channelMentionMembersOnlyEnabled(ctx, cerebroQueries, issue.WorkspaceID, actorType, actorID) {
		return recipientIDs
	}

	filtered := make(map[string]bool, len(recipientIDs))
	for id := range recipientIDs {
		userID, perr := util.ParseUUID(id)
		if perr != nil {
			continue
		}
		ok, serr := queries.IsIssueSubscriber(ctx, db.IsIssueSubscriberParams{
			IssueID:  issueID,
			UserType: "member",
			UserID:   userID,
		})
		if serr != nil {
			slog.Warn("channel mention guard: subscriber lookup failed", "error", serr)
			// Fail-open for this recipient so a transient error never drops a
			// legitimate notification.
			filtered[id] = true
			continue
		}
		if ok {
			filtered[id] = true
		}
	}
	return filtered
}

// channelMentionMembersOnlyEnabled resolves the FIR-2680 flag for this
// workspace+actor. Resolution mirrors dmReplyThreadsEnabled (and
// packages/cerebro-feature-flags/store.ts precedence: locked workspace >
// personal > unlocked workspace > default). It defaults ON (FIR-2680 go-live):
// with no override row the guard fires, matching the client registry default,
// so channels respect membership out of the box. A workspace/personal OFF
// override still wins, and a nil handle or any lookup failure fails safe (OFF —
// never drop a notification because the flag store was unreadable).
func channelMentionMembersOnlyEnabled(ctx context.Context, cerebroQueries *cerebrodb.Queries, workspaceID pgtype.UUID, actorType, actorID string) bool {
	if cerebroQueries == nil {
		return false
	}
	var wsEnabled, wsLocked, wsFound bool
	wsRows, err := cerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Warn("channel mention guard flag: workspace lookup failed", "error", err)
		return false
	}
	for _, row := range wsRows {
		if row.FlagKey == channelMentionMembersOnlyFlagKey {
			wsEnabled, wsLocked, wsFound = row.Enabled, row.Locked, true
			break
		}
	}
	if wsFound && wsLocked {
		return wsEnabled
	}
	if actorType == "member" && actorID != "" {
		if userID, perr := util.ParseUUID(actorID); perr == nil {
			on, gerr := cerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
				WorkspaceID: workspaceID,
				UserID:      userID,
				FlagKey:     channelMentionMembersOnlyFlagKey,
			})
			if gerr == nil {
				return on
			}
			if !errors.Is(gerr, pgx.ErrNoRows) {
				slog.Warn("channel mention guard flag: personal lookup failed", "error", gerr)
				return false
			}
		}
	}
	if wsFound {
		return wsEnabled
	}
	// No override anywhere → default ON (FIR-2680 go-live).
	return true
}
