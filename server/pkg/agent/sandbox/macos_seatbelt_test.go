//go:build darwin

package sandbox

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
