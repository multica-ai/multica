package kagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Lifecycle serializes authoritative enrollment changes, reconcile and deletion
// across local DEV processes. Queue agent IDs, never Desired snapshots. A
// tombstone is durably written before cleanup and is retained after deletion.
// Server DB enrollment/disable/drain wiring is still required for deployment.
type Lifecycle struct {
	reconciler *Reconciler
	journal    *Journal
}
type enrollment struct {
	Version int
	Desired Desired
	Deleted bool
	// DeletionComplete is set only after GET reports absence. Deleted alone
	// (including older journal records) always requires cleanup recovery.
	DeletionComplete bool             `json:",omitempty"`
	Execution        *ExecutionIntent `json:",omitempty"`
}

// ErrDeletionPending means the tombstone is durable but cleanup has not been
// confirmed by observing absence. Retry Delete or Recover; errors.Is also
// exposes any underlying ownership, transport, or concurrency failure.
var ErrDeletionPending = errors.New("kagent: deletion pending observed absence")

type Binding struct {
	Desired Desired
	Outcome Outcome
}

func NewLifecycle(r *Reconciler, j *Journal) (*Lifecycle, error) {
	if r == nil || j == nil {
		return nil, fmt.Errorf("kagent: reconciler and journal required")
	}
	return &Lifecycle{r, j}, nil
}
func (l *Lifecycle) record(agentID string) (*LockedRecord, enrollment, error) {
	if _, err := ResourceName(agentID); err != nil {
		return nil, enrollment{}, err
	}
	rec, err := l.journal.Lock("agent/" + strings.ToLower(agentID))
	if err != nil {
		return nil, enrollment{}, err
	}
	var state enrollment
	exists, err := rec.Load(&state)
	if err == nil && exists {
		err = validateEnrollment(state, strings.ToLower(agentID))
		if err == nil && !l.reconciler.namespaceAllowed(state.Desired.Namespace) {
			err = ErrNamespaceNotAllowed
		}
	}
	if err != nil {
		rec.Close()
		return nil, enrollment{}, err
	}
	return rec, state, nil
}

// Identity validation is shared by locked loads and lockless recovery scans.
func validateEnrollment(state enrollment, agentID string) error {
	if state.Version != 1 || state.Desired.AgentID != agentID || !agentIDPattern.MatchString(agentID) || !namespacePattern.MatchString(state.Desired.Namespace) || (state.DeletionComplete && !state.Deleted) {
		return fmt.Errorf("kagent: invalid enrollment identity or state")
	}
	return ValidateDesired(state.Desired)
}

// Enroll explicitly creates or updates desired state. Ownership is generated
// here and retained across edits. A deleted ID cannot be silently re-enrolled.
func (l *Lifecycle) Enroll(d Desired) error {
	rec, state, err := l.record(d.AgentID)
	if err != nil {
		return err
	}
	defer rec.Close()
	if state.Deleted {
		return ErrDeleted
	}
	if state.Version == 0 {
		d.OwnerToken = NewID()
		d.ExpectedUID = ""
	} else {
		if d.Namespace != state.Desired.Namespace {
			return fmt.Errorf("%w: enrollment namespace cannot change", ErrInvalidDesired)
		}
		d.OwnerToken = state.Desired.OwnerToken
		d.ExpectedUID = state.Desired.ExpectedUID
	}
	d.AgentID = strings.ToLower(d.AgentID)
	if err = ValidateDesired(d); err != nil {
		return err
	}
	if !l.reconciler.namespaceAllowed(d.Namespace) {
		return ErrNamespaceNotAllowed
	}
	state.Version = 1
	state.Desired = d
	return rec.Save(state)
}

// BindingLease holds the enrollment lock through an execution, preventing
// concurrent disable/delete. This DEV implementation permits one active turn
// per enrolled agent across processes; it makes no worker-scaling claim.
type BindingLease struct {
	Binding
	record *LockedRecord
	state  enrollment
}

// ExecutionIntent fences every conversation and context generation for an agent.
// It contains only local correlation and remote identity, never remote diagnostics.
// The enrollment lock is acquired before conversation locks and held until all
// execution persistence completes. Separate filesystems are not coordinated.
type ExecutionIntent struct {
	Version                         int
	ConversationKey                 string
	AttemptID, MessageID            string
	InputHash, Generation, Endpoint string
	TaskID, SessionID               string
	SuppressOutput                  bool
	DeadlineExceeded                bool
	ObservationPending              bool
	OutputUncertain                 bool
}

func (b *BindingLease) Execution() *ExecutionIntent {
	if b.state.Execution == nil {
		return nil
	}
	copy := *b.state.Execution
	return &copy
}

// SaveExecution publishes with file fsync, atomic rename, and directory fsync.
// Clearing is permitted only after the conversation's terminal commit succeeds.
func (b *BindingLease) SaveExecution(intent *ExecutionIntent) error {
	next := b.state
	next.Execution = intent
	if err := b.record.Save(next); err != nil {
		return err
	}
	b.state = next
	return nil
}

func (b *BindingLease) Close() error { return b.record.Close() }

