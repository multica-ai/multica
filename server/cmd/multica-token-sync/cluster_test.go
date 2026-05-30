package main

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClusterReader_ReadBrokerState(t *testing.T) {
	exp := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "multica-claude-oauth-broker", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("ACCESS"),
			"refresh_token": []byte("REFRESH"),
			"expires_at":    []byte(exp.Format(time.RFC3339)),
		},
	}
	k := fake.NewSimpleClientset(sec)
	state, err := ReadBrokerState(context.Background(), k, "multica", "multica-claude-oauth-broker")
	if err != nil {
		t.Fatalf("ReadBrokerState: %v", err)
	}
	if state.AccessToken != "ACCESS" || state.RefreshToken != "REFRESH" {
		t.Errorf("state = %+v", state)
	}
	if !state.ExpiresAt.Equal(exp) {
		t.Errorf("expires = %v, want %v", state.ExpiresAt, exp)
	}
}

func TestClusterReader_MissingSecret(t *testing.T) {
	k := fake.NewSimpleClientset()
	if _, err := ReadBrokerState(context.Background(), k, "multica", "missing"); err == nil {
		t.Error("expected error for missing Secret")
	}
}

func TestClusterReader_MissingTokenKeys(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "multica"},
		Data:       map[string][]byte{"expires_at": []byte("2026-06-01T00:00:00Z")},
	}
	k := fake.NewSimpleClientset(sec)
	_, err := ReadBrokerState(context.Background(), k, "multica", "b")
	if err == nil {
		t.Fatal("expected error for missing access_token / refresh_token")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Errorf("error should mention access_token: %v", err)
	}
}

func TestClusterReader_MalformedExpiresAt(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("A"),
			"refresh_token": []byte("R"),
			"expires_at":    []byte("not-a-timestamp"),
		},
	}
	k := fake.NewSimpleClientset(sec)
	if _, err := ReadBrokerState(context.Background(), k, "multica", "b"); err == nil {
		t.Error("expected error for malformed expires_at")
	}
}

func TestClusterReader_AbsentExpiresAtIsZero(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "multica"},
		Data: map[string][]byte{
			"access_token":  []byte("A"),
			"refresh_token": []byte("R"),
		},
	}
	k := fake.NewSimpleClientset(sec)
	state, err := ReadBrokerState(context.Background(), k, "multica", "b")
	if err != nil {
		t.Fatalf("ReadBrokerState: %v", err)
	}
	if !state.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt should be zero when key absent, got %v", state.ExpiresAt)
	}
}
