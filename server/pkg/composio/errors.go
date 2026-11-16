package composio

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// APIError is the canonical error returned by the SDK when Composio responds
// with a non-2xx HTTP status.
//
// The Composio error envelope as of v3.1 looks like:
//
//	{
//	  "error": {
//	    "message": "...",
//	    "code":    400,
//	    "slug":    "INVALID_INPUT",
//	    "status":  400,
//	    "request_id":   "req_...",
//	    "suggested_fix":"...",
//	    "errors": ["..."]
//	  }
//	}
//
// HTTPStatus is the transport status as observed locally; the rest mirrors
// the body if Composio returned one. RawBody is preserved verbatim so
// callers can log the full upstream response for debugging.
type APIError struct {
	HTTPStatus   int      `json:"-"`
	Message      string   `json:"message,omitempty"`
	Code         int      `json:"code,omitempty"`
	Slug         string   `json:"slug,omitempty"`
	Status       int      `json:"status,omitempty"`
	RequestID    string   `json:"request_id,omitempty"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	RawBody      []byte   `json:"-"`
}

// Error implements error. It surfaces the upstream status, slug, and message.
func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.HTTPStatus)
	}
	if e.Slug != "" {
		return fmt.Sprintf("composio: %d %s (%s)", e.HTTPStatus, msg, e.Slug)
	}
	return fmt.Sprintf("composio: %d %s", e.HTTPStatus, msg)
}

// IsNotFound reports whether the error is an HTTP 404 — useful for idempotent
// delete/revoke flows.
func (e *APIError) IsNotFound() bool { return e != nil && e.HTTPStatus == http.StatusNotFound }

// IsUnauthorized reports whether the error is an HTTP 401.
func (e *APIError) IsUnauthorized() bool {
	return e != nil && e.HTTPStatus == http.StatusUnauthorized
}

// IsRateLimited reports whether the error is an HTTP 429.
func (e *APIError) IsRateLimited() bool {
	return e != nil && e.HTTPStatus == http.StatusTooManyRequests
}

// parseAPIError decodes Composio's `{"error": {...}}` envelope. If the body
// is not the expected shape it returns an APIError carrying just HTTPStatus
// and RawBody so callers still see something useful.
func parseAPIError(status int, body []byte) *APIError {
	out := &APIError{HTTPStatus: status, RawBody: body}
	if len(body) == 0 {
		return out
	}
	var wire struct {
		Error APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		// Body is not the expected envelope — leave RawBody set, message empty.
		return out
	}
	wire.Error.HTTPStatus = status
	wire.Error.RawBody = body
	return &wire.Error
}
