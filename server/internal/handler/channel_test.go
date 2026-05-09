package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// ── Helpers ─────────────────────────────────────────────

func channelRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	// Inject workspace context from X-Workspace-ID header (for test, matches production middleware)
	// Set workspace context via header (matches production workspace middleware)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if wsID := req.Header.Get("X-Workspace-ID"); wsID != "" {
				// Use the handler's resolveWorkspaceID which reads from X-Workspace-ID header
				ws := h.resolveWorkspaceID(req)
				if ws == "" {
					ws = wsID
				}
				ctx := context.WithValue(req.Context(), struct{}{}, ws)
				req = req.WithContext(ctx)
			}
			next.ServeHTTP(w, req)
		})
	})
	r.Post("/api/channels", h.CreateChannel)
	r.Get("/api/channels", h.ListChannels)
	r.Route("/api/channels/{channelId}", func(r chi.Router) {
		r.Get("/", h.GetChannel)
		r.Delete("/", h.ArchiveChannel)
		r.Route("/members", func(r chi.Router) {
			r.Get("/", h.ListChannelMembers)
			r.Post("/", h.AddChannelMember)
			r.Delete("/{memberId}", h.RemoveChannelMember)
		})
		r.Route("/messages", func(r chi.Router) {
			r.Get("/", h.ListChannelMessages)
			r.Post("/", h.SendChannelMessage)
			r.Get("/{messageId}/replies", h.ListThreadReplies)
		})
		r.Post("/read", h.MarkChannelRead)
	})
	return r
}

func newChannelRequest(method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	setChannelUser(req)
	return req
}

func setChannelUser(req *http.Request) {
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
}

func withChannelID(req *http.Request, channelID string) *http.Request {
	return withURLParam(req, "channelId", channelID)
}

// ── DB-backed Integration Tests ─────────────────────────

func TestCreateChannelIntegration(t *testing.T) {
	if testHandler == nil {
		t.Skip("test DB not available")
	}

	router := channelRouter(testHandler)

	t.Run("create channel returns 201 and creator is member", func(t *testing.T) {
		req := newChannelRequest("POST", "/api/channels?workspace_id="+testWorkspaceID, map[string]string{
			"name": "test-channel-int",
			"type": "public",
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp ChannelResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Name != "test-channel-int" {
			t.Errorf("name mismatch: %s", resp.Name)
		}
		if !resp.IsMember {
			t.Error("creator should be auto-member")
		}
		if resp.ID == "" {
			t.Error("id should not be empty")
		}

		// Archive to clean up
		delReq := newChannelRequest("DELETE", "/api/channels/"+resp.ID, nil)
		delReq = withChannelID(delReq, resp.ID)
		delW := httptest.NewRecorder()
		router.ServeHTTP(delW, delReq)
	})
}

func TestSendChannelMessageIntegration(t *testing.T) {
	if testHandler == nil {
		t.Skip("test DB not available")
	}

	router := channelRouter(testHandler)

	// Create a test channel
	createReq := newChannelRequest("POST", "/api/channels?workspace_id="+testWorkspaceID, map[string]string{
		"name": "test-msg-int",
		"type": "public",
	})
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create channel failed: %d", createW.Code)
	}
	var ch ChannelResponse
	json.NewDecoder(createW.Body).Decode(&ch)

	t.Run("send message returns 201 with user author_type", func(t *testing.T) {
		req := newChannelRequest("POST", "/api/channels/"+ch.ID+"/messages",
			map[string]string{"content": "hello channel test"})
		req = withChannelID(req, ch.ID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var msg ChannelMessageResponse
		if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		if msg.AuthorType != "user" {
			t.Errorf("expected AuthorType=user, got %s", msg.AuthorType)
		}
		if msg.Content != "hello channel test" {
			t.Errorf("content mismatch: %s", msg.Content)
		}
	})

	t.Run("list messages returns sent messages", func(t *testing.T) {
		req := newChannelRequest("GET", "/api/channels/"+ch.ID+"/messages", nil)
		req = withChannelID(req, ch.ID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var msgs []ChannelMessageResponse
		json.NewDecoder(w.Body).Decode(&msgs)
		if len(msgs) == 0 {
			t.Fatal("expected at least 1 message")
		}
	})

	// Cleanup
	delReq := newChannelRequest("DELETE", "/api/channels/"+ch.ID, nil)
	delReq = withChannelID(delReq, ch.ID)
	delW := httptest.NewRecorder()
	router.ServeHTTP(delW, delReq)
}

func TestChannelMemberGating(t *testing.T) {
	if testHandler == nil {
		t.Skip("test DB not available")
	}

	router := channelRouter(testHandler)

	// Create a channel owned by the test user
	createReq := newChannelRequest("POST", "/api/channels?workspace_id="+testWorkspaceID, map[string]string{
		"name": "test-gating-int",
		"type": "public",
	})
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", createW.Code)
	}
	var ch ChannelResponse
	json.NewDecoder(createW.Body).Decode(&ch)

	t.Run("owner can access channel", func(t *testing.T) {
		req := newChannelRequest("GET", "/api/channels/"+ch.ID, nil)
		req = withChannelID(req, ch.ID)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("owner should have access, got %d", w.Code)
		}
	})

	// Cleanup
	delReq := newChannelRequest("DELETE", "/api/channels/"+ch.ID, nil)
	delReq = withChannelID(delReq, ch.ID)
	delW := httptest.NewRecorder()
	router.ServeHTTP(delW, delReq)
}
