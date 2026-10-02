package qoderruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// One journal belongs to exactly one bridge configuration and one active run.
// Credentials are never persisted. Keep this directory on persistent storage.
type journal struct {
	Binding   string `json:"binding"`
	RuntimeID string `json:"runtime_id"`
	Run       *run   `json:"run,omitempty"`
}
type run struct {
	AgentID   string           `json:"agent_id,omitempty"`
	Page      string           `json:"page,omitempty"`
	TaskID    string           `json:"task_id"`
	SessionID string           `json:"session_id,omitempty"`
	Prompt    string           `json:"prompt,omitempty"`
	Resources []map[string]any `json:"resources,omitempty"`
	Phase     string           `json:"phase"`
	Cursor    string           `json:"cursor,omitempty"`
	Seq       int              `json:"seq"`
	Started   bool             `json:"started"`
	Output    string           `json:"output,omitempty"`
	Failure   string           `json:"failure,omitempty"`
	Outcome   string           `json:"outcome,omitempty"`
}

func save(path string, j *journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".qoder-state-*")
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
	var j journal
	if err = json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("invalid bridge journal: %w", err)
	}
	if j.Binding != binding {
		return nil, fmt.Errorf("state directory belongs to a different bridge configuration")
	}
	if j.Run != nil {
		if j.Run.TaskID == "" {
			return nil, fmt.Errorf("bridge journal has no task ID")
		}
		switch j.Run.Phase {
		case "prepared", "final":
		case "created", "sending", "running":
			if j.Run.SessionID == "" {
				return nil, fmt.Errorf("bridge journal has no session ID")
			}
		default:
			return nil, fmt.Errorf("unknown bridge journal phase")
		}
	}
	return &j, nil
}

type persistenceError struct{ err error }

func (e *persistenceError) Error() string { return "persist bridge journal: " + e.err.Error() }
func (b *Bridge) persist() error {
	if err := save(b.path, b.state); err != nil {
		return &persistenceError{err}
	}
	return nil
}
