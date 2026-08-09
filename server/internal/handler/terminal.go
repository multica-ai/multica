package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/terminalhub"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/terminalproto"
)

const (
	terminalWSWriteWait      = 10 * time.Second
	terminalWSPongWait       = 60 * time.Second
	terminalWSPingPeriod     = 50 * time.Second
	terminalWSReadLimit      = 64 * 1024
	terminalInputBytesPerSec = 64 * 1024
)

var terminalUpgrader = websocket.Upgrader{CheckOrigin: realtime.CheckOrigin}

// GetTaskTerminalByUser returns metadata only. PTY input/output never enters
// this response or the database.
func (h *Handler) GetTaskTerminalByUser(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.authorizeTaskTerminal(w, r)
	if !ok {
		return
	}
	if !h.cfg.TerminalPTYEnabled || h.TerminalHub == nil {
		writeJSON(w, http.StatusOK, terminalhub.Metadata{TaskID: uuidToString(task.ID), WorkspaceID: workspaceID, ProtocolVersion: int(terminalproto.Version)})
		return
	}
	if meta, found := h.TerminalHub.GetByTask(uuidToString(task.ID)); found {
		writeJSON(w, http.StatusOK, meta)
		return
	}
	if meta, found := h.loadTerminalMetadata(r.Context(), task.ID); found {
		// A durable row proves a terminal existed, but after a server restart it
		// is not attachable until its daemon re-registers the live session.
		meta.Available = false
		if meta.Status == "running" {
			meta.Status = "reconnecting"
		}
		writeJSON(w, http.StatusOK, meta)
		return
	}
	writeJSON(w, http.StatusOK, terminalhub.Metadata{TaskID: uuidToString(task.ID), WorkspaceID: workspaceID, ProtocolVersion: int(terminalproto.Version)})
}

