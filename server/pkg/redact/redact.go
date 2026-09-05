// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"regexp"
	"strings"
)

// secretPattern pairs a compiled regex with its replacement text.
//
// partialAtEOF recognises a *prefix* of this pattern's secret sitting at the
// very end of a string — text that is not yet a full match but would become one
// if more bytes followed. PreviewPrefix uses it to decide where a bounded
// window has to be cut: a secret whose tail was left outside the window can
// never be matched by re, so the visible head would survive redaction in
// plaintext. See PreviewPrefix for the full argument.
//
// Every entry MUST supply either partialAtEOF or an entry in
// multilinePartialFuncs. TestEverySecretPatternHasPartial enforces this, so a
// newly added rule cannot silently ship without truncation coverage.
type secretPattern struct {
	re           *regexp.Regexp
	partialAtEOF *regexp.Regexp
	replacement  string
}

// partialFor builds an EOF-anchored matcher for "an incomplete occurrence of a
// secret that starts with one of lits".
//
// Two shapes are accepted, and the distinction is load-bearing:
//
//   - a STRICT prefix of a literal, with nothing after it ("Beare", "AKI").
//     Nothing may follow, because the literal itself is still unfinished.
//   - the COMPLETE literal followed by an optional, partial value ("AKIA1234").
//     The value tail must be optional: the window can end exactly at the
//     literal, before the separator or value has been emitted.
//
// Letting a strict prefix carry a value charset is what makes this dangerous
// rather than merely conservative: "a" is a prefix of "amqp://", so
// `a[^\s]*\z` matches the tail of any single-line output and would cut the
// whole preview away.
//
// leftBoundary MUST mirror the full rule's own left anchor. A partial matcher
// that is stricter than its full rule is not conservative — it is a hole. The
// AWS, connection-string and generic-credential rules have no \b, so they match
// a keyword glued to preceding text ("MY_AWS_SECRET_ACCESS_KEY="); a partial
// matcher that demanded \b there would skip exactly those occurrences and leave
// the credential's visible head in the preview, which is the leak this whole
// mechanism exists to prevent.
func partialFor(lits []string, valueTail string, leftBoundary boundaryKind) *regexp.Regexp {
	strict := make([]string, 0, 16)
	full := make([]string, 0, len(lits))
	for _, lit := range lits {
		for i := len(lit) - 1; i >= 1; i-- {
			strict = append(strict, regexp.QuoteMeta(lit[:i]))
		}
		full = append(full, regexp.QuoteMeta(lit))
	}
	anchor := ""
	if leftBoundary == wordBoundaryLeft {
		anchor = `\b`
	}
	return regexp.MustCompile(`(?i)` + anchor + `(?:` +
		`(?:` + strings.Join(strict, "|") + `)` +
		`|` +
		`(?:` + strings.Join(full, "|") + `)(?:` + valueTail + `)?` +
		`)\z`)
}

// boundaryKind records whether a full rule anchors its opener on a word
// boundary. It exists so the choice is stated per rule rather than assumed.
type boundaryKind int

const (
	// wordBoundaryLeft mirrors a full rule written with a leading \b.
	wordBoundaryLeft boundaryKind = iota
	// noBoundaryLeft mirrors a full rule with no left anchor, which therefore
	// matches its keyword mid-identifier.
	noBoundaryLeft
)

