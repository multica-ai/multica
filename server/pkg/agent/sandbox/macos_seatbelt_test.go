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
