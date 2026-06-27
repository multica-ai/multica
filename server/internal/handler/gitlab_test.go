package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestHandleGitLabWebhook_MissingSecret(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "anything")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_WrongToken(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "correct-secret")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "wrong")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_UnknownEvent(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
