package kagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrBusy           = errors.New("kagent: another operation holds this journal record")
	ErrDeleted        = errors.New("kagent: enrollment is deleted")
	ErrUnknownOutcome = errors.New("kagent: remote outcome requires reconciliation")
)

// Journal is a local DEV durability boundary, not a distributed dispatcher DB.
// All processes using an enrollment must share this directory and filesystem
// locks. Records contain config, correlation and bounded intentional completed
// output for replay, never task prompts or remote status diagnostics.
// Use a persistent private directory; losing it loses ownership and replay fences.
type Journal struct{ dir string }

type LockedRecord struct {
	file *os.File
	path string
}

func NewJournal(dir string) (*Journal, error) {
	if dir == "" {
		return nil, fmt.Errorf("kagent: persistent journal directory is required")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("kagent: journal directory must be private (0700)")
	}
	return &Journal{dir: absolute}, nil
}

// Lock rejects concurrent work, including a second process, rather than
// queueing a stale desired-state snapshot or overlapping conversation turn.
func (j *Journal) Lock(key string) (*LockedRecord, error) {
	hash := sha256.Sum256([]byte(key))
	path := filepath.Join(j.dir, hex.EncodeToString(hash[:]))
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return &LockedRecord{file: file, path: path + ".json"}, nil
}
func (r *LockedRecord) Close() error { return r.file.Close() }
func (r *LockedRecord) Load(out any) (bool, error) {
	file, err := os.Open(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return false, err
	}
	if len(raw) > 1<<20 {
		return false, fmt.Errorf("kagent: oversized journal record")
	}
	if uniqueJSON(raw) != nil {
		return false, fmt.Errorf("kagent: corrupt journal record")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return false, fmt.Errorf("kagent: corrupt journal record")
	}
	return true, nil
}
func (r *LockedRecord) Save(in any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("kagent: oversized journal record")
	}
	file, err := os.CreateTemp(filepath.Dir(r.path), ".journal-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), r.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(r.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// records enumerates every persisted enrollment record in the journal
// directory. This is the authoritative enrollment source for recovery:
// cluster labels and annotations are never consulted for it. Read-only and
// lockless: Save publishes complete records via atomic rename, so each load
// observes either the previous or the new complete record even while another
// process holds the record lock or is mid-save. Unreadable, corrupt,
// unknown-version or identity-less records are skipped — they contribute no
// ownership claims.
func (j *Journal) records() ([]EnrollmentRecord, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	var out []EnrollmentRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue // lock files and in-flight temp files are not records
		}
		rec := &LockedRecord{path: filepath.Join(j.dir, entry.Name())}
		var state enrollment
		exists, err := rec.Load(&state)
		if err != nil || !exists || validateEnrollment(state, state.Desired.AgentID) != nil || recordFile("agent/"+state.Desired.AgentID) != entry.Name() {
			continue
		}
		out = append(out, EnrollmentRecord{
			AgentID:     state.Desired.AgentID,
			Namespace:   state.Desired.Namespace,
			OwnerToken:  state.Desired.OwnerToken,
			ExpectedUID: state.Desired.ExpectedUID,
			Deleted:     state.Deleted,
		})
	}
	return out, nil
}

// CheckConversationFences prevents an existing conversation journal from being
// bypassed when upgrading to the agent-wide fence. Caller holds the enrollment
// lock. Only the exact known attempt being recovered may be adopted; no send is
// authorized by this exception. Corrupt/unreadable records fail closed.
func (j *Journal) CheckConversationFences(uid, owner, recoveryKey, recoveryAttempt string) error {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return err
	}
	recoveryHash := sha256.Sum256([]byte(recoveryKey))
	recoveryFile := hex.EncodeToString(recoveryHash[:]) + ".json"
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.IsDir() {
			return ErrUnknownOutcome
		}
		rec := &LockedRecord{path: filepath.Join(j.dir, entry.Name())}
		var shape map[string]json.RawMessage
		exists, err := rec.Load(&shape)
		if err != nil || !exists {
			return ErrUnknownOutcome
		}
		// Enrollment files share this directory, but must also be semantically
		// valid and stored under their own identity.
		if _, ok := shape["Desired"]; ok {
			var e enrollment
			if _, err := rec.Load(&e); err != nil || validateEnrollment(e, e.Desired.AgentID) != nil || recordFile("agent/"+e.Desired.AgentID) != entry.Name() {
				return ErrUnknownOutcome
			}
			continue
		}
		var state struct {
			Version                                                              int
			AgentUID, OwnerToken, Endpoint, SessionID, ActiveAttempt, Generation string
			Attempts                                                             map[string]struct {
				MessageID, InputHash, TaskID, Generation, TerminalState string
				Settled                                                 bool
			}
		}
		if _, err := rec.Load(&state); err != nil || state.Version != 2 || state.AgentUID == "" || !agentIDPattern.MatchString(state.OwnerToken) || state.Endpoint == "" || state.Attempts == nil {
			return ErrUnknownOutcome
		}
		// A matching UID with a different owner is a conflict, never unrelated.
		if state.AgentUID == uid && state.OwnerToken != owner {
			return ErrUnknownOutcome
		}
		unsettled := 0
		for id, a := range state.Attempts {
			if id == "" || !agentIDPattern.MatchString(a.MessageID) || !validHash(a.InputHash) || !agentIDPattern.MatchString(a.Generation) {
				return ErrUnknownOutcome
			}
			if a.Settled {
				if a.TaskID == "" || !terminalState(a.TerminalState) {
					return ErrUnknownOutcome
				}
			} else {
				unsettled++
				if id != state.ActiveAttempt || a.Generation != state.Generation || a.TerminalState != "" {
					return ErrUnknownOutcome
				}
			}
		}
		if state.ActiveAttempt == "" {
			if unsettled != 0 {
				return ErrUnknownOutcome
			}
			continue
		}
		a, ok := state.Attempts[state.ActiveAttempt]
		if !ok || a.Settled || unsettled != 1 {
			return ErrUnknownOutcome
		}
		if state.AgentUID != uid {
			continue
		}
		if entry.Name() == recoveryFile && state.ActiveAttempt == recoveryAttempt && a.TaskID != "" && agentIDPattern.MatchString(state.SessionID) {
			continue
		}
		return ErrUnknownOutcome
	}
	return nil
}

func recordFile(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:]) + ".json"
}

func terminalState(state string) bool {
	t := RemoteTask{}
	t.Status.State = state
	return t.Terminal()
}

func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
