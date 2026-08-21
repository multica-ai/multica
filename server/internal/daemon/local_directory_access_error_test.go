package daemon

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestDescribeDirAccessError_DarwinEPERM(t *testing.T) {
	orig := accessErrorGOOS
	accessErrorGOOS = "darwin"
	defer func() { accessErrorGOOS = orig }()
	for _, op := range []string{"read", "write"} {
		err := describeDirAccessError(op, "/tmp/foo", syscall.EPERM)
		msg := err.Error()
		if !strings.Contains(msg, "/tmp/foo") || !strings.Contains(msg, "Full Disk Access") || !strings.Contains(msg, "Privacy & Security") {
			t.Fatalf("op %s: missing advice text in %q", op, msg)
		}
		if !errors.Is(err, syscall.EPERM) {
			t.Fatalf("op %s: wrapped EPERM lost", op)
		}
	}
}

func TestDescribeDirAccessError_NonEPERMUnchanged(t *testing.T) {
	orig := accessErrorGOOS
	accessErrorGOOS = "darwin"
	defer func() { accessErrorGOOS = orig }()
	for _, op := range []string{"read", "write"} {
		got := describeDirAccessError(op, "/tmp/foo", os.ErrNotExist)
		want := fmt.Errorf("%s %q: %w", op, "/tmp/foo", os.ErrNotExist)
		if got.Error() != want.Error() {
			t.Fatalf("op %s: got %q want %q", op, got.Error(), want.Error())
		}
		if strings.Contains(got.Error(), "Full Disk Access") {
			t.Fatalf("op %s: unexpected advice text", op)
		}
	}
}

func TestDescribeDirAccessError_NonDarwinUnchanged(t *testing.T) {
	orig := accessErrorGOOS
	accessErrorGOOS = "linux"
	defer func() { accessErrorGOOS = orig }()
	for _, op := range []string{"read", "write"} {
		got := describeDirAccessError(op, "/tmp/foo", syscall.EPERM)
		if strings.Contains(got.Error(), "Full Disk Access") {
			t.Fatalf("op %s: advice leaked on linux", op)
		}
		want := fmt.Errorf("%s %q: %w", op, "/tmp/foo", syscall.EPERM)
		if got.Error() != want.Error() {
			t.Fatalf("op %s: got %q want %q", op, got.Error(), want.Error())
		}
	}
}

func TestCheckDirReadWrite_ReadDeniedUsesDescribe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores mode bits")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)
	err := checkDirReadWrite(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "read \"") {
		t.Fatalf("expected read prefix, got %q", err.Error())
	}
}
