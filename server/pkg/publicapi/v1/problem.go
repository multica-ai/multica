package publicapiv1

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

const ProblemContentType = "application/problem+json"

// FieldError describes one invalid request field. It is optional so endpoints
// can adopt field-level validation without changing the top-level envelope.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Problem is the stable v1 error envelope. Error is the compatibility alias
// consumed by existing Plugin clients; new clients should branch on Code and
// render Detail.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Code      string       `json:"code"`
	Detail    string       `json:"detail"`
	RequestID string       `json:"request_id"`
	Errors    []FieldError `json:"errors,omitempty"`
	Error     string       `json:"error"`
}

func CodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusPaymentRequired:
		return "payment_required"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "incompatible"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusInsufficientStorage:
		return "quota_exceeded"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusBadGateway:
		return "upstream_unavailable"
	default:
		return "internal_error"
	}
}

func requestID(r *http.Request) string {
	if r != nil {
		if id := strings.TrimSpace(chimw.GetReqID(r.Context())); id != "" {
			return id
		}
		if id := strings.TrimSpace(r.Header.Get(chimw.RequestIDHeader)); id != "" {
			return id
		}
	}
	return uuid.NewString()
}

// WriteProblem writes an RFC 9457-style problem with Multica's stable code and
// the legacy `error` alias. It is shared by auth middleware and handlers so a
// failure before routing has the same contract as one inside a resource.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	if code == "" {
		code = CodeForStatus(status)
	}
	title := http.StatusText(status)
	if title == "" {
		title = "Request failed"
	}
	id := requestID(r)
	payload := Problem{
		Type:      "urn:multica:problem:" + code,
		Title:     title,
		Status:    status,
		Code:      code,
		Detail:    detail,
		RequestID: id,
		Error:     detail,
	}

	w.Header().Set("Content-Type", ProblemContentType)
	w.Header().Set(chimw.RequestIDHeader, id)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	WriteProblem(w, r, http.StatusNotFound, "not_found", "resource not found")
}

func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.Routes != nil {
		// Routes is the root router, so match the full routing path rather than
		// RoutePath, which has already been trimmed by any mounted subrouters.
		path := r.URL.RawPath
		if path == "" {
			path = r.URL.Path
		}
		if path == "" {
			path = "/"
		}
		methods := make(map[string]bool)
		_ = chi.Walk(rctx.Routes, func(method, _ string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			methods[method] = true
			return nil
		})
		var allowed []string
		for method := range methods {
			// Match updates its context; never reuse the live request's context
			// or execute resource handlers just to discover supported methods.
			if rctx.Routes.Match(chi.NewRouteContext(), method, path) {
				allowed = append(allowed, method)
			}
		}
		if len(allowed) > 0 {
			sort.Strings(allowed)
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
	}
	WriteProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
