package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	sharedMemoryCaptureScript = "/Users/macmini16g256ssd/nexai/scripts/memory_auto_capture.py"
	sharedMemoryDebounce      = 30 * time.Second
	sharedMemoryCaptureMax    = 8
)

type sharedMemoryCaptureEvent struct {
	TaskID        string `json:"task_id"`
	CommentID     string `json:"comment_id,omitempty"`
	Timestamp     string `json:"timestamp"`
	ActorIsOwner  bool   `json:"actor_is_workspace_owner"`
	UserMessage   string `json:"user_message,omitempty"`
	ResultComment string `json:"result_comment,omitempty"`
}

var (
	sharedMemoryCaptureOnce sync.Once
	sharedMemoryCaptureCh   = make(chan sharedMemoryCaptureEvent, 64)
)

func enqueueSharedMemoryCapture(task Task, result TaskResult, logger *slog.Logger) {
	if !sharedMemoryEnabled() || task.ID == "" {
		return
	}
	event := sharedMemoryCaptureEvent{
		TaskID: task.ID, CommentID: task.TriggerCommentID,
		Timestamp:    time.Now().Format(time.RFC3339),
		ActorIsOwner: task.InitiatorType == "member" && task.InitiatorIsWorkspaceOwner,
	}
	if event.ActorIsOwner {
		event.UserMessage = strings.TrimSpace(task.ChatMessage)
		if event.UserMessage == "" {
			event.UserMessage = strings.TrimSpace(task.TriggerCommentContent)
		}
	}
	event.ResultComment = strings.TrimSpace(result.Comment)
	if len(event.UserMessage) > 5000 {
		event.UserMessage = event.UserMessage[:5000]
	}
	if len(event.ResultComment) > 5000 {
		event.ResultComment = event.ResultComment[:5000]
	}
	if event.UserMessage == "" && event.ResultComment == "" {
		return
	}
	sharedMemoryCaptureOnce.Do(func() { go runSharedMemoryCaptureQueue() })
	select {
	case sharedMemoryCaptureCh <- event:
	default:
		logger.Warn("shared memory capture queue full; dropping event", "task_id", shortID(task.ID))
	}
}

func runSharedMemoryCaptureQueue() {
	for first := range sharedMemoryCaptureCh {
		batch := []sharedMemoryCaptureEvent{first}
		timer := time.NewTimer(sharedMemoryDebounce)
	collect:
		for len(batch) < sharedMemoryCaptureMax {
			select {
			case next := <-sharedMemoryCaptureCh:
				batch = append(batch, next)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(sharedMemoryDebounce)
			case <-timer.C:
				break collect
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		flushSharedMemoryCapture(batch)
	}
}

func flushSharedMemoryCapture(batch []sharedMemoryCaptureEvent) {
	if len(batch) == 0 || !sharedMemoryEnabled() {
		return
	}
	script := strings.TrimSpace(os.Getenv("MULTICA_MEMORY_CAPTURE_SCRIPT"))
	if script == "" {
		script = sharedMemoryCaptureScript
	}
	python := sharedMemoryPython()
	input, err := json.Marshal(batch)
	if err != nil {
		slog.Warn("shared memory capture: encode failed", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.Output()
	if err != nil {
		slog.Warn("shared memory capture: worker failed", "error", err)
		return
	}
	slog.Info("shared memory capture: batch processed", "events", len(batch), "result", strings.TrimSpace(string(output)))
}
