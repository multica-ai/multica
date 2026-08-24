package daemon

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// ExtraHeadersEnv is the name of the environment variable the daemon
// consults at startup for extra outbound HTTP headers (used to attach
// per-deployment authorization headers, e.g. an internal reverse-proxy
// shared secret, to every server call). PR 1 of TIM-142 only parses and
// stores the value; PR 2 wires it into the transport.
const ExtraHeadersEnv = "MULTICA_EXTRA_HEADERS"

// Bounds that keep the operator-facing configuration surface from being
// abused: a misconfigured MULTICA_EXTRA_HEADERS, extra_headers, or a
// long --extra-header list must not grow without limit, and the bounds
// surface as a startup error rather than producing an oversized header
// block on every daemon->server request. Both limits are deliberately
// generous for documented use cases (a Cloudflare Access token + a
// proxy shared secret + several X-Forwarded-* entries all fit well
// under either ceiling) and tight enough to prevent abuse.
const (
	// MaxExtraHeaders is the most headers the daemon will accept across
	// flag + env + config-file combined. net/http's own request-header
	// ceiling is several thousand entries; 64 leaves plenty of room for
	// the documented identity / proxy / chain-proxy use cases.
	MaxExtraHeaders = 64
	// MaxExtraHeadersBytes is the aggregate cap on name + value string
	// length across every configured extra header. 16 KiB is well above
	// the sum of every documented entry and still rejects a 1 MiB blob
	// of attacker-controlled text at startup instead of on every request.
	MaxExtraHeadersBytes = 16 * 1024
)

// reservedHeaders are header names Multica refuses to let operators
// configure via extra-headers. Multica sets all of these itself on every
// outbound request (Authorization, Content-Type) or uses them for
// routing / forwarding that an operator-configured override would break
// silently (Host, Content-Length, Connection, Upgrade). Header matching
// uses http.CanonicalHeaderKey, so operator inputs in any case fold
// (e.g. "x-workspace-id" → "X-Workspace-Id") before being compared.
//
// X-Client-* is reserved because the daemon's identity is its own auth
// signal — an operator-controlled reverse proxy that injects a forged
// X-Client-Platform could spoof the daemon's identity and bypass the
// worktree / capability gates that rely on it (MUL-5707,
// DaemonCapabilityLocalWorktreeV1). X-Forwarded-* / Forwarded are
// reserved because the daemon MUST NOT trust a reverse proxy's claim
// about its peer unless the operator's deployment is explicitly
// structured to use that header, which lives outside this feature's
// default-off scope. Sec-WebSocket-* and Upgrade are reserved because
// the wakeup WebSocket dialer is what builds the upgrade request — an
// operator-injected Sec-WebSocket-Key would collide with gorilla's
// own and the handshake would fail unpredictably.
var reservedHeaders = map[string]struct{}{
	"Authorization":  {},
	"Host":           {},
	"Content-Length": {},
	"Content-Type":   {},
	"Connection":     {},
	"Upgrade":        {},
	"X-Workspace-Id": {},
	"X-Agent-Id":     {},
	"X-Task-Id":      {},
	"Forwarded":      {},
}

// reservedHeaderPrefixes catches the multi-variant names the canonical
// map can't enumerate: every Sec-WebSocket-*, every X-Client-*, every
// X-Forwarded-*. httpguts's canonical form lowercases the s in
// "WebSocket" (Sec-Websocket-*), so the prefix is written in canonical
// form. http.CanonicalHeaderKey normalizes operator input to the same
// shape before the prefix check.
var reservedHeaderPrefixes = []string{
	"Sec-Websocket-",
	"X-Client-",
	"X-Forwarded-",
}

// IsReservedHeader reports whether name is on the daemon's
// reserved-header blocklist. The name is canonicalized via
// http.CanonicalHeaderKey before the comparison, so operator inputs in
// any case (lower, UPPER, MiXeD) all match. Used both at parse time
// (ValidateHeaderNameValue) and as defence-in-depth at the per-request
// append site so a reserved header that slipped past one path can't be
// smuggled onto the wire from another.
func IsReservedHeader(name string) bool {
	canonical := http.CanonicalHeaderKey(name)
	if _, ok := reservedHeaders[canonical]; ok {
		return true
	}
	for _, prefix := range reservedHeaderPrefixes {
		if strings.HasPrefix(canonical, prefix) {
			return true
		}
	}
	return false
}

