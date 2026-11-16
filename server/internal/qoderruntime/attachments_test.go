package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type attachmentTransport func(*http.Request) (*http.Response, error)

func (f attachmentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatAttachmentDownloadURLLifetime(t *testing.T) {
	for _, delay := range []string{"none", "download", "upload"} {
		for _, image := range []bool{false, true} {
			t.Run(fmt.Sprintf("slow_%s/image_%t", delay, image), func(t *testing.T) {
				h := setup(t)
				h.task.IssueID = ""
				h.task.ChatSessionID = "chat-1"
				h.task.ChatMessage = "Read both files"
				second := [3]string{"second.txt", "text/plain", "second"}
				if image {
					second = [3]string{"second.png", "image/png", testPNG(t, 4, 4, false)}
				}
				chatAttach(map[string]string{}, h.task, [3]string{"first.txt", "text/plain", "first"}, second)
				var now int64
				var requests []string
				base := h.b.multica.client.Transport
				h.b.multica.client.Transport = attachmentTransport(func(r *http.Request) (*http.Response, error) {
					if !strings.HasPrefix(r.URL.Path, "/api/attachments/") {
						return base.RoundTrip(r)
					}
					if r.Header.Get("Authorization") != "Bearer mat_task" || r.Header.Get("X-Workspace-ID") != workspaceID {
						t.Error("attachment request missing task credential or workspace")
					}
					requests = append(requests, fmt.Sprintf("t=%d %s", now, r.URL.RequestURI()))
					status, body := http.StatusOK, ""
					if strings.HasSuffix(r.URL.Path, "/signed-download") {
						expires, err := strconv.ParseInt(r.URL.Query().Get("exp"), 10, 64)
						if err != nil {
							t.Errorf("invalid expiry: %v", err)
						}
						if now >= expires {
							status = http.StatusForbidden
						} else if strings.Contains(r.URL.Path, "/c0/") {
							// Model a transfer longer than the proxy URL's 60-second TTL
							// without sleeping or changing the bridge's real timeouts.
							if delay == "download" {
								now += 61
							} else if delay == "upload" {
								now += 40
							}
							body = "first"
						} else {
							body = second[2]
						}
					} else {
						id := strings.TrimPrefix(r.URL.Path, "/api/attachments/")
						file := [3]string{"first.txt", "text/plain", "first"}
						if id == "c1" {
							file = second
						}
						meta, err := json.Marshal(attachment{ID: id, Filename: file[0], ContentType: file[1], SizeBytes: int64(len(file[2])), DownloadURL: fmt.Sprintf("/api/attachments/%s/signed-download?exp=%d", id, now+60)})
						if err != nil {
							t.Error(err)
						}
						body = string(meta)
					}
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				qoderBase := h.b.qoder.client.Transport
				h.b.qoder.client.Transport = attachmentTransport(func(r *http.Request) (*http.Response, error) {
					resp, err := qoderBase.RoundTrip(r)
					if delay == "upload" && r.URL.Path == "/files" {
						now += 25
					}
					return resp, err
				})
				if err := h.b.step(context.Background()); err != nil {
					t.Fatal(err)
				}
				wantUploads, wantBlocks := 2, 1
				if image {
					wantUploads, wantBlocks = 1, 2
				}
				if h.failure != nil || h.sends != 1 || len(h.uploads) != wantUploads || len(h.sentContent) != wantBlocks {
					t.Fatalf("valid chat files should reach the agent: failure=%v sends=%d uploads=%d content=%v requests=%v", h.failure, h.sends, len(h.uploads), h.sentContent, requests)
				}
				if h.uploads["first.txt"] != "first" || !image && h.uploads["second.txt"] != "second" {
					t.Fatalf("unexpected uploaded content: %v", h.uploads)
				}
			})
		}
	}
}
