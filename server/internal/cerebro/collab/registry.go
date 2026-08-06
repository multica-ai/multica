package collab

import "sync"

// Registry holds the live rooms, one per note that somebody currently has
// open. A room is created on the first join and destroyed when the last peer
// leaves — nothing is persisted, because the note body in the database remains
// the source of truth.
type Registry struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{rooms: make(map[string]*Room)}
}

// Acquire returns the room for a note, creating it when needed. The second
// return value reports whether the room was created by this call, which the
// handler uses to decide if the joiner starts from the database body.
func (reg *Registry) Acquire(noteID string) (*Room, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if r, ok := reg.rooms[noteID]; ok {
		return r, false
	}
	r := NewRoom(noteID)
	reg.rooms[noteID] = r
	return r, true
}

// Release drops the room for a note once it is empty.
func (reg *Registry) Release(noteID string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	delete(reg.rooms, noteID)
}

// Get returns the room for a note without creating one.
func (reg *Registry) Get(noteID string) *Room {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.rooms[noteID]
}

// Count returns how many rooms are live. Used by tests and diagnostics.
func (reg *Registry) Count() int {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.rooms)
}
