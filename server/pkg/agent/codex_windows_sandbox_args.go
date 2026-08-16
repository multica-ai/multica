package agent

import (
	"regexp"
	"strings"
)

const codexWindowsUnelevatedSandboxArg = `windows.sandbox="unelevated"`

var codexWindowsSandboxArgRe = regexp.MustCompile(`^\s*windows\s*\.\s*sandbox\s*=`)

// NormalizeCodexWindowsSandboxCustomArgs returns the per-agent custom argv
// that should be stored or launched for a runtime on goos, plus whether the
// canonical leading pair is owned by Multica. The managed bit is persisted
// separately: the token values alone cannot distinguish a platform default
// from the same explicit setting supplied by a user.
//
// Runtime/profile arguments have lower argv priority than custom args. When
// they already own windows.sandbox, lowerPriorityOwns must be true so the
// managed prefix is removed instead of overriding that explicit setting. An
// explicit per-agent override likewise owns the setting and is never
// duplicated.
func NormalizeCodexWindowsSandboxCustomArgs(goos string, managed, lowerPriorityOwns bool, customArgs []string) ([]string, bool) {
	result := append([]string(nil), customArgs...)
	if managed && HasManagedCodexWindowsSandboxPrefix(result) {
		result = result[2:]
	}
	if goos != "windows" || HasCodexWindowsSandboxOverride(result) || lowerPriorityOwns {
		return result, false
	}
	return append([]string{"-c", codexWindowsUnelevatedSandboxArg}, result...), true
}

// LastCodexWindowsSandboxOverride returns the raw value from the last
// windows.sandbox assignment in Codex -c/--config argv. It accepts inline and
// two-token forms and mirrors the launch pipeline's one-layer quote cleanup.
func LastCodexWindowsSandboxOverride(args []string) (string, bool) {
	var last string
	found := false
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
			i++
			if i >= len(args) {
				continue
			}
			value = normalizeCodexSandboxToken(args[i])
		}
		if !codexWindowsSandboxArgRe.MatchString(value) {
			continue
		}
		if idx := strings.Index(value, "="); idx >= 0 {
			last = normalizeCodexSandboxToken(value[idx+1:])
			found = true
		}
	}
	return last, found
}

// HasCodexWindowsSandboxOverride reports whether argv contains a Codex -c or
// --config assignment for windows.sandbox.
func HasCodexWindowsSandboxOverride(args []string) bool {
	_, found := LastCodexWindowsSandboxOverride(args)
	return found
}

// HasManagedCodexWindowsSandboxPrefix reports whether args begin with the
// exact canonical pair. Callers must still have independent provenance before
// treating that pair as platform-owned; token equality alone is insufficient.
func HasManagedCodexWindowsSandboxPrefix(args []string) bool {
	return len(args) >= 2 && args[0] == "-c" && args[1] == codexWindowsUnelevatedSandboxArg
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
