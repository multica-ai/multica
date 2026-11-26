package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestDeviceLoginSavesCredentialAndMachineIdentity(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	machine := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/device/start":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			machine = in["client_name"]
			_ = json.NewEncoder(w).Encode(deviceStartResponse{DeviceCode: "secret", UserCode: "ABCD-2345", ExpiresIn: 600, Interval: 5})
		case "/auth/device/token":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["device_code"] != "secret" {
				t.Error("wrong polling code")
			}
			_ = json.NewEncoder(w).Encode(deviceTokenResponse{Token: "test-jwt"})
		case "/api/tokens":
			if r.Header.Get("Authorization") != "Bearer test-jwt" {
				t.Error("wrong exchange identity")
			}
			_, _ = w.Write([]byte(`{"token":"mul_test_device"}`))
		case "/api/me":
			_, _ = w.Write([]byte(`{"name":"Device Tester","email":"device@example.com"}`))
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_APP_URL", srv.URL)
	cmd := testCmd()
	if err := runAuthLoginDevice(cmd); err != nil {
		t.Fatal(err)
	}
	if machine != daemon.DefaultHostname() {
		t.Fatal("machine identity not sent", machine)
	}
	cfg, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "mul_test_device" || cfg.ServerURL != srv.URL || cfg.AppURL != srv.URL {
		t.Fatal("device login did not save config")
	}
}

// Ctrl-C cancels the command context; the wait loop must return promptly
// instead of sleeping out the poll interval until the code expires.
func TestDeviceLoginStopsWaitingWhenCancelled(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/device/start":
			_ = json.NewEncoder(w).Encode(deviceStartResponse{DeviceCode: "secret", UserCode: "ABCD-2345", ExpiresIn: 600, Interval: 5})
		case "/auth/device/token":
			cancel() // the user presses Ctrl-C while the CLI is waiting
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(deviceTokenResponse{Error: deviceAuthErrPending})
		default:
			t.Error("unexpected path", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_APP_URL", srv.URL)
	cmd := testCmd()
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- runAuthLoginDevice(cmd) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("device login kept waiting after cancellation")
	}
}
