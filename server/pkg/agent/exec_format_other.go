//go:build !windows

package agent

// isPlatformExecFormatError has nothing to add outside Windows: execve reports
// every "this file is not a program" case as ENOEXEC, which IsExecFormatError
// already checks.
func isPlatformExecFormatError(error) bool { return false }
