// Package agent — platform_command.go
//
// Base trust allowlist for Multica platform commands.
//
// Every CLI backend (Claude, Codex, future providers) MUST call
// IsTrustedPlatformCommand before routing a Bash tool request through the
// approval flow. Commands that pass this check are auto-allowed without
// user interaction.
//
// Design contract:
//   - This is the SINGLE source of truth for platform command trust.
//   - Individual CLI backends MAY add provider-specific trust rules on top,
//     but MUST NOT skip this base check.
//   - If a new read-only pipe utility is commonly used with `multica` CLI
//     commands, add it to safePipeCommands below.
package agent

import (
	"path/filepath"
	"regexp"
	"strings"
)

// safePipeCommands are read-only utilities that are safe to use in a pipeline
// with multica commands. They cannot modify the filesystem or exfiltrate data.
var safePipeCommands = map[string]bool{
	"cat":    true,
	"printf": true,
	"echo":   true,
	"head":   true,
	"tail":   true,
	"grep":   true,
	"wc":     true,
	"sort":   true,
	"tr":     true,
	"cut":    true,
	"sed":    true,
	"awk":    true,
	"jq":     true,
	"tee":    false, // writes to file — NOT safe
}

// safeRedirectRe matches harmless stderr-to-/dev/null redirections that
// agents commonly append (e.g. "2>/dev/null", "2> /dev/null").
// These are safe because they only discard stderr output.
var safeRedirectRe = regexp.MustCompile(`\s*2>\s*/dev/null\s*`)

// IsTrustedPlatformCommand reports whether a Bash command is a trusted
// Multica platform command that can be auto-allowed without user approval.
//
// A command is trusted when:
//  1. It contains at least one `multica` (or full-path-to-multica) invocation.
//  2. Every other pipeline segment is a known safe read-only utility.
//  3. No shell meta-characters that could escape the pipeline (&&, ||, ;, >, etc.).
//
// This is the base allowlist. All CLI backends MUST use this function.
func IsTrustedPlatformCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	// Strip safe stderr redirections before splitting so that "2>/dev/null"
	// does not trigger the ">" blocker.
	cleaned := safeRedirectRe.ReplaceAllString(command, " ")

	segments := strings.Split(cleaned, "|")
	foundMultica := false
	for _, segment := range segments {
		header := strings.TrimSpace(strings.SplitN(segment, "\n", 2)[0])
		if header == "" || strings.Contains(header, "&&") || strings.Contains(header, "||") || strings.ContainsAny(header, ";>`$\r") {
			return false
		}
		words := strings.Fields(header)
		if len(words) == 0 {
			continue
		}
		executable := words[0]
		if executable == "which" && len(words) == 2 && words[1] == "multica" {
			foundMultica = true
			continue
		}
		if safePipeCommands[executable] {
			continue
		}
		if executable == "multica" || filepath.Base(executable) == "multica" {
			foundMultica = true
			continue
		}
		return false
	}
	return foundMultica
}

// isTrustedPlatformCommand is the package-internal alias.
// Kept for backward compatibility with existing call sites.
func isTrustedPlatformCommand(command string) bool {
	return IsTrustedPlatformCommand(command)
}
