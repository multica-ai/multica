package publicapiv1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	api "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

func allowTokens(header http.Header) []string {
	var methods []string
	for _, line := range header.Values("Allow") {
		for method := range strings.SplitSeq(line, ",") {
			if method = strings.TrimSpace(method); method != "" {
				methods = append(methods, method)
			}
		}
	}
	slices.Sort(methods)
	return methods
}

func TestMethodNotAllowedResources(t *testing.T) {
	router := chi.NewRouter()
	calls := 0
	router.Group(func(r chi.Router) {
		r.Route(api.BasePath, func(r chi.Router) {
			for _, operation := range api.Operations {
				r.MethodFunc(operation.Method, operation.Path, func(w http.ResponseWriter, _ *http.Request) {
					calls++
					w.WriteHeader(http.StatusNoContent)
				})
			}
			r.NotFound(api.NotFound)
			r.MethodNotAllowed(api.MethodNotAllowed)
		})
	})
	cases := []struct {
		path, method string
		allowed      []string
	}{
		{"/v1/context", "POST", []string{"GET"}},
		{"/v1/issues/MUL-1", "POST", []string{"GET", "PATCH"}},
		{"/v1/issues/MUL-1/comments", "PATCH", []string{"GET", "POST"}},
		{"/v1/tasks/task-1/messages", "POST", []string{"GET"}},
		{"/v1/storage/user", "DELETE", []string{"GET"}},
		{"/v1/storage/user/key", "POST", []string{"DELETE", "GET", "PUT"}},
		{"/v1/storage/user/a%2Fb?include=value", "PATCH", []string{"DELETE", "GET", "PUT"}},
		{"/v1/issues/MUL-1", "HEAD", []string{"GET", "PATCH"}},
		{"/v1/issues/MUL-1", "OPTIONS", []string{"GET", "PATCH"}},
		{"/v1/issues/MUL-1", "QUERY", []string{"GET", "PATCH"}},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.Header.Set("X-Request-ID", "request-allow")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := allowTokens(response.Header()); !reflect.DeepEqual(got, tc.allowed) {
				t.Fatalf("Allow=%v want %v", got, tc.allowed)
			}
			if calls != 0 {
				t.Fatalf("method discovery executed %d resource handlers", calls)
			}
			if response.Header().Get("Content-Type") != api.ProblemContentType || response.Header().Get("X-Request-ID") != "request-allow" {
				t.Fatalf("unexpected headers: %v", response.Header())
			}
			var problem api.Problem
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			want := api.Problem{Type: "urn:multica:problem:method_not_allowed", Title: "Method Not Allowed", Status: 405, Code: "method_not_allowed", Detail: "method not allowed", RequestID: "request-allow", Error: "method not allowed"}
			if !reflect.DeepEqual(problem, want) {
				t.Fatalf("problem=%+v want %+v", problem, want)
			}
		})
	}
	// Each advertised operation still reaches the normal handler.
	for _, tc := range cases[:6] {
		for _, method := range tc.allowed {
			response := httptest.NewRecorder()
			before := calls
			router.ServeHTTP(response, httptest.NewRequest(method, tc.path, nil))
			if response.Code != http.StatusNoContent || calls != before+1 {
				t.Fatalf("%s %s status=%d calls=%d", method, tc.path, response.Code, calls)
			}
			if response.Header().Get("Allow") != "" {
				t.Fatal("Allow leaked to successful response")
			}
		}
	}
}

