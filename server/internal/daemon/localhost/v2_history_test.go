package localhost

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestV2HistoryPagesWithoutCrossingWorkspace(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "history", "provider": "claude"}, h)
	if status != http.StatusCreated {
		t.Fatal(status)
	}
	writer := svc.Store().NewWriter(store.WriterOptions{})
	defer writer.Close()
	for i := 0; i < 45; i++ {
		id := fmt.Sprintf("history-%d", i)
		_, err := writer.Submit(context.Background(), store.Batch{ConversationID: "history", StartTurn: &store.NewTurn{ID: id, StartedAt: time.Now()}, Messages: []store.NewMessage{{ID: "message-" + id, TurnID: id, Role: "user", Content: id, CreatedAt: time.Now()}}, CompleteTurn: &store.TurnCompletion{TurnID: id, Status: "completed", CompletedAt: time.Now()}})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/snapshot", nil, h)
	if len(snap["turns"].([]any)) != 20 || snap["has_older_turns"] != true {
		t.Fatalf("snapshot = %v", snap)
	}
	status, page := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/history?before_index=25", nil, h)
	turns := page["turns"].([]any)
	if status != http.StatusOK || len(turns) != 20 || turns[0].(map[string]any)["turn_index"] != float64(5) || page["has_older_turns"] != true {
		t.Fatalf("page = %d %v", status, page)
	}
	_, last := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/history?before_index=5", nil, h)
	if len(last["turns"].([]any)) != 5 || last["has_older_turns"] != false {
		t.Fatalf("last = %v", last)
	}
	for _, cursor := range []string{"", "-1", "invalid", "46"} {
		status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/history?before_index="+cursor, nil, h)
		if status != http.StatusBadRequest {
			t.Fatalf("cursor %q = %d", cursor, status)
		}
	}
	_, first := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/snapshot?turn_limit=10", nil, h)
	firstTurns := first["turns"].([]any)
	if len(firstTurns) != 10 || firstTurns[0].(map[string]any)["turn_index"] != float64(35) || first["has_older_turns"] != true {
		t.Fatalf("limited snapshot = %v", first)
	}
	_, older := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/history?before_index=35&turn_limit=10", nil, h)
	olderTurns := older["turns"].([]any)
	if len(olderTurns) != 10 || olderTurns[0].(map[string]any)["turn_index"] != float64(25) || older["has_older_turns"] != true {
		t.Fatalf("limited page = %v", older)
	}
	for _, limit := range []string{"0", "21", "-1", "many"} {
		for _, path := range []string{"snapshot?turn_limit=", "history?before_index=25&turn_limit="} {
			status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/"+path+limit, nil, h)
			if status != http.StatusBadRequest {
				t.Fatalf("%s%s = %d", path, limit, status)
			}
		}
	}
	other := h.Clone()
	other.Set(headerWorkspaceID, "ws-2")
	status, _ = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/history/history?before_index=25", nil, other)
	if status != http.StatusNotFound {
		t.Fatalf("other workspace = %d", status)
	}
}

func TestV2SummaryKeepsTurnSendTimeAfterLateOutput(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "recency", "provider": "claude"}, h)
	if status != http.StatusCreated {
		t.Fatal(status)
	}
	ctx := context.Background()
	w := svc.Store().NewWriter(store.WriterOptions{})
	defer w.Close()
	sent := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	late := sent.Add(2 * time.Minute)
	for _, batch := range []store.Batch{
		{ConversationID: "recency", StartTurn: &store.NewTurn{ID: "recency-turn", StartedAt: sent},
			Messages: []store.NewMessage{{ID: "recency-user", TurnID: "recency-turn", Role: "user", Content: "hello", CreatedAt: sent}},
			Summary:  &store.SummaryPatch{UpdatedAt: sent}},
		{ConversationID: "recency", Messages: []store.NewMessage{{ID: "recency-reply", TurnID: "recency-turn", Role: "assistant", Content: "late reply", CreatedAt: late}},
			CompleteTurn: &store.TurnCompletion{TurnID: "recency-turn", Status: "completed", CompletedAt: late},
			Summary:      &store.SummaryPatch{UpdatedAt: late}},
	} {
		if _, err := w.Submit(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	_, list := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, h)
	row := list["summaries"].([]any)[0].(map[string]any)
	if row["last_turn_sent_at"] != sent.Format(time.RFC3339Nano) || row["updated_at"] != late.Format(time.RFC3339Nano) {
		t.Fatalf("summary must separate send and activity times: %v", row)
	}
	_, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/recency/snapshot", nil, h)
	if snap["conversation"].(map[string]any)["last_turn_sent_at"] != row["last_turn_sent_at"] {
		t.Fatalf("snapshot disagrees with list: %v", snap)
	}
}

func TestV2AttachmentMetadataIsScoped(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	ctx := context.Background()
	scope, err := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := svc.Upload(ctx, scope, "", "guide.pdf", "application/pdf", strings.NewReader("%PDF-test"))
	if err != nil {
		t.Fatal(err)
	}
	path := "/local/v2/attachments/" + attachment.ID + "/metadata"
	status, metadata := v2Do(t, srv, http.MethodGet, path, nil, v2Headers("user-1"))
	if status != http.StatusOK || metadata["name"] != "guide.pdf" || metadata["mime_type"] != "application/pdf" {
		t.Fatalf("metadata = %d %v", status, metadata)
	}
	other := v2Headers("user-1")
	other.Set(headerWorkspaceID, "ws-2")
	status, _ = v2Do(t, srv, http.MethodGet, path, nil, other)
	if status != http.StatusNotFound {
		t.Fatalf("other workspace = %d", status)
	}
}

