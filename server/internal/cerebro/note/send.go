// Note↔destination coupling + "send comments to agent" (FIR-1621). This is the
// second backend brick for notes/documents as an agent-collaboration surface,
// built on top of the per-comment "sent" state from the first brick.
//
// Two reads/writes live here:
//
//   - ListNotesForReference (GET /api/notes/by-reference?object=&ref_id=) is the
//     reverse side of the existing note→issue reference: it lists the notes that
//     point AT a given object (an issue today), so the issue page can show its
//     coupled notes in the document list. The forward side (note→issue) already
//     exists in references.go; this makes the link two-way.
//
//   - SendComments (POST /api/notes/{id}/comments/send) dispatches the selected
//     (or all) unsent comments on a note to the destination the note is coupled
//     to. Coupling is the same generic note-reference machinery: object='issue'
//     bundles the comments into one issue comment and wakes any @-mentioned
//     agent via the existing issue mention-trigger; object='chat_session' posts
//     them as a chat message and enqueues the bound agent. Sending is always an
//     explicit user action — there is no auto-fire on @-mention inside a note.
//
// The agent dispatch goes through the upstream TaskService (injected as the
// narrow TaskDispatcher interface so this package never imports service), the
// same seam cerebro channels use to wake always-listening agents.
package note

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// coupledObjects are the reference `object` kinds a note may be coupled to as a
// send destination. A note can reference many things; only these two are valid
// places to dispatch comments.
const (
	couplingIssue = "issue"
	couplingChat  = "chat_session"
)

// TaskDispatcher is the narrow slice of the upstream TaskService that the send
// flow needs: wake a specific agent on an issue (mention trigger) or run the
// agent bound to a chat session. Declared here as an interface so the note
// package stays free of the service package; *service.TaskService satisfies it
// and is wired in router.go.
type TaskDispatcher interface {
	EnqueueTaskForMention(ctx context.Context, issue db.Issue, agentID pgtype.UUID, triggerCommentID pgtype.UUID) (db.AgentTaskQueue, error)
	EnqueueChatTask(ctx context.Context, chatSession db.ChatSession) (db.AgentTaskQueue, error)
}

// MentionGate is the same trigger-permission gate the issue comment path applies
// to @agent mentions (trigger_other_agent capability + private-agent visibility).
// The send flow reuses it so dispatching to an issue cannot wake an agent the
// sender would not be allowed to mention directly. Nil disables the gate (the
// service satisfies it; *mentiongate.Service is wired in router.go).
type MentionGate interface {
	CanTriggerMention(ctx context.Context, r *http.Request, workspaceID string, agentID, ownerID pgtype.UUID) (bool, error)
}

// referencedNoteRowToResponse maps a reverse-lookup row to the same NoteResponse
// the rest of the Notes API returns, so the issue page renders coupled notes
// identically to the personal Notes list.
func referencedNoteRowToResponse(n cerebrodb.ListNotesReferencingObjectRow) NoteResponse {
	return NoteResponse{
		ID:          uuidStr(n.ID),
		WorkspaceID: uuidStr(n.WorkspaceID),
		FolderID:    uuidPtr(n.FolderID),
		Title:       n.Title,
		Body:        n.Body,
		OwnerID:     uuidStr(n.OwnerID),
		Visibility:  n.Visibility,
		Pinned:      n.Pinned,
		CreatedAt:   tsStr(n.CreatedAt),
		UpdatedAt:   tsStr(n.UpdatedAt),
	}
}

