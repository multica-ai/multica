package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const openAPIv3JSON = `{
  "openapi": "3.0.0",
  "info": {"title": "Orders API", "version": "1.0"},
  "paths": {
    "/orders": {
      "get": {"summary": "list orders"},
      "post": {"summary": "create order"}
    },
    "/orders/{id}": {
      "get": {"summary": "get order"},
      "delete": {"summary": "delete order"},
      "parameters": [{"name": "id", "in": "path"}]
    }
  }
}`

const swaggerV2YAML = `swagger: "2.0"
info:
  title: Legacy API
  version: "1.0"
paths:
  /users:
    get:
      summary: list users
    put:
      summary: replace users
  /health:
    get:
      summary: healthcheck
`

func TestParseSpec_OpenAPIv3JSON(t *testing.T) {
	eps := parseSpec([]byte(openAPIv3JSON))
	if len(eps) != 2 {
		t.Fatalf("expected 2 endpoints, got %d: %+v", len(eps), eps)
	}
	// Sorted by path: /orders then /orders/{id}.
	if eps[0].Path != "/orders" {
		t.Fatalf("expected first path /orders, got %q", eps[0].Path)
	}
	if got := eps[0].Methods; len(got) != 2 || got[0] != "GET" || got[1] != "POST" {
		t.Fatalf("expected [GET POST] on /orders, got %v", got)
	}
	if eps[1].Path != "/orders/{id}" {
		t.Fatalf("expected /orders/{id}, got %q", eps[1].Path)
	}
	// "parameters" must NOT be treated as an HTTP method.
	if got := eps[1].Methods; len(got) != 2 || got[0] != "DELETE" || got[1] != "GET" {
		t.Fatalf("expected [DELETE GET] on /orders/{id}, got %v", got)
	}
}

func TestParseSpec_SwaggerV2YAML(t *testing.T) {
	eps := parseSpec([]byte(swaggerV2YAML))
	if len(eps) != 2 {
		t.Fatalf("expected 2 endpoints, got %d: %+v", len(eps), eps)
	}
	if eps[0].Path != "/health" || eps[1].Path != "/users" {
		t.Fatalf("unexpected paths: %+v", eps)
	}
	if got := eps[1].Methods; len(got) != 2 || got[0] != "GET" || got[1] != "PUT" {
		t.Fatalf("expected [GET PUT] on /users, got %v", got)
	}
}

func TestParseSpec_NotASpec(t *testing.T) {
	// A plain JSON document that happens to have a "paths" key but no
	// openapi/swagger marker must be rejected.
	if eps := parseSpec([]byte(`{"paths": {"/x": {"get": {}}}}`)); eps != nil {
		t.Fatalf("expected nil for non-spec document, got %+v", eps)
	}
	if eps := parseSpec([]byte(`not json or yaml at all: <<<`)); eps != nil {
		t.Fatalf("expected nil for garbage, got %+v", eps)
	}
}

func TestDiscoverAPIEndpoints_FromWellKnownPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openAPIv3JSON))
			return
		}
		// Base URL responds but is not a spec.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eps := discoverAPIEndpoints(ctx, srv.Client(), srv.URL, AuthConfig{})
	if len(eps) != 2 {
		t.Fatalf("expected 2 discovered endpoints, got %d: %+v", len(eps), eps)
	}
}

func TestDiscoverAPIEndpoints_SendsAuthHeaders(t *testing.T) {
	var sawBearer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			sawBearer = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openAPIv3JSON))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = discoverAPIEndpoints(ctx, srv.Client(), srv.URL, AuthConfig{BearerToken: "secret-token"})
	if sawBearer != "Bearer secret-token" {
		t.Fatalf("expected bearer header forwarded to spec fetch, got %q", sawBearer)
	}
}

func TestDiscoverAPIEndpoints_NoSpecReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if eps := discoverAPIEndpoints(ctx, srv.Client(), srv.URL, AuthConfig{}); eps != nil {
		t.Fatalf("expected nil when no spec is served, got %+v", eps)
	}
}
