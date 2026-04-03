//go:build !windows

package main

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning a detached
// daemon child process. On Unix systems we create a new session (Setsid) so
// the child is not terminated when the parent exits.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