// ListNotesForReference handles GET /api/notes/by-reference?object=&ref_id=.
// Returns every note in the workspace the caller may see that is coupled to the
// given object (e.g. all notes referencing one issue). Visibility is enforced in
// SQL — a private note coupled to an issue is only returned to its owner.
func (h *Handler) ListNotesForReference(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	object := strings.TrimSpace(r.URL.Query().Get("object"))
	refID := strings.TrimSpace(r.URL.Query().Get("ref_id"))
	if object == "" || refID == "" {
		writeError(w, http.StatusBadRequest, "object and ref_id are required")
		return
	}
	rows, err := h.Cerebro.ListNotesReferencingObject(r.Context(), cerebrodb.ListNotesReferencingObjectParams{
		WorkspaceID: wsUUID,
		Object:      object,
		RefID:       refID,
		ViewerID:    ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load coupled notes")
		return
	}
	out := make([]NoteResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, referencedNoteRowToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": out})
}

// sendCommentsRequest is the POST body for the send endpoint. CommentIDs is
// optional: when empty, every unsent comment on the note is sent ("send all");
// when present, only those comments are sent ("send selected"). Already-sent ids
// in the list are ignored (the operation only ever sends drafts).
//
// FIR-1753 — DestinationObject/DestinationRefID are an optional disambiguator:
// a note may be linked to more than one issue/chat (References lets you link
// several issues for context), but a send needs exactly one destination. When
// the note has multiple couplings the client passes the chosen one here; the
// server validates it is actually one of the note's couplings before using it.
// Empty (the common single-coupling case) keeps the old "resolve the one
// coupling" behaviour.
type sendCommentsRequest struct {
	CommentIDs        []string `json:"comment_ids"`
	DestinationObject string   `json:"destination_object"`
	DestinationRefID  string   `json:"destination_ref_id"`
}

// sendCommentsResponse echoes the result so the client can update its view
// without a refetch: the comments that were just sent (with their new
// sent_to_agent_at), how many drafts remain, and where they went.
type sendCommentsResponse struct {
	Sent             []CommentResponse `json:"sent"`
	UnsentRemaining  int64             `json:"unsent_remaining"`
	DestinationKind  string            `json:"destination_kind"`
	DestinationRefID string            `json:"destination_ref_id"`
	AgentsTriggered  int               `json:"agents_triggered"`
}

// SendComments handles POST /api/notes/{id}/comments/send. Anyone who can see
// the note may send (it is a deliberate collaboration action, the same access
// level as posting a comment). The note must be coupled to exactly one issue or
// chat; the selected/all unsent comments are bundled and dispatched there, then
// stamped as sent.
func (h *Handler) SendComments(w http.ResponseWriter, r *http.Request) {
	if h.Tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "agent dispatch is not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanComment(w, r, noteID, ownerUUID) {
		return
	}

	artifact, err := h.Upstream.GetArtifact(r.Context(), db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}

	var req sendCommentsRequest
	// An empty body is valid (means "send all unsent"); decodeJSON tolerates it.
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	object, refID, ok := h.resolveCoupling(w, r, noteID, uuidStr(artifact.IssueID), req.DestinationObject, req.DestinationRefID)
	if !ok {
		return
	}

	toSend, ok := h.gatherUnsentComments(w, r, noteID, req.CommentIDs)
	if !ok {
		return
	}
	if len(toSend) == 0 {
		writeError(w, http.StatusBadRequest, "no comments to send — only comments that @-tag an agent are sent")
		return
	}

	body := buildBundle(noteDisplayTitle(artifact.Title, artifact.Body), h.noteLink(r.Context(), wsUUID, noteID), toSend)

	var agentsTriggered int
	switch object {
	case couplingIssue:
		agentsTriggered, ok = h.dispatchToIssue(w, r, wsUUID, ownerUUID, refID, body, toSend)
	case couplingChat:
		agentsTriggered, ok = h.dispatchToChat(w, r, wsUUID, refID, body)
	default:
		writeError(w, http.StatusConflict, "note is not coupled to an issue or chat")
		return
	}
	if !ok {
		return
	}

	ids := make([]pgtype.UUID, 0, len(toSend))
	for _, c := range toSend {
		ids = append(ids, c.ID)
	}
	updated, err := h.Cerebro.MarkNoteCommentsSent(r.Context(), cerebrodb.MarkNoteCommentsSentParams{
		NoteID:     noteID,
		CommentIds: ids,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "comments were dispatched but marking them sent failed")
		return
	}
	// Remaining = unsent comments that still @-tag an agent (those are the only
	// ones that can be sent). Untagged drafts are not counted — they are not
	// pending dispatch.
	var remaining int64
	if rows, err := h.Cerebro.ListUnsentNoteComments(r.Context(), noteID); err == nil {
		for _, c := range rows {
			if hasAgentMention(c.Body) {
				remaining++
			}
		}
	}

	out := make([]CommentResponse, 0, len(updated))
	for _, c := range updated {
		out = append(out, commentToResponse(c))
	}
	writeJSON(w, http.StatusOK, sendCommentsResponse{
		Sent:             out,
		UnsentRemaining:  remaining,
		DestinationKind:  object,
		DestinationRefID: refID,
		AgentsTriggered:  agentsTriggered,
	})
}

// resolveCoupling reads the note's references and returns the issue or chat
// destination the comments should go to. Writes the error + returns ok=false
// when there is no coupling at all.
//
// FIR-1753 — a note may legitimately be linked to more than one issue/chat
// (References supports several issue links for context). The destination is
// resolved as:
//
//   - hintObject/hintRefID given → it must match one of the note's couplings;
//     send there. This is how the client disambiguates when the note has
//     multiple couplings (the user picked one in the panel).
//   - no hint + exactly one coupling → send there (the common case).
//   - no hint + multiple couplings → 409 telling the caller to choose. A
//     current client always passes the hint in this case, so this only fires
//     for an older client that cannot.
func (h *Handler) resolveCoupling(w http.ResponseWriter, r *http.Request, noteID pgtype.UUID, issueID, hintObject, hintRefID string) (object, refID string, ok bool) {
	refs, err := h.Cerebro.ListNoteReferencesByNote(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load note references")
		return "", "", false
	}
	var found []cerebrodb.CerebroNoteReference
	for _, ref := range refs {
		if ref.Object == couplingIssue || ref.Object == couplingChat {
			found = append(found, ref)
		}
	}
	if len(found) == 0 {
		// FIR-1852/FIR-1782 — a note unified to an issue via issue_id (the same
		// white-card link a document uses) carries no explicit reference row, but
		// that issue_id IS its issue coupling. Fall back to it so the note's
		// comments can be sent to the issue without a duplicate reference row.
		// This only runs when there are no explicit references, so notes that the
		// user has explicitly coupled keep their exact prior behaviour.
		if issueID != "" {
			return couplingIssue, issueID, true
		}
		writeError(w, http.StatusConflict, "note is not coupled to an issue or chat — couple it first")
		return "", "", false
	}
	hintRefID = strings.TrimSpace(hintRefID)
	if hintRefID != "" {
		hintObject = strings.TrimSpace(hintObject)
		for _, ref := range found {
			if ref.RefID == hintRefID && (hintObject == "" || ref.Object == hintObject) {
				return ref.Object, ref.RefID, true
			}
		}
		writeError(w, http.StatusConflict, "the chosen destination is not linked to this note")
		return "", "", false
	}
	if len(found) > 1 {
		writeError(w, http.StatusConflict, "this note is linked to more than one task or chat — choose which one to send to")
		return "", "", false
	}
	return found[0].Object, found[0].RefID, true
}

// gatherUnsentComments resolves the comments to send. With no ids it returns
// every unsent draft on the note; with ids it returns the named comments,
// validating each belongs to this note and skipping any already sent. Writes the
// error + returns ok=false on a bad id or cross-note id.
//
// FIR-1621 — only comments that @-tag an agent are ever sent: an untagged
// comment is local note discussion, not a message for an agent. This is the
// single chokepoint, so the rule holds whether the client sends all or a
// hand-picked selection.
func (h *Handler) gatherUnsentComments(w http.ResponseWriter, r *http.Request, noteID pgtype.UUID, ids []string) ([]cerebrodb.CerebroNoteComment, bool) {
	if len(ids) == 0 {
		rows, err := h.Cerebro.ListUnsentNoteComments(r.Context(), noteID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load comments")
			return nil, false
		}
		out := make([]cerebrodb.CerebroNoteComment, 0, len(rows))
		for _, c := range rows {
			if hasAgentMention(c.Body) {
				out = append(out, c)
			}
		}
		return out, true
	}
	out := make([]cerebrodb.CerebroNoteComment, 0, len(ids))
	for _, raw := range ids {
		cid, err := util.ParseUUID(strings.TrimSpace(raw))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid comment id: "+raw)
			return nil, false
		}
		c, err := h.Cerebro.GetNoteComment(r.Context(), cid)
		if err != nil || uuidStr(c.NoteID) != uuidStr(noteID) {
			writeError(w, http.StatusBadRequest, "comment not found on this note: "+raw)
			return nil, false
		}
		if c.SentToAgentAt.Valid {
			continue // already sent — silently skip, send is for drafts only
		}
		if !hasAgentMention(c.Body) {
			continue // not tagged at an agent — never dispatched, even if selected
		}
		out = append(out, c)
	}
	return out, true
}

