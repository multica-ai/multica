package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForDaemonStartup(t *testing.T) {
	const pid = 12345
	cases := []struct {
		name      string
		profile   string
		recordPID int
		responses []string
		wantReady bool
		wantError bool
	}{
		{name: "no port record", profile: "startup", wantError: true},
		{name: "stale port record", profile: "startup", recordPID: pid + 1,
			responses: []string{`{"status":"starting","profile":"startup","pid":12346}`}, wantError: true},
		{name: "no health response", profile: "startup", recordPID: pid,
			responses: []string{"unavailable"}, wantError: true},
		{name: "starting", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"startup","pid":12345}`}},
		{name: "running", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"running","profile":"startup","pid":12345}`}, wantReady: true},
		{name: "starting with wrong profile", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"other","pid":12345}`}, wantError: true},
		{name: "starting with wrong PID", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"startup","pid":12346}`}, wantError: true},
		{name: "starting without profile", recordPID: pid,
			responses: []string{`{"status":"starting","pid":12345}`}, wantError: true},
		{name: "starting without PID", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"startup"}`}, wantError: true},
		{name: "starting with unreadable profile", recordPID: pid,
			responses: []string{`{"status":"starting","profile":null,"pid":12345}`}, wantError: true},
		{name: "running with wrong profile", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"running","profile":"other","pid":12345}`}, wantError: true},
		{name: "running with wrong PID", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"running","profile":"startup","pid":12346}`}, wantError: true},
		{name: "default profile starting", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"","pid":12345}`}},
		{name: "default profile running", recordPID: pid,
			responses: []string{`{"status":"running","profile":"","pid":12345}`}, wantReady: true},
		{name: "starting then temporary health failure", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"startup","pid":12345}`, "unavailable"}},
		{name: "starting then running", profile: "startup", recordPID: pid,
			responses: []string{`{"status":"starting","profile":"startup","pid":12345}`, `{"status":"running","profile":"startup","pid":12345}`}, wantReady: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearDaemonTaskEnv(t)
			mkProfiles(t, "startup")
			var requests atomic.Int32
			if tc.recordPID != 0 {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/health" {
						t.Errorf("unexpected request to %s", r.URL.Path)
						http.NotFound(w, r)
						return
					}
					index := min(int(requests.Add(1))-1, len(tc.responses)-1)
					fmt.Fprint(w, tc.responses[index])
				}))
				t.Cleanup(server.Close)
				if err := publishDaemonPort(tc.profile, daemonPortRecord{
					Port: server.Listener.Addr().(*net.TCPAddr).Port,
					PID:  tc.recordPID,
				}); err != nil {
					t.Fatal(err)
				}
			}
			logs := daemonStartupLogs{
				logPath:    filepath.Join(daemonDirForProfile(tc.profile), "daemon.log"),
				errLogPath: filepath.Join(daemonDirForProfile(tc.profile), "daemon.err.log"),
			}
			timeout := 100 * time.Millisecond
			if len(tc.responses) > 1 {
				timeout = 750 * time.Millisecond
			}
			stderr := captureStderr(t)
			defer stderr.restore()
			// An open channel models a child that stays alive. No daemon is
			// launched or signalled; all health endpoints belong to this test.
			ready, err := waitForDaemonStartup(make(chan error), pid, tc.profile, timeout, logs, "")
			out := stderr.read()
			if ready != tc.wantReady || (err != nil) != tc.wantError {
				t.Fatalf("waitForDaemonStartup = (%v, %v), want ready=%v error=%v", ready, err, tc.wantReady, tc.wantError)
			}
			if tc.wantError {
				for _, hint := range []string{"timed out", timeout.String(), logs.logPath, logs.errLogPath} {
					if !strings.Contains(err.Error(), hint) {
						t.Errorf("error %q missing %q", err, hint)
					}
				}
			} else if !tc.wantReady && !strings.Contains(out, "Daemon is still starting") {
				t.Errorf("stderr = %q, want slow-start warning", out)
			}
			if tc.recordPID != pid && requests.Load() != 0 {
				t.Errorf("probed a stale port record %d times", requests.Load())
			}
			if tc.recordPID == pid && int(requests.Load()) < len(tc.responses) {
				t.Errorf("health requests = %d, want at least %d", requests.Load(), len(tc.responses))
			}
		})
	}
}
