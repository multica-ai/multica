package dictation

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	envStreamURL = "CEREBRO_DICTATION_STREAM_URL"

	writeWait = 10 * time.Second
)

type Handler struct {
	streamURL string
	dialer    *websocket.Dialer
	upgrader  websocket.Upgrader
	queries   *db.Queries
	patCache  *auth.PATCache
}

type Options struct {
	StreamURL string
	Dialer    *websocket.Dialer
	Queries   *db.Queries
	PATCache  *auth.PATCache
}

type outboundError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewFromEnv(queries *db.Queries, patCache *auth.PATCache) *Handler {
	return New(Options{
		StreamURL: os.Getenv(envStreamURL),
		Queries:   queries,
		PATCache:  patCache,
	})
}

func New(opts Options) *Handler {
	dialer := opts.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	return &Handler{
		streamURL: strings.TrimSpace(opts.StreamURL),
		dialer:    dialer,
		queries:   opts.Queries,
		patCache:  opts.PATCache,
		upgrader: websocket.Upgrader{
			CheckOrigin: checkOrigin,
		},
	}
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("dictation websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	workspaceID := h.workspaceIDFromRequest(r)
	if workspaceID == "" {
		writeError(conn, "unauthorized", "Dictation requires a workspace.")
		return
	}
	userID, err := h.userIDFromRequest(r)
	if err != nil {
		slog.Warn("dictation websocket auth failed", "error", err)
		writeError(conn, "unauthorized", "Dictation requires an authenticated workspace member.")
		return
	}
	if userID == "" {
		tokenString, err := readAuthMessage(conn)
		if err != nil {
			writeError(conn, "unauthorized", "Dictation requires an authenticated workspace member.")
			return
		}
		userID, err = h.authenticateToken(r.Context(), tokenString)
		if err != nil {
			slog.Warn("dictation websocket token auth failed", "error", err)
			writeError(conn, "unauthorized", "Dictation requires an authenticated workspace member.")
			return
		}
		if err := writeFrame(conn, websocket.TextMessage, []byte(`{"type":"auth_ack"}`)); err != nil {
			return
		}
	}
	if !h.isWorkspaceMember(r.Context(), userID, workspaceID) {
		writeError(conn, "unauthorized", "Dictation requires an authenticated workspace member.")
		return
	}
	if h.streamURL == "" {
		writeError(conn, "backend_not_configured", "Dictation streaming backend is not configured.")
		return
	}

	upstreamURL, err := h.buildUpstreamURL(workspaceID, userID)
	if err != nil {
		writeError(conn, "invalid_backend_url", "Dictation streaming backend URL is invalid.")
		return
	}

	headers := http.Header{}
	headers.Set("X-Workspace-ID", workspaceID)
	headers.Set("X-User-ID", userID)
	upstream, _, err := h.dialer.DialContext(r.Context(), upstreamURL, headers)
	if err != nil {
		slog.Warn("dictation upstream dial failed", "error", err)
		writeError(conn, "backend_unavailable", "Dictation streaming backend is unavailable.")
		return
	}
	defer upstream.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	errCh := make(chan error, 2)
	go proxyFrames(ctx, conn, upstream, errCh)
	go proxyFrames(ctx, upstream, conn, errCh)

	<-errCh
	cancel()
	closeWebSocket(conn)
	closeWebSocket(upstream)
}

func (h *Handler) workspaceIDFromRequest(r *http.Request) string {
	if workspaceID := middleware.WorkspaceIDFromContext(r.Context()); workspaceID != "" {
		return workspaceID
	}
	return strings.TrimSpace(chi.URLParam(r, "id"))
}

func (h *Handler) userIDFromRequest(r *http.Request) (string, error) {
	tokenString := tokenFromRequest(r)
	if tokenString == "" {
		return "", nil
	}
	return h.authenticateToken(r.Context(), tokenString)
}

func tokenFromRequest(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString != authHeader {
			return tokenString
		}
	}
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	return ""
}

type authFrame struct {
	Type    string `json:"type"`
	Payload struct {
		Token string `json:"token"`
	} `json:"payload"`
}

func readAuthMessage(conn *websocket.Conn) (string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(writeWait))
	defer conn.SetReadDeadline(time.Time{})
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return "", err
	}
	if messageType != websocket.TextMessage {
		return "", http.ErrNoCookie
	}
	var frame authFrame
	if err := json.Unmarshal(payload, &frame); err != nil {
		return "", err
	}
	token := strings.TrimSpace(frame.Payload.Token)
	if frame.Type != "auth" || token == "" {
		return "", http.ErrNoCookie
	}
	return token, nil
}

func (h *Handler) authenticateToken(ctx context.Context, tokenString string) (string, error) {
	if strings.HasPrefix(tokenString, "mul_") {
		hash := auth.HashToken(tokenString)
		if h.patCache != nil {
			if userID, ok := h.patCache.Get(ctx, hash); ok {
				return userID, nil
			}
		}
		if h.queries == nil {
			return "", http.ErrNoCookie
		}
		pat, err := h.queries.GetPersonalAccessTokenByHash(ctx, hash)
		if err != nil {
			return "", err
		}
		userID := util.UUIDToString(pat.UserID)
		if h.patCache != nil {
			var expiresAt time.Time
			if pat.ExpiresAt.Valid {
				expiresAt = pat.ExpiresAt.Time
			}
			h.patCache.Set(ctx, hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))
		}
		return userID, nil
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return "", jwt.ErrSignatureInvalid
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", jwt.ErrInvalidKey
	}
	sub, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(sub) == "" {
		return "", jwt.ErrInvalidKey
	}
	return sub, nil
}

func (h *Handler) isWorkspaceMember(ctx context.Context, userID, workspaceID string) bool {
	if h.queries == nil {
		return false
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return false
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}
	_, err = h.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userUUID,
		WorkspaceID: workspaceUUID,
	})
	return err == nil
}

func (h *Handler) buildUpstreamURL(workspaceID, userID string) (string, error) {
	u, err := url.Parse(h.streamURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	q := u.Query()
	if q.Get("workspace_id") == "" {
		q.Set("workspace_id", workspaceID)
	}
	if q.Get("user_id") == "" {
		q.Set("user_id", userID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func proxyFrames(ctx context.Context, src, dst *websocket.Conn, errCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			errCh <- ctx.Err()
			return
		default:
		}

		messageType, payload, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		if err := writeFrame(dst, messageType, payload); err != nil {
			errCh <- err
			return
		}
	}
}

func writeError(conn *websocket.Conn, code, message string) {
	payload, _ := json.Marshal(outboundError{
		Type:    "error",
		Code:    code,
		Message: message,
	})
	_ = writeFrame(conn, websocket.TextMessage, payload)
}

func writeFrame(conn *websocket.Conn, messageType int, payload []byte) error {
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteMessage(messageType, payload)
}

func closeWebSocket(conn *websocket.Conn) {
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(writeWait),
	)
}

var allowedOrigins = struct {
	once sync.Once
	list []string
}{}

func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	allowedOrigins.once.Do(func() {
		allowedOrigins.list = loadAllowedOrigins()
	})
	for _, allowed := range allowedOrigins.list {
		if origin == allowed {
			return true
		}
	}
	slog.Warn("dictation websocket rejected origin", "origin", origin)
	return false
}

func loadAllowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return []string{
			"http://localhost:3000",
			"http://localhost:5173",
			"http://localhost:5174",
		}
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if origin := strings.TrimSpace(part); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}
