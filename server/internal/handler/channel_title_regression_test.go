package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/llm"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChannelChatTitle_UsesCurrentSourceForLLMAndPublishesAfterCAS(t *testing.T) {
	requireDB(t)
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	const current = "Investigate the login redirect"
	sessionID := parseUUID(fx.ChatSession(t, uuidToString(chatTitleTestAgentID(t)), testutil.Cols{"title": current}))
	requests := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, message := range payload.Messages {
			if message.Role == "user" {
				requests <- message.Content
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Login redirect investigation"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	h := *testHandler
	h.Bus = events.New()
	h.LLM = llm.New(llm.Config{APIKey: "test-key", BaseURL: upstream.URL})
	updates := make(chan protocol.ChatSessionUpdatedPayload, 1)
	h.Bus.Subscribe(protocol.EventChatSessionUpdated, func(event events.Event) {
		payload, ok := event.Payload.(protocol.ChatSessionUpdatedPayload)
		if ok && payload.ChatSessionID == uuidToString(sessionID) {
			stored, err := h.Queries.GetChatSession(context.Background(), sessionID)
			if err != nil || stored.Title != payload.Title {
				t.Errorf("title event preceded committed CAS: stored=%q event=%q err=%v", stored.Title, payload.Title, err)
			}
			updates <- payload
		}
	})
	h.GenerateChannelChatTitle(parseUUID(testWorkspaceID), parseUUID(testUserID), sessionID, current, current)
	select {
	case source := <-requests:
		if source != current {
			t.Fatalf("LLM user source = %q, want exact current turn %q", source, current)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel title hook did not call the LLM")
	}
	select {
	case payload := <-updates:
		if payload.Title != "Login redirect investigation" {
			t.Fatalf("channel title update = %+v", payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel title hook did not publish the committed title")
	}
}
