package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDshLauncherHome(t *testing.T) {
	pinnedDshHome(t)
	selected := filepath.Join(t.TempDir(), "DSH 中文 home")
	for _, tc := range []struct{ name, script, want string }{
		{"unproven inheritance", "#!/bin/sh\nexec node bin.js \"$@\"\n", ""},
		{"sourced", "#!/bin/sh\n. '/launcher-env'\nexec node bin.js \"$@\"\n", ""},
		{"forwarded", "#!/bin/sh\nexec '/other-launcher' \"$@\"\n", ""},
		{"changed user home", "#!/bin/sh\nexport HOME='/other-user'\nexec node bin.js \"$@\"\n", ""},
		{"batch forwarded", "@echo off\r\ncall other-launcher.cmd %*\r\n", ""},
		{"unix literal", "#!/bin/sh\nexport DSH_HOME='" + selected + "'\nexec node bin.js \"$@\"\n", selected},
		{"batch literal", "@echo off\r\nsetlocal\r\nset \"DSH_HOME=" + selected + "\"\r\nnode bin.js %*\r\n", selected},
		{"dynamic", "#!/bin/sh\nexport DSH_HOME=\"$OTHER_HOME\"\n", ""},
		{"computed", "#!/bin/sh\nexport DSH_HOME=\"$(cat config)\"\n", ""},
		{"conditional", "#!/bin/sh\nif test -d /custom; then\nexport DSH_HOME='/custom'\nfi\n", ""},
		{"unset later", "#!/bin/sh\nexport DSH_HOME='/custom'\nunset DSH_HOME\n", ""},
		{"multiple assignments", "#!/bin/sh\nexport DSH_HOME='/first'\nexport DSH_HOME='/second'\n", ""},
		{"relative", "#!/bin/sh\nexport DSH_HOME='./relative'\n", ""},
		{"opaque executable", "MZ\x00binary", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher := filepath.Join(t.TempDir(), "dsh")
			if err := os.WriteFile(launcher, []byte(tc.script), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := dshLauncherHome(launcher); got != tc.want {
				t.Fatalf("home = %q, want %q", got, tc.want)
			}
		})
	}
	if got := dshMulticaProfileState(filepath.Join(t.TempDir(), "missing")); got != dshProfileUnknown {
		t.Fatalf("unreadable launcher state = %v, want unknown", got)
	}
}

func TestDshIndirectShimProfileLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixtures")
	}
	for _, kind := range []string{"sourced", "evaluated", "forwarded"} {
		t.Run(kind, func(t *testing.T) {
			inherited := pinnedDshHome(t)
			selected := t.TempDir()
			dir := t.TempDir()
			config := filepath.Join(dir, "launcher-env")
			runtimePath := filepath.Join(dir, "runtime")
			launcher := filepath.Join(dir, "dsh")
			body := `
case "$*" in
  plugin*) mkdir -p "$DSH_HOME/profiles/multica"; printf '%s' '{"dependencies":{"@multica-ai/dsh-runtime":"1.0.0"},"dsh":{"profile":{"bundles":["@multica-ai/dsh-runtime"]}}}' > "$DSH_HOME/profiles/multica/package.json" ;;
  *--probe*) if [ -f "$DSH_HOME/profiles/multica/package.json" ] && [ -z "$DSH_TEST_PROBE_FAIL" ]; then
    printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","protocol_version":1}'
    else exit 1; fi ;;
esac
`
			declaration := "export DSH_HOME='" + selected + "'\n"
			var script string
			switch kind {
			case "sourced":
				script = ". '" + config + "'\n" + body
			case "evaluated":
				script = "eval \"$(cat '" + config + "')\"\n" + body
			case "forwarded":
				script = "exec '" + runtimePath + "' \"$@\"\n"
			}
			for path, content := range map[string]string{
				config:      declaration,
				runtimePath: "#!/bin/sh\n" + declaration + body,
				launcher:    "#!/bin/sh\n" + script,
			} {
				if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// Neither an explicit daemon home nor its default ~/.dsh proves
			// where an indirect launcher will install or read a profile.
			for _, daemonHome := range []string{inherited, ""} {
				t.Setenv("DSH_HOME", daemonHome)
				t.Setenv("HOME", t.TempDir())
				if got := dshMulticaProfileState(launcher); got != dshProfileUnknown {
					t.Fatalf("indirect home: %v, want unknown", got)
				}
			}
			t.Setenv("DSH_HOME", inherited)
			t.Setenv("DSH_TEST_PROBE_FAIL", "")
			t.Setenv(dshProfileBundleEnv, "fixture-bundle")
			if err := provisionDshMulticaProfile(context.Background(), launcher, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
				t.Fatalf("install through an indirect launcher: %v", err)
			}
			if _, err := os.Stat(filepath.Join(selected, "profiles", "multica", "package.json")); err != nil {
				t.Fatal(err)
			}
			if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeOK {
				t.Fatalf("installed profile probe: %v, want compatible", got)
			}
			t.Setenv("DSH_TEST_PROBE_FAIL", "1")
			if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeUnavailable {
				t.Fatalf("transient probe failure: %v, want unavailable", got)
			}
			d := &Daemon{
				cfg:          Config{Agents: map[string]AgentEntry{"dsh": {Path: launcher}}},
				workspaces:   map[string]*workspaceState{"ws": {runtimeIDs: []string{"rt"}}},
				runtimeIndex: map[string]Runtime{"rt": {ID: "rt", Provider: "dsh"}},
			}
			if got := d.dshRuntimeProfileMismatch(); got != dshMismatchNone {
				t.Fatalf("unknown home must not force demotion: %v", got)
			}
		})
	}
}

func TestDshShimProfileLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture; batch home resolution is covered separately")
	}
	inherited := pinnedDshHome(t)
	selected := t.TempDir()
	launcher := filepath.Join(t.TempDir(), "dsh")
	script := "#!/bin/sh\nexport DSH_HOME='" + selected + "'\n" + `
case "$*" in
  plugin*) mkdir -p "$DSH_HOME/profiles/multica"; printf '%s' '{"dependencies":{"@multica-ai/dsh-runtime":"1.0.0"},"dsh":{"profile":{"bundles":["@multica-ai/dsh-runtime"]}}}' > "$DSH_HOME/profiles/multica/package.json" ;;
  *--probe*) if [ -f "$DSH_HOME/profiles/multica/package.json" ]; then
    printf '%s\n' '{"v":1,"type":"probe","runtime":"dsh","protocol_version":1}'
    else exit 1; fi ;;
esac
`
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// An unrelated manifest in the daemon home must not hide the missing
	// profile in the home selected by the launcher.
	writeDshProfileManifest(t, inherited, `{"dsh":{"profile":{"bundles":["dsh-profile-multica"]}}}`)
	if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeMissingProfile {
		t.Fatalf("before install: %v, want missing", got)
	}
	t.Setenv(dshProfileBundleEnv, "fixture-bundle")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := provisionDshMulticaProfile(context.Background(), launcher, logger); err != nil {
		t.Fatal(err)
	}
	if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeOK {
		t.Fatalf("after install: %v, want compatible", got)
	}
	d := &Daemon{cfg: Config{Agents: map[string]AgentEntry{"dsh": {Path: launcher}}}}
	if got := d.dshRuntimeProfileMismatch(); got != dshMismatchProfileWithoutRuntime {
		t.Fatalf("installed shim profile must trigger registration: %v", got)
	}
	// A temporary boot error with the selected manifest still on disk must
	// not permit repair, even when the daemon's own home has no manifest.
	pinnedDshHome(t)
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexport DSH_HOME='"+selected+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeUnavailable {
		t.Fatalf("transient boot failure: %v, want unavailable", got)
	}
}

func TestDshDynamicShimHomeStaysUnknown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	selected := t.TempDir()
	t.Setenv("DSH_TEST_SHIM_HOME", selected)
	launcher, _ := fakeDshScript(t, `DSH_HOME="$DSH_TEST_SHIM_HOME"
export DSH_HOME
case "$*" in
  plugin*) mkdir -p "$DSH_HOME/profiles/multica"; printf '%s' '{"dependencies":{"@multica-ai/dsh-runtime":"1.0.0"},"dsh":{"profile":{"bundles":["@multica-ai/dsh-runtime"]}}}' > "$DSH_HOME/profiles/multica/package.json" ;;
  *) exit 1 ;;
esac`)
	t.Setenv(dshProfileBundleEnv, "fixture-bundle")
	if got := dshMulticaProfileState(launcher); got != dshProfileUnknown {
		t.Fatalf("dynamic home: %v, want unknown", got)
	}
	if got := probeDshMulticaProfile(context.Background(), launcher); got != dshProbeUnavailable {
		t.Fatalf("opaque probe: %v, want unavailable", got)
	}
	d := &Daemon{
		cfg:          Config{Agents: map[string]AgentEntry{"dsh": {Path: launcher}}},
		workspaces:   map[string]*workspaceState{"ws": {runtimeIDs: []string{"rt"}}},
		runtimeIndex: map[string]Runtime{"rt": {ID: "rt", Provider: "dsh"}},
	}
	if got := d.dshRuntimeProfileMismatch(); got != dshMismatchNone {
		t.Fatalf("unknown home must not force demotion: %v", got)
	}
	if err := provisionDshMulticaProfile(context.Background(), launcher, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("successful install through an opaque shim must allow re-probing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(selected, "profiles", "multica", "package.json")); err != nil {
		t.Fatal(err)
	}
}

func TestDshUnverifiedInstallWithdrawsWaitAndRetriesDiscovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	t.Setenv("DSH_TEST_SHIM_HOME", t.TempDir())
	launcher, _ := fakeDshScript(t, `export DSH_HOME="$DSH_TEST_SHIM_HOME"`)
	t.Setenv(dshProfileBundleEnv, "fixture-bundle")
	rec, client := newDeregisterRecorder(t)
	d := &Daemon{
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:             client,
		workspaces:         map[string]*workspaceState{"ws": {}},
		agentDiscoveryKick: make(chan struct{}, 1),
		dshInstallWaits: []dshInstallWait{{
			workspaceID: "ws", runtimeID: "rt",
			reason: RuntimeOfflineReason{Code: RuntimeOfflineCodeDshProfile, Installing: true},
		}},
	}
	if !d.startDshProfileProvision(launcher) {
		t.Fatal("install did not start")
	}
	select {
	case <-d.agentDiscoveryKick:
	case <-time.After(10 * time.Second):
		t.Fatal("unverified install did not trigger discovery")
	}
	if reason, ok := rec.reasonFor("rt"); !ok || reason.Installing || reason.Detail != dshInstallGaveUpReason {
		t.Fatalf("completed install still advertises a wait: %+v (reported %v)", reason, ok)
	}
}