// ExtraHeaderFromFlag parses one `--extra-header` token. Only the
// `Name: Value` (HTTP-header-style) form is accepted; the `Name=value`
// (cobra-style) form is rejected so operators don't have to remember
// which separator to use for which input source. This matches what
// every operator example in SELF_HOSTING_ADVANCED.md and .env.example
// already does (`--extra-header "Cf-Access-Client-Id: ..."`, not
// `--extra-header "Cf-Access-Client-Id=..."`), so a token typed in the
// documented shape still parses. A `Name=value` token now errors with
// a clear "use 'Name: Value'" message — the documented form has been
// the only one in production for one release, and the previous
// leftmost-wins ambiguity only existed to bridge a transitional
// state.
//
// Callers that need the multi-line `Name: Value` spec (env-var /
// config-file style) should use ExtraHeadersFromSpec instead. The
// returned name has surrounding whitespace trimmed to match the
// parser's contract; the value is returned verbatim so multi-word
// values like `Bearer abc def` survive the colon.
func ExtraHeaderFromFlag(token string) (string, string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("extra-header token is empty")
	}
	colon := strings.IndexByte(token, ':')
	if colon < 0 {
		return "", "", fmt.Errorf("extra-header %q: expected 'Name: Value' (the only accepted syntax; '=' is not supported)", token)
	}
	name := strings.TrimSpace(token[:colon])
	// Trim leading/trailing whitespace from the value too — the
	// `Name: Value` shape matches RFC 7230 where a single OWS
	// (optional whitespace) after the colon is not part of the
	// value. Internal whitespace (tabs / spaces between words)
	// survives; only the edges are trimmed.
	value := strings.TrimSpace(token[colon+1:])
	if err := ValidateHeaderNameValue(name, value); err != nil {
		return "", "", err
	}
	return name, value, nil
}

// ExtraHeadersFromSpec parses a multi-line `Name: Value` spec (one
// header per line, `#` and blank lines ignored) into a net/http Header.
// An empty / whitespace-only spec returns (nil, nil) so callers can
// treat "unset" and "no headers" uniformly. The first parse error
// wins; a partially-parsed result is discarded so a misconfigured line
// never injects a header that did pass validation on its own.
//
// Lines are split on `\n` and trailing `\r` stripped (CRLF from
// Windows-edited env vars or config files). Embedded CR is rejected
// rather than silently folded to whitespace so we don't accept
// header-injection payloads that ride on Windows line endings.
//
// Bounds (MaxExtraHeaders + MaxExtraHeadersBytes) are enforced
// incrementally as each entry is added, so an oversized spec fails at
// the offending line rather than after building a giant header map.
func ExtraHeadersFromSpec(spec string) (http.Header, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	hdr := http.Header{}
	var totalBytes int
	for _, line := range strings.Split(spec, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("extra-headers spec: line %q: expected 'Name: Value'", line)
		}
		name := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		if err := ValidateHeaderNameValue(name, value); err != nil {
			return nil, fmt.Errorf("extra-headers spec line %q: %w", line, err)
		}
		if err := addExtraHeaderWithBounds(hdr, name, value, &totalBytes); err != nil {
			return nil, fmt.Errorf("extra-headers spec: %w", err)
		}
	}
	if len(hdr) == 0 {
		return nil, nil
	}
	return hdr, nil
}

// ValidateHeaderNameValue enforces the invariants an HTTP header pair
// needs to be safe to attach to an outbound request:
//
//   - name must be non-empty and contain no CR, LF, NUL, or colon.
//     Colons are the field/value separator on the wire, so a name
//     that contains one is a header-injection primitive; the same is
//     true of CR/LF/NUL, which Go's net/http client (and most reverse
//     proxies) reject outright. httpguts.ValidHeaderFieldName
//     additionally rejects names with spaces and other token-illegal
//     characters per RFC 7230 — a header like `X Bad Name` passes
//     the CR/LF/NUL check, lets the daemon start, then fails every
//     request inside net/http, breaking the documented "fail-fast
//     at startup" contract.
//   - value may be empty (e.g. `X-Empty:`) but must contain no CR,
//     LF, or NUL — those let an attacker smuggle additional headers
//     or truncate the request. httpguts.ValidHeaderFieldValue catches
//     the long tail of value characters the spec rejects (NUL, other
//     CTL bytes).
//   - name must not be a reserved header Multica sets or routes on
//     itself; see reservedHeaders + reservedHeaderPrefixes for the
//     full list and the rationale per entry.
//
// The CR/LF/NUL check stays in front of httpguts because the explicit
// per-byte loop produces a clearer error message ("contains carriage
// return") than httpguts' generic "not a valid header field" — the
// extra context saves operators a round-trip through the docs when
// their editor silently inserted a Windows line ending.
func ValidateHeaderNameValue(name, value string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("header name must be non-empty (got %q)", name)
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return fmt.Errorf("header name %q: contains colon (header-injection primitive)", name)
	}
	if label := firstCRLFNUL(name); label != "" {
		return fmt.Errorf("header name %q: contains %s (header-injection primitive)", name, label)
	}
	if !httpguts.ValidHeaderFieldName(name) {
		return fmt.Errorf("header name %q: not a valid HTTP header field name (per RFC 7230)", name)
	}
	if label := firstCRLFNUL(value); label != "" {
		return fmt.Errorf("header value for %q: contains %s (header-injection primitive)", name, label)
	}
	if !httpguts.ValidHeaderFieldValue(value) {
		return fmt.Errorf("header value for %q: not a valid HTTP header field value (per RFC 7230)", name)
	}
	if IsReservedHeader(name) {
		return fmt.Errorf("header name %q is reserved (managed by Multica): refusing to override", name)
	}
	return nil
}

