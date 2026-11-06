package localhost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestV2SessionBrowseUsesBoundedDirectoryPagesAndAccurateCounts(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	ctx := context.Background()
	s := svc.Store()
	scopeID, err := s.EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFolderProject(ctx, scopeID, "repo", "Repository", "/repo", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFolderProject(ctx, scopeID, "empty", "Empty", "/empty", time.Now()); err != nil {
		t.Fatal(err)
	}
	for i := range 18 {
		cwd := "/repo/"
		if i == 17 {
			cwd = "/other"
		}
		if _, err := s.CreateConversation(ctx, store.NewConversation{ID: fmt.Sprintf("session-%02d", i), ScopeID: scopeID, Provider: "claude", Title: fmt.Sprintf("Session %02d", i), Cwd: cwd, CreatedAt: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
	}
	h := v2Headers("user-1")
	status, catalog := v2Do(t, srv, http.MethodGet, "/local/v2/session-groups", nil, h)
	if status != http.StatusOK {
		t.Fatalf("groups = %d, %v", status, catalog)
	}
	groups := catalog["groups"].([]any)
	if len(groups) != 3 || groups[0].(map[string]any)["total"] != float64(17) || groups[1].(map[string]any)["total"] != float64(0) || groups[2].(map[string]any)["total"] != float64(1) {
		t.Fatalf("groups = %v", groups)
	}
	seen := map[string]bool{}
	cursor := ""
	for index, count := range []int{8, 8, 1} {
		path := "/local/v2/sessions/browse?folder_id=repo&limit=8&cursor=" + url.QueryEscape(cursor)
		status, page := v2Do(t, srv, http.MethodGet, path, nil, h)
		if status != http.StatusOK {
			t.Fatalf("page %d = %d, %v", index, status, page)
		}
		rows := page["summaries"].([]any)
		if len(rows) != count {
			t.Fatalf("page %d has %d summaries, want %d", index, len(rows), count)
		}
		for _, row := range rows {
			id := row.(map[string]any)["conversation_id"].(string)
			if seen[id] || id == "session-17" {
				t.Fatalf("duplicate/wrong-directory summary %s", id)
			}
			seen[id] = true
		}
		if index < 2 {
			next, ok := page["next_cursor"].(string)
			if !ok || next == "" || next == cursor || page["has_more"] != true {
				t.Fatalf("cursor did not advance: %v", page)
			}
			cursor = next
		} else if page["next_cursor"] != nil || page["has_more"] != false {
			t.Fatalf("last page = %v", page)
		}
	}
	for _, suffix := range []string{"&q=changed", "&archived=true", "&exclude_id=session-00"} {
		status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&cursor="+url.QueryEscape(cursor)+suffix, nil, h)
		if status != http.StatusBadRequest {
			t.Fatalf("reused cursor filter %s = %d", suffix, status)
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=bad", "cursor=not-json", "archived=bad"} {
		if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?"+query, nil, h); status != http.StatusBadRequest {
			t.Fatalf("invalid %s = %d", query, status)
		}
	}
	status, page := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&q=Session+12", nil, h)
	if status != http.StatusOK || len(page["summaries"].([]any)) != 1 {
		t.Fatalf("server search = %d, %v", status, page)
	}
	status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&archived=true", nil, h)
	if status != http.StatusOK || len(page["summaries"].([]any)) != 0 {
		t.Fatalf("archive filter = %d, %v", status, page)
	}
	other := h.Clone()
	other.Set(headerWorkspaceID, "other-workspace")
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo", nil, other); status != http.StatusNotFound {
		t.Fatalf("foreign folder = %d", status)
	}
	if status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, h); status != http.StatusOK || len(page["summaries"].([]any)) != 18 {
		t.Fatalf("legacy listing changed = %d, %v", status, page)
	}
	if status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse", nil, other); status != http.StatusOK || len(page["summaries"].([]any)) != 0 {
		t.Fatalf("foreign scope leaked summaries = %d, %v", status, page)
	}
	value := true
	if _, err := svc.UpdateMetadata(ctx, scopeID, "session-16", service.MetadataUpdate{Pinned: &value}); err != nil {
		t.Fatal(err)
	}
	status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&limit=8", nil, h)
	if status != http.StatusOK || page["summaries"].([]any)[0].(map[string]any)["conversation_id"] != "session-16" {
		t.Fatalf("pinned order = %d, %v", status, page)
	}
	if _, err := svc.UpdateMetadata(ctx, scopeID, "session-00", service.MetadataUpdate{Archived: &value}); err != nil {
		t.Fatal(err)
	}
	status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&archived=true", nil, h)
	if status != http.StatusOK || len(page["summaries"].([]any)) != 1 {
		t.Fatalf("archived page = %d, %v", status, page)
	}
	status, catalog = v2Do(t, srv, http.MethodGet, "/local/v2/session-groups", nil, h)
	if status != http.StatusOK || catalog["groups"].([]any)[0].(map[string]any)["total"] != float64(16) {
		t.Fatalf("archive count = %d, %v", status, catalog)
	}
	status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?folder_id=repo&exclude_id=session-16", nil, h)
	if status != http.StatusOK {
		t.Fatalf("exclude = %d, %v", status, page)
	}
	for _, row := range page["summaries"].([]any) {
		if row.(map[string]any)["conversation_id"] == "session-16" {
			t.Fatal("excluded current conversation was returned")
		}
	}
	if _, err := s.CreateConversation(ctx, store.NewConversation{ID: "side-child", ScopeID: scopeID, Provider: "claude", SideSourceID: "session-16", Cwd: "/repo", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status, page = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?side_source_id=session-16", nil, h)
	if status != http.StatusOK || len(page["summaries"].([]any)) != 1 || page["summaries"].([]any)[0].(map[string]any)["conversation_id"] != "side-child" {
		t.Fatalf("side page = %d, %v", status, page)
	}
}

func TestV2ScopedHelloStartsLiveListingWithoutReplayingTheDirectory(t *testing.T) {
	t.Parallel()
	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	for i := range 9 {
		if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": fmt.Sprintf("older-%d", i), "provider": "claude"}, h); status != http.StatusCreated {
			t.Fatalf("create = %d", status)
		}
	}
	status, hello := v2Do(t, srv, http.MethodGet, "/local/v2/hello?scoped=true", nil, h)
	if status != http.StatusOK || hello["scope_seq"].(float64) < 9 {
		t.Fatalf("scoped hello = %d, %v", status, hello)
	}
	if status, legacy := v2Do(t, srv, http.MethodGet, "/local/v2/hello", nil, h); status != http.StatusOK || legacy["scope_seq"] != nil {
		t.Fatalf("unscoped hello changed = %d, %v", status, legacy)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe_scope", "after_scope_seq": hello["scope_seq"]}); err != nil {
		t.Fatal(err)
	}
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "fresh", "provider": "claude"}, h)
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var message map[string]any
		if err := ws.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message["type"] != "scope.summaries" {
			continue
		}
		rows := message["summaries"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["conversation_id"] != "fresh" {
			t.Fatalf("directory was replayed: %v", rows)
		}
		break
	}
	bad := h.Clone()
	bad.Set(headerUserID, "other-user")
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/hello?scoped=true", nil, bad); status != http.StatusForbidden {
		t.Fatalf("foreign scoped hello = %d", status)
	}
}

