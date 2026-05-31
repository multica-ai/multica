package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func brokerSecret(access, refresh string, exp time.Time) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte(access),
			"refresh_token": []byte(refresh),
			"expires_at":    []byte(exp.Format(time.RFC3339)),
		},
	}
}

func TestSync_WritesKeychainWhenMissing(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	k := fake.NewSimpleClientset(brokerSecret("ACCESS", "REFRESH", exp))
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}
	res, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if !res.Wrote {
		t.Error("expected write on first sync")
	}

	raw, err := kc.Read("claude", "u")
	if err != nil {
		t.Fatalf("keychain read: %v", err)
	}
	var got struct {
		ClaudeAiOauth struct {
			AccessToken      string   `json:"accessToken"`
			RefreshToken     string   `json:"refreshToken"`
			ExpiresAt        int64    `json:"expiresAt"`
			Scopes           []string `json:"scopes"`
			SubscriptionType string   `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if got.ClaudeAiOauth.AccessToken != "ACCESS" || got.ClaudeAiOauth.RefreshToken != "REFRESH" {
		t.Errorf("payload tokens wrong: %+v", got.ClaudeAiOauth)
	}
	if got.ClaudeAiOauth.SubscriptionType != "max" {
		t.Errorf("subscriptionType = %q", got.ClaudeAiOauth.SubscriptionType)
	}
	if len(got.ClaudeAiOauth.Scopes) != 4 {
		t.Errorf("scopes = %v", got.ClaudeAiOauth.Scopes)
	}
	if got.ClaudeAiOauth.ExpiresAt != exp.UnixMilli() {
		t.Errorf("ExpiresAt = %d want %d", got.ClaudeAiOauth.ExpiresAt, exp.UnixMilli())
	}
}

func TestSync_SkipsWhenUnchanged(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	k := fake.NewSimpleClientset(brokerSecret("A", "R", exp))
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}

	r1, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil {
		t.Fatalf("SyncOnce#1: %v", err)
	}
	if !r1.Wrote {
		t.Error("expected write on first sync")
	}

	r2, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil {
		t.Fatalf("SyncOnce#2: %v", err)
	}
	if r2.Wrote {
		t.Error("expected no-op on second sync with unchanged broker state")
	}
	if r2.OldFingerprint == "" || r2.OldFingerprint != r2.NewFingerprint {
		t.Errorf("fingerprints should match on no-op: old=%s new=%s", r2.OldFingerprint, r2.NewFingerprint)
	}
}

func TestSync_RewritesWhenTokenChanges(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}

	// First broker state.
	k1 := fake.NewSimpleClientset(brokerSecret("A1", "R1", exp))
	if _, err := SyncOnce(context.Background(), cfg, k1, kc, discardLogger()); err != nil {
		t.Fatalf("SyncOnce#1: %v", err)
	}
	// Broker rotates → new access/refresh tokens.
	k2 := fake.NewSimpleClientset(brokerSecret("A2", "R2", exp))
	r, err := SyncOnce(context.Background(), cfg, k2, kc, discardLogger())
	if err != nil {
		t.Fatalf("SyncOnce#2: %v", err)
	}
	if !r.Wrote {
		t.Error("expected re-write after broker rotation")
	}
	if r.OldFingerprint == r.NewFingerprint {
		t.Errorf("fingerprints should differ after rotation: %s", r.NewFingerprint)
	}
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	k := fake.NewSimpleClientset(brokerSecret("A", "R", exp))
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{
		Namespace: "multica", SecretName: "broker",
		KeychainService: "claude", KeychainAccount: "u",
		DryRun: true,
	}
	res, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger())
	if err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if res.Wrote {
		t.Error("dry-run must not write")
	}
	if _, err := kc.Read("claude", "u"); err == nil {
		t.Error("keychain should still be empty after dry-run")
	}
}

func TestSync_BrokerErrorPropagates(t *testing.T) {
	k := fake.NewSimpleClientset() // no secret
	kc := &stubKeychain{data: map[string][]byte{}}
	cfg := &Config{Namespace: "multica", SecretName: "broker", KeychainService: "claude", KeychainAccount: "u"}
	if _, err := SyncOnce(context.Background(), cfg, k, kc, discardLogger()); err == nil {
		t.Error("expected error when broker secret missing")
	}
}
