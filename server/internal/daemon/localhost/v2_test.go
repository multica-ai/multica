package localhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/multica-ai/multica/server/internal/localconversation/protocol"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/service"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

const v2Token = "test-token"

// autoAdapter completes every turn right after its prompt is sent.
type autoAdapter struct {
	handler claude.Handler
	done    chan struct{}
	once    sync.Once
}

func (a *autoAdapter) Request(_ context.Context, typ string, _ map[string]any) (json.RawMessage, error) {
	if typ == "send" {
		go func() {
			a.handler(claude.Output{Type: "sdk_message", Message: json.RawMessage(`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"done"}]}}`)})
			a.handler(claude.Output{Type: "sdk_message", Message: json.RawMessage(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)})
		}()
	}
	return json.RawMessage(`{}`), nil
}
func (a *autoAdapter) Close(context.Context) error { a.once.Do(func() { close(a.done) }); return nil }
func (a *autoAdapter) Done() <-chan struct{}       { return a.done }
func (a *autoAdapter) Err() error                  { return nil }

func newV2Server(t *testing.T) (*httptest.Server, *service.Service) {
	t.Helper()
	srv, svc, _ := newV2Fixture(t)
	return srv, svc
}

func newV2Fixture(t *testing.T) (*httptest.Server, *service.Service, *V2) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, desktopUserIDFile), []byte("user-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc, err := service.Open(context.Background(), service.Options{
		Path: filepath.Join(dir, "local-conversations", "conversations.sqlite"),
		Claude: func(context.Context, sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
			return service.ClaudeSession{Fields: map[string]any{"executable": "/bin/claude"}}, func(_ context.Context, h claude.Handler) (service.ClaudeAdapter, error) {
				return &autoAdapter{handler: h, done: make(chan struct{})}, nil
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	mux := http.NewServeMux()
	v := NewV2(svc, v2Token, dir)
	v.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, svc, v
}

func v2Headers(user string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+v2Token)
	h.Set(headerAPIOrigin, "https://api.multica.ai")
	h.Set(headerUserID, user)
	h.Set(headerWorkspaceID, "ws-1")
	return h
}

func TestV2CompactValidatesHistoryAndRequestIdentity(t *testing.T) {
	t.Parallel()
	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "compact-empty", "provider": "claude"}, h)
	if status != http.StatusCreated {
		t.Fatalf("create = %d, %v", status, body)
	}
	path := "/local/v2/sessions/compact-empty/commands"
	status, body = v2Do(t, srv, http.MethodPost, path, map[string]any{"type": "compact"}, h)
	if status != http.StatusBadRequest {
		t.Fatalf("missing request = %d, %v", status, body)
	}
	status, body = v2Do(t, srv, http.MethodPost, path, map[string]any{"type": "compact", "request_id": "compact-1"}, h)
	apiError, _ := body["error"].(map[string]any)
	if status != http.StatusConflict || apiError["kind"] != "compact_unavailable" {
		t.Fatalf("empty history = %d, %v", status, body)
	}
}

func v2Do(t *testing.T, srv *httptest.Server, method, path string, body any, header http.Header) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, srv.URL+path, reader)
	req.Header = header
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func serverMessageSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	for id, path := range protocol.SchemaFiles {
		raw, err := protocol.Schemas.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddResource(id, doc); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := c.Compile(protocol.LocalV2ID + "#/$defs/server_message")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestV2RejectsUnauthorizedAndUnscopedRequests(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	noToken := v2Headers("user-1")
	noToken.Del("Authorization")
	if status, _ := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, noToken); status != http.StatusForbidden {
		t.Fatalf("no token = %d", status)
	}
	noScope := http.Header{"Authorization": {"Bearer " + v2Token}}
	if status, body := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, noScope); status != http.StatusBadRequest || body["error"].(map[string]any)["kind"] != "invalid_scope" {
		t.Fatalf("no scope = %d %v", status, body)
	}
	if status, body := v2Do(t, srv, http.MethodGet, "/local/v2/sessions", nil, v2Headers("someone-else")); status != http.StatusForbidden || body["error"].(map[string]any)["kind"] != "account_mismatch" {
		t.Fatalf("other account = %d %v", status, body)
	}
}

