package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------- V2 response shapes ----------

type ChannelMessageV2Response struct {
	ID          string                     `json:"id"`
	ChannelID   string                     `json:"channel_id"`
	WorkspaceID string                     `json:"workspace_id"`
	AuthorType  string                     `json:"author_type"`
	AuthorID    *string                    `json:"author_id"`
	AuthorName  *string                    `json:"author_name,omitempty"`
	Content     string                     `json:"content"`
	ReplyToID   *string                    `json:"reply_to_id,omitempty"`
	ReplyCount  int32                      `json:"reply_count"`
	CreatedAt   string                     `json:"created_at"`
	UpdatedAt   string                     `json:"updated_at"`
	AgentTasks  []ChannelAgentTaskResponse `json:"agent_tasks,omitempty"`
	Issues      []threadIssueResponse      `json:"issues,omitempty"`
}

type ChannelAgentTaskResponse struct {
	ID               string  `json:"id"`
	AgentID          string  `json:"agent_id"`
	RuntimeID        string  `json:"runtime_id"`
	IssueID          string  `json:"issue_id"`
	Status           string  `json:"status"`
	Priority         int32   `json:"priority"`
	Result           any     `json:"result"`
	Error            *string `json:"error"`
	FailureReason    string  `json:"failure_reason,omitempty"`
	TriggerSummary   *string `json:"trigger_summary,omitempty"`
	ChannelID        string  `json:"channel_id"`
	ChannelMessageID string  `json:"channel_message_id"`
	ChannelThreadID  string  `json:"channel_thread_id,omitempty"`
	ChannelReplyToID string  `json:"channel_reply_to_id,omitempty"`
	CreatedAt        string  `json:"created_at"`
	DispatchedAt     *string `json:"dispatched_at,omitempty"`
	StartedAt        *string `json:"started_at,omitempty"`
	CompletedAt      *string `json:"completed_at,omitempty"`
	Kind             string  `json:"kind"`
	AgentName        string  `json:"agent_name,omitempty"`
	// WorkDir is the daemon-reported absolute working directory, populated
	// once execution starts. RelativeWorkDir is its privacy-safe display form
	// (home prefix stripped) — the transcript dialog renders the workdir copy
	// button off RelativeWorkDir, so without these the channel scenario
	// silently drops the affordance that the issue scenario has. Mirrors
	// AgentTaskResponse in agent.go.
	WorkDir         string `json:"work_dir,omitempty"`
	RelativeWorkDir string `json:"relative_work_dir,omitempty"`
}

type ChannelContextResponse struct {
	Channel        ChannelResponse         `json:"channel"`
	Members        []ChannelMemberResponse `json:"members"`
	Messages       []ChannelContextMessage `json:"messages"`
	TriggerMessage *ChannelContextMessage  `json:"trigger_message,omitempty"`
	Replies        []ChannelContextMessage `json:"replies,omitempty"`
}

type ChannelContextMessage struct {
	ID         string  `json:"id"`
	AuthorType string  `json:"author_type"`
	AuthorID   *string `json:"author_id"`
	AuthorName *string `json:"author_name,omitempty"`
	Content    string  `json:"content"`
	CreatedAt  string  `json:"created_at"`
}

func channelMessageV2ToResponse(m db.ListChannelMessagesRow) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  m.ReplyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelMessageToV2Response(m db.ChannelMessage, replyCount int32) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  replyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelMessageLatestToResponse(m db.ListChannelMessagesLatestRow) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  m.ReplyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelMessagePaginatedToResponse(m db.ListChannelMessagesPaginatedRow) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  m.ReplyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelMessageAfterToResponse(m db.ListChannelMessagesAfterRow) ChannelMessageV2Response {
	return ChannelMessageV2Response{
		ID:          uuidToString(m.ID),
		ChannelID:   uuidToString(m.ChannelID),
		WorkspaceID: uuidToString(m.WorkspaceID),
		AuthorType:  m.AuthorType,
		AuthorID:    uuidToPtr(m.AuthorID),
		Content:     m.Content,
		ReplyToID:   uuidToPtr(m.ReplyToID),
		ReplyCount:  m.ReplyCount,
		CreatedAt:   timestampToString(m.CreatedAt),
		UpdatedAt:   timestampToString(m.UpdatedAt),
	}
}