func (h *Handler) TaskTerminalWebSocket(mc realtime.MembershipChecker, pr realtime.PATResolver, resolveSlug realtime.SlugResolver, w http.ResponseWriter, r *http.Request) {
	if !h.cfg.TerminalPTYEnabled || h.TerminalHub == nil {
		writeError(w, http.StatusNotFound, "terminal unavailable")
		return
	}
	conn, userID, workspaceID, ok := realtime.AuthenticateWebSocket(mc, pr, resolveSlug, w, r)
	if !ok {
		return
	}
	task, authorized := h.authorizeTaskTerminalIdentity(r.Context(), chi.URLParam(r, "taskId"), userID, workspaceID)
	if !authorized {
		slog.Debug("terminal browser attach rejected", "task_id", chi.URLParam(r, "taskId"), "error_code", "task_access_denied")
		peer := terminalhub.NewPeer(uuid.NewString(), userID, nil)
		terminalPeerError(peer, "terminal task is not accessible")
		if item := <-peer.Send; len(item.Data) > 0 {
			_ = conn.WriteMessage(item.MessageType, item.Data)
		}
		_ = conn.Close()
		return
	}
	peer := terminalhub.NewPeer(uuid.NewString(), userID, nil)
	defer func() {
		h.TerminalHub.UnregisterPeer(peer)
		_ = conn.Close()
	}()
	go terminalWritePump(conn, peer)

	conn.SetReadLimit(terminalWSReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	messageType, raw, err := conn.ReadMessage()
	if err != nil || messageType != websocket.TextMessage {
		slog.Debug("terminal browser attach rejected", "task_id", uuidToString(task.ID), "error_code", "attach_frame_missing")
		terminalPeerError(peer, "first terminal message must be attach")
		return
	}
	var attach terminalproto.Message
	if json.Unmarshal(raw, &attach) != nil {
		terminalPeerError(peer, "first terminal message must be attach")
		return
	}
	// A browser may carry both a valid auth cookie and the Desktop bearer token.
	// Cookie authentication is completed before this handler, so tolerate the
	// redundant token frame and acknowledge it without putting the token in a
	// URL or log. Token-only clients had this frame consumed by AuthenticateWebSocket.
	if attach.Type == "auth" {
		terminalEnqueueJSON(peer, terminalproto.Message{Type: "auth_ack", ProtocolVersion: int(terminalproto.Version)})
		messageType, raw, err = conn.ReadMessage()
		if err != nil || messageType != websocket.TextMessage || json.Unmarshal(raw, &attach) != nil {
			slog.Debug("terminal browser attach rejected", "task_id", uuidToString(task.ID), "error_code", "attach_after_auth_missing")
			terminalPeerError(peer, "first terminal message must be attach")
			return
		}
	}
	if attach.Type != "attach" {
		slog.Debug("terminal browser attach rejected", "task_id", uuidToString(task.ID), "error_code", "attach_frame_invalid")
		terminalPeerError(peer, "first terminal message must be attach")
		return
	}
	meta, err := h.TerminalHub.AttachBrowser(uuidToString(task.ID), peer, attach.LastSeq)
	if err != nil {
		slog.Debug("terminal browser attach rejected", "task_id", uuidToString(task.ID), "error_code", "session_unavailable")
		terminalPeerError(peer, "terminal session unavailable")
		return
	}
	slog.Debug("terminal browser attached", "task_id", uuidToString(task.ID), "terminal_session_id", meta.SessionID, "observer_id", peer.ID)
	_ = conn.SetReadDeadline(time.Now().Add(terminalWSPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(terminalWSPongWait))
	})

	windowStart := time.Now()
	windowBytes := 0
	for {
		messageType, raw, err = conn.ReadMessage()
		if err != nil {
			slog.Debug("terminal browser disconnected", "task_id", uuidToString(task.ID), "terminal_session_id", meta.SessionID)
			return
		}
		switch messageType {
		case websocket.BinaryMessage:
			now := time.Now()
			if now.Sub(windowStart) >= time.Second {
				windowStart, windowBytes = now, 0
			}
			windowBytes += len(raw)
			if windowBytes > terminalInputBytesPerSec {
				terminalPeerError(peer, "terminal input rate exceeded")
				return
			}
			if err := h.TerminalHub.ForwardBrowserBinary(peer, raw); err != nil {
				terminalPeerError(peer, publicTerminalError(err))
			}
		case websocket.TextMessage:
			var msg terminalproto.Message
			if err := json.Unmarshal(raw, &msg); err != nil {
				terminalPeerError(peer, "invalid terminal control message")
				continue
			}
			if msg.SessionID == "" {
				msg.SessionID = meta.SessionID
			}
			sessionID, parseErr := uuid.Parse(msg.SessionID)
			if parseErr != nil || msg.SessionID != meta.SessionID {
				terminalPeerError(peer, "terminal session mismatch")
				continue
			}
			switch msg.Type {
			case "claim_control", "claim":
				token, expires, claimErr := h.TerminalHub.ClaimControl(sessionID, peer)
				if claimErr != nil {
					terminalPeerError(peer, publicTerminalError(claimErr))
				} else {
					h.persistTerminalLease(r.Context(), sessionID, userID, token, expires)
					h.persistTerminalControlEvent(r.Context(), sessionID, userID, "claim")
				}
			case "renew_control", "renew":
				expires, renewErr := h.TerminalHub.RenewControl(sessionID, peer, msg.LeaseToken)
				if renewErr != nil {
					terminalPeerError(peer, publicTerminalError(renewErr))
				} else {
					h.persistTerminalLease(r.Context(), sessionID, userID, msg.LeaseToken, expires)
					h.persistTerminalControlEvent(r.Context(), sessionID, userID, "renew")
				}
			case "release_control", "release":
				if releaseErr := h.TerminalHub.ReleaseControl(sessionID, peer, msg.LeaseToken); releaseErr != nil {
					terminalPeerError(peer, publicTerminalError(releaseErr))
				} else {
					h.deleteTerminalLease(r.Context(), sessionID)
					h.persistTerminalControlEvent(r.Context(), sessionID, userID, "release")
				}
			case "resize", "ctrl_c":
				if forwardErr := h.TerminalHub.ForwardBrowserMessage(peer, msg); forwardErr != nil {
					terminalPeerError(peer, publicTerminalError(forwardErr))
				}
			case "ack":
				// The bounded replay ring is sequence-addressed; the next reconnect's
				// attach.last_seq is authoritative. ACKs need no durable write.
			case "ping":
				terminalEnqueueJSON(peer, terminalproto.Message{Type: "pong", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, OutputSeq: meta.OutputSeq})
			default:
				terminalPeerError(peer, "unsupported terminal control message")
			}
		}
	}
}

