package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_SendsPrivateTokenHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	var out map[string]any
	if err := c.get(context.Background(), "glpat-abc", "/ping", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if gotToken != "glpat-abc" {
		t.Fatalf("PRIVATE-TOKEN header = %q, want %q", gotToken, "glpat-abc")
	}
}

func TestClient_Parses401AsErrUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestClient_Parses404AsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestClient_ArrayShapedErrorMessageJoined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		// GitLab validation-error shape.
		w.Write([]byte(`{"message": ["title can't be blank", "iid is invalid"]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	want := "title can't be blank; iid is invalid"
	if apiErr.Message != want {
		t.Errorf("Message = %q, want %q", apiErr.Message, want)
	}
}

func TestClient_MissingMessageFallsBackToBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// No `message` field at all.
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	err := c.get(context.Background(), "tok", "/x", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Message != `{"error": "bad request"}` {
		t.Errorf("Message = %q, want raw body", apiErr.Message)
	}
}
