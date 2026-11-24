package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Inbox types for doc discussions.
const (
	InboxTypeDocComment   = "doc_comment"
	InboxTypeDocMentioned = "doc_mentioned"
)

// NotifyDocComment tells the members a new doc comment is for: everyone it
// @mentions, and everyone who already wrote in the discussion. The author and
// guests are never notified, and a member gets one item even when both apply.
// Shared by comments posted through the API and the reply the platform posts
// for a doc discussion run that ended without replying.
func NotifyDocComment(ctx context.Context, q *db.Queries, bus *events.Bus, doc db.Doc, root, comment db.DocComment, actorType, actorID string) {
	author := util.UUIDToString(comment.AuthorID)
	mentioned := map[string]bool{}
	for _, m := range util.ParseMentions(comment.Content) {
		if m.Type == "member" && m.ID != author {
			mentioned[m.ID] = true
		}
	}
	participants := map[string]bool{}
	if ids, err := q.ListDocThreadParticipants(ctx, db.ListDocThreadParticipantsParams{
		WorkspaceID: doc.WorkspaceID, DocID: doc.ID, RootID: root.ID,
	}); err == nil {
		for _, id := range ids {
			if s := util.UUIDToString(id); s != author && !mentioned[s] {
				participants[s] = true
			}
		}
	}
	details, _ := json.Marshal(map[string]string{
		"doc_id":     util.UUIDToString(doc.ID),
		"thread_id":  util.UUIDToString(root.ID),
		"comment_id": util.UUIDToString(comment.ID),
		"quote":      firstRunes(root.AnchorQuote, 200),
	})
	body := firstRunes(comment.Content, 500)
	actorUUID, _ := util.ParseUUID(actorID)
	deliver := func(recipient, kind string) {
		recipientUUID, err := util.ParseUUID(recipient)
		if err != nil {
			return
		}
		// Docs are closed to guests (and to anyone outside the workspace), so
		// a notification carrying the doc's title and text must not reach them.
		member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID: recipientUUID, WorkspaceID: doc.WorkspaceID,
		})
		if err != nil || projectaccess.IsGuestRole(member.Role) {
			return
		}
		if _, _, err := inbox.Deliver(ctx, q, bus, inbox.Notification{Params: db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   doc.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipientUUID,
			Type:          kind,
			Severity:      "info",
			Title:         doc.Title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: actorType, Valid: true},
			ActorID:       actorUUID,
			Details:       details,
		}}); err != nil {
			slog.Warn("doc comment notification failed", "doc_id", util.UUIDToString(doc.ID), "recipient_id", recipient, "error", err)
		}
	}
	for id := range mentioned {
		deliver(id, InboxTypeDocMentioned)
	}
	for id := range participants {
		deliver(id, InboxTypeDocComment)
	}
}

func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