func channelAgentTaskToResponse(t db.AgentTaskQueue, agents map[string]db.Agent, workspaceID string) ChannelAgentTaskResponse {
	failureReason := ""
	if t.FailureReason.Valid {
		failureReason = t.FailureReason.String
	}
	agentID := uuidToString(t.AgentID)
	agentName := ""
	if agent, ok := agents[agentID]; ok {
		agentName = agent.Name
	}
	workDir := ""
	if t.WorkDir.Valid {
		workDir = t.WorkDir.String
	}
	return ChannelAgentTaskResponse{
		ID:               uuidToString(t.ID),
		AgentID:          agentID,
		RuntimeID:        uuidToString(t.RuntimeID),
		IssueID:          uuidToString(t.IssueID),
		Status:           t.Status,
		Priority:         t.Priority,
		Result:           nil,
		Error:            textToPtr(t.Error),
		FailureReason:    failureReason,
		TriggerSummary:   textToPtr(t.TriggerSummary),
		ChannelID:        uuidToString(t.ChannelID),
		ChannelMessageID: uuidToString(t.ChannelMessageID),
		ChannelThreadID:  uuidToString(t.ChannelThreadID),
		ChannelReplyToID: uuidToString(t.ChannelReplyToID),
		CreatedAt:        timestampToString(t.CreatedAt),
		DispatchedAt:     timestampToPtr(t.DispatchedAt),
		StartedAt:        timestampToPtr(t.StartedAt),
		CompletedAt:      timestampToPtr(t.CompletedAt),
		Kind:             computeTaskKind(t),
		AgentName:        agentName,
		WorkDir:          workDir,
		RelativeWorkDir:  relativeWorkDir(workDir, workspaceID, uuidToString(t.ID)),
	}
}

func (h *Handler) attachChannelAgentTasks(ctx context.Context, messages []ChannelMessageV2Response) []ChannelMessageV2Response {
	if len(messages) == 0 {
		return messages
	}
	messageIDs := make([]pgtype.UUID, 0, len(messages))
	for _, msg := range messages {
		if id, err := parseUUIDErr(msg.ID); err == nil {
			messageIDs = append(messageIDs, id)
		}
	}
	if len(messageIDs) == 0 {
		return messages
	}
	tasks, err := h.Queries.ListChannelMentionTasksForMessages(ctx, messageIDs)
	if err != nil {
		return messages
	}
	// All messages in a channel share one workspace; relativeWorkDir needs
	// it to strip the home prefix. Derive from the first message rather than
	// threading a param through every caller.
	workspaceID := ""
	if len(messages) > 0 {
		workspaceID = messages[0].WorkspaceID
	}
	agentIDs := make(map[string]pgtype.UUID)
	for _, task := range tasks {
		agentIDs[uuidToString(task.AgentID)] = task.AgentID
	}
	agents := make(map[string]db.Agent, len(agentIDs))
	for id, uuid := range agentIDs {
		if agent, err := h.Queries.GetAgent(ctx, uuid); err == nil {
			agents[id] = agent
		}
	}
	byMessage := make(map[string][]ChannelAgentTaskResponse)
	for _, task := range tasks {
		msgID := uuidToString(task.ChannelMessageID)
		byMessage[msgID] = append(byMessage[msgID], channelAgentTaskToResponse(task, agents, workspaceID))
	}
	for i := range messages {
		messages[i].AgentTasks = byMessage[messages[i].ID]
	}
	return messages
}

