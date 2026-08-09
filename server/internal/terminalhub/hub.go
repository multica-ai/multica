// Package terminalhub relays opaque terminal bytes between authenticated
// daemon and browser peers. It owns reconnect replay and controller leases but
// never logs or persists terminal payloads.
package terminalhub

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/pkg/terminalproto"
)

const (
	DefaultRelayRingBytes = 8 * 1024 * 1024
	DefaultLeaseDuration  = 30 * time.Second
	DefaultPeerQueue      = 256
	DefaultBrowserLimit   = 16
)

var (
	ErrSessionNotFound = errors.New("terminal session not found")
	ErrNotController   = errors.New("terminal input requires controller lease")
	ErrLeaseConflict   = errors.New("terminal controller lease is held by another client")
	ErrInvalidPeer     = errors.New("terminal peer is not authorized for the session")
	ErrGeneration      = errors.New("terminal session generation conflicts with the active task generation")
	ErrBrowserLimit    = errors.New("terminal browser connection limit reached")
)

type Outbound struct {
	MessageType int
	Data        []byte
}

type Peer struct {
	ID         string
	UserID     string
	RuntimeIDs map[string]struct{}
	Send       chan Outbound
	done       chan struct{}
	closed     atomic.Bool
}

func NewPeer(id, userID string, runtimeIDs []string) *Peer {
	runtimeSet := make(map[string]struct{}, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		runtimeSet[runtimeID] = struct{}{}
	}
	return &Peer{ID: id, UserID: userID, RuntimeIDs: runtimeSet, Send: make(chan Outbound, DefaultPeerQueue), done: make(chan struct{})}
}

func (p *Peer) Done() <-chan struct{} { return p.done }

func (p *Peer) Close() {
	if p.closed.CompareAndSwap(false, true) {
		close(p.done)
	}
}

func (p *Peer) Enqueue(messageType int, data []byte) bool {
	if p.closed.Load() {
		return false
	}
	item := Outbound{MessageType: messageType, Data: append([]byte(nil), data...)}
	select {
	case p.Send <- item:
		return true
	default:
		p.Close()
		return false
	}
}

type Metadata struct {
	Available             bool   `json:"available"`
	ProtocolVersion       int    `json:"protocol_version"`
	SessionID             string `json:"session_id,omitempty"`
	TaskID                string `json:"task_id"`
	IssueID               string `json:"issue_id,omitempty"`
	AgentID               string `json:"agent_id,omitempty"`
	WorkspaceID           string `json:"workspace_id,omitempty"`
	RuntimeID             string `json:"runtime_id,omitempty"`
	DaemonID              string `json:"daemon_id,omitempty"`
	Provider              string `json:"provider,omitempty"`
	Mode                  string `json:"mode,omitempty"`
	Status                string `json:"status,omitempty"`
	StructuredObservation string `json:"structured_observation,omitempty"`
	Generation            int    `json:"generation,omitempty"`
	Cols                  uint16 `json:"cols,omitempty"`
	Rows                  uint16 `json:"rows,omitempty"`
	OutputSeq             uint64 `json:"output_seq,omitempty"`
	ProviderSessionID     string `json:"provider_session_id,omitempty"`
	ExitCode              *int   `json:"exit_code,omitempty"`
	ExitReason            string `json:"exit_reason,omitempty"`
	Capability            string `json:"capability,omitempty"`
	ReplayAvailable       bool   `json:"replay_available"`
	OldestSeq             uint64 `json:"oldest_seq,omitempty"`
	ObserverCount         int    `json:"observer_count"`
	ControllerActive      bool   `json:"controller_active"`
}

type relayChunk struct {
	seq  uint64
	raw  []byte
	size int
}

type controllerLease struct {
	peerID    string
	userID    string
	token     string
	expiresAt time.Time
}

type session struct {
	meta      Metadata
	daemon    *Peer
	browsers  map[string]*Peer
	ring      []relayChunk
	ringBytes int
	lease     controllerLease
}

type Options struct {
	RelayRingBytes int
	LeaseDuration  time.Duration
	Now            func() time.Time
}

type Hub struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*session
	byTask   map[string]uuid.UUID
	daemons  map[string]*Peer
	opts     Options
	onChange func(Metadata)
}

