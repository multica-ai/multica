package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMediaHTTPMultipartAndDedicatedTransport(t *testing.T) {
	for _, image := range []bool{true, false} {
		t.Run(map[bool]string{true: "image", false: "file"}[image], func(t *testing.T) {
			f := newLarkFake(t)
			f.stubToken("token", 7200)
			endpoint, field, key := larkUploadPathFile, "file", "file_key"
			if image {
				endpoint, field, key = larkUploadPathImage, "image", "image_key"
			}
			data := []byte("file bytes\x00with binary")
			f.mux.HandleFunc(endpoint, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.ContentLength <= int64(len(data)) || r.Header.Get("Authorization") != "Bearer token" {
					t.Errorf("invalid request framing/authentication: %s size=%d", r.Method, r.ContentLength)
				}
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, header, err := r.FormFile(field)
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()
				got, _ := io.ReadAll(file)
				if !bytes.Equal(got, data) || header.Filename != "chart.png" {
					t.Errorf("multipart file mismatch: %q %q", got, header.Filename)
				}
				if image && r.FormValue("image_type") != "message" {
					t.Error("missing image_type")
				}
				if !image && (r.FormValue("file_type") != "stream" || r.FormValue("file_name") != "chart.png") {
					t.Error("missing file metadata")
				}
				writeJSON(w, map[string]any{"code": 0, "data": map[string]string{key: "resource-key"}})
			})
			var ordinaryUploads atomic.Int32
			ordinary := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == endpoint {
					ordinaryUploads.Add(1)
					return nil, errors.New("upload used JSON transport")
				}
				return http.DefaultTransport.RoundTrip(r)
			})}
			c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), HTTPClient: ordinary, ResourceHTTPClient: f.srv.Client()}).(mediaAPIClient)
			var got string
			var err error
			if image {
				got, err = c.UploadImage(context.Background(), UploadImageParams{InstallationID: testCreds(), Filename: "chart.png", Data: data})
			} else {
				got, err = c.UploadFile(context.Background(), UploadFileParams{InstallationID: testCreds(), Filename: "chart.png", FileType: "stream", Data: data})
			}
			if err != nil || got != "resource-key" || ordinaryUploads.Load() != 0 {
				t.Fatalf("upload=%q err=%v ordinary=%d", got, err, ordinaryUploads.Load())
			}
		})
	}
}

