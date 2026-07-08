//go:build windows

package execenv

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func getWindowsRegistrySandboxMode() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()

	val, _, err := k.GetStringValue("MULTICA_CODEX_WINDOWS_SANDBOX_MODE")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(val)
}
