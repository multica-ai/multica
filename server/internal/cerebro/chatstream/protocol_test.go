package chatstream

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewWriterSetsSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if sw == nil {
		t.Fatal("NewWriter returned nil writer")
	}
	h := rec.Header()
	if got := h.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := h.Get(UIMessageStreamHeader); got != UIMessageStreamVersion {
		t.Errorf("%s = %q, want %q", UIMessageStreamHeader, got, UIMessageStreamVersion)
	}
	if got := h.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := h.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
}

func TestWriteChunkFramesAsSSEData(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, err := NewWriter(rec)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := sw.WriteChunk(TextDeltaChunk{Type: ChunkTypeTextDelta, ID: "t0", Delta: "hello"}); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("frame missing data: prefix: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("frame missing blank-line terminator: %q", body)
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(body, "data: "), "\n\n")), &chunk); err != nil {
		t.Fatalf("frame payload is not JSON: %v", err)
	}
	if chunk["type"] != "text-delta" || chunk["id"] != "t0" || chunk["delta"] != "hello" {
		t.Errorf("unexpected chunk payload: %v", chunk)
	}
}

func TestWriteDoneTerminator(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, _ := NewWriter(rec)
	if err := sw.WriteDone(); err != nil {
		t.Fatalf("WriteDone: %v", err)
	}
	if got := rec.Body.String(); got != "data: [DONE]\n\n" {
		t.Errorf("WriteDone frame = %q, want data: [DONE]", got)
	}
}

func TestWritePingIsSSEComment(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, _ := NewWriter(rec)
	if err := sw.WritePing(); err != nil {
		t.Fatalf("WritePing: %v", err)
	}
	if got := rec.Body.String(); !strings.HasPrefix(got, ":") {
		t.Errorf("ping frame should be an SSE comment, got %q", got)
	}
}

// TestWriteAssistantMessage verifies the full UI-message-stream sequence for
// one completed assistant turn: start → text-start → text-delta → text-end →
// finish → [DONE], in that order, sharing a stable text part id.
func TestWriteAssistantMessage(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, _ := NewWriter(rec)

	meta := FinishMetadata{TaskID: "task-1", MessageID: "msg-1", ElapsedMs: 1200}
	if err := sw.WriteAssistantMessage("msg-1", "final answer", meta); err != nil {
		t.Fatalf("WriteAssistantMessage: %v", err)
	}

	frames := parseFrames(t, rec.Body.String())
	wantTypes := []string{"start", "text-start", "text-delta", "text-end", "finish"}
	if len(frames) != len(wantTypes)+1 { // +1 for [DONE]
		t.Fatalf("got %d frames, want %d: %v", len(frames), len(wantTypes)+1, frames)
	}
	for i, want := range wantTypes {
		var chunk map[string]any
		if err := json.Unmarshal([]byte(frames[i]), &chunk); err != nil {
			t.Fatalf("frame %d not JSON: %v", i, err)
		}
		if chunk["type"] != want {
			t.Errorf("frame %d type = %v, want %s", i, chunk["type"], want)
		}
	}
	var start map[string]any
	json.Unmarshal([]byte(frames[0]), &start)
	if start["messageId"] != "msg-1" {
		t.Errorf("start.messageId = %v, want msg-1", start["messageId"])
	}
	var delta map[string]any
	json.Unmarshal([]byte(frames[2]), &delta)
	if delta["delta"] != "final answer" {
		t.Errorf("text-delta.delta = %v, want final answer", delta["delta"])
	}
	var finish map[string]any
	json.Unmarshal([]byte(frames[4]), &finish)
	md, _ := finish["messageMetadata"].(map[string]any)
	if md == nil || md["taskId"] != "task-1" {
		t.Errorf("finish.messageMetadata = %v, want taskId task-1", finish["messageMetadata"])
	}
	if frames[len(frames)-1] != "[DONE]" {
		t.Errorf("last frame = %q, want [DONE]", frames[len(frames)-1])
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	sw, _ := NewWriter(rec)
	if err := sw.WriteError("access denied: workspace_id is required"); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	frames := parseFrames(t, rec.Body.String())
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want error + [DONE]: %v", len(frames), frames)
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(frames[0]), &chunk); err != nil {
		t.Fatalf("error frame not JSON: %v", err)
	}
	if chunk["type"] != "error" || chunk["errorText"] != "access denied: workspace_id is required" {
		t.Errorf("unexpected error chunk: %v", chunk)
	}
	if frames[1] != "[DONE]" {
		t.Errorf("stream not terminated with [DONE]: %v", frames)
	}
}

// parseFrames splits an SSE body into its data payloads, ignoring comments.
func parseFrames(t *testing.T, body string) []string {
	t.Helper()
	var frames []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			frames = append(frames, strings.TrimPrefix(line, "data: "))
		}
	}
	return frames
}
