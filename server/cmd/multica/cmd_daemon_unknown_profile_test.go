package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// mkProfiles creates ~/.multica/profiles/<name> for each name under a fresh
// temp HOME and returns that HOME. It also moves the test off the current
// working directory: inDaemonManagedExecutionContext() detects a daemon task
// from a workdir marker as well as from env, so a suite running inside a
// managed task would otherwise see every command as task-scoped.
func mkProfiles(t *testing.T, names ...string) string {
	t.Helper()
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range names {
		dir := filepath.Join(home, ".multica", "profiles", filepath.FromSlash(name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create profile %q: %v", name, err)
		}
		// A profile is a directory holding a config.json; the file is what
		// separates a real profile from a bare parent directory.
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write config for profile %q: %v", name, err)
		}
	}
	return home
}

// daemonStatusCmdFor builds a cobra command carrying the flags runDaemonStatus reads.
func daemonStatusCmdFor(t *testing.T, profile, output string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("output", "table", "")
	if profile != "" {
		if err := cmd.Flags().Set("profile", profile); err != nil {
			t.Fatalf("set profile flag: %v", err)
		}
	}
	if output != "" {
		if err := cmd.Flags().Set("output", output); err != nil {
			t.Fatalf("set output flag: %v", err)
		}
	}
	return cmd
}

func TestRequireKnownProfile(t *testing.T) {
	t.Run("default profile is never validated", func(t *testing.T) {
		mkProfiles(t)
		if err := requireKnownProfile(""); err != nil {
			t.Fatalf("requireKnownProfile(\"\") = %v, want nil", err)
		}
	})

	t.Run("existing profile passes", func(t *testing.T) {
		mkProfiles(t, "dev", "desktop-api.multica.ai")
		if err := requireKnownProfile("desktop-api.multica.ai"); err != nil {
			t.Fatalf("requireKnownProfile = %v, want nil", err)
		}
	})

	t.Run("unknown profile lists the known ones sorted", func(t *testing.T) {
		mkProfiles(t, "prod", "dev", "desktop-api.multica.ai")

		err := requireKnownProfile("desktop-api.multica")
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("requireKnownProfile = %v, want *unknownProfileError", err)
		}
		if unknown.Profile != "desktop-api.multica" {
			t.Fatalf("Profile = %q, want the name the user passed", unknown.Profile)
		}
		want := []string{"desktop-api.multica.ai", "dev", "prod"}
		if strings.Join(unknown.Known, ",") != strings.Join(want, ",") {
			t.Fatalf("Known = %v, want %v (sorted)", unknown.Known, want)
		}
		msg := unknown.Error()
		// The whole point of #6694: the message must name the typo AND the
		// real profile, so the fix is visible without further digging.
		if !strings.Contains(msg, `"desktop-api.multica"`) || !strings.Contains(msg, "desktop-api.multica.ai") {
			t.Fatalf("error message %q must name both the bad profile and the known ones", msg)
		}
	})

	// Regression: profile names may contain separators. `multica --profile
	// team/dev config set ...` creates ~/.multica/profiles/team/dev, so
	// validating against a flat top-level listing would reject a profile the
	// same CLI had just created, and suggest its parent "team" instead.
	t.Run("nested profile created by the CLI is accepted", func(t *testing.T) {
		mkProfiles(t, "team/dev", "dev")
		if err := requireKnownProfile("team/dev"); err != nil {
			t.Fatalf("requireKnownProfile(\"team/dev\") = %v, want nil", err)
		}
	})

	t.Run("a bare parent directory is not offered as a profile", func(t *testing.T) {
		mkProfiles(t, "team/dev")

		// "team" exists on disk but holds no config.json, so it is a parent,
		// not something the user could pass to --profile.
		err := requireKnownProfile("nope")
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("requireKnownProfile = %v, want *unknownProfileError", err)
		}
		want := []string{"team/dev"}
		if strings.Join(unknown.Known, ",") != strings.Join(want, ",") {
			t.Fatalf("Known = %v, want %v", unknown.Known, want)
		}
	})

	t.Run("login hint quotes an awkward profile name", func(t *testing.T) {
		mkProfiles(t)

		err := requireKnownProfile("my profile")
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("requireKnownProfile = %v, want *unknownProfileError", err)
		}
		if !strings.Contains(unknown.Error(), "--profile 'my profile'") {
			t.Fatalf("error message %q should shell-quote the name so the hint is copy-pasteable", unknown.Error())
		}
	})

	t.Run("no profiles root yet points at login", func(t *testing.T) {
		mkProfiles(t)

		err := requireKnownProfile("staging")
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("requireKnownProfile = %v, want *unknownProfileError", err)
		}
		if len(unknown.Known) != 0 {
			t.Fatalf("Known = %v, want empty", unknown.Known)
		}
		if !strings.Contains(unknown.Error(), "multica login --profile staging") {
			t.Fatalf("error message %q should tell the user how to create it", unknown.Error())
		}
	})
}

