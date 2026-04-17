package gitlab

import "errors"

// Sentinel errors that callers match via errors.Is.
var (
	ErrUnauthorized = errors.New("gitlab: unauthorized (401)")
	ErrForbidden    = errors.New("gitlab: forbidden (403)")
	ErrNotFound     = errors.New("gitlab: not found (404)")
)

// APIError wraps non-classified non-2xx responses so callers see the HTTP status + message.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string { return e.Message }
