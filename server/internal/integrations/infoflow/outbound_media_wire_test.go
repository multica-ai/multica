package infoflow

// wire_test.go — the payloads, checked against the real SDK.
//
// outbound_media.go chooses *which* field of an InfoFlow send it fills: an
// image's content is the BOS object key, a Markdown message's content is the
// text. Those choices are the whole difference between "the user sees the
// chart" and "the user sees the word image", and a fake sender cannot catch a
// mistake in them. So this test drives the production SDK against a local
// server and reads the JSON that would have crossed the wire.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dolfly/infoflow"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// capturedRequest is one send the fake platform received.
type capturedRequest struct {
	path string
	body map[string]any
}

// fakeInfoFlow answers the two routes a send touches: the token endpoint and
// the endpoint under test. It returns the path and decoded body so a test can
// assert on the wire format.
func fakeInfoFlow(t *testing.T, reply string) (*infoflow.Client, *[]capturedRequest) {
	t.Helper()
	var got []capturedRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/app_access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"ok","data":{"app_access_token":"test-token","expire":7200}}`))
	})
	record := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		got = append(got, capturedRequest{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}
	mux.HandleFunc("/app/message/send", record)
	mux.HandleFunc("/robot/msg/groupmsgsend", record)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := infoflow.NewClient(infoflow.Config{
		AppKey:     "test-app-key",
		AppSecret:  "test-app-secret",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("infoflow.NewClient: %v", err)
	}
	return client, &got
}

const (
	userSendReply  = `{"code":"ok","data":{"errcode":0,"msgkey":123}}`
	groupSendReply = `{"code":"ok","data":{"errcode":0,"data":{"messageid":7,"msgseqid":1,"ctime":2}}}`
)

// TestWire_UserImageCarriesTheObjectKey pins the field an image is delivered in.
// The content is the key the upload returned, not a URL and not the filename.
func TestWire_UserImageCarriesTheObjectKey(t *testing.T) {
	client, got := fakeInfoFlow(t, userSendReply)

	if _, err := sendImage(context.Background(), client, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, "sdk-uploads/chart.png"); err != nil {
		t.Fatalf("sendImage: %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("requests = %d, want 1", len(*got))
	}
	req := (*got)[0]
	if req.path != "/app/message/send" {
		t.Fatalf("path = %q, want /app/message/send", req.path)
	}
	if req.body["msgtype"] != "image" {
		t.Fatalf("msgtype = %v, want image", req.body["msgtype"])
	}
	if req.body["touser"] != "u1" {
		t.Fatalf("touser = %v, want u1", req.body["touser"])
	}
	image, ok := req.body["image"].(map[string]any)
	if !ok {
		t.Fatalf("image = %#v, want an object", req.body["image"])
	}
	if image["content"] != "sdk-uploads/chart.png" {
		t.Fatalf("image.content = %v, want the uploaded object key", image["content"])
	}
}

// TestWire_UserMarkdownCarriesTheText pins that a non-image file travels as a
// Markdown message whose content is the link.
func TestWire_UserMarkdownCarriesTheText(t *testing.T) {
	client, got := fakeInfoFlow(t, userSendReply)

	link := fileLinkMarkdown("report.pdf", "https://bos.example/x")
	if _, err := sendMarkdown(context.Background(), client, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, link); err != nil {
		t.Fatalf("sendMarkdown: %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("requests = %d, want 1", len(*got))
	}
	req := (*got)[0]
	if req.body["msgtype"] != "md" {
		t.Fatalf("msgtype = %v, want md", req.body["msgtype"])
	}
	md, ok := req.body["md"].(map[string]any)
	if !ok {
		t.Fatalf("md = %#v, want an object", req.body["md"])
	}
	if md["content"] != link {
		t.Fatalf("md.content = %v, want %q", md["content"], link)
	}
}

// TestWire_GroupUsesTheEnvelope pins the group shape: the target is the group
// id in the header, and the body element type is the uppercase message type.
func TestWire_GroupUsesTheEnvelope(t *testing.T) {
	client, got := fakeInfoFlow(t, groupSendReply)

	target := sendTarget{ChatType: channel.ChatTypeGroup, ChatID: "987654"}
	if _, err := sendImage(context.Background(), client, target, "sdk-uploads/chart.png"); err != nil {
		t.Fatalf("sendImage(group): %v", err)
	}
	if _, err := sendMarkdown(context.Background(), client, target, "[report.pdf](https://bos.example/x)"); err != nil {
		t.Fatalf("sendMarkdown(group): %v", err)
	}

	if len(*got) != 2 {
		t.Fatalf("requests = %d, want 2", len(*got))
	}
	for i, wantType := range []string{"IMAGE", "MD"} {
		req := (*got)[i]
		if req.path != "/robot/msg/groupmsgsend" {
			t.Fatalf("request %d path = %q, want /robot/msg/groupmsgsend", i, req.path)
		}
		message, ok := req.body["message"].(map[string]any)
		if !ok {
			t.Fatalf("request %d: message = %#v, want an object", i, req.body["message"])
		}
		header, ok := message["header"].(map[string]any)
		if !ok {
			t.Fatalf("request %d: header = %#v, want an object", i, message["header"])
		}
		if header["msgtype"] != wantType {
			t.Fatalf("request %d: header.msgtype = %v, want %s", i, header["msgtype"], wantType)
		}
		// The group id is a JSON number in the header.
		if header["toid"] != float64(987654) {
			t.Fatalf("request %d: header.toid = %v, want 987654", i, header["toid"])
		}
		body, ok := message["body"].([]any)
		if !ok || len(body) != 1 {
			t.Fatalf("request %d: body = %#v, want one element", i, message["body"])
		}
		elem, ok := body[0].(map[string]any)
		if !ok {
			t.Fatalf("request %d: body[0] = %#v, want an object", i, body[0])
		}
		if elem["type"] != wantType {
			t.Fatalf("request %d: body[0].type = %v, want %s", i, elem["type"], wantType)
		}
	}
}

// TestWire_RefusesAnEmptyKeyLocally pins that a blank image key is a local
// refusal: the SDK would reject it anyway, but a request that never leaves is
// cheaper and does not spend a token on the way.
func TestWire_RefusesAnEmptyKeyLocally(t *testing.T) {
	client, got := fakeInfoFlow(t, userSendReply)

	if _, err := sendImage(context.Background(), client, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, "  "); err == nil {
		t.Fatal("sendImage(blank key) = nil, want an error")
	}
	if _, err := sendMarkdown(context.Background(), client, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, " "); err == nil {
		t.Fatal("sendMarkdown(blank) = nil, want an error")
	}
	if len(*got) != 0 {
		t.Fatalf("requests = %d, want none", len(*got))
	}
}
