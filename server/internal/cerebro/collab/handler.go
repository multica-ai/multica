package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/auth"
)

// Querier is the slice of the database this handler needs. Tests supply a fake.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Access decides what a user may do with a note. It is satisfied in production
// by the note package's own visibility + folder-grant rules, so live editing
// can never widen access beyond what saving already allows.
type Access interface {
	// CanSee reports whether the user may open the note at all.
	CanSee(ctx context.Context, noteID, userID string) (bool, error)
	// CanEdit reports whether the user may change it. A viewer still joins the
	// room — they see other people's carets and text live — but their steps are
	// refused, so read-only stays read-only.
	CanEdit(ctx context.Context, noteID, userID string) (bool, error)
}

// Handler serves the live co-editing WebSocket for notes.
type Handler struct {
	Rooms  *Registry
	DB     Querier
	Access Access

	// CheckOrigin gates browser upgrades; same CSWSH defense as the terminal
	// endpoint. Tests override it.
	CheckOrigin func(r *http.Request) bool

	// WriteTimeout bounds a single frame write to a peer.
	WriteTimeout time.Duration
}

// New builds the handler.
func New(db Querier, access Access) *Handler {
	return &Handler{
		Rooms:        NewRegistry(),
		DB:           db,
		Access:       access,
		CheckOrigin:  defaultCheckOrigin,
		WriteTimeout: 10 * time.Second,
	}
}

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
	if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") {
		return true
	}
	return false
}

const (
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
	maxFrame   = 1 << 20 // 1 MiB: a paste of a large document, not a stream
)

// ServeWS upgrades the connection and runs one peer's session in a room.
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	noteID := chi.URLParam(r, "id")
	if _, err := uuid.Parse(noteID); err != nil {
		http.Error(w, `{"error":"invalid note id"}`, http.StatusBadRequest)
		return
	}

	// Cookie auth before the upgrade keeps the common browser path to a single
	// round-trip; PAT clients authenticate in the first frame instead.
	//
	// Both paths go through authenticate(). This endpoint is registered outside
	// the auth middleware (it authenticates itself, like the terminal WS), so
	// the X-User-ID header here is whatever the caller typed — never a value the
	// middleware stamped. Trusting it let any caller that reaches this route
	// claim any user id and read or write their notes, so it is not read at all.
	var userID string
	if cookie, err := r.Cookie(auth.AuthCookieName); err == nil && cookie.Value != "" {
		uid, aerr := h.authenticate(r.Context(), cookie.Value)
		if aerr != nil {
			http.Error(w, `{"error":"invalid auth cookie"}`, http.StatusUnauthorized)
			return
		}
		userID = uid
	}

	upgrader := websocket.Upgrader{CheckOrigin: h.CheckOrigin}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("collab: ws upgrade failed", "error", err, "note", noteID)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(maxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	if userID == "" {
		token, aerr := h.readAuthFrame(conn)
		if aerr != nil {
			h.writeNow(conn, encode(SimpleMessage{Type: MsgAuthError, Reason: "auth failed"}))
			return
		}
		uid, aerr := h.authenticate(r.Context(), token)
		if aerr != nil {
			h.writeNow(conn, encode(SimpleMessage{Type: MsgAuthError, Reason: "invalid token"}))
			return
		}
		userID = uid
		h.writeNow(conn, encode(SimpleMessage{Type: MsgAuthAck}))
	}

	canSee, err := h.Access.CanSee(r.Context(), noteID, userID)
	if err != nil || !canSee {
		h.writeNow(conn, encode(SimpleMessage{Type: MsgAuthError, Reason: "no access to this note"}))
		return
	}
	canEdit, err := h.Access.CanEdit(r.Context(), noteID, userID)
	if err != nil {
		canEdit = false
	}

	h.run(r.Context(), conn, noteID, userID, h.displayName(r.Context(), userID), canEdit)
}

// run drives one peer: join, pump frames, leave.
func (h *Handler) run(ctx context.Context, conn *websocket.Conn, noteID, userID, name string, canEdit bool) {
	room, created := h.Rooms.Acquire(noteID)
	peer := NewPeer(uuid.NewString(), userID, name, canEdit)
	peers := room.Join(peer)

	// A joiner needs the room's live document. The first peer in a fresh room
	// keeps the body it already loaded from the database; a later joiner gets
	// the cached snapshot, and we ask an existing peer to refresh it so the
	// next joiner is current.
	welcome := WelcomeMessage{
		Type:    MsgWelcome,
		PeerID:  peer.ID,
		Color:   peer.Color,
		CanEdit: canEdit,
		Version: room.Version(),
		Peers:   peers,
	}
	if !created {
		welcome.Snapshot = room.Snapshot()
		if welcome.Snapshot != nil {
			if since, serr := room.StepsSince(welcome.Snapshot.Version); serr == nil {
				welcome.Steps = since
			}
		}
	}
	peer.Send(encode(welcome))

	if !created {
		if donor := room.OldestPeer(peer.ID); donor != nil {
			donor.Send(encode(SimpleMessage{Type: MsgSnapshotRequest}))
		}
	}
	room.Broadcast(encode(PeersMessage{Type: MsgPeers, Peers: peers}), peer.ID)

	done := make(chan struct{})
	go h.writePump(conn, peer, done)

	h.readPump(ctx, conn, room, peer)

	close(done)
	remaining, empty := room.Leave(peer.ID)
	if empty {
		h.Rooms.Release(noteID)
		return
	}
	room.Broadcast(encode(PeersMessage{Type: MsgPeers, Peers: remaining}), "")
}