func (h *Handler) authorizeTaskTerminalIdentity(ctx context.Context, taskID, userID, workspaceID string) (db.AgentTaskQueue, bool) {
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: taskUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		return db.AgentTaskQueue{}, false
	}
	if task.ChatSessionID.Valid {
		chat, err := h.Queries.GetChatSessionInWorkspace(ctx, db.GetChatSessionInWorkspaceParams{ID: task.ChatSessionID, WorkspaceID: workspaceUUID})
		return task, err == nil && uuidToString(chat.CreatorID) == userID
	}
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: workspaceUUID})
	if err != nil || !h.canAccessPrivateAgent(ctx, agent, "member", userID, workspaceID) {
		return db.AgentTaskQueue{}, false
	}
	return task, true
}

func (h *Handler) DaemonTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.TerminalPTYEnabled || h.TerminalHub == nil {
		writeError(w, http.StatusNotFound, "terminal unavailable")
		return
	}
	runtimeIDs := parseRuntimeIDs(r)
	if len(runtimeIDs) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_ids required")
		return
	}
	authorizedWorkspaces := make(map[string]string, len(runtimeIDs))
	daemonID := middleware.DaemonIDFromContext(r.Context())
	for _, runtimeID := range runtimeIDs {
		rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
		if !ok {
			return
		}
		var daemonOK bool
		daemonID, daemonOK = bindTerminalDaemonID(daemonID, rt.DaemonID.String, rt.DaemonID.Valid)
		if !daemonOK {
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		authorizedWorkspaces[runtimeID] = uuidToString(rt.WorkspaceID)
	}
	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := terminalhub.NewPeer(uuid.NewString(), requestUserID(r), runtimeIDs)
	h.TerminalHub.RegisterDaemon(peer)
	defer func() {
		h.TerminalHub.UnregisterPeer(peer)
		_ = conn.Close()
	}()
	go terminalWritePump(conn, peer)
	conn.SetReadLimit(terminalWSReadLimit)
	_ = conn.SetReadDeadline(time.Now().Add(terminalWSPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(terminalWSPongWait))
	})
	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.BinaryMessage {
			if err := h.TerminalHub.PublishDaemonBinary(peer, raw); err != nil {
				terminalPeerError(peer, publicTerminalError(err))
			}
			continue
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var msg terminalproto.Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			terminalPeerError(peer, "invalid daemon terminal control message")
			continue
		}
		switch msg.Type {
		case "hello":
			terminalEnqueueJSON(peer, terminalproto.Message{Type: "hello", ProtocolVersion: int(terminalproto.Version), Status: "ready"})
		case "session":
			workspaceID, runtimeOK := authorizedWorkspaces[msg.RuntimeID]
			if !runtimeOK || msg.WorkspaceID != workspaceID || msg.DaemonID == "" || msg.DaemonID != daemonID {
				terminalPeerError(peer, "terminal session runtime mismatch")
				continue
			}
			taskID, taskErr := util.ParseUUID(msg.TaskID)
			workspaceUUID, workspaceErr := util.ParseUUID(msg.WorkspaceID)
			if taskErr != nil || workspaceErr != nil {
				terminalPeerError(peer, "terminal session task mismatch")
				continue
			}
			task, taskErr := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: taskID, WorkspaceID: workspaceUUID})
			generation := int(task.Attempt)
			if generation < 1 {
				generation = 1
			}
			if taskErr != nil || msg.AgentID != uuidToString(task.AgentID) || msg.IssueID != uuidToString(task.IssueID) || msg.RuntimeID != uuidToString(task.RuntimeID) || msg.Generation != generation {
				terminalPeerError(peer, "terminal session task mismatch")
				continue
			}
			meta, registerErr := h.TerminalHub.RegisterSession(peer, msg)
			if registerErr != nil {
				terminalPeerError(peer, publicTerminalError(registerErr))
				continue
			}
			terminalEnqueueJSON(peer, terminalproto.Message{Type: "registered", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, TaskID: meta.TaskID, OutputSeq: meta.OutputSeq})
		case "state", "exit", "structured_observation":
			if publishErr := h.TerminalHub.PublishDaemonMessage(peer, msg); publishErr != nil {
				terminalPeerError(peer, publicTerminalError(publishErr))
			}
		default:
			terminalPeerError(peer, "unsupported daemon terminal control message")
		}
	}
}

