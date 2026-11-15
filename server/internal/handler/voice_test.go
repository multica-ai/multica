package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/speech"
)

type fakeVoiceSessions struct {
	session speech.Session
	err     error
}

func (f fakeVoiceSessions) NewSession(context.Context) (speech.Session, error) {
	return f.session, f.err
}

func postVoiceSessionAs(h *Handler, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/voice/sessions", nil)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	w := httptest.NewRecorder()
	h.CreateVoiceSession(w, req)
	return w
}

func postVoiceSession(h *Handler) *httptest.ResponseRecorder {
	return postVoiceSessionAs(h, "user-1")
}

func TestCreateVoiceSessionIsOffWithoutAProvider(t *testing.T) {
	if w := postVoiceSession(&Handler{}); w.Code != http.StatusNotFound {
		t.Fatalf("want 404 without a speech provider, got %d", w.Code)
	}
}

func TestCreateVoiceSession(t *testing.T) {
	want := speech.Session{Provider: "deepgram", URL: "wss://stream.test/listen?model=m", Token: "t", ExpiresIn: 30}
	w := postVoiceSession(&Handler{Speech: fakeVoiceSessions{session: want}})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("a token must not be cached, Cache-Control=%q", got)
	}
	var got speech.Session
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("session: got %+v, want %+v", got, want)
	}
}

func TestCreateVoiceSessionProviderDown(t *testing.T) {
	w := postVoiceSession(&Handler{Speech: fakeVoiceSessions{err: errors.New("dial tcp: refused")}})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", w.Code)
	}
}

func TestCreateVoiceSessionNeedsAUser(t *testing.T) {
	w := postVoiceSessionAs(&Handler{Speech: fakeVoiceSessions{}}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without a user, got %d", w.Code)
	}
}

func TestCreateVoiceSessionIsLimitedPerUser(t *testing.T) {
	h := &Handler{Speech: fakeVoiceSessions{session: speech.Session{Token: "t"}}, VoiceSessionLimiter: NewMemoryVoiceSessionLimiter()}
	for i := 0; i < voiceSessionsPerWindow; i++ {
		if w := postVoiceSessionAs(h, "user-1"); w.Code != http.StatusOK {
			t.Fatalf("session %d: want 200, got %d", i+1, w.Code)
		}
	}
	if w := postVoiceSessionAs(h, "user-1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("over the budget: want 429, got %d", w.Code)
	}
	if w := postVoiceSessionAs(h, "user-2"); w.Code != http.StatusOK {
		t.Fatalf("another user has their own budget: want 200, got %d", w.Code)
	}
}

func TestRedisVoiceSessionLimiterIsSharedAcrossReplicas(t *testing.T) {
	rdb := newRedisTestClient(t)
	defer rdb.Close()

	ctx := context.Background()
	a, b := NewRedisVoiceSessionLimiter(rdb), NewRedisVoiceSessionLimiter(rdb)
	for i := 0; i < voiceSessionsPerWindow; i++ {
		replica := a
		if i%2 == 1 {
			replica = b
		}
		if !replica.Allow(ctx, "voice-user") {
			t.Fatalf("session %d should be allowed", i+1)
		}
	}
	if a.Allow(ctx, "voice-user") || b.Allow(ctx, "voice-user") {
		t.Fatal("the budget must be shared by every replica")
	}
}