// readPump consumes frames from one peer until the connection dies.
func (h *Handler) readPump(_ context.Context, conn *websocket.Conn, room *Room, peer *Peer) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			peer.Send(encode(SimpleMessage{Type: MsgError, Reason: "malformed frame"}))
			continue
		}

		switch msg.Type {
		case MsgPing:
			peer.Send(encode(SimpleMessage{Type: MsgPong}))

		case MsgCaret:
			room.Broadcast(encode(CaretMessage{
				Type:   MsgCaret,
				PeerID: peer.ID,
				Name:   peer.Name,
				Color:  peer.Color,
				Anchor: msg.Anchor,
				Head:   msg.Head,
			}), peer.ID)

		case MsgSteps:
			if !peer.CanEdit {
				// Read-only peers watch; they never write. Telling them plainly
				// beats the old failure mode where a shared note looked
				// editable and the text vanished on save (FIR-1981).
				peer.Send(encode(SimpleMessage{Type: MsgError, Reason: "read-only: you can view this note but not edit it"}))
				continue
			}
			accepted, missing, version, serr := room.Submit(peer.ID, msg.Version, msg.Steps)
			if serr != nil {
				peer.Send(encode(SimpleMessage{Type: MsgReload, Reason: "too far behind"}))
				continue
			}
			if !accepted {
				peer.Send(encode(RejectedMessage{Type: MsgRejected, Version: version, Missing: missing}))
				continue
			}
			applied, aerr := room.StepsSince(msg.Version)
			if aerr != nil {
				peer.Send(encode(SimpleMessage{Type: MsgReload, Reason: "too far behind"}))
				continue
			}
			room.Broadcast(encode(StepsAppliedMessage{Type: MsgStepsApplied, Version: version, Steps: applied}), "")
			if room.NeedsSnapshot() {
				peer.Send(encode(SimpleMessage{Type: MsgSnapshotRequest}))
			}

		case MsgSnapshot:
			if len(msg.Doc) == 0 {
				continue
			}
			room.SetSnapshot(Snapshot{Version: msg.Version, Doc: msg.Doc})
		}
	}
}

// writePump serialises all writes to a connection — gorilla/websocket allows
// only one concurrent writer.
func (h *Handler) writePump(conn *websocket.Conn, peer *Peer, done <-chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case msg := <-peer.Outbound():
			_ = conn.SetWriteDeadline(time.Now().Add(h.WriteTimeout))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(h.WriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Handler) writeNow(conn *websocket.Conn, msg []byte) {
	_ = conn.SetWriteDeadline(time.Now().Add(h.WriteTimeout))
	_ = conn.WriteMessage(websocket.TextMessage, msg)
}

// readAuthFrame reads the first frame, expecting {"type":"auth","token":"..."}.
func (h *Handler) readAuthFrame(conn *websocket.Conn) (string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	var msg ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != MsgAuth || msg.Token == "" {
		return "", fmt.Errorf("expected auth frame")
	}
	return msg.Token, nil
}

// authenticate resolves a PAT or JWT to a user id, mirroring the terminal
// endpoint's WS auth.
func (h *Handler) authenticate(ctx context.Context, token string) (string, error) {
	if strings.HasPrefix(token, "mul_") {
		hash := auth.HashToken(token)
		var userID string
		err := h.DB.QueryRow(ctx,
			`SELECT user_id::text FROM personal_access_tokens WHERE hash = $1 AND (expires_at IS NULL OR expires_at > now())`,
			hash).Scan(&userID)
		if err != nil {
			return "", fmt.Errorf("invalid PAT")
		}
		return userID, nil
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !parsed.Valid {
		return "", fmt.Errorf("invalid token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims")
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		sub, _ = claims["user_id"].(string)
	}
	if sub == "" {
		return "", fmt.Errorf("no subject in token")
	}
	return sub, nil
}

// displayName looks up the name shown on the remote caret. A failed lookup is
// not fatal — an unnamed caret is still a useful caret.
func (h *Handler) displayName(ctx context.Context, userID string) string {
	if h.DB == nil {
		return ""
	}
	var name string
	// The table is "user" (singular, quoted — it is a reserved word). Querying
	// a non-existent "users" errored on every join, so every caret and presence
	// line rendered without a name.
	if err := h.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(name, ''), email) FROM "user" WHERE id = $1`, userID).Scan(&name); err != nil {
		return ""
	}
	return name
}