func TestV2SessionBrowseKeepsLateOutputBehindTheSendTimeCursor(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	ctx := context.Background()
	w := svc.Store().NewWriter(store.WriterOptions{})
	defer w.Close()
	at := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c"} {
		status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": id, "provider": "claude"}, h)
		if status != http.StatusCreated {
			t.Fatal(status)
		}
		sent := at.Add(time.Duration(i) * time.Minute)
		_, err := w.Submit(ctx, store.Batch{ConversationID: id, StartTurn: &store.NewTurn{ID: id + "-turn", StartedAt: sent},
			Messages: []store.NewMessage{{ID: id + "-user", TurnID: id + "-turn", Role: "user", CreatedAt: sent}}, Summary: &store.SummaryPatch{UpdatedAt: sent}})
		if err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	for i, want := range []string{"c", "b", "a"} {
		status, page := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?limit=1&cursor="+url.QueryEscape(cursor), nil, h)
		if status != http.StatusOK {
			t.Fatalf("page = %d %v", status, page)
		}
		rows := page["summaries"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["conversation_id"] != want {
			t.Fatalf("page %d = %v, want %s", i, rows, want)
		}
		if i < 2 {
			next, ok := page["next_cursor"].(string)
			if !ok || next == "" {
				t.Fatalf("page %d lost the remaining user turns: %v", i, page)
			}
			cursor = next
		} else if page["next_cursor"] != nil {
			t.Fatalf("last page = %v", page)
		}
		if i == 0 {
			data, err := base64.RawURLEncoding.DecodeString(cursor)
			if err != nil {
				t.Fatal(err)
			}
			var old sessionBrowseCursor
			if err := json.Unmarshal(data, &old); err != nil {
				t.Fatal(err)
			}
			old.Version = 1
			data, err = json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?cursor="+base64.RawURLEncoding.EncodeToString(data), nil, h); status != http.StatusBadRequest {
				t.Fatalf("activity-based cursor accepted: %d", status)
			}
			old.Version = 2
			old.Position.SortAt = "not-a-timestamp"
			data, err = json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/browse?cursor="+base64.RawURLEncoding.EncodeToString(data), nil, h); status != http.StatusBadRequest {
				t.Fatalf("malformed cursor time accepted: %d", status)
			}
			_, err = w.Submit(ctx, store.Batch{ConversationID: "a", CompleteTurn: &store.TurnCompletion{TurnID: "a-turn", Status: "completed", CompletedAt: at.Add(time.Hour)}, Summary: &store.SummaryPatch{UpdatedAt: at.Add(time.Hour)}})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