func TestDaemonStatusUnknownProfile(t *testing.T) {
	t.Run("text mode fails without touching stdout", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "desktop-api.multica.ai")

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "desktop-api.multica", ""), nil)
		})
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("runDaemonStatus = %v, want *unknownProfileError", err)
		}
		if strings.Contains(out, "stopped") {
			t.Fatalf("stdout = %q, must not claim 'stopped' for an unknown profile", out)
		}
	})

	t.Run("json mode prints one document and exits non-zero", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t, "desktop-api.multica.ai", "dev")

		out, err := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "desktop-api.multica", "json"), nil)
		})
		// errSilent keeps the stderr copy away while still exiting non-zero.
		if !errors.Is(err, errSilent) {
			t.Fatalf("runDaemonStatus = %v, want errSilent", err)
		}
		if code := cli.ExitCodeFor(err); code == 0 {
			t.Fatalf("exit code = %d, want non-zero", code)
		}

		var payload struct {
			Status        string   `json:"status"`
			Profile       string   `json:"profile"`
			KnownProfiles []string `json:"known_profiles"`
		}
		if err := json.Unmarshal([]byte(out), &payload); err != nil {
			t.Fatalf("stdout is not a single valid JSON document: %v\n%s", err, out)
		}
		if payload.Status != "unknown_profile" {
			t.Fatalf("status = %q, want unknown_profile", payload.Status)
		}
		if payload.Profile != "desktop-api.multica" {
			t.Fatalf("profile = %q, want the name the user passed", payload.Profile)
		}
		want := []string{"desktop-api.multica.ai", "dev"}
		if strings.Join(payload.KnownProfiles, ",") != strings.Join(want, ",") {
			t.Fatalf("known_profiles = %v, want %v", payload.KnownProfiles, want)
		}
	})

	t.Run("json mode renders known_profiles as [] not null", func(t *testing.T) {
		clearDaemonTaskEnv(t)
		mkProfiles(t)

		out, _ := captureStdout(t, func() error {
			return runDaemonStatus(daemonStatusCmdFor(t, "staging", "json"), nil)
		})
		if !strings.Contains(out, `"known_profiles": []`) {
			t.Fatalf("stdout = %q, want an empty array rather than null", out)
		}
	})
}

// A known profile must keep reporting "stopped" when its daemon is not
// running: the new check narrows nothing for legitimate names.
func TestDaemonStatusKnownProfileStillReportsStopped(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t, "definitely-idle-profile")

	out, err := captureStdout(t, func() error {
		return runDaemonStatus(daemonStatusCmdFor(t, "definitely-idle-profile", ""), nil)
	})
	if err != nil {
		t.Fatalf("runDaemonStatus = %v, want nil", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("stdout = %q, want a stopped report", out)
	}
}

