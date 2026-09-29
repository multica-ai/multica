package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// autostartCmdFor builds a command carrying the flags the autostart commands
// read (profile, output).
func autostartCmdFor(t *testing.T, profile, output string) *cobra.Command {
	t.Helper()
	return daemonStatusCmdFor(t, profile, output)
}

func TestAutostartArgs(t *testing.T) {
	t.Parallel()

	if got, want := strings.Join(autostartArgs(""), " "), "daemon start --foreground"; got != want {
		t.Errorf("autostartArgs(default) = %q, want %q", got, want)
	}
	if got, want := strings.Join(autostartArgs("staging"), " "), "daemon start --foreground --profile staging"; got != want {
		t.Errorf("autostartArgs(staging) = %q, want %q", got, want)
	}
}

func TestAutostartProfileSlug(t *testing.T) {
	t.Parallel()

	safe := []string{"staging", "desktop-api.multica.ai", "dev_2", "prod-eu"}
	for _, profile := range safe {
		if got := autostartProfileSlug(profile); got != profile {
			t.Errorf("autostartProfileSlug(%q) = %q, want the profile unchanged", profile, got)
		}
	}

	// Separators and other unsafe characters must not survive into a
	// filename or unit name...
	slug := autostartProfileSlug("team/dev")
	if strings.ContainsAny(slug, `/\`) || slug == "team/dev" {
		t.Errorf("autostartProfileSlug(team/dev) = %q, want a separator-free token", slug)
	}
	// ...and two profiles that sanitize to the same prefix must stay
	// distinguishable, or one daemon's registration would overwrite the
	// other's.
	if autostartProfileSlug("team/dev") == autostartProfileSlug("team-dev") {
		t.Errorf("slugs of team/dev and team-dev collide; the hash suffix must keep them distinct")
	}
	// Deterministic: the same profile must map to the same entry forever.
	if autostartProfileSlug("team/dev") != autostartProfileSlug("team/dev") {
		t.Errorf("autostartProfileSlug is not deterministic")
	}
}

func TestWindowsAutostartCommand(t *testing.T) {
	t.Parallel()

	const exe = `C:\Program Files\multica\multica.exe`
	got := windowsAutostartCommand(autostartSpec{Exe: exe, Args: autostartArgs("")})
	want := `"C:\Program Files\multica\multica.exe" daemon start --foreground`
	if got != want {
		t.Errorf("windowsAutostartCommand(default) = %q, want %q", got, want)
	}

	got = windowsAutostartCommand(autostartSpec{Exe: exe, Args: autostartArgs("staging")})
	want = `"C:\Program Files\multica\multica.exe" daemon start --foreground --profile staging`
	if got != want {
		t.Errorf("windowsAutostartCommand(staging) = %q, want %q", got, want)
	}
}

func TestWindowsQuoteArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"daemon", "daemon"},
		{`C:\multica.exe`, `C:\multica.exe`},
		{"", `""`},
		{"a b", `"a b"`},
		// Trailing backslashes inside a quoted run must be doubled or the
		// parser counts them as escaping the closing quote.
		{`C:\dir with space\`, `"C:\dir with space\\"`},
		{`C:\dir \`, `"C:\dir \\"`},
		{`say "hi"`, `"say \"hi\""`},
	}
	for _, c := range cases {
		if got := windowsQuoteArg(c.in); got != c.want {
			t.Errorf("windowsQuoteArg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWindowsRunValueName(t *testing.T) {
	t.Parallel()

	if got := windowsRunValueName(""); got != "Multica" {
		t.Errorf("windowsRunValueName(default) = %q, want %q", got, "Multica")
	}
	if got := windowsRunValueName("staging"); got != "Multica (staging)" {
		t.Errorf("windowsRunValueName(staging) = %q, want %q", got, "Multica (staging)")
	}
}

func TestLaunchAgentPlistContent(t *testing.T) {
	t.Parallel()

	spec := autostartSpec{
		Exe:     "/opt/homebrew/bin/multica",
		Args:    autostartArgs("staging"),
		PathEnv: "/usr/bin:/opt/homebrew/bin",
	}
	label := launchAgentLabel("staging")
	if label != "ai.multica.daemon.staging" {
		t.Fatalf("launchAgentLabel(staging) = %q", label)
	}
	if got := launchAgentLabel(""); got != "ai.multica.daemon" {
		t.Fatalf("launchAgentLabel(default) = %q", got)
	}

	content := launchAgentPlistContent(spec, label)
	for _, want := range []string{
		// Ownership marker: refresh and enable/disable refuse the file
		// without it, so it has to be there in everything we write.
		"<key>" + autostartPlistMarkerKey + "</key>",
		"<string>" + autostartPlistMarkerValue + "</string>",
		"<string>ai.multica.daemon.staging</string>",
		"<key>RunAtLoad</key>",
		"<key>ProgramArguments</key>",
		"<string>/opt/homebrew/bin/multica</string>",
		"<string>--foreground</string>",
		"<string>--profile</string>",
		"<string>staging</string>",
		"<key>PATH</key>",
		"<string>/usr/bin:/opt/homebrew/bin</string>",
		"<key>ProcessType</key>",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing %q:\n%s", want, content)
		}
	}
	// No KeepAlive: it would restart a daemon the user (or launchd at
	// logout) just stopped.
	if strings.Contains(content, "KeepAlive") {
		t.Errorf("plist must not set KeepAlive:\n%s", content)
	}

	if !launchAgentManaged([]byte(content)) {
		t.Errorf("launchAgentManaged() = false for a plist we just wrote")
	}
	if launchAgentManaged([]byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>x</string></dict></plist>`)) {
		t.Errorf("launchAgentManaged() = true for a plist without our marker")
	}

	// status must be able to read the argv back out of the written file.
	if got, want := launchAgentStoredCommand([]byte(content)),
		"/opt/homebrew/bin/multica daemon start --foreground --profile staging"; got != want {
		t.Errorf("launchAgentStoredCommand = %q, want %q", got, want)
	}
}

