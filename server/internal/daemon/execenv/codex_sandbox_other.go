//go:build !windows

package execenv

func getWindowsRegistrySandboxMode() string {
	return ""
}
