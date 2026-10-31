package claudemanagedruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// One journal belongs to exactly one bridge configuration and holds every
// active run, bounded by the connection's concurrency limit. Credentials are
// never persisted. Keep this directory on persistent storage.
type journal struct {
	Binding   string   `json:"binding"`
	RuntimeID string   `json:"runtime_id"`
	Cleanup   []string `json:"cleanup,omitempty"`
	Runs      []*run   `json:"runs,omitempty"`
}
type run struct {
	// PriorSessionID is the session of this agent's previous run on the issue.
	// An idle prior session is continued instead of creating a new one.
	PriorSessionID     string           `json:"prior_session_id,omitempty"`
	Continued          bool             `json:"continued,omitempty"`
	Deadline           time.Time        `json:"deadline"`
	SubmissionDeadline time.Time        `json:"submission_deadline,omitempty"`
	RetryAt            time.Time        `json:"retry_at,omitempty"`
	AgentID            string           `json:"agent_id,omitempty"`
	Page               string           `json:"page,omitempty"`
	TaskID             string           `json:"task_id"`
	SessionID          string           `json:"session_id,omitempty"`
	Prompt             string           `json:"prompt,omitempty"`
	Resources          []map[string]any `json:"resources,omitempty"`
	Phase              string           `json:"phase"`
	Cursor             string           `json:"cursor,omitempty"`
	Seq                int              `json:"seq"`
	Started            bool             `json:"started"`
	Output             string           `json:"output,omitempty"`
	Failure            string           `json:"failure,omitempty"`
	Outcome            string           `json:"outcome,omitempty"`

	// CursorAt is the processed_at of the Cursor event; empty when it had none.
	CursorAt string `json:"cursor_at,omitempty"`
	// PageFrom is the created_at[gte] filter of the request that returned
	// Page; the page request repeats it.
	PageFrom string `json:"page_from,omitempty"`

	// CreateAttemptAt is when the first session create for this run was sent.
	// After an ambiguous create, sessions listed from then on are matched by
	// task ID before any retry. CreateRetried allows exactly one retry.
	CreateAttemptAt time.Time `json:"create_attempt_at,omitempty"`
	CreateRetried   bool      `json:"create_retried,omitempty"`
}

func save(path string, j *journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".claude-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncStateDir(filepath.Dir(path))
}
func readJournal(path, binding string) (*journal, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &journal{Binding: binding}, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeJournal(b, binding)
}
func decodeJournal(b []byte, binding string) (*journal, error) {
	if len(b) == 0 {
		return &journal{Binding: binding}, nil
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("invalid bridge journal: %w", err)
	}
	if j.Binding != binding {
		return nil, fmt.Errorf("state directory belongs to a different bridge configuration")
	}
	if j.Binding == "" {
		j.Binding = binding
	}
	seen := map[string]bool{}
	for _, r := range j.Runs {
		if r == nil || r.TaskID == "" {
			return nil, fmt.Errorf("bridge journal has no task ID")
		}
		if seen[r.TaskID] {
			return nil, fmt.Errorf("bridge journal has a duplicate task")
		}
		seen[r.TaskID] = true
		switch r.Phase {
		case "prepared", "creating", "final":
		case "created", "sending", "running":
			if r.SessionID == "" {
				return nil, fmt.Errorf("bridge journal has no session ID")
			}
		default:
			return nil, fmt.Errorf("unknown bridge journal phase")
		}
	}
	return &j, nil
}

func (j *journal) remove(r *run) {
	for i, existing := range j.Runs {
		if existing == r {
			j.Runs = append(j.Runs[:i], j.Runs[i+1:]...)
			return
		}
	}
}

type persistenceError struct{ err error }

func (e *persistenceError) Error() string { return "persist bridge journal: " + e.err.Error() }
func (b *Bridge) persist() error {
	if b.cfg.SaveState != nil {
		raw, err := json.Marshal(b.state)
		if err != nil {
			return &persistenceError{err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.cfg.SaveState(ctx, raw); err != nil {
			return &persistenceError{err}
		}
		return nil
	}
	if err := save(b.path, b.state); err != nil {
		return &persistenceError{err}
	}
	return nil
}
