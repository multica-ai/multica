package execenv

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeCSC writes a shell script that simulates the csc CLI.
// commands is a map of command substring -> exit code (0=success, 1=fail).
// The script logs every invocation to {dir}/invocations.log for verification.
func writeFakeCSC(t *testing.T, dir string, commands map[string]int) string {
	t.Helper()
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	script.WriteString("echo \"$@\" >> " + filepath.Join(dir, "invocations.log") + "\n")
	for substr, exitCode := range commands {
		script.WriteString("echo \"$@\" | grep -q '" + substr + "' && exit " + strconvItoa(exitCode) + "\n")
	}
	script.WriteString("exit 0\n")

	path := filepath.Join(dir, "csc")
	if err := os.WriteFile(path, []byte(script.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// strconvItoa converts a small non-negative int to its decimal string.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func readInvocations(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// testPlugin returns a standard plugin for testing.
func testPlugin() *AgentPlugin {
	return &AgentPlugin{
		ID:   "test-plugin-id",
		Name: "cospower",
		Install: &PluginInstall{
			Method:              "plugin_marketplace",
			Marketplace:         "example/marketplace",
			PluginName:          "cospower",
			MarketplaceName:     "marketplace",
			MarketplaceRepo:     "https://github.com/example/marketplace.git",
			MarketplaceVerified: true,
		},
	}
}

func TestSetupPlugins_DispatchesToCSC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, nil)
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupPlugins(context.Background(), "csc", fakeBin, workDir, testPlugin(), slog.Default())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	invocations := readInvocations(t, dir)
	if len(invocations) < 3 {
		t.Fatalf("expected at least 3 invocations (add+update+install), got %d: %v", len(invocations), invocations)
	}
}

func TestSetupPlugins_UnknownProviderSkips(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupPlugins(context.Background(), "claude", "/usr/bin/claude", workDir, testPlugin(), slog.Default())
	if err != nil {
		t.Fatalf("unknown provider should skip silently, got: %v", err)
	}
}

func TestSetupPlugins_EmptyPluginsSkips(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupPlugins(context.Background(), "csc", "/usr/bin/csc", workDir, nil, slog.Default())
	if err != nil {
		t.Fatalf("empty plugins should skip silently, got: %v", err)
	}
}

func TestSetupPlugins_EmptyBinSkips(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupPlugins(context.Background(), "csc", "", workDir, testPlugin(), slog.Default())
	if err != nil {
		t.Fatalf("empty bin should skip silently, got: %v", err)
	}
}

func TestSetupCSCPlugins_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, nil)
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, testPlugin(), slog.Default())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	invocations := readInvocations(t, dir)
	// Expected: marketplace add + update + install = 3 invocations
	if len(invocations) != 3 {
		t.Fatalf("expected 3 invocations, got %d: %v", len(invocations), invocations)
	}
	if !strings.Contains(invocations[0], "plugin marketplace add") {
		t.Errorf("first invocation should be marketplace add, got: %s", invocations[0])
	}
	if !strings.Contains(invocations[1], "plugin update") {
		t.Errorf("second invocation should be plugin update, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[1], "cospower") {
		t.Errorf("update should mention plugin name cospower, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[2], "plugin install") {
		t.Errorf("third invocation should be plugin install, got: %s", invocations[2])
	}
	if !strings.Contains(invocations[2], "cospower") {
		t.Errorf("install should mention plugin name cospower, got: %s", invocations[2])
	}
	if !strings.Contains(invocations[2], "-s project") {
		t.Errorf("install should use -s project scope, got: %s", invocations[2])
	}
	// Verify no --dir flag
	for _, inv := range invocations {
		if strings.Contains(inv, "--dir") {
			t.Errorf("commands should not use --dir flag, got: %s", inv)
		}
	}
}

func TestSetupCSCPlugins_MarketplaceAddFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{"marketplace add": 1})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, testPlugin(), slog.Default())
	if err == nil {
		t.Fatal("expected error when marketplace add fails")
	}
	if !strings.Contains(err.Error(), "marketplace add") {
		t.Errorf("error should mention marketplace add, got: %v", err)
	}
}

func TestSetupCSCPlugins_UpdateFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{"plugin update": 1})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, testPlugin(), slog.Default())
	if err == nil {
		t.Fatal("expected error when plugin update fails")
	}
	if !strings.Contains(err.Error(), "plugin update") {
		t.Errorf("error should mention plugin update, got: %v", err)
	}
}

func TestSetupCSCPlugins_InstallFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{"plugin install": 1})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, testPlugin(), slog.Default())
	if err == nil {
		t.Fatal("expected error when plugin install fails")
	}
	if !strings.Contains(err.Error(), "plugin install") {
		t.Errorf("error should mention plugin install, got: %v", err)
	}
}

func TestSetupCSCPlugins_EmptyPlugins(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), "/usr/bin/csc", workDir, nil, slog.Default())
	if err != nil {
		t.Fatalf("empty plugins should succeed, got: %v", err)
	}
}

func TestSetupCSCPlugins_EmptyCSCBin(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), "", workDir, testPlugin(), slog.Default())
	if err != nil {
		t.Fatalf("empty cscBin should succeed, got: %v", err)
	}
}

func TestSetupCSCPlugins_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep 300\n"
	fakeBin := filepath.Join(dir, "csc")
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := setupCSCPlugins(ctx, fakeBin, workDir, testPlugin(), slog.Default())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSetupCSCPlugins_ErrorMessageContainsURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{"marketplace add": 1})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	plugin := testPlugin()
	err := setupCSCPlugins(context.Background(), fakeBin, workDir, plugin, slog.Default())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), plugin.Install.MarketplaceRepo) {
		t.Errorf("error should contain marketplace repo %q, got: %v", plugin.Install.MarketplaceRepo, err)
	}
}