func TestV2DrainAndUndrain(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	tokenOnly := http.Header{"Authorization": {"Bearer " + v2Token}}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/drain", nil, http.Header{}); status != http.StatusForbidden {
		t.Fatalf("drain without token = %d", status)
	}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/drain", nil, tokenOnly); status != http.StatusOK || body["busy"] != float64(0) {
		t.Fatalf("drain = %d %v", status, body)
	}
	create := map[string]any{"id": "conv-1", "provider": "claude"}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", create, v2Headers("user-1")); status != http.StatusCreated {
		t.Fatalf("create = %d", status)
	}
	send := map[string]any{"type": "send", "request_id": "r1", "text": "hi"}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", send, v2Headers("user-1")); status != http.StatusServiceUnavailable || body["error"].(map[string]any)["kind"] != "draining" {
		t.Fatalf("send while draining = %d %v", status, body)
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/stop-providers", nil, tokenOnly); status != http.StatusOK {
		t.Fatalf("stop providers = %d", status)
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/undrain", nil, tokenOnly); status != http.StatusOK {
		t.Fatalf("undrain = %d", status)
	}
}

func TestV2SendSnapshotAndCatchUpOverWebSocket(t *testing.T) {
	t.Parallel()

	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude", "cwd": "/repo"}, h); status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, body)
	}
	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "req-1", "text": "hi"}, h)
	if status != http.StatusAccepted || body["turn_id"] != "turn-req-1" || body["replayed"] != false {
		t.Fatalf("send = %d %v", status, body)
	}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "req-1", "text": "hi"}, h); status != http.StatusAccepted || body["replayed"] != true {
		t.Fatalf("retry = %d %v", status, body)
	}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "req-1", "text": "changed"}, h); status != http.StatusConflict {
		t.Fatalf("conflict = %d %v", status, body)
	}

	schema := serverMessageSchema(t)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/local/v2/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, h)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err := ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe", "conversation_id": "conv-1", "after_seq": 0, "store_generation": svc.Generation()}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var types []string
	var statuses []string
	for !containsString(types, "turn.completed") {
		var raw json.RawMessage
		if err := ws.ReadJSON(&raw); err != nil {
			t.Fatalf("read: %v (events %v)", err, types)
		}
		doc, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("server message violates the v2 schema: %v\n%s", err, raw)
		}
		var msg struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		}
		_ = json.Unmarshal(raw, &msg)
		if msg.Type == "subscription.status" {
			statuses = append(statuses, msg.Status)
		}
		for _, e := range msg.Events {
			types = append(types, e.Type)
		}
	}
	if fmt.Sprint(types) != "[user.message turn.started item.completed turn.completed]" || statuses[0] != "subscribed" {
		t.Fatalf("events %v statuses %v", types, statuses)
	}

	status, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/snapshot", nil, h)
	if status != http.StatusOK || snap["snapshot_seq"].(float64) < 4 || len(snap["turns"].([]any)) != 1 {
		t.Fatalf("snapshot = %d %v", status, snap)
	}
	turn := snap["turns"].([]any)[0].(map[string]any)
	if turn["status"] != "completed" || len(turn["messages"].([]any)) != 2 {
		t.Fatalf("snapshot turn = %v", turn)
	}
}

func TestV2SubscriptionRequiresTheCurrentStoreGeneration(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, h)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe", "conversation_id": "conv-1", "after_seq": 0, "store_generation": "old"})
	var msg map[string]any
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := ws.ReadJSON(&msg); err != nil || msg["status"] != "snapshot_required" {
		t.Fatalf("status = %v, %v", msg, err)
	}
}

