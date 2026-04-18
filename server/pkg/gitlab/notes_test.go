package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListNotes_PerIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/issues/42/notes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Note{
			{ID: 1, Body: "hello", System: false, Author: User{ID: 100, Username: "alice"}, UpdatedAt: "2026-04-17T10:00:00Z"},
			{ID: 2, Body: "added status::todo", System: true, Author: User{ID: 200, Username: "bot"}, UpdatedAt: "2026-04-17T10:01:00Z"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	notes, err := c.ListNotes(context.Background(), "tok", 7, 42)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 || !notes[1].System {
		t.Errorf("unexpected: %+v", notes)
	}
}

func TestCreateNote_SendsPOSTWithBody(t *testing.T) {
	var capturedMethod, capturedPath, capturedToken, capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":555,"body":"hello","author":{"id":9,"username":"u"},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	note, err := c.CreateNote(context.Background(), "tok", 42, 7, "hello")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s", capturedToken)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(capturedBody), &body)
	if body["body"] != "hello" {
		t.Errorf("body field = %v, want hello", body["body"])
	}
	if note.ID != 555 || note.Body != "hello" {
		t.Errorf("note = %+v, want ID=555 body=hello", note)
	}
}

func TestCreateNote_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.CreateNote(context.Background(), "tok", 1, 1, "x")
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error = %v, want 403 or forbidden", err)
	}
}

func TestUpdateNote_SendsPUTWithBody(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":555,"body":"updated","author":{"id":9},"updated_at":"2026-04-17T13:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	note, err := c.UpdateNote(context.Background(), "tok", 42, 7, 555, "updated")
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes/555" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedBody["body"] != "updated" {
		t.Errorf("body = %v, want updated", capturedBody["body"])
	}
	if note.ID != 555 || note.Body != "updated" {
		t.Errorf("note = %+v", note)
	}
}

func TestUpdateNote_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.UpdateNote(context.Background(), "tok", 1, 1, 1, "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteNote_SendsDELETE(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 42, 7, 555); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes/555" {
		t.Errorf("path = %s", capturedPath)
	}
}

func TestDeleteNote_404IsIdempotentSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 1, 1, 1); err != nil {
		t.Fatalf("expected 404 as success, got %v", err)
	}
}

func TestDeleteNote_PropagatesNon404Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 1, 1, 1); err == nil {
		t.Fatal("expected error for 403")
	}
}
