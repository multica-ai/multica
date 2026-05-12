package agent

import (
	"path/filepath"
	"strings"
)

func isTrustedReadOnlyPlatformCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, ";&|<>`$\\\n\r") {
		return false
	}
	words := strings.Fields(command)
	if len(words) == 2 && words[0] == "which" && words[1] == "multica" {
		return true
	}
	if len(words) < 3 {
		return false
	}
	executable := words[0]
	isMultica := executable == "multica" || filepath.Base(executable) == "multica"
	return isMultica && words[1] == "issue" && words[2] == "get"
}
