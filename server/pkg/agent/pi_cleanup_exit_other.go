//go:build !linux && !darwin && !windows

package agent

import "os/exec"

const piCanObserveRuntimeExit = false

func piRuntimeExited(*exec.Cmd) bool { return false }
