package main

import (
	"strings"
	"unicode"
)

func normalizeCapturedUserText(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if s == "" {
		return ""
	}
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == ' ' && i > 0 && i+1 < len(runes) && isHan(runes[i-1]) && isHan(runes[i+1]) {
			continue
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func isSlashInput(content string) bool {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return false
	}
	name := strings.TrimPrefix(fields[0], "/")
	if name == "" || strings.Contains(name, "/") {
		return false
	}
	switch name {
	case "approvals", "clear", "compact", "diff", "exit", "help", "init", "mcp", "model", "new", "prompts", "quit", "resume", "review", "settings", "status":
		return true
	default:
		return false
	}
}

func isHan(r rune) bool {
	return unicode.Is(unicode.Han, r)
}

func isStatusOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(strings.TrimLeft(s, "✓✔•└─>› ")))
	statusPrefixes := []string{"think", "thinking", "work", "working", "loading", "running", "processing", "waiting"}
	for _, prefix := range statusPrefixes {
		if strings.HasPrefix(lower, prefix) && len([]rune(s)) <= 40 {
			return true
		}
	}
	if isBareProgress(s) {
		return true
	}
	onlyMarks := true
	for _, r := range s {
		if !strings.ContainsRune(`|/-\.*•·⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ `, r) {
			onlyMarks = false
			break
		}
	}
	return onlyMarks
}

func isBareProgress(s string) bool {
	if !strings.Contains(s, "%") || len([]rune(s)) > 30 {
		return false
	}
	for _, r := range s {
		if unicode.IsDigit(r) || strings.ContainsRune("% .,/[]()=-", r) {
			continue
		}
		return false
	}
	return true
}