// attachChannelMessageIssues links each top-level message to the issues that
// were produced from its thread (source_thread.root_message_id = message.id),
// so the channel timeline can render a linked-issue card directly on the
// message — the channel-side half of the OPE-1943 bidirectional display.
// Single batched query regardless of message count (no N+1). Best-effort: a
// query error leaves Issues empty rather than failing the whole list.
func (h *Handler) attachChannelMessageIssues(ctx context.Context, wsUUID pgtype.UUID, messages []ChannelMessageV2Response) []ChannelMessageV2Response {
	if len(messages) == 0 {
		return messages
	}
	messageIDs := make([]pgtype.UUID, 0, len(messages))
	for _, msg := range messages {
		if id, err := parseUUIDErr(msg.ID); err == nil {
			messageIDs = append(messageIDs, id)
		}
	}
	if len(messageIDs) == 0 {
		return messages
	}
	rows, err := h.Queries.ListIssuesByChannelMessages(ctx, messageIDs)
	if err != nil {
		return messages
	}
	issuePrefix := h.getIssuePrefix(ctx, wsUUID)
	byMessage := make(map[string][]threadIssueResponse)
	for _, row := range rows {
		rootID := uuidToString(row.RootMessageID)
		byMessage[rootID] = append(byMessage[rootID], threadIssueResponse{
			ID:         uuidToString(row.ID),
			Identifier: issuePrefix + "-" + strconv.Itoa(int(row.Number)),
			Number:     row.Number,
			Title:      row.Title,
			Status:     row.Status,
			Priority:   row.Priority,
		})
	}
	for i := range messages {
		messages[i].Issues = byMessage[messages[i].ID]
	}
	return messages
}

// attachChannelAuthorNames resolves the display name for each message author
// so the UI can render a real Member/Agent name instead of the bare
// author_type ("member"/"agent"). Lookups are deduplicated per distinct author
// and skipped for system messages (no author_id). Names that can't be resolved
// (deleted user/agent) are left nil, and the client keeps its own fallback.
func (h *Handler) attachChannelAuthorNames(ctx context.Context, messages []ChannelMessageV2Response) []ChannelMessageV2Response {
	if len(messages) == 0 {
		return messages
	}
	// Cache by "type:id" so a chatty author is resolved once per request.
	names := make(map[string]string)
	for i := range messages {
		m := &messages[i]
		if m.AuthorID == nil || *m.AuthorID == "" {
			continue
		}
		key := m.AuthorType + ":" + *m.AuthorID
		name, seen := names[key]
		if !seen {
			name = h.resolveChannelActorName(ctx, m.AuthorType, *m.AuthorID)
			names[key] = name
		}
		if name != "" {
			n := name
			m.AuthorName = &n
		}
	}
	return messages
}

