// Interim edit lock on notes (Wave 3 / G3, TECH-3556). Registered on the
// existing note.Handler.Routes under /api/notes/{id}/lock. A soft single-writer
// lock that stops two people silently overwriting each other until full live
// co-editing lands. See migration 9079_cerebro_note_lock for the model.
//
//   - GET  /lock         — read the current lock state (is it free / who holds
//     it / is the lock still live).
//   - POST /lock         — acquire or heartbeat the lock. ?force=true takes over
//     a live lock held by someone else ("Take over").
//   - DELETE /lock       — release the lock (only the holder).
//
// "Live" is decided here, not in SQL: a lock counts as held only while its
// heartbeat is within lockTimeout of now. A holder who closes the tab stops
// heartbeating and the lock goes stale — the next editor acquires it with no
// sweeper involved.
package note

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// lockTimeout is how long a lock survives without a heartbeat. The editor
// heartbeats well inside this window (~every 30s); a missed window frees the
// lock for the next editor.
const lockTimeout = 90 * time.Second

// LockResponse is the wire shape for a note's lock state. The server resolves
// `live` (heartbeat within lockTimeout) and `held_by_me` so the client doesn't
// reimplement the timeout rule.
type LockResponse struct {
	NoteID     string  `json:"note_id"`
	LockedBy   *string `json:"locked_by"`
	LockedAt   string  `json:"locked_at"`
	Heartbeat  string  `json:"lock_heartbeat_at"`
	Live       bool    `json:"live"`
	HeldByMe   bool    `json:"held_by_me"`
	TimeoutSec int     `json:"timeout_sec"`
}

func lockResponse(noteID, me pgtype.UUID, lockedBy pgtype.UUID, lockedAt, heartbeat pgtype.Timestamptz) LockResponse {
	live := lockedBy.Valid && heartbeat.Valid && time.Since(heartbeat.Time) < lockTimeout
	return LockResponse{
		NoteID:     uuidStr(noteID),
		LockedBy:   uuidPtr(lockedBy),
		LockedAt:   tsStr(lockedAt),
		Heartbeat:  tsStr(heartbeat),
		Live:       live,
		HeldByMe:   live && uuidStr(lockedBy) == uuidStr(me),
		TimeoutSec: int(lockTimeout / time.Second),
	}
}

// GetLock returns the current lock state. Any viewer who can see the note.
func (h *Handler) GetLock(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, me, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, me) {
		return
	}
	row, err := h.Cerebro.GetNoteLock(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "note not found")
		return
	}
	writeJSON(w, http.StatusOK, lockResponse(noteID, me, row.LockedBy, row.LockedAt, row.LockHeartbeatAt))
}

// AcquireLock acquires or heartbeats the lock for the caller. With ?force=true
// it takes over a live lock held by someone else. Returns the resulting lock
// state; on a failed (non-forced) acquire it returns 409 with the live state so
// the client can show "X is editing" + a Take over action.
func (h *Handler) AcquireLock(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, me, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	// Editing requires being able to see the note; the lock itself is the
	// concurrency guard, independent of note ownership (shared/workspace notes
	// are editable by the owner only today, but the lock API is owner-agnostic
	// so it keeps working if collaborative editing widens later).
	if !h.requireCanSee(w, r, noteID, me) {
		return
	}

	force := r.URL.Query().Get("force") == "true"
	if force {
		row, err := h.Cerebro.ForceAcquireNoteLock(r.Context(), cerebrodb.ForceAcquireNoteLockParams{
			ArtifactID: noteID,
			LockedBy:   me,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to take over lock")
			return
		}
		writeJSON(w, http.StatusOK, lockResponse(noteID, me, row.LockedBy, row.LockedAt, row.LockHeartbeatAt))
		return
	}

	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-lockTimeout), Valid: true}
	row, err := h.Cerebro.AcquireNoteLock(r.Context(), cerebrodb.AcquireNoteLockParams{
		ArtifactID:      noteID,
		LockedBy:        me,
		LockHeartbeatAt: cutoff,
	})
	if err == nil {
		writeJSON(w, http.StatusOK, lockResponse(noteID, me, row.LockedBy, row.LockedAt, row.LockHeartbeatAt))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to acquire lock")
		return
	}
	// Someone else holds a live lock — report it so the client can offer takeover.
	cur, err := h.Cerebro.GetNoteLock(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusConflict, "note is locked by another editor")
		return
	}
	writeJSON(w, http.StatusConflict, lockResponse(noteID, me, cur.LockedBy, cur.LockedAt, cur.LockHeartbeatAt))
}

// ReleaseLock releases the lock if the caller holds it. Idempotent — releasing
// a lock you no longer hold (taken over) is a no-op success.
func (h *Handler) ReleaseLock(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, me, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, me) {
		return
	}
	if err := h.Cerebro.ReleaseNoteLock(r.Context(), cerebrodb.ReleaseNoteLockParams{
		ArtifactID: noteID,
		LockedBy:   me,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to release lock")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