func TestLaunchAgentPlistContentEscapesXML(t *testing.T) {
	t.Parallel()

	spec := autostartSpec{Exe: "/opt/a&b/multica", Args: autostartArgs("")}
	content := launchAgentPlistContent(spec, launchAgentLabel(""))
	if !strings.Contains(content, "/opt/a&amp;b/multica") {
		t.Errorf("plist must XML-escape the executable path:\n%s", content)
	}
	if got, want := launchAgentStoredCommand([]byte(content)), "/opt/a&b/multica daemon start --foreground"; got != want {
		t.Errorf("launchAgentStoredCommand = %q, want the unescaped argv back", got)
	}
}

func TestSystemdUnitContent(t *testing.T) {
	t.Parallel()

	spec := autostartSpec{
		Exe:     "/usr/local/bin/multica",
		Args:    autostartArgs("staging"),
		PathEnv: "/home/u/.local/bin:/usr/bin",
	}
	content := systemdUnitContent(spec)

	for _, want := range []string{
		// Ownership marker on the first line.
		autostartManagedComment,
		"ExecStart=/usr/local/bin/multica daemon start --foreground --profile staging",
		`Environment="PATH=/home/u/.local/bin:/usr/bin"`,
		"Restart=on-failure",
		"RestartSec=10",
		// The binary-update handoff contract: runDaemonForeground exits with
		// this status under our unit and systemd must restart the new binary.
		"RestartForceExitStatus=" + strconv.Itoa(daemonSystemdHandoffExitStatus),
		// Bounded retries: enough for a boot racing the network, few enough
		// that a persistent failure stops instead of spinning.
		"StartLimitIntervalSec=120",
		"StartLimitBurst=5",
		// The enable hook links default.target.wants — keep them in step.
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("unit missing %q:\n%s", want, content)
		}
	}
	if !autostartCommentMarked(content) {
		t.Errorf("autostartCommentMarked() = false for a unit we just wrote")
	}
	if autostartCommentMarked("Description=Some user unit\nExecStart=/other\n") {
		t.Errorf("autostartCommentMarked() = true for a foreign unit")
	}

	if got := systemdStoredCommand(content); !strings.HasPrefix(got, "/usr/local/bin/multica daemon start") {
		t.Errorf("systemdStoredCommand = %q, want the ExecStart command", got)
	}
}