func TestMediaHTTPAttachmentUploadAndMessageTypes(t *testing.T) {
	cases := []struct {
		filename    string
		contentType string
		data        []byte
		uploadType  string
		messageType string
	}{
		{"clip.mp4", "video/mp4", []byte("\x00\x00\x00\x18ftypmp42 video bytes"), "mp4", "media"},
		{"voice.opus", "audio/ogg", []byte("OggS\x00OpusHead audio bytes"), "opus", "audio"},
		{"archive.zip", "application/zip", []byte("PK\x03\x04 archive bytes"), "stream", "file"},
		{"report.pdf", "application/pdf", []byte("%PDF-1.4 document bytes"), "pdf", "file"},
	}
	for _, tc := range cases {
		for _, reply := range []bool{false, true} {
			route := "send"
			if reply {
				route = "reply"
			}
			t.Run(tc.filename+"/"+route, func(t *testing.T) {
				f := newLarkFake(t)
				f.stubToken("token", 7200)
				p, q, _ := newMediaTestPatcher(t)
				client := NewHTTPAPIClient(HTTPClientConfig{
					BaseURL: f.URL(), HTTPClient: f.srv.Client(),
					ResourceHTTPClient: f.srv.Client(), Logger: newDiscardLogger(),
				})
				p.client = client
				p.media = client.(mediaAPIClient)
				if !p.SetAttachments(fakeObjectStore{objects: map[string][]byte{tc.filename: tc.data}}) {
					t.Fatal("attachment delivery unavailable")
				}
				target := ReplyTarget{}
				path := "/open-apis/im/v1/messages"
				if reply {
					target = ReplyTarget{MessageID: "om_parent", InThread: true}
					path += "/om_parent/reply"
				}
				resourceKey := "file_uploaded_" + tc.filename
				var uploadType atomic.Value
				var uploads, sends atomic.Int32
				f.mux.HandleFunc(larkUploadPathFile, func(w http.ResponseWriter, r *http.Request) {
					uploads.Add(1)
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" {
						t.Errorf("invalid upload method or authorization: %s", r.Method)
					}
					if err := r.ParseMultipartForm(1024); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer r.MultipartForm.RemoveAll()
					file, header, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer file.Close()
					data, err := io.ReadAll(file)
					if err != nil || !bytes.Equal(data, tc.data) || header.Filename != tc.filename {
						t.Errorf("uploaded attachment changed: filename=%q data=%q err=%v", header.Filename, data, err)
					}
					gotType := r.FormValue("file_type")
					uploadType.Store(gotType)
					if gotType != tc.uploadType || r.FormValue("file_name") != tc.filename {
						t.Errorf("upload metadata: type=%q name=%q", gotType, r.FormValue("file_name"))
					}
					writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"file_key": resourceKey}})
				})
				f.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					var body struct {
						ReceiveID     string `json:"receive_id"`
						MessageType   string `json:"msg_type"`
						Content       string `json:"content"`
						ReplyInThread *bool  `json:"reply_in_thread"`
						UUID          string `json:"uuid"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer token" || r.URL.Path != path {
						t.Errorf("invalid message request: %s %s", r.Method, r.URL.Path)
					}
					gotUploadType, _ := uploadType.Load().(string)
					if gotUploadType != tc.uploadType || body.MessageType != tc.messageType {
						t.Errorf("uploaded %q must be sent as %q, got upload=%q message=%q", tc.filename, tc.messageType, gotUploadType, body.MessageType)
						w.WriteHeader(http.StatusBadRequest)
						writeJSON(w, map[string]any{"code": 230055, "msg": "The type of file upload does not match the type of message being sent."})
						return
					}
					var content map[string]string
					if err := json.Unmarshal([]byte(body.Content), &content); err != nil || len(content) != 1 || content["file_key"] != resourceKey {
						t.Errorf("message did not use the uploaded file key: content=%v err=%v", content, err)
					}
					if body.UUID != "8b6fcdea-23ae-5401-9eeb-2295f8ac53a5" {
						t.Errorf("attachment UUID is not stable: %q", body.UUID)
					}
					if reply {
						if body.ReceiveID != "" || body.ReplyInThread == nil || !*body.ReplyInThread || r.URL.RawQuery != "" {
							t.Errorf("reply destination changed: receive_id=%q in_thread=%v query=%q", body.ReceiveID, body.ReplyInThread, r.URL.RawQuery)
						}
					} else if body.ReceiveID != q.binding.ChannelChatID || body.ReplyInThread != nil || r.URL.Query().Get("receive_id_type") != "chat_id" {
						t.Errorf("chat destination changed: receive_id=%q in_thread=%v query=%q", body.ReceiveID, body.ReplyInThread, r.URL.RawQuery)
					}
					writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"message_id": "om_attachment"}})
				})
				row := fileRow(t, "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb", tc.filename, tc.contentType)
				messageID := uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")
				if err := p.deliverOneAttachment(context.Background(), testCreds(), q.binding, &target, q.installation.WorkspaceID, messageID, row); err != nil {
					t.Fatal(err)
				}
				if uploads.Load() != 1 || sends.Load() != 1 {
					t.Fatalf("expected one upload and one message: uploads=%d sends=%d", uploads.Load(), sends.Load())
				}
			})
		}
	}
}

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMediaHTTPRefreshReplaysMultipart(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newLarkFake(t)
			f.mux.HandleFunc("/open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
				n := f.tokenN.Add(1)
				token := "old"
				if n > 1 {
					token = "fresh"
				}
				writeJSON(w, map[string]any{"code": 0, "tenant_access_token": token, "expire": 7200})
			})
			var calls atomic.Int32
			f.mux.HandleFunc(larkUploadPathImage, func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseMultipartForm(1024); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, _, err := r.FormFile("image")
				if err != nil {
					t.Error(err)
					return
				}
				defer file.Close()
				b, _ := io.ReadAll(file)
				if string(b) != "unchanged" {
					t.Errorf("replayed body=%q", b)
				}
				n := calls.Add(1)
				if n == 1 {
					w.WriteHeader(status)
					writeJSON(w, map[string]any{"code": codeTenantTokenInvalid, "msg": "expired"})
					return
				}
				if r.Header.Get("Authorization") != "Bearer fresh" {
					t.Error("token not refreshed")
				}
				writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"image_key": "fresh-image"}})
			})
			c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), ResourceHTTPClient: f.srv.Client()}).(mediaAPIClient)
			got, err := c.UploadImage(context.Background(), UploadImageParams{InstallationID: testCreds(), Data: []byte("unchanged")})
			if err != nil || got != "fresh-image" || calls.Load() != 2 || f.tokenN.Load() != 2 {
				t.Fatalf("got=%q err=%v calls=%d tokens=%d", got, err, calls.Load(), f.tokenN.Load())
			}
		})
	}
}

func TestMediaHTTPUploadLimits(t *testing.T) {
	f := newLarkFake(t)
	f.stubToken("token", 7200)
	var calls atomic.Int32
	f.mux.HandleFunc(larkUploadPathImage, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"image_key": "key"}})
	})
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), ResourceHTTPClient: f.srv.Client()}).(mediaAPIClient)
	for _, size := range []int{0, larkImageUploadMaxBytes + 1} {
		if _, err := c.UploadImage(context.Background(), UploadImageParams{InstallationID: testCreds(), Data: make([]byte, size)}); err == nil {
			t.Errorf("accepted image size %d", size)
		}
	}
	if _, err := c.UploadFile(context.Background(), UploadFileParams{InstallationID: testCreds(), Data: make([]byte, larkFileUploadMaxBytes+1)}); err == nil {
		t.Error("accepted oversized file")
	}
	if calls.Load() != 0 || f.tokenN.Load() != 0 {
		t.Error("invalid bytes reached transport")
	}
	if _, err := c.UploadImage(context.Background(), UploadImageParams{InstallationID: testCreds(), Data: make([]byte, larkImageUploadMaxBytes)}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("boundary calls=%d", calls.Load())
	}
}

func TestMediaHTTPUploadFailuresAreBounded(t *testing.T) {
	for _, body := range []string{`{"code":0,"data":{}}`, `{"code":234001,"msg":"format rejected"}`, "not-json", strings.Repeat("x", 65<<10)} {
		t.Run(body[:min(len(body), 35)], func(t *testing.T) {
			f := newLarkFake(t)
			f.stubToken("token", 7200)
			var calls atomic.Int32
			f.mux.HandleFunc(larkUploadPathFile, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, body) })
			c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), ResourceHTTPClient: f.srv.Client()}).(mediaAPIClient)
			_, err := c.UploadFile(context.Background(), UploadFileParams{InstallationID: testCreds(), FileType: "stream", Data: []byte("x")})
			if err == nil || calls.Load() != 1 {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
		})
	}
	f := newLarkFake(t)
	f.stubToken("token", 7200)
	f.mux.HandleFunc(larkUploadPathImage, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), ResourceHTTPClient: f.srv.Client(), ResourceDownloadTimeout: 20 * time.Millisecond}).(mediaAPIClient)
	_, err := c.UploadImage(context.Background(), UploadImageParams{InstallationID: testCreds(), Data: []byte("x")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing upload deadline: %v", err)
	}
}

func TestMediaHTTPMessageRoutingAndUUID(t *testing.T) {
	for _, image := range []bool{true, false} {
		for _, reply := range []bool{true, false} {
			t.Run(map[bool]string{true: "image", false: "file"}[image]+map[bool]string{true: "-reply", false: "-send"}[reply], func(t *testing.T) {
				f := newLarkFake(t)
				f.stubToken("token", 7200)
				verify := func(r *http.Request, body map[string]any) {
					kind, key := "file", "file_key"
					if image {
						kind, key = "image", "image_key"
					}
					if body["msg_type"] != kind || body["uuid"] != "stable-id" {
						t.Errorf("envelope=%v", body)
					}
					var content map[string]string
					if err := json.Unmarshal([]byte(body["content"].(string)), &content); err != nil || content[key] != "resource" {
						t.Errorf("content=%v err=%v", content, err)
					}
					if reply {
						if body["reply_in_thread"] != true {
							t.Error("thread lost")
						}
					} else if body["receive_id"] != "oc_chat" || r.URL.Query().Get("receive_id_type") != "chat_id" {
						t.Error("chat target lost")
					}
				}
				response := map[string]any{"code": 0, "data": map[string]string{"message_id": "om_result"}}
				if reply {
					f.stubReply(response, func(r *http.Request, id string, b map[string]any) {
						if id != "om_parent" {
							t.Error("wrong parent")
						}
						verify(r, b)
					})
				} else {
					f.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
						var b map[string]any
						_ = json.NewDecoder(r.Body).Decode(&b)
						verify(r, b)
						writeJSON(w, response)
					})
				}
				c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL()}).(mediaAPIClient)
				target := ReplyTarget{}
				if reply {
					target = ReplyTarget{MessageID: "om_parent", InThread: true}
				}
				var got string
				var err error
				if image {
					got, err = c.SendImageMessage(context.Background(), SendImageParams{InstallationID: testCreds(), ChatID: "oc_chat", ImageKey: "resource", ReplyTarget: target, UUID: "stable-id"})
				} else {
					got, err = c.SendFileMessage(context.Background(), SendFileParams{InstallationID: testCreds(), ChatID: "oc_chat", FileKey: "resource", ReplyTarget: target, UUID: "stable-id"})
				}
				if err != nil || got != "om_result" {
					t.Fatalf("got=%q err=%v", got, err)
				}
			})
		}
	}
}

func TestMediaHTTPMessageRefreshesEnvelopeTokenRejection(t *testing.T) {
	f := newLarkFake(t)
	f.stubToken("token", 7200)
	var calls atomic.Int32
	f.mux.HandleFunc("/open-apis/im/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		if body["uuid"] != "same-send" {
			t.Error("authentication retry changed idempotency key")
		}
		if calls.Add(1) == 1 {
			writeJSON(w, map[string]any{"code": codeTenantTokenInvalid, "msg": "expired"})
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]string{"message_id": "accepted"}})
	})
	c := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL(), Logger: newDiscardLogger()}).(mediaAPIClient)
	id, err := c.SendFileMessage(context.Background(), SendFileParams{InstallationID: testCreds(), ChatID: "oc_chat", FileKey: "file", UUID: "same-send"})
	if err != nil || id != "accepted" || calls.Load() != 2 || f.tokenN.Load() != 2 {
		t.Fatalf("id=%q err=%v calls=%d tokens=%d", id, err, calls.Load(), f.tokenN.Load())
	}
}
