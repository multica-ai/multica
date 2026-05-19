//go:build darwin

package sandbox

// CEREBRO-PATCH(agent-sandbox-macos-seatbelt-test): cerebro modification of upstream file

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSeatbelt_DeniesSensitivePath spawns sandbox-exec with a real profile
// and verifies that reads under a denied home subpath fail with Operation
// not permitted. This is the kernel-level smoke test required by the
// JEH-321 acceptance criteria.
func TestSeatbelt_DeniesSensitivePath(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	secretDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatalf("mkdir secret: %v", err)
	}
	secretFile := filepath.Join(secretDir, "id_ed25519")
	if err := os.WriteFile(secretFile, []byte("SECRET"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/cat", secretFile)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected sandboxed cat to fail; got output: %s", out)
	}
	combined := string(out)
	if !strings.Contains(combined, "Operation not permitted") &&
		!strings.Contains(combined, "denied") {
		t.Logf("note: expected 'Operation not permitted' / 'denied' in output, got: %s", combined)
	}
	if strings.Contains(combined, "SECRET") {
		t.Errorf("sandbox failed to deny secret read; output included secret:\n%s", combined)
	}
}

// TestSeatbelt_ProfileParsesWithProductionAllowlist is the regression test
// for JEH-354: with a realistic daemon-shaped allowlist (loopback IP,
// loopback hostname, server FQDN, provider FQDNs) the generated profile
// must load into sandbox-exec without parse errors. Before the
// host-to-port translation, sandbox-exec rejected (remote tcp "127.0.0.1:N")
// with "host must be * or localhost in network address" and the wrapped
// process exited 65 in ~24ms — every claude task on prod failed instantly.
//
// The check is deliberately simple: spawn /usr/bin/true under the profile
// and assert exit 0. A profile parse error makes sandbox-exec itself exit
// non-zero before /usr/bin/true ever runs, which is exactly the failure
// mode this test guards against.
func TestSeatbelt_ProfileParsesWithProductionAllowlist(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// Mirrors the host:port pairs the daemon assembles for a Claude task:
	// loopback (IP and hostname forms), the Multica server, and the two
	// Anthropic API endpoints. All four providers (claude/cursor/gemini/
	// copilot) follow the same shape — public FQDN host:port pairs — so
	// passing this test for the Claude shape implies the others parse too.
	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
		AllowedHosts: []string{
			"127.0.0.1:19514",
			"localhost:19514",
			"multica.example.com:443",
			"api.anthropic.com:443",
			"statsig.anthropic.com:443",
		},
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/usr/bin/true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		profile, _ := os.ReadFile(profilePath)
		t.Fatalf("sandbox-exec failed to load profile: %v\nstderr: %s\nprofile:\n%s", err, out, profile)
	}
}

// TestSeatbelt_DNSResolutionInsideSandbox guards the second half of the
// JEH-354 fix: hostname resolution on macOS goes through a unix-domain
// socket at /private/var/run/mDNSResponder, NOT raw UDP/53. Without an
// explicit network-outbound allow on that socket, every getaddrinfo()
// inside the sandbox fails — so even with port 443 open, claude cannot
// reach api.anthropic.com.
//
// We resolve `localhost` (via dscacheutil → mDNSResponder) rather than a
// public host so the test stays hermetic and offline-friendly. A profile
// that misses the mDNSResponder rule fails this test even on an
// air-gapped CI runner.
func TestSeatbelt_DNSResolutionInsideSandbox(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}
	dscache, err := exec.LookPath("dscacheutil")
	if err != nil {
		t.Skip("dscacheutil not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
		// Empty allowlist — DNS must work regardless of which TCP hosts
		// are allowed; the failure mode we're guarding doesn't depend on
		// the allowlist.
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, dscache, "-q", "host", "-a", "name", "localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		profile, _ := os.ReadFile(profilePath)
		t.Fatalf("dscacheutil failed inside sandbox (mDNSResponder rule missing?): %v\nstderr: %s\nprofile:\n%s", err, out, profile)
	}
	if !strings.Contains(string(out), "127.0.0.1") {
		t.Errorf("expected localhost to resolve to 127.0.0.1 inside sandbox; got: %s", out)
	}
}