func TestSystemdExecArgEscaping(t *testing.T) {
	t.Parallel()

	cases := []struct{ in, want string }{
		{"daemon", "daemon"},
		// '%' is a systemd specifier and must be doubled everywhere.
		{"/a%b", "/a%%b"},
		{"a b", `"a b"`},
		{"", `""`},
		{`a"b`, `"a\"b"`},
	}
	for _, c := range cases {
		if got := systemdExecArg(c.in); got != c.want {
			t.Errorf("systemdExecArg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestXdgAutostartContent(t *testing.T) {
	t.Parallel()

	spec := autostartSpec{
		Exe:     "/home/u/bin/multica",
		Args:    autostartArgs(""),
		PathEnv: "/home/u/.local/bin:/usr/bin",
	}
	content := xdgAutostartContent(spec)

	for _, want := range []string{
		"[Desktop Entry]",
		autostartManagedComment,
		"Type=Application",
		// Autostart only — it must not show up as an application.
		"NoDisplay=true",
		"Exec=env PATH=/home/u/.local/bin:/usr/bin /home/u/bin/multica daemon start --foreground",
		"Terminal=false",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("desktop entry missing %q:\n%s", want, content)
		}
	}
	if !autostartCommentMarked(content) {
		t.Errorf("autostartCommentMarked() = false for a .desktop we just wrote")
	}

	if got := xdgStoredCommand(content); !strings.HasPrefix(got, "env PATH=") {
		t.Errorf("xdgStoredCommand = %q, want the Exec line", got)
	}

	// '%' in PATH would be read as a desktop-entry field code unescaped.
	spec.PathEnv = "/a%b"
	content = xdgAutostartContent(spec)
	if !strings.Contains(content, "PATH=/a%%b") {
		t.Errorf("desktop entry must double '%%' in PATH:\n%s", content)
	}
}

// stubAutostartPlatform replaces the platform seams for the duration of the
// test so no registry / LaunchAgents / systemd state is touched.
type stubAutostartPlatform struct {
	supported      bool
	refreshAllowed bool
	write          func(profile string, spec autostartSpec) (autostartState, bool, error)
	remove         func(profile string) (autostartState, bool, error)
	read           func(profile string) (autostartState, error)
}

func stubPlatform(t *testing.T, s stubAutostartPlatform) {
	t.Helper()
	origSupported := autostartSupported
	origWrite := writeAutostart
	origRemove := removeAutostart
	origRead := readAutostart
	origRefreshAllowed := autostartRefreshAllowed
	autostartSupported = func() bool { return s.supported }
	autostartRefreshAllowed = func(string) bool { return s.refreshAllowed }
	if s.write != nil {
		writeAutostart = s.write
	}
	if s.remove != nil {
		removeAutostart = s.remove
	}
	if s.read != nil {
		readAutostart = s.read
	}
	t.Cleanup(func() {
		autostartSupported = origSupported
		writeAutostart = origWrite
		removeAutostart = origRemove
		readAutostart = origRead
		autostartRefreshAllowed = origRefreshAllowed
	})
}

func TestShouldManageAutostart(t *testing.T) {
	t.Run("plain human starts are managed", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{supported: true})
		if !shouldManageAutostart() {
			t.Fatal("shouldManageAutostart() = false, want true for a plain human start")
		}
	})

	t.Run("manager-launched daemons are left to their manager", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "desktop")
		stubPlatform(t, stubAutostartPlatform{supported: true})
		if shouldManageAutostart() {
			t.Fatal("shouldManageAutostart() = true, want false when a manager owns the daemon")
		}
	})

	t.Run("unsupported platform skips", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{supported: false})
		if shouldManageAutostart() {
			t.Fatal("shouldManageAutostart() = true, want false on an unsupported platform")
		}
	})
}

