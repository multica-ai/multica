//go:build !windows

package kagent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func lifecycleForTest(t *testing.T, r *Reconciler, dir string) *Lifecycle {
	t.Helper()
	j, err := NewJournal(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLifecycle(r, j)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLifecycleTombstoneFencesStaleReconcilerAcrossRestart(t *testing.T) {
	r, store := newTestReconciler(t)
	dir := t.TempDir()
	l := lifecycleForTest(t, r, dir)
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	binding, err := l.Reconcile(context.Background(), d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Desired.ExpectedUID == "" || binding.Desired.OwnerToken == d.OwnerToken {
		t.Fatal("durable identity not generated and pinned")
	}
	// A second lifecycle instance represents a process with queued stale work.
	otherR, _ := New(store, testConfig())
	other := lifecycleForTest(t, otherR, dir)
	if err = l.Delete(context.Background(), d.AgentID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Reconcile(context.Background(), d.AgentID); !errors.Is(err, ErrDeleted) {
		t.Fatalf("stale reconciliation: %v", err)
	}
	if err = other.Enroll(d); !errors.Is(err, ErrDeleted) {
		t.Fatalf("silent reenrollment: %v", err)
	}
	name, _ := ResourceName(d.AgentID)
	if _, err = store.Get(context.Background(), d.Namespace, name); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted resource resurrected")
	}
	if store.count("create") != 1 {
		t.Fatal("stale writer issued create")
	}
}

func TestLifecycleLeaseBlocksDeleteAndBoundMissingResource(t *testing.T) {
	r, store := newTestReconciler(t)
	dir := t.TempDir()
	l := lifecycleForTest(t, r, dir)
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	lease, err := l.Acquire(context.Background(), d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	other := lifecycleForTest(t, r, dir)
	if err = other.Delete(context.Background(), d.AgentID, true); !errors.Is(err, ErrBusy) {
		t.Fatalf("deleted active enrollment: %v", err)
	}
	if err = other.Enroll(d); !errors.Is(err, ErrBusy) {
		t.Fatalf("edited active enrollment: %v", err)
	}
	lease.Close()
	name, _ := ResourceName(d.AgentID)
	delete(store.objs, key(d.Namespace, name))
	if _, err = l.Reconcile(context.Background(), d.AgentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing bound object recreated: %v", err)
	}
	if store.count("create") != 1 {
		t.Fatal("recreated bound object")
	}
}

func TestOwnershipRequiresEnrollmentTokenAndUID(t *testing.T) {
	r, store := newTestReconciler(t)
	d := testDesired()
	name := mustCreate(t, r, &d)
	obj := store.objs[key(d.Namespace, name)]
	delete(obj.Metadata.Annotations, AnnotationOwnerToken)
	if _, err := r.reconcile(context.Background(), d); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("label-only ownership accepted: %v", err)
	}
	obj.Metadata.Annotations[AnnotationOwnerToken] = d.OwnerToken
	d.ExpectedUID = "some-other-uid"
	if err := r.delete(context.Background(), d, true); !errors.Is(err, ErrNotOwned) {
		t.Fatalf("replaced resource deleted: %v", err)
	}
}

// TestLifecycleRecoverTrustsOnlyPersistedEnrollment is the journal-driven
// recovery regression: the persisted enrollment record is the only ownership
// authority. A resource that copies the ownership labels and the owner-token
// annotation of an enrolled agent — but not its pinned UID — is invisible to
// recovery, and a tombstoned enrollment claims nothing at all.
func TestLifecycleRecoverTrustsOnlyPersistedEnrollment(t *testing.T) {
	r, store := newTestReconciler(t)
	dir := t.TempDir()
	l := lifecycleForTest(t, r, dir)
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	lease, err := l.Acquire(context.Background(), d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	uid, name := lease.Outcome.UID, mustResourceName(lease.Desired.AgentID)
	states, err := l.Recover(context.Background())
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(states) != 1 || states[0].UID != uid || states[0].Name != name {
		t.Fatalf("expected the enrolled agent's bound state, got %+v", states)
	}

	// Forge: copy the ownership labels and the owner-token annotation from
	// the real resource onto another one. Labels and annotations have
	// identical write authority, so this is exactly what a hostile writer can
	// do; the journal pins only the original UID, so the copy must stay
	// unclaimed.
	forged := forgedResource(lease.Desired, "forged-agent", uid)
	store.objs["kagent-system/forged-agent"] = forged
	if states, err = l.Recover(context.Background()); err != nil {
		t.Fatalf("recover with forged resource: %v", err)
	}
	if len(states) != 1 || states[0].Name != name {
		t.Fatalf("forged resource recovered as owned state: %+v", states)
	}

	// Release the lease, then tombstone-delete: even the real resource's
	// state is no longer claimed once the record is Deleted.
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(context.Background(), d.AgentID, true); err != nil {
		t.Fatal(err)
	}
	// Stale-writer resurrection: forge the resource back under the
	// deterministic name with the copied token but a fresh server UID. The
	// tombstone must keep it unclaimed.
	store.objs[key(d.Namespace, name)] = forgedResource(lease.Desired, name, "forged-uid")
	if states, err = l.Recover(context.Background()); err != nil {
		t.Fatalf("recover after delete: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("tombstoned enrollment recovered as owned state: %+v", states)
	}
}

func mustResourceName(agentID string) string {
	name, err := ResourceName(agentID)
	if err != nil {
		panic(err)
	}
	return name
}

func TestReadinessRejectsFutureAndDuplicateConditions(t *testing.T) {
	for _, conditions := range [][]Condition{
		{{Type: ConditionReady, Status: "True", ObservedGeneration: 3}},
		{{Type: ConditionReady, Status: "True", ObservedGeneration: 2}, {Type: ConditionReady, Status: "False", ObservedGeneration: 2}},
	} {
		a := &Agent{Metadata: ObjectMeta{Generation: 2}, Status: AgentStatus{Conditions: conditions}}
		if ready, _ := readiness(a); ready {
			t.Fatalf("unsafe readiness accepted: %+v", conditions)
		}
	}
}

type deletionStore struct {
	Store
	get    func(context.Context, string, string) (*Agent, error)
	remove func(context.Context, string, string, DeletePreconditions) error
}

func (s *deletionStore) Get(ctx context.Context, ns, name string) (*Agent, error) {
	if s.get != nil {
		return s.get(ctx, ns, name)
	}
	return s.Store.Get(ctx, ns, name)
}

func (s *deletionStore) Delete(ctx context.Context, ns, name string, pre DeletePreconditions) error {
	if s.remove != nil {
		return s.remove(ctx, ns, name, pre)
	}
	return s.Store.Delete(ctx, ns, name, pre)
}

func readEnrollment(t *testing.T, l *Lifecycle, id string) enrollment {
	t.Helper()
	rec, state, err := l.record(id)
	if err != nil {
		t.Fatal(err)
	}
	rec.Close()
	return state
}

func TestLifecycleDeletionObservationAndRecovery(t *testing.T) {
	for _, mode := range []string{"accepted", "finalizer", "lost-before", "lost-after", "observation-failed", "delete-not-found", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			r, backing := newTestReconciler(t)
			transport := &deletionStore{Store: backing}
			r.store = transport
			dir := t.TempDir()
			l := lifecycleForTest(t, r, dir)
			d := testDesired()
			if err := l.Enroll(d); err != nil {
				t.Fatal(err)
			}
			binding, err := l.Reconcile(ctx, d.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			d = binding.Desired
			name := mustResourceName(d.AgentID)
			transportErr := errors.New("response lost")
			calls := 0
			// Inspect the durable tombstone without reacquiring the held lock.
			transport.remove = func(ctx context.Context, ns, name string, pre DeletePreconditions) error {
				calls++
				if pre.UID != d.ExpectedUID || pre.ResourceVersion != "rv-1" {
					t.Fatalf("incorrect delete preconditions: %+v", pre)
				}
				records, err := l.journal.records()
				if err != nil || len(records) != 1 || !records[0].Deleted {
					t.Fatalf("DELETE before durable tombstone: %+v, %v", records, err)
				}
				switch mode {
				case "finalizer":
					ts := "2026-11-18T00:00:00Z"
					backing.objs[key(ns, name)].Metadata.DeletionTimestamp = &ts
				case "lost-before":
					return transportErr
				case "lost-after":
					if err := backing.Delete(ctx, ns, name, pre); err != nil {
						t.Fatal(err)
					}
					return transportErr
				case "observation-failed":
					if err := backing.Delete(ctx, ns, name, pre); err != nil {
						t.Fatal(err)
					}
					transport.get = func(context.Context, string, string) (*Agent, error) { return nil, transportErr }
				case "delete-not-found":
					// A DELETE 404 alone must not complete cleanup.
					return ErrNotFound
				case "replacement":
					backing.objs[key(ns, name)] = forgedResource(d, name, "replacement-uid")
					return ErrPreconditionFailed
				}
				return nil
			}
			err = l.Delete(ctx, d.AgentID, true)
			if mode == "lost-after" {
				if err != nil {
					t.Fatalf("verified absence after ambiguous response: %v", err)
				}
			} else if !errors.Is(err, ErrDeletionPending) {
				t.Fatalf("must report pending cleanup: %v", err)
			}
			if mode == "lost-before" && !errors.Is(err, transportErr) {
				t.Fatalf("lost transport cause: %v", err)
			}
			if mode == "replacement" && !errors.Is(err, ErrNotOwned) {
				t.Fatalf("replacement ownership cause missing: %v", err)
			}
			if state := readEnrollment(t, l, d.AgentID); !state.Deleted || state.DeletionComplete != (mode == "lost-after") || state.Desired.ExpectedUID != d.ExpectedUID {
				t.Fatalf("incorrect durable phase/identity: %+v", state)
			}
			if calls != 1 {
				t.Fatalf("unexpected deletes (replacement must never be deleted): %d", calls)
			}
			transport.get = nil
			transport.remove = nil
			restarted := lifecycleForTest(t, r, dir)
			states, err := restarted.Recover(ctx)
			if len(states) != 0 {
				t.Fatalf("tombstone reported as live: %+v", states)
			}
			switch mode {
			case "finalizer", "replacement":
				if !errors.Is(err, ErrDeletionPending) {
					t.Fatalf("recovery must remain pending: %v", err)
				}
				if backing.count("delete") != 0 {
					t.Fatal("recovery deleted terminating/replacement object")
				}
				delete(backing.objs, key(d.Namespace, name)) // external controller finishes cleanup
				if _, err := restarted.Recover(ctx); err != nil {
					t.Fatalf("recovery after absence: %v", err)
				}
			default:
				if err != nil {
					t.Fatalf("recovery: %v", err)
				}
			}
			if state := readEnrollment(t, restarted, d.AgentID); !state.Deleted || !state.DeletionComplete {
				t.Fatalf("completion not durable: %+v", state)
			}
			if err := restarted.Enroll(d); !errors.Is(err, ErrDeleted) {
				t.Fatalf("reenrollment must remain blocked: %v", err)
			}
		})
	}
}

func TestLifecycleRecoverCrashAfterTombstone(t *testing.T) {
	r, store := newTestReconciler(t)
	dir := t.TempDir()
	l := lifecycleForTest(t, r, dir)
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reconcile(context.Background(), d.AgentID); err != nil {
		t.Fatal(err)
	}
	// Simulate the crash boundary, also the legacy Deleted-only journal format.
	rec, state, err := l.record(d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	state.Deleted = true
	if err := rec.Save(state); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	store.deleteConflicts = 1
	restarted := lifecycleForTest(t, r, dir)
	if _, err := restarted.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.count("delete") != 2 || !readEnrollment(t, restarted, d.AgentID).DeletionComplete {
		t.Fatal("recovery did not resume conditional cleanup after crash")
	}
}

func TestLifecycleDeleteUnboundFailsClosedAndRequiresCallerDrain(t *testing.T) {
	r, store := newTestReconciler(t)
	l := lifecycleForTest(t, r, t.TempDir())
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(context.Background(), d.AgentID, false); !errors.Is(err, ErrDrainRequired) {
		t.Fatalf("caller drain assertion required: %v", err)
	}
	if readEnrollment(t, l, d.AgentID).Deleted {
		t.Fatal("missing drain assertion wrote tombstone")
	}
	for _, present := range []bool{false, true} {
		if present {
			name := mustResourceName(d.AgentID)
			bound := readEnrollment(t, l, d.AgentID).Desired
			store.objs[key(d.Namespace, name)] = forgedResource(bound, name, "unbound-uid")
		}
		if err := l.Delete(context.Background(), d.AgentID, true); !errors.Is(err, ErrDeletionPending) || !errors.Is(err, ErrNotOwned) {
			t.Fatalf("unbound cleanup must fail closed (present=%v): %v", present, err)
		}
		if _, err := l.Recover(context.Background()); !errors.Is(err, ErrNotOwned) {
			t.Fatalf("recovery adopted unbound resource: %v", err)
		}
	}
	if store.count("delete") != 0 || readEnrollment(t, l, d.AgentID).DeletionComplete {
		t.Fatal("unbound enrollment deleted or completed")
	}
}

func TestLifecycleRecoveryPreservesFencesAndContinuesPastLockedDeletion(t *testing.T) {
	ctx := context.Background()
	r, store := newTestReconciler(t)
	l := lifecycleForTest(t, r, t.TempDir())
	d := testDesired()
	if err := l.Enroll(d); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reconcile(ctx, d.AgentID); err != nil {
		t.Fatal(err)
	}
	rec, state, err := l.record(d.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	state.Deleted = true
	if err := rec.Save(state); err != nil {
		t.Fatal(err)
	}
	other := testDesired()
	other.AgentID = NewID()
	if err := l.Enroll(other); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Reconcile(ctx, other.AgentID); err != nil {
		t.Fatal(err)
	}
	fence, err := l.journal.Lock("attempt/unknown-outcome")
	if err != nil {
		t.Fatal(err)
	}
	defer fence.Close()
	wantFence := map[string]string{"phase": "unknown", "messageID": NewID()}
	if err := fence.Save(wantFence); err != nil {
		t.Fatal(err)
	}
	states, err := l.Recover(ctx)
	if !errors.Is(err, ErrBusy) || !errors.Is(err, ErrDeletionPending) || len(states) != 1 || states[0].AgentID != other.AgentID {
		t.Fatalf("locked cleanup must preserve live observations: %+v, %v", states, err)
	}
	if store.count("delete") != 0 {
		t.Fatal("recovery bypassed enrollment lock")
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	var gotFence map[string]string
	if exists, err := fence.Load(&gotFence); err != nil || !exists || gotFence["phase"] != wantFence["phase"] || gotFence["messageID"] != wantFence["messageID"] {
		t.Fatalf("unknown attempt fence changed: %+v, %v", gotFence, err)
	}
}
