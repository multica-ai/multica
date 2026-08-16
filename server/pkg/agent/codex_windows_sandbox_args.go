package agent

import (
	"regexp"
	"strings"
)

const codexWindowsUnelevatedSandboxArg = `windows.sandbox="unelevated"`

var codexWindowsSandboxArgRe = regexp.MustCompile(`^\s*windows\s*\.\s*sandbox\s*=`)

// NormalizeCodexWindowsSandboxCustomArgs returns the per-agent custom argv
// that should be stored or launched for a runtime on goos. Multica reserves
// the exact leading pair below as its managed representation, which makes the
// operation idempotent and lets runtime-only edits remove the platform
// condition when an agent moves away from Windows Codex.
//
// Runtime/profile arguments have lower argv priority than custom args. When
// they already own windows.sandbox, lowerPriorityOwns must be true so the
// managed prefix is removed instead of overriding that explicit setting. An
// explicit per-agent override likewise owns the setting and is never
// duplicated.
func NormalizeCodexWindowsSandboxCustomArgs(goos string, lowerPriorityOwns bool, customArgs []string) []string {
	result := withoutManagedCodexWindowsSandboxPrefix(customArgs)
	if goos != "windows" || lowerPriorityOwns || HasCodexWindowsSandboxOverride(result) {
		return result
	}
	return append([]string{"-c", codexWindowsUnelevatedSandboxArg}, result...)
}

// HasCodexWindowsSandboxOverride reports whether argv contains a Codex -c or
// --config assignment for windows.sandbox. It accepts both inline and
// two-token forms and mirrors the launch pipeline's one-layer quote cleanup.
func HasCodexWindowsSandboxOverride(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := normalizeCodexSandboxToken(args[i])
		flag := arg
		value := ""
		inline := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			value = normalizeCodexSandboxToken(arg[idx+1:])
			inline = true
		}
		if flag != "-c" && flag != "--config" {
			continue
		}
		if !inline {
			if i+1 >= len(args) {
				continue
			}
			i++
			value = normalizeCodexSandboxToken(args[i])
		}
		if codexWindowsSandboxArgRe.MatchString(value) {
			return true
		}
	}
	return false
}

func withoutManagedCodexWindowsSandboxPrefix(args []string) []string {
	start := 0
	if len(args) >= 2 && args[0] == "-c" && args[1] == codexWindowsUnelevatedSandboxArg {
		start = 2
	}
	return append([]string(nil), args[start:]...)
}

// normalizeCodexSandboxToken mirrors the one-layer shell-quote cleanup in the
// launch pipeline without otherwise rewriting the persisted argv token.
func normalizeCodexSandboxToken(token string) string {
	if len(token) >= 2 && ((token[0] == '\'' && token[len(token)-1] == '\'') ||
		(token[0] == '"' && token[len(token)-1] == '"')) {
		return token[1 : len(token)-1]
	}
	return token
}
