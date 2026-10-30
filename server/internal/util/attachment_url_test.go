package util

import "testing"

func TestAttachmentIDFromDownloadURL(t *testing.T) {
	const id = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	tests := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{name: "relative", url: AttachmentDownloadPath(id), want: id, ok: true},
		{name: "absolute", url: "https://api.example.test" + AttachmentDownloadPath(id), want: id, ok: true},
		{name: "uppercase HTTP scheme", url: "HTTPS://api.example.test" + AttachmentDownloadPath(id), want: id, ok: true},
		{name: "query and fragment", url: AttachmentDownloadPath(id) + "?preview=1#page=2", want: id, ok: true},
		{name: "uppercase UUID", url: "/api/attachments/AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE/download", want: id, ok: true},
		{name: "wrong route", url: "/api/attachments/" + id + "/signed-download", ok: false},
		{name: "nested path", url: "/proxy/api/attachments/" + id + "/download", ok: false},
		{name: "path traversal", url: AttachmentDownloadPath(id) + "/../other", ok: false},
		{name: "non UUID", url: AttachmentDownloadPath("attachment-1"), ok: false},
		{name: "unsupported scheme", url: "attachment:" + AttachmentDownloadPath(id), ok: false},
		{name: "protocol relative", url: "//api.example.test" + AttachmentDownloadPath(id), ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AttachmentIDFromDownloadURL(tt.url)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("AttachmentIDFromDownloadURL(%q) = %q, %v; want %q, %v", tt.url, got, ok, tt.want, tt.ok)
			}
		})
	}
}
