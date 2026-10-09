package lark

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The device-flow bind session is transient coordination state, not
// business data: it lives only between "user clicked Bind" and "the Bot
// row committed", and nothing reads it afterwards. Per the architecture
// doc, that shape belongs in Redis with an in-process implementation for
// single-instance development — the same split the runtime update /
// model-list / local-skill request stores already use.
//
// It has to be SHARED, not per-process, because the browser polls
// GET /lark/install/{session_id}/status every ~5s and any replica can
// receive that poll. When the state lived in one process's map, a poll
// routed elsewhere 404'd and the dialog reported "session lost" about 5s
// after the QR rendered (MUL-7340).
//
// What is NOT stored here: the device_code. It is a bearer credential —
// anyone holding it can complete the authorization — and only the process
// running the polling goroutine needs it, so it stays in that process's
// memory. The consequence is deliberate and worth stating plainly: a
// session does NOT survive the death of the process that started it. Other
// replicas keep serving `pending` until ExpiresAt, at which point the read
// path reports it expired. Sharing the status fixes the 404; it does not
// make an in-flight scan resumable elsewhere.

// InstallSessionState is the status projection the install dialog reads.
type InstallSessionState struct {
	ID          string
	WorkspaceID pgtype.UUID
	// InitiatorID authorizes the status read (session initiator, or a
	// workspace owner/admin). Never serialized to the client.
	InitiatorID pgtype.UUID

	Status         RegistrationSessionStatus
	InstallationID pgtype.UUID
	ErrorReason    string
	ErrorMessage   string

	// ExpiresAt is Lark's device_code deadline. A session still pending
	// past it is reported as expired by the read path, so a session whose
	// owning process died does not hang on `pending` forever.
	ExpiresAt time.Time
}

// ErrInstallSessionNotFound is returned for an unknown, expired-out, or
// wrong-workspace session id. Deliberately one error for all three: a
// distinguishable "exists but not yours" would let a caller enumerate
// session ids across workspaces.
var ErrInstallSessionNotFound = errors.New("lark: install session not found")

// InstallSessionStore is the shared home for in-flight bind sessions.
//
// MarkTerminal is first-writer-wins by contract: the expiry deadline and a
// poll result can fire concurrently, and the user must keep the outcome
// they were already shown. A losing write is a no-op, NOT an error.
type InstallSessionStore interface {
	// Create registers a new pending session. ttl bounds how long the
	// record is retained; callers size it to cover the QR window plus the
	// terminal-read window that follows it.
	Create(ctx context.Context, state InstallSessionState, ttl time.Duration) error

	// Get returns the session scoped to workspaceID, or
	// ErrInstallSessionNotFound.
	Get(ctx context.Context, workspaceID pgtype.UUID, id string) (InstallSessionState, error)

	// MarkTerminal records the first terminal outcome for id and ignores
	// any later one. It must also be self-repairing: a call that loses the
	// race still moves retention onto the terminal window, so a retry
	// after a partially-applied write leaves the record consistent.
	//
	// Returns ErrInstallSessionNotFound when the session is gone — the
	// signal to a retrying caller that there is nothing left to fix.
	MarkTerminal(ctx context.Context, id string, outcome InstallSessionOutcome, ttl time.Duration) error
}

// InstallSessionOutcome is the terminal half of a session — the only part
// that ever changes after Create.
type InstallSessionOutcome struct {
	Status         RegistrationSessionStatus
	InstallationID pgtype.UUID
	ErrorReason    string
	ErrorMessage   string
}

// MemoryInstallSessionStore is the single-process implementation used when
// the deployment has no Redis. Correct for local development and for a
// genuinely single-replica install; on a multi-replica deployment WITHOUT
// Redis it reproduces MUL-7340, which is why the router logs a warning
// rather than choosing this silently.
type MemoryInstallSessionStore struct {
	mu      sync.Mutex
	entries map[string]memoryInstallSession
	// now is overridable so retention is testable without sleeping.
	now func() time.Time
}

type memoryInstallSession struct {
	state     InstallSessionState
	retainTil time.Time
}

func NewMemoryInstallSessionStore() *MemoryInstallSessionStore {
	return &MemoryInstallSessionStore{
		entries: make(map[string]memoryInstallSession),
		now:     time.Now,
	}
}

func (s *MemoryInstallSessionStore) Create(_ context.Context, state InstallSessionState, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.entries[state.ID] = memoryInstallSession{state: state, retainTil: s.now().Add(ttl)}
	return nil
}

func (s *MemoryInstallSessionStore) Get(_ context.Context, workspaceID pgtype.UUID, id string) (InstallSessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	// Mirror the Redis TTL: a record past its retention is gone, not stale.
	if !ok || !entry.retainTil.After(s.now()) {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}
	if entry.state.WorkspaceID != workspaceID {
		return InstallSessionState{}, ErrInstallSessionNotFound
	}
	return entry.state, nil
}

func (s *MemoryInstallSessionStore) MarkTerminal(_ context.Context, id string, outcome InstallSessionOutcome, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok || !entry.retainTil.After(s.now()) {
		return ErrInstallSessionNotFound
	}
	// First writer wins — a second terminal write leaves the recorded
	// outcome alone. Retention still moves, matching the unconditional
	// EXPIRE in the Redis script, so the two implementations agree on what
	// a retry does.
	if entry.state.Status == RegistrationStatusPending {
		entry.state.Status = outcome.Status
		entry.state.InstallationID = outcome.InstallationID
		entry.state.ErrorReason = outcome.ErrorReason
		entry.state.ErrorMessage = outcome.ErrorMessage
	}
	entry.retainTil = s.now().Add(ttl)
	s.entries[id] = entry
	return nil
}

// pruneLocked drops records past their retention. Called on Create — the
// rare path — rather than on Get, which fires every ~5s per open dialog.
func (s *MemoryInstallSessionStore) pruneLocked() {
	now := s.now()
	for id, entry := range s.entries {
		if !entry.retainTil.After(now) {
			delete(s.entries, id)
		}
	}
}
