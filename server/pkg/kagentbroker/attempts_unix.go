//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package kagentbroker

import (
	"os"
	"syscall"
)

func lockJournal(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func openOwnershipFile(path string, flags int) (*os.File, error) {
	return os.OpenFile(path, flags|syscall.O_NOFOLLOW, 0600)
}
func singleLink(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}