func TestV2ScopeSubscriptionStreamsSummaries(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	schema := serverMessageSchema(t)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.WriteJSON(map[string]any{"version": 2, "type": "subscribe_scope", "after_scope_seq": 0})
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude", "title": "排查"}, h)
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "send", "request_id": "r", "text": "hi"}, h)

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	seen := map[string]bool{}
	for !seen["idle"] || !seen["running"] {
		var raw json.RawMessage
		if err := ws.ReadJSON(&raw); err != nil {
			t.Fatalf("read: %v (seen %v)", err, seen)
		}
		doc, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err := schema.Validate(doc); err != nil {
			t.Fatalf("server message violates the v2 schema: %v\n%s", err, raw)
		}
		var msg struct {
			Summaries []struct {
				Status string `json:"status"`
			} `json:"summaries"`
		}
		_ = json.Unmarshal(raw, &msg)
		for _, s := range msg.Summaries {
			seen[s.Status] = true
		}
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestV2UpdateSettingsCommand(t *testing.T) {
	t.Parallel()

	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, h)
	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "update_settings", "model": "claude-opus-5-5", "permission_mode": "full-access"}, h)
	if status != http.StatusOK {
		t.Fatalf("update = %d %v", status, body)
	}
	conv, _ := svc.Store().Conversation(context.Background(), "conv-1")
	if conv.Model.String != "claude-opus-5-5" || conv.PermissionMode != "full-access" {
		t.Fatalf("stored = %+v", conv)
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "update_settings", "model": "default"}, h); status != http.StatusOK {
		t.Fatal("could not store the old default row")
	}
	if _, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/snapshot", nil, h); snap["settings"].(map[string]any)["model"] != nil {
		t.Fatalf("the old default row must read as following the default: %v", snap["settings"])
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "update_settings", "model": nil}, h); status != http.StatusOK {
		t.Fatalf("reset model = %d", status)
	}
	if conv, _ := svc.Store().Conversation(context.Background(), "conv-1"); conv.Model.Valid || conv.PermissionMode != "full-access" {
		t.Fatalf("after reset = %+v", conv)
	}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "update_settings", "permission_mode": "yolo"}, h); status != http.StatusBadRequest || body["error"].(map[string]any)["kind"] != "invalid_settings" {
		t.Fatalf("invalid = %d %v", status, body)
	}
}

func TestV2AnswerCommandAndPendingQuestionsInSnapshot(t *testing.T) {
	t.Parallel()

	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, h)
	w := svc.Store().NewWriter(store.WriterOptions{})
	defer w.Close()
	question := `{"request_id":"q1","questions":[{"id":"go","question":"继续吗？","options":[{"label":"是"},{"label":"否"}],"multiSelect":false,"allowFreeText":false}]}`
	_, err := w.Submit(context.Background(), store.Batch{ConversationID: "conv-1", StartTurn: &store.NewTurn{ID: "turn-1", StartedAt: time.Now()},
		Interactions: []store.NewInteraction{{ID: "question:turn-1:q1", TurnID: "turn-1", Kind: "question", Request: json.RawMessage(question), CreatedAt: time.Now()}}})
	if err != nil {
		t.Fatal(err)
	}
	_, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/snapshot", nil, h)
	pending := snap["pending_interactions"].([]any)
	if len(pending) != 1 || pending[0].(map[string]any)["interaction_id"] != "question:turn-1:q1" {
		t.Fatalf("pending = %v", pending)
	}
	answer := func(value string) (int, map[string]any) {
		return v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands",
			map[string]any{"type": "answer", "interaction_id": "question:turn-1:q1", "answer": map[string]any{"go": value}}, h)
	}
	if status, body := answer("是"); status != http.StatusOK {
		t.Fatalf("answer = %d %v", status, body)
	}
	if status, body := answer("否"); status != http.StatusConflict || body["error"].(map[string]any)["kind"] != "already_answered" {
		t.Fatalf("second answer = %d %v", status, body)
	}
	_, snap = v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/snapshot", nil, h)
	if len(snap["pending_interactions"].([]any)) != 0 || snap["conversation"].(map[string]any)["pending_interactions"].(float64) != 0 {
		t.Fatalf("snapshot after answer = %v", snap)
	}
}