func New(opts Options) *Hub {
	if opts.RelayRingBytes <= 0 {
		opts.RelayRingBytes = DefaultRelayRingBytes
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = DefaultLeaseDuration
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Hub{sessions: make(map[uuid.UUID]*session), byTask: make(map[string]uuid.UUID), daemons: make(map[string]*Peer), opts: opts}
}

func (h *Hub) SetChangeHook(fn func(Metadata)) { h.onChange = fn }

func (h *Hub) RegisterDaemon(peer *Peer) {
	h.mu.Lock()
	for runtimeID := range peer.RuntimeIDs {
		h.daemons[runtimeID] = peer
	}
	h.mu.Unlock()
}

func (h *Hub) UnregisterPeer(peer *Peer) {
	peer.Close()
	h.mu.Lock()
	for runtimeID, registered := range h.daemons {
		if registered == peer {
			delete(h.daemons, runtimeID)
		}
	}
	for _, s := range h.sessions {
		if s.daemon == peer {
			s.daemon = nil
			if s.meta.Status == "running" {
				s.meta.Status = "reconnecting"
			}
			broadcastJSON(s.browsers, terminalproto.Message{Type: "state", ProtocolVersion: int(terminalproto.Version), SessionID: s.meta.SessionID, Status: s.meta.Status, OutputSeq: s.meta.OutputSeq})
		}
		delete(s.browsers, peer.ID)
		if s.lease.peerID == peer.ID {
			s.lease = controllerLease{}
			broadcastJSON(s.browsers, terminalproto.Message{Type: "control", ProtocolVersion: int(terminalproto.Version), SessionID: s.meta.SessionID, Controller: false})
		}
	}
	h.mu.Unlock()
}

func (h *Hub) RegisterSession(peer *Peer, msg terminalproto.Message) (Metadata, error) {
	id, err := uuid.Parse(msg.SessionID)
	if err != nil || msg.TaskID == "" || msg.WorkspaceID == "" || msg.RuntimeID == "" {
		return Metadata{}, errors.New("terminal session metadata is incomplete")
	}
	if msg.Generation <= 0 {
		msg.Generation = 1
	}
	if _, ok := peer.RuntimeIDs[msg.RuntimeID]; !ok {
		return Metadata{}, ErrInvalidPeer
	}
	cols, rows := clampSize(msg.Cols, msg.Rows)
	meta := Metadata{Available: true, ProtocolVersion: int(terminalproto.Version), SessionID: id.String(), TaskID: msg.TaskID, IssueID: msg.IssueID, AgentID: msg.AgentID, WorkspaceID: msg.WorkspaceID, RuntimeID: msg.RuntimeID, DaemonID: msg.DaemonID, Provider: msg.Provider, Mode: "pty", Status: defaultString(msg.Status, "running"), StructuredObservation: defaultString(msg.StructuredObservation, "unavailable"), Generation: msg.Generation, Cols: cols, Rows: rows, OutputSeq: msg.OutputSeq, ProviderSessionID: msg.ProviderSessionID, ExitCode: msg.ExitCode, ExitReason: msg.ExitReason, Capability: "terminal-pty-v1", ReplayAvailable: true}
	h.mu.Lock()
	if activeID, ok := h.byTask[msg.TaskID]; ok && activeID != id {
		active := h.sessions[activeID]
		if active != nil && active.meta.Generation >= msg.Generation {
			h.mu.Unlock()
			return Metadata{}, ErrGeneration
		}
	}
	s, exists := h.sessions[id]
	if !exists {
		s = &session{meta: meta, browsers: make(map[string]*Peer)}
		h.sessions[id] = s
	} else if s.meta.TaskID != msg.TaskID || s.meta.WorkspaceID != msg.WorkspaceID || s.meta.RuntimeID != msg.RuntimeID || s.meta.Generation != msg.Generation {
		h.mu.Unlock()
		return Metadata{}, ErrInvalidPeer
	} else {
		if s.meta.OutputSeq > meta.OutputSeq {
			meta.OutputSeq = s.meta.OutputSeq
		}
		s.meta = meta
	}
	s.daemon = peer
	h.byTask[msg.TaskID] = id
	browsers := clonePeers(s.browsers)
	h.mu.Unlock()
	broadcastJSON(browsers, terminalproto.Message{Type: "state", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, Status: meta.Status, Cols: meta.Cols, Rows: meta.Rows, OutputSeq: meta.OutputSeq, StructuredObservation: meta.StructuredObservation})
	h.changed(meta)
	return meta, nil
}

func (h *Hub) GetByTask(taskID string) (Metadata, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	id, ok := h.byTask[taskID]
	if !ok {
		return Metadata{TaskID: taskID, ProtocolVersion: int(terminalproto.Version)}, false
	}
	s, ok := h.sessions[id]
	if !ok {
		return Metadata{TaskID: taskID, ProtocolVersion: int(terminalproto.Version)}, false
	}
	meta := s.meta
	meta.ObserverCount = len(s.browsers)
	meta.ControllerActive = s.lease.token != "" && h.opts.Now().Before(s.lease.expiresAt)
	if len(s.ring) > 0 {
		meta.OldestSeq = s.ring[0].seq
	}
	return meta, true
}

// AttachBrowser registers a read-only observer and enqueues a deterministic
// attach/replay sequence. The caller may subsequently ClaimControl.
func (h *Hub) AttachBrowser(taskID string, peer *Peer, after uint64) (Metadata, error) {
	h.mu.Lock()
	id, ok := h.byTask[taskID]
	if !ok {
		h.mu.Unlock()
		return Metadata{}, ErrSessionNotFound
	}
	s := h.sessions[id]
	if _, attached := s.browsers[peer.ID]; !attached && len(s.browsers) >= DefaultBrowserLimit {
		h.mu.Unlock()
		return Metadata{}, ErrBrowserLimit
	}
	s.browsers[peer.ID] = peer
	meta := s.meta
	chunks := append([]relayChunk(nil), s.ring...)
	oldest := uint64(0)
	if len(chunks) > 0 {
		oldest = chunks[0].seq
	}
	meta.ObserverCount = len(s.browsers)
	meta.ControllerActive = s.lease.token != "" && h.opts.Now().Before(s.lease.expiresAt)
	meta.OldestSeq = oldest
	gap := oldest > 0 && after+1 < oldest
	h.mu.Unlock()

	enqueueJSON(peer, terminalproto.Message{Type: "attached", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, TaskID: meta.TaskID, RuntimeID: meta.RuntimeID, Provider: meta.Provider, Mode: meta.Mode, Status: meta.Status, StructuredObservation: meta.StructuredObservation, Generation: meta.Generation, Cols: meta.Cols, Rows: meta.Rows, OutputSeq: meta.OutputSeq})
	if gap {
		enqueueJSON(peer, terminalproto.Message{Type: "gap", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, LastSeq: after, OldestSeq: oldest, OutputSeq: meta.OutputSeq})
	}
	enqueueReplay(peer, id, chunks, after)
	enqueueJSON(peer, terminalproto.Message{Type: "replay_complete", ProtocolVersion: int(terminalproto.Version), SessionID: meta.SessionID, OutputSeq: meta.OutputSeq})
	return meta, nil
}

// enqueueReplay coalesces adjacent historical output into protocol-sized
// frames. Interactive TUIs frequently emit thousands of tiny ANSI writes at
// startup; replaying those one-for-one can overflow an otherwise healthy
// browser peer's bounded queue before its writer goroutine has a chance to
// drain. The sequence on each batch is the last included source sequence, so a
// reconnect ACK still means the browser has received every byte through it.
func enqueueReplay(peer *Peer, sessionID uuid.UUID, chunks []relayChunk, after uint64) {
	payload := make([]byte, 0, terminalproto.MaxPayloadBytes)
	sequence := uint64(0)
	flush := func() bool {
		if len(payload) == 0 {
			return true
		}
		raw, err := terminalproto.EncodeBinary(terminalproto.KindOutput, sessionID, sequence, payload)
		if err != nil || !peer.Enqueue(websocket.BinaryMessage, raw) {
			return false
		}
		payload = payload[:0]
		return true
	}
	for _, chunk := range chunks {
		if chunk.seq <= after {
			continue
		}
		frame, err := terminalproto.DecodeBinary(chunk.raw)
		if err != nil || frame.Kind != terminalproto.KindOutput || frame.SessionID != sessionID {
			continue
		}
		if len(payload)+len(frame.Payload) > terminalproto.MaxPayloadBytes && !flush() {
			return
		}
		payload = append(payload, frame.Payload...)
		sequence = chunk.seq
	}
	flush()
}

func (h *Hub) PublishDaemonBinary(peer *Peer, raw []byte) error {
	frame, err := terminalproto.DecodeBinary(raw)
	if err != nil || frame.Kind != terminalproto.KindOutput {
		return terminalproto.ErrInvalidFrame
	}
	h.mu.Lock()
	s, ok := h.sessions[frame.SessionID]
	if !ok || s.daemon != peer {
		h.mu.Unlock()
		return ErrInvalidPeer
	}
	if frame.Sequence <= s.meta.OutputSeq {
		h.mu.Unlock()
		return nil
	}
	if frame.Sequence != s.meta.OutputSeq+1 {
		broadcastJSON(s.browsers, terminalproto.Message{Type: "gap", ProtocolVersion: int(terminalproto.Version), SessionID: s.meta.SessionID, LastSeq: s.meta.OutputSeq, OldestSeq: frame.Sequence, OutputSeq: frame.Sequence})
	}
	s.meta.OutputSeq = frame.Sequence
	s.ring = append(s.ring, relayChunk{seq: frame.Sequence, raw: append([]byte(nil), raw...), size: len(frame.Payload)})
	s.ringBytes += len(frame.Payload)
	for s.ringBytes > h.opts.RelayRingBytes && len(s.ring) > 1 {
		s.ringBytes -= s.ring[0].size
		s.ring = s.ring[1:]
	}
	browsers := clonePeers(s.browsers)
	meta := s.meta
	h.mu.Unlock()
	for _, browser := range browsers {
		browser.Enqueue(websocket.BinaryMessage, raw)
	}
	_ = meta // output_seq is checkpointed on state/exit, never once per terminal chunk
	return nil
}

func (h *Hub) PublishDaemonMessage(peer *Peer, msg terminalproto.Message) error {
	id, err := uuid.Parse(msg.SessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	h.mu.Lock()
	s, ok := h.sessions[id]
	if !ok || s.daemon != peer {
		h.mu.Unlock()
		return ErrInvalidPeer
	}
	switch msg.Type {
	case "state":
		s.meta.Status = msg.Status
		if msg.Cols != 0 && msg.Rows != 0 {
			s.meta.Cols, s.meta.Rows = clampSize(msg.Cols, msg.Rows)
		}
		if msg.StructuredObservation != "" {
			s.meta.StructuredObservation = msg.StructuredObservation
		}
		if msg.ProviderSessionID != "" {
			s.meta.ProviderSessionID = msg.ProviderSessionID
		}
	case "exit":
		s.meta.Status = "exited"
		s.meta.ExitCode = msg.ExitCode
		s.meta.ExitReason = msg.ExitReason
		s.meta.ProviderSessionID = msg.ProviderSessionID
	case "structured_observation":
		s.meta.StructuredObservation = msg.StructuredObservation
	default:
		h.mu.Unlock()
		return fmt.Errorf("unsupported daemon terminal message %q", msg.Type)
	}
	msg.ProtocolVersion = int(terminalproto.Version)
	msg.OutputSeq = s.meta.OutputSeq
	browsers := clonePeers(s.browsers)
	meta := s.meta
	h.mu.Unlock()
	broadcastJSON(browsers, msg)
	h.changed(meta)
	return nil
}

func (h *Hub) ClaimControl(sessionID uuid.UUID, peer *Peer) (string, time.Time, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionID]
	if !ok || s.browsers[peer.ID] != peer {
		return "", time.Time{}, ErrSessionNotFound
	}
	now := h.opts.Now()
	if s.lease.token != "" && now.Before(s.lease.expiresAt) && s.lease.peerID != peer.ID {
		return "", s.lease.expiresAt, ErrLeaseConflict
	}
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(h.opts.LeaseDuration)
	s.lease = controllerLease{peerID: peer.ID, userID: peer.UserID, token: token, expiresAt: expires}
	broadcastJSON(s.browsers, terminalproto.Message{Type: "control", ProtocolVersion: int(terminalproto.Version), SessionID: sessionID.String(), Controller: false, LeaseExpiresAt: expires.UTC().Format(time.RFC3339Nano)})
	enqueueJSON(peer, terminalproto.Message{Type: "control", ProtocolVersion: int(terminalproto.Version), SessionID: sessionID.String(), Controller: true, LeaseToken: token, LeaseExpiresAt: expires.UTC().Format(time.RFC3339Nano)})
	return token, expires, nil
}

func (h *Hub) RenewControl(sessionID uuid.UUID, peer *Peer, token string) (time.Time, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionID]
	if !ok || s.lease.peerID != peer.ID || s.lease.token == "" || s.lease.token != token || !h.opts.Now().Before(s.lease.expiresAt) {
		return time.Time{}, ErrNotController
	}
	s.lease.expiresAt = h.opts.Now().Add(h.opts.LeaseDuration)
	enqueueJSON(peer, terminalproto.Message{Type: "control", ProtocolVersion: int(terminalproto.Version), SessionID: sessionID.String(), Controller: true, LeaseToken: token, LeaseExpiresAt: s.lease.expiresAt.UTC().Format(time.RFC3339Nano)})
	return s.lease.expiresAt, nil
}

