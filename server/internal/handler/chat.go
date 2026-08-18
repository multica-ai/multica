package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// chatSessionTitleMaxLen caps the rename input. Long enough to fit a
// meaningful summary, short enough to keep the dropdown row scannable.
const chatSessionTitleMaxLen = 200

// ---------------------------------------------------------------------------
// Chat Sessions
// ---------------------------------------------------------------------------

type CreateChatSessionRequest struct {
	AgentID   string  `json:"agent_id"`
	Title     string  `json:"title"`
	ProjectID *string `json:"project_id"`
}

func (h *Handler) CreateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	projectID := pgtype.UUID{Valid: false}
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		projectID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(*req.ProjectID), "project_id")
		if !ok {
			return
		}
	}

	// Verify agent exists in workspace.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	// Invocation gate: starting a chat produces agent runs, so it uses the
	// invoke permission (MUL-3963), not the softer view gate. Agent-to-agent
	// chat sessions are judged by the top-of-chain originator.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}

	// Create inside a tx that first takes a FOR KEY SHARE lock on the workspace
	// row: it conflicts with DeleteWorkspace's FOR UPDATE, so a session cannot be
	// created into a workspace whose delete is in progress and then be orphaned by
	// the finalizer after the delete's sweep (#5219 create/delete protocol; see
	// LockWorkspaceForChatSessionCreate).
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), workspaceUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workspace")
		return
	}
	if projectID.Valid {
		if _, err := qtx.LockProjectForChatSessionCreate(r.Context(), db.LockProjectForChatSessionCreateParams{
			ID:          projectID,
			WorkspaceID: workspaceUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "project not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to lock project")
			return
		}
	}

	session, err := qtx.CreateChatSession(r.Context(), db.CreateChatSessionParams{
		WorkspaceID: workspaceUUID,
		AgentID:     agentID,
		CreatorID:   parseUUID(userID),
		Title:       req.Title,
		ProjectID:   projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit chat session create")
		return
	}

	writeJSON(w, http.StatusCreated, chatSessionToResponse(session))
}

func (h *Handler) ListChatSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	// Compute the accessible-agents set once and use it to drop sessions
	// whose target agent the caller no longer has access to — without this,
	// a member whose role was downgraded would still see the session list
	// (and transcripts via ListChatMessages) for any private agent they
	// previously had access to. Falls back to the user's role from the
	// workspace member context.
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	status := r.URL.Query().Get("status")

	// Two call sites → two row types with identical shape. Collect into a
	// common response slice via small per-branch loops.
	var resp []ChatSessionResponse
	if status == "all" {
		rows, err := h.Queries.ListAllChatSessionsByCreator(r.Context(), db.ListAllChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
			return
		}
		resp = make([]ChatSessionResponse, 0, len(rows))
		for _, s := range rows {
			if _, ok := allowed[uuidToString(s.AgentID)]; !ok {
				continue
			}
			resp = append(resp, ChatSessionResponse{
				ID:          uuidToString(s.ID),
				WorkspaceID: uuidToString(s.WorkspaceID),
				AgentID:     uuidToString(s.AgentID),
				CreatorID:   uuidToString(s.CreatorID),
				ProjectID:   uuidToPtr(s.ProjectID),
				Title:       s.Title,
				Status:      s.Status,
				HasUnread:   s.UnreadCount > 0,
				UnreadCount: int(s.UnreadCount),
				LastMessage: buildChatLastMessage(s.LastMessageAt, s.LastMessageContent, s.LastMessageRole, s.LastMessageFailureReason, s.LastMessageKind),
				Pinned:      s.PinnedAt.Valid,
				CreatedAt:   timestampToString(s.CreatedAt),
				UpdatedAt:   timestampToString(s.UpdatedAt),
			})
		}
	} else {
		rows, err := h.Queries.ListChatSessionsByCreator(r.Context(), db.ListChatSessionsByCreatorParams{
			WorkspaceID: parseUUID(workspaceID),
			CreatorID:   parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
			return
		}
		resp = make([]ChatSessionResponse, 0, len(rows))
		for _, s := range rows {
			if _, ok := allowed[uuidToString(s.AgentID)]; !ok {
				continue
			}
			resp = append(resp, ChatSessionResponse{
				ID:          uuidToString(s.ID),
				WorkspaceID: uuidToString(s.WorkspaceID),
				AgentID:     uuidToString(s.AgentID),
				CreatorID:   uuidToString(s.CreatorID),
				ProjectID:   uuidToPtr(s.ProjectID),
				Title:       s.Title,
				Status:      s.Status,
				HasUnread:   s.UnreadCount > 0,
				UnreadCount: int(s.UnreadCount),
				LastMessage: buildChatLastMessage(s.LastMessageAt, s.LastMessageContent, s.LastMessageRole, s.LastMessageFailureReason, s.LastMessageKind),
				Pinned:      s.PinnedAt.Valid,
				CreatedAt:   timestampToString(s.CreatedAt),
				UpdatedAt:   timestampToString(s.UpdatedAt),
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) loadChatSessionForUser(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionID, "chat session id")
	if !ok {
		return db.ChatSession{}, false
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.ChatSession{}, false
	}
	session, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
		ID:          sessionUUID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return db.ChatSession{}, false
	}
	if uuidToString(session.CreatorID) != userID {
		writeError(w, http.StatusForbidden, "not your chat session")
		return db.ChatSession{}, false
	}
	return session, true
}

// gateChatSessionForUser combines the session ownership check with the
// private-agent access gate so a member who has lost access to the target
// agent (role downgrade, ownership transfer, agent flipped to private)
// cannot continue reading the chat transcript even though they remain the
// session creator. Returns ok=false after writing the error response.
func (h *Handler) gateChatSessionForUser(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return db.ChatSession{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return db.ChatSession{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return db.ChatSession{}, false
	}
	return session, true
}

// gatePublicChatSessionForUser adds the member-visible projection boundary to
// the normal ownership and private-agent checks. Internal channel commands stay
// durable, but a session containing only those commands is not a public Chat.
// Cleanup and recovery operations keep using the lower-level ownership gates
// so an empty bound session can still be archived or deleted and a member's
// own cancelled draft is never stranded. Every ordinary Chat interaction uses
// this gate so a cached hidden session ID cannot resurrect a command-only Chat.
func (h *Handler) gatePublicChatSessionForUser(w http.ResponseWriter, r *http.Request, userID, workspaceID, sessionID string) (db.ChatSession, bool) {
	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return db.ChatSession{}, false
	}
	if _, err := h.Queries.GetPublicChatSessionInWorkspace(r.Context(), db.GetPublicChatSessionInWorkspaceParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "chat session not found")
		return db.ChatSession{}, false
	}
	return session, true
}

func (h *Handler) GetChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, chatSessionToResponse(session))
}

type UpdateChatSessionRequest struct {
	Title     *string         `json:"title"`
	ProjectID json.RawMessage `json:"project_id"`
}