func TestV2ManagesConversationsAndFolderProjects(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	folder := t.TempDir()
	status, project := v2Do(t, srv, http.MethodPost, "/local/v2/folder-projects", map[string]any{"path": folder}, h)
	if status != http.StatusOK || project["id"] == "" {
		t.Fatalf("add folder = %d %v", status, project)
	}
	_, list := v2Do(t, srv, http.MethodGet, "/local/v2/folder-projects", nil, h)
	if len(list["projects"].([]any)) != 1 {
		t.Fatalf("folder projects = %v", list)
	}
	status, created := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude", "folder_project_id": project["id"]}, h)
	if status != http.StatusCreated || created["cwd"] != project["path"] || created["title"] != service.DefaultTitle {
		t.Fatalf("create = %d %v", status, created)
	}
	_, source := v2Do(t, srv, http.MethodGet, "/local/v2/directory-source?cwd="+url.QueryEscape(project["path"].(string)), nil, h)
	if source["id"] != "conv-1" {
		t.Fatalf("directory source = %v", source)
	}
	command := func(body map[string]any) (int, map[string]any) {
		return v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", body, h)
	}
	if status, body := command(map[string]any{"type": "update", "title": "Plan", "pinned": true, "project_id": "p-1"}); status != http.StatusOK || body["title"] != "Plan" || body["pinned"] != true || body["project_id"] != "p-1" {
		t.Fatalf("update = %d %v", status, body)
	}
	if status, body := command(map[string]any{"type": "update", "project_id": nil}); status != http.StatusOK || body["project_id"] != nil || body["pinned"] != true {
		t.Fatalf("clear project = %d %v", status, body)
	}
	if status, body := command(map[string]any{"type": "set_directory", "cwd": "relative"}); status != http.StatusBadRequest || body["error"].(map[string]any)["kind"] != "invalid_request" {
		t.Fatalf("bad directory = %d %v", status, body)
	}
	if status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-2", "provider": "gemini"}, h); status != http.StatusBadRequest {
		t.Fatalf("unknown provider = %d %v", status, body)
	}
}

func TestV2UploadsAndServesFiles(t *testing.T) {
	t.Parallel()

	srv, svc := newV2Server(t)
	h := v2Headers("user-1")
	raw := func(method, path string, body string, header http.Header) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header = header.Clone()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	upload := h.Clone()
	upload.Set("Content-Type", "text/plain")
	upload.Set("X-File-Name", url.QueryEscape("说明 1.txt"))
	res := raw(http.MethodPost, "/local/v2/attachments", "hello world", upload)
	var created map[string]any
	_ = json.NewDecoder(res.Body).Decode(&created)
	if res.StatusCode != http.StatusCreated || created["name"] != "说明 1.txt" || created["size"].(float64) != 11 {
		t.Fatalf("upload = %d %v", res.StatusCode, created)
	}
	ranged := h.Clone()
	ranged.Set("Range", "bytes=6-")
	res = raw(http.MethodGet, "/local/v2/attachments/"+created["id"].(string), "", ranged)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusPartialContent || string(body) != "world" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download = %d %q %v", res.StatusCode, body, res.Header)
	}
	other := v2Headers("user-1")
	other.Set(headerWorkspaceID, "ws-2")
	if res := raw(http.MethodGet, "/local/v2/attachments/"+created["id"].(string), "", other); res.StatusCode != http.StatusNotFound {
		t.Fatalf("download from another workspace = %d", res.StatusCode)
	}

	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	if _, err := svc.Store().CreateConversation(context.Background(), store.NewConversation{ID: "conv-1", ScopeID: 1, Provider: "claude", Cwd: cwd}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "a.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if res := raw(http.MethodGet, "/local/v2/sessions/conv-1/image?path="+url.QueryEscape(filepath.Join(cwd, "a.png")), "", h); res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("image = %d %v", res.StatusCode, res.Header)
	}
	status, failure := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/image?path="+url.QueryEscape(filepath.Join(t.TempDir(), "x.png")), nil, h)
	if status != http.StatusBadRequest && status != http.StatusForbidden {
		t.Fatalf("image outside = %d %v", status, failure)
	}
}

func TestV2ProviderRoutes(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	status, body := v2Do(t, srv, http.MethodGet, "/local/v2/providers", nil, h)
	if status != http.StatusOK || len(body["providers"].([]any)) != 2 {
		t.Fatalf("providers = %d %v", status, body)
	}
	if status, body := v2Do(t, srv, http.MethodGet, "/local/v2/models?provider=claude", nil, h); status != http.StatusBadRequest || body["error"].(map[string]any)["kind"] != "unsupported_provider" {
		t.Fatalf("models without catalog = %d %v", status, body)
	}
}