func channelContextRowToMessage(m db.GetChannelContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func channelMessageForContextToMessage(m db.GetChannelMessageForContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

func channelReplyForContextToMessage(m db.ListChannelMessageRepliesForContextRow) ChannelContextMessage {
	return ChannelContextMessage{
		ID:         uuidToString(m.ID),
		AuthorType: m.AuthorType,
		AuthorID:   uuidToPtr(m.AuthorID),
		AuthorName: textToPtr(m.AuthorName),
		Content:    m.Content,
		CreatedAt:  timestampToString(m.CreatedAt),
	}
}

// ---------- V2 handlers ----------

// ListChannelMessages lists top-level messages in a channel (flat timeline).
// Supports pagination: ?limit=N (default 20) returns the latest N messages.
// ?before=<RFC3339> loads older messages before that timestamp.
// ?around=<messageUUID> deep-links: loads a window centered on the target
// message (half older + target + half newer) so the client can scroll to it
// even when it falls outside the latest page. has_more reflects older
// history so upward infinite-scroll keeps working from there.
func (h *Handler) ListChannelMessages(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	limitStr := r.URL.Query().Get("limit")
	beforeStr := r.URL.Query().Get("before")
	aroundStr := r.URL.Query().Get("around")

	limit := int32(20)
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = int32(v)
		}
	}

	var resp []ChannelMessageV2Response
	var hasMore bool
	loaded := false
	// highlight is set when ?around targets a reply: the window is centered on
	// the reply's thread root (so the root lands in the top-level list), and
	// these fields tell the client which thread to auto-expand and which reply
	// to scroll-highlight. Empty for top-level targets (the client highlights
	// the ?message id directly).
	var highlight *channelMessageHighlight

	// ?around=<id> deep-links to a window centered on the target message. A
	// malformed id is a 400; a well-formed id that no longer exists or belongs
	// to another channel silently falls back to the latest page — a stale
	// shared link should still open the channel, not white-screen it. A reply
	// target is resolved to its thread root so the root message is the window
	// center (replies never appear in the top-level list); the original reply
	// id is echoed back via `highlight` so the client can expand the thread and
	// scroll to it.
	if aroundStr != "" {
		targetID, parseOK := parseUUIDOrBadRequest(w, aroundStr, "around message id")
		if !parseOK {
			return
		}
		if target, err := h.Queries.GetChannelMessage(r.Context(), targetID); err == nil &&
			target.ChannelID == cctx.channel.ID {
			center := target
			if target.ThreadID.Valid {
				// Reply target — re-anchor on its thread root message.
				thread, terr := h.Queries.GetChannelThread(r.Context(), target.ThreadID)
				if terr == nil && thread.RootMessageID.Valid {
					if root, rerr := h.Queries.GetChannelMessage(r.Context(), thread.RootMessageID); rerr == nil &&
						root.ChannelID == cctx.channel.ID {
						center = root
						highlight = &channelMessageHighlight{
							RootMessageID: uuidToString(root.ID),
							ThreadID:      uuidToString(thread.ID),
							MessageID:     uuidToString(target.ID),
						}
					}
				}
			}
			if !center.ThreadID.Valid {
				window, more, err := h.loadAroundWindow(r.Context(), cctx, center, limit)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "failed to list messages")
					return
				}
				resp, hasMore, loaded = window, more, true
			}
		}
	}

	if !loaded && beforeStr != "" {
		beforeTime, err := time.Parse(time.RFC3339Nano, beforeStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before timestamp")
			return
		}
		messages, err := h.Queries.ListChannelMessagesPaginated(r.Context(), db.ListChannelMessagesPaginatedParams{
			ChannelID: cctx.channel.ID,
			CreatedAt: pgtype.Timestamptz{Time: beforeTime, Valid: true},
			Limit:     limit + 1,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list messages")
			return
		}
		if len(messages) > int(limit) {
			hasMore = true
			messages = messages[:limit]
		}
		resp = make([]ChannelMessageV2Response, len(messages))
		for i, m := range messages {
			resp[i] = channelMessagePaginatedToResponse(m)
		}
		slices.Reverse(resp)
		loaded = true
	}

	if !loaded {
		messages, err := h.Queries.ListChannelMessagesLatest(r.Context(), db.ListChannelMessagesLatestParams{
			ChannelID: cctx.channel.ID,
			Limit:     limit + 1,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list messages")
			return
		}
		if len(messages) > int(limit) {
			hasMore = true
			messages = messages[:limit]
		}
		resp = make([]ChannelMessageV2Response, len(messages))
		for i, m := range messages {
			resp[i] = channelMessageLatestToResponse(m)
		}
		slices.Reverse(resp)
	}

	resp = h.attachChannelAgentTasks(r.Context(), resp)
	resp = h.attachChannelAuthorNames(r.Context(), resp)
	resp = h.attachChannelMessageIssues(r.Context(), wsUUID, resp)
	response := map[string]any{"messages": resp, "total": len(resp), "has_more": hasMore}
	if highlight != nil {
		response["highlight"] = highlight
	}
	writeJSON(w, http.StatusOK, response)
}

// channelMessageHighlight is returned by ListChannelMessages when ?around
// targets a reply: the client centers the window on root_message_id (which
// lives in the top-level list), auto-expands thread_id, and scroll-highlights
// message_id (the original reply).
type channelMessageHighlight struct {
	RootMessageID string `json:"root_message_id"`
	ThreadID      string `json:"thread_id"`
	MessageID     string `json:"message_id"`
}

// loadAroundWindow loads a deep-link window centered on a target message: up
// to `half` older top-level messages, the target itself, and up to `half`
// newer ones — returned in ASC display order. hasMore is true when older
// history exists beyond the loaded window so upward infinite-scroll keeps
// working. The caller has already validated that target is a top-level
// message in cctx.channel.
func (h *Handler) loadAroundWindow(ctx context.Context, cctx channelContext, target db.ChannelMessage, limit int32) ([]ChannelMessageV2Response, bool, error) {
	half := limit / 2
	if half < 1 {
		half = 1
	}

	// Older side: created_at < target, DESC. Request half+1 to detect whether
	// older history exists beyond what we return (mirrors the latest/before
	// branches' limit+1 trick).
	older, err := h.Queries.ListChannelMessagesPaginated(ctx, db.ListChannelMessagesPaginatedParams{
		ChannelID: cctx.channel.ID,
		CreatedAt: target.CreatedAt,
		Limit:     half + 1,
	})
	if err != nil {
		return nil, false, err
	}
	hasMore := int32(len(older)) > half
	if hasMore {
		older = older[:half]
	}

	// Newer side: created_at > target, ASC.
	newer, err := h.Queries.ListChannelMessagesAfter(ctx, db.ListChannelMessagesAfterParams{
		ChannelID: cctx.channel.ID,
		CreatedAt: target.CreatedAt,
		Limit:     half,
	})
	if err != nil {
		return nil, false, err
	}

	replyCount, err := h.Queries.CountMessageReplies(ctx, target.ID)
	if err != nil {
		replyCount = 0
	}

	// Assemble ASC order: older (reversed to ASC) + target + newer.
	resp := make([]ChannelMessageV2Response, 0, len(older)+1+len(newer))
	for i := len(older) - 1; i >= 0; i-- {
		resp = append(resp, channelMessagePaginatedToResponse(older[i]))
	}
	resp = append(resp, channelMessageToV2Response(target, replyCount))
	for _, m := range newer {
		resp = append(resp, channelMessageAfterToResponse(m))
	}
	return resp, hasMore, nil
}

// SendChannelMessage posts a top-level message to a channel (no thread required).
func (h *Handler) SendChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canPost() {
		writeError(w, http.StatusForbidden, "you cannot post in this channel")
		return
	}
	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	var authorID pgtype.UUID
	if actorID != "" {
		if id, err := parseUUIDErr(actorID); err == nil {
			authorID = id
		}
	}
	authorType := actorType
	if authorType != "agent" {
		authorType = "member"
	}
	msg, err := h.Queries.CreateChannelMessageTopLevel(r.Context(), db.CreateChannelMessageTopLevelParams{
		ChannelID:   cctx.channel.ID,
		WorkspaceID: wsUUID,
		AuthorType:  authorType,
		Content:     content,
		AuthorID:    authorID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send message")
		return
	}
	h.Queries.TouchChannel(r.Context(), cctx.channel.ID)
	resp := channelMessageToV2Response(msg, 0)
	if name := h.resolveChannelActorName(r.Context(), actorType, actorID); name != "" {
		resp.AuthorName = &name
	}
	h.publish(protocol.EventChannelMessageCreated, workspaceID, actorType, actorID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	h.processChannelMessageMentions(r.Context(), cctx.channel, msg, actorType, actorID)
	writeJSON(w, http.StatusCreated, resp)
}

// ReplyToMessage replies to a message, auto-creating a thread if needed.
func (h *Handler) ReplyToMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canPost() {
		writeError(w, http.StatusForbidden, "you cannot post in this channel")
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Verify target message exists and belongs to this channel.
	targetMsg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(targetMsg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	var req CreateChannelMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	var authorID pgtype.UUID
	if actorID != "" {
		if id, err := parseUUIDErr(actorID); err == nil {
			authorID = id
		}
	}
	authorType := actorType
	if authorType != "agent" {
		authorType = "member"
	}
	// channel_thread.created_by REFERENCES "user"(id); agents live in the
	// agent table, so stamping an agent UUID here violates the FK and the
	// reply fails with a generic "failed to create reply thread" (this is the
	// @-mention-then-agent-replies path). Mirror the CreateThread handler:
	// agents get a NULL created_by; members keep their user UUID. authorID is
	// still used below for the reply message's author_id, which is NOT a FK.
	threadCreatedBy := authorID
	if authorType == "agent" {
		threadCreatedBy = pgtype.UUID{}
	}

	// Find or create thread for this message.
	thread, err := h.Queries.GetThreadByRootMessage(r.Context(), msgUUID)
	if err != nil {
		// Thread doesn't exist yet — create one.
		title := truncateUTF8(targetMsg.Content, 50)
		thread, err = h.Queries.CreateChannelThread(r.Context(), db.CreateChannelThreadParams{
			ChannelID:     cctx.channel.ID,
			WorkspaceID:   wsUUID,
			Title:         title,
			CreatedBy:     threadCreatedBy,
			RootMessageID: msgUUID,
		})
		if err != nil {
			slog.Error("reply: create channel thread failed",
				"channel_id", uuidToString(cctx.channel.ID),
				"root_message_id", msgID, "author_id", actorID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create reply thread")
			return
		}
		h.publish(protocol.EventChannelThreadCreated, workspaceID, actorType, actorID, map[string]any{
			"thread":     channelThreadToResponse(thread),
			"channel_id": uuidToString(cctx.channel.ID),
		})
	}

	// Insert the reply message.
	reply, err := h.Queries.CreateChannelMessageReply(r.Context(), db.CreateChannelMessageReplyParams{
		ThreadID:    thread.ID,
		ChannelID:   cctx.channel.ID,
		WorkspaceID: wsUUID,
		AuthorType:  authorType,
		Content:     content,
		ReplyToID:   msgUUID,
		AuthorID:    authorID,
	})
	if err != nil {
		slog.Error("reply: insert channel reply failed",
			"channel_id", uuidToString(cctx.channel.ID),
			"thread_id", uuidToString(thread.ID),
			"reply_to_id", msgID, "author_type", authorType, "author_id", actorID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to post reply")
		return
	}
	h.Queries.BumpChannelThread(r.Context(), thread.ID)
	h.Queries.TouchChannel(r.Context(), cctx.channel.ID)

	resp := channelMessageToV2Response(reply, 0)
	if name := h.resolveChannelActorName(r.Context(), actorType, actorID); name != "" {
		resp.AuthorName = &name
	}
	h.publish(protocol.EventChannelMessageCreated, workspaceID, actorType, actorID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
		"thread_id":  uuidToString(thread.ID),
		"reply_to":   msgID,
	})
	h.processChannelMessageMentions(r.Context(), cctx.channel, reply, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"message": resp,
		"thread":  channelThreadToResponse(thread),
	})
}

// GetMessageThread returns the thread (replies) for a specific message.
func (h *Handler) GetMessageThread(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Get the root message.
	rootMsg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(rootMsg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Get the thread (may not exist yet).
	thread, err := h.Queries.GetThreadByRootMessage(r.Context(), msgUUID)
	if err != nil {
		// No thread yet — return just the root message with no replies.
		rootResp := h.attachChannelAgentTasks(r.Context(), []ChannelMessageV2Response{channelMessageToV2Response(rootMsg, 0)})
		rootResp = h.attachChannelAuthorNames(r.Context(), rootResp)
		writeJSON(w, http.StatusOK, map[string]any{
			"root_message": rootResp[0],
			"replies":      []ChannelMessageV2Response{},
			"thread":       nil,
		})
		return
	}

	// List replies in the thread.
	replies, err := h.Queries.ListMessageReplies(r.Context(), msgUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list replies")
		return
	}
	replyResp := make([]ChannelMessageV2Response, len(replies))
	for i, m := range replies {
		replyResp[i] = channelMessageToV2Response(m, 0)
	}
	rootResp := h.attachChannelAgentTasks(r.Context(), []ChannelMessageV2Response{channelMessageToV2Response(rootMsg, int32(len(replies)))})
	replyResp = h.attachChannelAgentTasks(r.Context(), replyResp)
	rootResp = h.attachChannelAuthorNames(r.Context(), rootResp)
	replyResp = h.attachChannelAuthorNames(r.Context(), replyResp)
	writeJSON(w, http.StatusOK, map[string]any{
		"root_message": rootResp[0],
		"replies":      replyResp,
		"thread":       channelThreadToResponse(thread),
	})
}

// RemoveChannelMember removes a member from a channel (by manager).
func (h *Handler) RemoveChannelMember(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !cctx.canManage() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	targetID := chi.URLParam(r, "userId")
	targetUUID, ok := parseUUIDOrBadRequest(w, targetID, "user id")
	if !ok {
		return
	}
	if err := h.Queries.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{
		ChannelID: cctx.channel.ID,
		UserID:    targetUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	h.publish(protocol.EventChannelMemberLeft, workspaceID, "member", requestUserID(r), map[string]any{
		"channel_id": uuidToString(cctx.channel.ID),
		"user_id":    targetID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"removed": true, "channel_id": uuidToString(cctx.channel.ID), "user_id": targetID})
}

// ConvertMessageToIssue converts a channel message into an issue.
// If the message does not already have a thread, one is created implicitly so
// that the spec invariant "Issue is produced from a thread" always holds.
func (h *Handler) ConvertMessageToIssue(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}

	// Get the message.
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Parse optional overrides from request body.
	var req struct {
		Title       *string `json:"title"`
		Description *string `json:"description"`
		ProjectID   *string `json:"project_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Auto-fill title from message content.
	title := truncateUTF8(msg.Content, 80)
	if req.Title != nil && *req.Title != "" {
		title = *req.Title
	}

	// Description: full message content + source annotation.
	description := msg.Content + "\n\n---\n_来源：频道消息_"
	if req.Description != nil && *req.Description != "" {
		description = *req.Description
	}

	// Determine the creator user ID.
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	// Use transaction to safely create the issue with a number.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
		return
	}

	var projectID pgtype.UUID
	if req.ProjectID != nil && *req.ProjectID != "" {
		if pid, perr := parseUUIDErr(*req.ProjectID); perr == nil {
			projectID = pid
		}
	}

	issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
		WorkspaceID: wsUUID,
		Title:       title,
		Description: pgtype.Text{String: description, Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userUUID,
		Number:      issueNumber,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	// Ensure thread exists: if no thread yet, create one implicitly.
	thread, terr := qtx.GetThreadByRootMessage(r.Context(), msgUUID)
	if terr != nil {
		// No thread yet — create one anchored to this message.
		threadTitle := truncateUTF8(msg.Content, 50)
		thread, err = qtx.CreateChannelThread(r.Context(), db.CreateChannelThreadParams{
			ChannelID:     cctx.channel.ID,
			WorkspaceID:   wsUUID,
			Title:         threadTitle,
			CreatedBy:     userUUID,
			RootMessageID: msgUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create thread for issue")
			return
		}
	}

	// Link the issue to the source channel and thread.
	qtx.LinkIssueSource(r.Context(), db.LinkIssueSourceParams{
		ID:              issue.ID,
		SourceChannelID: cctx.channel.ID,
		SourceThreadID:  thread.ID,
	})

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	// Post "created from thread" system activity (best-effort, outside tx).
	issue.SourceChannelID = cctx.channel.ID
	issue.SourceThreadID = thread.ID
	h.linkIssueToThreadActivity(r.Context(), &issue, thread)

	writeJSON(w, http.StatusCreated, map[string]any{
		"issue_id":     uuidToString(issue.ID),
		"issue_number": issue.Number,
		"title":        issue.Title,
		"thread_id":    uuidToString(thread.ID),
	})
}

// GetChannelContext returns channel info, members, and recent messages for agent injection.
func (h *Handler) GetChannelContext(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	recent := int32(20)
	if v := r.URL.Query().Get("recent"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			recent = int32(n)
		}
	}
	messageID := strings.TrimSpace(r.URL.Query().Get("message"))
	includeReplies := r.URL.Query().Get("include_replies") == "true" || r.URL.Query().Get("include-replies") == "true"

	// Get members.
	members, err := h.Queries.ListChannelMembers(r.Context(), cctx.channel.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	memberResp := make([]ChannelMemberResponse, len(members))
	for i, m := range members {
		memberResp[i] = channelMemberRowToResponse(m)
	}

	// Get recent messages.
	msgs, err := h.Queries.GetChannelContext(r.Context(), db.GetChannelContextParams{
		ChannelID: cctx.channel.ID,
		Limit:     recent,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get context messages")
		return
	}
	// Reverse to chronological order (query returns DESC).
	msgResp := make([]ChannelContextMessage, len(msgs))
	for i, m := range msgs {
		msgResp[len(msgs)-1-i] = channelContextRowToMessage(m)
	}

	resp := ChannelContextResponse{
		Channel:  channelToResponse(cctx.channel),
		Members:  memberResp,
		Messages: msgResp,
	}
	if messageID != "" {
		msgUUID, ok := parseUUIDOrBadRequest(w, messageID, "message id")
		if !ok {
			return
		}
		trigger, err := h.Queries.GetChannelMessageForContext(r.Context(), db.GetChannelMessageForContextParams{
			ChannelID: cctx.channel.ID,
			ID:        msgUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "message not found")
			return
		}
		triggerResp := channelMessageForContextToMessage(trigger)
		resp.TriggerMessage = &triggerResp
		if includeReplies {
			replies, err := h.Queries.ListChannelMessageRepliesForContext(r.Context(), db.ListChannelMessageRepliesForContextParams{
				ChannelID: cctx.channel.ID,
				ID:        msgUUID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to get message replies")
				return
			}
			resp.Replies = make([]ChannelContextMessage, len(replies))
			for i, reply := range replies {
				resp.Replies[i] = channelReplyForContextToMessage(reply)
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- Message Update / Delete ----------

// UpdateChannelMessage updates a message's content. Only the original author
// or a channel manager may update a message.
func (h *Handler) UpdateChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	// Permission: only the author or a channel manager can update.
	userID := requestUserID(r)
	isAuthor := msg.AuthorID.Valid && uuidToString(msg.AuthorID) == userID
	if !isAuthor && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "you can only edit your own messages")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	updated, err := h.Queries.UpdateChannelMessage(r.Context(), db.UpdateChannelMessageParams{
		ID:      msgUUID,
		Content: content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update message")
		return
	}
	resp := channelMessageToV2Response(updated, 0)
	if name := h.resolveChannelActorName(r.Context(), updated.AuthorType, uuidToString(updated.AuthorID)); name != "" {
		resp.AuthorName = &name
	}
	h.publish(protocol.EventChannelMessageUpdated, workspaceID, "member", userID, map[string]any{
		"message":    resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteChannelMessage deletes a message. Only the original author or a channel
// manager may delete a message.
func (h *Handler) DeleteChannelMessage(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	msgID := chi.URLParam(r, "msgId")
	msgUUID, ok := parseUUIDOrBadRequest(w, msgID, "message id")
	if !ok {
		return
	}
	msg, err := h.Queries.GetChannelMessage(r.Context(), msgUUID)
	if err != nil || uuidToString(msg.ChannelID) != uuidToString(cctx.channel.ID) {
		writeError(w, http.StatusNotFound, "message not found")
		return
	}

	userID := requestUserID(r)
	isAuthor := msg.AuthorID.Valid && uuidToString(msg.AuthorID) == userID
	if !isAuthor && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "you can only delete your own messages")
		return
	}

	if err := h.Queries.DeleteChannelMessage(r.Context(), msgUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	h.publish(protocol.EventChannelMessageDeleted, workspaceID, "member", userID, map[string]any{
		"message_id": msgID,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "message_id": msgID})
}

// ---------- Thread Get / Update ----------

// GetChannelThread returns a single thread by ID.
func (h *Handler) GetChannelThreadByID(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadInChannel(w, r, cctx.channel.ID)
	if !ok {
		return
	}
	issues, _ := h.Queries.ListThreadIssues(r.Context(), thread.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"thread": channelThreadToResponse(thread),
		"issues": threadIssuesToResponse(issues, h.getIssuePrefix(r.Context(), wsUUID)),
	})
}

// UpdateChannelThread updates a thread's title.
func (h *Handler) UpdateChannelThread(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	cctx, ok := h.loadChannelContext(w, r, wsUUID, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	thread, ok := h.loadThreadInChannel(w, r, cctx.channel.ID)
	if !ok {
		return
	}

	// Permission: thread creator or channel manager.
	userID := requestUserID(r)
	isCreator := thread.CreatedBy.Valid && uuidToString(thread.CreatedBy) == userID
	if !isCreator && !cctx.canManage() {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	updated, err := h.Queries.UpdateChannelThreadTitle(r.Context(), db.UpdateChannelThreadTitleParams{
		ID:    thread.ID,
		Title: title,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update thread")
		return
	}
	resp := channelThreadToResponse(updated)
	h.publish(protocol.EventChannelThreadUpdated, workspaceID, "member", userID, map[string]any{
		"thread":     resp,
		"channel_id": uuidToString(cctx.channel.ID),
	})
	writeJSON(w, http.StatusOK, resp)
}
