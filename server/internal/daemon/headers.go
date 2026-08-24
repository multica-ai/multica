package daemon

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

// ExtraHeadersEnv is the name of the environment variable the daemon
// consults at startup for extra outbound HTTP headers (used to attach
// per-deployment authorization headers, e.g. an internal reverse-proxy
// shared secret, to every server call). PR 1 of TIM-142 only parses and
// stores the value; PR 2 wires it into the transport.
const ExtraHeadersEnv = "MULTICA_EXTRA_HEADERS"

// ExtraHeaderFromFlag parses one `--extra-header` token. Both the
// `Name=value` (cobra-style) and `Name: Value` (HTTP-header-style) forms
// are accepted; whichever separator appears first wins. This matches
// what every operator example in SELF_HOSTING_ADVANCED.md and .env.example
// already does (`--extra-header "Cf-Access-Client-Id: ..."`, not
// `--extra-header "Cf-Access-Client-Id=..."`), so a token typed in the
// documented shape no longer trips a "expected 'name=value'" error.
//
// Callers that need the multi-line `Name: Value` spec (env-var /
// config-file style) should use ExtraHeadersFromSpec instead. The
// returned name has surrounding whitespace trimmed to match the parser's
// contract; the value is returned verbatim so multi-word values like
// `Bearer abc def` survive a single separator.
func ExtraHeaderFromFlag(token string) (string, string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", "", fmt.Errorf("extra-header token is empty")
	}
	// Pick whichever separator (`=` or `:`) appears first. Leftmost
	// wins, which means a value containing one of the two separators
	// still parses correctly (the other one becomes part of the value).
	// Names must not contain `:` (enforced by ValidateHeaderNameValue),
	// so the colon form is safe — it cannot be confused with a value
	// that itself starts with `=`.
	eq := strings.IndexByte(token, '=')
	colon := strings.IndexByte(token, ':')
	sep := -1
	switch {
	case eq < 0 && colon < 0:
		return "", "", fmt.Errorf("extra-header %q: expected 'Name: Value' or 'Name=value'", token)
	case eq < 0:
		sep = colon
	case colon < 0:
		sep = eq
	case colon < eq:
		sep = colon
	default:
		sep = eq
	}
	name := strings.TrimSpace(token[:sep])
	// Trim leading/trailing whitespace from the value too — the colon
	// form is the documented `Name: Value` shape, and per RFC 7230 a
	// single OWS (optional whitespace) after the colon is not part of
	// the value. Internal whitespace (tabs / spaces between words)
	// survives; only the edges are trimmed. ExtraHeadersFromSpec has
	// used this convention since PR 1, so the flag form now agrees.
	value := strings.TrimSpace(token[sep+1:])
	if name == "" {
		return "", "", fmt.Errorf("extra-header name must be non-empty (got token %q)", token)
	}
	if err := ValidateHeaderNameValue(name, value); err != nil {
		return "", "", err
	}
	return name, value, nil
}

// ExtraHeadersFromSpec parses a multi-line `Name: Value` spec (one
// header per line, `#` and blank lines ignored) into a net/http Header.
// An empty / whitespace-only spec returns (nil, nil) so callers can treat
// "unset" and "no headers" uniformly. The first parse error wins; a
// partially-parsed result is discarded so a misconfigured line never
// injects a header that did pass validation on its own.
//
// Lines are split on `\n` and trailing `\r` stripped (CRLF from
// Windows-edited env vars or config files). Embedded CR is rejected
// rather than silently folded to whitespace so we don't accept
// header-injection payloads that ride on Windows line endings.
func ExtraHeadersFromSpec(spec string) (http.Header, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	hdr := http.Header{}
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
		hdr.Add(name, value)
	}
	if len(hdr) == 0 {
		return nil, nil
	}
	return hdr, nil
}

// ValidateHeaderNameValue enforces the minimum invariants an HTTP header
// pair needs to be safe to attach to an outbound request:
//
//   - name must be non-empty and contain no CR, LF, NUL, or colon. Colons
//     are the field/value separator on the wire, so a name that contains
//     one is a header-injection primitive; the same is true of CR/LF/NUL,
//     which Go's net/http client (and most reverse proxies) reject
//     outright.
//   - value may be empty (e.g. `X-Empty:`) but must contain no CR, LF, or
//     NUL — those let an attacker smuggle additional headers or truncate
//     the request.
//
// We deliberately re-check rather than relying on net/http's
// ValidHeaderFieldName / ValidHeaderFieldValue so the error wraps with our
// domain-specific message and gives precise feedback on the offending byte.
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
	if label := firstCRLFNUL(value); label != "" {
		return fmt.Errorf("header value for %q: contains %s (header-injection primitive)", name, label)
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
// Errors are returned for invalid headers in either source (CR/LF/NUL
// or colons in name) so a misconfigured env var or config.json fails
// the daemon startup instead of silently dropping some headers.
func resolveExtraHeaders(override http.Header, file map[string]string) (http.Header, error) {
	if override != nil {
		return override, nil
	}
	if raw := strings.TrimSpace(os.Getenv(ExtraHeadersEnv)); raw != "" {
		return ExtraHeadersFromSpec(raw)
	}
	if len(file) == 0 {
		return nil, nil
	}
	hdr := http.Header{}
	for name, value := range file {
		if err := ValidateHeaderNameValue(name, value); err != nil {
			return nil, fmt.Errorf("extra_headers in config.json: %w", err)
		}
		hdr.Add(name, value)
	}
	if len(hdr) == 0 {
		return nil, nil
	}
	return hdr, nil
}
