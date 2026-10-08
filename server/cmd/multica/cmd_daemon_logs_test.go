package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// daemonLogsCmdFor builds a cobra command carrying the flags runDaemonLogs
// reads, wired to a buffer so what the command tells the user is observable.
func daemonLogsCmdFor(t *testing.T, profile string, lines int, follow bool) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	cmd := daemonStatusCmdFor(t, profile, "")
	cmd.Flags().BoolP("follow", "f", false, "")
	cmd.Flags().IntP("lines", "n", 50, "")
	if err := cmd.Flags().Set("lines", strconv.Itoa(lines)); err != nil {
		t.Fatalf("set lines flag: %v", err)
	}
	if follow {
		if err := cmd.Flags().Set("follow", "true"); err != nil {
			t.Fatalf("set follow flag: %v", err)
		}
	}
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	return cmd, &errOut
}

// tailCall records what runDaemonLogs handed to the platform tail, plus what
// the user had already been told at that moment.
type tailCall struct {
	called       bool
	path         string
	lines        int
	follow       bool
	stderrAtCall string
}

// stubTailLog replaces the tail seam so the test observes the command without
// spawning `tail` (and without a follow-mode run that never returns).
func stubTailLog(t *testing.T, errOut *bytes.Buffer) *tailCall {
	t.Helper()
	rec := &tailCall{}
	prev := tailLog
	t.Cleanup(func() { tailLog = prev })
	tailLog = func(path string, lines int, follow bool) error {
		rec.called = true
		rec.path, rec.lines, rec.follow = path, lines, follow
		rec.stderrAtCall = errOut.String()
		return nil
	}
	return rec
}

func seedDaemonLog(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("hello from the daemon\n"), 0o644); err != nil {
		t.Fatalf("seed daemon log: %v", err)
	}
}

// The default profile and a named one resolve to different files, and only the
// named-profile layout was missing from the docs — a Desktop install logs to
// ~/.multica/profiles/<name>/daemon.log while ~/.multica/daemon.log may still
// hold a readable, stale log from an earlier default-profile daemon (#6038).
func TestDaemonLogSourcePathResolvesPerProfile(t *testing.T) {
	t.Run("default profile", func(t *testing.T) {
		home := mkProfiles(t)

		got, err := daemonLogSourcePath("")
		if err != nil {
			t.Fatalf("daemonLogSourcePath(\"\") = %v, want nil", err)
		}
		want := filepath.Join(home, ".multica", "daemon.log")
		if got != want {
			t.Fatalf("daemonLogSourcePath(\"\") = %q, want %q", got, want)
		}
	})

	t.Run("named profile", func(t *testing.T) {
		home := mkProfiles(t, "desktop-api.multica.ai")

		got, err := daemonLogSourcePath("desktop-api.multica.ai")
		if err != nil {
			t.Fatalf("daemonLogSourcePath = %v, want nil", err)
		}
		want := filepath.Join(home, ".multica", "profiles", "desktop-api.multica.ai", "daemon.log")
		if got != want {
			t.Fatalf("daemonLogSourcePath = %q, want %q", got, want)
		}
	})

	t.Run("result is absolute", func(t *testing.T) {
		mkProfiles(t, "dev")
		for _, profile := range []string{"", "dev"} {
			got, err := daemonLogSourcePath(profile)
			if err != nil {
				t.Fatalf("daemonLogSourcePath(%q) = %v, want nil", profile, err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("daemonLogSourcePath(%q) = %q, want an absolute path", profile, got)
			}
		}
	})
}

// The success path used to be silent about which file it was reading, so the
// only command that knows the answer withheld it exactly when it worked. Both
// profile layouts must announce the absolute path, for plain reads and for
// -n / -f alike.
func TestRunDaemonLogsAnnouncesResolvedPath(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		label   string
		lines   int
		follow  bool
	}{
		{"default profile", "", "default", 50, false},
		{"default profile with lines", "", "default", 100, false},
		{"named profile", "desktop-api.multica.ai", "desktop-api.multica.ai", 50, false},
		{"named profile following", "desktop-api.multica.ai", "desktop-api.multica.ai", 50, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearDaemonTaskEnv(t)
			mkProfiles(t, "desktop-api.multica.ai")

			wantPath, err := daemonLogSourcePath(tc.profile)
			if err != nil {
				t.Fatalf("daemonLogSourcePath: %v", err)
			}
			seedDaemonLog(t, wantPath)

			cmd, errOut := daemonLogsCmdFor(t, tc.profile, tc.lines, tc.follow)
			rec := stubTailLog(t, errOut)

			if err := runDaemonLogs(cmd, nil); err != nil {
				t.Fatalf("runDaemonLogs = %v, want nil", err)
			}

			if !strings.Contains(errOut.String(), wantPath) {
				t.Errorf("daemon logs output = %q, want the resolved path %q", errOut.String(), wantPath)
			}
			if !strings.Contains(errOut.String(), tc.label) {
				t.Errorf("daemon logs output = %q, want the profile label %q", errOut.String(), tc.label)
			}

			if !rec.called {
				t.Fatal("runDaemonLogs did not tail the log")
			}
			if rec.path != wantPath {
				t.Errorf("tailed %q, want %q — the announced path must be the one read", rec.path, wantPath)
			}
			if rec.lines != tc.lines || rec.follow != tc.follow {
				t.Errorf("tail(lines=%d, follow=%v), want (lines=%d, follow=%v)", rec.lines, rec.follow, tc.lines, tc.follow)
			}
			// In follow mode the tail never returns, so a path announced
			// afterwards would never be seen; it must already be out.
			if !strings.Contains(rec.stderrAtCall, wantPath) {
				t.Errorf("path was not announced before tailing started (stderr at call = %q)", rec.stderrAtCall)
			}
		})
	}
}

