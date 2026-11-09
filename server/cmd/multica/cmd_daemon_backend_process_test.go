package main

import (
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real process/HTTP/OS-lock fixture. It never discovers or starts agent CLIs.
func TestBackendHandoffServiceProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_BACKEND_SERVICE") != "1" {
		t.Skip("subprocess fixture")
	}
	t.Setenv("HOME", os.Getenv("MULTICA_TEST_SERVICE_HOME"))
	t.Setenv("USERPROFILE", os.Getenv("MULTICA_TEST_SERVICE_HOME"))
	profile := "handoff-fixture"
	if err := os.MkdirAll(daemonDirForProfile(profile), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireDaemonProfileLock(profile, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	var draining atomic.Bool
	stop := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		count := 1
		if draining.Load() {
			count = 0
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "running", "pid": os.Getpid(), "profile": profile, "os": runtime.GOOS, "owner_id": "a", "active_task_count": count, "claims_in_flight": count, "draining": draining.Load()})
	})
	mux.HandleFunc("/drain", func(w http.ResponseWriter, r *http.Request) {
		draining.Store(r.Method == http.MethodPost)
		w.WriteHeader(200)
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); stop <- struct{}{} })
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	if err := writeDaemonShutdownResult(profile, daemonShutdownResult{PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := publishDaemonPort(profile, daemonPortRecord{Port: port, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stop:
	case <-time.After(20 * time.Second):
		t.Fatal("fixture shutdown timed out")
	}
	server.Close()
	confirmed := os.Getenv("MULTICA_TEST_UNCONFIRMED") != "1"
	if err := writeDaemonShutdownResult(profile, daemonShutdownResult{PID: os.Getpid(), CleanupConfirmed: confirmed}); err != nil {
		t.Fatal(err)
	}
}

func TestBackendHandoffConfirmsProcessAndTaskCleanup(t *testing.T) {
	for _, unconfirmed := range []bool{false, true} {
		t.Run(strconv.FormatBool(unconfirmed), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			child := exec.Command(os.Args[0], "-test.run=^TestBackendHandoffServiceProcess$")
			child.Env = append(os.Environ(), "MULTICA_TEST_BACKEND_SERVICE=1", "MULTICA_TEST_SERVICE_HOME="+home, "MULTICA_TEST_UNCONFIRMED="+map[bool]string{false: "0", true: "1"}[unconfirmed])
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { child.Process.Kill(); child.Wait() }()
			deadline := time.Now().Add(10 * time.Second)
			var record daemonPortRecord
			for time.Now().Before(deadline) {
				if r, ok := readDaemonPortRecord("handoff-fixture"); ok {
					record = r
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if record.PID == 0 {
				t.Fatal("fixture never published its connection")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			health := checkDaemonHealthOnPort(ctx, record.Port)
			cancel()
			cmd := &cobra.Command{}
			cmd.Flags().String("profile", "handoff-fixture", "")
			cmd.Flags().String("server-url", "https://example.test", "")
			cmd.Flags().Int("expected-pid", record.PID+1, "")
			if err := runDaemonStop(cmd, nil); err == nil || !strings.Contains(err.Error(), "changed after confirmation") {
				t.Fatalf("stale confirmation reached stop: %v", err)
			}
			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			if current := checkDaemonHealthOnPort(ctx, record.Port); !daemonAlive(current) {
				t.Fatal("stale confirmation stopped the service")
			}
			cancel()
			err := drainAndStopBackend(&backendServiceRecord{Profile: "handoff-fixture", PID: record.PID, Port: record.Port}, health, false)
			if (err != nil) != unconfirmed {
				t.Fatalf("cleanup result should gate replacement: %v", err)
			}
			stopped, stopErr := daemonStopComplete("handoff-fixture")
			if stopErr != nil || !stopped {
				t.Fatalf("process still owns lock: %v", stopErr)
			}

		})
	}
}