// TestSyncDaemonAutostart pins the opt-in contract of `daemon start`: it may
// hint and it may heal an owned entry — it must never create one, and it
// must never touch a foreign file or refresh under an external supervisor.
func TestSyncDaemonAutostart(t *testing.T) {
	t.Run("nothing registered prints the hint and writes nothing", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		writes := 0
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{Mechanism: autostartMechanismWindowsRun}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				writes++
				return autostartState{}, false, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", true)
		out := sc.read()
		if writes != 0 {
			t.Fatalf("writeAutostart calls = %d, want 0 — only 'enable' may create", writes)
		}
		if !strings.Contains(out, "multica daemon autostart enable") {
			t.Errorf("stderr = %q, want the enable hint", out)
		}
	})

	t.Run("unregistered but unannounced stays silent", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{}, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", false)
		if out := sc.read(); out != "" {
			t.Errorf("stderr = %q, want silence when announce is false", out)
		}
	})

	t.Run("owned entry is healed silently", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		var wroteArgs []string
		stubPlatform(t, stubAutostartPlatform{
			supported:      true,
			refreshAllowed: true,
			read: func(string) (autostartState, error) {
				return autostartState{Enabled: true, Managed: true}, nil
			},
			write: func(_ string, spec autostartSpec) (autostartState, bool, error) {
				wroteArgs = spec.Args
				return autostartState{Enabled: true, Managed: true}, false, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", true)
		out := sc.read()
		if strings.Join(wroteArgs, " ") != "daemon start --foreground --profile staging" {
			t.Fatalf("refresh args = %q, want the profile's foreground command", wroteArgs)
		}
		if out != "" {
			t.Errorf("stderr = %q, a healing refresh must stay silent", out)
		}
	})

	t.Run("external supervisor blocks the refresh", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		writes := 0
		stubPlatform(t, stubAutostartPlatform{
			supported:      true,
			refreshAllowed: false,
			read: func(string) (autostartState, error) {
				return autostartState{Enabled: true, Managed: true}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				writes++
				return autostartState{}, false, nil
			},
		})
		syncDaemonAutostart("staging", true)
		if writes != 0 {
			t.Fatalf("writeAutostart calls = %d, want 0 under an external supervisor", writes)
		}
	})

	t.Run("foreign entry is never rewritten and gets no hint", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		writes := 0
		stubPlatform(t, stubAutostartPlatform{
			supported:      true,
			refreshAllowed: true,
			read: func(string) (autostartState, error) {
				return autostartState{Present: true, Enabled: true, Managed: false}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				writes++
				return autostartState{}, false, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", true)
		out := sc.read()
		if writes != 0 {
			t.Fatalf("writeAutostart calls = %d, want 0 for a file Multica does not own", writes)
		}
		if out != "" {
			t.Errorf("stderr = %q, want silence — it IS registered, just not ours", out)
		}
	})

	t.Run("present but unlinked foreign entry stays silent too", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		writes := 0
		stubPlatform(t, stubAutostartPlatform{
			supported:      true,
			refreshAllowed: true,
			read: func(string) (autostartState, error) {
				// The review's Linux shape: file at our path, not linked, not ours.
				return autostartState{Present: true, Enabled: false, Managed: false}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				writes++
				return autostartState{}, false, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", true)
		out := sc.read()
		if writes != 0 || out != "" {
			t.Fatalf("writes=%d stderr=%q, want neither a rewrite nor an enable-hint for a foreign unlinked file", writes, out)
		}
	})

	t.Run("owned but unlinked entry gets the hint, never a silent heal", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		writes := 0
		stubPlatform(t, stubAutostartPlatform{
			supported:      true,
			refreshAllowed: true,
			read: func(string) (autostartState, error) {
				// e.g. the user ran `systemctl disable` on OUR unit: the hint
				// may point at enable, but a plain `daemon start` must not
				// re-link an entry the user deliberately turned off.
				return autostartState{Present: true, Enabled: false, Managed: true}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				writes++
				return autostartState{}, false, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("staging", true)
		out := sc.read()
		if writes != 0 {
			t.Fatalf("writeAutostart calls = %d, want 0 — refresh must not re-enable a disabled entry", writes)
		}
		if !strings.Contains(out, "multica daemon autostart enable") {
			t.Errorf("stderr = %q, want the enable hint for an owned-but-unlinked entry", out)
		}
	})

	t.Run("manager-launched daemons get nothing", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "desktop")
		reads := 0
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				reads++
				return autostartState{}, nil
			},
		})
		sc := captureStderr(t)
		syncDaemonAutostart("", true)
		out := sc.read()
		if reads != 0 || out != "" {
			t.Fatalf("reads=%d stderr=%q, want no interaction at all", reads, out)
		}
	})
}

func TestRunDaemonAutostartEnableDisableStatus(t *testing.T) {
	t.Run("enable registers and reports the PATH-only caveat", func(t *testing.T) {
		mkProfiles(t, "staging")
		var gotProfile string
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{Mechanism: autostartMechanismLaunchd}, nil
			},
			write: func(profile string, spec autostartSpec) (autostartState, bool, error) {
				gotProfile = profile
				return autostartState{
					Enabled:   true,
					Managed:   true,
					Mechanism: autostartMechanismLaunchd,
					Location:  "/tmp/agent.plist",
				}, true, nil
			},
		})

		sc := captureStderr(t)
		err := runDaemonAutostartEnable(autostartCmdFor(t, "staging", ""), nil)
		out := sc.read()
		if err != nil {
			t.Fatalf("runDaemonAutostartEnable = %v", err)
		}
		if gotProfile != "staging" {
			t.Errorf("registered profile = %q, want staging", gotProfile)
		}
		for _, want := range []string{
			"Boot autostart enabled", "staging", "launchd LaunchAgent", "/tmp/agent.plist",
			// The review's requirement: spell out what does NOT survive into
			// the login session, at the moment of registration.
			"only PATH is carried",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("enable output %q missing %q", out, want)
			}
		}
	})

	t.Run("enable is idempotent in its wording", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{Mechanism: autostartMechanismWindowsRun}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				return autostartState{Enabled: true, Managed: true, Mechanism: autostartMechanismWindowsRun}, false, nil
			},
		})
		sc := captureStderr(t)
		err := runDaemonAutostartEnable(autostartCmdFor(t, "", ""), nil)
		out := sc.read()
		if err != nil {
			t.Fatalf("runDaemonAutostartEnable = %v", err)
		}
		if !strings.Contains(out, "already enabled") {
			t.Errorf("enable output = %q, want 'already enabled' when nothing changed", out)
		}
	})

	t.Run("enable refuses to overwrite a file Multica does not own", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Present:   true,
					Enabled:   true,
					Managed:   false,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon.service",
				}, nil
			},
			write: func(string, autostartSpec) (autostartState, bool, error) {
				t.Fatal("writeAutostart must not run for an unmanaged file")
				return autostartState{}, false, nil
			},
		})
		err := runDaemonAutostartEnable(autostartCmdFor(t, "", ""), nil)
		if err == nil || !strings.Contains(err.Error(), "not created by Multica") {
			t.Fatalf("runDaemonAutostartEnable = %v, want a refusal naming the foreign file", err)
		}
	})

	t.Run("enable rejects an unknown profile", func(t *testing.T) {
		mkProfiles(t, "known")
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			write: func(string, autostartSpec) (autostartState, bool, error) {
				t.Fatal("writeAutostart must not run for an unknown profile")
				return autostartState{}, false, nil
			},
		})
		err := runDaemonAutostartEnable(autostartCmdFor(t, "nope", ""), nil)
		var unknown *unknownProfileError
		if !errors.As(err, &unknown) {
			t.Fatalf("runDaemonAutostartEnable = %v, want an unknown-profile error", err)
		}
	})

	t.Run("disable removes and reports", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{Enabled: true, Managed: true, Mechanism: autostartMechanismSystemd}, nil
			},
			remove: func(string) (autostartState, bool, error) {
				return autostartState{Mechanism: autostartMechanismSystemd}, true, nil
			},
		})
		sc := captureStderr(t)
		err := runDaemonAutostartDisable(autostartCmdFor(t, "", ""), nil)
		out := sc.read()
		if err != nil {
			t.Fatalf("runDaemonAutostartDisable = %v", err)
		}
		if !strings.Contains(out, "Boot autostart disabled") {
			t.Errorf("disable output = %q, want the disabled confirmation", out)
		}
	})

	t.Run("disable refuses to remove a file Multica does not own", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Present:   true,
					Enabled:   true,
					Managed:   false,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon.service",
				}, nil
			},
			remove: func(string) (autostartState, bool, error) {
				t.Fatal("removeAutostart must not run for an unmanaged file")
				return autostartState{}, false, nil
			},
		})
		err := runDaemonAutostartDisable(autostartCmdFor(t, "", ""), nil)
		if err == nil || !strings.Contains(err.Error(), "not created by Multica") {
			t.Fatalf("runDaemonAutostartDisable = %v, want a refusal naming the foreign file", err)
		}
	})

	// The review's Linux gap: Enabled only means "linked in
	// default.target.wants", so a hand-written unit that is not enabled (or
	// enabled under another target) reads disabled — guarding on Enabled let
	// `disable` delete it. Presence, not enablement, must drive the refusal.
	t.Run("disable refuses an unlinked file Multica does not own", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Present:   true,
					Enabled:   false,
					Managed:   false,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon.service",
				}, nil
			},
			remove: func(string) (autostartState, bool, error) {
				t.Fatal("removeAutostart must not run for an unmanaged, unlinked file")
				return autostartState{}, false, nil
			},
		})
		err := runDaemonAutostartDisable(autostartCmdFor(t, "", ""), nil)
		if err == nil || !strings.Contains(err.Error(), "not created by Multica") {
			t.Fatalf("runDaemonAutostartDisable = %v, want a refusal for the present-but-unlinked foreign file", err)
		}
	})

	t.Run("disable of a never-enabled profile says so without failing", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{Mechanism: autostartMechanismWindowsRun}, nil
			},
			remove: func(string) (autostartState, bool, error) {
				return autostartState{Mechanism: autostartMechanismWindowsRun}, false, nil
			},
		})
		sc := captureStderr(t)
		err := runDaemonAutostartDisable(autostartCmdFor(t, "", ""), nil)
		out := sc.read()
		if err != nil {
			t.Fatalf("runDaemonAutostartDisable = %v, want nil for an idempotent cleanup", err)
		}
		if !strings.Contains(out, "not enabled") {
			t.Errorf("disable output = %q, want 'not enabled'", out)
		}
	})

	t.Run("status renders json", func(t *testing.T) {
		mkProfiles(t, "staging")
		stubPlatform(t, stubAutostartPlatform{
			read: func(string) (autostartState, error) {
				return autostartState{
					Enabled:   true,
					Managed:   true,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon-staging.service",
					Command:   "/usr/local/bin/multica daemon start --foreground --profile staging",
				}, nil
			},
		})
		out, err := captureStdout(t, func() error {
			return runDaemonAutostartStatus(daemonStatusCmdFor(t, "staging", "json"), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonAutostartStatus = %v", err)
		}
		// PrintJSON pretty-prints with a space after the colon; assert on
		// the rendered shape rather than a compact encoding we do not own.
		for _, want := range []string{`"profile": "staging"`, `"enabled": true`, `"mechanism": "systemd"`, `"command": "`} {
			if !strings.Contains(out, want) {
				t.Errorf("status json = %s, want it to contain %s", out, want)
			}
		}
	})

	t.Run("status renders table", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Enabled:   true,
					Managed:   true,
					Mechanism: autostartMechanismWindowsRun,
					Location:  `HKCU\Run\Multica`,
				}, nil
			},
		})
		out, err := captureStdout(t, func() error {
			return runDaemonAutostartStatus(daemonStatusCmdFor(t, "", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonAutostartStatus = %v", err)
		}
		for _, want := range []string{"Profile", "enabled", "Windows Run key", `HKCU\Run\Multica`} {
			if !strings.Contains(out, want) {
				t.Errorf("status table = %q, want it to contain %q", out, want)
			}
		}
	})

	t.Run("status flags an entry Multica does not own", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Present:   true,
					Enabled:   true,
					Managed:   false,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon.service",
				}, nil
			},
		})
		out, err := captureStdout(t, func() error {
			return runDaemonAutostartStatus(daemonStatusCmdFor(t, "", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonAutostartStatus = %v", err)
		}
		if !strings.Contains(out, "not created by Multica") {
			t.Errorf("status = %q, want it to flag the unmanaged entry", out)
		}
	})

	// Present-but-unlinked (or linked under another target): status must
	// still say whose file it is even though the verdict line says disabled.
	t.Run("status flags an unlinked entry Multica does not own", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			read: func(string) (autostartState, error) {
				return autostartState{
					Present:   true,
					Enabled:   false,
					Managed:   false,
					Mechanism: autostartMechanismSystemd,
					Location:  "/home/u/.config/systemd/user/multica-daemon.service",
				}, nil
			},
		})
		out, err := captureStdout(t, func() error {
			return runDaemonAutostartStatus(daemonStatusCmdFor(t, "", ""), nil)
		})
		if err != nil {
			t.Fatalf("runDaemonAutostartStatus = %v", err)
		}
		if !strings.Contains(out, "disabled") {
			t.Errorf("status = %q, want the enabled/disabled verdict to stay disabled", out)
		}
		if !strings.Contains(out, "not created by Multica") {
			t.Errorf("status = %q, want it to flag the present-but-unlinked unmanaged entry", out)
		}
	})
}