// Literal openers, kept next to the rules that consume them so the two cannot
// drift apart.
var (
	awsSecretKeyNames = []string{"aws_secret_access_key", "secret_access_key", "secretaccesskey", "secret_accesskey", "secretaccess_key"}
	githubTokenPrefix = []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"}
	slackTokenPrefix  = []string{"xoxb-", "xoxp-", "xoxo-", "xoxr-", "xoxa-", "xoxs-", "xoxe-"}
	stripeKeyPrefix   = []string{"sk_live_", "rk_live_"}
	// Every spelling the full rule accepts. Its scheme group is
	// (?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://, so the optional "ql"
	// applies to each base — "mysqlql://" and "redisql://" are matched by the
	// full rule and therefore need partial coverage too. Listing only the
	// realistic spellings would leave the others matchable but uncuttable.
	connectionSchemes = buildConnectionSchemes()
	genericSecretKeys = []string{
		"API_KEY", "API_SECRET", "SECRET_KEY", "SECRET", "ACCESS_TOKEN", "AUTH_TOKEN",
		"PRIVATE_KEY", "DATABASE_URL", "DB_PASSWORD", "DB_URL", "REDIS_URL", "PASSWORD", "TOKEN",
	}
)

// buildConnectionSchemes expands the full rule's scheme grammar into concrete
// openers. Derived rather than hand-listed so the two cannot drift: if a base
// is added to the regex, it is added here in the same edit.
func buildConnectionSchemes() []string {
	bases := []string{"postgres", "mysql", "mongodb", "redis", "amqp"}
	out := make([]string, 0, len(bases)*2)
	for _, base := range bases {
		out = append(out, base+"ql://", base+"://")
	}
	return out
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// AWS access key IDs (always start with AKIA)
	{
		re:           regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
		partialAtEOF: partialFor([]string{"AKIA"}, `[0-9A-Z]{0,16}`, wordBoundaryLeft),
		replacement:  "[REDACTED AWS KEY]",
	},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{
		re:           regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}`),
		partialAtEOF: partialFor(awsSecretKeyNames, `\s*(?:[=:]\s*[A-Za-z0-9/+=]{0,40})?`, noBoundaryLeft),
		replacement:  "[REDACTED AWS SECRET]",
	},

	// PEM private keys (multi-line)
	// The partial case is handled by pemPartialStart rather than a
	// partialAtEOF regex: RE2 has no lookahead, so "a BEGIN marker with no
	// matching END" cannot be expressed as one anchored pattern.
	{
		re:          regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`),
		replacement: "[REDACTED PRIVATE KEY]",
	},

	// GitHub tokens (classic PAT, OAuth, user-to-server, server-to-server, refresh)
	{
		re:           regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`),
		partialAtEOF: partialFor(githubTokenPrefix, `[A-Za-z0-9_]{0,255}`, wordBoundaryLeft),
		replacement:  "[REDACTED GITHUB TOKEN]",
	},

	// GitHub fine-grained personal access tokens use the github_pat_ prefix,
	// which the classic ghp_/gho_/... pattern above does not cover. Without
	// this line a fine-grained PAT emitted in agent output leaks unredacted
	// to the database and WebSocket broadcast.
	{
		re:           regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,255}\b`),
		partialAtEOF: partialFor([]string{"github_pat_"}, `[A-Za-z0-9_]{0,255}`, wordBoundaryLeft),
		replacement:  "[REDACTED GITHUB TOKEN]",
	},

	// OpenAI / Anthropic API keys
	{
		re:           regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
		partialAtEOF: partialFor([]string{"sk-"}, `[A-Za-z0-9_-]*`, wordBoundaryLeft),
		replacement:  "[REDACTED API KEY]",
	},

	// Slack bot/user/legacy tokens. The char class includes 'e' so the
	// newer xoxe- config/refresh tokens are covered alongside xoxb/p/o/r/a/s.
	{
		re:           regexp.MustCompile(`\bxox[bporase]-[A-Za-z0-9\-]{10,}\b`),
		partialAtEOF: partialFor(slackTokenPrefix, `[A-Za-z0-9\-]*`, wordBoundaryLeft),
		replacement:  "[REDACTED SLACK TOKEN]",
	},

	// Slack app-level tokens use the xapp- prefix, which the xox*- rule above
	// does not match. Without this an app-level token echoed in agent output
	// leaks unredacted to the DB / WebSocket broadcast.
	{
		re:           regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}\b`),
		partialAtEOF: partialFor([]string{"xapp-"}, `[A-Za-z0-9-]*`, wordBoundaryLeft),
		replacement:  "[REDACTED SLACK TOKEN]",
	},

	// GitLab personal access tokens
	{
		re:           regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`),
		partialAtEOF: partialFor([]string{"glpat-"}, `[A-Za-z0-9_-]*`, wordBoundaryLeft),
		replacement:  "[REDACTED GITLAB TOKEN]",
	},

	// Google API keys always start with the AIza prefix and are 39 chars total
	// (AIza + 35). Capture and restore the trailing delimiter so keys ending in
	// a non-word character such as '-' are still redacted.
	{
		re:           regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)`),
		partialAtEOF: partialFor([]string{"AIza"}, `[0-9A-Za-z_-]{0,35}`, wordBoundaryLeft),
		replacement:  "[REDACTED GOOGLE API KEY]$1",
	},

	// Stripe secret / restricted live keys (sk_live_ / rk_live_). The sk-
	// rule above only matches the hyphen form used by OpenAI/Anthropic; Stripe
	// uses an underscore, so live keys are not covered without this. Publishable
	// keys (pk_live_) are intentionally excluded — they are not secret.
	{
		re:           regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`),
		partialAtEOF: partialFor(stripeKeyPrefix, `[0-9A-Za-z]*`, wordBoundaryLeft),
		replacement:  "[REDACTED STRIPE KEY]",
	},

	// JWT tokens (three base64url segments)
	{
		re:           regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
		partialAtEOF: partialFor([]string{"ey"}, `[A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]*){0,2}`, wordBoundaryLeft),
		replacement:  "[REDACTED JWT]",
	},

	// Generic "Bearer <token>" in output
	{
		re:           regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`),
		partialAtEOF: partialFor([]string{"Bearer"}, `\s+[A-Za-z0-9\-._~+/]*=*`, wordBoundaryLeft),
		replacement:  "Bearer [REDACTED]",
	},

	// Connection strings with embedded passwords
	{
		re:           regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`),
		partialAtEOF: partialFor(connectionSchemes, `[^\s]*`, noBoundaryLeft),
		replacement:  "[REDACTED CONNECTION STRING]@",
	},

	// Generic key=value patterns for common secret env var names
	{
		re:           regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`),
		partialAtEOF: partialFor(genericSecretKeys, `\s*(?:[=:]\s*\S*)?`, noBoundaryLeft),
		replacement:  "[REDACTED CREDENTIAL]",
	},
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

// pemBeginMarker / pemEndMarker locate the delimiters of the multi-line PEM
// rule. The end marker must be matched as a whole: a window cut in the middle
// of "-----END RSA PRIVATE KEY-----" contains the substring "-----END" while
// the key is in fact still open, and a substring test would call it closed.
var (
	pemBeginMarker = regexp.MustCompile(`-----BEGIN[A-Z\s]*PRIVATE KEY-----`)
	pemEndMarker   = regexp.MustCompile(`-----END[A-Z\s]*PRIVATE KEY-----`)

	// pemBeginPartial matches an incomplete BEGIN marker at end of string.
	//
	// Derived from the same grammar as pemBeginMarker rather than from a list
	// of known marker spellings. An allowlist drifts in both directions: the
	// full rule accepts any [A-Z\s]* algorithm name, so "-----BEGIN CUSTOM
	// PRIVATE KEY-----" is redacted in full but was invisible to a canonical
	// allowlist; meanwhile the allowlist carried "PGP PRIVATE KEY BLOCK", which
	// the full rule does not accept at all. Both directions are maintenance
	// hazards, and the first is a leak.
	//
	// The alternation walks the fixed literal "-----BEGIN" one byte at a time,
	// then allows a complete "-----BEGIN" followed by any prefix of
	// "[A-Z\s]*PRIVATE KEY-----" — the same tail the full rule matches.
	pemBeginPartial = regexp.MustCompile(
		`(?:` + literalPrefixAlternation("-----BEGIN") + `|` +
			`-----BEGIN[A-Z\s]*` + literalPrefixAlternation("PRIVATE KEY-----") + `|` +
			`-----BEGIN[A-Z\s]*` +
			`)\z`)
)

// literalPrefixAlternation returns a group matching any non-empty prefix of
// lit, longest first so the regex engine prefers the longest match.
func literalPrefixAlternation(lit string) string {
	alts := make([]string, 0, len(lit))
	for n := len(lit); n >= 1; n-- {
		alts = append(alts, regexp.QuoteMeta(lit[:n]))
	}
	return "(?:" + strings.Join(alts, "|") + ")"
}

// strictPrefixAlternation returns a group matching any strict prefix of any
// literal, longest first so the regex engine prefers the longest cut.
func strictPrefixAlternation(lits []string) string {
	seen := make(map[string]bool)
	alts := make([]string, 0, 64)
	longest := 0
	for _, lit := range lits {
		if len(lit) > longest {
			longest = len(lit)
		}
	}
	for n := longest - 1; n >= 1; n-- {
		for _, lit := range lits {
			if n >= len(lit) {
				continue
			}
			p := lit[:n]
			if seen[p] {
				continue
			}
			seen[p] = true
			alts = append(alts, regexp.QuoteMeta(p))
		}
	}
	return "(?:" + strings.Join(alts, "|") + ")"
}

// pemPartialStart reports where an unterminated PEM private key begins, or -1.
// This is the multi-line counterpart to partialAtEOF; see secretPattern.
func pemPartialStart(s string) int {
	// Skip past any complete key first. Its END marker ends in "-----", which
	// is itself a strict prefix of a BEGIN marker, so a scan over the whole
	// string would read a properly closed key as an unfinished one.
	searchFrom := 0
	for {
		loc := pemBeginMarker.FindStringIndex(s[searchFrom:])
		if loc == nil {
			break
		}
		begin := searchFrom + loc[0]
		end := pemEndMarker.FindStringIndex(s[begin:])
		if end == nil {
			// Opened and never closed: everything from here on is key material.
			return begin
		}
		searchFrom = begin + end[1]
	}
	if loc := pemBeginPartial.FindStringIndex(s[searchFrom:]); loc != nil {
		return searchFrom + loc[0]
	}
	return -1
}

// PreviewPrefix returns the longest prefix of s that is safe to keep when the
// rest of s is being discarded, together with the redacted form of that prefix.
//
// Truncating first and redacting afterwards is not safe, and the failure is not
// hypothetical: cutting "postgres://user:<pw>@host" before the '@', or a JWT
// before its second '.', leaves text that no pattern in this package matches,
// so the visible head of the credential is stored and broadcast in plaintext.
// Redacting the whole input first would be safe but costs ~0.6us/byte over an
// unbounded tool output — several seconds of the user's CPU for a payload whose
// tail is about to be thrown away.
//
// So: scan only the window, and before redacting it, cut away any trailing text
// that could be the beginning of a secret whose completion lies outside. What
// remains cannot contain a straddling secret, and the ordinary redaction pass
// handles everything wholly inside.
//
// The returned prefix is a rune boundary. Callers still own the byte budget:
// redaction can grow text (a 7-byte "TOKEN=x" becomes a 21-byte placeholder),
// so redactedPrefix may be longer than prefix.
func PreviewPrefix(s string) (prefix, redactedPrefix string) {
	cut := len(s)
	for _, p := range patterns {
		if p.partialAtEOF == nil {
			continue
		}
		if loc := p.partialAtEOF.FindStringIndex(s); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	if i := pemPartialStart(s); i >= 0 && i < cut {
		cut = i
	}
	prefix = s[:cut]
	return prefix, Text(prefix)
}
