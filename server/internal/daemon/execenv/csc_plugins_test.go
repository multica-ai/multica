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
// Avoids importing strconv for a test helper that only needs single-digit values.
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

func TestSetupCSCPlugins_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, nil) // all commands succeed
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	invocations := readInvocations(t, dir)
	if len(invocations) < 2 {
		t.Fatalf("expected at least 2 invocations, got %d: %v", len(invocations), invocations)
	}
	if !strings.Contains(invocations[0], "plugin marketplace add") {
		t.Errorf("first invocation should be marketplace add, got: %s", invocations[0])
	}
	if !strings.Contains(invocations[1], "plugin install") {
		t.Errorf("second invocation should be plugin install, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[1], "cospower@costrict-plugins") {
		t.Errorf("install should use cospower@costrict-plugins, got: %s", invocations[1])
	}
	if !strings.Contains(invocations[1], workDir) {
		t.Errorf("install should pass --dir workdir, got: %s", invocations[1])
	}
}

func TestSetupCSCPlugins_MarketplaceAddFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"marketplace add": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error when marketplace add fails")
	}
	if !strings.Contains(err.Error(), "marketplace add") {
		t.Errorf("error should mention marketplace add, got: %v", err)
	}
}

func TestSetupCSCPlugins_InstallFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"plugin install": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error when plugin install fails")
	}
	if !strings.Contains(err.Error(), "plugin install") {
		t.Errorf("error should mention plugin install, got: %v", err)
	}
}

func TestSetupCSCPlugins_CSCBinEmpty(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), "", workDir, slog.Default())
	if err != nil {
		t.Fatalf("empty cscBin should skip silently, got: %v", err)
	}
	// No invocations.log should exist
	if _, err := os.Stat(filepath.Join(dir, "invocations.log")); err == nil {
		t.Error("expected no invocations when cscBin is empty")
	}
}

func TestSetupCSCPlugins_Timeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	// Write a fake that sleeps forever
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

	err := setupCSCPlugins(ctx, fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "marketplace add") && !strings.Contains(err.Error(), "context") {
		t.Errorf("error should mention marketplace add or context, got: %v", err)
	}
}

func TestSetupCSCPlugins_ErrorMessageContainsURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake not supported on windows")
	}
	dir := t.TempDir()
	fakeBin := writeFakeCSC(t, dir, map[string]int{
		"marketplace add": 1,
	})
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := setupCSCPlugins(context.Background(), fakeBin, workDir, slog.Default())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error should contain the marketplace URL for debugging
	if !strings.Contains(err.Error(), cscMarketplaceURL) {
		t.Errorf("error should contain marketplace URL %q, got: %v", cscMarketplaceURL, err)
	}
}
