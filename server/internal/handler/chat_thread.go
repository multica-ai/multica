package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Threads in a direct chat (MUL-8357). A thread is a chat_session whose
// parent_chat_session_id is the conversation it branched from and whose
// root_chat_message_id is the message it answers, so sending, streaming,
// queueing and read state are the ordinary chat endpoints on the thread's id.

const (
	chatThreadTitleMaxRunes    = 60
	chatIssueTitleMaxRunes     = 120
	chatThreadRootContextRunes = 4000
)

// requireTeamPage answers 404 while the Team page is off, so the endpoints
// behind it do not exist for clients that cannot render them.
func (h *Handler) requireTeamPage(w http.ResponseWriter, r *http.Request) bool {
	if !featureflags.TeamPageEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

type chatThreadError struct {
	status  int
	message string
}

func (e *chatThreadError) Error() string { return e.message }

type CreateChatThreadRequest struct {
	RootMessageID string `json:"root_message_id"`
}

// CreateChatThread opens the thread on one message of the caller's
// conversation, creating it on first open.
func (h *Handler) CreateChatThread(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	parent, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	var req CreateChatThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rootID, ok := parseUUIDOrBadRequest(w, req.RootMessageID, "root_message_id")
	if !ok {
		return
	}
	thread, created, terr := h.getOrCreateChatThread(r, parent, rootID, userID, workspaceID)
	if terr != nil {
		writeError(w, terr.status, terr.message)
		return
	}
	if created {
		h.publishChatThreadListChanged(workspaceID, userID, uuidToString(thread.ID))
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, chatSessionToResponse(thread))
}

func (h *Handler) getOrCreateChatThread(r *http.Request, parent db.ChatSession, rootID pgtype.UUID, userID, workspaceID string) (db.ChatSession, bool, *chatThreadError) {
	ctx := r.Context()
	if parent.ParentChatSessionID.Valid {
		return db.ChatSession{}, false, &chatThreadError{http.StatusBadRequest, "a thread cannot have threads"}
	}
	root, err := h.Queries.GetChatMessage(ctx, rootID)
	if err != nil || root.ChatSessionID != parent.ID || root.MessageKind == protocol.ChatMessageKindOnboardingKickoff {
		return db.ChatSession{}, false, &chatThreadError{http.StatusNotFound, "message not found"}
	}
	if existing, err := h.Queries.GetChatThreadByRoot(ctx, db.GetChatThreadByRootParams{
		RootChatMessageID: rootID, WorkspaceID: parent.WorkspaceID,
	}); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to load thread"}
	}

	if parent.Status == "archived" {
		return db.ChatSession{}, false, &chatThreadError{http.StatusBadRequest, "chat session is archived"}
	}
	bindings, err := h.Queries.ListChannelChatSessionBindingsBySessions(ctx, []pgtype.UUID{parent.ID})
	if err != nil {
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to load chat channel"}
	}
	if len(bindings) > 0 {
		return db.ChatSession{}, false, &chatThreadError{http.StatusBadRequest, "threads are not available in channel chats"}
	}
	agent, err := h.Queries.GetAgent(ctx, parent.AgentID)
	if err != nil {
		return db.ChatSession{}, false, &chatThreadError{http.StatusNotFound, "agent not found"}
	}
	if agent.ArchivedAt.Valid {
		return db.ChatSession{}, false, &chatThreadError{http.StatusBadRequest, "agent is archived"}
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(ctx, agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		return db.ChatSession{}, false, &chatThreadError{http.StatusForbidden, "you do not have access to this agent"}
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to start transaction"}
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, parent.WorkspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChatSession{}, false, &chatThreadError{http.StatusNotFound, "workspace not found"}
		}
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to lock workspace"}
	}
	thread, err := qtx.CreateChatThreadSession(ctx, db.CreateChatThreadSessionParams{
		ID:                  dbid.NewV7(),
		WorkspaceID:         parent.WorkspaceID,
		AgentID:             parent.AgentID,
		CreatorID:           parent.CreatorID,
		Title:               firstLineSummary(root.Content, chatThreadTitleMaxRunes),
		ProjectID:           parent.ProjectID,
		ParentChatSessionID: parent.ID,
		RootChatMessageID:   rootID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Lost the race to a concurrent open of the same message.
		tx.Rollback(ctx)
		existing, gerr := h.Queries.GetChatThreadByRoot(ctx, db.GetChatThreadByRootParams{
			RootChatMessageID: rootID, WorkspaceID: parent.WorkspaceID,
		})
		if gerr != nil {
			return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to load thread"}
		}
		return existing, false, nil
	}
	if err != nil {
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to create thread"}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChatSession{}, false, &chatThreadError{http.StatusInternalServerError, "failed to commit thread"}
	}
	return thread, true, nil
}

// publishChatThreadListChanged tells the creator's clients to refetch their
// session list, which carries threads for clients that render them.
func (h *Handler) publishChatThreadListChanged(workspaceID, creatorID, sessionID string) {
	h.publishChat(protocol.EventChatSessionCreated, workspaceID, "member", creatorID, sessionID, map[string]any{
		"workspace_id":    workspaceID,
		"chat_session_id": sessionID,
		"creator_id":      creatorID,
	})
}

type CreateIssueFromChatMessageRequest struct {
	Title string `json:"title"`
}

type ChatMessageIssueResponse struct {
	IssueID         string `json:"issue_id"`
	Identifier      string `json:"identifier"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	ChatMessageID   string `json:"chat_message_id"`
	ThreadSessionID string `json:"thread_session_id"`
}

type CreateIssueFromChatMessageResponse struct {
	Issue  IssueResponse            `json:"issue"`
	Link   ChatMessageIssueResponse `json:"link"`
	Thread ChatSessionResponse      `json:"thread"`
}

// CreateIssueFromChatMessage turns a message into an issue assigned to the
// conversation's agent. The issue is linked to the message's thread, and the
// thread gets a line now and on every later status move.
func (h *Handler) CreateIssueFromChatMessage(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	messageID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "messageId"), "message id")
	if !ok {
		return
	}
	var req CreateIssueFromChatMessageRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	message, err := h.Queries.GetChatMessage(r.Context(), messageID)
	if err != nil || message.ChatSessionID != session.ID || strings.TrimSpace(message.Content) == "" {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// A message inside a thread reports into that thread; a message in the
	// main conversation gets (or reuses) its own.
	thread := session
	if !session.ParentChatSessionID.Valid {
		var terr *chatThreadError
		var created bool
		thread, created, terr = h.getOrCreateChatThread(r, session, messageID, userID, workspaceID)
		if terr != nil {
			writeError(w, terr.status, terr.message)
			return
		}
		if created {
			h.publishChatThreadListChanged(workspaceID, userID, uuidToString(thread.ID))
		}
	}

	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	assigneeType := pgtype.Text{String: "agent", Valid: true}
	if code, msg := h.validateAssigneePair(r.Context(), r, workspaceID, assigneeType, agent.ID); code != 0 {
		writeError(w, code, msg)
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = firstLineSummary(message.Content, chatIssueTitleMaxRunes)
	}
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	status, ok := h.resolveIssueStatusKey(w, r, session.WorkspaceID, "todo")
	if !ok {
		return
	}
	prefix := h.getIssuePrefix(r.Context(), session.WorkspaceID)
	result, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID:  session.WorkspaceID,
		Title:        title,
		Description:  pgtype.Text{String: message.Content, Valid: true},
		Status:       status,
		Priority:     "none",
		AssigneeType: assigneeType,
		AssigneeID:   agent.ID,
		CreatorType:  "member",
		CreatorID:    parseUUID(userID),
		ProjectID:    session.ProjectID,
		// The message is the request; repeating a sentence is not a duplicate.
		AllowDuplicate: true,
	}, service.IssueCreateOpts{
		ActorID: userID,
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, labels []db.IssueLabel) map[string]any {
			response := issueToResponse(issue, prefix)
			labelResponses := labelsToResponse(labels)
			response.Labels = &labelResponses
			return map[string]any{"issue": response}
		},
	})
	if err != nil {
		slog.Error("create issue from chat message failed", "chat_session_id", uuidToString(session.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}
	link, err := h.Queries.CreateChatMessageIssue(r.Context(), db.CreateChatMessageIssueParams{
		IssueID:         result.Issue.ID,
		WorkspaceID:     session.WorkspaceID,
		ChatSessionID:   session.ID,
		ChatMessageID:   message.ID,
		ThreadSessionID: thread.ID,
		CreatedBy:       parseUUID(userID),
	})
	if err != nil {
		slog.Error("link chat message issue failed", "issue_id", uuidToString(result.Issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to link issue")
		return
	}
	response := issueToResponse(result.Issue, prefix)
	h.fillStatusCategory(r.Context(), session.WorkspaceID, &response)
	content := fmt.Sprintf("Created %s and assigned it to %s.", issueMentionMarkdown(response), agent.Name)
	if err := PostChatIssueEvent(r.Context(), h.Queries, h.Bus, link, content); err != nil {
		slog.Warn("post chat issue event failed", "issue_id", response.ID, "error", err)
	}
	writeJSON(w, http.StatusCreated, CreateIssueFromChatMessageResponse{
		Issue:  response,
		Link:   chatMessageIssueResponse(link.ChatMessageID, link.ThreadSessionID, response.ID, response.Identifier, response.Title, response.Status),
		Thread: chatSessionToResponse(thread),
	})
}

// ListChatMessageIssues lists the issues created from this conversation's
// messages, so each message can show what it became.
func (h *Handler) ListChatMessageIssues(w http.ResponseWriter, r *http.Request) {
	if !h.requireTeamPage(w, r) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, chi.URLParam(r, "sessionId"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListChatMessageIssuesBySession(r.Context(), db.ListChatMessageIssuesBySessionParams{
		ChatSessionID: session.ID, WorkspaceID: session.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat issues")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), session.WorkspaceID)
	out := make([]ChatMessageIssueResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, chatMessageIssueResponse(row.ChatMessageID, row.ThreadSessionID, uuidToString(row.IssueID),
			issueIdentifier(prefix, row.IssueNumber), row.IssueTitle, row.IssueStatus))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out})
}

func chatMessageIssueResponse(messageID, threadID pgtype.UUID, issueID, identifier, title, status string) ChatMessageIssueResponse {
	return ChatMessageIssueResponse{
		IssueID:         issueID,
		Identifier:      identifier,
		Title:           title,
		Status:          status,
		ChatMessageID:   uuidToString(messageID),
		ThreadSessionID: uuidToString(threadID),
	}
}

func issueIdentifier(prefix string, number int32) string {
	return fmt.Sprintf("%s-%d", prefix, number)
}

func issueMentionMarkdown(issue IssueResponse) string {
	return fmt.Sprintf("[%s](mention://issue/%s)", issue.Identifier, issue.ID)
}

// PostChatIssueEvent writes one issue line into the linked thread and tells
// the creator's clients. Shared by the create endpoint and the issue status
// listener.
func PostChatIssueEvent(ctx context.Context, queries *db.Queries, bus *events.Bus, link db.ChatMessageIssue, content string) error {
	msg, err := queries.CreateChatEventMessage(ctx, db.CreateChatEventMessageParams{
		ID:            dbid.NewV7(),
		ChatSessionID: link.ThreadSessionID,
		Content:       content,
		MessageKind:   protocol.ChatMessageKindIssueEvent,
	})
	if err != nil {
		return err
	}
	if err := queries.TouchChatSession(ctx, link.ThreadSessionID); err != nil {
		slog.Warn("touch chat thread after issue event failed", "chat_session_id", uuidToString(link.ThreadSessionID), "error", err)
	}
	if bus == nil {
		return nil
	}
	// Chat events reach only the session's owner; an event without one is
	// dropped by the realtime layer.
	thread, err := queries.GetChatSession(ctx, link.ThreadSessionID)
	if err != nil {
		return nil
	}
	workspaceID := uuidToString(thread.WorkspaceID)
	creatorID := uuidToString(thread.CreatorID)
	threadID := uuidToString(thread.ID)
	bus.Publish(events.Event{
		Type: protocol.EventChatMessage, WorkspaceID: workspaceID, ActorType: "system",
		ChatSessionID: threadID, ChatSessionOwnerID: creatorID,
		Payload: protocol.ChatMessagePayload{
			ChatSessionID: threadID,
			MessageID:     uuidToString(msg.ID),
			Role:          "assistant",
			Content:       msg.Content,
			CreatedAt:     timestampToString(msg.CreatedAt),
		},
	})
	bus.Publish(events.Event{
		Type: protocol.EventChatSessionCreated, WorkspaceID: workspaceID, ActorType: "member", ActorID: creatorID,
		ChatSessionID: threadID, ChatSessionOwnerID: creatorID,
		Payload: map[string]any{"workspace_id": workspaceID, "chat_session_id": threadID, "creator_id": creatorID},
	})
	return nil
}

// firstLineSummary is the first non-empty line of content with markdown
// markers dropped, capped at maxRunes.
func firstLineSummary(content string, maxRunes int) string {
	line := ""
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) != "" {
			line = l
			break
		}
	}
	line = strings.NewReplacer("#", "", "*", "", "`", "", ">", "", "~", "", "_", "").Replace(line)
	line = strings.Join(strings.Fields(line), " ")
	if runes := []rune(line); len(runes) > maxRunes {
		line = strings.TrimSpace(string(runes[:maxRunes-1])) + "…"
	}
	return line
}

// chatThreadRootContext frames a thread's first turn: the agent starts a fresh
// conversation for the thread, so it needs the message being replied to.
func chatThreadRootContext(root db.ChatMessage, reply string) string {
	quoted := strings.TrimSpace(root.Content)
	if runes := []rune(quoted); len(runes) > chatThreadRootContextRunes {
		quoted = string(runes[:chatThreadRootContextRunes]) + "…"
	}
	author := "the user"
	if root.Role == "assistant" {
		author = "you"
	}
	lines := strings.Split(quoted, "\n")
	for i, l := range lines {
		lines[i] = "> " + l
	}
	return fmt.Sprintf("This is a thread on an earlier message in your direct chat, written by %s:\n\n%s\n\nReply in this thread:\n\n%s",
		author, strings.Join(lines, "\n"), reply)
}
