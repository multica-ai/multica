package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeCodexExe(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "codex.exe")
	if err := os.WriteFile(exe, []byte("codex-cli fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return exe
}

func writeCodexUserRuntime(t *testing.T, localAppData, id string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(localAppData, "OpenAI", "Codex", "bin", id)
	exe := writeCodexExe(t, dir)
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return exe
}

func writeCodexMsix(t *testing.T, programFiles, packageName string) string {
	t.Helper()
	dir := filepath.Join(programFiles, "WindowsApps", packageName, "app", "resources")
	return writeCodexExe(t, dir)
}

func TestCodexDesktopBundlePathsFor_LinuxIsEmpty(t *testing.T) {
	if got := codexDesktopBundlePathsFor("linux", envFrom(nil), "/home/jane"); got != nil {
		t.Fatalf("linux candidates = %#v, want nil", got)
	}
}

func TestCodexDesktopBundlePathsFor_DarwinKeepsHistoricalOrder(t *testing.T) {
	got := codexDesktopBundlePathsFor("darwin", envFrom(nil), "/Users/jane")
	wantSuffixes := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
		"/Users/jane/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex",
		"/Users/jane/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Users/jane/Applications/Codex.app/Contents/Resources/codex",
	}
	if len(got) != len(wantSuffixes) {
		t.Fatalf("got %d candidates %#v, want %d", len(got), got, len(wantSuffixes))
	}
	for i, want := range wantSuffixes {
		if filepath.ToSlash(got[i]) != want {
			t.Fatalf("candidate %d = %q, want %q", i, got[i], want)
		}
	}
}

func TestCodexDesktopBundlePathsFor_WindowsFindsUserRuntimeOutsidePATH(t *testing.T) {
	local := t.TempDir()
	exe := writeCodexUserRuntime(t, local, "1cb21d4ee104a51c", time.Now())

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"LOCALAPPDATA": local,
	}), "")
	if len(got) != 1 || got[0] != exe {
		t.Fatalf("candidates = %#v, want [%q]", got, exe)
	}
}

// An upgrade writes a new hash directory and can leave the previous one behind.
// The newest directory is the one Desktop is driving; offering an abandoned
// payload first would register a CLI the user has already replaced.
func TestCodexDesktopBundlePathsFor_WindowsPrefersNewestUserRuntime(t *testing.T) {
	local := t.TempDir()
	writeCodexUserRuntime(t, local, "aaaa-old", time.Now().Add(-72*time.Hour))
	current := writeCodexUserRuntime(t, local, "bbbb-current", time.Now())

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"LOCALAPPDATA": local,
	}), "")
	if len(got) == 0 || got[0] != current {
		t.Fatalf("first candidate = %#v, want the newest runtime %q", got, current)
	}
	if !strings.Contains(strings.Join(got, "\n"), "aaaa-old") {
		t.Error("the older runtime was dropped rather than ranked lower")
	}
}

func TestCodexDesktopBundlePathsFor_WindowsSkipsIncompleteUserRuntime(t *testing.T) {
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "OpenAI", "Codex", "bin", "empty-hash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "OpenAI", "Codex", "bin", "not-a-dir"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"LOCALAPPDATA": local,
	}), "")
	if len(got) != 0 {
		t.Fatalf("candidates = %#v, want none", got)
	}
}

func TestCodexDesktopBundlePathsFor_WindowsPrefersHighestMsixVersion(t *testing.T) {
	programFiles := t.TempDir()
	writeCodexMsix(t, programFiles, "OpenAI.Codex_26.1110.1000.0_x64__2p2nqsd0c76g0")
	current := writeCodexMsix(t, programFiles, "OpenAI.Codex_26.1120.5083.0_x64__2p2nqsd0c76g0")
	writeCodexMsix(t, programFiles, "Contoso.Other_99.0.0.0_x64__abcdefghij")
	if err := os.MkdirAll(filepath.Join(programFiles, "WindowsApps", "OpenAI.Codex_26.1121.0.0_x64__2p2nqsd0c76g0"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"ProgramFiles": programFiles,
	}), "")
	if len(got) != 2 || got[0] != current {
		t.Fatalf("candidates = %#v, want highest version %q first", got, current)
	}
}

