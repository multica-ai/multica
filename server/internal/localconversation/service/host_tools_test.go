package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestUploadAgentAvatarOnce(t *testing.T) {
	t.Parallel()

	f, cloud := remoteFixture(t)
	ctx := context.Background()
	cwd, _ := filepath.EvalSymlinks(t.TempDir())
	if _, err := f.svc.Store().CreateConversation(ctx, store.NewConversation{ID: "conv-a", ScopeID: f.scopeID, Provider: "claude", Cwd: cwd}); err != nil {
		t.Fatal(err)
	}
	conv, _ := f.svc.Store().Conversation(ctx, "conv-a")
	png := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 32)...)
	must(t, os.WriteFile(filepath.Join(cwd, "logo.png"), png, 0o600))
	must(t, os.WriteFile(filepath.Join(cwd, "fake.png"), []byte("GIF89a not a png"), 0o600))
	outside := filepath.Join(t.TempDir(), "x.png")
	must(t, os.WriteFile(outside, png, 0o600))
	tool := f.svc.avatarTool(conv, cloud)
	call := func(args string) (map[string]any, error) {
		res, err := tool.Call(ctx, json.RawMessage(args))
		v, _ := res.Value.(map[string]any)
		return v, err
	}
	if _, err := call(`{"agent_id":"agent-1","file_path":"` + outside + `","request_id":"a0"}`); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("outside file = %v", err)
	}
	if _, err := call(`{"agent_id":"agent-1","file_path":"` + filepath.Join(cwd, "fake.png") + `","request_id":"a1"}`); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched content = %v", err)
	}
	args := `{"agent_id":"agent-1","file_path":"` + filepath.Join(cwd, "logo.png") + `","request_id":"a2"}`
	v, err := call(args)
	if err != nil || v["status"] != "updated" || v["attachment_id"] != "att-1" {
		t.Fatalf("upload = %v, %v", v, err)
	}
	if again, err := call(args); err != nil || again["status"] != "updated" || cloud.uploads != 1 {
		t.Fatalf("retry = %v, %v (uploads %d)", again, err, cloud.uploads)
	}
}

func TestDeveloperLogsExportThroughDesktopOnce(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	var queries []string
	f.svc.SetHost(func(context.Context, int64, string, string, any) (json.RawMessage, error) {
		t.Fatal("developer logs must not go to a host that may be another device")
		return nil, nil
	})
	f.svc.SetLocalHost(func(_ context.Context, _ int64, _ string, kind string, payload any) (json.RawMessage, error) {
		data, _ := json.Marshal(payload)
		queries = append(queries, kind+" "+string(data))
		return json.RawMessage(`{"path":"/tmp/logs.json"}`), nil
	})
	conv, _ := f.svc.Store().Conversation(context.Background(), "conv-1")
	tool := f.svc.developerLogTool(conv)
	for range 2 {
		res, err := tool.Call(context.Background(), json.RawMessage(`{"request_id":"l1"}`))
		if err != nil || res.Value.(map[string]any)["path"] != "/tmp/logs.json" {
			t.Fatalf("export = %+v, %v", res, err)
		}
	}
	if len(queries) != 1 || queries[0] != `developer_logs.export {"conversationId":"conv-1","sinceMinutes":60}` {
		t.Fatalf("host requests = %v", queries)
	}
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"request_id":"l1","scope":"workspace"}`)); err == nil {
		t.Fatal("reused a request id for another export")
	}
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"request_id":"l2","scope":"workspace","conversation_id":"conv-1"}`)); err == nil {
		t.Fatal("workspace export with a conversation id")
	}
}