// UpdateChatSession updates one user-editable field on a chat session. Title
// is surfaced by inline rename; project_id controls the project context used
// by subsequent turns. Status and pinned keep their dedicated endpoints,
// agent/creator/workspace are immutable, and the resume pointers
// (session_id / work_dir / runtime_id) remain daemon-owned.
func (h *Handler) UpdateChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req UpdateChatSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	hasTitle := req.Title != nil
	hasProjectID := req.ProjectID != nil
	if hasTitle == hasProjectID {
		writeError(w, http.StatusBadRequest, "exactly one of title or project_id is required")
		return
	}

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	var (
		updated db.ChatSession
		err     error
	)
	var projectIDChanged bool
	if hasTitle {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title is required")
			return
		}
		if len([]rune(title)) > chatSessionTitleMaxLen {
			writeError(w, http.StatusBadRequest, "title is too long")
			return
		}
		updated, err = h.Queries.UpdateChatSessionTitle(r.Context(), db.UpdateChatSessionTitleParams{
			ID:    session.ID,
			Title: title,
		})
	} else {
		projectID := pgtype.UUID{Valid: false}
		if string(req.ProjectID) != "null" {
			var rawProjectID string
			if err := json.Unmarshal(req.ProjectID, &rawProjectID); err != nil {
				writeError(w, http.StatusBadRequest, "project_id must be a UUID or null")
				return
			}
			rawProjectID = strings.TrimSpace(rawProjectID)
			if rawProjectID == "" {
				writeError(w, http.StatusBadRequest, "project_id must be a UUID or null")
				return
			}
			projectID, ok = parseUUIDOrBadRequest(w, rawProjectID, "project_id")
			if !ok {
				return
			}
		}

		tx, txErr := h.TxStarter.Begin(r.Context())
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to start transaction")
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.Queries.WithTx(tx)

		if projectID.Valid {
			if _, lockErr := qtx.LockProjectForChatSessionCreate(r.Context(), db.LockProjectForChatSessionCreateParams{
				ID:          projectID,
				WorkspaceID: session.WorkspaceID,
			}); lockErr != nil {
				if errors.Is(lockErr, pgx.ErrNoRows) {
					writeError(w, http.StatusNotFound, "project not found")
					return
				}
				writeError(w, http.StatusInternalServerError, "failed to lock project")
				return
			}
		}

		updated, err = qtx.UpdateChatSessionProject(r.Context(), db.UpdateChatSessionProjectParams{
			ID:          session.ID,
			WorkspaceID: session.WorkspaceID,
			ProjectID:   projectID,
		})
		if err == nil {
			err = tx.Commit(r.Context())
		}
		projectIDChanged = true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	resolvedSessionID := uuidToString(updated.ID)
	payload := protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	}
	if projectIDChanged {
		projectID := uuidToPtr(updated.ProjectID)
		payload.ProjectID = &projectID
	}
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, payload)

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

type SetChatSessionPinnedRequest struct {
	Pinned bool `json:"pinned"`
}

