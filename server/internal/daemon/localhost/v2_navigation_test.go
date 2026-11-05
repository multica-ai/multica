package localhost

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestV2NavigationCountsVisibleNodesAndReadsBoundedWindows(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "nav", "provider": "claude"}, h)
	writer := svc.Store().NewWriter(store.WriterOptions{})
	defer writer.Close()
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("turn-%d", i)
		batch := store.Batch{ConversationID: "nav", StartTurn: &store.NewTurn{ID: id, StartedAt: time.Now()}, CompleteTurn: &store.TurnCompletion{TurnID: id, Status: "completed", CompletedAt: time.Now()}}
		if i%3 != 0 {
			batch.Messages = []store.NewMessage{{ID: "message-" + id, TurnID: id, Role: "user", Content: "Prompt " + id, CreatedAt: time.Now()}}
		}
		if _, err := writer.Submit(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
	}
	status, count := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation", nil, h)
	if status != 200 || count["node_count"] != float64(20) || len(count["turns"].([]any)) != 0 || count["range"] != nil {
		t.Fatalf("count=%d %v", status, count)
	}
	status, page := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?node_index=2&before=1&after=2", nil, h)
	if status != 200 {
		t.Fatalf("page=%d %v", status, page)
	}
	r := page["range"].(map[string]any)
	turns := page["turns"].([]any)
	if r["start_index"] != float64(3) || r["end_index"] != float64(6) || r["has_before"] != true || r["has_after"] != true || len(turns) != 4 {
		t.Fatalf("window=%v", page)
	}
	nodes := page["nodes"].([]any)
	if nodes[0].(map[string]any)["message_id"] != "message-turn-4" || nodes[0].(map[string]any)["node_index"] != float64(2) {
		t.Fatalf("nodes=%v", nodes)
	}
	_, older := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?before_index=3&limit=10", nil, h)
	if len(older["turns"].([]any)) != 3 || older["range"].(map[string]any)["has_before"] != false {
		t.Fatalf("older=%v", older)
	}
	_, newer := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?after_index=27&limit=10", nil, h)
	if len(newer["turns"].([]any)) != 2 || newer["range"].(map[string]any)["has_after"] != false {
		t.Fatalf("newer=%v", newer)
	}
	_, metadata := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?before_index=30&limit=10&include_body=false", nil, h)
	directory, ok := metadata["directory_range"].(map[string]any)
	if !ok || directory["start_index"] != float64(20) || directory["end_index"] != float64(29) || directory["has_before"] != true || directory["has_after"] != false || metadata["range"] != nil || len(metadata["turns"].([]any)) != 0 || len(metadata["nodes"].([]any)) != 7 {
		t.Fatalf("metadata paging must be independent of body loading: %v", metadata)
	}
	_, empty := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?before_index=1&limit=1&include_body=false", nil, h)
	if len(empty["nodes"].([]any)) != 0 || empty["directory_range"].(map[string]any)["has_after"] != true {
		t.Fatalf("hidden turns must retain metadata paging boundaries: %v", empty)
	}
	for _, query := range []string{"node_index=-1", "node_index=x", "node_index=1&before_index=2", "before=20&after=0&node_index=0", "limit=0", "before_index=31", "after_index=30", "node_index=1&node_index=2"} {
		status, body := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?"+query, nil, h)
		if status != 400 {
			t.Errorf("query %s=%d %v", query, status, body)
		}
	}
	status, _ = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation?node_index=20", nil, h)
	if status != 404 {
		t.Fatalf("missing node=%d", status)
	}
	other := h.Clone()
	other.Set(headerWorkspaceID, "ws-2")
	status, _ = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/nav/navigation", nil, other)
	if status != 404 {
		t.Fatalf("other workspace=%d", status)
	}
}
