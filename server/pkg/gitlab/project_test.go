package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetProject_ByNumericID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/123" {
			t.Errorf("path = %q, want /api/v4/projects/123", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 123, "path_with_namespace": "group/app"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	p, err := c.GetProject(context.Background(), "tok", "123")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 123 || p.PathWithNamespace != "group/app" {
		t.Fatalf("unexpected project: %+v", p)
	}
}

func TestGetProject_ByPathIsURLEncoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// URL path delivered by httptest already decodes %2F back to /, so the
		// handler sees /api/v4/projects/group/app. We check RequestURI for the
		// raw form instead.
		if r.RequestURI != "/api/v4/projects/group%2Fapp" {
			t.Errorf("request URI = %q, want /api/v4/projects/group%%2Fapp", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 7, "path_with_namespace": "group/app"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	p, err := c.GetProject(context.Background(), "tok", "group/app")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 7 {
		t.Fatalf("id = %d, want 7", p.ID)
	}
}

func TestGetProject_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "404 Project Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.GetProject(context.Background(), "tok", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetProject_FullURLIsNormalized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A pasted browser URL like https://gitlab.com/group/app should resolve
		// the same as the bare path "group/app" — host gets stripped, slashes
		// get URL-encoded.
		if r.RequestURI != "/api/v4/projects/group%2Fapp" {
			t.Errorf("request URI = %q, want /api/v4/projects/group%%2Fapp", r.RequestURI)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 11, "path_with_namespace": "group/app"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	p, err := c.GetProject(context.Background(), "tok", "https://gitlab.com/group/app")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ID != 11 {
		t.Fatalf("id = %d, want 11", p.ID)
	}
}