// SetChatSessionPinned pins or unpins a chat so it sticks to the top of the
// caller's conversation list. Pin state is per-session and, since sessions are
// per-creator, inherently per-user. It never bumps updated_at (see the SQL) so
// an unpinned chat does not jump the activity-sorted list.
func (h *Handler) SetChatSessionPinned(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SetChatSessionPinnedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	updated, err := h.Queries.SetChatSessionPinned(r.Context(), db.SetChatSessionPinnedParams{
		ID:     session.ID,
		Pinned: req.Pinned,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	resolvedSessionID := uuidToString(updated.ID)
	pinned := updated.PinnedAt.Valid
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		Pinned:        &pinned,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	})

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

type SetChatSessionArchivedRequest struct {
	Archived bool `json:"archived"`
}

// SetChatSessionArchived archives or unarchives a chat session owned by the
// caller. Archiving is the reversible sibling of delete: the row stays in the
// user's history (behind the "Archived" entry) and the conversation becomes
// read-only — SendChatMessage refuses status='archived'. Hard delete is only
// offered from the archived list, so nothing is destroyed in one hover click.
//
// Archiving also severs any external-channel binding. The web send path already
// treats status='archived' as read-only, but the channel engine (Feishu/Slack)
// resolves inbound traffic through channel_chat_session_binding without checking
// session status, so a bound session kept accumulating agent replies — and a
// stuck unread badge — after the user archived it (MUL-4372). Dropping the
// binding in the same tx makes the next inbound message for that external chat
// create a fresh chat_session under a new binding (see EnsureSession) instead of
// reviving this archived one. Unarchive deliberately does NOT recreate the
// binding: if later traffic already forked a new session, that session owns the
// channel now, and restoring the old binding would steal it back.
//
// Severing that binding is what makes archiving a bound session cancel its
// in-flight tasks: the adapter still holds the chat id, so a turn that survives
// the archive answers into a group room the session no longer belongs to. That
// cancel is therefore scoped to sessions that HAD a binding. Archiving a
// web-only chat leaves its tasks alone — it is the reversible organizing action
// the UI presents (no confirmation, unarchive undoes it), and the destructive
// counterpart is the Stop button, which cancels through CancelTaskByUser and
// gives the user their typed prompt back.
func (h *Handler) SetChatSessionArchived(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SetChatSessionArchivedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.SetChatSessionArchived(r.Context(), db.SetChatSessionArchivedParams{
		ID:       session.ID,
		Archived: req.Archived,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update chat session")
		return
	}

	// Rows cancelled inside the tx, broadcast after it commits. nil when the
	// request unarchives and nil for a web-only chat; BroadcastCancelledTasks
	// is a no-op on an empty slice.
	var cancelled []db.AgentTaskQueue

	if req.Archived {
		// Read the binding BEFORE the delete below, which is what erases the
		// evidence: after it runs there is no way to tell a session that was
		// on a channel from one that never was.
		_, bindingErr := qtx.GetChannelChatSessionBindingBySessionAny(r.Context(), session.ID)
		switch {
		case bindingErr == nil:
			// Bound, and the delete below is about to drop that binding.
			// ClaimAgentTask does not read chat_session.status, so a task
			// queued before the archive stays claimable after it: the daemon
			// runs a turn on a conversation the user closed, spending runtime
			// and quota and writing assistant messages into an archived chat.
			// The binding is gone by then, so the brief describes the run as a
			// private web chat — the misattribution the history note below
			// exists to prevent — while the outbound senders, which resolve
			// their destination through that same deleted row, have no route
			// left to deliver on. Cancel in the same tx that drops the
			// binding, as DeleteChatSession does.
			//
			// The query is not limited to never-started work: it also matches
			// dispatched, running, waiting_local_directory and deferred, which
			// is why the returned rows have to reach the post-commit broadcast
			// below rather than being discarded.
			cancelled, err = qtx.CancelAgentTasksByChatSession(r.Context(), session.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to cancel queued tasks for the archived session")
				return
			}
		case errors.Is(bindingErr, pgx.ErrNoRows):
			// A web-only chat has no room, no adapter and nowhere for a late
			// answer to land, so there is nothing here to protect anyone from
			// and archiving stays what the UI already says it is: the
			// reversible "put it away" step that unarchive undoes, offered
			// with no confirmation, with hard delete available only after it.
			// Cancelling would also destroy the user's typed prompt — the Stop
			// button cancels through CancelTaskByUser, which hands the prompt
			// back (cancelled_chat_message.restore_to_input, or a durable
			// chat_draft_restore row), and CancelAgentTasksByChatSession does
			// neither.
		default:
			// We cannot tell whether this session was bound. Carrying on would
			// silently skip the cancel and reopen the bug above, so fail the
			// archive rather than guess; the caller can retry.
			slog.Warn("read chat session channel binding failed", "session_id", sessionID, "error", bindingErr)
			writeError(w, http.StatusInternalServerError, "failed to read chat session channel binding")
			return
		}
		if err := qtx.DeleteChannelChatSessionBindingBySession(r.Context(), session.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to clear chat session channel binding")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit chat session archive failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit chat session update")
		return
	}

	// Post-commit broadcasts, same as DeleteChatSession: subscribers should
	// never observe events for a tx that didn't actually persist. This is the
	// call that captures the cancellation and revokes the tasks' mat_ tokens,
	// reconciles each agent off 'working', emits task:cancelled so other
	// clients drop the row instead of showing it queued until the next
	// refresh, and wakes the runtime so a queued successor is claimed now
	// rather than at the daemon's next poll.
	h.TaskService.BroadcastCancelledTasks(r.Context(), workspaceID, cancelled)

	resolvedSessionID := uuidToString(updated.ID)
	status := updated.Status
	h.publishChat(protocol.EventChatSessionUpdated, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionUpdatedPayload{
		ChatSessionID: resolvedSessionID,
		Title:         updated.Title,
		Status:        &status,
		UpdatedAt:     timestampToString(updated.UpdatedAt),
	})

	writeJSON(w, http.StatusOK, chatSessionToResponse(updated))
}

// DeleteChatSession hard-deletes a chat session owned by the caller. The
// row lock + cancel + delete run inside a single tx so a concurrent
// SendChatMessage cannot enqueue a task that would later be orphaned by
// the FK ON DELETE SET NULL on agent_task_queue.chat_session_id. Cancel
// failure aborts the delete; events fire only after commit.
func (h *Handler) DeleteChatSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// FOR UPDATE on the chat_session row blocks any concurrent INSERT into
	// agent_task_queue that references it (the FK validation needs a
	// KEY SHARE lock). After we commit the delete, the blocked INSERT
	// fails its FK check, so it can't land an orphaned task.
	if _, err := qtx.LockChatSessionForDelete(r.Context(), session.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already gone — treat as idempotent success.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock chat session")
		return
	}

	// Claim, clear, prioritize, and direct send all lock the agent before task
	// rows. Keep delete on the same agent -> task suffix after its session lock,
	// otherwise a builder-agent delete can deadlock with a concurrent claim.
	if _, err := qtx.GetAgentForClaimUpdate(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock chat agent")
		return
	}

	cancelled, err := qtx.CancelAgentTasksByChatSession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel chat session tasks")
		return
	}

	// channel_chat_session_binding used to carry a chat_session FK with
	// ON DELETE CASCADE; MUL-3515 §4 dropped every channel_* foreign key, so
	// prune the binding here in the same tx that deletes its chat_session.
	if err := qtx.DeleteChannelChatSessionBindingBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session binding")
		return
	}
	// channel_outbound_card_message is also keyed by chat_session_id with no FK
	// and no reaper, so prune it in the same tx or a deleted chat session leaves
	// permanent orphan card rows (#4810 follow-up).
	if err := qtx.DeleteChannelOutboundCardMessagesBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session outbound cards")
		return
	}
	// chat_draft_restore is keyed by chat_session_id with no FK either, so its
	// pending restores are pruned here rather than by a DB cascade (#5219). The
	// LockChatSessionForDelete above doubles as the deleter half of the
	// draft-restore protocol: FinalizeDeferredCancelledChat takes the same
	// chat_session lock before inserting a restore, so it cannot slip one past
	// this sweep (see LockChatSessionForTask in chat.sql).
	if err := qtx.DeleteChatDraftRestoresBySession(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session draft restores")
		return
	}

	// Same no-FK chore, for the agent configuration a builder conversation was
	// editing. A no-op for ordinary chats, which never have one.
	if err := qtx.DeleteAgentBuilderDraft(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent builder draft")
		return
	}

	if err := qtx.DeleteChatSession(r.Context(), db.DeleteChatSessionParams{
		ID:          session.ID,
		WorkspaceID: session.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete chat session")
		return
	}
	if err := qtx.DeleteAgentLabelAssignmentsByAgent(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove chat session agent label assignments")
		return
	}
	if err := qtx.DeleteSystemAgentByID(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up chat session agent")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("commit chat session delete failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to commit chat session delete")
		return
	}

	// Post-commit broadcasts. Subscribers should never observe events for a
	// tx that didn't actually persist.
	//
	// The workspace has to come from the session we just deleted: the tasks were
	// cancelled and returned before the delete, so they still carry its id, but
	// the row they would be resolved through is gone by now.
	h.TaskService.BroadcastCancelledTasks(r.Context(), workspaceID, cancelled)

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatSessionDeleted, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionDeletedPayload{
		ChatSessionID: resolvedSessionID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Chat Messages
// ---------------------------------------------------------------------------

type SendChatMessageRequest struct {
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids"`
}

type SendChatMessageResponse struct {
	MessageID     string `json:"message_id"`
	TaskID        string `json:"task_id"`
	SupportsQueue bool   `json:"supports_queue"`
	Queued        bool   `json:"queued"`
	// AttachmentIDs are the attachment rows actually bound to this message by
	// the server. The client diffs these against the ids it requested so it
	// can warn the user when an attachment silently failed to bind — no extra
	// round-trip needed. No `omitempty`: a send that requested attachments but
	// bound none must serialize `[]` (not be omitted), otherwise the client
	// can't tell "all binds failed" from "older server without this field" and
	// would silently skip the very warning this exists for. When no
	// attachments were requested the value is nil → `null`, which the client's
	// guard short-circuits on the requested-ids check.
	AttachmentIDs []string `json:"attachment_ids"`
	// CreatedAt anchors the chat StatusPill timer the instant the user
	// hits send. Without it the front-end falls back to its local clock
	// and the timer "snaps backwards" later when WS events deliver the
	// real created_at. Returning it here means the pill renders 0s from
	// the start with a stable anchor.
	CreatedAt string `json:"created_at"`
}

func (h *Handler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req SendChatMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Pre-validate attachment ids early so invalid input returns 400 before
	// any state mutation. The actual link runs after CreateChatMessage so we
	// have a message_id to back-fill into the attachment rows.
	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, req.AttachmentIDs, "attachment_ids")
	if !ok {
		return
	}

	// Load chat session and re-check the private-agent gate on every send.
	// The session's creator passed the gate at create time, but their
	// workspace role (or the agent's owner) may have changed since — keep
	// stale sessions from being a back-door into a private agent the user
	// can no longer reach. Agent senders bypass to preserve A2A collaboration.
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	// Archived sessions are read-only: refuse to enqueue new agent work for
	// them (see SetChatSessionArchived). The frontend disables the composer
	// for status='archived' and only offers unarchive/delete there. Legacy
	// soft-archived rows from before the feature are covered by the same check.
	if session.Status != "active" {
		writeError(w, http.StatusBadRequest, "chat session is archived")
		return
	}

	// Preflight the agent's enqueue preconditions BEFORE persisting the user
	// message. EnqueueChatTask (below) rejects an archived or runtime-less agent
	// with an error; without this check a stale client (agent archived in
	// another tab) would land the user message, then get a 500, leaving an
	// orphan message with no task or reply. Fail fast with a 4xx and mutate
	// nothing. See EnqueueChatTask's ErrChatTaskAgentArchived / NoRuntime.
	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat agent")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "chat agent is archived")
		return
	}
	// Shared verdict: an unbound agent and a machine whose CLI cannot run are
	// both refusals here, with their own codes. A merely offline runtime is not
	// checked at all — chat messages queue for it, as they always have.
	if verdict, err := service.AgentReadiness(r.Context(), h.Queries, agent); err == nil && verdict.Blocked() {
		h.writeDispatchBlocked(w, http.StatusConflict, verdict.Reason)
		return
	}

	// Re-run the INVOKE gate on every send, not just the softer view gate in
	// gatePublicChatSessionForUser (MUL-4525). canAccessPrivateAgent lets a workspace
	// admin keep reading a transcript, but sending a message enqueues a run and
	// must satisfy canInvokeAgent — which has no admin bypass. A session created
	// while the user could invoke the agent must stop enqueuing work the instant
	// that permission is revoked (agent flipped private, ownership moved, target
	// removed from the allow-list), and it must fail BEFORE we persist the user
	// message / attachments / task. Blocked returns a structured, enumeration-safe
	// reason so the composer can explain it without leaking private-agent details.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	// Detect whether this is the very first human message in the session,
	// BEFORE we insert the new row. This scopes LLM auto-titling (MUL-4295) to
	// the opening turn: we upgrade the default/original title exactly once, off
	// the first user message, and never re-run it on every subsequent send. A
	// query error here is treated as "not first" so we simply skip generation
	// (best-effort — never block the send).
	hadUserMessage := true
	if existed, err := h.Queries.ChatSessionHasUserMessage(r.Context(), session.ID); err == nil {
		hadUserMessage = existed
	}

	// Persist the whole turn atomically (MUL-4351): the owning task, the user
	// message bound to that task (so it belongs to the task's immutable input
	// batch the instant it exists), attachment bindings, and the session touch
	// all commit together, and the daemon is only notified after the commit. For
	// web chat the sender is the authenticated request user (sessions are
	// creator-only), so they are the task initiator — surfaced to the agent
	// under `## Task Initiator`. actorType/actorID were resolved above for the
	// invoke gate.
	sent, err := h.TaskService.SendDirectChatMessage(r.Context(), session, agent, parseUUID(userID), req.Content, attachmentIDs, actorType, parseUUID(actorID))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrChatSessionArchived):
			writeError(w, http.StatusConflict, "chat session is archived")
		case errors.Is(err, service.ErrChatTaskAgentArchived):
			writeError(w, http.StatusConflict, "chat agent is archived")
		case errors.Is(err, service.ErrChatTaskAgentNoRuntime):
			writeError(w, http.StatusConflict, "chat agent has no runtime")
		default:
			writeError(w, http.StatusInternalServerError, "failed to send chat message: "+err.Error())
		}
		return
	}
	msg := sent.Message
	task := sent.Task

	// AttachmentIDs actually bound by the server. Requested-but-unbound ids are
	// surfaced to the client so it can warn the user (see SendChatMessageResponse).
	var boundAttachmentIDs []string
	if len(attachmentIDs) > 0 {
		boundAttachmentIDs = make([]string, 0, len(sent.BoundAttachmentIDs))
		for _, id := range sent.BoundAttachmentIDs {
			boundAttachmentIDs = append(boundAttachmentIDs, uuidToString(id))
		}
	}

	taskContext := h.TaskService.AnalyticsContextForTask(r.Context(), task)
	platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.ChatMessageSent(
		userID,
		workspaceID,
		uuidToString(session.ID),
		uuidToString(task.ID),
		uuidToString(session.AgentID),
		taskContext.RuntimeMode,
		taskContext.Provider,
		platform,
	))

	// Broadcast the user message.
	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatMessage, workspaceID, "member", userID, resolvedSessionID, protocol.ChatMessagePayload{
		ChatSessionID: resolvedSessionID,
		MessageID:     uuidToString(msg.ID),
		Role:          "user",
		Content:       req.Content,
		TaskID:        uuidToString(task.ID),
		CreatedAt:     timestampToString(msg.CreatedAt),
	})

	// First user message → kick off best-effort LLM auto-titling (MUL-4295).
	// Fire-and-forget and non-blocking: the response below is written whether
	// or not a title is ever generated, and a disabled/failing LLM layer
	// silently keeps the original first-message-derived title. session.Title
	// is the default/original title observed here and drives the CAS so a
	// manual rename mid-generation is never clobbered.
	if !hadUserMessage {
		h.maybeGenerateChatTitleAsync(workspaceID, userID, session.ID, session.Title, req.Content)
	}

	writeJSON(w, http.StatusCreated, SendChatMessageResponse{
		MessageID:     uuidToString(msg.ID),
		TaskID:        uuidToString(task.ID),
		SupportsQueue: true,
		Queued:        sent.Queued,
		CreatedAt:     timestampToString(task.CreatedAt),
		AttachmentIDs: boundAttachmentIDs,
	})
}

