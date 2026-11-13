//go:build !windows

package wecom

import "testing"

func TestMediaTempFileIsPrivateAndUnlinked(t *testing.T) {
	dir := t.TempDir()
	f, err := createMediaTempFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	assertMediaTempDirEmpty(t, dir)
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the plaintext file is mode %v, want no group or other access", perm)
	}
}
