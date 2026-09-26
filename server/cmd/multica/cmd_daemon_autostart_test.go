package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// autostartCmdFor builds a command carrying the flags the autostart commands
// read (profile, output) plus the opt-out flag `daemon start` consults.
func autostartCmdFor(t *testing.T, profile, output string) *cobra.Command {
	t.Helper()
	cmd := daemonStatusCmdFor(t, profile, output)
	cmd.Flags().Bool("no-autostart", false, "")
	return cmd
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
		"ExecStart=/usr/local/bin/multica daemon start --foreground --profile staging",
		`Environment="PATH=/home/u/.local/bin:/usr/bin"`,
		"Restart=on-failure",
		"RestartSec=10",
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
	supported bool
	write     func(profile string, spec autostartSpec) (autostartState, bool, error)
	remove    func(profile string) (autostartState, bool, error)
	read      func(profile string) (autostartState, error)
}

func stubPlatform(t *testing.T, s stubAutostartPlatform) {
	t.Helper()
	origSupported := autostartSupported
	origWrite := writeAutostart
	origRemove := removeAutostart
	origRead := readAutostart
	autostartSupported = func() bool { return s.supported }
	writeAutostart = s.write
	removeAutostart = s.remove
	readAutostart = s.read
	t.Cleanup(func() {
		autostartSupported = origSupported
		writeAutostart = origWrite
		removeAutostart = origRemove
		readAutostart = origRead
	})
}

func TestShouldRegisterAutostart(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().Bool("no-autostart", false, "")
		return cmd
	}

	t.Run("registers by default", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{supported: true})
		if !shouldRegisterAutostart(newCmd()) {
			t.Fatal("shouldRegisterAutostart() = false, want true for a plain human start")
		}
	})

	t.Run("--no-autostart skips", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{supported: true})
		cmd := newCmd()
		if err := cmd.Flags().Set("no-autostart", "true"); err != nil {
			t.Fatalf("set flag: %v", err)
		}
		if shouldRegisterAutostart(cmd) {
			t.Fatal("shouldRegisterAutostart() = true, want false under --no-autostart")
		}
	})

	t.Run("manager-launched daemons are left to their manager", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "desktop")
		stubPlatform(t, stubAutostartPlatform{supported: true})
		if shouldRegisterAutostart(newCmd()) {
			t.Fatal("shouldRegisterAutostart() = true, want false when a manager owns the daemon")
		}
	})

	t.Run("unsupported platform skips", func(t *testing.T) {
		t.Setenv("MULTICA_LAUNCHED_BY", "")
		stubPlatform(t, stubAutostartPlatform{supported: false})
		if shouldRegisterAutostart(newCmd()) {
			t.Fatal("shouldRegisterAutostart() = true, want false on an unsupported platform")
		}
	})
}

func TestEnsureDaemonAutostartRegistersAndAnnounces(t *testing.T) {
	var wrote []string
	stubPlatform(t, stubAutostartPlatform{
		supported: true,
		write: func(profile string, spec autostartSpec) (autostartState, bool, error) {
			wrote = append(wrote, profile+"|"+strings.Join(spec.Args, " "))
			return autostartState{Enabled: true, Mechanism: autostartMechanismWindowsRun}, true, nil
		},
	})

	cmd := autostartCmdFor(t, "staging", "")
	sc := captureStderr(t)
	ensureDaemonAutostart(cmd, "staging", true)
	out := sc.read()

	if len(wrote) != 1 || wrote[0] != "staging|daemon start --foreground --profile staging" {
		t.Fatalf("writeAutostart calls = %q, want one registration for the profile", wrote)
	}
	if !strings.Contains(out, "Boot autostart: enabled") {
		t.Errorf("announce output = %q, want the boot-autostart line", out)
	}

	// Silent mode (the foreground path under a supervisor) still registers.
	wrote = nil
	sc = captureStderr(t)
	ensureDaemonAutostart(cmd, "staging", false)
	out = sc.read()
	if len(wrote) != 1 {
		t.Fatalf("writeAutostart calls = %d, want 1 even when not announcing", len(wrote))
	}
	if out != "" {
		t.Errorf("stderr = %q, want no announcement when announce is false", out)
	}

	// The opt-out must prevent the registration itself, not just silence it.
	wrote = nil
	if err := cmd.Flags().Set("no-autostart", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	sc = captureStderr(t)
	ensureDaemonAutostart(cmd, "staging", true)
	out = sc.read()
	if len(wrote) != 0 || out != "" {
		t.Fatalf("under --no-autostart: writes=%d stderr=%q, want neither", len(wrote), out)
	}
}

func TestRunDaemonAutostartEnableDisableStatus(t *testing.T) {
	t.Run("enable registers and reports", func(t *testing.T) {
		mkProfiles(t, "staging")
		var gotProfile string
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			write: func(profile string, spec autostartSpec) (autostartState, bool, error) {
				gotProfile = profile
				return autostartState{
					Enabled:   true,
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
		for _, want := range []string{"Boot autostart enabled", "staging", "launchd LaunchAgent", "/tmp/agent.plist"} {
			if !strings.Contains(out, want) {
				t.Errorf("enable output %q missing %q", out, want)
			}
		}
	})

	t.Run("enable is idempotent in its wording", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
			write: func(string, autostartSpec) (autostartState, bool, error) {
				return autostartState{Enabled: true, Mechanism: autostartMechanismWindowsRun}, false, nil
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

	t.Run("disable of a never-enabled profile says so without failing", func(t *testing.T) {
		mkProfiles(t)
		stubPlatform(t, stubAutostartPlatform{
			supported: true,
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
		for _, want := range []string{"Profile", "disabled", "Windows Run key", `HKCU\Run\Multica`} {
			if !strings.Contains(out, want) {
				t.Errorf("status table = %q, want it to contain %q", out, want)
			}
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
