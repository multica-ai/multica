package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agentcli"
)

type cliUpgradeFakeRunner struct {
	out string
	err error
}

func (f *cliUpgradeFakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f.out, f.err
}

func withFastAgentCLIUpgradeDrain(t *testing.T, d time.Duration) {
	t.Helper()
	prev := agentCLIUpgradeDrainTimeout
	agentCLIUpgradeDrainTimeout = d
	t.Cleanup(func() { agentCLIUpgradeDrainTimeout = prev })
}

func TestBeginAgentCLIUpgradeDrain_WaitsForIdleThenSucceeds(t *testing.T) {
	withFastAgentCLIUpgradeDrain(t, time.Second)
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.activeTasks.Store(1)

	done := make(chan error, 1)
	go func() {
		done <- d.beginAgentCLIUpgradeDrain(context.Background())
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.claimMu.Lock()
		paused := d.pauseClaims
		d.claimMu.Unlock()
		if paused {
			break
		}
		time.Sleep(time.Millisecond)
	}
	d.claimMu.Lock()
	if !d.pauseClaims {
		d.claimMu.Unlock()
		t.Fatal("expected pauseClaims during drain")
	}
	d.claimMu.Unlock()

	select {
	case <-done:
		t.Fatal("drain returned before tasks finished")
	case <-time.After(30 * time.Millisecond):
	}

	d.activeTasks.Store(0)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("drain err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not finish after idle")
	}
	if !d.updating.Load() {
		t.Fatal("updating flag should remain set until caller releases")
	}
	d.releaseClaimBarrier()
	d.updating.Store(false)
}

func TestBeginAgentCLIUpgradeDrain_TimeoutDoesNotKill(t *testing.T) {
	withFastAgentCLIUpgradeDrain(t, 40*time.Millisecond)
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.activeTasks.Store(1)

	err := d.beginAgentCLIUpgradeDrain(context.Background())
	if !errors.Is(err, errAgentCLIUpgradeDrainTO) {
		t.Fatalf("err = %v, want drain timeout", err)
	}
	if d.updating.Load() {
		t.Fatal("updating should clear on drain timeout")
	}
	d.claimMu.Lock()
	paused := d.pauseClaims
	d.claimMu.Unlock()
	if paused {
		t.Fatal("pauseClaims should clear on drain timeout")
	}
	if got := d.activeTasks.Load(); got != 1 {
		t.Fatalf("activeTasks = %d, drain must not kill tasks", got)
	}
}

func TestBeginAgentCLIUpgradeDrain_BusyWhenUpdating(t *testing.T) {
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	d.updating.Store(true)
	err := d.beginAgentCLIUpgradeDrain(context.Background())
	if !errors.Is(err, errAgentCLIUpgradeBusy) {
		t.Fatalf("err = %v, want busy", err)
	}
}

func TestHandleAgentCLIUpgrade_GrokFailsWithNoInstallSource(t *testing.T) {
	withFastUpdateReportBackoffs(t)
	withFastAgentCLIUpgradeDrain(t, time.Second)

	var last map[string]any
	var reports atomic.Int32
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		reports.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		last = payload
		w.WriteHeader(http.StatusOK)
	})

	d.handleAgentCLIUpgrade(context.Background(), "runtime-1", &PendingAgentCLIUpgrade{
		ID:  "job-grok",
		CLI: "grok",
	})

	if got := reports.Load(); got < 2 {
		t.Fatalf("reports = %d, want at least draining + failed", got)
	}
	if last["status"] != "failed" {
		t.Fatalf("final status = %v, want failed", last["status"])
	}
	errMsg, _ := last["error"].(string)
	if !strings.Contains(errMsg, "grok") || !strings.Contains(errMsg, "no registry package") {
		t.Fatalf("error = %q, want registry-package message", errMsg)
	}
	if d.updating.Load() {
		t.Fatal("updating should clear after failure")
	}
}

func TestHandleAgentCLIUpgrade_SucceedsAfterDrain(t *testing.T) {
	withFastUpdateReportBackoffs(t)
	withFastAgentCLIUpgradeDrain(t, time.Second)

	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	prev := managedCLIUpgrader
	managedCLIUpgrader = func(*Daemon) *agentcli.Upgrader {
		return &agentcli.Upgrader{
			Runner:  &cliUpgradeFakeRunner{out: "ok"},
			HomeDir: home,
			Prefix:  filepath.Join(home, ".local"),
			LookPath: func(string) (string, error) {
				return "", os.ErrNotExist
			},
			Stat: os.Stat,
			ProbeOne: func(ctx context.Context, cli string) (agentcli.Probe, error) {
				return agentcli.Probe{CLI: cli, Current: "9.9.9", Path: bin}, nil
			},
		}
	}
	t.Cleanup(func() { managedCLIUpgrader = prev })

	var last map[string]any
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		last = payload
		w.WriteHeader(http.StatusOK)
	})
	d.activeTasks.Store(1)

	done := make(chan struct{})
	go func() {
		d.handleAgentCLIUpgrade(context.Background(), "runtime-1", &PendingAgentCLIUpgrade{
			ID:  "job-codex",
			CLI: "codex",
		})
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.claimMu.Lock()
		paused := d.pauseClaims
		d.claimMu.Unlock()
		if paused {
			break
		}
		time.Sleep(time.Millisecond)
	}
	d.activeTasks.Store(0)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade did not finish")
	}
	if last["status"] != "succeeded" {
		t.Fatalf("final status = %v payload=%v, want succeeded", last["status"], last)
	}
	if last["current"] != "9.9.9" {
		t.Fatalf("current = %v, want 9.9.9", last["current"])
	}
	if d.updating.Load() {
		t.Fatal("updating should clear after success")
	}
	d.claimMu.Lock()
	paused := d.pauseClaims
	d.claimMu.Unlock()
	if paused {
		t.Fatal("pauseClaims should clear after success")
	}
}

func TestHandleAgentCLIUpgrade_BusyRequeuesNotFailed(t *testing.T) {
	withFastUpdateReportBackoffs(t)

	var statuses []string
	d, _ := updateReportDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		if s, _ := payload["status"].(string); s != "" {
			statuses = append(statuses, s)
		}
		w.WriteHeader(http.StatusOK)
	})
	d.updating.Store(true)

	d.handleAgentCLIUpgrade(context.Background(), "runtime-1", &PendingAgentCLIUpgrade{
		ID:  "job-busy",
		CLI: "codex",
	})

	if len(statuses) == 0 {
		t.Fatal("expected at least one status report")
	}
	last := statuses[len(statuses)-1]
	if last != "queued" {
		t.Fatalf("final status = %q (all=%v), want queued (Busy must not fail)", last, statuses)
	}
	for _, s := range statuses {
		if s == "failed" {
			t.Fatalf("statuses = %v, Busy path must never report failed", statuses)
		}
	}
	if !d.updating.Load() {
		t.Fatal("pre-existing updating flag must remain set")
	}
}
