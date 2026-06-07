package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/runtime"
)

// PoolQuerier is the subset of pgxpool.Pool the handler needs. Tests can
// supply a fake; production wires in the real pool.
type PoolQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Handler exposes HTTP and WebSocket endpoints for the interactive
// terminal feature. It is constructed with the broker and a DB pool used
// only to read/write the agent_runtime.presentation_mode column (sqlc-
// generated access is the long-term home; raw pgx until the next
// regeneration).
type Handler struct {
	Broker *Broker
	DB     PoolQuerier

	// CheckOrigin gates browser WS upgrades. Defaults to a strict same-
	// origin check based on the Host header; tests override.
	CheckOrigin func(r *http.Request) bool
}

// New constructs the cerebro terminal handler.
func New(broker *Broker, pool *pgxpool.Pool) *Handler {
	return &Handler{
		Broker:      broker,
		DB:          pool,
		CheckOrigin: defaultCheckOrigin,
	}
}

// defaultCheckOrigin allows same-host and localhost variants. The cerebro
// terminal endpoint is auth-gated by the surrounding middleware, so origin
// is a CSWSH defense rather than the primary access control.
func defaultCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	if host == "" {
		return false
	}
	for _, scheme := range []string{"https://", "http://"} {
		if origin == scheme+host {
			return true
		}
	}
	// Dev: allow localhost variants regardless of port.
	if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
		return true
	}
	return false
}

// ----- Request/response types -----

type createSessionRequest struct {
	RuntimeID string   `json:"runtime_id"`
	Command   []string `json:"command,omitempty"`
}

type createSessionResponse struct {
	ID         string   `json:"id"`
	RuntimeID  string   `json:"runtime_id"`
	Command    []string `json:"command"`
	CreatedAt  string   `json:"created_at"`
	AttachPath string   `json:"attach_path"`
}

type listSessionsResponse struct {
	Sessions []sessionSummary `json:"sessions"`
}

// activeSessionResponse describes the live session (if any) attached to a
// runtime. The active-session endpoint returns this on 200, or 204 with no
// body when no session exists.
type activeSessionResponse struct {
	SessionID  string `json:"session_id"`
	AttachPath string `json:"attach_path"`
	CreatedAt  string `json:"created_at"`
}

type sessionSummary struct {
	ID        string   `json:"id"`
	RuntimeID string   `json:"runtime_id"`
	Command   []string `json:"command"`
	CreatedAt string   `json:"created_at"`
	Closed    bool     `json:"closed"`
}

// ----- HTTP handlers -----

// CreateSession handles POST /api/cerebro/terminal/sessions. The runtime
// must have presentation_mode = 'interactive', and the caller must belong
// to the runtime's workspace.
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user")
		return
	}

	var body createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.RuntimeID == "" {
		writeJSONError(w, http.StatusBadRequest, "runtime_id required")
		return
	}

	wsID, mode, err := h.readRuntime(r.Context(), body.RuntimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "runtime not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if mode != "interactive" {
		writeJSONError(w, http.StatusConflict, "runtime is not in interactive presentation mode")
		return
	}
	if !h.userInWorkspace(r.Context(), userID, wsID) {
		writeJSONError(w, http.StatusForbidden, "no workspace access")
		return
	}

	// Process lifetime is decoupled from the HTTP request; a session may
	// outlive the create call by hours. context.Background is intentional.
	s, err := h.Broker.Create(context.Background(), CreateOptions{
		RuntimeID:   body.RuntimeID,
		WorkspaceID: wsID,
		OwnerUserID: userID,
		Command:     body.Command,
	})
	if err != nil {
		slog.Error("terminal: create session", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, createSessionResponse{
		ID:         s.ID,
		RuntimeID:  s.RuntimeID,
		Command:    s.Command,
		CreatedAt:  s.CreatedAt.Format(time.RFC3339Nano),
		AttachPath: "/api/cerebro/terminal/sessions/" + s.ID + "/ws",
	})
}

