package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// envFrom turns a map into the lookup dshDesktopBundlePathsFor takes, so a test
// can describe a Windows environment without touching the host's.
func envFrom(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

// mkShim writes one generated CLI payload — <dir>/bin — and stamps the payload
// directory, which is what orders candidates within a tier.
func mkShim(t *testing.T, dir string, modTime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return dir
}

// hostCommandsPayload writes the layout confirmed on a real Windows install:
//
//	<appData>\host-commands\<profile>\generations\<hash>-<uuid>\bin\dsh.cmd
func hostCommandsPayload(t *testing.T, appData, profile, generation string, modTime time.Time) string {
	t.Helper()
	return mkShim(t, filepath.Join(appData, "host-commands", profile, "generations", generation), modTime)
}

// cliPayload writes the other shape: <appData>\cli\<hash>\bin.
func cliPayload(t *testing.T, appData, hash string, modTime time.Time) string {
	t.Helper()
	return mkShim(t, filepath.Join(appData, "cli", hash), modTime)
}

// The layout that actually works on Windows. The shim sets DSH_HOME and calls
// the Desktop executable by absolute path, so it needs nothing on PATH — which
// is why it is preferred over every other candidate.
func TestDshDesktopShimCandidates_HostCommandsLayout(t *testing.T) {
	appData := t.TempDir()
	gen := "0d077d22abfd9f6817de376152441a787680be20b8d5a17350390d72570c17ce-a67d0d81-d10b-4bbe-b1d1-2ac2eb7fcbce"
	dir := hostCommandsPayload(t, appData, "desktop", gen, time.Now())

	got := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows"))
	want := []string{
		filepath.Join(dir, "bin", "dsh.exe"),
		filepath.Join(dir, "bin", "dsh.cmd"),
		filepath.Join(dir, "bin", "dsh.bat"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidates =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The tier order is load-bearing, not cosmetic. Nothing here executes a
// candidate, so a broken-but-present shim is indistinguishable from a working
// one at this layer — and on the install that prompted this, the cli shim was
// exactly that: present, resolvable by exec.LookPath, and exiting 9009. Ranking
// host-commands first is the only thing that keeps it from winning.
func TestDshDesktopShimCandidates_HostCommandsOutranksCLIEvenWhenOlder(t *testing.T) {
	appData := t.TempDir()
	// The cli payload is NEWER, so only the tier can decide this.
	hostCommandsPayload(t, appData, "desktop", "host-gen", time.Now().Add(-72*time.Hour))
	cliPayload(t, appData, "cli-hash", time.Now())

	got := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows"))
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	if !strings.Contains(got[0], filepath.Join("host-commands", "desktop")) {
		t.Fatalf("first candidate = %q, want the host-commands shim", got[0])
	}
	// The cli payload still gets a turn: on a Desktop version that generates no
	// host-commands it is the only shim there is.
	if !strings.Contains(strings.Join(got, "\n"), filepath.Join("cli", "cli-hash")) {
		t.Error("the cli payload was dropped entirely rather than ranked lower")
	}
}

// The profile level belongs to the user, so it is enumerated rather than
// assumed to be "desktop".
func TestDshDesktopShimCandidates_EnumeratesEveryProfile(t *testing.T) {
	appData := t.TempDir()
	hostCommandsPayload(t, appData, "desktop", "gen-old", time.Now().Add(-time.Hour))
	newest := hostCommandsPayload(t, appData, "some-other-profile", "gen-new", time.Now())

	got := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows"))
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	if !strings.HasPrefix(got[0], newest) {
		t.Fatalf("first candidate = %q, want the newest generation %q regardless of profile", got[0], newest)
	}
	if !strings.Contains(strings.Join(got, "\n"), "desktop") {
		t.Error("a profile other than the newest was dropped")
	}
}

// An upgrade writes a new generation and can leave the old one behind. The
// newest is the one the app is driving; offering an abandoned generation first
// would register a CLI the user has already replaced.
func TestDshDesktopShimCandidates_NewestGenerationWins(t *testing.T) {
	appData := t.TempDir()
	hostCommandsPayload(t, appData, "desktop", "aaaa-old", time.Now().Add(-72*time.Hour))
	current := hostCommandsPayload(t, appData, "desktop", "bbbb-current", time.Now())

	got := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows"))
	if len(got) == 0 || !strings.HasPrefix(got[0], current) {
		t.Fatalf("first candidate = %v, want the newest generation %q", got, current)
	}
}

// Two payloads can share a filesystem timestamp tick. ReadDir order is not a
// promise, so the name has to break the tie or the daemon would pick a
// different CLI on different rounds.
func TestDshDesktopShimCandidates_TieBreakIsStable(t *testing.T) {
	appData := t.TempDir()
	stamp := time.Now().Add(-time.Hour)
	for _, gen := range []string{"aaaa", "cccc", "bbbb"} {
		hostCommandsPayload(t, appData, "desktop", gen, stamp)
	}

	first := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows"))
	for i := 0; i < 5; i++ {
		if again := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows")); strings.Join(again, "\n") != strings.Join(first, "\n") {
			t.Fatalf("order changed between calls:\n%v\nvs\n%v", first, again)
		}
	}
	if !strings.Contains(first[0], "cccc") {
		t.Errorf("first candidate = %q, want the name tie-break to pick cccc", first[0])
	}
}

// A stray file, a payload with no bin directory (a half-extracted download), a
// profile with no generations directory, and a missing app-data root are all
// "nothing to offer" rather than a candidate that fails at spawn time.
func TestDshDesktopShimCandidates_SkipsWhatCannotHoldACLI(t *testing.T) {
	appData := t.TempDir()
	if err := os.MkdirAll(filepath.Join(appData, "cli", "half-extracted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(appData, "host-commands", "desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appData, "cli", "manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := dshDesktopShimCandidates(appData, dshDesktopShimNames("windows")); len(got) != 0 {
		t.Fatalf("candidates = %v, want none", got)
	}
	if got := dshDesktopShimCandidates(filepath.Join(appData, "nope"), dshDesktopShimNames("windows")); len(got) != 0 {
		t.Fatalf("candidates = %v for a missing root, want none", got)
	}
	if got := dshDesktopShimCandidates("", dshDesktopShimNames("windows")); len(got) != 0 {
		t.Fatalf("candidates = %v for an empty root, want none", got)
	}
}

// %APPDATA% is redirected on managed and roaming-profile machines, so the
// environment wins; composing from home is the last resort. macOS has no such
// variable and is always home-relative.
func TestDshDesktopAppDataDir(t *testing.T) {
	fromEnv := dshDesktopAppDataDir("windows", envFrom(map[string]string{
		"APPDATA": `D:\Profiles\jane\Roaming`,
	}), `C:\Users\jane`)
	if want := filepath.Join(`D:\Profiles\jane\Roaming`, "DSH Desktop"); fromEnv != want {
		t.Errorf("windows dir = %q, want %q", fromEnv, want)
	}

	fromHome := dshDesktopAppDataDir("windows", envFrom(nil), `C:\Users\jane`)
	if want := filepath.Join(`C:\Users\jane`, "AppData", "Roaming", "DSH Desktop"); fromHome != want {
		t.Errorf("windows dir = %q, want %q", fromHome, want)
	}

	mac := dshDesktopAppDataDir("darwin", envFrom(nil), "/Users/jane")
	if want := filepath.Join("/Users/jane", "Library", "Application Support", "DSH Desktop"); mac != want {
		t.Errorf("darwin dir = %q, want %q", mac, want)
	}

	// Nothing to name, and inventing a directory would make every candidate
	// below it a false path.
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if got := dshDesktopAppDataDir(goos, envFrom(nil), ""); got != "" {
			t.Errorf("%s dir = %q with no environment and no home, want empty", goos, got)
		}
	}
}

// The macOS app-bundle entry point is a Node script started through its
// shebang, so it needs node on PATH. It stays as a candidate — it works on a
// machine that has node — but it must never outrank a self-contained shim, and
// it must never appear on Windows, where nothing can start a .js.
func TestDshDesktopBundlePathsFor_ShimOutranksTheNodeScript(t *testing.T) {
	home := t.TempDir()
	appData := filepath.Join(home, "Library", "Application Support", "DSH Desktop")
	shim := hostCommandsPayload(t, appData, "desktop", "gen", time.Now())

	got := dshDesktopBundlePathsFor("darwin", envFrom(nil), home)
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	if !strings.HasPrefix(got[0], shim) {
		t.Fatalf("first candidate = %q, want the self-contained shim %q", got[0], shim)
	}
	last := got[len(got)-1]
	if !strings.HasSuffix(last, "bin.js") {
		t.Errorf("last candidate = %q, want the app-bundle script ranked last", last)
	}

	// Windows never offers it: exec.LookPath rejects a .js unless PATHEXT says
	// otherwise, and CreateProcess cannot start one either.
	for _, path := range dshDesktopBundlePathsFor("windows", envFrom(map[string]string{"APPDATA": appData}), home) {
		if strings.HasSuffix(path, ".js") {
			t.Errorf("windows candidate %q cannot be spawned there", path)
		}
	}
}

// The pnpm `dsh plugin` forwards to lives under runtime-commands in the app's
// own data directory — and the two platforms disagree about the shape. macOS
// 2.0.5 keeps a flat runtime-commands/bin; a Windows install keeps
// runtime-commands/generations/<id>/bin. Composing either path would have left
// the other reporting "pnpm not found on PATH" on exactly the machines the
// automatic install exists for, so both are searched.
func TestDshPluginPathDirs_FindsBothLayouts(t *testing.T) {
	t.Setenv(dshPluginPathEnv, "")

	t.Run("generational", func(t *testing.T) {
		home := t.TempDir()
		appData := dshDesktopAppDataDir(runtime.GOOS, os.Getenv, home)
		if appData == "" {
			t.Skip("no app-data location on this platform")
		}
		want := mkShim(t, filepath.Join(appData, "runtime-commands", "generations", "gen-1"), time.Now())
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		got := dshPluginPathDirs()
		if len(got) == 0 || got[0] != filepath.Join(want, "bin") {
			t.Fatalf("dirs = %v, want %q first", got, filepath.Join(want, "bin"))
		}
	})

	t.Run("flat", func(t *testing.T) {
		home := t.TempDir()
		appData := dshDesktopAppDataDir(runtime.GOOS, os.Getenv, home)
		if appData == "" {
			t.Skip("no app-data location on this platform")
		}
		flat := filepath.Join(appData, "runtime-commands", "bin")
		if err := os.MkdirAll(flat, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		got := dshPluginPathDirs()
		if len(got) != 1 || got[0] != flat {
			t.Fatalf("dirs = %v, want exactly %q", got, flat)
		}
	})
}

// An explicit override is the whole answer. An operator who pinned a directory
// that does not exist is trying to bypass the bundled pnpm, and silently
// falling back to it would defeat that — they should get "pnpm not found".
func TestDshPluginPathDirs_OverrideWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(dshPluginPathEnv, dir)
	if got := dshPluginPathDirs(); len(got) != 1 || got[0] != dir {
		t.Fatalf("dirs = %v, want exactly %q", got, dir)
	}

	t.Setenv(dshPluginPathEnv, filepath.Join(dir, "does-not-exist"))
	if got := dshPluginPathDirs(); len(got) != 0 {
		t.Fatalf("dirs = %v, want none for an override that does not exist", got)
	}
}

// An upgrade writes a new generation and can leave the old one behind; the
// newest is the one the app is driving. The flat directory ranks last, because
// a host holding both is one where the generational layout is the newer.
func TestDshDesktopBinDirs_Ordering(t *testing.T) {
	root := t.TempDir()
	mkShim(t, filepath.Join(root, "old"), time.Now().Add(-72*time.Hour))
	current := mkShim(t, filepath.Join(root, "current"), time.Now())
	flat := filepath.Join(root, "bin")
	if err := os.MkdirAll(flat, 0o755); err != nil {
		t.Fatal(err)
	}

	got := dshDesktopBinDirs(root)
	want := []string{
		filepath.Join(current, "bin"),
		filepath.Join(root, "old", "bin"),
		flat,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("dirs =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// A payload with no bin, a stray file, and a missing root offer nothing.
	if err := os.MkdirAll(filepath.Join(root, "half-extracted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if len(dshDesktopBinDirs(root)) != 3 {
		t.Errorf("a payload without a bin directory was offered: %v", dshDesktopBinDirs(root))
	}
	if got := dshDesktopBinDirs(filepath.Join(root, "nope")); len(got) != 0 {
		t.Errorf("dirs = %v for a missing root, want none", got)
	}
	if got := dshDesktopBinDirs(""); len(got) != 0 {
		t.Errorf("dirs = %v for an empty root, want none", got)
	}
}

// macOS keeps its app-bundle candidates: system-wide /Applications before the
// per-user ~/Applications, and only the first when there is no home.
func TestDshDesktopBundlePathsFor_DarwinAppBundle(t *testing.T) {
	const bundle = "DSH Desktop.app/Contents/Resources/app.asar.unpacked/node_modules/@deepseek-ai/dsh/lib/bin.js"
	got := dshDesktopBundlePathsFor("darwin", envFrom(nil), "/nonexistent-home")

	want := []string{
		filepath.Join("/Applications", bundle),
		filepath.Join("/nonexistent-home", "Applications", bundle),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("candidates =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if paths := dshDesktopBundlePathsFor("darwin", envFrom(nil), ""); len(paths) != 1 {
		t.Errorf("with no home, want only the /Applications candidate, got %v", paths)
	}
}

// Linux ships no DSH Desktop, and a candidate list for a platform with no known
// install location can only produce false positives.
func TestDshDesktopBundlePathsFor_UnknownPlatform(t *testing.T) {
	if got := dshDesktopBundlePathsFor("linux", envFrom(nil), "/home/jane"); len(got) != 0 {
		t.Fatalf("candidates = %v, want none", got)
	}
}