func (l *Lifecycle) Reconcile(ctx context.Context, agentID string) (Binding, error) {
	lease, err := l.Acquire(ctx, agentID)
	if err != nil {
		return Binding{}, err
	}
	defer lease.Close()
	return lease.Binding, nil
}
func (l *Lifecycle) Acquire(ctx context.Context, agentID string) (*BindingLease, error) {
	rec, state, err := l.record(agentID)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*BindingLease, error) { rec.Close(); return nil, err }
	if state.Version == 0 {
		return fail(ErrNotFound)
	}
	if state.Deleted {
		return fail(ErrDeleted)
	}
	out, err := l.reconciler.reconcile(ctx, state.Desired)
	if err != nil {
		return fail(err)
	}
	if out.UID == "" {
		return fail(fmt.Errorf("kagent: missing resource UID"))
	}
	if state.Desired.ExpectedUID == "" {
		if out.Action != ActionCreated {
			return fail(ErrNotOwned)
		}
		state.Desired.ExpectedUID = out.UID
		if err = rec.Save(state); err != nil {
			return fail(err)
		}
	}
	if out.unsupportedTemplate {
		return fail(ErrUnsupportedField)
	}
	if out.Terminating {
		return fail(ErrTerminating)
	}
	return &BindingLease{Binding: Binding{state.Desired, out}, record: rec, state: state}, nil
}

// Delete marks enrollment deleted before sending cleanup. drained is a caller
// assertion that new work is stopped and remote work is quiescent, not proof
// established by this package. The server must supply that proof before calling.
// The assertion is retained by the tombstone for recovery across restarts.
// nil means GET confirmed absence; accepted DELETE alone returns
// ErrDeletionPending. Retrying cleanup keeps the same UID binding. Tombstones
// and attempt fences are retained permanently; reenrollment remains blocked.
func (l *Lifecycle) Delete(ctx context.Context, agentID string, drained bool) error {
	if !drained {
		return ErrDrainRequired
	}
	rec, state, err := l.record(agentID)
	if err != nil {
		return err
	}
	defer rec.Close()
	if state.Version == 0 {
		return ErrNotFound
	}
	if !state.Deleted {
		state.Deleted = true
		if err = rec.Save(state); err != nil {
			return err
		}
	}
	return l.cleanupDeletion(ctx, rec, state)
}

// cleanupDeletion runs under the enrollment lock. Never infer ownership from
// copied labels/token, or clear finalizers to force deletion to complete.
func (l *Lifecycle) cleanupDeletion(ctx context.Context, rec *LockedRecord, state enrollment) error {
	if state.DeletionComplete {
		return nil
	}
	d := state.Desired
	pending := func(err error) error { return errors.Join(ErrDeletionPending, err) }
	if d.ExpectedUID == "" || d.OwnerToken == "" {
		return pending(ErrNotOwned)
	}
	if !l.reconciler.namespaceAllowed(d.Namespace) {
		return pending(ErrNamespaceNotAllowed)
	}
	name, err := ResourceName(d.AgentID)
	if err != nil {
		return pending(err)
	}
	observe := func() (bool, bool, error) {
		cur, err := l.reconciler.store.Get(ctx, d.Namespace, name)
		if errors.Is(err, ErrNotFound) {
			return true, false, nil
		}
		if err != nil {
			return false, false, err
		}
		if cur == nil || !ownedBy(cur, d) {
			return false, false, ErrNotOwned
		}
		return false, cur.Metadata.DeletionTimestamp != nil, nil
	}
	absent, terminating, err := observe()
	if err != nil {
		return pending(err)
	}
	if !absent {
		if terminating {
			return ErrDeletionPending
		}
		// The reconciler rechecks ownership and uses UID/resourceVersion
		// preconditions on each bounded retry. Even an ambiguous response must
		// be followed by observation, never interpreted as completion alone.
		deleteErr := l.reconciler.delete(ctx, d, true)
		absent, _, err = observe()
		if err != nil || !absent {
			return pending(errors.Join(deleteErr, err))
		}
	}
	state.DeletionComplete = true
	if err := rec.Save(state); err != nil {
		return pending(err)
	}
	return nil
}

// Recover observes cluster state for enrolled agents, cross-checked against
// the authoritative persisted enrollment records from the journal. Only
// resources matching a live, bound record (token, enrolled namespace and
// pinned UID) are reported as owned state; a resource that merely copies
// Multica labels or the owner-token annotation — even under the deterministic
// name — is never reported. Recovery makes no ownership claims for
// tombstoned or unbound enrollments. Pending tombstones are repaired under
// their enrollment locks using the caller drain assertion persisted by Delete.
// Cleanup failures (including ErrDeletionPending) are returned alongside live
// observations; one pending enrollment does not stop recovery of the others.
func (l *Lifecycle) Recover(ctx context.Context) ([]AgentState, error) {
	records, err := l.journal.records()
	if err != nil {
		return nil, err
	}
	var cleanupErr error
	for _, record := range records {
		if !record.Deleted {
			continue
		}
		rec, state, err := l.record(record.AgentID)
		if err == nil {
			if state.Deleted {
				err = l.cleanupDeletion(ctx, rec, state)
			}
			rec.Close()
		}
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("agent %s: %w", record.AgentID, errors.Join(ErrDeletionPending, err)))
		}
	}
	states, err := l.reconciler.Recover(ctx, records)
	return states, errors.Join(cleanupErr, err)
}
