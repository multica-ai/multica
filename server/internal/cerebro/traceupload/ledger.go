package traceupload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Status is the lifecycle state of a ledger entry.
type Status string

const (
	// StatusPending: recorded, not yet confirmed ingested. The boot-sweep and
	// the retry loop both target pending entries. Transient upload failures
	// keep the entry pending (with Attempts bumped) so it is retried.
	StatusPending Status = "pending"
	// StatusIngested: registry returned 200 (ingested or idempotent no-op).
	// Terminal — never sent again.
	StatusIngested Status = "ingested"
	// StatusDeadLetter: exceeded MaxAge without success. Terminal — parked and
	// alarmed, never sent again automatically.
	StatusDeadLetter Status = "dead_letter"
)

// Entry is one tracked session JSONL. It carries the Sidecar so the boot-sweep
// can re-send a file long after the task that produced it has gone — a raw
// transcript on disk has no Multica task/issue identity of its own.
type Entry struct {
	Key         string    `json:"key"` // sidecar.Key(): task_id|session_id
	Sidecar     Sidecar   `json:"sidecar"`
	FilePath    string    `json:"file_path"`
	Status      Status    `json:"status"`
	Attempts    int       `json:"attempts"`
	FirstSeen   time.Time `json:"first_seen"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	// ContentSHA256 + ContentSize identify the file contents at record time
	// (the issue's "hash/offset"). They let a future run detect that a file
	// grew or was replaced since it was last seen.
	ContentSHA256 string `json:"content_sha256,omitempty"`
	ContentSize   int64  `json:"content_size,omitempty"`
}

// SessionState tracks per-session_id upload progress so a session that grows
// across multiple tasks (Claude `--resume` appends to the same
// `<sessionID>.jsonl`) only sends each event once. Keyed by session_id (NOT by
// the entry's `task_id|session_id` key) because the same session_id can have
// multiple entries under different task_ids.
//
// LastUploadedSize is how many bytes of this session's file were already
// confirmed ingested. LastUploadedSHA256 is the sha256 of bytes
// [0:LastUploadedSize] — used to detect that the file was truncated/rewritten
// rather than appended, in which case we fall back to a full upload.
type SessionState struct {
	SessionID          string    `json:"session_id"`
	LastUploadedSize   int64     `json:"last_uploaded_size"`
	LastUploadedSHA256 string    `json:"last_uploaded_sha256"`
	LastUploadedAt     time.Time `json:"last_uploaded_at"`
}

// Ledger is a crash-safe, JSON-backed record of every session JSONL the daemon
// has tried (or still needs) to upload. It is the single source of truth for
// the boot-sweep: anything not StatusIngested / StatusDeadLetter is owed to the
// registry. It also tracks per-session upload progress so Claude `--resume` (a
// growing transcript across multiple tasks) only ships each event once.
type Ledger struct {
	path string

	mu       sync.Mutex
	entries  map[string]*Entry
	sessions map[string]*SessionState
}

// ledgerFile is the on-disk JSON shape. Older ledger files were a bare entry
// array — LoadLedger accepts both shapes for forward compatibility.
type ledgerFile struct {
	Entries  []*Entry        `json:"entries"`
	Sessions []*SessionState `json:"sessions,omitempty"`
}

// LoadLedger opens (or creates) the ledger file at path. A missing file is a
// fresh ledger; a corrupt file is reported as an error so the caller can decide
// (the daemon logs and starts empty rather than crashing).
func LoadLedger(path string) (*Ledger, error) {
	l := &Ledger{
		path:     path,
		entries:  make(map[string]*Entry),
		sessions: make(map[string]*SessionState),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	if len(data) == 0 {
		return l, nil
	}
	// Accept both the new wrapper object {entries, sessions} and the legacy
	// bare entry array, so an upgrade does not strand old ledger files.
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var entries []*Entry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, fmt.Errorf("parse ledger: %w", err)
		}
		for _, e := range entries {
			if e != nil && e.Key != "" {
				l.entries[e.Key] = e
			}
		}
		return l, nil
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	for _, e := range f.Entries {
		if e != nil && e.Key != "" {
			l.entries[e.Key] = e
		}
	}
	for _, s := range f.Sessions {
		if s != nil && s.SessionID != "" {
			l.sessions[s.SessionID] = s
		}
	}
	return l, nil
}

// Upsert records (or refreshes) a pending entry for a freshly-closed task. If
// the entry already exists and is terminal (ingested / dead_letter) it is left
// untouched and reused == false is returned — we never resurrect a settled row
// from a duplicate task-close. Returns the live entry.
func (l *Ledger) Upsert(s Sidecar, filePath, sha string, size int64, now time.Time) (*Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := s.Key()
	if existing, ok := l.entries[key]; ok {
		if existing.Status == StatusIngested || existing.Status == StatusDeadLetter {
			return existing, nil
		}
		// Refresh the path/hash in case the transcript grew between the
		// in-run record and task close; keep the existing attempt history.
		existing.FilePath = filePath
		existing.ContentSHA256 = sha
		existing.ContentSize = size
		if err := l.flushLocked(); err != nil {
			return nil, err
		}
		return existing, nil
	}

	e := &Entry{
		Key:           key,
		Sidecar:       s,
		FilePath:      filePath,
		Status:        StatusPending,
		FirstSeen:     now,
		ContentSHA256: sha,
		ContentSize:   size,
	}
	l.entries[key] = e
	if err := l.flushLocked(); err != nil {
		return nil, err
	}
	return e, nil
}

// MarkIngested settles an entry as successfully delivered.
func (l *Ledger) MarkIngested(key string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return nil
	}
	e.Status = StatusIngested
	e.LastAttempt = now
	e.LastError = ""
	return l.flushLocked()
}

// MarkFailed bumps the attempt count and records the error, keeping the entry
// pending for retry.
func (l *Ledger) MarkFailed(key string, now time.Time, errMsg string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return nil
	}
	e.Attempts++
	e.LastAttempt = now
	e.LastError = errMsg
	return l.flushLocked()
}

// MarkDeadLetter parks an entry that has outlived MaxAge.
func (l *Ledger) MarkDeadLetter(key string, now time.Time, errMsg string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return nil
	}
	e.Status = StatusDeadLetter
	e.LastAttempt = now
	if errMsg != "" {
		e.LastError = errMsg
	}
	return l.flushLocked()
}

// Pending returns a snapshot of every entry still owed to the registry
// (StatusPending), sorted oldest-first so the boot-sweep drains the backlog in
// arrival order.
func (l *Ledger) Pending() []*Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []*Entry
	for _, e := range l.entries {
		if e.Status == StatusPending {
			cp := *e
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FirstSeen.Before(out[j].FirstSeen) })
	return out
}

// Get returns a copy of the entry for key, or nil.
func (l *Ledger) Get(key string) *Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e, ok := l.entries[key]; ok {
		cp := *e
		return &cp
	}
	return nil
}

// SessionState returns a copy of the upload-progress record for sessionID, or
// nil if this session has never been uploaded. Used to compute the delta to
// send when a resumed session is uploaded again under a new task_id.
func (l *Ledger) SessionState(sessionID string) *SessionState {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s, ok := l.sessions[sessionID]; ok {
		cp := *s
		return &cp
	}
	return nil
}

// MarkSessionUploaded records that bytes [0:size] of sessionID's transcript
// (with sha256 = sha) have been confirmed ingested. A later upload of the same
// session reads this to decide what suffix is new and what is a re-send.
func (l *Ledger) MarkSessionUploaded(sessionID string, size int64, sha string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if sessionID == "" {
		return nil
	}
	s, ok := l.sessions[sessionID]
	if !ok {
		s = &SessionState{SessionID: sessionID}
		l.sessions[sessionID] = s
	}
	// Only advance the watermark — never regress it. If a later upload arrives
	// with a SMALLER size (e.g. a new codex rollout file under a reused
	// session_id), leave the prior record alone; the caller already chose to
	// send the full new file when its prefix did not match.
	if size > s.LastUploadedSize {
		s.LastUploadedSize = size
		s.LastUploadedSHA256 = sha
	}
	s.LastUploadedAt = now
	return l.flushLocked()
}

// flushLocked atomically persists the whole ledger. Caller holds l.mu.
func (l *Ledger) flushLocked() error {
	if l.path == "" {
		return nil // in-memory only (tests)
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("ledger mkdir: %w", err)
	}
	entries := make([]*Entry, 0, len(l.entries))
	for _, e := range l.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FirstSeen.Before(entries[j].FirstSeen) })
	sessions := make([]*SessionState, 0, len(l.sessions))
	for _, s := range l.sessions {
		sessions = append(sessions, s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].SessionID < sessions[j].SessionID })
	data, err := json.MarshalIndent(ledgerFile{Entries: entries, Sessions: sessions}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write ledger tmp: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("rename ledger: %w", err)
	}
	return nil
}
