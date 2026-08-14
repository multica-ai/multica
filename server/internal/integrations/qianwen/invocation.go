package qianwen

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	invocationSignatureVersion = "QIANWEN-HMAC-SHA256-V1"
	pairingRedeemOperation     = "binding_redeem"
	submitRequestOperation     = "request_submit"
	statusRequestOperation     = "request_status"
	taskListOperation          = "task_list"
	invocationClockSkew        = 2 * time.Minute
	maxInvocationIdentityBytes = 1024
	invocationNonceBytes       = 32
)

var (
	ErrIdentityUnavailable = errors.New("qianwen: invocation identity is unavailable")
	ErrInvalidInvocation   = errors.New("qianwen: invalid signed invocation")
	ErrStaleInvocation     = errors.New("qianwen: signed invocation is outside the replay window")
)

// InvocationMetadata is injected by fixed Qianwen system-context parameters
// and a Groovy signing script. The two identity values are opaque,
// case-sensitive ciphertext: Multica never decrypts or normalizes them.
type InvocationMetadata struct {
	OpenUserID string
	OpenUUID   string
	Timestamp  string
	Nonce      string
	Signature  string
}

// TaskListInvocation carries the signed Qianwen identity envelope for the
// caller-relative current-task list. The operation has no model-controlled
// body or query parameters, so every semantic input is covered by the fixed
// identity headers and this operation label.
type TaskListInvocation struct {
	Request  TaskListRequest
	Identity InvocationMetadata
}

// TaskListRequest is the normalized, signed pagination request. HTTP parsing
// supplies the default limit before verification; an empty cursor denotes the
// first page.
type TaskListRequest struct {
	Limit  int
	Cursor string
}

// CanonicalSubmitInvocation signs only values available to Qianwen's parsed
// body/header/config maps. The exact UTF-8 query is represented by its SHA-256
// digest so embedded newlines cannot make the newline-delimited tuple
// ambiguous; request_id is normalized to canonical UUID text on both sides.
func CanonicalSubmitInvocation(invocation SubmitInvocation) (string, error) {
	if err := validateInvocationMetadata(invocation.Identity); err != nil {
		return "", err
	}
	requestID, err := normalizeRequestID(invocation.Request.RequestID)
	if err != nil || strings.TrimSpace(invocation.Request.Query) == "" || len(invocation.Request.Query) > maxQueryBytes || !utf8.ValidString(invocation.Request.Query) {
		return "", ErrInvalidInvocation
	}
	queryDigest := sha256.Sum256([]byte(invocation.Request.Query))
	return strings.Join([]string{
		invocationSignatureVersion,
		submitRequestOperation,
		invocation.Identity.Timestamp,
		invocation.Identity.Nonce,
		invocation.Identity.OpenUserID,
		invocation.Identity.OpenUUID,
		requestID,
		hex.EncodeToString(queryDigest[:]),
	}, "\n"), nil
}

func VerifySubmitInvocationSignature(token string, invocation SubmitInvocation, now time.Time) error {
	canonical, err := CanonicalSubmitInvocation(invocation)
	if err != nil {
		return err
	}
	return verifyCanonicalInvocation(token, canonical, invocation.Identity, now)
}

func CanonicalStatusInvocation(invocation StatusInvocation) (string, error) {
	if err := validateInvocationMetadata(invocation.Identity); err != nil {
		return "", err
	}
	requestID, err := normalizeRequestID(invocation.RequestID)
	if err != nil {
		return "", ErrInvalidInvocation
	}
	return strings.Join([]string{
		invocationSignatureVersion,
		statusRequestOperation,
		invocation.Identity.Timestamp,
		invocation.Identity.Nonce,
		invocation.Identity.OpenUserID,
		invocation.Identity.OpenUUID,
		requestID,
	}, "\n"), nil
}

func VerifyStatusInvocationSignature(token string, invocation StatusInvocation, now time.Time) error {
	canonical, err := CanonicalStatusInvocation(invocation)
	if err != nil {
		return err
	}
	return verifyCanonicalInvocation(token, canonical, invocation.Identity, now)
}

func CanonicalTaskListInvocation(invocation TaskListInvocation) (string, error) {
	if err := validateInvocationMetadata(invocation.Identity); err != nil {
		return "", err
	}
	if invocation.Request.Limit < 1 || invocation.Request.Limit > 20 || len(invocation.Request.Cursor) > 512 || !validOpaqueHeader(invocation.Request.Cursor, 512) {
		return "", ErrInvalidInvocation
	}
	cursor := invocation.Request.Cursor
	if cursor == "" {
		cursor = "-"
	}
	return strings.Join([]string{
		invocationSignatureVersion,
		taskListOperation,
		invocation.Identity.Timestamp,
		invocation.Identity.Nonce,
		invocation.Identity.OpenUserID,
		invocation.Identity.OpenUUID,
		strconv.Itoa(invocation.Request.Limit),
		cursor,
	}, "\n"), nil
}

