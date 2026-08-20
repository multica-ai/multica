package tagaccess

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	HTTPAssertionHeader          = "X-VIBES-Tag-Assertion"
	HTTPAssertionSignatureHeader = "X-VIBES-Tag-Assertion-Signature"
	HTTPAssertionKeyIDHeader     = "X-VIBES-Tag-Assertion-Key-ID"
	HTTPAssertionSchemaVersion   = uint64(1)
	HTTPAssertionIssuer          = "vibes-tag-gateway-v1"
	HTTPAssertionAudience        = "vibes-tag-browser-http-v1"
	WebSocketAssertionAudience   = "vibes-tag-browser-ws-v1"
	HTTPAssertionMaxLifetime     = 10 * time.Second
	httpAssertionDomain          = "vibes-tag-gateway-request-v1\n"
	maxHTTPAssertionBytes        = 16 * 1024
	maxHTTPAssertionBodyBytes    = 101 << 20
	maxHTTPAssertionTargetBytes  = 8192
	maxHTTPAssertionPartBytes    = 4096
)

var (
	ErrInvalidHTTPAssertion = errors.New("invalid VIBES Tag HTTP assertion")
	httpAssertionSafeID     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	httpAssertionMethod     = regexp.MustCompile(`^[A-Z][A-Z0-9-]{0,31}$`)
	httpAssertionSHA256     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// HTTPAssertion is the normative VIBES Tag Gateway request assertion v1.
// Field order is part of the cross-language HMAC contract.
type HTTPAssertion struct {
	SchemaVersion              uint64 `json:"schemaVersion"`
	Issuer                     string `json:"issuer"`
	Audience                   string `json:"audience"`
	KeyID                      string `json:"keyId"`
	Method                     string `json:"method"`
	Path                       string `json:"path"`
	Query                      string `json:"query"`
	BodySHA256                 string `json:"bodySha256"`
	UserID                     string `json:"userId"`
	WorkspaceID                string `json:"workspaceId"`
	SessionID                  string `json:"sessionId"`
	AccountEpoch               uint64 `json:"accountEpoch"`
	SessionWorkspaceGeneration uint64 `json:"sessionWorkspaceGeneration"`
	AuthorityVersion           uint64 `json:"authorityVersion"`
	MembershipGeneration       uint64 `json:"membershipGeneration"`
	IssuedAt                   int64  `json:"issuedAt"`
	ExpiresAt                  int64  `json:"expiresAt"`
	RequestID                  string `json:"requestId"`
	Nonce                      string `json:"nonce"`
}

type HTTPAssertionVerifier struct {
	keys           map[string][]byte
	clock          Clock
	audience       string
	requiredMethod string
}

// NewHTTPAssertionVerifier constructs the HTTP-only consumer. Issuer,
// audience, and schema are fixed contract constants rather than configuration.
func NewHTTPAssertionVerifier(keys map[string][]byte, clock Clock) (*HTTPAssertionVerifier, error) {
	return newHTTPAssertionVerifier(keys, clock, HTTPAssertionAudience, "")
}

// NewWebSocketAssertionVerifier constructs the browser WebSocket consumer
// using the same #299 assertion contract and key material as HTTP admission.
// Its audience is fixed and only a GET upgrade with an empty body is valid.
func NewWebSocketAssertionVerifier(keys map[string][]byte, clock Clock) (*HTTPAssertionVerifier, error) {
	return newHTTPAssertionVerifier(keys, clock, WebSocketAssertionAudience, http.MethodGet)
}

func newHTTPAssertionVerifier(keys map[string][]byte, clock Clock, audience, requiredMethod string) (*HTTPAssertionVerifier, error) {
	if len(keys) == 0 || !configuredDependency(clock) {
		return nil, ErrInvalidHTTPAssertion
	}
	if audience != HTTPAssertionAudience && audience != WebSocketAssertionAudience {
		return nil, ErrInvalidHTTPAssertion
	}
	cloned := make(map[string][]byte, len(keys))
	for keyID, key := range keys {
		if !httpAssertionSafeID.MatchString(keyID) || len(key) < sha256.Size {
			return nil, ErrInvalidHTTPAssertion
		}
		cloned[keyID] = append([]byte(nil), key...)
	}
	return &HTTPAssertionVerifier{keys: cloned, clock: clock, audience: audience, requiredMethod: requiredMethod}, nil
}

func (v *HTTPAssertionVerifier) configured() bool {
	return v != nil && len(v.keys) > 0 && configuredDependency(v.clock)
}

// VerifyRequest authenticates all three detached assertion headers and binds
// their canonical path, query, method, and raw unsafe body to the request.
func (v *HTTPAssertionVerifier) VerifyRequest(request *http.Request) (HTTPAssertion, error) {
	if !v.configured() || request == nil {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	if v.requiredMethod == http.MethodGet && (request.ContentLength > 0 || len(request.TransferEncoding) > 0) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	payloadHeader, ok := exactlyOneHeader(request, HTTPAssertionHeader, maxHTTPAssertionBytes)
	if !ok {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	signatureHeader, ok := exactlyOneHeader(request, HTTPAssertionSignatureHeader, 128)
	if !ok {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	keyIDHeader, ok := exactlyOneHeader(request, HTTPAssertionKeyIDHeader, 128)
	if !ok || !httpAssertionSafeID.MatchString(keyIDHeader) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}

	payload, err := base64.RawURLEncoding.Strict().DecodeString(payloadHeader)
	if err != nil || len(payload) == 0 || len(payload) > maxHTTPAssertionBytes {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(signatureHeader)
	if err != nil || len(signature) != sha256.Size {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	var assertion HTTPAssertion
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&assertion); err != nil {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	canonical, err := CanonicalHTTPAssertion(assertion)
	if err != nil || assertion.KeyID != keyIDHeader || assertion.Audience != v.audience ||
		(v.requiredMethod != "" && assertion.Method != v.requiredMethod) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	canonicalPayload := canonical[len(httpAssertionDomain):]
	if !bytes.Equal(canonicalPayload, payload) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	key, ok := v.keys[assertion.KeyID]
	if !ok {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(canonical)
	if !hmac.Equal(signature, mac.Sum(nil)) || !validHTTPAssertionTime(assertion, v.clock.Now()) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}

	path, query, err := CanonicalHTTPRequestTarget(request)
	if err != nil || assertion.Method != request.Method || assertion.Path != path || assertion.Query != query {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	if safeHTTPMethod(request.Method) {
		body, err := readAndRestoreHTTPBody(request)
		if err != nil || len(body) != 0 || assertion.BodySHA256 != "" {
			return HTTPAssertion{}, ErrInvalidHTTPAssertion
		}
		return assertion, nil
	}
	body, err := readAndRestoreHTTPBody(request)
	if err != nil {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	digest := sha256.Sum256(body)
	if assertion.BodySHA256 != hex.EncodeToString(digest[:]) {
		return HTTPAssertion{}, ErrInvalidHTTPAssertion
	}
	return assertion, nil
}

func exactlyOneHeader(request *http.Request, name string, maxLength int) (string, bool) {
	values := request.Header.Values(name)
	returnValue := ""
	if len(values) == 1 {
		returnValue = values[0]
	}
	return returnValue, len(values) == 1 && returnValue != "" && len(returnValue) <= maxLength
}

// HasGatewayAssertionHeaders reports whether any part of the detached #299
// transport is present. Adapters use it only to select the browser admission
// path; the verifier still requires exactly one of all three headers.
func HasGatewayAssertionHeaders(request *http.Request) bool {
	return request != nil && (len(request.Header.Values(HTTPAssertionHeader)) > 0 ||
		len(request.Header.Values(HTTPAssertionSignatureHeader)) > 0 ||
		len(request.Header.Values(HTTPAssertionKeyIDHeader)) > 0)
}

// CanonicalHTTPAssertion returns the domain-separated normative HMAC bytes.
func CanonicalHTTPAssertion(assertion HTTPAssertion) ([]byte, error) {
	if !validHTTPAssertionShape(assertion) {
		return nil, ErrInvalidHTTPAssertion
	}
	payload, err := json.Marshal(assertion)
	if err != nil {
		return nil, ErrInvalidHTTPAssertion
	}
	canonical := make([]byte, 0, len(httpAssertionDomain)+len(payload))
	canonical = append(canonical, httpAssertionDomain...)
	canonical = append(canonical, payload...)
	return canonical, nil
}

func validHTTPAssertionShape(assertion HTTPAssertion) bool {
	if assertion.SchemaVersion != HTTPAssertionSchemaVersion ||
		assertion.Issuer != HTTPAssertionIssuer || !validBrowserAssertionAudience(assertion.Audience) ||
		!httpAssertionSafeID.MatchString(assertion.KeyID) || !httpAssertionMethod.MatchString(assertion.Method) ||
		!validAssertionPath(assertion.Path) || len(assertion.Query) > maxHTTPAssertionPartBytes || strings.HasPrefix(assertion.Query, "?") ||
		!httpAssertionSafeID.MatchString(assertion.UserID) || !httpAssertionSafeID.MatchString(assertion.WorkspaceID) ||
		!httpAssertionSafeID.MatchString(assertion.SessionID) || !httpAssertionSafeID.MatchString(assertion.RequestID) ||
		!httpAssertionSafeID.MatchString(assertion.Nonce) ||
		assertion.AccountEpoch == 0 || assertion.AccountEpoch > maxDatabaseCounter ||
		assertion.SessionWorkspaceGeneration == 0 || assertion.SessionWorkspaceGeneration > maxDatabaseCounter ||
		assertion.AuthorityVersion == 0 || assertion.AuthorityVersion > maxDatabaseCounter ||
		assertion.MembershipGeneration == 0 || assertion.MembershipGeneration > maxDatabaseCounter ||
		assertion.IssuedAt <= 0 || assertion.ExpiresAt <= assertion.IssuedAt ||
		assertion.ExpiresAt-assertion.IssuedAt > HTTPAssertionMaxLifetime.Milliseconds() {
		return false
	}
	if safeHTTPMethod(assertion.Method) {
		return assertion.BodySHA256 == ""
	}
	return httpAssertionSHA256.MatchString(assertion.BodySHA256)
}

func validBrowserAssertionAudience(audience string) bool {
	return audience == HTTPAssertionAudience || audience == WebSocketAssertionAudience
}

func validAssertionPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.Contains(path, "#") && len(path) <= maxHTTPAssertionPartBytes
}

func validHTTPAssertionTime(assertion HTTPAssertion, now time.Time) bool {
	issuedAt := time.UnixMilli(assertion.IssuedAt)
	expiresAt := time.UnixMilli(assertion.ExpiresAt)
	return !issuedAt.After(now) && expiresAt.After(now)
}

func readAndRestoreHTTPBody(request *http.Request) ([]byte, error) {
	if request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxHTTPAssertionBodyBytes+1))
	if err != nil || len(body) > maxHTTPAssertionBodyBytes {
		return nil, ErrInvalidHTTPAssertion
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func safeHTTPMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions || method == http.MethodTrace
}

// CanonicalHTTPRequestTarget ports canonicalizeTagGatewayTarget exactly:
// duplicate query-pair order is preserved, names/values use RFC3986 encoding,
// and percent escapes are normalized before comparison.
func CanonicalHTTPRequestTarget(request *http.Request) (string, string, error) {
	if request == nil || request.URL == nil {
		return "", "", ErrInvalidHTTPAssertion
	}
	path := request.URL.EscapedPath()
	if path == "" || !validRawTarget(path, request.URL.RawQuery) {
		return "", "", ErrInvalidHTTPAssertion
	}
	path = removeWHATWGDotSegments(strings.ReplaceAll(path, `\`, "/"))
	path, err := normalizeAssertionPath(path)
	if err != nil || len(path) > maxHTTPAssertionPartBytes {
		return "", "", ErrInvalidHTTPAssertion
	}
	query, err := canonicalAssertionQuery(request.URL.RawQuery)
	if err != nil || len(query) > maxHTTPAssertionPartBytes {
		return "", "", ErrInvalidHTTPAssertion
	}
	return path, query, nil
}

func validRawTarget(path, query string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "#") || strings.Contains(query, "#") || len(path)+len(query) > maxHTTPAssertionTargetBytes {
		return false
	}
	return validPercentEncoding(path) && validPercentEncoding(query)
}

func validPercentEncoding(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			continue
		}
		if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
			return false
		}
		index += 2
	}
	return true
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func normalizeAssertionPath(path string) (string, error) {
	var normalized strings.Builder
	normalized.Grow(len(path))
	for index := 0; index < len(path); index++ {
		if path[index] != '%' {
			normalized.WriteByte(path[index])
			continue
		}
		if index+2 >= len(path) || !isHex(path[index+1]) || !isHex(path[index+2]) {
			return "", ErrInvalidHTTPAssertion
		}
		decoded, err := hex.DecodeString(path[index+1 : index+3])
		if err != nil {
			return "", ErrInvalidHTTPAssertion
		}
		if isUnreserved(decoded[0]) {
			normalized.WriteByte(decoded[0])
		} else {
			normalized.WriteByte('%')
			normalized.WriteString(strings.ToUpper(path[index+1 : index+3]))
		}
		index += 2
	}
	return normalized.String(), nil
}

func canonicalAssertionQuery(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	pairs := strings.Split(raw, "&")
	canonical := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		decodedName, err := queryUnescape(name)
		if err != nil {
			return "", ErrInvalidHTTPAssertion
		}
		decodedValue, err := queryUnescape(value)
		if err != nil {
			return "", ErrInvalidHTTPAssertion
		}
		canonical = append(canonical, rfc3986(decodedName)+"="+rfc3986(decodedValue))
	}
	return strings.Join(canonical, "&"), nil
}

func removeWHATWGDotSegments(path string) string {
	segments := strings.Split(path, "/")
	resolved := make([]string, 1, len(segments))
	for index, segment := range segments[1:] {
		last := index == len(segments)-2
		switch {
		case isWHATWGSingleDotSegment(segment):
			if last {
				resolved = append(resolved, "")
			}
		case isWHATWGDoubleDotSegment(segment):
			if len(resolved) > 1 {
				resolved = resolved[:len(resolved)-1]
			}
			if last {
				resolved = append(resolved, "")
			}
		default:
			resolved = append(resolved, segment)
		}
	}
	return strings.Join(resolved, "/")
}

func isWHATWGSingleDotSegment(segment string) bool {
	return segment == "." || strings.EqualFold(segment, "%2e")
}

func isWHATWGDoubleDotSegment(segment string) bool {
	return segment == ".." || strings.EqualFold(segment, ".%2e") ||
		strings.EqualFold(segment, "%2e.") || strings.EqualFold(segment, "%2e%2e")
}

func queryUnescape(value string) (string, error) {
	var decoded strings.Builder
	decoded.Grow(len(value))
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '+':
			decoded.WriteByte(' ')
		case '%':
			if index+2 >= len(value) || !isHex(value[index+1]) || !isHex(value[index+2]) {
				return "", ErrInvalidHTTPAssertion
			}
			bytes, err := hex.DecodeString(value[index+1 : index+3])
			if err != nil {
				return "", ErrInvalidHTTPAssertion
			}
			decoded.WriteByte(bytes[0])
			index += 2
		default:
			decoded.WriteByte(value[index])
		}
	}
	return strings.ToValidUTF8(decoded.String(), string(utf8.RuneError)), nil
}

func rfc3986(value string) string {
	var encoded strings.Builder
	for _, current := range []byte(value) {
		if isUnreserved(current) {
			encoded.WriteByte(current)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteString(strings.ToUpper(hex.EncodeToString([]byte{current})))
	}
	return encoded.String()
}

func isUnreserved(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || strings.ContainsRune("-._~", rune(value))
}