func TestMethodNotAllowedMountedRouting(t *testing.T) {
	chi.RegisterMethod("PROPFIND")
	cases := []struct {
		name, path string
		register   func(chi.Router, http.HandlerFunc)
		allowed    []string
	}{
		{"root", "/items/42", func(r chi.Router, h http.HandlerFunc) { r.Get("/items/{id}", h); r.Patch("/items/{id}", h) }, []string{"GET", "PATCH"}},
		{"parameterized mount", "/orgs/acme/v1/items/42", func(r chi.Router, h http.HandlerFunc) {
			r.Route("/orgs/{org}/v1", func(r chi.Router) { r.Get("/items/{id}", h); r.Patch("/items/{id}", h) })
		}, []string{"GET", "PATCH"}},
		{"nested inherited callback", "/v1/nested/items/42", func(r chi.Router, h http.HandlerFunc) {
			r.Route("/v1", func(r chi.Router) {
				r.MethodNotAllowed(api.MethodNotAllowed)
				r.Route("/nested", func(r chi.Router) { r.Get("/items/{id}", h); r.Put("/items/{id}", h) })
			})
		}, []string{"GET", "PUT"}},
		{"separate mounted router", "/prefix/v1/items/42", func(r chi.Router, h http.HandlerFunc) {
			sub := chi.NewRouter()
			sub.Route("/v1", func(r chi.Router) { r.Get("/items/{id}", h); r.Patch("/items/{id}", h) })
			r.Mount("/prefix", sub)
		}, []string{"GET", "PATCH"}},
		{"regexp", "/v1/items/42", func(r chi.Router, h http.HandlerFunc) {
			r.Get("/v1/items/{id:[0-9]+}", h)
			r.Put("/v1/items/{id:[0-9]+}", h)
		}, []string{"GET", "PUT"}},
		{"static and parameter paths", "/items/fixed", func(r chi.Router, h http.HandlerFunc) { r.Get("/items/fixed", h); r.Patch("/items/{id}", h) }, []string{"GET", "PATCH"}},
		{"static and wildcard paths", "/files/index", func(r chi.Router, h http.HandlerFunc) { r.Get("/files/*", h); r.Put("/files/index", h) }, []string{"GET", "PUT"}},
		{"explicit methods", "/items/42", func(r chi.Router, h http.HandlerFunc) {
			r.Get("/items/{id}", h)
			r.Head("/items/{id}", h)
			r.Options("/items/{id}", h)
			r.MethodFunc("QUERY", "/items/{id}", h)
			r.MethodFunc("PROPFIND", "/items/{id}", h)
		}, []string{"GET", "HEAD", "OPTIONS", "PROPFIND", "QUERY"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := chi.NewRouter()
			calls := 0
			tc.register(router, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) })
			router.MethodNotAllowed(api.MethodNotAllowed)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, tc.path, nil))
			if response.Code != 405 || !reflect.DeepEqual(allowTokens(response.Header()), tc.allowed) {
				t.Fatalf("status=%d Allow=%v want %v", response.Code, allowTokens(response.Header()), tc.allowed)
			}
			if calls != 0 {
				t.Fatal("discovery executed resource handler")
			}
			for _, method := range tc.allowed {
				response := httptest.NewRecorder()
				before := calls
				router.ServeHTTP(response, httptest.NewRequest(method, tc.path, nil))
				if response.Code != 204 || calls != before+1 {
					t.Fatalf("advertised %s was not supported: status=%d calls=%d", method, response.Code, calls)
				}
			}
		})
	}
}

func TestMethodNotAllowedPreservesRequest(t *testing.T) {
	type contextKey struct{}
	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Route("/orgs/{org}", func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/storage/{scope}/{key}", func(http.ResponseWriter, *http.Request) { t.Fatal("discovery executed resource handler") })
			r.Put("/storage/{scope}/{key}", func(http.ResponseWriter, *http.Request) { t.Fatal("discovery executed resource handler") })
			r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
				rctx := chi.RouteContext(r.Context())
				beforeContext := fmt.Sprintf("%#v", rctx)
				beforeURL := *r.URL
				beforeMethod, beforeURI, beforePattern := r.Method, r.RequestURI, r.Pattern
				beforePathValue := r.PathValue("org")
				api.MethodNotAllowed(w, r)
				if beforeContext != fmt.Sprintf("%#v", rctx) || beforeURL != *r.URL || beforeMethod != r.Method || beforeURI != r.RequestURI || beforePattern != r.Pattern || beforePathValue != r.PathValue("org") {
					t.Fatal("method discovery changed the live request or route context")
				}
				if r.Context().Value(contextKey{}) != "value" || chi.URLParam(r, "org") != "acme" {
					t.Fatal("request context changed")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "untouched body" {
					t.Fatalf("body=%q err=%v", body, err)
				}
			})
		})
	})
	request := httptest.NewRequest("POST", "/orgs/acme/v1/storage/user/a%2Fb?keep=1", strings.NewReader("untouched body"))
	request = request.WithContext(context.WithValue(request.Context(), contextKey{}, "value"))
	request.Header.Set("X-Request-ID", "from-middleware")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 405 || !reflect.DeepEqual(allowTokens(response.Header()), []string{"GET", "PUT"}) || response.Header().Get("X-Request-ID") != "from-middleware" {
		t.Fatalf("status=%d headers=%v", response.Code, response.Header())
	}
}

func TestMethodNotAllowedRoutingBoundaries(t *testing.T) {
	router := chi.NewRouter()
	router.Route(api.BasePath, func(r chi.Router) {
		r.Get(api.PathIssue, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected resource dispatch") })
		r.Get("/numeric/{id:[0-9]+}", func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected resource dispatch") })
		r.NotFound(api.NotFound)
		r.MethodNotAllowed(api.MethodNotAllowed)
	})
	for _, path := range []string{"/v1", "/v1/", "/v1/missing", "/v1/issues/MUL-1/extra", "/v1/numeric/not-numeric"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
		if response.Code != 404 || len(allowTokens(response.Header())) != 0 {
			t.Fatalf("POST %s status=%d Allow=%v", path, response.Code, allowTokens(response.Header()))
		}
	}
	// Chi rejects unregistered methods before entering the /v1 subrouter, even
	// for nonexistent paths. Keep that separate from known-method 404 routing.
	for _, path := range []string{"/v1/issues/MUL-1", "/v1/missing"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("UNREGISTERED", path, nil))
		if response.Code != 405 || response.Header().Get("Allow") != "" || response.Body.Len() != 0 {
			t.Fatalf("unknown method %s status=%d headers=%v body=%q", path, response.Code, response.Header(), response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	api.MethodNotAllowed(response, httptest.NewRequest("POST", "/unrouted", nil))
	if response.Code != 405 || response.Header().Get("Allow") != "" {
		t.Fatal("standalone callback without route context changed")
	}
}
