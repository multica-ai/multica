package main

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TokenState is the persistent OAuth state the broker manages. Stored as
// three Secret keys (access_token, refresh_token, expires_at) for easy
// inspection with `kubectl get secret -o jsonpath=...`.
type TokenState struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type SecretStore struct {
	k                 kubernetes.Interface
	namespace         string
	name              string
	accessTokenSecret string // separate, write-only mirror for worker pods to read
}

func NewSecretStore(k kubernetes.Interface, namespace, name, accessTokenSecret string) *SecretStore {
	return &SecretStore{k: k, namespace: namespace, name: name, accessTokenSecret: accessTokenSecret}
}

// Load reads the secret and decodes the three keys into a TokenState. A
// missing secret or missing refresh_token is fatal — the broker can't
// function without bootstrap state.
func (s *SecretStore) Load(ctx context.Context) (*TokenState, error) {
	sec, err := s.k.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("secret %s/%s not found — bootstrap required", s.namespace, s.name)
		}
		return nil, fmt.Errorf("load secret: %w", err)
	}
	state := &TokenState{
		AccessToken:  string(sec.Data["access_token"]),
		RefreshToken: string(sec.Data["refresh_token"]),
	}
	if rawExp, ok := sec.Data["expires_at"]; ok && len(rawExp) > 0 {
		t, err := time.Parse(time.RFC3339, string(rawExp))
		if err != nil {
			return nil, fmt.Errorf("parse expires_at %q: %w", rawExp, err)
		}
		state.ExpiresAt = t
	}
	if state.RefreshToken == "" {
		return nil, fmt.Errorf("secret %s/%s missing refresh_token", s.namespace, s.name)
	}
	return state, nil
}

// Store writes the three keys back. Creates the secret if it doesn't exist
// yet — useful in tests; in production the bootstrap procedure creates it
// up front (Task 15) and Store always finds it.
func (s *SecretStore) Store(ctx context.Context, state *TokenState) error {
	patch := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace},
		Data: map[string][]byte{
			"access_token":  []byte(state.AccessToken),
			"refresh_token": []byte(state.RefreshToken),
			"expires_at":    []byte(state.ExpiresAt.Format(time.RFC3339)),
		},
	}
	_, err := s.k.CoreV1().Secrets(s.namespace).Update(ctx, patch, metav1.UpdateOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		_, err = s.k.CoreV1().Secrets(s.namespace).Create(ctx, patch, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("store secret: %w", err)
	}
	return nil
}

// MirrorAccessToken writes ONLY the current access_token to a separate secret
// (named via accessTokenSecret). Worker Job pods inject this into the env as
// CLAUDE_CODE_OAUTH_TOKEN via secretKeyRef. Workers never see the refresh
// token — only short-lived access tokens — preserving the broker's
// "refresh_token lives in exactly one place" invariant.
//
// Idempotent — patches Update; falls back to Create on NotFound.
func (s *SecretStore) MirrorAccessToken(ctx context.Context, accessToken string) error {
	if s.accessTokenSecret == "" {
		return fmt.Errorf("MirrorAccessToken: accessTokenSecret name not configured")
	}
	patch := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: s.accessTokenSecret, Namespace: s.namespace},
		Data:       map[string][]byte{"access_token": []byte(accessToken)},
	}
	_, err := s.k.CoreV1().Secrets(s.namespace).Update(ctx, patch, metav1.UpdateOptions{})
	if err != nil && k8serrors.IsNotFound(err) {
		_, err = s.k.CoreV1().Secrets(s.namespace).Create(ctx, patch, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("mirror access_token: %w", err)
	}
	return nil
}