func bindTerminalDaemonID(current, runtimeDaemonID string, valid bool) (string, bool) {
	if !valid || runtimeDaemonID == "" {
		return "", false
	}
	if current == "" {
		return runtimeDaemonID, true
	}
	return current, current == runtimeDaemonID
}

func terminalWritePump(conn *websocket.Conn, peer *terminalhub.Peer) {
	defer conn.Close()
	ticker := time.NewTicker(terminalWSPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case item := <-peer.Send:
			_ = conn.SetWriteDeadline(time.Now().Add(terminalWSWriteWait))
			if err := conn.WriteMessage(item.MessageType, item.Data); err != nil {
				peer.Close()
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(terminalWSWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				peer.Close()
				return
			}
		case <-peer.Done():
			return
		}
	}
}

func terminalEnqueueJSON(peer *terminalhub.Peer, msg terminalproto.Message) {
	raw, err := json.Marshal(msg)
	if err == nil {
		peer.Enqueue(websocket.TextMessage, raw)
	}
}

func terminalPeerError(peer *terminalhub.Peer, message string) {
	terminalEnqueueJSON(peer, terminalproto.Message{Type: "error", ProtocolVersion: int(terminalproto.Version), Error: message})
}

func publicTerminalError(err error) string {
	switch {
	case errors.Is(err, terminalhub.ErrSessionNotFound):
		return "terminal session unavailable"
	case errors.Is(err, terminalhub.ErrNotController):
		return "terminal is read-only; claim control to type"
	case errors.Is(err, terminalhub.ErrLeaseConflict):
		return "another viewer controls this terminal"
	case errors.Is(err, terminalproto.ErrInvalidFrame):
		return "invalid terminal frame"
	default:
		return "terminal relay unavailable"
	}
}

func (h *Handler) authorizeTaskTerminal(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	workspaceID := ctxWorkspaceID(r.Context())
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	taskUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return db.AgentTaskQueue{}, "", false
	}
	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: taskUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return db.AgentTaskQueue{}, "", false
	}
	if task.ChatSessionID.Valid {
		chat, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{ID: task.ChatSessionID, WorkspaceID: workspaceUUID})
		if err != nil || uuidToString(chat.CreatorID) != userID {
			writeError(w, http.StatusForbidden, "not your task")
			return db.AgentTaskQueue{}, "", false
		}
	} else {
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: workspaceUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return db.AgentTaskQueue{}, "", false
		}
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return db.AgentTaskQueue{}, "", false
		}
	}
	return task, workspaceID, true
}

func (h *Handler) persistTerminalMetadata(meta terminalhub.Metadata) {
	if h.DB == nil || !meta.Available {
		return
	}
	sessionID, err := util.ParseUUID(meta.SessionID)
	if err != nil {
		return
	}
	taskID, err := util.ParseUUID(meta.TaskID)
	if err != nil {
		return
	}
	workspaceID, err := util.ParseUUID(meta.WorkspaceID)
	if err != nil {
		return
	}
	runtimeID, err := util.ParseUUID(meta.RuntimeID)
	if err != nil {
		return
	}
	ended := meta.Status == "exited" || meta.Status == "failed"
	_, err = h.DB.Exec(context.Background(), `
INSERT INTO agent_terminal_session
    (id, workspace_id, issue_id, task_id, agent_id, runtime_id, daemon_id, generation, protocol_version, provider, terminal_mode, process_state, structured_observation, cols, rows, output_seq, provider_session_id, exit_code, exit_reason, exited_at, updated_at)
VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,NULLIF($17,''),$18,NULLIF($19,''),CASE WHEN $20 THEN NOW() ELSE NULL END,NOW())
ON CONFLICT (id) DO UPDATE SET
    process_state = EXCLUDED.process_state,
    structured_observation = EXCLUDED.structured_observation,
    cols = EXCLUDED.cols,
    rows = EXCLUDED.rows,
    output_seq = GREATEST(agent_terminal_session.output_seq, EXCLUDED.output_seq),
    provider_session_id = COALESCE(EXCLUDED.provider_session_id, agent_terminal_session.provider_session_id),
    exit_code = COALESCE(EXCLUDED.exit_code, agent_terminal_session.exit_code),
    exit_reason = COALESCE(EXCLUDED.exit_reason, agent_terminal_session.exit_reason),
    exited_at = COALESCE(EXCLUDED.exited_at, agent_terminal_session.exited_at),
    updated_at = NOW()`, sessionID, workspaceID, meta.IssueID, taskID, meta.AgentID, runtimeID, meta.DaemonID, meta.Generation, meta.ProtocolVersion, meta.Provider, meta.Mode, meta.Status, meta.StructuredObservation, int32(meta.Cols), int32(meta.Rows), int64(meta.OutputSeq), meta.ProviderSessionID, meta.ExitCode, meta.ExitReason, ended)
	if err != nil {
		slog.Warn("terminal metadata persistence failed", "terminal_session_id", meta.SessionID, "error_code", "metadata_persist_failed")
	}
}

