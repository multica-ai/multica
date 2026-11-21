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
	sharedMemoryCaptureQueueSize = 64
)

type sharedMemoryCaptureQueue struct {
	ch       chan sharedMemoryCaptureEvent
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
	debounce time.Duration
	flush    func([]sharedMemoryCaptureEvent)
}

func newSharedMemoryCaptureQueue() *sharedMemoryCaptureQueue {
	return newSharedMemoryCaptureQueueWith(sharedMemoryDebounce, flushSharedMemoryCapture)
}

func newSharedMemoryCaptureQueueWith(debounce time.Duration, flush func([]sharedMemoryCaptureEvent)) *sharedMemoryCaptureQueue {
	queue := &sharedMemoryCaptureQueue{
		ch:       make(chan sharedMemoryCaptureEvent, sharedMemoryCaptureQueueSize),
		done:     make(chan struct{}),
		debounce: debounce,
		flush:    flush,
	}
	go queue.run()
	return queue
}

func (q *sharedMemoryCaptureQueue) enqueue(event sharedMemoryCaptureEvent) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	select {
	case q.ch <- event:
		return true
	default:
		return false
	}
}

// shutdown closes the producer side and waits for the worker to flush every
// event accepted before shutdown. Closing the channel also interrupts the
// debounce wait so the final partial batch is not delayed or discarded.
func (q *sharedMemoryCaptureQueue) shutdown() {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.ch)
	}
	q.mu.Unlock()
	<-q.done
}

func (q *sharedMemoryCaptureQueue) run() {
	defer close(q.done)
	for first := range q.ch {
		batch := []sharedMemoryCaptureEvent{first}
		timer := time.NewTimer(q.debounce)
	collect:
		for len(batch) < sharedMemoryCaptureMax {
			select {
			case next, ok := <-q.ch:
				if !ok {
					stopSharedMemoryCaptureTimer(timer)
					q.flush(batch)
					return
				}
				batch = append(batch, next)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(q.debounce)
			case <-timer.C:
				break collect
			}
		}
		stopSharedMemoryCaptureTimer(timer)
		q.flush(batch)
	}
}

func stopSharedMemoryCaptureTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func enqueueSharedMemoryCapture(queue *sharedMemoryCaptureQueue, task Task, result TaskResult, logger *slog.Logger) {
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
	if queue == nil || !queue.enqueue(event) {
		logger.Warn("shared memory capture queue full; dropping event", "task_id", shortID(task.ID))
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
