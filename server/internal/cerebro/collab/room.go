// Package collab is the live co-editing engine for notes (FIR-1317).
//
// It is the "B" track: several people type in the same note at once and see
// each other's caret, replacing the single-writer lock + conflict dialog that
// shipped as track "A" (FIR-3601 point 1).
//
// Design note — why there is no CRDT and no separate sync service:
// the server is a dumb *authority* in the classic prosemirror-collab sense. It
// never transforms a step and never renders the document; it only
//
//  1. hands out a monotonic version number,
//  2. keeps the ordered step log since the last snapshot, and
//  3. relays steps, carets and presence to the other peers in the room.
//
// Rebasing is the client's job, exactly as prosemirror-collab expects. That
// keeps the whole engine inside the existing Go server and the existing
// WebSocket stack — no Yjs, no Hocuspocus container, no extra hosting bill.
//
// Persistence is deliberately unchanged: the note body in the database stays
// the single source of truth and is still written through the normal
// UpdateNote path, so version history, Line history and search keep working.
// A room is in-memory only and disappears when the last peer leaves.
package collab

import (
	"encoding/json"
	"errors"
	"sync"
)

// maxBufferedSteps caps the per-room step log. When the log grows past this,
// the room asks a peer for a fresh snapshot and drops everything below it, so
// a long editing session cannot grow without bound.
const maxBufferedSteps = 600

// ErrSnapshotTooOld is returned when a client asks for the steps since a
// version that predates the room's snapshot base. The client must reload the
// note from the snapshot instead of replaying steps.
var ErrSnapshotTooOld = errors.New("collab: requested version is older than the room snapshot")

// StepRecord is one submitted step plus the identity of the client that
// produced it. Version is the room version *after* the step was applied, so a
// client that holds version V wants every record with Version > V.
type StepRecord struct {
	Version  int             `json:"version"`
	ClientID string          `json:"client_id"`
	Step     json.RawMessage `json:"step"`
}

// Snapshot is an opaque ProseMirror document as JSON, together with the room
// version it represents. The server never inspects Doc — it only stores and
// forwards it, which is what keeps this package free of editor-schema
// knowledge.
type Snapshot struct {
	Version int             `json:"version"`
	Doc     json.RawMessage `json:"doc"`
}

// Room is the live editing session for one note.
//
// The zero value is not usable; construct with NewRoom.
type Room struct {
	NoteID string

	mu       sync.Mutex
	version  int
	base     int // version the current snapshot represents
	steps    []StepRecord
	snapshot *Snapshot
	peers    map[string]*Peer
}

// NewRoom creates an empty room for a note. A fresh room starts at version 0,
// meaning "the note body as it is stored in the database"; the first peer to
// join supplies the snapshot for anyone joining after it.
func NewRoom(noteID string) *Room {
	return &Room{NoteID: noteID, peers: make(map[string]*Peer)}
}

// Version returns the room's current version.
func (r *Room) Version() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

// Submit applies a batch of steps from one client.
//
// It accepts the batch only when the client's version matches the room's — the
// prosemirror-collab contract. On a mismatch it returns accepted=false plus the
// steps the client is missing, so the client can rebase and resubmit. Callers
// must broadcast accepted steps to every peer, including the sender.
func (r *Room) Submit(clientID string, version int, steps []json.RawMessage) (accepted bool, missing []StepRecord, current int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if version != r.version {
		since, serr := r.stepsSinceLocked(version)
		return false, since, r.version, serr
	}
	for _, s := range steps {
		r.version++
		r.steps = append(r.steps, StepRecord{Version: r.version, ClientID: clientID, Step: s})
	}
	return true, nil, r.version, nil
}

// StepsSince returns every step a client on `version` has not seen yet.
func (r *Room) StepsSince(version int) ([]StepRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stepsSinceLocked(version)
}

func (r *Room) stepsSinceLocked(version int) ([]StepRecord, error) {
	if version < r.base {
		return nil, ErrSnapshotTooOld
	}
	out := make([]StepRecord, 0, len(r.steps))
	for _, s := range r.steps {
		if s.Version > version {
			out = append(out, s)
		}
	}
	return out, nil
}

