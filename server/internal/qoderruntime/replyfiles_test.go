package qoderruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type qoderFile struct {
	data         string
	downloadable bool
	// scope is the owning session; empty means the run's own session.
	scope string
}

type replyUpload struct {
	name, taskID, data string
	afterComplete      bool
}

func chatTask(h *harness) {
	h.task.IssueID = ""
	h.task.ChatSessionID = "chat-1"
	h.task.ChatMessage = "Write the report"
}

// deliver appends an agent.artifact_delivered event and the file Qoder keeps.
func (h *harness) deliver(eventID, fileID, name, data string, size int, downloadable bool) {
	if h.qoderFiles == nil {
		h.qoderFiles = map[string]qoderFile{}
	}
	h.qoderFiles[fileID] = qoderFile{data: data, downloadable: downloadable}
	h.events = append(h.events, map[string]any{"id": eventID, "type": "agent.artifact_delivered", "file_id": fileID, "original_filename": name, "size": size, "content_type": "application/octet-stream"})
}

// stepUntilDone steps the bridge until the run is reported, as the ticker would.
func (h *harness) stepUntilDone(t *testing.T) {
	t.Helper()
	for i := 0; i < 10 && h.complete == nil && h.failure == nil; i++ {
		if err := h.b.step(context.Background()); err != nil {
			t.Logf("step %d: %v", i, err)
		}
	}
	if h.complete == nil {
		t.Fatalf("run not completed: %#v", h.failure)
	}
}

func TestChatReplyCarriesDeliveredFiles(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.prompt, "A file over 10 MB is not attached; tell the user so instead of saying it was sent.") {
		t.Fatalf("prompt does not describe reply files: %s", h.prompt)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a2", "file_data", "data.csv", "a,b\n1,2\n", 8, true)
	// Delivering the same file again does not attach it twice.
	h.deliver("evt_a3", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a4", "file_internal", "scratch.bin", "x", 1, false)
	h.deliver("evt_a5", "file_big", "big.zip", "", maxReplyFileBytes+1, true)
	// A file of another session, such as another chat's upload, is not passed on.
	h.deliver("evt_a6", "file_other", "other.txt", "secret", 6, true)
	f := h.qoderFiles["file_other"]
	f.scope = "sess_other"
	h.qoderFiles["file_other"] = f
	h.finish("end_turn")
	h.stepUntilDone(t)
	want := []replyUpload{{"report.pdf", "task-1", "%PDF-1.7", false}, {"data.csv", "task-1", "a,b\n1,2\n", false}}
	if fmt.Sprint(h.replyUploads) != fmt.Sprint(want) {
		t.Fatalf("reply uploads: %#v", h.replyUploads)
	}
	if !strings.Contains(fmt.Sprint(h.complete["output"]), "Tests passed") {
		t.Fatalf("result: %#v", h.complete)
	}
}

func TestReplyFileUploadIsRetriedThenRefusalIsSkipped(t *testing.T) {
	h := setup(t)
	chatTask(h)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.deliver("evt_a2", "file_data", "data.csv", "a,b", 3, true)
	// A 502 is retried on the next step; a 400 is a refusal and skips the file.
	h.replyUploadStatus = []int{502, 400}
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 1 || h.replyUploads[0].name != "data.csv" {
		t.Fatalf("reply uploads: %#v", h.replyUploads)
	}
}

func TestReplyFilesAfterRestartCompleteWithoutThem(t *testing.T) {
	h := setup(t)
	chatTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 0 {
		t.Fatalf("uploaded without the task credential: %#v", h.replyUploads)
	}
}

func TestIssueRunDoesNotAttachDeliveredFiles(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.prompt, "DeliverArtifacts") {
		t.Fatalf("issue prompt promises reply files: %s", h.prompt)
	}
	h.deliver("evt_a1", "file_report", "report.pdf", "%PDF-1.7", 8, true)
	h.finish("end_turn")
	h.stepUntilDone(t)
	if len(h.replyUploads) != 0 {
		t.Fatalf("issue run uploaded reply files: %#v", h.replyUploads)
	}
}
