package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func markWhatsNewSeen(t *testing.T, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/me/whats-new/seen", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.MarkWhatsNewSeen(w, req)
	return w
}

func storedWhatsNewSeenVersion(t *testing.T, userID string) *string {
	t.Helper()
	var v *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT whats_new_seen_version FROM "user" WHERE id = $1`, userID,
	).Scan(&v); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	return v
}

func TestMarkWhatsNewSeenOnlyAdvances(t *testing.T) {
	userID := newWaitlistTestUser(t, "whats-new-advance@multica.ai")

	if v := storedWhatsNewSeenVersion(t, userID); v != nil {
		t.Fatalf("new user should start with NULL, got %q", *v)
	}

	steps := []struct {
		send string
		want string
	}{
		{"0.7.9", "0.7.9"},
		// Numeric, not lexicographic: 0.7.10 is newer than 0.7.9.
		{"0.7.10", "0.7.10"},
		// A stale client must not roll the marker back.
		{"0.7.2", "0.7.10"},
		{"0.7.10", "0.7.10"},
		{"1.0.0", "1.0.0"},
	}
	for _, step := range steps {
		w := markWhatsNewSeen(t, userID, map[string]string{"version": step.send})
		if w.Code != http.StatusOK {
			t.Fatalf("send %s: expected 200, got %d: %s", step.send, w.Code, w.Body.String())
		}
		var resp UserResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.WhatsNewSeenVersion == nil || *resp.WhatsNewSeenVersion != step.want {
			t.Fatalf("send %s: response version = %v, want %s", step.send, resp.WhatsNewSeenVersion, step.want)
		}
		if v := storedWhatsNewSeenVersion(t, userID); v == nil || *v != step.want {
			t.Fatalf("send %s: stored version = %v, want %s", step.send, v, step.want)
		}
	}
}

func TestMarkWhatsNewSeenRejectsMalformedVersion(t *testing.T) {
	userID := newWaitlistTestUser(t, "whats-new-invalid@multica.ai")

	for _, version := range []string{"", "v0.7.2", "0.7", "0.7.2-beta", "0.7.2.1", "9999999.0.0", "a.b.c"} {
		w := markWhatsNewSeen(t, userID, map[string]string{"version": version})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("version %q: expected 400, got %d", version, w.Code)
		}
	}
	if v := storedWhatsNewSeenVersion(t, userID); v != nil {
		t.Fatalf("rejected requests must not write, got %q", *v)
	}
}

func TestGetMeAlwaysSerializesWhatsNewSeenVersion(t *testing.T) {
	userID := newWaitlistTestUser(t, "whats-new-getme@multica.ai")

	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("X-User-ID", userID)
	w := httptest.NewRecorder()
	testHandler.GetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got, ok := raw["whats_new_seen_version"]
	if !ok {
		t.Fatal("whats_new_seen_version must be present so clients can detect support")
	}
	if string(got) != "null" {
		t.Fatalf("expected null for a fresh user, got %s", got)
	}
}