func (h *Hub) ReleaseControl(sessionID uuid.UUID, peer *Peer, token string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[sessionID]
	if !ok || s.lease.peerID != peer.ID || s.lease.token != token {
		return ErrNotController
	}
	s.lease = controllerLease{}
	broadcastJSON(s.browsers, terminalproto.Message{Type: "control", ProtocolVersion: int(terminalproto.Version), SessionID: sessionID.String(), Controller: false})
	return nil
}

func (h *Hub) ForwardBrowserBinary(peer *Peer, raw []byte) error {
	frame, err := terminalproto.DecodeBinary(raw)
	if err != nil || frame.Kind != terminalproto.KindInput {
		return terminalproto.ErrInvalidFrame
	}
	h.mu.Lock()
	s, ok := h.sessions[frame.SessionID]
	if !ok || s.browsers[peer.ID] != peer {
		h.mu.Unlock()
		return ErrSessionNotFound
	}
	if s.lease.peerID != peer.ID || s.lease.token == "" || !h.opts.Now().Before(s.lease.expiresAt) {
		h.mu.Unlock()
		return ErrNotController
	}
	daemon := s.daemon
	h.mu.Unlock()
	if daemon == nil || !daemon.Enqueue(websocket.BinaryMessage, raw) {
		return errors.New("terminal daemon is disconnected")
	}
	return nil
}

