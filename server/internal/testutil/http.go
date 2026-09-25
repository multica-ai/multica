package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
)

// JSONRequest builds a request whose body is body encoded as JSON.
//
// body is passed through unencoded when it is already a string, []byte or
// io.Reader, so a test can post a malformed payload — the malformed-response
// tests the endpoint checklist asks for would otherwise have to bypass this
// helper and rebuild the headers by hand.
func JSONRequest(method, path string, body any) *http.Request {
	var reader io.Reader
	switch v := body.(type) {
	case nil:
		reader = &bytes.Buffer{}
	case string:
		reader = strings.NewReader(v)
	case []byte:
		reader = bytes.NewReader(v)
	case io.Reader:
		reader = v
	default:
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(v); err != nil {
			// Encoding a literal built inside a test can only fail on a value
			// no test means to send (a channel, a NaN); panicking here reports
			// it at the call site instead of as a confusing empty body later.
			panic(fmt.Sprintf("testutil: encode request body for %s %s: %v", method, path, err))
		}
		reader = &buf
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// WithHeaders sets alternating key/value header pairs and returns req.
func WithHeaders(req *http.Request, kv ...string) *http.Request {
	if len(kv)%2 != 0 {
		panic("testutil: WithHeaders needs an even number of key/value arguments")
	}
	for i := 0; i < len(kv); i += 2 {
		req.Header.Set(kv[i], kv[i+1])
	}
	return req
}

// WithURLParams attaches alternating chi URL param key/value pairs, which is
// how a handler called directly — without its router — reads path segments.
func WithURLParams(req *http.Request, kv ...string) *http.Request {
	if len(kv)%2 != 0 {
		panic("testutil: WithURLParams needs an even number of key/value arguments")
	}
	rctx := chi.NewRouteContext()
	for i := 0; i < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TB is the part of *testing.T these helpers use. Taking the interface rather
// than the concrete type is what lets this package assert on its own failure
// messages — the messages are the reason a shared assertion is worth having,
// so they need a test of their own.
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	Cleanup(func())
}

// Response is the recorded result of one handler call. Its assertions are
// fatal: a test that misread the status has already misread everything it
// would decode out of the body.
type Response struct {
	*httptest.ResponseRecorder

	t   TB
	req *http.Request
}

// Call runs h against req and records what it wrote.
func Call(t TB, h http.HandlerFunc, req *http.Request) *Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, req)
	return &Response{ResponseRecorder: rec, t: t, req: req}
}

// Want fails the test unless the handler wrote status code want. The failure
// message carries the request line and the response body, because the body is
// where a handler says which of its several rejections fired.
func (r *Response) Want(want int) *Response {
	r.t.Helper()
	if r.Code != want {
		r.t.Fatalf("%s %s: expected %d, got %d: %s",
			r.req.Method, r.req.URL.RequestURI(), want, r.Code, r.Body.String())
	}
	return r
}

// WantOneOf fails the test unless the status is one of want. Use it only where
// more than one status is genuinely correct; asserting a set to dodge a flake
// hides the flake.
func (r *Response) WantOneOf(want ...int) *Response {
	r.t.Helper()
	for _, code := range want {
		if r.Code == code {
			return r
		}
	}
	r.t.Fatalf("%s %s: expected one of %v, got %d: %s",
		r.req.Method, r.req.URL.RequestURI(), want, r.Code, r.Body.String())
	return r
}

// JSON decodes the response body into dest.
func (r *Response) JSON(dest any) *Response {
	r.t.Helper()
	if err := json.Unmarshal(r.Body.Bytes(), dest); err != nil {
		r.t.Fatalf("%s %s: decode response: %v: %s",
			r.req.Method, r.req.URL.RequestURI(), err, r.Body.String())
	}
	return r
}

// Map decodes the body into a generic object, for assertions that only touch
// one or two fields and do not earn a named struct.
func (r *Response) Map() map[string]any {
	r.t.Helper()
	out := map[string]any{}
	r.JSON(&out)
	return out
}

// Text returns the raw response body.
func (r *Response) Text() string { return r.Body.String() }

// Decode runs the handler, asserts the status and decodes the body in one
// expression, for the common case where the caller wants a typed payload and
// has no other use for the response.
func Decode[T any](t TB, h http.HandlerFunc, req *http.Request, want int) T {
	t.Helper()
	var out T
	Call(t, h, req).Want(want).JSON(&out)
	return out
}
