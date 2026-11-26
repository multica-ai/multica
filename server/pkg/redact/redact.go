// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"regexp"
	"sort"
	"strings"
)

// secretPattern pairs a compiled regex with its replacement text.
//
// A pattern with a rule is precise enough to report to people (Find), not just
// to mask in agent output. Its secret is the subexpression named "secret", or
// the whole match when there is none, and placeholder (replacement when
// empty) replaces that span.
type secretPattern struct {
	re          *regexp.Regexp
	replacement string
	rule        string
	placeholder string
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// AWS access key IDs (always start with AKIA)
	{re: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), replacement: "[REDACTED AWS KEY]", rule: RuleAWSAccessKey},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{re: regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*(?P<secret>[A-Za-z0-9/+=]{40})`), replacement: "[REDACTED AWS SECRET]", rule: RuleAWSSecretKey},

	// PEM private keys (multi-line)
	{re: regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`), replacement: "[REDACTED PRIVATE KEY]", rule: RulePrivateKey},

	// GitHub tokens (classic PAT, OAuth, user-to-server, server-to-server, refresh)
	{re: regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), replacement: "[REDACTED GITHUB TOKEN]", rule: RuleGitHubToken},

	// GitHub fine-grained personal access tokens use the github_pat_ prefix,
	// which the classic ghp_/gho_/... pattern above does not cover. Without
	// this line a fine-grained PAT emitted in agent output leaks unredacted
	// to the database and WebSocket broadcast.
	{re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,255}\b`), replacement: "[REDACTED GITHUB TOKEN]", rule: RuleGitHubToken},

	// Anthropic API keys. They also match the sk- rule below, which masks
	// them the same way; this line only tells the two apart for Find.
	{re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`), replacement: "[REDACTED API KEY]", rule: RuleAnthropicKey},

	// OpenAI / Anthropic API keys
	{re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), replacement: "[REDACTED API KEY]", rule: RuleOpenAIKey},

	// Slack bot/user/legacy tokens. The char class includes 'e' so the
	// newer xoxe- config/refresh tokens are covered alongside xoxb/p/o/r/a/s.
	{re: regexp.MustCompile(`\bxox[bporase]-[A-Za-z0-9\-]{10,}\b`), replacement: "[REDACTED SLACK TOKEN]", rule: RuleSlackToken},

	// Slack app-level tokens use the xapp- prefix, which the xox*- rule above
	// does not match. Without this an app-level token echoed in agent output
	// leaks unredacted to the DB / WebSocket broadcast.
	{re: regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}\b`), replacement: "[REDACTED SLACK TOKEN]", rule: RuleSlackToken},

	// GitLab personal access tokens
	{re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), replacement: "[REDACTED GITLAB TOKEN]", rule: RuleGitLabToken},

	// Multica user PATs, daemon tokens, task tokens, and cloud-node PATs.
	// Preserve short JSON control escapes that act as token separators.
	// The space-free placeholder lets the generic TOKEN= rule below
	// replace it in one piece.
	{re: regexp.MustCompile(`(?:\b|(\\[bfnrt]))(?P<secret>(?:mat|mul|mdt|mcn)_[0-9a-fA-F]{40})\b`), replacement: "${1}[REDACTED_MULTICA_TOKEN]", rule: RuleMulticaToken, placeholder: "[REDACTED_MULTICA_TOKEN]"},

	// Google API keys always start with the AIza prefix and are 39 chars total
	// (AIza + 35). Capture and restore the trailing delimiter so keys ending in
	// a non-word character such as '-' are still redacted.
	{re: regexp.MustCompile(`\b(?P<secret>AIza[0-9A-Za-z_-]{35})([^0-9A-Za-z_-]|$)`), replacement: "[REDACTED GOOGLE API KEY]$2", rule: RuleGoogleAPIKey, placeholder: "[REDACTED GOOGLE API KEY]"},

	// Stripe secret / restricted live keys (sk_live_ / rk_live_). The sk-
	// rule above only matches the hyphen form used by OpenAI/Anthropic; Stripe
	// uses an underscore, so live keys are not covered without this. Publishable
	// keys (pk_live_) are intentionally excluded — they are not secret.
	{re: regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`), replacement: "[REDACTED STRIPE KEY]", rule: RuleStripeKey},

	// JWT tokens (three base64url segments)
	{re: regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), replacement: "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{re: regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), replacement: "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{re: regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:(?P<secret>[^@\s]+)@`), replacement: "[REDACTED CONNECTION STRING]@", rule: RuleConnectionPassword, placeholder: "[REDACTED PASSWORD]"},

	// Generic key=value patterns for common secret env var names
	{re: regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), replacement: "[REDACTED CREDENTIAL]"},
}

// Rules Find reports. JWTs, Bearer headers and KEY=value pairs are masked in
// agent output but never reported: they match too much that is not a secret.
const (
	RuleAWSAccessKey       = "aws_access_key"
	RuleAWSSecretKey       = "aws_secret_key"
	RulePrivateKey         = "private_key"
	RuleGitHubToken        = "github_token"
	RuleAnthropicKey       = "anthropic_api_key"
	RuleOpenAIKey          = "openai_api_key"
	RuleSlackToken         = "slack_token"
	RuleGitLabToken        = "gitlab_token"
	RuleMulticaToken       = "multica_token"
	RuleGoogleAPIKey       = "google_api_key"
	RuleStripeKey          = "stripe_key"
	RuleConnectionPassword = "connection_string_password"
)

// Match is one secret Find located. Start and End are byte offsets of the
// secret itself, which for some rules is only part of what the pattern
// matched (the password inside a connection string).
type Match struct {
	Rule        string
	Start, End  int
	Value       string
	Placeholder string
}

// Find returns the secrets in s that a reportable rule recognises, ordered by
// position and never overlapping. Where two rules claim the same text, the
// earlier pattern wins, as it does in Text.
func Find(s string) []Match {
	var out []Match
	for _, p := range patterns {
		if p.rule == "" {
			continue
		}
		group := p.re.SubexpIndex("secret")
		for _, loc := range p.re.FindAllStringSubmatchIndex(s, -1) {
			start, end := loc[0], loc[1]
			if group > 0 {
				start, end = loc[2*group], loc[2*group+1]
			}
			if start < 0 || overlaps(out, start, end) {
				continue
			}
			placeholder := p.placeholder
			if placeholder == "" {
				placeholder = p.replacement
			}
			out = append(out, Match{Rule: p.rule, Start: start, End: end, Value: s[start:end], Placeholder: placeholder})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func overlaps(ms []Match, start, end int) bool {
	for _, m := range ms {
		if start < m.End && m.Start < end {
			return true
		}
	}
	return false
}

// Replace returns s with each match's secret swapped for its placeholder. The
// matches must come from Find(s).
func Replace(s string, ms []Match) string {
	var b strings.Builder
	prev := 0
	for _, m := range ms {
		b.WriteString(s[prev:m.Start])
		b.WriteString(m.Placeholder)
		prev = m.End
	}
	b.WriteString(s[prev:])
	return b.String()
}

var maskPrefix = regexp.MustCompile(`^(?:[a-z]+[-_]){1,3}`)

// Mask is a preview of a secret that is safe to store and show: the prefix
// that names its kind (ghp_, sk-proj-) and, for long tokens, the last four
// characters, which is how providers list keys. Passwords and private keys
// show nothing of their own.
func Mask(m Match) string {
	switch m.Rule {
	case RuleConnectionPassword:
		return "••••"
	case RulePrivateKey:
		return "-----BEGIN PRIVATE KEY----- ••••"
	}
	prefix := maskPrefix.FindString(m.Value)
	if strings.HasPrefix(m.Value, "AKIA") || strings.HasPrefix(m.Value, "AIza") {
		prefix = m.Value[:4]
	}
	rest := []rune(m.Value[len(prefix):])
	if len(rest) < 16 {
		return prefix + "••••"
	}
	return prefix + "••••••••" + string(rest[len(rest)-4:])
}

// maxRedactDepth bounds the walk in redactValue. Tool inputs are decoded from
// daemon-supplied JSON, so nesting depth is attacker-influenced; without a
// bound a pathologically nested payload would recurse until the stack blows and
// take the process down. Real tool inputs nest a handful of levels at most, so
// this only ever trips on abuse.
const maxRedactDepth = 32

// depthLimitPlaceholder replaces anything below maxRedactDepth. Returning the
// raw value there would hand back an unscrubbed string, which is exactly what
// this package exists to prevent, so the fail-safe direction is to drop it.
const depthLimitPlaceholder = "[REDACTED DEPTH LIMIT]"

// InputMap returns a copy of m with every string value passed through Text,
// including strings nested inside maps and slices.
//
// The nested walk is load-bearing, not defensive tidying: providers record
// structured tool inputs, and Codex records a file edit as
// changes[]{path, diff, content}. A top-level-only pass leaves a credential
// inside a patch body — or the full contents of a deleted .env — untouched on
// its way to the database and the WebSocket broadcast.
func InputMap(m map[string]any) map[string]any {
	return redactMap(m, 0)
}

func redactMap(m map[string]any, depth int) map[string]any {
	if m == nil {
		return nil
	}
	if depth >= maxRedactDepth {
		return map[string]any{"_": depthLimitPlaceholder}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = redactValue(v, depth+1)
	}
	return out
}

// redactValue scrubs a single decoded JSON value, recursing through the
// composite shapes json.Unmarshal produces plus []string, which providers use
// for argv-style inputs.
//
// Composites are copied rather than scrubbed in place: the caller still holds
// the original map and keeps using it after redaction (the daemon handler logs
// and re-reads it), so mutating through the shared reference would be a
// surprise at a distance.
func redactValue(v any, depth int) any {
	if depth >= maxRedactDepth {
		return depthLimitPlaceholder
	}
	switch t := v.(type) {
	case string:
		return Text(t)
	case map[string]any:
		return redactMap(t, depth)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactValue(e, depth+1)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = Text(e)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, e := range t {
			out[k] = Text(e)
		}
		return out
	default:
		return v
	}
}

// Text scans the input string for known secret patterns and replaces
// matches with safe placeholders.
//
// It deliberately does NOT mask the local home directory. That masking used to
// live here, but it was never a boundary: anyone who can read a transcript
// already sees repository paths, file contents, commands and diffs, so hiding
// one path segment protected nothing while making paths unusable for copy-paste
// and debugging. It was also incoherent about whose home directory it hid —
// Content/Output are redacted in the server's ingest handler, so a hosted
// deployment matched them against the *server's* home, never the machine
// running the agent. Transcript visibility is an authorization concern and is
// handled at that layer, not by string replacement here.
func Text(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	return s
}
