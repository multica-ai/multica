package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// callFetch drives the loopback /request-secret/fetch handler with the given
// bearer token and body.
func callFetch(d *Daemon, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/request-secret/fetch", &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	d.requestSecretFetchHandler()(w, req)
	return w
}

func TestRequestSecretFetchHandler_HappyPath(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	d.requestSecrets.Put("task-1", "mat_tok", "jwt-secret")

	w := callFetch(d, "mat_tok", map[string]string{"task_id": "task-1"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret != "jwt-secret" {
		t.Fatalf("expected jwt-secret, got %q", resp.Secret)
	}
	// One-shot: a replay gets 404.
	if w := callFetch(d, "mat_tok", map[string]string{"task_id": "task-1"}); w.Code != http.StatusNotFound {
		t.Fatalf("replay should be 404, got %d", w.Code)
	}
}

func TestRequestSecretFetchHandler_WrongTokenNotBurned(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	d.requestSecrets.Put("task-1", "mat_tok", "jwt-secret")

	// A caller with a different token (e.g. a non-whitelisted tool that scraped
	// some other credential) gets nothing, and does NOT burn the entry.
	if w := callFetch(d, "mat_wrong", map[string]string{"task_id": "task-1"}); w.Code != http.StatusNotFound {
		t.Fatalf("wrong token should be 404, got %d", w.Code)
	}
	// The legitimate child still fetches it.
	if w := callFetch(d, "mat_tok", map[string]string{"task_id": "task-1"}); w.Code != http.StatusOK {
		t.Fatalf("legit token should be 200 after wrong-token probe, got %d", w.Code)
	}
}

func TestRequestSecretFetchHandler_MissingToken(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	d.requestSecrets.Put("task-1", "mat_tok", "jwt-secret")
	if w := callFetch(d, "", map[string]string{"task_id": "task-1"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing token should be 404, got %d", w.Code)
	}
}

func TestRequestSecretFetchHandler_MissingTaskID(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	if w := callFetch(d, "mat_tok", map[string]string{}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing task_id should be 400, got %d", w.Code)
	}
}

func TestRequestSecretFetchHandler_UnknownTask(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	if w := callFetch(d, "mat_tok", map[string]string{"task_id": "nope"}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown task should be 404, got %d", w.Code)
	}
}

func TestRequestSecretFetchHandler_MethodNotAllowed(t *testing.T) {
	d := &Daemon{requestSecrets: newRequestSecretBroker(0)}
	req := httptest.NewRequest(http.MethodGet, "/request-secret/fetch", nil)
	req.Header.Set("Authorization", "Bearer mat_tok")
	w := httptest.NewRecorder()
	d.requestSecretFetchHandler()(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should be 405, got %d", w.Code)
	}
}