// hasAgentMention reports whether the comment body @-tags at least one agent.
// It is the gate for "send to the agent": only tagged comments are dispatched.
func hasAgentMention(body string) bool {
	for _, m := range util.ParseMentions(body) {
		if m.Type == "agent" {
			return true
		}
	}
	return false
}

// noteDisplayTitle is the human name for the note in the dispatched bundle. A
// note's artifact.Title is often empty (the first body line doubles as the
// title in the Notes UI), which used to render "From note “”" on the issue
// comment (FIR-1873). Fall back to the first non-empty body line, stripped of
// leading markdown, capped; finally to "Untitled note" so the name is never
// blank.
func noteDisplayTitle(title, body string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#> ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 80 {
			line = strings.TrimSpace(line[:80]) + "…"
		}
		return line
	}
	return "Untitled note"
}

// noteLink builds the in-app path to the note (e.g. "/acme/notes?note=<id>") so
// the dispatched issue comment can deep-link back to its source note (FIR-1873).
// Best-effort: an unresolvable workspace yields an empty string and the bundle
// falls back to a plain (unlinked) note name.
func (h *Handler) noteLink(ctx context.Context, wsUUID, noteID pgtype.UUID) string {
	ws, err := h.Upstream.GetWorkspace(ctx, wsUUID)
	if err != nil || strings.TrimSpace(ws.Slug) == "" {
		return ""
	}
	return fmt.Sprintf("/%s/notes?note=%s", ws.Slug, uuidStr(noteID))
}

