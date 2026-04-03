//go:build windows

package main

import "syscall"

// daemonSysProcAttr returns the SysProcAttr used when spawning a detached
// daemon child process. On Windows, Setsid is not available; we use
// CREATE_NEW_PROCESS_GROUP so the child is not terminated when the parent
// console exits.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