// ListSessions returns the caller's active sessions. Used by the UI to
// re-attach after a reload.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user")
		return
	}
	all := h.Broker.List("", userID)
	out := listSessionsResponse{Sessions: make([]sessionSummary, 0, len(all))}
	for _, s := range all {
		out.Sessions = append(out.Sessions, sessionSummary{
			ID:        s.ID,
			RuntimeID: s.RuntimeID,
			Command:   s.Command,
			CreatedAt: s.CreatedAt.Format(time.RFC3339Nano),
			Closed:    s.closed.Load(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// DeleteSession kills the child and removes the session.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	sessionID := chi.URLParam(r, "sessionId")
	s := h.Broker.Get(sessionID)
	if s == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.OwnerUserID != userID {
		writeJSONError(w, http.StatusForbidden, "not owner")
		return
	}
	_ = h.Broker.Close(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// AttachWS upgrades to a WebSocket bound to one session.
//
// Wire format — JSON frames:
//
//	{"type":"stdout","data":"<base64>"}     server → client
//	{"type":"stdin","data":"<base64>"}      client → server
//	{"type":"exit","code":<int>}            server → client (terminal frame)
//
// Base64 avoids interpreting child output as text; the frontend decodes
// before feeding xterm.js.
func (h *Handler) AttachWS(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user")
		return
	}
	sessionID := chi.URLParam(r, "sessionId")
	s := h.Broker.Get(sessionID)
	if s == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.OwnerUserID != userID {
		writeJSONError(w, http.StatusForbidden, "not owner")
		return
	}

	upgrader := websocket.Upgrader{CheckOrigin: h.CheckOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("terminal: ws upgrade failed", "error", err, "session", sessionID)
		return
	}
	defer conn.Close()
	slog.Info("terminal: ws attached", "session", sessionID, "user", userID)

	sub, write, detach := s.Attach()
	defer detach()

	servePump(r.Context(), conn, s, sub, write)
	slog.Info("terminal: ws detached", "session", sessionID)
}

// servePump is the per-connection bidirectional pump.
func servePump(ctx context.Context, conn *websocket.Conn, s *Session, sub *Subscriber, writeStdin func([]byte) error) {
	const (
		writeWait  = 10 * time.Second
		pongWait   = 60 * time.Second
		pingPeriod = (pongWait * 9) / 10
	)
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	done := make(chan struct{})

	// Read pump: client → child stdin.
	go func() {
		defer close(done)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Data []byte `json:"data"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "stdin":
				if err := writeStdin(msg.Data); err != nil {
					return
				}
			}
		}
	}()

	// Write pump: child stdout → client + periodic pings.
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case chunk, ok := <-sub.C:
			if !ok {
				// session terminated; send exit and stop.
				frame := map[string]any{"type": "exit", "code": s.ExitCode()}
				if err := s.ExitErr(); err != nil {
					if s.ExitCode() == 0 {
						frame["code"] = -1
					}
					frame["error"] = err.Error()
				}
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = conn.WriteJSON(frame)
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteJSON(map[string]any{"type": "stdout", "data": chunk}); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ----- DB helpers (raw pgx until sqlc regen) -----

const (
	readRuntimeSQL = `
SELECT workspace_id::text, presentation_mode
FROM agent_runtime
WHERE id = $1::uuid`

	readRuntimeProviderSQL = `
SELECT provider, workspace_id::text
FROM agent_runtime
WHERE id = $1::uuid`

	updatePresentationModeSQL = `
UPDATE agent_runtime
SET presentation_mode = $2, updated_at = now()
WHERE id = $1::uuid
RETURNING workspace_id::text`

	userInWorkspaceSQL = `
SELECT EXISTS (
    SELECT 1 FROM member
    WHERE user_id = $1::uuid AND workspace_id = $2::uuid
)`
)

func (h *Handler) readRuntime(ctx context.Context, runtimeID string) (workspaceID, mode string, err error) {
	row := h.DB.QueryRow(ctx, readRuntimeSQL, runtimeID)
	err = row.Scan(&workspaceID, &mode)
	return
}

func (h *Handler) readRuntimeProvider(ctx context.Context, runtimeID string) (provider, workspaceID string, err error) {
	row := h.DB.QueryRow(ctx, readRuntimeProviderSQL, runtimeID)
	err = row.Scan(&provider, &workspaceID)
	return
}

func (h *Handler) userInWorkspace(ctx context.Context, userID, workspaceID string) bool {
	var ok bool
	row := h.DB.QueryRow(ctx, userInWorkspaceSQL, userID, workspaceID)
	if err := row.Scan(&ok); err != nil {
		return false
	}
	return ok
}

// SetPresentationMode is exposed as a separate endpoint so the runtime
// detail UI can toggle it without going through the full upstream runtime
// update payload (which doesn't know about the cerebro column).
func (h *Handler) SetPresentationMode(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	if runtimeID == "" {
		writeJSONError(w, http.StatusBadRequest, "runtime_id required")
		return
	}
	var body struct {
		PresentationMode string `json:"presentation_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.PresentationMode != "headless" && body.PresentationMode != "interactive" {
		writeJSONError(w, http.StatusBadRequest, "presentation_mode must be 'headless' or 'interactive'")
		return
	}

	// v1 of the interactive terminal mirrors local daemon stdout into the
	// broker — cloud-runtime providers (managed HTTPS gateway) don't run a
	// local stream we can tee, so the toggle is gated to daemon runtimes.
	// Headless is always allowed (it's the default + the disable path).
	if body.PresentationMode == "interactive" {
		provider, _, err := h.readRuntimeProvider(r.Context(), runtimeID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeJSONError(w, http.StatusNotFound, "runtime not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if isCloudRuntimeProvider(provider) {
			writeJSONError(w, http.StatusBadRequest, "cloud_runtime_not_supported")
			return
		}
	}

	var wsID string
	row := h.DB.QueryRow(r.Context(), updatePresentationModeSQL, runtimeID, body.PresentationMode)
	if err := row.Scan(&wsID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "runtime not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if !h.userInWorkspace(r.Context(), userID, wsID) {
		writeJSONError(w, http.StatusForbidden, "no workspace access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"runtime_id":        runtimeID,
		"presentation_mode": body.PresentationMode,
	})
}

// GetActiveSession returns the live broker session for a runtime, if one
// exists. The frontend polls or fetches this on mount to discover whether
// an interactive agent is already streaming, so it can attach without
// having to start a session it doesn't own (v1 daemon-side adopted
// sessions are the canonical source). Returns 204 No Content when no live
// session exists.
func (h *Handler) GetActiveSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing user")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	if !validUUID(runtimeID) {
		writeJSONError(w, http.StatusBadRequest, "runtimeId must be a UUID")
		return
	}
	wsID, _, err := h.readRuntime(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "runtime not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.userInWorkspace(r.Context(), userID, wsID) {
		writeJSONError(w, http.StatusForbidden, "no workspace access")
		return
	}

	s := h.Broker.GetByRuntime(runtimeID)
	if s == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, activeSessionResponse{
		SessionID:  s.ID,
		AttachPath: "/api/cerebro/terminal/sessions/" + s.ID + "/ws",
		CreatedAt:  s.CreatedAt.Format(time.RFC3339Nano),
	})
}

// isCloudRuntimeProvider reports whether a provider is a server-side
// managed runtime (today: firtal-gateway). v1 of the interactive terminal
// cannot mirror their output — they execute model calls server-side over
// HTTPS rather than streaming through a local daemon.
func isCloudRuntimeProvider(provider string) bool {
	switch provider {
	case runtime.FirtalGatewayProvider:
		return true
	default:
		return false
	}
}

// validUUID is a cheap parser-only check; the actual access control happens
// via the workspace-membership query below. Existing handlers that hand
// the raw string into `$1::uuid` would error at the DB layer on bad input;
// validating here just produces a clearer 400.
func validUUID(s string) bool {
	if s == "" {
		return false
	}
	_, err := uuid.Parse(s)
	return err == nil
}

// GetPresentationMode lets the UI fetch the current value without
// reloading the whole runtime row.
func (h *Handler) GetPresentationMode(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	runtimeID := chi.URLParam(r, "runtimeId")
	wsID, mode, err := h.readRuntime(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "runtime not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !h.userInWorkspace(r.Context(), userID, wsID) {
		writeJSONError(w, http.StatusForbidden, "no workspace access")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"runtime_id":        runtimeID,
		"presentation_mode": mode,
	})
}

// ----- Small JSON helpers -----

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
