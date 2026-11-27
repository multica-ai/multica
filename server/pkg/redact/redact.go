// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"encoding/base64"
	"regexp"
	"strings"
	"unicode/utf8"
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

	// GitHub tokens (classic PAT, OAuth, user-to-server, server-to-server, refresh)
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// GitHub fine-grained personal access tokens use the github_pat_ prefix,
	// which the classic ghp_/gho_/... pattern above does not cover. Without
	// this line a fine-grained PAT emitted in agent output leaks unredacted
	// to the database and WebSocket broadcast.
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// OpenAI / Anthropic API keys
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[REDACTED API KEY]"},

	// Slack bot/user/legacy tokens. The char class includes 'e' so the
	// newer xoxe- config/refresh tokens are covered alongside xoxb/p/o/r/a/s.
	{regexp.MustCompile(`\bxox[bporase]-[A-Za-z0-9\-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// Slack app-level tokens use the xapp- prefix, which the xox*- rule above
	// does not match. Without this an app-level token echoed in agent output
	// leaks unredacted to the DB / WebSocket broadcast.
	{regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// GitLab personal access tokens
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), "[REDACTED GITLAB TOKEN]"},

	// Multica user PATs, daemon tokens, task tokens, and cloud-node PATs.
	// Preserve short JSON control escapes that act as token separators.
	// The space-free placeholder lets the generic TOKEN= rule below
	// replace it in one piece.
	{regexp.MustCompile(`(?:\b|(\\[bfnrt]))(?:mat|mul|mdt|mcn)_[0-9a-fA-F]{40}\b`), "${1}[REDACTED_MULTICA_TOKEN]"},

	// Google API keys always start with the AIza prefix and are 39 chars total
	// (AIza + 35). Capture and restore the trailing delimiter so keys ending in
	// a non-word character such as '-' are still redacted.
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)`), "[REDACTED GOOGLE API KEY]$1"},

	// Stripe secret / restricted live keys (sk_live_ / rk_live_). The sk-
	// rule above only matches the hyphen form used by OpenAI/Anthropic; Stripe
	// uses an underscore, so live keys are not covered without this. Publishable
	// keys (pk_live_) are intentionally excluded — they are not secret.
	{regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`), "[REDACTED STRIPE KEY]"},

	// JWT tokens (three base64url segments)
	{regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`), "[REDACTED CONNECTION STRING]@"},

	// Generic key=value patterns for common secret env var names
	{regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), "[REDACTED CREDENTIAL]"},
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
	return redactText(s, 0)
}

func redactText(s string, depth int) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	if depth >= maxBase64Depth {
		return s
	}
	return base64Run.ReplaceAllStringFunc(s, func(run string) string {
		if base64HidesSecret(run, depth) {
			return base64Placeholder
		}
		return run
	})
}

// Piping a secret through base64 (`cat .env | base64`) hides it from every
// pattern above, so a long base64-looking run is decoded and scanned too. The
// whole run is replaced when its decoded text contains a secret; runs that
// decode to binary (images, archives) or to nothing secret stay as they are.
//
// A run may be wrapped across lines, as GNU base64 prints it at 76 columns,
// and may be cut mid-quantum by output truncation. Both alphabets are
// accepted. Padding only ever ends an encoding, so a run stops there; without
// padding the run can still swallow a neighbouring word or line, which
// textHidesSecret tolerates.
var base64Run = regexp.MustCompile(`[A-Za-z0-9+/_-]{20,}(?:\r?\n[A-Za-z0-9+/_-]{4,})*={0,2}`)

const (
	base64Placeholder = "[REDACTED BASE64 SECRET]"
	// Decoded text is scanned with the same rules, so a secret encoded twice
	// is found too; deeper nesting is not worth the decode cost.
	maxBase64Depth = 2
	// The shortest run worth decoding: 24 characters carry 18 bytes, about the
	// shortest `KEY=value` line a secret pattern matches.
	minBase64Run = 24
	// Runs past this are file or image payloads, not a pasted secret, and
	// decoding them on every transcript message is not free.
	maxBase64Run = 1 << 20
)

var (
	base64LineBreaks  = strings.NewReplacer("\r", "", "\n", "")
	base64URLAlphabet = strings.NewReplacer("-", "+", "_", "/")
)

func base64HidesSecret(run string, depth int) bool {
	compact := strings.TrimRight(base64LineBreaks.Replace(run), "=")
	if len(compact) < minBase64Run || len(compact) > maxBase64Run || !mixedCase(compact) {
		return false
	}
	compact = base64URLAlphabet.Replace(compact)
	// A run glued to a preceding word (`payload_<base64>`) decodes that word
	// to junk bytes, and starts off the 4-character grid unless the word's
	// length is a multiple of 4. Each alignment is tried.
	for shift := 0; shift < 4; shift++ {
		body := compact[shift:]
		body = body[:len(body)-len(body)%4]
		decoded, err := base64.RawStdEncoding.DecodeString(body)
		if err != nil {
			continue
		}
		if textHidesSecret(decoded, depth) {
			return true
		}
	}
	return false
}

// mixedCase is a cheap first filter: base64 of anything 24 characters long
// all but certainly has both cases, while the paths and snake_case names that
// also match base64Run usually do not.
func mixedCase(s string) bool {
	var upper, lower bool
	for i := 0; i < len(s); i++ {
		c := s[i]
		upper = upper || ('A' <= c && c <= 'Z')
		lower = lower || ('a' <= c && c <= 'z')
		if upper && lower {
			return true
		}
	}
	return false
}

// Shorter than this, decoded text is too short to hold a secret.
const minBase64Text = 12

// textHidesSecret scans each stretch of text in b, UTF-8 with no control
// characters other than whitespace, for a secret. Words glued to the run or
// lines swallowed with it decode to junk bytes around the real text, so the
// junk splits the stretches rather than disqualifying the run; binary rarely
// holds a stretch long enough to scan, and never one that matches a secret.
func textHidesSecret(b []byte, depth int) bool {
	start := 0
	for i := 0; i <= len(b); {
		size := 1
		if i < len(b) {
			var r rune
			r, size = utf8.DecodeRune(b[i:])
			if (r != utf8.RuneError || size > 1) && (r >= 0x20 || r == '\n' || r == '\r' || r == '\t') && r != 0x7f {
				i += size
				continue
			}
		}
		if i-start >= minBase64Text {
			text := string(b[start:i])
			if redactText(text, depth+1) != text {
				return true
			}
		}
		i += size
		start = i
	}
	return false
}
