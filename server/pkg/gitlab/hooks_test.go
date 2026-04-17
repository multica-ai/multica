package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateProjectHook_PostsCorrectBody(t *testing.T) {
	var got CreateProjectHookInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/hooks" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ProjectHook{ID: 99, URL: got.URL})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateProjectHook(context.Background(), "tok", 7, CreateProjectHookInput{
		URL:                      "https://multica.example/api/gitlab/webhook",
		Token:                    "secret-xyz",
		IssuesEvents:             true,
		ConfidentialIssuesEvents: true,
		NoteEvents:               true,
		ConfidentialNoteEvents:   true,
		EmojiEvents:              true,
		LabelEvents:              true,
		ReleasesEvents:           false,
	})
	if err != nil {
		t.Fatalf("CreateProjectHook: %v", err)
	}
	if got.URL != "https://multica.example/api/gitlab/webhook" || got.Token != "secret-xyz" {
		t.Errorf("server received %+v", got)
	}
	if !got.IssuesEvents || !got.NoteEvents || !got.EmojiEvents || !got.LabelEvents {
		t.Errorf("expected issues/note/emoji/label events enabled: %+v", got)
	}
	if out.ID != 99 {
		t.Errorf("returned ID = %d, want 99", out.ID)
	}
}

func TestDeleteProjectHook_HitsCorrectPath(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/hooks/99" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteProjectHook(context.Background(), "tok", 7, 99); err != nil {
		t.Fatalf("DeleteProjectHook: %v", err)
	}
	if !hit {
		t.Errorf("server was not hit")
	}
}

func TestDeleteProjectHook_404IsNotAnError(t *testing.T) {
	// Disconnect should be tolerant — if the hook was already deleted out
	// of band, that's fine; we just want to ensure it's gone.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteProjectHook(context.Background(), "tok", 7, 99); err != nil {
		t.Errorf("expected no error for 404, got: %v", err)
	}
}