func (h *Handler) loadTerminalMetadata(ctx context.Context, taskID interface{}) (terminalhub.Metadata, bool) {
	if h.DB == nil {
		return terminalhub.Metadata{}, false
	}
	var meta terminalhub.Metadata
	var cols, rows int32
	var outputSeq int64
	err := h.DB.QueryRow(ctx, `
SELECT id::text, task_id::text, workspace_id::text, COALESCE(issue_id::text,''), agent_id::text,
       runtime_id::text, daemon_id::text, provider, terminal_mode, process_state,
       structured_observation, generation, protocol_version, cols, rows, output_seq,
       COALESCE(provider_session_id,''), exit_code, COALESCE(exit_reason,'')
FROM agent_terminal_session WHERE task_id = $1 ORDER BY generation DESC LIMIT 1`, taskID).Scan(
		&meta.SessionID, &meta.TaskID, &meta.WorkspaceID, &meta.IssueID, &meta.AgentID, &meta.RuntimeID, &meta.DaemonID,
		&meta.Provider, &meta.Mode, &meta.Status, &meta.StructuredObservation, &meta.Generation, &meta.ProtocolVersion,
		&cols, &rows, &outputSeq, &meta.ProviderSessionID, &meta.ExitCode, &meta.ExitReason,
	)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("terminal metadata lookup failed", "error_code", "metadata_lookup_failed")
		}
		return terminalhub.Metadata{}, false
	}
	meta.Capability = "terminal-pty-v1"
	meta.Cols, meta.Rows, meta.OutputSeq = uint16(cols), uint16(rows), uint64(outputSeq)
	return meta, true
}

func (h *Handler) persistTerminalLease(ctx context.Context, sessionID uuid.UUID, userID, token string, expires time.Time) {
	if h.DB == nil {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return
	}
	hash := sha256.Sum256([]byte(token))
	if _, err := h.DB.Exec(ctx, `
INSERT INTO agent_terminal_control_lease (session_id, controller_user_id, lease_token_hash, expires_at, updated_at)
VALUES ($1,$2,$3,$4,NOW())
ON CONFLICT (session_id) DO UPDATE SET controller_user_id=EXCLUDED.controller_user_id, lease_token_hash=EXCLUDED.lease_token_hash, expires_at=EXCLUDED.expires_at, updated_at=NOW()`, sessionID, userUUID, hash[:], expires); err != nil {
		slog.Warn("terminal lease persistence failed", "terminal_session_id", sessionID, "error_code", "lease_persist_failed")
	}
}

func (h *Handler) deleteTerminalLease(ctx context.Context, sessionID uuid.UUID) {
	if h.DB == nil {
		return
	}
	if _, err := h.DB.Exec(ctx, `DELETE FROM agent_terminal_control_lease WHERE session_id=$1`, sessionID); err != nil {
		slog.Warn("terminal lease deletion failed", "terminal_session_id", sessionID, "error_code", "lease_delete_failed")
	}
}

func (h *Handler) persistTerminalControlEvent(ctx context.Context, sessionID uuid.UUID, userID, eventType string) {
	if h.DB == nil {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return
	}
	if _, err := h.DB.Exec(ctx, `
INSERT INTO agent_terminal_control_event (id, terminal_session_id, user_id, event_type, metadata)
VALUES ($1,$2,$3,$4,'{}'::jsonb)`, uuid.New(), sessionID, userUUID, eventType); err != nil {
		slog.Warn("terminal control audit persistence failed", "terminal_session_id", sessionID, "error_code", "control_event_persist_failed")
	}
}
