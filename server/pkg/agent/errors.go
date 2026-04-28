package agent

import (
	"errors"
	"fmt"
	"time"
)

// ProviderError is a structured error representing a transient or permanent
// failure from an agent backend (LLM provider, CLI tool, or runtime).
// It is returned by Backend.Execute or surfaced through Result.Error so that
// the daemon and retry executor can classify failures without regex.
type ProviderError struct {
	Code      ErrorCode
	Message   string
	RetryAfter time.Duration // populated for ErrRateLimited; zero otherwise
}

func (e *ProviderError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("%s (retry after %s): %s", e.Code, e.RetryAfter, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ErrorCode classifies provider-level failures.
type ErrorCode string

const (
	ErrRateLimited       ErrorCode = "rate_limited"
	ErrServiceUnavailable ErrorCode = "service_unavailable"
	ErrGatewayError      ErrorCode = "gateway_error"
	ErrTimeout           ErrorCode = "timeout"
	ErrContextExceeded   ErrorCode = "context_exceeded"
	ErrQuotaExhausted    ErrorCode = "quota_exhausted"
)

// TransientErrorCodes are failure categories that may resolve on retry.
var TransientErrorCodes = map[ErrorCode]bool{
	ErrRateLimited:        true,
	ErrServiceUnavailable: true,
	ErrGatewayError:       true,
	ErrTimeout:            true,
}

// PermanentErrorCodes are failure categories that should not be retried.
var PermanentErrorCodes = map[ErrorCode]bool{
	ErrContextExceeded: true,
	ErrQuotaExhausted:  true,
}

// NewProviderError creates a ProviderError with the given code and message.
func NewProviderError(code ErrorCode, message string) *ProviderError {
	return &ProviderError{Code: code, Message: message}
}

// NewRateLimitedError creates a ProviderError for rate-limit responses.
// retryAfter should be parsed from the Retry-After header when available.
func NewRateLimitedError(message string, retryAfter time.Duration) *ProviderError {
	return &ProviderError{Code: ErrRateLimited, Message: message, RetryAfter: retryAfter}
}

// IsProviderError reports whether err is a *ProviderError.
func IsProviderError(err error) (*ProviderError, bool) {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
