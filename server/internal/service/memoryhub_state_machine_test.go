package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestBindingTransitionTable(t *testing.T) {
	// Legal transitions from the frozen v1.3 A2.1 table.
	legal := []struct{ from, to BindingStatus }{
		{BindingUnbound, BindingBinding},
		{BindingBinding, BindingBinding},
		{BindingBinding, BindingBound},
		{BindingBinding, BindingSyncFailed},
		{BindingBinding, BindingCompensating},
		{BindingBound, BindingUnbound},
		{BindingBound, BindingBinding},
		{BindingBound, BindingBound},
		{BindingBound, BindingSyncFailed},
		{BindingBound, BindingCompensating},
		{BindingSyncFailed, BindingBinding},
		{BindingSyncFailed, BindingBound},
		{BindingSyncFailed, BindingSyncFailed},
		{BindingSyncFailed, BindingCompensating},
		{BindingSyncFailed, BindingBlocked},
		{BindingCompensating, BindingUnbound},
		{BindingCompensating, BindingBinding},
		{BindingCompensating, BindingBound},
		{BindingCompensating, BindingCompensating},
		{BindingCompensating, BindingBlocked},
		{BindingBlocked, BindingBinding},
		{BindingBlocked, BindingBlocked},
	}
	for _, tr := range legal {
		if !ValidBindingTransition(tr.from, tr.to) {
			t.Fatalf("legal transition rejected: %s -> %s", tr.from, tr.to)
		}
	}
}

func TestBindingTransitionInvalid(t *testing.T) {
	// blocked -> unbound is explicitly invalid.
	if ValidBindingTransition(BindingBlocked, BindingUnbound) {
		t.Fatal("blocked -> unbound must be invalid")
	}
	// unbound cannot jump to bound / blocked.
	if ValidBindingTransition(BindingUnbound, BindingBound) {
		t.Fatal("unbound -> bound must be invalid")
	}
	if ValidBindingTransition(BindingUnbound, BindingBlocked) {
		t.Fatal("unbound -> blocked must be invalid")
	}
	// unbound has no self-loop.
	if ValidBindingTransition(BindingUnbound, BindingUnbound) {
		t.Fatal("unbound -> unbound must be invalid")
	}
}

func TestBindingIdempotencyKeyDeterministic(t *testing.T) {
	var scope pgtype.UUID
	a := bindingIdempotencyKey("ws-1", "workspace", scope, "issue", "sub-1")
	b := bindingIdempotencyKey("ws-1", "workspace", scope, "issue", "sub-1")
	if a != b {
		t.Fatal("idempotency key must be deterministic")
	}
	// Different subject -> different key.
	c := bindingIdempotencyKey("ws-1", "workspace", scope, "issue", "sub-2")
	if a == c {
		t.Fatal("different subject must produce different key")
	}
}
