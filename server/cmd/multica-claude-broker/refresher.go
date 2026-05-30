package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrNotLeader is returned when a refresh was requested on a broker pod
// that isn't currently the lease holder. HTTP callers and the background
// loop treat this as "serve cached and try again later" — refresh is a
// leader-only action.
var ErrNotLeader = errors.New("not the leader; refresh skipped")

// LeaderGate is the subset of LeaderState that Refresher needs. Pulling it
// out lets tests pass a stub without spinning up an actual elector.
type LeaderGate interface {
	IsLeader() bool
}

// Refresher composes the Secret store, leader gate, and OAuth client.
type Refresher struct {
	store      *SecretStore
	leader     LeaderGate
	oauth      *OAuthClient
	refreshPad time.Duration
}

func NewRefresher(store *SecretStore, leader LeaderGate, oauth *OAuthClient, refreshPad time.Duration) *Refresher {
	return &Refresher{store: store, leader: leader, oauth: oauth, refreshPad: refreshPad}
}

// RefreshIfNeeded loads the current state and, if we're the leader and the
// access_token is within RefreshPad of expiry, calls Anthropic and persists
// the rotated state. Returns (refreshed, current_state, err).
//
// Errors:
//   - ErrNotLeader: serve cached if non-expired; the next leader will refresh.
//   - *PermanentError (oauth_client): operator must intervene; cached preserved.
//   - *TransientError: caller serves cached; next tick retries.
func (r *Refresher) RefreshIfNeeded(ctx context.Context) (bool, *TokenState, error) {
	state, err := r.store.Load(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("load: %w", err)
	}

	// Still fresh enough — leadership doesn't matter for this branch.
	if !state.ExpiresAt.IsZero() && time.Until(state.ExpiresAt) > r.refreshPad {
		return false, state, nil
	}

	if !r.leader.IsLeader() {
		return false, state, ErrNotLeader
	}

	res, err := r.oauth.Refresh(ctx, state.RefreshToken)
	if err != nil {
		// Preserve cached state. Classification preserved via errors.As.
		return false, state, err
	}

	newState := &TokenState{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
	}
	if newState.RefreshToken == "" {
		// Anthropic occasionally omits the rotated value when re-using the
		// existing refresh_token is acceptable. Keep the old one.
		newState.RefreshToken = state.RefreshToken
	}
	if err := r.store.Store(ctx, newState); err != nil {
		return false, state, fmt.Errorf("persist new state: %w", err)
	}
	return true, newState, nil
}