type ChatMessagesCursorResponse struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type ChatMessagesPageResponse struct {
	Messages   []ChatMessageResponse       `json:"messages"`
	Limit      int                         `json:"limit"`
	HasMore    bool                        `json:"has_more"`
	NextCursor *ChatMessagesCursorResponse `json:"next_cursor,omitempty"`
}

func parseChatMessagesPageParams(r *http.Request) (int, pgtype.Timestamptz, pgtype.UUID, error) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid limit")
		}
		limit = parsed
	}

	rawBeforeCreatedAt := r.URL.Query().Get("before_created_at")
	rawBeforeID := r.URL.Query().Get("before_id")
	if rawBeforeCreatedAt == "" && rawBeforeID == "" {
		return limit, pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	if rawBeforeCreatedAt == "" || rawBeforeID == "" {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	beforeTime, err := time.Parse(time.RFC3339Nano, rawBeforeCreatedAt)
	if err != nil {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	beforeID, err := util.ParseUUID(rawBeforeID)
	if err != nil {
		return 0, pgtype.Timestamptz{}, pgtype.UUID{}, errors.New("invalid cursor")
	}
	return limit, pgtype.Timestamptz{Time: beforeTime, Valid: true}, beforeID, nil
}

// RegenerateChatQuickActionsResponse acknowledges an accepted refresh request.
// message_id is the assistant turn the refreshed pills will attach to — the
// client anchors its pending placeholder on it and resolves it when the
// chat:quick_actions supplement arrives.
// RegenerateChatQuickActionsRequest names the assistant turn the client is
// refreshing. The server confirms it is still the session's latest turn before
// enqueuing (409 otherwise), so the client's pending marker stays aligned with
// the turn chat:quick_actions will resolve — no ack reconciliation needed
// (MUL-5149).
type RegenerateChatQuickActionsRequest struct {
	MessageID string `json:"message_id"`
}

type RegenerateChatQuickActionsResponse struct {
	MessageID string `json:"message_id"`
}

// RegenerateChatQuickActions re-runs the suggestion pass for a session's latest
// assistant turn on explicit user request (the "refresh" button on the
// quick-actions row, MUL-5149). Generation is server-side, so this spawns no
// agent run; the refreshed pills arrive over the same chat:quick_actions
// realtime path as the automatic pass.
func (h *Handler) RegenerateChatQuickActions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	if session.Status != "active" {
		writeError(w, http.StatusBadRequest, "chat session is archived")
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), session.AgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load chat agent")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "chat agent is archived")
		return
	}
	// The refresh no longer runs the agent, but it is still a user-triggered
	// spend against that agent's conversation, so it keeps clearing the same
	// INVOKE gate as a send (MUL-4525) rather than the softer view gate in
	// gatePublicChatSessionForUser. Deliberately NOT relaxed as a side effect of
	// moving generation server-side.
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	var req RegenerateChatQuickActionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	expectedMessageID, ok := parseUUIDOrBadRequest(w, req.MessageID, "message_id")
	if !ok {
		return
	}

	messageID, targetTask, err := h.TaskService.RegenerateChatQuickActions(r.Context(), session, expectedMessageID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrChatQuickActionsStale):
			// The turn the client was refreshing is no longer the latest — its
			// view is stale (a newer reply arrived). 409 → the client rolls back
			// its optimistic marker and re-offers refresh on the new turn.
			writeError(w, http.StatusConflict, "a newer reply arrived — refresh it instead")
		case errors.Is(err, service.ErrChatQuickActionsBusy):
			// A turn is already running for this session; it is about to replace
			// the reply these pills would hang on. 409 → the client rolls back
			// and can refresh once the session settles.
			writeError(w, http.StatusConflict, "still working — try refreshing in a moment")
		case errors.Is(err, service.ErrChatQuickActionsNoTurn):
			writeError(w, http.StatusConflict, "no assistant reply to refresh yet")
		case errors.Is(err, service.ErrChatQuickActionsUnavailable):
			writeError(w, http.StatusServiceUnavailable, "suggestions are not available on this deployment")
		default:
			writeError(w, http.StatusInternalServerError, "failed to regenerate quick actions")
		}
		return
	}
	// Detached like the automatic pass: answer 202 now, deliver the refreshed
	// pills over chat:quick_actions.
	h.TaskService.GenerateChatQuickActionsAsync(targetTask, service.ChatQuickActionsRefresh)
	writeJSON(w, http.StatusAccepted, RegenerateChatQuickActionsResponse{
		MessageID: uuidToString(messageID),
	})
}

