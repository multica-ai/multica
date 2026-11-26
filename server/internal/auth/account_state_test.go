package auth

import (
	"context"
	"testing"
)

// A request that read the database before a suspension committed must not
// overwrite the suspension in the cache with its stale answer.
func TestAccountStates_StaleFillDoesNotOverwritePut(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()
	const userID = "account-state-race-user"
	rdb.Del(ctx, accountStateKey(userID))
	t.Cleanup(func() { rdb.Del(ctx, accountStateKey(userID)) })

	var accounts *AccountStates
	accounts = NewAccountStates(rdb, func(context.Context, string) (AccountState, error) {
		// The suspension commits and is published while this read is in flight.
		accounts.Put(ctx, userID, AccountState{Suspended: true})
		return AccountState{}, nil
	})

	if _, err := accounts.Get(ctx, userID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	state, err := accounts.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !state.Suspended {
		t.Fatal("a stale fill overwrote the suspension")
	}
}

func TestAccountStates_WithoutRedisReadsEveryTime(t *testing.T) {
	calls := 0
	accounts := NewAccountStates(nil, func(context.Context, string) (AccountState, error) {
		calls++
		return AccountState{Suspended: calls > 1}, nil
	})
	ctx := context.Background()
	if state, _ := accounts.Get(ctx, "u"); state.Suspended {
		t.Fatal("first read should not be suspended")
	}
	if state, _ := accounts.Get(ctx, "u"); !state.Suspended {
		t.Fatal("second read should see the suspension")
	}
}

func TestAccountState_CheckSession(t *testing.T) {
	if err := (AccountState{Suspended: true}).CheckSession(5); err != ErrTemporarilyDisabledUser {
		t.Fatalf("suspended: %v", err)
	}
	if err := (AccountState{TokenVersion: 2}).CheckSession(1); err != ErrSessionRevoked {
		t.Fatalf("older session: %v", err)
	}
	if err := (AccountState{TokenVersion: 2}).CheckSession(2); err != nil {
		t.Fatalf("current session: %v", err)
	}
}
