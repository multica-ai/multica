//go:build windows

package localenv

import (
	"os/exec"
	"syscall"
)

const supported = false

func shellCommand(script string) *exec.Cmd { return exec.Command("cmd", "/C", script) }

func setProcessGroup(*exec.Cmd) {}

func signalGroup(int, syscall.Signal) error { return ErrUnsupported }

func groupAlive(int) bool { return false }

func processGroup(int) int { return 0 }
