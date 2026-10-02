//go:build !windows

package qoderruntime

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
)

func lockState(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("bridge state is already locked: %w", err)
	}
	return f, nil
}

func syncStateDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