// The announcement goes to stderr, so `multica daemon logs | grep ...` and a
// redirect to a file keep seeing log content only.
func TestRunDaemonLogsAnnouncementDoesNotPolluteStdout(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t, "dev")

	wantPath, err := daemonLogSourcePath("dev")
	if err != nil {
		t.Fatalf("daemonLogSourcePath: %v", err)
	}
	seedDaemonLog(t, wantPath)

	cmd, errOut := daemonLogsCmdFor(t, "dev", 50, false)
	var out bytes.Buffer
	cmd.SetOut(&out)
	stubTailLog(t, errOut)

	stdout, err := captureStdout(t, func() error { return runDaemonLogs(cmd, nil) })
	if err != nil {
		t.Fatalf("runDaemonLogs = %v, want nil", err)
	}
	if strings.Contains(stdout, wantPath) || strings.Contains(out.String(), wantPath) {
		t.Errorf("the path must not go to stdout (os.Stdout=%q, cmd out=%q)", stdout, out.String())
	}
}

// The missing-file error already named a path; it must name the absolute one
// for the profile that was asked for, not the default profile's.
func TestRunDaemonLogsMissingFileNamesProfilePath(t *testing.T) {
	clearDaemonTaskEnv(t)
	home := mkProfiles(t, "desktop-api.multica.ai")

	// A readable log for the DEFAULT profile exists — the exact trap from
	// #6038. The error must still point at the requested profile's path.
	seedDaemonLog(t, filepath.Join(home, ".multica", "daemon.log"))

	cmd, errOut := daemonLogsCmdFor(t, "desktop-api.multica.ai", 50, false)
	rec := stubTailLog(t, errOut)

	err := runDaemonLogs(cmd, nil)
	if err == nil {
		t.Fatal("runDaemonLogs = nil, want the missing-log-file error")
	}
	want := filepath.Join(home, ".multica", "profiles", "desktop-api.multica.ai", "daemon.log")
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to name %q", err, want)
	}
	if rec.called {
		t.Error("runDaemonLogs tailed a file it had just reported missing")
	}
}