func TestV2QueueCommands(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, h)
	item := map[string]any{"requestId": "q1", "content": "later", "display": map[string]any{"id": "q1", "createdAtUnixMs": 1}}
	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "queue_enqueue", "item": item, "paused": true}, h)
	if status != http.StatusOK || body["paused"] != true || len(body["items"].([]any)) != 1 {
		t.Fatalf("enqueue = %d %v", status, body)
	}
	_, queue := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/queue", nil, h)
	if queue["revision"].(float64) != 1 {
		t.Fatalf("queue = %v", queue)
	}
	status, body = v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "queue_update", "action": "remove", "request_id": "q1"}, h)
	if status != http.StatusOK || len(body["items"].([]any)) != 0 {
		t.Fatalf("remove = %d %v", status, body)
	}
	_, snap := v2Do(t, srv, http.MethodGet, "/local/v2/sessions/conv-1/snapshot", nil, h)
	if snap["conversation"].(map[string]any)["queue_count"].(float64) != 0 {
		t.Fatalf("summary = %v", snap["conversation"])
	}
}

func TestV2HostRequestsReachTheConnectedDesktop(t *testing.T) {
	t.Parallel()

	srv, svc, v := newV2Fixture(t)
	h := v2Headers("user-1")
	scopeID, _ := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if _, err := v.hosts.request(context.Background(), scopeID, "", "settings.confirm", map[string]any{}); !errors.Is(err, service.ErrHostUnavailable) {
		t.Fatalf("without Desktop = %v", err)
	}
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/local/v2/ws", h)
	if err != nil {
		t.Fatal(err)
	}
	// The scope is attached once the connection is served.
	eventuallyV2(t, func() bool {
		v.hosts.mu.Lock()
		defer v.hosts.mu.Unlock()
		return len(v.hosts.byScope[scopeID]) == 1
	})
	schema := serverMessageSchema(t)
	answer := func(reply func(id string) map[string]any) chan struct{} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			var raw json.RawMessage
			_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			if err := ws.ReadJSON(&raw); err != nil {
				t.Error(err)
				return
			}
			doc, _ := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err := schema.Validate(doc); err != nil {
				t.Errorf("host.request violates the schema: %v", err)
			}
			var msg struct {
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(raw, &msg)
			_ = ws.WriteJSON(reply(msg.RequestID))
		}()
		return done
	}

	done := answer(func(id string) map[string]any {
		return map[string]any{"version": 2, "type": "host.reply", "request_id": id, "result": map[string]any{"approved": true}}
	})
	result, err := v.hosts.request(context.Background(), scopeID, "", "settings.confirm", map[string]any{"operation": "x"})
	<-done
	if err != nil || string(result) != `{"approved":true}` {
		t.Fatalf("result = %s, %v", result, err)
	}
	done = answer(func(id string) map[string]any {
		return map[string]any{"version": 2, "type": "host.reply", "request_id": id, "error": map[string]any{"kind": "declined", "message": "no"}}
	})
	var hostErr *service.HostError
	_, err = v.hosts.request(context.Background(), scopeID, "", "settings.confirm", map[string]any{})
	<-done
	if !errors.As(err, &hostErr) || hostErr.Kind != "declined" {
		t.Fatalf("declined = %v", err)
	}

	// A request pending when Desktop disconnects fails instead of hanging.
	go func() {
		var raw json.RawMessage
		_ = ws.ReadJSON(&raw)
		_ = ws.Close()
	}()
	if _, err := v.hosts.request(context.Background(), scopeID, "", "settings.confirm", map[string]any{}); !errors.Is(err, service.ErrHostUnavailable) {
		t.Fatalf("after disconnect = %v", err)
	}
}