// TestSeatbelt_WritablePathAllowsWriteOutsideWorkdir is the JEH-405
// regression: agent CLIs (claude, cursor, gemini, …) keep state files
// under $HOME (~/.claude.json, ~/.cursor, …). Before WritablePaths
// existed, the sandbox profile only granted file-write* under Workdir
// and TempDir, so the very first attempt to persist session state was
// denied and the CLI exited 0 with empty output. This test simulates
// that flow: a file outside the workdir but inside a WritablePath must
// be both readable and writable from within the sandbox.
func TestSeatbelt_WritablePathAllowsWriteOutsideWorkdir(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	stateDir := filepath.Join(tmpHome, ".fake-cli")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	stateFile := filepath.Join(stateDir, "session.json")

	profilePath, err := WriteToTemp(Profile{
		Workdir:       workdir,
		Home:          tmpHome,
		WritablePaths: []string{stateDir},
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	// /bin/sh is a sandbox-friendly subprocess; the redirection happens
	// inside the sandboxed shell so the write is what's being tested.
	script := "echo PERSISTED > " + stateFile + " && cat " + stateFile
	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		profile, _ := os.ReadFile(profilePath)
		t.Fatalf("sandboxed write to WritablePath failed: %v\noutput: %s\nprofile:\n%s", err, out, profile)
	}
	if !strings.Contains(string(out), "PERSISTED") {
		t.Errorf("expected file contents in output, got: %s", out)
	}
	got, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file after sandboxed write: %v", err)
	}
	if !strings.Contains(string(got), "PERSISTED") {
		t.Errorf("state file did not receive sandboxed write: %q", got)
	}
}

