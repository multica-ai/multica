package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReviewAttachmentUploadBeyondSixtySeconds(t *testing.T) {
	recorder := &uploadRecorder{t: t, attachmentID: "01a1cf5b-0000-7000-8000-000000000001"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/storage/") {
			_, _ = io.Copy(io.Discard, r.Body)
			timer := time.NewTimer(61 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		recorder.ServeHTTP(w, r)
	}))
	defer server.Close()
	setCLITestServerEnv(t, server.URL)
	t.Setenv("MULTICA_HTTP_TIMEOUT", "")
	t.Setenv("MULTICA_HTTP_STALL_TIMEOUT", "120s")
	path := writeFixture(t, chdirTemp(t), "large.txt", 1024)
	cmd := newAttachmentUploadTestCmd()
	mustSet(t, cmd, "task", "task-review")
	started := time.Now()
	if _, err := captureStdout(t, func() error { return runAttachmentUpload(cmd, []string{path}) }); err != nil {
		t.Fatalf("upload failed after %s despite transfer budget: %v", time.Since(started).Round(time.Millisecond), err)
	}
}