func eventuallyV2(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// newFocusV2Fixture counts the adapter processes a conversation's focus starts.
func newFocusV2Fixture(t *testing.T) (*httptest.Server, *V2, *service.Service, int64, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, desktopUserIDFile), []byte("user-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	starts := &atomic.Int32{}
	svc, err := service.Open(context.Background(), service.Options{Path: filepath.Join(dir, "c.sqlite"), Claude: func(context.Context, sqlitedb.Conversation) (service.ClaudeSession, service.StartClaudeAdapter, error) {
		return service.ClaudeSession{Fields: map[string]any{"executable": "/bin/claude"}}, func(_ context.Context, h claude.Handler) (service.ClaudeAdapter, error) {
			starts.Add(1)
			return &autoAdapter{handler: h, done: make(chan struct{})}, nil
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	mux := http.NewServeMux()
	v := NewV2(svc, v2Token, dir)
	v.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	scopeID, err := svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "user-1", WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store().CreateConversation(context.Background(), store.NewConversation{ID: "old", ScopeID: scopeID, Provider: "claude", ProviderSessionID: "native-1", Cwd: dir, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return srv, v, svc, scopeID, starts
}

func TestV2HelloAdvertisesConversationFocus(t *testing.T) {
	t.Parallel()
	srv, _ := newV2Server(t)
	status, body := v2Do(t, srv, http.MethodGet, "/local/v2/hello", nil, v2Headers("user-1"))
	capabilities, _ := body["capabilities"].([]any)
	if status != http.StatusOK || !slices.Contains(capabilities, any(ConversationFocusCapability)) {
		t.Fatalf("hello = %d, %v", status, body)
	}
}

func TestV2FocusPreheatsWithoutActingOnTheConversation(t *testing.T) {
	t.Parallel()
	srv, v, _, _, starts := newFocusV2Fixture(t)
	h := v2Headers("user-1")
	h.Set(headerClientID, "client-1")
	conn := dialWS(t, srv.URL, h)
	focus := func(id string, focused bool) {
		t.Helper()
		if err := conn.WriteJSON(map[string]any{"version": V2Version, "type": "conversation.focus", "conversation_id": id, "focused": focused}); err != nil {
			t.Fatal(err)
		}
	}
	focus("missing", true) // Refused without a reply or a closed connection.
	focus("old", true)
	deadline := time.Now().Add(3 * time.Second)
	for starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if starts.Load() != 1 {
		t.Fatalf("adapter starts = %d", starts.Load())
	}
	if origin := v.origin("old"); origin != "" {
		t.Fatalf("focus counted as acting on the conversation: %q", origin)
	}
}

func TestV2RemoteFocusPreheatsOnThisDevice(t *testing.T) {
	t.Parallel()
	_, v, _, _, starts := newFocusV2Fixture(t)
	open := &atomic.Bool{}
	open.Store(true)
	remote := remoteServer(t, v, RemotePeer{ID: "peer-1", DeviceName: "Phone"}, open)
	conn := dialWS(t, remote.URL, remoteHeaders("client-1"))
	if err := conn.WriteJSON(map[string]any{"version": V2Version, "type": "conversation.focus", "conversation_id": "old", "focused": true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for starts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if starts.Load() != 1 {
		t.Fatalf("adapter starts = %d", starts.Load())
	}
}

func TestV2PromptStatuses(t *testing.T) {
	t.Parallel()

	srv, _ := newV2Server(t)
	h := v2Headers("user-1")
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions", map[string]any{"id": "conv-1", "provider": "claude"}, h)
	item := map[string]any{"requestId": "q1", "content": "later", "display": map[string]any{"id": "q1", "createdAtUnixMs": 1}}
	v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/commands", map[string]any{"type": "queue_enqueue", "item": item, "paused": true}, h)

	status, body := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/conv-1/queue/status", map[string]any{"request_ids": []string{"q1", "never"}}, h)
	statuses, _ := body["statuses"].(map[string]any)
	if status != http.StatusOK || statuses["q1"] != "queued" || statuses["never"] != "none" {
		t.Fatalf("statuses = %d %v", status, body)
	}
	if status, _ := v2Do(t, srv, http.MethodPost, "/local/v2/sessions/other/queue/status", map[string]any{"request_ids": []string{"q1"}}, h); status != http.StatusNotFound {
		t.Fatalf("another conversation = %d", status)
	}
	_, hello := v2Do(t, srv, http.MethodGet, "/local/v2/hello", nil, h)
	if capabilities, _ := hello["capabilities"].([]any); !slices.Contains(capabilities, any(PromptStatusCapability)) {
		t.Fatalf("capabilities = %v", capabilities)
	}
}