func VerifyTaskListInvocationSignature(token string, invocation TaskListInvocation, now time.Time) error {
	canonical, err := CanonicalTaskListInvocation(invocation)
	if err != nil {
		return err
	}
	return verifyCanonicalInvocation(token, canonical, invocation.Identity, now)
}

func verifyCanonicalInvocation(token, canonical string, metadata InvocationMetadata, now time.Time) error {
	invokedAt, err := parseInvocationTimestamp(metadata.Timestamp)
	if err != nil {
		return ErrInvalidInvocation
	}
	if !invocationTimestampFresh(invokedAt, now) {
		return ErrStaleInvocation
	}
	provided, err := hex.DecodeString(metadata.Signature)
	if err != nil || len(provided) != sha256.Size {
		return ErrInvalidInvocation
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ErrInvalidInvocation
	}
	return nil
}

// CanonicalPairingRedeemInvocation returns the semantic payload shared with
// the Qianwen Groovy signer. The provider exposes parsed path/query/header/body
// and config maps, but does not promise access to a raw HTTP body, escaped path,
// or Authorization header; signing only these explicit values keeps the
// contract implementable from documented inputs.
func CanonicalPairingRedeemInvocation(code string, metadata InvocationMetadata) (string, error) {
	if err := validateInvocationMetadata(metadata); err != nil {
		return "", err
	}
	if len(code) != 8 || strings.Trim(code, "0123456789") != "" {
		return "", ErrInvalidInvocation
	}
	return strings.Join([]string{
		invocationSignatureVersion,
		pairingRedeemOperation,
		metadata.Timestamp,
		metadata.Nonce,
		metadata.OpenUserID,
		metadata.OpenUUID,
		code,
	}, "\n"), nil
}

// VerifyPairingRedeemSignature authenticates every semantic redemption field
// and rejects timestamps outside a bounded replay window. The Qianwen plugin
// must expose the same qws_ value to its private Groovy config parameter; the
// platform's Bearer configuration is not documented as script-readable.
// Durable nonce replay prevention is performed by the redemption transaction.
func VerifyPairingRedeemSignature(token, code string, metadata InvocationMetadata, now time.Time) error {
	invokedAt, err := verifyPairingRedeemMAC(token, code, metadata)
	if err != nil {
		return err
	}
	if !invocationTimestampFresh(invokedAt, now) {
		return ErrStaleInvocation
	}
	return nil
}

// verifyPairingRedeemMAC validates the signed semantic payload without making
// a freshness decision. The Service uses this split to return an already
// committed terminal outcome after a provider loses the HTTP response, while
// still rejecting every previously unseen request outside the clock window.
func verifyPairingRedeemMAC(token, code string, metadata InvocationMetadata) (time.Time, error) {
	canonical, err := CanonicalPairingRedeemInvocation(code, metadata)
	if err != nil {
		return time.Time{}, err
	}
	invokedAt, err := parseInvocationTimestamp(metadata.Timestamp)
	if err != nil {
		return time.Time{}, ErrInvalidInvocation
	}
	provided, err := hex.DecodeString(metadata.Signature)
	if err != nil || len(provided) != sha256.Size {
		return time.Time{}, ErrInvalidInvocation
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return time.Time{}, ErrInvalidInvocation
	}
	return invokedAt, nil
}

func invocationTimestampFresh(invokedAt, now time.Time) bool {
	delta := now.Sub(invokedAt)
	return delta >= -invocationClockSkew && delta <= invocationClockSkew
}

func validateInvocationMetadata(metadata InvocationMetadata) error {
	if metadata.OpenUserID == "" || metadata.OpenUUID == "" {
		return ErrIdentityUnavailable
	}
	if !validOpaqueHeader(metadata.OpenUserID, maxInvocationIdentityBytes) || !validOpaqueHeader(metadata.OpenUUID, maxInvocationIdentityBytes) {
		return ErrInvalidInvocation
	}
	if _, err := parseInvocationTimestamp(metadata.Timestamp); err != nil {
		return ErrInvalidInvocation
	}
	if len(metadata.Nonce) != invocationNonceBytes || !validOpaqueHeader(metadata.Nonce, invocationNonceBytes) {
		return ErrInvalidInvocation
	}
	return nil
}

func validOpaqueHeader(value string, maxBytes int) bool {
	return len(value) <= maxBytes && !strings.ContainsAny(value, "\r\n\x00")
}

func parseInvocationTimestamp(raw string) (time.Time, error) {
	if raw == "" || strings.Trim(raw, "0123456789") != "" {
		return time.Time{}, ErrInvalidInvocation
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, ErrInvalidInvocation
	}
	return time.UnixMilli(value), nil
}
