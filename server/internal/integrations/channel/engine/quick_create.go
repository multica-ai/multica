package engine

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Canonical /issue quick-create texts. The Router writes them into the
// transcript so the chat agent sees what was acknowledged; channel repliers
// reuse the same strings so the conversation and the transcript never drift.
const (
	IssueQueuedAckText   = "👀 On it — I'm turning that into an issue. I'll post the result here when it's ready."
	IssueUsageText       = "Tell me what to file, e.g. `/issue the login button does nothing on Safari`."
	IssueQueueFailedText = "⚠️ Something went wrong creating the issue. Please try again."
)

// quickCreatePrompt builds the quick-create prompt for an /issue turn from the
// turn's OWN content: the composed body (text with every inline image markdown
// kept in its original position) with the leading /issue command token removed.
// This deliberately differs from the image-stripped command title/description
// in two ways that each fix a real defect:
//
//   - A bare "/issue" carrying only image(s) still yields a non-empty prompt
//     (the image markdown), so the image is filed onto an issue instead of
//     being discarded, and text/image interleaving survives into the prompt.
//   - It reads ONLY this turn, never the previous-message fallback, so a lone
//     "/issue" with no content of its own returns "" and the caller shows the
//     usage hint instead of dragging a prior turn (e.g. an earlier image) in.
//
// The command is still detected upstream from the image-stripped Message.Text
// (an image-first "/issue …" must not stop being a command); this only shapes
// the prompt, so the two never disagree on issue-ness.
func quickCreatePrompt(msg channel.InboundMessage, staged []StagedMedia) string {
	if len(msg.Segments) == 0 {
		// Plain-text turn (no media): strip the command straight off the text.
		rest, _ := stripIssuePrefix(msg.Text)
		return strings.TrimSpace(rest)
	}
	out := make([]channel.Segment, 0, len(msg.Segments))
	stripped := false
	for _, seg := range msg.Segments {
		if !stripped && strings.TrimSpace(seg.Text) != "" {
			// The command lives in the first non-blank text run; remove its token
			// and drop the run if nothing else remains.
			stripped = true
			if rest, ok := stripIssuePrefix(seg.Text); ok {
				seg.Text = rest
			}
			if strings.TrimSpace(seg.Text) == "" {
				continue
			}
		}
		out = append(out, seg)
	}
	m := msg
	m.Segments = out
	return strings.TrimSpace(ComposeBody(m, staged))
}

// stripIssuePrefix removes a leading /issue command token from one text run,
// returning the remainder and whether the token was present. It mirrors
// ParseIssueCommand's prefix rules: leading whitespace is tolerated, and
// /issue must be a whole token (so "/issuetracker" is left intact).
func stripIssuePrefix(s string) (string, bool) {
	trimmed := strings.TrimLeft(s, " \t")
	if !strings.HasPrefix(trimmed, issueCommandPrefix) {
		return s, false
	}
	rest := trimmed[len(issueCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' && r0 != '\n' {
			return s, false
		}
	}
	return rest, true
}

// handleQuickCreate runs the quick-create branch of pipeline step 8. The user
// turn is already durable; every outcome here is reported through res flags
// (the replier renders them) plus a Router-authored transcript note so the
// session's next chat run knows what happened.
func (r *Router) handleQuickCreate(ctx context.Context, set ResolverSet, inst ResolvedInstallation, requesterID, sessionID pgtype.UUID, prompt string, attachmentIDs []pgtype.UUID, res *Result) {
	if prompt == "" {
		// A bare /issue with no content of its own (no text, no image). The
		// quick-create path never falls back to a previous message, so there is
		// nothing to file; mirror the usage hint into the transcript so the next
		// chat run sees it was answered. Such a turn carries no staged media (any
		// image composes a non-empty prompt), so nothing needs discarding.
		r.appendAssistantNote(ctx, sessionID, IssueUsageText)
		res.IssueUsage = true
		return
	}
	if _, err := set.QuickCreate.EnqueueQuickCreateChatTask(ctx, inst.WorkspaceID, requesterID, inst.AgentID, prompt, sessionID, attachmentIDs); err != nil {
		r.logger.Error("channel router: quick-create enqueue failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
		// The task that would have threaded the media onto the issue never
		// enqueued, so its chat-unbound attachment rows would dangle bound to
		// neither a chat nor an issue — drop the rows. The staged storage OBJECTS
		// stay: the committed chat_message body embeds their markdown inline, so
		// deleting them would leave a broken image in the transcript.
		r.discardIssueAttachmentRows(ctx, set, inst.WorkspaceID, attachmentIDs)
		r.appendAssistantNote(ctx, sessionID, IssueQueueFailedText)
		res.IssueQueueFailed = true
		return
	}
	r.appendAssistantNote(ctx, sessionID, IssueQueuedAckText)
	res.IssueQueued = true
}

// discardIssueAttachmentRows deletes the chat-unbound attachment rows the Router
// staged for an /issue turn whose issue never got created (an enqueue failure),
// so they do not dangle bound to neither a chat nor an issue. The staged storage
// OBJECTS are deliberately kept: the durable chat_message body embeds their
// markdown inline, so removing them would break the transcript image. Optional
// and best-effort: a nil Attachments seam or no rows is a no-op.
func (r *Router) discardIssueAttachmentRows(ctx context.Context, set ResolverSet, workspaceID pgtype.UUID, attachmentIDs []pgtype.UUID) {
	if set.Attachments != nil && len(attachmentIDs) > 0 {
		set.Attachments.DiscardAttachments(ctx, workspaceID, attachmentIDs)
	}
}

// appendAssistantNote writes a Router-authored assistant row. Best-effort: a
// failure is logged, never surfaced — the conversation reply still goes out.
func (r *Router) appendAssistantNote(ctx context.Context, sessionID pgtype.UUID, text string) {
	if r.messages == nil {
		return
	}
	if _, err := r.messages.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: sessionID,
		Role:          "assistant",
		Content:       text,
	}); err != nil {
		r.logger.Warn("channel router: assistant note append failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
	}
}
