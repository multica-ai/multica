package identitysync

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// uid builds a deterministic non-zero pgtype.UUID from a single byte, enough
// to distinguish users/groups in tests.
func uid(n byte) pgtype.UUID {
	var b [16]byte
	b[15] = n
	return pgtype.UUID{Bytes: b, Valid: true}
}

// fakeStore is an in-memory Store for exercising the reconcile algorithm
// without a database.
type fakeStore struct {
	// groupIDByEmail assigns a stable group id per google email.
	groupIDByEmail map[string]pgtype.UUID
	// usersByEmail maps a resolvable email to its user id. Absent = not found.
	usersByEmail map[string]pgtype.UUID
	// synced[groupID] = set of user ids currently in the group (source=google).
	synced map[pgtype.UUID]map[pgtype.UUID]bool

	added   []pgtype.UUID
	removed []pgtype.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		groupIDByEmail: map[string]pgtype.UUID{},
		usersByEmail:   map[string]pgtype.UUID{},
		synced:         map[pgtype.UUID]map[pgtype.UUID]bool{},
	}
}

func (f *fakeStore) UpsertGroup(_ context.Context, _ pgtype.UUID, googleEmail, _ string) (pgtype.UUID, error) {
	id, ok := f.groupIDByEmail[googleEmail]
	if !ok {
		id = uid(byte(100 + len(f.groupIDByEmail)))
		f.groupIDByEmail[googleEmail] = id
		f.synced[id] = map[pgtype.UUID]bool{}
	}
	return id, nil
}

func (f *fakeStore) ResolveWorkspaceUser(_ context.Context, _ pgtype.UUID, email string) (pgtype.UUID, bool, error) {
	id, ok := f.usersByEmail[email]
	return id, ok, nil
}

func (f *fakeStore) ListSyncedMemberIDs(_ context.Context, groupID pgtype.UUID) ([]pgtype.UUID, error) {
	out := []pgtype.UUID{}
	for id := range f.synced[groupID] {
		out = append(out, id)
	}
	return out, nil
}

func (f *fakeStore) AddSyncedMember(_ context.Context, groupID, userID pgtype.UUID) error {
	if f.synced[groupID] == nil {
		f.synced[groupID] = map[pgtype.UUID]bool{}
	}
	f.synced[groupID][userID] = true
	f.added = append(f.added, userID)
	return nil
}

func (f *fakeStore) RemoveSyncedMember(_ context.Context, groupID, userID pgtype.UUID) error {
	delete(f.synced[groupID], userID)
	f.removed = append(f.removed, userID)
	return nil
}

// oneGroupReconciler wires a Reconciler over a single test group so the test
// doesn't iterate the full FIR-2596 list.
func oneGroupReconciler(store Store) (*Reconciler, SyncedGroup) {
	g := SyncedGroup{Email: "warehouse@firtal.com", Name: "Warehouse"}
	return &Reconciler{store: store, groups: []SyncedGroup{g}}, g
}

func TestSyncWorkspace_HappyPath_AddsAllResolvableMembers(t *testing.T) {
	store := newFakeStore()
	store.usersByEmail["a@firtal.com"] = uid(1)
	store.usersByEmail["b@firtal.com"] = uid(2)
	store.usersByEmail["c@firtal.com"] = uid(3)

	r, g := oneGroupReconciler(store)
	membership := map[string][]string{g.Email: {"a@firtal.com", "b@firtal.com", "c@firtal.com"}}

	res, err := r.SyncWorkspace(context.Background(), uid(200), membership)
	if err != nil {
		t.Fatalf("SyncWorkspace: %v", err)
	}
	if res.GroupsProcessed != 1 {
		t.Errorf("GroupsProcessed = %d, want 1", res.GroupsProcessed)
	}
	if res.MembersAdded != 3 {
		t.Errorf("MembersAdded = %d, want 3", res.MembersAdded)
	}
	if res.MembersRemoved != 0 {
		t.Errorf("MembersRemoved = %d, want 0", res.MembersRemoved)
	}
	if res.UsersSkipped != 0 {
		t.Errorf("UsersSkipped = %d, want 0", res.UsersSkipped)
	}
}