func (h *Handler) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	messages, err := h.Queries.ListChatMessages(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}
	messages = visibleChatMessages(messages)

	messageIDs := make([]pgtype.UUID, len(messages))
	for i, m := range messages {
		messageIDs[i] = m.ID
	}
	groupedAtt := h.groupChatMessageAttachments(r.Context(), workspaceID, messageIDs)

	resp := make([]ChatMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = chatMessageToResponse(m, groupedAtt[uuidToString(m.ID)])
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListChatMessagesPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	limit, beforeCreatedAt, beforeID, err := parseChatMessagesPageParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A session can contain one server-authored onboarding kickoff. Fetch one
	// extra row beyond the normal lookahead so hiding it cannot make a visible
	// page appear shorter or lose its next cursor.
	messages, err := h.Queries.ListChatMessagesPage(r.Context(), db.ListChatMessagesPageParams{
		ChatSessionID:   session.ID,
		Limit:           int32(limit + 2),
		BeforeCreatedAt: beforeCreatedAt,
		BeforeID:        beforeID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list chat messages")
		return
	}
	messages = visibleChatMessages(messages)
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	var nextCursor *ChatMessagesCursorResponse
	if hasMore && len(messages) > 0 {
		oldest := messages[len(messages)-1]
		nextCursor = &ChatMessagesCursorResponse{
			CreatedAt: oldest.CreatedAt.Time.Format(time.RFC3339Nano),
			ID:        uuidToString(oldest.ID),
		}
	}
	// SQL fetches newest windows first so the empty cursor opens at the recent
	// tail. Reverse each cursor page before serializing to keep message order
	// chronological within the viewport.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	messageIDs := make([]pgtype.UUID, len(messages))
	for i, m := range messages {
		messageIDs[i] = m.ID
	}
	groupedAtt := h.groupChatMessageAttachments(r.Context(), workspaceID, messageIDs)

	resp := make([]ChatMessageResponse, len(messages))
	for i, m := range messages {
		resp[i] = chatMessageToResponse(m, groupedAtt[uuidToString(m.ID)])
	}
	writeJSON(w, http.StatusOK, ChatMessagesPageResponse{
		Messages:   resp,
		Limit:      limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

// PendingChatTaskResponse is returned by GetPendingChatTask — either the
// current in-flight task's id/status, or an empty object when none is active.
// CreatedAt is the anchor the frontend uses to time the chat StatusPill
// (elapsed seconds = now - CreatedAt). It must come from the server because
// optimistic seeds don't have a real task created_at and the timer needs to
// survive refresh / reopen.
type PendingChatTaskResponse struct {
	TaskID        string                   `json:"task_id,omitempty"`
	Status        string                   `json:"status,omitempty"`
	CreatedAt     string                   `json:"created_at,omitempty"`
	SupportsQueue bool                     `json:"supports_queue"`
	QueuedTasks   []QueuedChatTaskResponse `json:"queued_tasks,omitempty"`
}

type QueuedChatTaskResponse struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	MessageID string `json:"message_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type PrioritizeQueuedChatTaskResponse struct {
	TaskID       string `json:"task_id"`
	ActiveTaskID string `json:"active_task_id,omitempty"`
}

// MarkChatSessionRead clears the session's unread_since (→ has_unread=false)
// and broadcasts chat:session_read so other devices of the same user drop
// their badges.
func (h *Handler) MarkChatSessionRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	if err := h.Queries.MarkChatSessionRead(r.Context(), session.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark session read")
		return
	}

	resolvedSessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatSessionRead, workspaceID, "member", userID, resolvedSessionID, protocol.ChatSessionReadPayload{
		ChatSessionID: resolvedSessionID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Deferred-cancellation draft restores (#5219)
// ---------------------------------------------------------------------------

// ChatDraftRestoreResponse is one recoverable composer draft: a deferred
// cancellation settled as empty-transcript after the cancel HTTP response
// returned, so the deleted prompt is persisted server-side until the
// creator's client applies and consumes it. Attachments are resolved from
// the stored ids at read time so they carry the normal handler URL policy.
type ChatDraftRestoreResponse struct {
	ID            string               `json:"id"`
	ChatSessionID string               `json:"chat_session_id"`
	TaskID        string               `json:"task_id"`
	Content       string               `json:"content"`
	Attachments   []AttachmentResponse `json:"attachments,omitempty"`
	CreatedAt     string               `json:"created_at"`
}

type ChatDraftRestoresResponse struct {
	Restores []ChatDraftRestoreResponse `json:"restores"`
}

// ListChatDraftRestores returns the session's pending draft restores.
// Creator-only via loadChatSessionForUser — deliberately not the
// private-agent gate: the content is the caller's own deleted prompt, and
// losing agent access must not strand it server-side forever.
func (h *Handler) ListChatDraftRestores(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListChatDraftRestoresBySession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list draft restores")
		return
	}

	var attachmentIDs []pgtype.UUID
	for _, row := range rows {
		attachmentIDs = append(attachmentIDs, row.AttachmentIds...)
	}
	attachmentsByID := map[string]AttachmentResponse{}
	if len(attachmentIDs) > 0 {
		attRows, err := h.Queries.ListAttachmentsByIDs(r.Context(), db.ListAttachmentsByIDsParams{
			AttachmentIds: attachmentIDs,
			WorkspaceID:   session.WorkspaceID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load draft restore attachments")
			return
		}
		attachmentMode := attachmentURLModeFromRequest(r)
		for _, a := range attRows {
			attachmentsByID[uuidToString(a.ID)] = h.attachmentToResponse(a, attachmentMode)
		}
	}

	resp := ChatDraftRestoresResponse{Restores: make([]ChatDraftRestoreResponse, 0, len(rows))}
	for _, row := range rows {
		item := ChatDraftRestoreResponse{
			ID:            uuidToString(row.ID),
			ChatSessionID: uuidToString(row.ChatSessionID),
			TaskID:        uuidToString(row.TaskID),
			Content:       row.Content,
			CreatedAt:     timestampToString(row.CreatedAt),
		}
		for _, attID := range row.AttachmentIds {
			if a, ok := attachmentsByID[uuidToString(attID)]; ok {
				item.Attachments = append(item.Attachments, a)
			}
		}
		resp.Restores = append(resp.Restores, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ConsumeChatDraftRestore deletes one draft restore after the creator's
// client applied it. Idempotent: consuming an already-consumed (or never
// existing) restore still returns 204, so a retried consume after a lost
// response can't fail the client.
func (h *Handler) ConsumeChatDraftRestore(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	restoreUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "restoreId"), "restore id")
	if !ok {
		return
	}

	if _, err := h.Queries.DeleteChatDraftRestore(r.Context(), db.DeleteChatDraftRestoreParams{
		ID:            restoreUUID,
		ChatSessionID: session.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to consume draft restore")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pruneRuntimeSystemAgentChatDraftRestores drops the pending draft restores of
// every chat_session a runtime teardown is about to remove through the agent
// cascade (chat_session.agent_id is ON DELETE CASCADE, migration 033).
// chat_draft_restore has no FK (MUL-3515) and no reaper, so a restore left
// behind keeps the user's prompt text forever, unreachable and undeletable.
//
// Every runtime/agent teardown path must call this in its own transaction and
// BEFORE deleting the agent rows — the queries join through them. Only system
// agents are in scope: since MUL-5559 a runtime delete unbinds its user agents
// instead of deleting them, so their sessions and restores must survive.
//
// The sessions are locked before the sweep: that is the deleter half of the
// mutual-exclusion protocol with FinalizeDeferredCancelledChat, which would
// otherwise insert a restore this sweep can no longer see (see LockChatSession*
// in chat.sql).
//
// The workspace teardown has its own copy of this shape (locks, then sweeps
// inside the DeleteWorkspace CTE) because that statement's prune must stay in
// the same statement as the workspace row it commits with.
func pruneRuntimeSystemAgentChatDraftRestores(ctx context.Context, q *db.Queries, runtimeID pgtype.UUID) error {
	if _, err := q.LockChatSessionsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if err := q.DeleteChatDraftRestoresBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	// Builder drafts only ever hang off a system carrier, so they are pruned
	// here and nowhere else — the archived-agent sweep above has none to find.
	return q.DeleteAgentBuilderDraftsBySystemRuntimeAgents(ctx, runtimeID)
}

// PendingChatTasksResponse is the aggregate view consumed by the FAB.
type PendingChatTasksResponse struct {
	Tasks []PendingChatTaskItem `json:"tasks"`
}

type PendingChatTaskItem struct {
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	ChatSessionID string `json:"chat_session_id"`
}

type CancelledChatMessageResponse struct {
	ChatSessionID  string               `json:"chat_session_id"`
	MessageID      string               `json:"message_id"`
	Content        string               `json:"content"`
	RestoreToInput bool                 `json:"restore_to_input"`
	Attachments    []AttachmentResponse `json:"attachments,omitempty"`
}

type CancelTaskByUserResponse struct {
	AgentTaskResponse
	CancelledChatMessage *CancelledChatMessageResponse `json:"cancelled_chat_message,omitempty"`
}

// ListPendingChatTasks returns every in-flight chat task owned by the current
// user in this workspace. Drives the FAB's "running" indicator when the chat
// window is closed (no per-session query is subscribed). Tasks belonging to
// private agents the caller has lost access to are dropped from the response.
func (h *Handler) ListPendingChatTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	// No accessible agents → every row would be filtered out anyway. Skip the
	// DB round-trip and return an empty list (mirrors HasPendingChatTasks).
	if len(allowed) == 0 {
		writeJSON(w, http.StatusOK, PendingChatTasksResponse{Tasks: []PendingChatTaskItem{}})
		return
	}

	rows, err := h.Queries.ListPendingChatTasksByCreator(r.Context(), db.ListPendingChatTasksByCreatorParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending chat tasks")
		return
	}

	// The pending query now returns cs.agent_id per row, so we can filter
	// out private agents the caller has lost access to directly against the
	// already-loaded `allowed` set — no second ListAllChatSessionsByCreator
	// scan on this hot path (MUL-4159).
	items := make([]PendingChatTaskItem, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if _, ok := allowed[agentID]; !ok {
			continue
		}
		items = append(items, PendingChatTaskItem{
			TaskID:        uuidToString(row.TaskID),
			Status:        row.Status,
			ChatSessionID: uuidToString(row.ChatSessionID),
		})
	}
	writeJSON(w, http.StatusOK, PendingChatTasksResponse{Tasks: items})
}

// HasPendingChatTasksResponse is the boolean fast-path payload consumed by the
// FAB, which only needs to know whether any in-flight chat task exists — not
// the full list.
type HasPendingChatTasksResponse struct {
	HasPending bool `json:"has_pending"`
}

// HasPendingChatTasks answers "does the current user have any in-flight chat
// task in this workspace?" with a single EXISTS query, for the FAB's running
// indicator. It is the boolean sibling of ListPendingChatTasks: the detailed
// list is reserved for the ChatWindow history / stop-task flows.
//
// Permission filtering is preserved end-to-end: the set of agents the caller
// may currently see is resolved the same way as ListPendingChatTasks and
// pushed into the query as agent_ids, so a member who lost access to a private
// agent never sees a true from a task on that agent (MUL-4159). An empty
// accessible-agent set short-circuits to false without hitting the DB.
func (h *Handler) HasPendingChatTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed, ok := h.accessibleAgentIDs(r.Context(), workspaceID, actorType, actorID, member.Role)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent access")
		return
	}

	// No accessible agents → nothing the caller may see can be pending.
	// Skip the round-trip and return false.
	if len(allowed) == 0 {
		writeJSON(w, http.StatusOK, HasPendingChatTasksResponse{HasPending: false})
		return
	}

	agentIDs := make([]pgtype.UUID, 0, len(allowed))
	for id := range allowed {
		agentIDs = append(agentIDs, parseUUID(id))
	}

	hasPending, err := h.Queries.HasPendingChatTasksByCreator(r.Context(), db.HasPendingChatTasksByCreatorParams{
		WorkspaceID: parseUUID(workspaceID),
		CreatorID:   parseUUID(userID),
		AgentIds:    agentIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check pending chat tasks")
		return
	}

	writeJSON(w, http.StatusOK, HasPendingChatTasksResponse{HasPending: hasPending})
}

// GetPendingChatTask returns the current task plus its ordered follow-ups. The
// root fields retain the original response shape so older clients continue to
// recover a single pending task after refresh / reopen.
func (h *Handler) GetPendingChatTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	tasks, err := h.Queries.ListPendingChatTasksForSession(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending chat tasks")
		return
	}
	if len(tasks) == 0 {
		writeJSON(w, http.StatusOK, PendingChatTaskResponse{SupportsQueue: true})
		return
	}

	head := tasks[0]
	queued := make([]QueuedChatTaskResponse, 0, len(tasks)-1)
	for _, task := range tasks[1:] {
		if task.Status != "queued" {
			continue
		}
		queued = append(queued, QueuedChatTaskResponse{
			TaskID:    uuidToString(task.ID),
			Status:    task.Status,
			CreatedAt: timestampToString(task.CreatedAt),
			MessageID: uuidToString(task.MessageID),
			Content:   task.Content,
		})
	}

	writeJSON(w, http.StatusOK, PendingChatTaskResponse{
		TaskID:        uuidToString(head.ID),
		Status:        head.Status,
		CreatedAt:     timestampToString(head.CreatedAt),
		SupportsQueue: true,
		QueuedTasks:   queued,
	})
}

// PrioritizeQueuedChatTask moves one queued follow-up ahead of its FIFO peers.
// The client then cancels the current task through the existing cancellation
// endpoint, preserving that path's transcript and draft-restore semantics.
func (h *Handler) PrioritizeQueuedChatTask(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}

	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start prioritize transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// ClaimTask takes this same row lock before selecting work. Holding it here
	// makes "prioritize + report the active task" one server-authoritative
	// decision instead of two client requests racing the daemon.
	if _, err := qtx.GetAgentForClaimUpdate(r.Context(), session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock chat agent")
		return
	}
	prioritized, err := qtx.PrioritizeQueuedChatTask(
		r.Context(),
		db.PrioritizeQueuedChatTaskParams{ID: taskID, ChatSessionID: session.ID},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// The compare-and-set rejects both a stale queue row and a still-valid
		// follow-up that has no claimed reply to replace. Distinguish them for
		// current clients while preserving the same 409 compatibility contract.
		queuedTask, loadErr := qtx.GetAgentTask(r.Context(), taskID)
		if loadErr != nil && !errors.Is(loadErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load queued task")
			return
		}
		if loadErr == nil &&
			queuedTask.Status == "queued" &&
			queuedTask.ChatSessionID.Valid &&
			uuidToString(queuedTask.ChatSessionID) == uuidToString(session.ID) {
			writeError(w, http.StatusConflict, "there is no active reply to replace")
			return
		}
		writeError(w, http.StatusConflict, "task is no longer queued")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prioritize queued task")
		return
	}
	queuedTask, err := qtx.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load prioritized task")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit queued task priority")
		return
	}
	h.TaskService.BroadcastTaskQueued(r.Context(), queuedTask)

	writeJSON(w, http.StatusOK, PrioritizeQueuedChatTaskResponse{
		TaskID:       uuidToString(prioritized.TaskID),
		ActiveTaskID: uuidToString(prioritized.ActiveTaskID),
	})
}

// ClearQueuedChatTasks cancels every queued follow-up without touching the
// session's current positional head, even when that head is not claimed yet.
func (h *Handler) ClearQueuedChatTasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")
	session, ok := h.gatePublicChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}
	if err := h.TaskService.CancelQueuedChatTasks(r.Context(), session.ID, session.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear queued tasks")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Task cancellation (user-facing, with ownership check)
// ---------------------------------------------------------------------------

// CancelTaskByUser cancels a task the caller is allowed to act on within the
// current workspace.
//
// Tenancy is enforced uniformly through the task's owning agent: every
// agent_task_queue row carries a NOT NULL agent_id (ON DELETE CASCADE, so the
// agent always exists), and agents are workspace-scoped. GetAgentTaskInWorkspace
// is therefore the single tenant guard that works regardless of which optional
// source FK (issue / chat_session / autopilot_run) is set — which is what makes
// run_only autopilot tasks and quick_create tasks (whose issue does not exist
// yet) cancellable at all. Keying cancellation off issue_id / chat_session_id
// alone is exactly what 404'd these tasks before (MUL-2827).
//
// On top of tenancy, two privacy models layer on:
//   - a chat task is private to the member who started the conversation, so
//     only that creator may cancel it;
//   - every other task surfaces on the agent Activity tab and the workspace
//     task snapshot, both of which hide private agents from members without
//     access. Cancellation mirrors that gate via canAccessPrivateAgent so the
//     id-only endpoint is never more permissive than the surface that exposes
//     the task.
func (h *Handler) CancelTaskByUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID:          taskUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	var (
		queuedOnly      bool
		expectedSession pgtype.UUID
		queueAction     string
	)
	if expectedStatus := r.URL.Query().Get("expected_status"); expectedStatus != "" {
		if expectedStatus != "queued" {
			writeError(w, http.StatusBadRequest, "expected_status must be queued")
			return
		}
		sessionID := r.URL.Query().Get("chat_session_id")
		var ok bool
		expectedSession, ok = parseUUIDOrBadRequest(w, sessionID, "chat_session_id")
		if !ok {
			return
		}
		if !task.ChatSessionID.Valid || task.ChatSessionID != expectedSession {
			writeError(w, http.StatusConflict, "task does not belong to the expected chat session")
			return
		}
		queueAction = r.URL.Query().Get("queue_action")
		if queueAction != "edit" && queueAction != "remove" {
			writeError(w, http.StatusBadRequest, "queue_action must be edit or remove")
			return
		}
		queuedOnly = true
	}

	if task.ChatSessionID.Valid {
		// Chat privacy: only the member who opened the conversation may
		// cancel its task, even though the workspace is shared.
		cs, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          task.ChatSessionID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if uuidToString(cs.CreatorID) != userID {
			writeError(w, http.StatusForbidden, "not your task")
			return
		}
	} else {
		// Issue / autopilot / quick_create tasks are all visible on the
		// agent Activity tab + workspace snapshot, which gate private
		// agents. Mirror that gate here.
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          task.AgentID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return
		}
	}

	cancelled, err := h.TaskService.CancelTaskWithResult(r.Context(), taskUUID, service.CancelTaskOptions{
		ClientSupportsDraftRestore: requestHasClientCapability(r, protocol.AppCapabilityChatDraftRestoreV1),
		QueuedOnly:                 queuedOnly,
		ExpectedChatSession:        expectedSession,
		QueueAction:                queueAction,
		UserInitiated:              true,
	})
	if errors.Is(err, service.ErrTaskNoLongerQueued) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := CancelTaskByUserResponse{
		AgentTaskResponse: taskToResponse(cancelled.Task, workspaceID),
	}
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.AgentTaskResponse.Attribution})
	if cancelled.CancelledChatMessage != nil {
		attachments := make([]AttachmentResponse, 0, len(cancelled.CancelledChatMessage.Attachments))
		attachmentMode := attachmentURLModeFromRequest(r)
		for _, a := range cancelled.CancelledChatMessage.Attachments {
			attachments = append(attachments, h.attachmentToResponse(a, attachmentMode))
		}
		resp.CancelledChatMessage = &CancelledChatMessageResponse{
			ChatSessionID:  cancelled.CancelledChatMessage.ChatSessionID,
			MessageID:      cancelled.CancelledChatMessage.MessageID,
			Content:        cancelled.CancelledChatMessage.Content,
			RestoreToInput: cancelled.CancelledChatMessage.RestoreToInput,
			Attachments:    attachments,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Response types & helpers
// ---------------------------------------------------------------------------

type ChatSessionResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AgentID     string  `json:"agent_id"`
	CreatorID   string  `json:"creator_id"`
	ProjectID   *string `json:"project_id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	// Only populated by list endpoints — single-session fetches return 0/false/nil.
	// HasUnread is kept as a convenience (== UnreadCount > 0) for existing consumers.
	HasUnread   bool             `json:"has_unread"`
	UnreadCount int              `json:"unread_count"`
	LastMessage *ChatLastMessage `json:"last_message"`
	// Pinned marks a chat the user has stuck to the top of the list. Populated
	// by list endpoints and by the pin/unpin + single-session responses.
	Pinned    bool   `json:"pinned"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ChatLastMessage is a preview of a session's most recent message, used to
// render the IM-style conversation list (name + snippet + time). nil when the
// session has no messages yet.
type ChatLastMessage struct {
	Content       string  `json:"content"`
	Role          string  `json:"role"`
	CreatedAt     string  `json:"created_at"`
	FailureReason *string `json:"failure_reason"`
	// MessageKind is 'message' (default) or 'no_response'. Hidden onboarding
	// kickoff rows make buildChatLastMessage return nil and are never exposed.
	MessageKind string `json:"message_kind"`
}

// buildChatLastMessage assembles the preview from list-row columns; returns nil
// when there is no last message (the LEFT JOIN produced a NULL timestamp).
func buildChatLastMessage(at pgtype.Timestamptz, content, role string, failure pgtype.Text, kind string) *ChatLastMessage {
	if !at.Valid || kind == protocol.ChatMessageKindOnboardingKickoff {
		return nil
	}
	return &ChatLastMessage{
		Content:       content,
		Role:          role,
		CreatedAt:     timestampToString(at),
		FailureReason: textToPtr(failure),
		MessageKind:   normalizeMessageKind(kind),
	}
}

type ChatMessageResponse struct {
	ID            string  `json:"id"`
	ChatSessionID string  `json:"chat_session_id"`
	Role          string  `json:"role"`
	Content       string  `json:"content"`
	TaskID        *string `json:"task_id"`
	CreatedAt     string  `json:"created_at"`
	// FailureReason flags an assistant row synthesized by FailTask's chat
	// fallback. Front-end uses it to switch to the destructive bubble.
	FailureReason *string `json:"failure_reason"`
	// ElapsedMs is the wall-clock duration from task creation to terminal
	// state. Drives "Replied in 38s" / "Failed after 12s" captions.
	ElapsedMs *int64 `json:"elapsed_ms"`
	// MessageKind is additive. User-facing list handlers filter onboarding
	// kickoff rows; clients still understand that kind as a compatibility guard.
	MessageKind string `json:"message_kind"`
	// QuickActions are sanitized follow-ups generated with this assistant turn.
	// Always an empty array for legacy rows and user messages.
	QuickActions []protocol.ChatQuickAction `json:"quick_actions"`
	// Attachments linked to this message via chat_message_id. The chat
	// bubble renders file cards from these, and the daemon claim path
	// (daemon.go) pulls structured metadata from the same source so the
	// agent can `multica attachment download <id>` rather than guessing
	// from a markdown URL that may expire.
	Attachments []AttachmentResponse `json:"attachments,omitempty"`
}

func chatSessionToResponse(s db.ChatSession) ChatSessionResponse {
	return ChatSessionResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		AgentID:     uuidToString(s.AgentID),
		CreatorID:   uuidToString(s.CreatorID),
		ProjectID:   uuidToPtr(s.ProjectID),
		Title:       s.Title,
		Status:      s.Status,
		Pinned:      s.PinnedAt.Valid,
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func chatMessageToResponse(m db.ChatMessage, attachments []AttachmentResponse) ChatMessageResponse {
	return ChatMessageResponse{
		ID:            uuidToString(m.ID),
		ChatSessionID: uuidToString(m.ChatSessionID),
		Role:          m.Role,
		Content:       m.Content,
		TaskID:        uuidToPtr(m.TaskID),
		CreatedAt:     timestampToString(m.CreatedAt),
		FailureReason: textToPtr(m.FailureReason),
		ElapsedMs:     int8ToPtr(m.ElapsedMs),
		MessageKind:   normalizeMessageKind(m.MessageKind),
		QuickActions:  decodeChatQuickActions(m.QuickActions),
		Attachments:   attachments,
	}
}

// visibleChatMessages removes product-authored context that is sent to the
// agent runtime but is never part of the member-visible conversation. The
// daemon reads its task-scoped input through ListChatInputMessages, so this
// user-facing filter does not remove the kickoff from Mika's execution.
func visibleChatMessages(messages []db.ChatMessage) []db.ChatMessage {
	visible := make([]db.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if message.MessageKind == protocol.ChatMessageKindOnboardingKickoff {
			continue
		}
		visible = append(visible, message)
	}
	return visible
}

func decodeChatQuickActions(raw []byte) []protocol.ChatQuickAction {
	actions := make([]protocol.ChatQuickAction, 0)
	if len(raw) == 0 {
		return actions
	}
	if err := json.Unmarshal(raw, &actions); err != nil {
		return []protocol.ChatQuickAction{}
	}
	if actions == nil {
		return []protocol.ChatQuickAction{}
	}
	if len(actions) > chatQuickActionResponseLimit {
		actions = actions[:chatQuickActionResponseLimit]
	}
	return actions
}

const chatQuickActionResponseLimit = 3

// normalizeMessageKind maps a stored chat_message.message_kind to the value the
// API exposes. Unknown / empty kinds degrade to 'message' so a future kind
// never surprises an older client into a broken render (MUL-4351).
func normalizeMessageKind(kind string) string {
	switch kind {
	case protocol.ChatMessageKindNoResponse:
		return protocol.ChatMessageKindNoResponse
	case protocol.ChatMessageKindOnboardingKickoff:
		return protocol.ChatMessageKindOnboardingKickoff
	case protocol.ChatMessageKindOnboardingOpening:
		return protocol.ChatMessageKindOnboardingOpening
	default:
		return protocol.ChatMessageKindMessage
	}
}
