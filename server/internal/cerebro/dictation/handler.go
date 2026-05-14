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

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/middleware"
)

const (
	envStreamURL = "CEREBRO_DICTATION_STREAM_URL"

	writeWait = 10 * time.Second
)

type Handler struct {
	streamURL string
	dialer    *websocket.Dialer
	upgrader  websocket.Upgrader
}

type Options struct {
	StreamURL string
	Dialer    *websocket.Dialer
}

type outboundError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewFromEnv() *Handler {
	return New(Options{
		StreamURL: os.Getenv(envStreamURL),
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

	workspaceID := middleware.WorkspaceIDFromContext(r.Context())
	userID := r.Header.Get("X-User-ID")
	if workspaceID == "" || userID == "" {
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
