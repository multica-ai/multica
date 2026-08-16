package agent

import (
	"regexp"
	"strings"
)

const codexWindowsUnelevatedSandboxArg = `windows.sandbox="unelevated"`

var codexWindowsSandboxArgRe = regexp.MustCompile(`^\s*windows\s*\.\s*sandbox\s*=`)

// EnsureCodexWindowsSandboxCustomArgs returns the per-agent custom arguments
// that should be stored and launched for Codex on goos. The managed Windows
// default lives in two argv elements. It is appended after unrelated custom
// arguments so the existing extra-then-custom precedence remains intact.
//
// An explicit windows.sandbox override in either the lower-priority runtime
// args or the per-agent custom args owns the setting and prevents the default
// from being added. Existing occurrences are deliberately left in place:
// Codex config overrides are last-wins, so collapsing user entries here could
// change their meaning.
func EnsureCodexWindowsSandboxCustomArgs(goos string, extraArgs, customArgs []string) []string {
	result := append([]string(nil), customArgs...)
	if goos != "windows" || hasCodexWindowsSandboxArg(extraArgs) || hasCodexWindowsSandboxArg(customArgs) {
		return result
	}
	return append(result, "-c", codexWindowsUnelevatedSandboxArg)
}

func hasCodexWindowsSandboxArg(args []string) bool {
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

// normalizeCodexSandboxToken mirrors the one-layer shell-quote cleanup in the
// launch pipeline without otherwise rewriting the persisted argv token.
func normalizeCodexSandboxToken(token string) string {
	if len(token) >= 2 && ((token[0] == '\'' && token[len(token)-1] == '\'') ||
		(token[0] == '"' && token[len(token)-1] == '"')) {
		return token[1 : len(token)-1]
	}
	return token
}
