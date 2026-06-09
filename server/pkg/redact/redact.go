// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/user"
	"regexp"
	"strings"
)

// secretPattern pairs a compiled regex with its replacement text.
type secretPattern struct {
	re          *regexp.Regexp
	replacement string
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// AWS access key IDs (always start with AKIA)
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "[REDACTED AWS KEY]"},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}`), "[REDACTED AWS SECRET]"},

	// PEM private keys (multi-line)
	{regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`), "[REDACTED PRIVATE KEY]"},

	// GitHub tokens (classic PAT, fine-grained, OAuth, etc.)
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// OpenAI / Anthropic API keys
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[REDACTED API KEY]"},

	// Slack tokens
	{regexp.MustCompile(`\bxox[bporas]-[A-Za-z0-9\-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// GitLab personal access tokens
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), "[REDACTED GITLAB TOKEN]"},

	// JWT tokens (three base64url segments)
	{regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`), "[REDACTED CONNECTION STRING]@"},

	// Generic key=value patterns for common secret env var names
	{regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), "[REDACTED CREDENTIAL]"},
}

var sensitiveJSONKey = regexp.MustCompile(`(?i)(^|[_-])(api[_-]?key|api[_-]?secret|secret[_-]?key|secret|access[_-]?token|auth[_-]?token|private[_-]?key|database[_-]?url|db[_-]?password|db[_-]?url|redis[_-]?url|password|token|jwt)($|[_-])`)

const redactedCredential = "[REDACTED CREDENTIAL]"

// InputMap returns a copy of m with all string values passed through Text and
// any nested custom_env object replaced by coarse metadata. Non-string values
// are preserved unless they are nested inside a secret-bearing key.
func InputMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	redacted, _ := redactJSONMap(m)
	if out, ok := redacted.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

// homeDir is resolved once at init for path redaction.
var homeDir string
var username string

func init() {
	homeDir, _ = os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
}

// Text scans the input string for known secret patterns and replaces
// matches with safe placeholders. It also masks the local user's home
// directory path to prevent leaking the username.
func Text(s string) string {
	s = redactStructuredJSON(s)
	return redactPlainText(s)
}

func redactPlainText(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}

	// Redact home directory paths (e.g. /Users/john/ → /Users/****/).
	if homeDir != "" && username != "" {
		masked := strings.Replace(homeDir, username, "****", 1)
		s = strings.ReplaceAll(s, homeDir, masked)
	}

	return s
}

func redactStructuredJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s
	}

	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()

	var value any
	if err := dec.Decode(&value); err != nil {
		return s
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return s
	}

	redacted, changed := redactJSONValue(value)
	if !changed {
		return s
	}
	out, err := json.Marshal(redacted)
	if err != nil {
		return s
	}
	return string(out)
}

func redactJSONValue(value any) (any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return redactJSONMap(v)
	case []any:
		out := make([]any, len(v))
		changed := false
		for i, item := range v {
			redacted, itemChanged := redactJSONValue(item)
			out[i] = redacted
			changed = changed || itemChanged
		}
		return out, changed
	case string:
		redacted := redactPlainText(v)
		return redacted, redacted != v
	default:
		return value, false
	}
}

func redactJSONMap(m map[string]any) (any, bool) {
	out := make(map[string]any, len(m))
	changed := false
	customEnvSeen := false
	customEnvKeyCount := 0

	for k, v := range m {
		if strings.EqualFold(k, "custom_env") {
			customEnvSeen = true
			customEnvKeyCount = countObjectKeys(v)
			changed = true
			continue
		}

		if sensitiveJSONKey.MatchString(k) {
			out[k] = redactedCredential
			changed = true
			continue
		}

		redacted, valueChanged := redactJSONValue(v)
		out[k] = redacted
		changed = changed || valueChanged
	}

	if customEnvSeen {
		out["has_custom_env"] = customEnvKeyCount > 0
		out["custom_env_key_count"] = customEnvKeyCount
		out["custom_env_redacted"] = true
	}

	return out, changed
}

func countObjectKeys(value any) int {
	if m, ok := value.(map[string]any); ok {
		return len(m)
	}
	return 0
}
