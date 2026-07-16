package redact

import (
	"regexp"
	"strings"
)

var cerebroOAuthRefreshPatterns = []secretPattern{
	{
		re:          regexp.MustCompile(`(?i)(["'](?:refresh_token|refreshtoken|refresh)["']\s*:\s*["'])[^"']+`),
		replacement: `${1}[REDACTED OAUTH REFRESH]`,
	},
	{
		re:          regexp.MustCompile(`(?i)(\b(?:refresh_token|refreshtoken)\b\s*[=:]\s*["']?)[^\s,"'}]+`),
		replacement: `[REDACTED OAUTH REFRESH]`,
	},
}

var nonOAuthFieldCharacter = regexp.MustCompile(`[^a-z0-9]+`)

func init() {
	// Named OAuth refresh fields must be handled before the broad TOKEN rule,
	// otherwise only the suffix is masked and the safer diagnostic category is lost.
	patterns = append(cerebroOAuthRefreshPatterns, patterns...)
}

func redactInputValue(key string, value any) any {
	switch typed := value.(type) {
	case string:
		if isOAuthRefreshField(key) {
			return "[REDACTED OAUTH REFRESH]"
		}
		return Text(typed)
	case map[string]any:
		out := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			out[nestedKey] = redactInputValue(nestedKey, nestedValue)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, nestedValue := range typed {
			out[i] = redactInputValue("", nestedValue)
		}
		return out
	default:
		return value
	}
}

func isOAuthRefreshField(key string) bool {
	normalized := nonOAuthFieldCharacter.ReplaceAllString(strings.ToLower(key), "")
	return normalized == "refreshtoken" || normalized == "refresh"
}
