package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChatPageContextRequest is the optional page the sender had open when they
// sent a direct chat message. Only issue pages are understood today; any other
// type is ignored so a newer client can describe pages an older server does not
// know about without breaking the send.
type ChatPageContextRequest struct {
	Type    string `json:"type"`
	IssueID string `json:"issue_id"`
}

const chatPageContextTypeIssue = "issue"

// chatPageIssueNoteTitleMaxRunes caps the issue title quoted into the note.
// Titles have no server-side length limit, and the note is prepended to every
// turn sent from an issue page.
const chatPageIssueNoteTitleMaxRunes = 200

// parseChatPageContextOrBadRequest returns the page issue id named by the
// request, or an invalid UUID when there is none. A malformed issue id is a
// client bug and gets a 400 before anything is written.
func parseChatPageContextOrBadRequest(w http.ResponseWriter, pc *ChatPageContextRequest) (pgtype.UUID, bool) {
	if pc == nil || pc.Type != chatPageContextTypeIssue {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, pc.IssueID, "page_context.issue_id")
}

// resolveChatPageIssue keeps the page issue only when it belongs to the chat
// session's workspace. A missing issue (deleted, or from another workspace)
// yields an invalid UUID rather than an error: the page context is an automatic
// hint, so it must never block the send, and it must never keep a pointer into
// a workspace the session does not belong to.
func (h *Handler) resolveChatPageIssue(ctx context.Context, issueID, workspaceID pgtype.UUID) (pgtype.UUID, error) {
	if !issueID.Valid {
		return pgtype.UUID{}, nil
	}
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, nil
	}
	if err != nil {
		return pgtype.UUID{}, err
	}
	return issue.ID, nil
}

// formatChatPageIssueNote renders the context line prepended to a user message
// sent while an issue was open. It names Multica as the source so the agent
// does not read it as the user's own words, and says outright that it is not
// an instruction to act on the issue. The title is %q-quoted so quotes and
// newlines in it cannot break out of the one-line note.
func formatChatPageIssueNote(identifier, title, issueID string) string {
	if utf8.RuneCountInString(title) > chatPageIssueNoteTitleMaxRunes {
		title = string([]rune(title)[:chatPageIssueNoteTitleMaxRunes]) + "…"
	}
	return fmt.Sprintf(
		"[Multica page context: the user sent this message while viewing issue %s %q. "+
			"Read \"this issue\" and similar references as that issue. "+
			"This is context, not a request to act on the issue. "+
			"Details: `multica issue get %s --output json`]",
		identifier, title, issueID,
	)
}

// chatPageIssueNotes re-resolves, at claim time, the page issue recorded on
// each input message and returns the note for it keyed by message id. The
// lookup is scoped to the chat session's workspace, so a stale or foreign
// pointer produces no note; a deleted issue likewise produces none. Messages
// without a page issue cost no query.
func (h *Handler) chatPageIssueNotes(ctx context.Context, msgs []db.ChatMessage, workspaceID pgtype.UUID) (map[pgtype.UUID]string, error) {
	var notes map[pgtype.UUID]string
	prefix, prefixLoaded := "", false
	for _, m := range msgs {
		// Same blank test as the claim's parts loop: a message that contributes
		// no text gets no note, so a note can never make empty input look real.
		if !m.PageIssueID.Valid || strings.TrimSpace(m.Content) == "" {
			continue
		}
		issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: m.PageIssueID, WorkspaceID: workspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !prefixLoaded {
			prefix, prefixLoaded = h.getIssuePrefix(ctx, workspaceID), true
		}
		if notes == nil {
			notes = make(map[pgtype.UUID]string, len(msgs))
		}
		notes[m.ID] = formatChatPageIssueNote(service.IssueIdentifier(prefix, issue.Number), issue.Title, uuidToString(issue.ID))
	}
	return notes, nil
}