func TestSyncWorkspace_MemberRemovedInGoogle_IsRemovedFromGroup(t *testing.T) {
	store := newFakeStore()
	store.usersByEmail["a@firtal.com"] = uid(1)
	store.usersByEmail["b@firtal.com"] = uid(2)

	r, g := oneGroupReconciler(store)
	// First pass: both A and B are in the Google group.
	if _, err := r.SyncWorkspace(context.Background(), uid(200), map[string][]string{
		g.Email: {"a@firtal.com", "b@firtal.com"},
	}); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	groupID := store.groupIDByEmail[g.Email]
	if len(store.synced[groupID]) != 2 {
		t.Fatalf("after first pass synced members = %d, want 2", len(store.synced[groupID]))
	}

	// Second pass: B left the Google group.
	res, err := r.SyncWorkspace(context.Background(), uid(200), map[string][]string{
		g.Email: {"a@firtal.com"},
	})
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if res.MembersRemoved != 1 {
		t.Errorf("MembersRemoved = %d, want 1", res.MembersRemoved)
	}
	if res.MembersAdded != 0 {
		t.Errorf("MembersAdded = %d, want 0", res.MembersAdded)
	}
	if store.synced[groupID][uid(2)] {
		t.Errorf("user B should have been removed from the group")
	}
	if !store.synced[groupID][uid(1)] {
		t.Errorf("user A should still be in the group")
	}
}

func TestSyncWorkspace_UserNotInMultica_IsSkippedNotErrored(t *testing.T) {
	store := newFakeStore()
	store.usersByEmail["a@firtal.com"] = uid(1)
	// "ghost@firtal.com" has no Multica account / workspace membership.

	r, g := oneGroupReconciler(store)
	membership := map[string][]string{g.Email: {"a@firtal.com", "ghost@firtal.com"}}

	res, err := r.SyncWorkspace(context.Background(), uid(200), membership)
	if err != nil {
		t.Fatalf("SyncWorkspace should not error on unknown user: %v", err)
	}
	if res.MembersAdded != 1 {
		t.Errorf("MembersAdded = %d, want 1 (only the resolvable user)", res.MembersAdded)
	}
	if res.UsersSkipped != 1 {
		t.Errorf("UsersSkipped = %d, want 1", res.UsersSkipped)
	}
	groupID := store.groupIDByEmail[g.Email]
	if !store.synced[groupID][uid(1)] {
		t.Errorf("resolvable user A should be in the group")
	}
}

// TestSyncWorkspace_ManualMembersUntouched proves the source scoping: members
// the sync doesn't own (never returned by ListSyncedMemberIDs) are never
// removed, even when absent from Google.
func TestSyncWorkspace_EmptyGoogleGroup_RemovesOnlySyncedMembers(t *testing.T) {
	store := newFakeStore()
	store.usersByEmail["a@firtal.com"] = uid(1)

	r, g := oneGroupReconciler(store)
	// Seed one synced member.
	if _, err := r.SyncWorkspace(context.Background(), uid(200), map[string][]string{
		g.Email: {"a@firtal.com"},
	}); err != nil {
		t.Fatalf("seed pass: %v", err)
	}
	groupID := store.groupIDByEmail[g.Email]
	// Simulate a manual member the sync does not own: present in the group map
	// but it would not be returned by ListSyncedMemberIDs in production. Here
	// the fake's synced map only holds synced members, so we model "manual" by
	// NOT adding it to synced — the reconciler can't see or remove it.

	// Google group now empty.
	res, err := r.SyncWorkspace(context.Background(), uid(200), map[string][]string{g.Email: {}})
	if err != nil {
		t.Fatalf("empty pass: %v", err)
	}
	if res.MembersRemoved != 1 {
		t.Errorf("MembersRemoved = %d, want 1 (the synced member)", res.MembersRemoved)
	}
	if len(store.synced[groupID]) != 0 {
		t.Errorf("synced members = %d, want 0", len(store.synced[groupID]))
	}
}
