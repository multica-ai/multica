//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func writeTaskIdentityPinScript(dir string, vars map[string]string) (string, error) {
	path := filepath.Join(dir, taskIdentityPinName)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, k := range sortedEnvKeys(vars) {
		if !isSafeEnvKey(k) {
			continue
		}
		fmt.Fprintf(&b, "export %s=%s\n", k, posixShellQuote(vars[k]))
	}
	b.WriteString("exec \"$@\"\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func posixShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
