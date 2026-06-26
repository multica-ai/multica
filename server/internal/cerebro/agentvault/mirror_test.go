package agentvault

import (
	"context"
	"errors"
	"sort"
	"testing"
)

// fakeAccessStore is an in-memory accessReconciler keyed by vault name (one
// agent, one workspace — enough for the reconcile unit).
type fakeAccessStore struct {
	rows     map[string]string // vault -> role
	setCalls int
	delCalls int
}

func newFakeAccessStore(initial map[string]string) *fakeAccessStore {
	rows := make(map[string]string, len(initial))
	for k, v := range initial {
		rows[k] = v
	}
	return &fakeAccessStore{rows: rows}
}

func (f *fakeAccessStore) ListForAgent(_ context.Context, _, _ string) ([]Access, error) {
	out := make([]Access, 0, len(f.rows))
	for v, r := range f.rows {
		out = append(out, Access{Vault: v, Role: r})
	}
	return out, nil
}

func (f *fakeAccessStore) SetAccess(_ context.Context, _, _, vault, role string) error {
	f.setCalls++
	f.rows[vault] = role
	return nil
}

func (f *fakeAccessStore) DeleteAccess(_ context.Context, _, _, vault string) error {
	f.delCalls++
	delete(f.rows, vault)
	return nil
}

func (f *fakeAccessStore) vaults() []string {
	out := make([]string, 0, len(f.rows))
	for v := range f.rows {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// fakeGrantSource returns a fixed desired set (or an error).
type fakeGrantSource struct {
	boxes []BoxGrant
	err   error
}

func (f fakeGrantSource) GrantedBoxes(_ context.Context, _, _ string) ([]BoxGrant, error) {
	return f.boxes, f.err
}

func TestReconcileAgentAccess_AddsKeepsDeletes(t *testing.T) {
	// Current: bravo (read-only), charlie (member). Desired: alpha + bravo.
	store := newFakeAccessStore(map[string]string{"bravo": "read-only", "charlie": "member"})
	src := fakeGrantSource{boxes: []BoxGrant{
		{Vault: "alpha", Role: "read-only"},
		{Vault: "bravo", Role: "read-only"},
	}}

	if err := reconcileAgentAccess(context.Background(), store, src, "ws", "agent"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	got := store.vaults()
	want := []string{"alpha", "bravo"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("vaults = %v, want %v", got, want)
	}
	// alpha was added (1 set); bravo already correct (no set); charlie deleted (1 del).
	if store.setCalls != 1 {
		t.Errorf("setCalls = %d, want 1 (only the new box)", store.setCalls)
	}
	if store.delCalls != 1 {
		t.Errorf("delCalls = %d, want 1 (only the dropped box)", store.delCalls)
	}
}

func TestReconcileAgentAccess_CorrectsRole(t *testing.T) {
	store := newFakeAccessStore(map[string]string{"alpha": "member"})
	src := fakeGrantSource{boxes: []BoxGrant{{Vault: "alpha", Role: "read-only"}}}

	if err := reconcileAgentAccess(context.Background(), store, src, "ws", "agent"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if store.rows["alpha"] != "read-only" {
		t.Errorf("alpha role = %q, want read-only (role corrected)", store.rows["alpha"])
	}
	if store.setCalls != 1 {
		t.Errorf("setCalls = %d, want 1 (role rewrite)", store.setCalls)
	}
}

func TestReconcileAgentAccess_EmptyGrantsClearsAll(t *testing.T) {
	// Deny-by-default: no chain grants ⇒ every existing box removed.
	store := newFakeAccessStore(map[string]string{"alpha": "read-only", "bravo": "member"})
	src := fakeGrantSource{boxes: nil}

	if err := reconcileAgentAccess(context.Background(), store, src, "ws", "agent"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("rows = %v, want empty (deny-by-default)", store.rows)
	}
	if store.delCalls != 2 {
		t.Errorf("delCalls = %d, want 2", store.delCalls)
	}
}

func TestReconcileAgentAccess_GrantSourceErrorLeavesTableUntouched(t *testing.T) {
	store := newFakeAccessStore(map[string]string{"alpha": "read-only"})
	src := fakeGrantSource{err: errors.New("chain resolve failed")}

	err := reconcileAgentAccess(context.Background(), store, src, "ws", "agent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Fail closed: nothing written or deleted; prior projection intact.
	if store.setCalls != 0 || store.delCalls != 0 {
		t.Errorf("table mutated on error: set=%d del=%d, want 0/0", store.setCalls, store.delCalls)
	}
	if store.rows["alpha"] != "read-only" {
		t.Errorf("alpha lost on error: %v", store.rows)
	}
}

func TestReconcileAgentAccess_BlankAndDefaultRole(t *testing.T) {
	// A blank vault is skipped; a blank role defaults to read-only.
	store := newFakeAccessStore(nil)
	src := fakeGrantSource{boxes: []BoxGrant{
		{Vault: "  ", Role: "read-only"},
		{Vault: "alpha", Role: ""},
	}}

	if err := reconcileAgentAccess(context.Background(), store, src, "ws", "agent"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(store.rows) != 1 || store.rows["alpha"] != "read-only" {
		t.Errorf("rows = %v, want {alpha: read-only}", store.rows)
	}
}

func TestVaultFromResourcePattern(t *testing.T) {
	cases := []struct {
		name      string
		pattern   string
		wantVault string
		wantOK    bool
	}{
		{"plain vault", "agentvault-vault:bigquery", "bigquery", true},
		{"trims surrounding space", "agentvault-vault:  cloudflare  ", "cloudflare", true},
		{"blank after prefix is not a grant", "agentvault-vault:   ", "", false},
		{"empty after prefix is not a grant", "agentvault-vault:", "", false},
		{"credential-id resource is not a vault", "cerebro-credential:1b9d-uuid", "", false},
		{"type resource is not a vault", "cerebro-credential-type:api_key", "", false},
		{"unrelated resource", "repo:firtal/cerebro", "", false},
		{"empty pattern", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vault, ok := VaultFromResourcePattern(c.pattern)
			if ok != c.wantOK || vault != c.wantVault {
				t.Fatalf("VaultFromResourcePattern(%q) = (%q, %v); want (%q, %v)",
					c.pattern, vault, ok, c.wantVault, c.wantOK)
			}
		})
	}
}