func (h *Hub) ForwardBrowserMessage(peer *Peer, msg terminalproto.Message) error {
	id, err := uuid.Parse(msg.SessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	h.mu.Lock()
	s, ok := h.sessions[id]
	if !ok || s.browsers[peer.ID] != peer {
		h.mu.Unlock()
		return ErrSessionNotFound
	}
	if (msg.Type == "resize" || msg.Type == "ctrl_c") && (s.lease.peerID != peer.ID || s.lease.token == "" || !h.opts.Now().Before(s.lease.expiresAt)) {
		h.mu.Unlock()
		return ErrNotController
	}
	if msg.Type == "resize" {
		msg.Cols, msg.Rows = clampSize(msg.Cols, msg.Rows)
		s.meta.Cols, s.meta.Rows = msg.Cols, msg.Rows
	}
	daemon := s.daemon
	h.mu.Unlock()
	if daemon == nil {
		return errors.New("terminal daemon is disconnected")
	}
	return enqueueJSONError(daemon, msg)
}

func (h *Hub) changed(meta Metadata) {
	if h.onChange != nil {
		h.onChange(meta)
	}
}

func clonePeers(in map[string]*Peer) map[string]*Peer {
	out := make(map[string]*Peer, len(in))
	for id, peer := range in {
		out[id] = peer
	}
	return out
}

func broadcastJSON(peers map[string]*Peer, msg terminalproto.Message) {
	for _, peer := range peers {
		enqueueJSON(peer, msg)
	}
}

func enqueueJSON(peer *Peer, msg terminalproto.Message) {
	_ = enqueueJSONError(peer, msg)
}

func enqueueJSONError(peer *Peer, msg terminalproto.Message) error {
	raw, err := jsonMarshal(msg)
	if err != nil {
		return err
	}
	if !peer.Enqueue(websocket.TextMessage, raw) {
		return errors.New("terminal peer send queue is full")
	}
	return nil
}

var jsonMarshal = func(value any) ([]byte, error) {
	return json.Marshal(value)
}

func randomToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func clampSize(cols, rows uint16) (uint16, uint16) {
	if cols < 20 {
		cols = 20
	}
	if cols > 400 {
		cols = 400
	}
	if rows < 5 {
		rows = 5
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}