func TestV2UploadsCanBeLinkedAndDiscarded(t *testing.T) {
	t.Parallel()
	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	const id = "00000000000000000000000000000001"
	upload := func(id, content string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/local/v2/attachments", strings.NewReader(content))
		req.Header = h.Clone()
		req.Header.Set("Content-Type", "text/plain")
		req.Header.Set("X-File-Name", "a.txt")
		req.Header.Set("X-Attachment-ID", id)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	if res := upload(id, "hello"); res.StatusCode != http.StatusCreated {
		t.Fatalf("upload = %d", res.StatusCode)
	}
	// SHA-256 of "hello".
	const sum = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	const linkedID = "00000000000000000000000000000002"
	link := func(id, sum string) (int, map[string]any) {
		return v2Do(t, srv, http.MethodPost, "/local/v2/attachments/link", map[string]any{"id": id, "sha256": sum, "name": "b.txt", "mime_type": "text/plain"}, h)
	}
	if status, _ := link("00000000000000000000000000000003", strings.Repeat("0", 64)); status != http.StatusNotFound {
		t.Fatalf("unknown content = %d", status)
	}
	if status, _ := link(linkedID, "short"); status != http.StatusBadRequest {
		t.Fatalf("invalid hash = %d", status)
	}
	status, linked := link(linkedID, sum)
	if status != http.StatusCreated || linked["id"] != linkedID || linked["name"] != "b.txt" || linked["size"] != float64(5) {
		t.Fatalf("link = %d %v", status, linked)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/local/v2/attachments/"+linkedID, nil)
	req.Header = h.Clone()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if cache := res.Header.Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("attachment Cache-Control = %q", cache)
	}

	if status, _ := v2Do(t, srv, http.MethodDelete, "/local/v2/attachments/"+linkedID, nil, h); status != http.StatusNoContent {
		t.Fatalf("discard = %d", status)
	}
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/attachments/"+linkedID+"/metadata", nil, h); status != http.StatusNotFound {
		t.Fatalf("after discard = %d", status)
	}
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/attachments/"+id+"/metadata", nil, h); status != http.StatusOK {
		t.Fatalf("the shared attachment = %d", status)
	}
}

func TestRemoteSnapshotRevalidatesWithETag(t *testing.T) {
	t.Parallel()
	_, svc, v := newV2Fixture(t)
	ctx := context.Background()
	scope, err := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, service.CreateCommand{ScopeID: scope, ID: "conv", Provider: "claude"}); err != nil {
		t.Fatal(err)
	}
	var open atomic.Bool
	open.Store(true)
	srv := remoteServer(t, v, RemotePeer{ID: "peer-1", DeviceName: "phone"}, &open)
	read := func(etag string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/local/v2/sessions/conv/snapshot", nil)
		req.Header = remoteHeaders("client-1")
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	first := read("")
	etag := first.Header.Get("ETag")
	if first.StatusCode != http.StatusOK || etag == "" || !strings.Contains(first.Header.Get("Cache-Control"), "no-cache") {
		t.Fatalf("first read = %d %v", first.StatusCode, first.Header)
	}
	if again := read(etag); again.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged read = %d", again.StatusCode)
	}
	if changed := read(`"stale"`); changed.StatusCode != http.StatusOK {
		t.Fatalf("changed read = %d", changed.StatusCode)
	}
}

func TestSnapshotDescribesItemsAcrossItsWindow(t *testing.T) {
	t.Parallel()
	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "window", "provider": "claude"}, h); status != http.StatusCreated {
		t.Fatal(status)
	}
	writer := svc.Store().NewWriter(store.WriterOptions{})
	defer writer.Close()
	submit := func(batch store.Batch) {
		t.Helper()
		batch.ConversationID = "window"
		if _, err := writer.Submit(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
	}
	at := func(minute int) time.Time { return time.Date(2026, 11, 3, 10, minute, 0, 0, time.UTC) }
	// The oldest turn starts a background task that outlives the snapshot's window.
	submit(store.Batch{StartTurn: &store.NewTurn{ID: "starter", StartedAt: at(0)}, Events: []store.NewEvent{
		{TurnID: "starter", ItemID: "task", Type: "item.started", Payload: []byte(`{"kind":"background_task"}`), CreatedAt: at(0)},
		{TurnID: "starter", ItemID: "tool", Type: "item.started", Payload: []byte(`{"kind":"tool_call"}`), CreatedAt: at(1)},
		{TurnID: "starter", ItemID: "tool", Type: "item.completed", Final: true, Payload: []byte(`{"kind":"tool_call","status":"completed"}`), CreatedAt: at(2)},
	}})
	submit(store.Batch{StartTurn: &store.NewTurn{ID: "later", StartedAt: at(4)}})

	_, narrow := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/window/snapshot?turn_limit=1", nil, h)
	open := narrow["open_background_items"].([]any)
	if len(open) != 1 || open[0].(map[string]any)["item_id"] != "task" {
		t.Fatalf("open background items = %v", open)
	}
	_, wide := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/window/snapshot", nil, h)
	if len(wide["open_background_items"].([]any)) != 0 {
		t.Fatalf("a loaded turn's item repeated: %v", wide["open_background_items"])
	}
	for _, event := range wide["turns"].([]any)[0].(map[string]any)["events"].([]any) {
		e := event.(map[string]any)
		if e["item_id"] == "tool" && (e["first_seq"] == nil || e["first_seq"].(float64) >= e["seq"].(float64) || e["first_at"] != at(1).Format(time.RFC3339Nano)) {
			t.Fatalf("final event's start = %v", e)
		}
	}
}