// firstCRLFNUL returns a human-readable label for the first CR, LF, or
// NUL byte present in s, or "" if none of those bytes appear. Shared
// between name and value validation so the messages read consistently.
func firstCRLFNUL(s string) string {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r':
			return "carriage return"
		case '\n':
			return "line feed"
		case 0:
			return "NUL"
		}
	}
	return ""
}

// addExtraHeaderWithBounds appends (name, value) to hdr, enforcing the
// package-wide bounds (MaxExtraHeaders + MaxExtraHeadersBytes) so a
// misconfigured spec fails one entry at a time. totalBytes is a
// running counter the caller threads through the loop so we don't
// rescan the header map per entry; both ExtraHeadersFromSpec and the
// CLI flag loop in cmd_daemon.go use this helper.
func addExtraHeaderWithBounds(hdr http.Header, name, value string, totalBytes *int) error {
	if len(hdr) >= MaxExtraHeaders {
		return fmt.Errorf("too many extra headers (limit %d)", MaxExtraHeaders)
	}
	next := *totalBytes + len(name) + len(value)
	if next > MaxExtraHeadersBytes {
		return fmt.Errorf("aggregate size %d bytes exceeds limit %d", next, MaxExtraHeadersBytes)
	}
	hdr.Add(name, value)
	*totalBytes = next
	return nil
}

// resolveExtraHeaders implements the three-tier precedence for the
// daemon's outbound HTTP headers (TIM-142): a non-nil `override` (even
// when empty) wins over the MULTICA_EXTRA_HEADERS env var; a non-empty
// env var wins over the config-file map; a non-empty config-file map
// wins over the unset default. nil at every level returns (nil, nil) so
// callers can treat "unset" and "no headers" uniformly.
//
// The non-nil-but-empty case for `override` is meaningful: an operator
// who passes `--extra-header ""` from the command line (PR 2 wiring)
// is explicitly saying "I want zero extra headers, ignore the env
// and the config file" — a different signal from "flag not passed".
// Treating it the same as `len(override) > 0` would silently fall
// through to MULTICA_EXTRA_HEADERS and put the operator's clear intent
// on the floor.
//
// Errors are returned for invalid headers in either source
// (CR/LF/NUL, colon in name, reserved name, httpguts failure, bounds
// exceeded) so a misconfigured env var or config.json fails the daemon
// startup instead of silently dropping some headers.
//
// The config-file map (`extra_headers`) is the documented
// "last-write-wins" surface for duplicate names: Go's JSON decoder
// collapses duplicate keys to the last value seen, and the daemon
// documents that behaviour rather than expanding the config schema to
// `map[string][]string` (which would be a breaking change for every
// existing config file).
func resolveExtraHeaders(override http.Header, file map[string]string) (http.Header, error) {
	if override != nil {
		if len(override) > MaxExtraHeaders {
			return nil, fmt.Errorf("extra_headers from --extra-header: too many entries (limit %d)", MaxExtraHeaders)
		}
		var total int
		for name, values := range override {
			total += len(name)
			for _, v := range values {
				total += len(v)
			}
		}
		if total > MaxExtraHeadersBytes {
			return nil, fmt.Errorf("extra_headers from --extra-header: aggregate size %d bytes exceeds limit %d", total, MaxExtraHeadersBytes)
		}
		return override, nil
	}
	if raw := strings.TrimSpace(os.Getenv(ExtraHeadersEnv)); raw != "" {
		return ExtraHeadersFromSpec(raw)
	}
	if len(file) == 0 {
		return nil, nil
	}
	hdr := http.Header{}
	var totalBytes int
	for name, value := range file {
		if err := ValidateHeaderNameValue(name, value); err != nil {
			return nil, fmt.Errorf("extra_headers in config.json: %w", err)
		}
		if err := addExtraHeaderWithBounds(hdr, name, value, &totalBytes); err != nil {
			return nil, fmt.Errorf("extra_headers in config.json: %w", err)
		}
	}
	if len(hdr) == 0 {
		return nil, nil
	}
	return hdr, nil
}