func TestCodexDesktopBundlePathsFor_WindowsUserRuntimeOutranksMsix(t *testing.T) {
	local := t.TempDir()
	programFiles := t.TempDir()
	user := writeCodexUserRuntime(t, local, "1cb21d4ee104a51c", time.Now())
	msix := writeCodexMsix(t, programFiles, "OpenAI.Codex_26.1120.5083.0_x64__2p2nqsd0c76g0")

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"LOCALAPPDATA": local,
		"ProgramFiles": programFiles,
	}), "")
	if len(got) != 2 || got[0] != user || got[1] != msix {
		t.Fatalf("candidates = %#v, want user runtime %q before MSIX %q", got, user, msix)
	}
}

func TestCodexDesktopBundlePathsFor_WindowsFallsBackToHomeLocalAppData(t *testing.T) {
	home := t.TempDir()
	exe := writeCodexUserRuntime(t, filepath.Join(home, "AppData", "Local"), "home-hash", time.Now())

	got := codexDesktopBundlePathsFor("windows", envFrom(nil), home)
	if len(got) != 1 || got[0] != exe {
		t.Fatalf("candidates = %#v, want [%q] from %%USERPROFILE%%\\AppData\\Local", got, exe)
	}
}

func TestCodexDesktopBundlePathsFor_WindowsIgnoresUnreadableWindowsApps(t *testing.T) {
	local := t.TempDir()
	exe := writeCodexUserRuntime(t, local, "1cb21d4ee104a51c", time.Now())

	got := codexDesktopBundlePathsFor("windows", envFrom(map[string]string{
		"LOCALAPPDATA": local,
		"ProgramFiles": filepath.Join(local, "missing-program-files"),
	}), "")
	if len(got) != 1 || got[0] != exe {
		t.Fatalf("candidates = %#v, want the user runtime alone", got)
	}
}

// On Windows, a Desktop-only install outside PATH must still be registered.
// This uses the live enumerator (not a stub) so the env wiring stays honest.
func TestProbeAgentCLIsDiscoversWindowsCodexDesktopCLI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("live Windows executable detection and PATHEXT rules")
	}

	local := t.TempDir()
	exe := writeCodexUserRuntime(t, local, "1cb21d4ee104a51c", time.Now())
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "bash.exe"))
	pinNonCodexAgentsToMissingPaths(t)

	got, ok := probeAgentCLIs()["codex"]
	if !ok {
		t.Fatalf("expected codex from the Desktop per-user runtime, got none")
	}
	if got.Path != exe {
		t.Fatalf("codex path = %q, want %q", got.Path, exe)
	}
}

func TestProbeAgentCLIsWindowsCodexPATHStillWins(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("live Windows executable detection and PATHEXT rules")
	}

	local := t.TempDir()
	writeCodexUserRuntime(t, local, "1cb21d4ee104a51c", time.Now())
	pathDir := t.TempDir()
	onPath := filepath.Join(pathDir, "codex.exe")
	if err := os.WriteFile(onPath, []byte("codex-cli from PATH\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("ProgramFiles", t.TempDir())
	t.Setenv("PATH", pathDir)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "bash.exe"))
	pinNonCodexAgentsToMissingPaths(t)

	got, ok := probeAgentCLIs()["codex"]
	if !ok {
		t.Fatal("expected codex from PATH")
	}
	if got.Path != onPath {
		t.Fatalf("codex path = %q, want PATH entry %q", got.Path, onPath)
	}
}

func TestMsixCodexPackageVersion(t *testing.T) {
	got, ok := msixCodexPackageVersion("OpenAI.Codex_26.1120.5083.0_x64__2p2nqsd0c76g0")
	if !ok {
		t.Fatal("expected to parse the reported package folder")
	}
	if got != [4]int{26, 1120, 5083, 0} {
		t.Fatalf("version = %v, want 26.1120.5083.0", got)
	}
	if _, ok := msixCodexPackageVersion("Contoso.Other_1.0.0.0_x64__abcdefghij"); ok {
		t.Fatal("parsed a non-Codex package")
	}
	if _, ok := msixCodexPackageVersion("OpenAI.Codex_not-a-version_x64__2p2nqsd0c76g0"); ok {
		t.Fatal("parsed a non-numeric version")
	}
}
