package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestMediaPipelineDatabaseAttachmentsReachHTTPOnce(t *testing.T) {
	for _, markdown := range []bool{false, true} {
		name := "plain reply"
		if markdown {
			name = "native reply with artifact image"
		}
		t.Run(name, func(t *testing.T) { testMediaPipelineDatabaseAttachmentsReachHTTPOnce(t, markdown) })
	}
}

func testMediaPipelineDatabaseAttachmentsReachHTTPOnce(t *testing.T, markdown bool) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("explicit disposable DATABASE_URL required")
	}
	pool := channelScopeTestDB(t)
	fx := testutil.New(pool, "", "")
	suffix := uuid.NewString()
	fx.UserID = fx.User(t, "Media pipeline test", "media-pipeline-"+suffix+"@example.invalid")
	fx.WorkspaceID = fx.Workspace(t, "Media pipeline test", "media-pipeline-"+suffix)
	runtimeID := fx.Runtime(t, "Media pipeline test")
	agentID := fx.Agent(t, "Media pipeline test", runtimeID)
	sessionID := fx.ChatSession(t, agentID)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "chat_session_id": sessionID})
	fx.Exec(t, "UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1", taskID)
	fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "task_id": taskID, "role": "user", "content": "Create a chart and report", "channel_ingested": true})
	messageID := fx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "task_id": taskID, "role": "assistant", "content": "Here are the chart and report."})
	appID := "cli_media_" + suffix
	config, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret_encrypted": ""})
	installationID := fx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agentID, "channel_type": channelTypeFeishu,
		"installer_user_id": fx.UserID, "config": string(config)})
	bindingID := fx.Insert(t, "channel_chat_session_binding", testutil.Cols{"chat_session_id": sessionID, "installation_id": installationID, "channel_type": channelTypeFeishu, "channel_chat_id": "oc_pipeline", "chat_type": "p2p"})
	fx.InsertNoID(t, "channel_task_delivery", testutil.Cols{"task_id": taskID, "binding_id": bindingID, "installation_id": installationID, "channel_type": channelTypeFeishu, "channel_chat_id": "oc_pipeline", "chat_type": "p2p", "route_revision": 1}, "task_id=$1", taskID)
	fx.Cleanup(t, "DELETE FROM channel_reply_delivery WHERE turn_id=$1", taskID)
	data := map[string][]byte{"chart.png": pngBytes, "report.pdf": []byte("%PDF-1.4 exact report bytes")}
	var chartID string
	for name, content := range data {
		kind := "application/pdf"
		if name == "chart.png" {
			kind = "image/png"
		}
		id := fx.Insert(t, "attachment", testutil.Cols{"workspace_id": fx.WorkspaceID, "task_id": taskID, "chat_session_id": sessionID, "chat_message_id": messageID,
			"uploader_type": "agent", "uploader_id": agentID, "filename": name, "url": fakeObjectPrefix + name, "content_type": kind, "size_bytes": len(content)})
		if name == "chart.png" {
			chartID = id
		}
	}
	fake := newLarkFake(t)
	fake.stubToken("token", 7200)
	for _, image := range []bool{true, false} {
		path, field, name, key := larkUploadPathFile, "file", "report.pdf", "file_key"
		if image {
			path, field, name, key = larkUploadPathImage, "image", "chart.png", "image_key"
		}
		fake.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			f, _, err := r.FormFile(field)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			got, _ := io.ReadAll(f)
			if !bytes.Equal(got, data[name]) {
				t.Error("stored attachment bytes changed")
			}
			resourceKey := name
			if image {
				resourceKey = "img_chart"
			}
			writeJSON(w, map[string]any{"code": 0, "data": map[string]string{key: resourceKey}})
		})
	}
	kinds := map[string]int{}
	var kindsMu sync.Mutex
	patches := 0
	fake.stubPatch(map[string]any{"code": 0}, func(_ *http.Request, id string, body map[string]string) {
		kindsMu.Lock()
		defer kindsMu.Unlock()
		patches++
		if id != "om_accepted" {
			t.Errorf("updated a different answer: %q", id)
		}
		var card struct {
			Config struct {
				UpdateMulti bool `json:"update_multi"`
			}
			Body struct{ Elements []struct{ Content string } }
		}
		if err := json.Unmarshal([]byte(body["content"]), &card); err != nil {
			t.Error(err)
			return
		}
		want := "Here are the chart and report.\n\n![chart.png](img_chart)"
		if markdown {
			want = "Here are the chart and report.\n![chart](img_chart)\nAfter the chart."
		}
		if !card.Config.UpdateMulti || len(card.Body.Elements) != 1 || card.Body.Elements[0].Content != want {
			t.Errorf("mixed card lost text/image ordering: %+v", card)
		}
	})
	fake.stubSend(map[string]any{"code": 0, "data": map[string]string{"message_id": "om_accepted"}}, func(_ *http.Request, body map[string]string) {
		kindsMu.Lock()
		defer kindsMu.Unlock()
		kinds[body["msg_type"]]++
		if body["msg_type"] == "interactive" {
			var card struct {
				Body struct{ Elements []struct{ Content string } }
			}
			if err := json.Unmarshal([]byte(body["content"]), &card); err != nil {
				t.Fatal(err)
			}
			want := "Here are the chart and report."
			if markdown {
				want += "\n[chart](https://multica.example/api/attachments/" + chartID + "/download)\nAfter the chart."
			}
			if len(card.Body.Elements) != 1 || card.Body.Elements[0].Content != want {
				t.Errorf("Feishu would reject an artifact URL used as an image key: %+v", card)
			}
		}
		if (body["msg_type"] == "image" || body["msg_type"] == "file") && body["uuid"] == "" {
			t.Error("attachment send missing UUID")
		}
	})
	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: fake.URL(), ResourceHTTPClient: fake.srv.Client(), Logger: newDiscardLogger()})
	queries := db.New(pool)
	p := NewPatcher(NewChannelStore(queries), fakeCredentials{secret: "test-secret"}, client, PatcherConfig{Logger: newDiscardLogger()})
	if !p.SetAttachments(fakeObjectStore{objects: data}) {
		t.Fatal("file delivery not enabled")
	}
	p.spawn = func(f func()) { f() }
	event := events.Event{Type: protocol.EventChatDone, WorkspaceID: fx.WorkspaceID, TaskID: taskID, ChatSessionID: sessionID,
		Payload: protocol.ChatDonePayload{TaskID: taskID, ChatSessionID: sessionID, MessageID: messageID, Content: "Here are the chart and report."}}
	if markdown {
		payload := event.Payload.(protocol.ChatDonePayload)
		payload.Content += "\n![chart](https://multica.example/api/attachments/" + chartID + "/download)\nAfter the chart."
		event.Payload = payload
	}
	if err := p.processEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := p.processEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	kindsMu.Lock()
	defer kindsMu.Unlock()
	if kinds["interactive"] != 1 || kinds["text"] != 0 || kinds["image"] != 0 || kinds["file"] != 1 || patches != 1 {
		t.Fatalf("completion replay changed mixed reply: messages=%v patches=%d", kinds, patches)
	}
	row, err := queries.GetChannelReplyDelivery(context.Background(), util.MustParseUUID(taskID))
	if err != nil || row.Phase != "settled" {
		t.Fatalf("delivery not settled: %v %v", row, err)
	}
}
