package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestClient_RedeemRequestSecret_UsesDaemonToken asserts the redeem call
// authenticates with the DAEMON token (not the task's mat_ token, which the
// agent runtime also holds), POSTs the handle AND the task id, and targets the
// /api/daemon redeem path.
func TestClient_RedeemRequestSecret_UsesDaemonToken(t *testing.T) {
	const daemonToken = "mdt_daemon_token"
	var gotAuth, gotPath, gotHandle, gotTaskID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var in struct {
			Handle string `json:"handle"`
			TaskID string `json:"task_id"`
		}
		_ = json.Unmarshal(body, &in)
		gotHandle = in.Handle
		gotTaskID = in.TaskID
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"secret": "jwt-xyz"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken(daemonToken)

	secret, err := c.RedeemRequestSecret(context.Background(), "task-uuid-1", "handle-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "jwt-xyz" {
		t.Fatalf("expected secret jwt-xyz, got %q", secret)
	}
	if gotAuth != "Bearer "+daemonToken {
		t.Fatalf("expected daemon token in Authorization, got %q", gotAuth)
	}
	if gotPath != requestSecretRedeemPath {
		t.Fatalf("expected path %s, got %s", requestSecretRedeemPath, gotPath)
	}
	if !strings.HasPrefix(gotPath, "/api/daemon/") {
		t.Fatalf("redeem must target the daemon route, got %s", gotPath)
	}
	if gotHandle != "handle-123" {
		t.Fatalf("expected handle forwarded, got %q", gotHandle)
	}
	if gotTaskID != "task-uuid-1" {
		t.Fatalf("expected task id forwarded, got %q", gotTaskID)
	}
	// Daemon-level token untouched.
	if c.Token() != daemonToken {
		t.Fatalf("daemon token mutated by redeem: %q", c.Token())
	}
}

func TestClient_RedeemRequestSecret_404IsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"request secret handle not redeemable"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("mdt_daemon_token")
	_, err := c.RedeemRequestSecret(context.Background(), "task-uuid-1", "handle-123")
	if !errors.Is(err, errRequestSecretHandleRejected) {
		t.Fatalf("expected errRequestSecretHandleRejected on 404, got %v", err)
	}
}

func TestClient_RedeemRequestSecret_RequiresInputs(t *testing.T) {
	c := NewClient("http://unused.invalid")
	c.SetToken("mdt_daemon_token")
	// Missing task id must be refused before any network call.
	if _, err := c.RedeemRequestSecret(context.Background(), "", "handle-123"); err == nil {
		t.Fatal("expected error for empty task id")
	}
	// Missing handle must be refused.
	if _, err := c.RedeemRequestSecret(context.Background(), "task-uuid-1", ""); err == nil {
		t.Fatal("expected error for empty handle")
	}
	// Missing daemon token must be refused.
	c2 := NewClient("http://unused.invalid")
	if _, err := c2.RedeemRequestSecret(context.Background(), "task-uuid-1", "handle-123"); err == nil {
		t.Fatal("expected error when daemon token missing")
	}
}

func TestClient_RedeemRequestSecret_EmptySecretIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"secret": ""})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetToken("mdt_daemon_token")
	if _, err := c.RedeemRequestSecret(context.Background(), "task-uuid-1", "handle-123"); err == nil {
		t.Fatal("expected error when server returns empty secret")
	}
}
