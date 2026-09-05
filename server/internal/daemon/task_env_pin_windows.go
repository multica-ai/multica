//go:build windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeTaskIdentityPinScript(dir string, vars map[string]string) (string, error) {
	path := filepath.Join(dir, taskIdentityPinName+".cmd")
	var b strings.Builder
	b.WriteString("@echo off\r\nsetlocal\r\n")
	for _, k := range sortedEnvKeys(vars) {
		if !isSafeEnvKey(k) {
			continue
		}
		fmt.Fprintf(&b, "set \"%s=%s\"\r\n", k, windowsEnvValue(vars[k]))
	}
	b.WriteString("%*\r\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func windowsEnvValue(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}