// buildBundle renders the note title + the comment bodies into one message body.
// Comment bodies are kept verbatim so any @-mention links inside them survive
// into the dispatched issue comment / chat message and drive the agent trigger.
//
// The note name links back to the source note (noteLink) when available so the
// reader can open it; otherwise it renders as plain quoted text (FIR-1873).
//
// When a comment is anchored to a span of the note/document (a "markering" — the
// author selected text and commented on it), the quoted span is the context the
// agent needs to act on the comment. We render it as a blockquote above the
// comment so the agent sees WHICH text the remark is about, not just the remark.
func buildBundle(title, link string, comments []cerebrodb.CerebroNoteComment) string {
	type entry struct{ quote, body string }
	entries := make([]entry, 0, len(comments))
	for _, c := range comments {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		quote := ""
		if c.AnchorQuote.Valid {
			quote = strings.TrimSpace(c.AnchorQuote.String)
		}
		entries = append(entries, entry{quote: quote, body: body})
	}
	var b strings.Builder
	if link != "" {
		fmt.Fprintf(&b, "From note [%s](%s) (%d comment", title, link, len(entries))
	} else {
		fmt.Fprintf(&b, "From note “%s” (%d comment", title, len(entries))
	}
	if len(entries) != 1 {
		b.WriteString("s")
	}
	b.WriteString("):\n\n")
	for _, e := range entries {
		if e.quote != "" {
			// Collapse newlines so the quoted span stays on one blockquote line.
			oneLine := strings.Join(strings.Fields(e.quote), " ")
			fmt.Fprintf(&b, "- On “%s”:\n  %s\n", oneLine, e.body)
		} else {
			fmt.Fprintf(&b, "- %s\n", e.body)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// dispatchToIssue posts the bundle as one issue comment and wakes every agent
// @-mentioned across the sent comments via the existing issue mention-trigger.
// The comment is authored by the sending member, so the trigger runs with a
// human origin (no agent-delegation needed). Returns the number of distinct
// agents triggered.
func (h *Handler) dispatchToIssue(w http.ResponseWriter, r *http.Request, wsUUID, ownerUUID pgtype.UUID, refID, body string, comments []cerebrodb.CerebroNoteComment) (int, bool) {
	issueUUID, err := util.ParseUUID(refID)
	if err != nil {
		writeError(w, http.StatusConflict, "coupled issue id is invalid")
		return 0, false
	}
	issue, err := h.Upstream.GetIssue(r.Context(), issueUUID)
	if err != nil || uuidStr(issue.WorkspaceID) != uuidStr(wsUUID) {
		writeError(w, http.StatusConflict, "coupled issue not found in this workspace")
		return 0, false
	}
	comment, err := h.Upstream.CreateComment(r.Context(), db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: wsUUID,
		AuthorType:  "member",
		AuthorID:    ownerUUID,
		Content:     body,
		Type:        "comment",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to post issue comment")
		return 0, false
	}
	// Wake each distinct agent mentioned across the bundled comments. Best-effort
	// per agent: a single bad/archived/not-permitted mention must not fail the
	// whole send. Each agent passes the same trigger gate the issue comment path
	// applies, so the send cannot wake an agent the sender may not mention.
	triggered := 0
	for _, agentID := range distinctAgentMentions(comments) {
		au, err := util.ParseUUID(agentID)
		if err != nil {
			continue
		}
		if h.Gate != nil {
			agent, err := h.Upstream.GetAgent(r.Context(), au)
			if err != nil {
				continue
			}
			allowed, err := h.Gate.CanTriggerMention(r.Context(), r, uuidStr(wsUUID), au, agent.OwnerID)
			if err != nil || !allowed {
				continue
			}
		}
		if _, err := h.Tasks.EnqueueTaskForMention(r.Context(), issue, au, comment.ID); err == nil {
			triggered++
		}
	}
	return triggered, true
}

// dispatchToChat posts the bundle as a user chat message on the coupled session
// and enqueues its bound agent — mirroring SendChatMessage's create-then-enqueue
// order so the daemon always finds the message.
func (h *Handler) dispatchToChat(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, refID, body string) (int, bool) {
	sessionUUID, err := util.ParseUUID(refID)
	if err != nil {
		writeError(w, http.StatusConflict, "coupled chat id is invalid")
		return 0, false
	}
	session, err := h.Upstream.GetChatSession(r.Context(), sessionUUID)
	if err != nil || uuidStr(session.WorkspaceID) != uuidStr(wsUUID) {
		writeError(w, http.StatusConflict, "coupled chat not found in this workspace")
		return 0, false
	}
	if session.Status != "active" {
		writeError(w, http.StatusConflict, "coupled chat session is archived")
		return 0, false
	}
	if _, err := h.Upstream.CreateChatMessage(r.Context(), db.CreateChatMessageParams{
		ChatSessionID: session.ID,
		Role:          "user",
		Content:       body,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to post chat message")
		return 0, false
	}
	if _, err := h.Tasks.EnqueueChatTask(r.Context(), session); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue chat task")
		return 0, false
	}
	return 1, true
}

// distinctAgentMentions returns the unique agent ids @-mentioned across the
// given comment bodies, in first-seen order.
func distinctAgentMentions(comments []cerebrodb.CerebroNoteComment) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range comments {
		for _, m := range util.ParseMentions(c.Body) {
			if m.Type != "agent" || seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, m.ID)
		}
	}
	return out
}