// SetSnapshot stores a document snapshot supplied by a peer and drops every
// step it already contains. A stale snapshot (older than one we already hold,
// or ahead of the room version — which would mean the peer invented history) is
// ignored, so a slow peer answering late cannot rewind the room.
func (r *Room) SetSnapshot(s Snapshot) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s.Version > r.version {
		return false
	}
	if r.snapshot != nil && s.Version <= r.base {
		return false
	}
	r.snapshot = &Snapshot{Version: s.Version, Doc: s.Doc}
	r.base = s.Version
	kept := r.steps[:0]
	for _, st := range r.steps {
		if st.Version > s.Version {
			kept = append(kept, st)
		}
	}
	r.steps = kept
	return true
}

// Snapshot returns the stored snapshot, or nil when no peer has supplied one
// yet (a brand-new room, where a joiner loads the note from the database).
func (r *Room) Snapshot() *Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot == nil {
		return nil
	}
	cp := *r.snapshot
	return &cp
}

// NeedsSnapshot reports whether the step log has grown past the cap, meaning
// the room should ask a peer for a fresh snapshot to compact itself.
func (r *Room) NeedsSnapshot() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.steps) > maxBufferedSteps
}

// --- presence ---

// Peer is one connected editor in a room.
type Peer struct {
	// ID is unique per connection, so the same person in two tabs shows up as
	// two carets — which is what they see in Google Docs too.
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	CanEdit  bool   `json:"can_edit"`
	outbound chan []byte
}

// Send queues a message for this peer. It never blocks: a peer whose buffer is
// full is dropped by the caller rather than allowed to stall the room.
func (p *Peer) Send(msg []byte) bool {
	select {
	case p.outbound <- msg:
		return true
	default:
		return false
	}
}

// Outbound exposes the peer's queue to the connection writer.
func (p *Peer) Outbound() <-chan []byte { return p.outbound }

// NewPeer builds a peer with a bounded outbound queue.
func NewPeer(id, userID, name string, canEdit bool) *Peer {
	return &Peer{
		ID:       id,
		UserID:   userID,
		Name:     name,
		Color:    ColorFor(userID),
		CanEdit:  canEdit,
		outbound: make(chan []byte, 64),
	}
}

// Join adds a peer and returns the full peer list after the join.
func (r *Room) Join(p *Peer) []*Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[p.ID] = p
	return r.peerListLocked()
}

// Leave removes a peer and reports whether the room is now empty.
func (r *Room) Leave(peerID string) (remaining []*Peer, empty bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, peerID)
	list := r.peerListLocked()
	return list, len(list) == 0
}

// Peers returns the current peer list.
func (r *Room) Peers() []*Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peerListLocked()
}

func (r *Room) peerListLocked() []*Peer {
	out := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p)
	}
	return out
}

// Broadcast queues a message for every peer except exclude (pass "" to include
// everyone). Peers whose queue is full are returned so the caller can close
// their connection instead of letting them wedge the room.
func (r *Room) Broadcast(msg []byte, exclude string) (stalled []string) {
	for _, p := range r.Peers() {
		if p.ID == exclude {
			continue
		}
		if !p.Send(msg) {
			stalled = append(stalled, p.ID)
		}
	}
	return stalled
}

// OldestPeer returns an arbitrary peer that can supply a snapshot, preferring
// one other than exclude. It returns nil when nobody else is in the room.
func (r *Room) OldestPeer(exclude string) *Peer {
	for _, p := range r.Peers() {
		if p.ID != exclude {
			return p
		}
	}
	return nil
}

// ColorFor maps a user to a stable caret colour, so the same person keeps the
// same colour across sessions and across everyone else's screen.
func ColorFor(userID string) string {
	palette := []string{
		"#2563eb", // blue
		"#16a34a", // green
		"#db2777", // pink
		"#ea580c", // orange
		"#7c3aed", // violet
		"#0891b2", // cyan
		"#ca8a04", // amber
		"#dc2626", // red
	}
	var sum int
	for i := 0; i < len(userID); i++ {
		sum = sum*31 + int(userID[i])
	}
	if sum < 0 {
		sum = -sum
	}
	return palette[sum%len(palette)]
}
