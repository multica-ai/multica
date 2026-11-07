package localhost

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestV2GroupUnreadIncludesHiddenPagesAndAcknowledgesExactResult(t *testing.T) {
	srv, svc := newV2Server(t)
	ctx := context.Background()
	s := svc.Store()
	scope, err := s.EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFolderProject(ctx, scope, "folder", "Folder", "/repo", time.Now()); err != nil {
		t.Fatal(err)
	}
	w := s.NewWriter(store.WriterOptions{})
	defer w.Close()
	for i := range 10 {
		id := fmt.Sprintf("c%d", i)
		if _, err := s.CreateConversation(ctx, store.NewConversation{ID: id, ScopeID: scope, Provider: "claude", Title: id, Cwd: "/repo", CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	submit := func(b store.Batch) {
		t.Helper()
		b.ConversationID = "c0"
		if _, err := w.Submit(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b"} {
		submit(store.Batch{StartTurn: &store.NewTurn{ID: id, StartedAt: time.Now()}})
		submit(store.Batch{CompleteTurn: &store.TurnCompletion{TurnID: id, Status: "completed", CompletedAt: time.Now()}})
	}
	h := v2Headers("user-1")
	// Searching an already-read conversation still reports the entire directory's unread state.
	status, catalog := v2Do(t, srv, http.MethodGet, "/local/v2/session-groups?q=c9", nil, h)
	if status != http.StatusOK {
		t.Fatalf("groups: %d %v", status, catalog)
	}
	groups := catalog["groups"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["has_unread_completion"] != true {
		t.Fatalf("lost hidden unread: %v", groups)
	}
	status, page := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=folder&limit=8", nil, h)
	if status != http.StatusOK {
		t.Fatalf("page: %d %v", status, page)
	}
	for _, item := range page["summaries"].([]any) {
		if item.(map[string]any)["conversation_id"] == "c0" {
			t.Fatal("fixture unread must be outside the first page")
		}
	}
	ws := dialWS(t, srv.URL, h)
	if err := ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe_scope", "after_scope_seq": catalog["scope_seq"]}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"store_generation": svc.Generation(), "turn_id": "a", "turn_index": 0, "completion_seq": 1}
	status, summary := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/c0/completion-read", body, h)
	if status != http.StatusOK || summary["has_unread_completion"] != true || summary["read_turn_index"] != float64(0) {
		t.Fatalf("old acknowledgement consumed B: %d %v", status, summary)
	}
	for _, bad := range []map[string]any{
		{"store_generation": "old", "turn_id": "b", "turn_index": 1},
		{"store_generation": svc.Generation(), "turn_id": "b", "turn_index": 4},
		{"store_generation": svc.Generation(), "turn_id": "a"},
	} {
		if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/c0/completion-read", bad, h); status != http.StatusBadRequest {
			t.Fatalf("invalid completion accepted: %d", status)
		}
	}
	other := v2Headers("user-1")
	other.Set("X-Workspace-ID", "another")
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/c0/completion-read", body, other); status != http.StatusNotFound {
		t.Fatalf("cross-scope read: %d", status)
	}
	body["turn_id"], body["turn_index"], body["completion_seq"] = "b", 1, 2
	for range 2 {
		status, summary = v2Do(t, srv, http.MethodPost, "/local/v2/sessions/c0/completion-read", body, h)
		if status != http.StatusOK || summary["has_unread_completion"] != false {
			t.Fatalf("read B: %d %v", status, summary)
		}
	}
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	readUntil(t, ws, func(message map[string]any) bool {
		summaries, _ := message["summaries"].([]any)
		for _, item := range summaries {
			row := item.(map[string]any)
			if row["conversation_id"] == "c0" && row["read_turn_index"] == float64(1) && row["has_unread_completion"] == false {
				return true
			}
		}
		return false
	})
	_, catalog = v2Do(t, srv, http.MethodGet, "/local/v2/session-groups", nil, h)
	if catalog["groups"].([]any)[0].(map[string]any)["has_unread_completion"] != false {
		t.Fatalf("group remained unread: %v", catalog)
	}
}