// TestSeatbelt_WritablePathOverlappingSensitiveStillDenied verifies the
// "last match wins" guarantee at the kernel layer: if a caller passes a
// WritablePath that overlaps a sensitive home subpath (a misconfiguration
// we explicitly refuse to honour), sandbox-exec must still deny the
// write. The unit test asserts ordering in the generated profile; this
// test asserts that the kernel actually enforces it.
func TestSeatbelt_WritablePathOverlappingSensitiveStillDenied(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	secretDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatalf("mkdir secret: %v", err)
	}
	secretFile := filepath.Join(secretDir, "id_rsa")

	profilePath, err := WriteToTemp(Profile{
		Workdir:       workdir,
		Home:          tmpHome,
		WritablePaths: []string{secretDir}, // intentional misconfig
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	script := "echo LEAK > " + secretFile
	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/sh", "-c", script)
	if err := cmd.Run(); err == nil {
		t.Fatalf("sandboxed write to ~/.ssh succeeded but should be denied")
	}
	if data, err := os.ReadFile(secretFile); err == nil && strings.Contains(string(data), "LEAK") {
		t.Errorf("sandbox failed to seal sensitive path; secret file received write: %q", data)
	}
}

// TestSeatbelt_KeychainAccessDenied is the JEH-1774 kernel-level proof
// that the default (KeychainAccessDeny) mode actually blocks reads of
// the keychain DB. We pretend a sandboxed agent tries to read a file
// under $HOME/Library/Keychains; the sandbox must deny it even though
// the broad $HOME read would otherwise cover the path. The unit test
// asserts that the deny rule is emitted in the right order; this test
// asserts the kernel honours it.
func TestSeatbelt_KeychainAccessDenied(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	keychainDir := filepath.Join(tmpHome, "Library", "Keychains")
	if err := os.MkdirAll(keychainDir, 0o700); err != nil {
		t.Fatalf("mkdir keychain dir: %v", err)
	}
	// A real macOS install would put the user's login DB here; we stand
	// in for it with a sentinel value so the test asserts "the bytes
	// never reached the sandboxed process".
	keychainFile := filepath.Join(keychainDir, "login.keychain-db")
	if err := os.WriteFile(keychainFile, []byte("SECRET-KEYCHAIN"), 0o600); err != nil {
		t.Fatalf("write fake keychain: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
		// Default KeychainAccess (Deny) — the failure mode under test.
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/cat", keychainFile)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected sandboxed cat of keychain DB to fail; got output: %s", out)
	}
	if strings.Contains(string(out), "SECRET-KEYCHAIN") {
		t.Errorf("sandbox failed to deny keychain read; output included secret:\n%s", out)
	}
}

// TestSeatbelt_KeychainReadOnlyModeStillReads is the regression test for
// the legacy opt-in path (claude/cursor/gemini/copilot). Read access
// to the keychain DB must continue to work so the agent CLI can fetch
// its own OAuth token via libsecurity; only writes are denied.
func TestSeatbelt_KeychainReadOnlyModeStillReads(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	keychainDir := filepath.Join(tmpHome, "Library", "Keychains")
	if err := os.MkdirAll(keychainDir, 0o700); err != nil {
		t.Fatalf("mkdir keychain dir: %v", err)
	}
	keychainFile := filepath.Join(keychainDir, "login.keychain-db")
	if err := os.WriteFile(keychainFile, []byte("LEGACY-TOKEN"), 0o600); err != nil {
		t.Fatalf("write fake keychain: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir:        workdir,
		Home:           tmpHome,
		KeychainAccess: KeychainAccessReadOnly,
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/cat", keychainFile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		profile, _ := os.ReadFile(profilePath)
		t.Fatalf("sandboxed read of keychain DB failed in read-only mode: %v\noutput: %s\nprofile:\n%s", err, out, profile)
	}
	if !strings.Contains(string(out), "LEGACY-TOKEN") {
		t.Errorf("expected keychain contents in sandboxed read, got: %s", out)
	}

	// Writes must still be denied in read-only mode.
	writeCmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/sh", "-c", "echo CORRUPT > "+keychainFile)
	if err := writeCmd.Run(); err == nil {
		t.Fatalf("sandboxed write to keychain DB succeeded but should be denied")
	}
	if data, err := os.ReadFile(keychainFile); err == nil && strings.Contains(string(data), "CORRUPT") {
		t.Errorf("sandbox failed to deny keychain write; file now contains: %q", data)
	}
}

// TestSeatbelt_AllowsWorkdirAccess verifies the positive case: reads inside
// the workdir succeed. Catches over-broad deny rules that would break
// legitimate agent operation.
func TestSeatbelt_AllowsWorkdirAccess(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	scratch := filepath.Join(workdir, "hello.txt")
	if err := os.WriteFile(scratch, []byte("hello sandbox"), 0o644); err != nil {
		t.Fatalf("write scratch: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/cat", scratch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandboxed cat of workdir file failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "hello sandbox") {
		t.Errorf("expected workdir file contents in output, got: %s", out)
	}
}

// TestSeatbelt_AgentShapeRunsEndToEnd is the JEH-418 acceptance proof for
// "agents actually work when sandboxed". It exercises every operation class
// a real claude task needs at startup, in one sandbox-exec invocation:
//
//   - read existing state under $HOME/.claude (config, prior session)
//   - write new state under $HOME/.claude (session marker)
//   - resolve a hostname (DNS via mDNSResponder)
//   - read project files inside the workdir
//   - write a session log to the workdir
//   - produce non-empty stdout (the JEH-405 silent-fail symptom was exit 0
//     with empty stdout when one of the above was denied)
//
// The "agent" is /bin/sh running an inline script — we don't need the real
// claude binary to prove the sandbox profile permits this shape. If a future
// change to the profile breaks any single operation, this test catches it
// before it reaches Sara's Mac mini and surfaces as "claude returned empty
// output" in production.
func TestSeatbelt_AgentShapeRunsEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec not available on this host")
	}
	dscache, err := exec.LookPath("dscacheutil")
	if err != nil {
		t.Skip("dscacheutil not available on this host")
	}

	tmpHome := t.TempDir()
	workdir := filepath.Join(tmpHome, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// Pre-populate the agent's $HOME/.claude with a config the script will
	// read — mirrors how a real claude install looks before a task starts.
	claudeDir := filepath.Join(tmpHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "config.json"), []byte(`{"version":"test"}`), 0o644); err != nil {
		t.Fatalf("write claude config: %v", err)
	}

	// Pre-populate a project file inside the workdir — the "agent" reads it
	// to simulate looking at the repo before responding.
	if err := os.WriteFile(filepath.Join(workdir, "README.md"), []byte("project content"), 0o644); err != nil {
		t.Fatalf("write workdir README: %v", err)
	}

	profilePath, err := WriteToTemp(Profile{
		Workdir: workdir,
		Home:    tmpHome,
		// Mirrors the production shape buildSandboxConfig assembles for a
		// claude task on a real runtime.
		AllowedHosts: []string{
			"127.0.0.1:19514",
			"localhost:19514",
			"api.anthropic.com:443",
		},
		WritablePaths: []string{claudeDir},
	})
	if err != nil {
		t.Fatalf("WriteToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(profilePath) })

	// The script does five things, ordered to match a real claude startup.
	// Any single failure makes the script exit non-zero and the test fails
	// with the script's stderr — so we get a precise diagnosis instead of
	// the silent-fail symptom we're trying to prevent.
	script := `
set -eu
cat ` + filepath.Join(claudeDir, "config.json") + ` > /dev/null
echo '{"started":true}' > ` + filepath.Join(claudeDir, "session.json") + `
` + dscache + ` -q host -a name localhost > /dev/null
cat ` + filepath.Join(workdir, "README.md") + ` > /dev/null
echo 'turn 1' > ` + filepath.Join(workdir, "session.log") + `
echo ok
`

	cmd := exec.Command("sandbox-exec", "-f", profilePath, "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		profile, _ := os.ReadFile(profilePath)
		t.Fatalf("agent-shape script failed inside sandbox: %v\noutput: %q\nprofile:\n%s", err, out, profile)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		t.Fatalf("agent-shape script produced empty output — JEH-405 silent-fail regression?")
	}
	if trimmed != "ok" {
		t.Fatalf("expected stdout 'ok', got %q", out)
	}

	// The session marker the script wrote must actually be on disk — a
	// silently-denied write would leave the file missing and the agent's
	// next turn would lose its conversation context.
	if _, err := os.Stat(filepath.Join(claudeDir, "session.json")); err != nil {
		t.Errorf("session.json was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workdir, "session.log")); err != nil {
		t.Errorf("session.log was not written: %v", err)
	}
}
