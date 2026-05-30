package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type stubLeader struct{ leader bool }

func (s *stubLeader) IsLeader() bool { return s.leader }

func makeRefresher(t *testing.T, initial *TokenState, isLeader bool, srvURL string) *Refresher {
	t.Helper()
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
		Data: map[string][]byte{
			"access_token":  []byte(initial.AccessToken),
			"refresh_token": []byte(initial.RefreshToken),
			"expires_at":    []byte(initial.ExpiresAt.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(sec)
	store := NewSecretStore(k, "ns", "s")
	oauth := newClientForTest(srvURL, "client-id-x", "oauth-2025-04-20")
	return NewRefresher(store, &stubLeader{leader: isLeader}, oauth, 5*time.Minute)
}

func TestRefreshIfNeeded_StillFresh_NoRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("Anthropic must not be called when token is fresh")
	}))
	defer srv.Close()
	state := &TokenState{
		AccessToken:  "A",
		RefreshToken: "R",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	r := makeRefresher(t, state, true, srv.URL)
	refreshed, _, err := r.RefreshIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("RefreshIfNeeded: %v", err)
	}
	if refreshed {
		t.Errorf("expected no refresh for fresh token")
	}
}

func TestRefreshIfNeeded_ExpiringButNotLeader_ReturnsCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("non-leader must not call Anthropic")
	}))
	defer srv.Close()
	state := &TokenState{
		AccessToken:  "A",
		RefreshToken: "R",
		ExpiresAt:    time.Now().Add(1 * time.Minute), // within refresh pad
	}
	r := makeRefresher(t, state, false, srv.URL)
	refreshed, cached, err := r.RefreshIfNeeded(context.Background())
	if !errors.Is(err, ErrNotLeader) {
		t.Errorf("expected ErrNotLeader, got %v", err)
	}
	if refreshed {
		t.Errorf("non-leader refreshed")
	}
	if cached == nil || cached.AccessToken != "A" {
		t.Errorf("non-leader didn't return cached state: %+v", cached)
	}
}

func TestRefreshIfNeeded_LeaderRefreshes(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ACCESS_NEW",
			"refresh_token": "REFRESH_NEW",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	state := &TokenState{
		AccessToken:  "ACCESS_OLD",
		RefreshToken: "REFRESH_OLD",
		ExpiresAt:    time.Now().Add(2 * time.Minute), // within refresh pad
	}
	r := makeRefresher(t, state, true, srv.URL)
	refreshed, newState, err := r.RefreshIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("RefreshIfNeeded: %v", err)
	}
	if !refreshed {
		t.Errorf("expected refresh")
	}
	if newState.AccessToken != "ACCESS_NEW" || newState.RefreshToken != "REFRESH_NEW" {
		t.Errorf("unexpected new state: %+v", newState)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("server calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}

func TestRefreshIfNeeded_LeaderRefresh_EmptyRotatedTokenKeepsOld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ACCESS_NEW",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()
	state := &TokenState{
		AccessToken:  "ACCESS_OLD",
		RefreshToken: "REFRESH_OLD",
		ExpiresAt:    time.Now().Add(1 * time.Minute),
	}
	r := makeRefresher(t, state, true, srv.URL)
	_, newState, err := r.RefreshIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("RefreshIfNeeded: %v", err)
	}
	if newState.RefreshToken != "REFRESH_OLD" {
		t.Errorf("expected refresh_token to be preserved when server omits it; got %q", newState.RefreshToken)
	}
}

func TestRefreshIfNeeded_LeaderPermanentError_PreservesCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	state := &TokenState{
		AccessToken:  "ACCESS_OLD",
		RefreshToken: "REFRESH_OLD",
		ExpiresAt:    time.Now().Add(1 * time.Minute),
	}
	r := makeRefresher(t, state, true, srv.URL)
	refreshed, cached, err := r.RefreshIfNeeded(context.Background())
	var perm *PermanentError
	if !errors.As(err, &perm) {
		t.Errorf("expected PermanentError, got %T: %v", err, err)
	}
	if refreshed {
		t.Errorf("must not mark refreshed on permanent error")
	}
	if cached == nil || cached.AccessToken != "ACCESS_OLD" {
		t.Errorf("cached state not preserved: %+v", cached)
	}
}