func TestAutostartCommandsRejectUnsupportedPlatform(t *testing.T) {
	mkProfiles(t)
	stubPlatform(t, stubAutostartPlatform{supported: false})

	for name, run := range map[string]func(*cobra.Command, []string) error{
		"enable":  runDaemonAutostartEnable,
		"disable": runDaemonAutostartDisable,
	} {
		err := run(autostartCmdFor(t, "", ""), nil)
		if err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Errorf("autostart %s on an unsupported platform = %v, want a clear refusal", name, err)
		}
	}
}

// TestAutostartSpecForUsesStableBrewPath pins the review's Linuxbrew failure
// mode end to end: os.Executable() reports the versioned keg path on brew
// installs, `brew upgrade` cleans the old keg, and an autostart entry
// recorded with that path would restart a deleted binary (systemd 203/EXEC)
// or point at a missing file at the next login. The spec must carry the
// stable <prefix>/bin/multica symlink instead — resolved through the same
// shared helper as the daemon's restart target so the two cannot drift.
func TestAutostartSpecForUsesStableBrewPath(t *testing.T) {
	orig := daemonExecutable
	daemonExecutable = func() (string, error) {
		return "/opt/homebrew/Cellar/multica/0.4.33/bin/multica", nil
	}
	t.Cleanup(func() { daemonExecutable = orig })

	spec, err := autostartSpecFor("")
	if err != nil {
		t.Fatalf("autostartSpecFor = %v", err)
	}
	want := filepath.Join("/opt/homebrew", "bin", "multica")
	if spec.Exe != want {
		t.Fatalf("autostartSpecFor exe = %q, want the stable brew symlink path %q — a recorded keg path is deleted by the next brew upgrade",
			spec.Exe, want)
	}
	if got, want := strings.Join(spec.Args, " "), "daemon start --foreground"; got != want {
		t.Errorf("autostartSpecFor args = %q, want %q", got, want)
	}
}