// End-to-end counterpart of the nested-profile regression: `daemon status` on
// a profile the CLI created as "team/dev" must fall through to the normal
// health probe rather than stopping at validation.
func TestDaemonStatusNestedProfileStillProbes(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t, "team/dev")

	out, err := captureStdout(t, func() error {
		return runDaemonStatus(daemonStatusCmdFor(t, "team/dev", ""), nil)
	})
	if err != nil {
		t.Fatalf("runDaemonStatus = %v, want nil", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("stdout = %q, want the probe result for a valid nested profile", out)
	}
}

// The default profile owns ~/.multica directly and has no profiles/ entry, so
// it must never be validated — scripts calling plain `daemon status` are the
// most common caller and must be untouched.
func TestDaemonStatusDefaultProfileNeverValidated(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t)

	out, err := captureStdout(t, func() error {
		return runDaemonStatus(daemonStatusCmdFor(t, "", "json"), nil)
	})
	if err != nil {
		t.Fatalf("runDaemonStatus = %v, want nil", err)
	}
	if strings.Contains(out, "unknown_profile") {
		t.Fatalf("stdout = %q, default profile must not be validated", out)
	}
}

// `daemon start` is deliberately NOT validated: creating a brand-new profile
// still goes through the existing login flow, which needs to accept a name
// that does not exist on disk yet.
func TestDaemonStartAcceptsUnknownProfile(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t)

	cmd := daemonStatusCmdFor(t, "brand-new-profile", "")
	cmd.Flags().Bool("foreground", false, "")
	err := runDaemonBackground(cmd)

	var unknown *unknownProfileError
	if errors.As(err, &unknown) {
		t.Fatal("daemon start must not reject an unknown profile; it is how new profiles are created")
	}
	// It fails for the pre-existing reason instead: nobody is logged in.
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("runDaemonBackground = %v, want the not-logged-in error", err)
	}
}

// The other lifecycle commands must reject an unknown profile too, and must do
// it before acting: `stop` on a mistyped profile silently no-ops today, which
// reads as "already stopped" for a daemon that is still running elsewhere.
func TestDaemonLifecycleCommandsRejectUnknownProfile(t *testing.T) {
	cases := []struct {
		name string
		run  func(*cobra.Command) error
	}{
		{"stop", func(c *cobra.Command) error { return runDaemonStop(c, nil) }},
		{"restart", func(c *cobra.Command) error { return runDaemonRestart(c, nil) }},
		{"logs", func(c *cobra.Command) error { return runDaemonLogs(c, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearDaemonTaskEnv(t)
			mkProfiles(t, "desktop-api.multica.ai")

			cmd := daemonStatusCmdFor(t, "desktop-api.multica", "")
			cmd.Flags().Bool("follow", false, "")
			cmd.Flags().Int("lines", 50, "")

			var unknown *unknownProfileError
			if err := tc.run(cmd); !errors.As(err, &unknown) {
				t.Fatalf("daemon %s = %v, want *unknownProfileError", tc.name, err)
			}
		})
	}
}

// Ordering guard: inside a daemon-managed task the --profile rejection must
// fire BEFORE requireKnownProfile, otherwise listing the profiles root would
// disclose the Owner's profile names to a task.
func TestDaemonStatusTaskContextRejectsProfileBeforeListing(t *testing.T) {
	clearDaemonTaskEnv(t)
	mkProfiles(t, "owner-secret-profile")
	t.Setenv("MULTICA_TASK_ID", "task-test")
	t.Setenv("MULTICA_DAEMON_PORT", "19601")

	out, err := captureStdout(t, func() error {
		return runDaemonStatus(daemonStatusCmdFor(t, "whatever", "json"), nil)
	})
	if err == nil {
		t.Fatal("runDaemonStatus = nil, want the task-context rejection")
	}
	var unknown *unknownProfileError
	if errors.As(err, &unknown) {
		t.Fatal("task-context guard must fire before profile validation")
	}
	if !strings.Contains(err.Error(), "not available inside a daemon-managed task") {
		t.Fatalf("error = %v, want the task-context rejection", err)
	}
	if strings.Contains(out, "owner-secret-profile") {
		t.Fatalf("stdout = %q, must never disclose Owner profile names inside a task", out)
	}
}
