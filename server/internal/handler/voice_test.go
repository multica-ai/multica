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

func postVoiceSession(h *Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.CreateVoiceSession(w, httptest.NewRequest(http.MethodPost, "/api/voice/sessions", nil))
	return w
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
