//go:build darwin || linux

package authority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestLoadPrivateKeyFileRejectsPathSwappedToFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	_, keyPath := writePKCS8KeyFile(t, dir, 0o600)
	checkedKeyPath := filepath.Join(dir, "authority-key-checked.pem")
	openStarted := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		_, err := loadPrivateKeyFileWithOpener(keyPath, func(path string) (*os.File, error) {
			if err := os.Rename(path, checkedKeyPath); err != nil {
				return nil, err
			}
			if err := unix.Mkfifo(path, 0o600); err != nil {
				return nil, err
			}
			close(openStarted)
			return openPrivateKeyFileNoFollow(path)
		})
		result <- err
	}()

	<-openStarted
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("error = %v, want descriptor-level FIFO rejection", err)
		}
	case <-time.After(500 * time.Millisecond):
		// Unblock the vulnerable reader so the test exits cleanly after proving the hang.
		writerFD, err := unix.Open(keyPath, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("open FIFO rescue writer: %v", err)
		}
		if err := unix.Close(writerFD); err != nil {
			t.Fatalf("close FIFO rescue writer: %v", err)
		}
		<-result
		t.Fatal("authority private key opener blocked after the checked path was swapped to a FIFO")
	}
}
