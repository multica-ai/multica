//go:build windows

package daemon

import "golang.org/x/sys/windows/registry"

func windowsUserPath() string {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	path, _, err := key.GetStringValue("Path")
	if err != nil {
		return ""
	}
	return path
}
