package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	defaultTaskSupplementPollInterval  = 5 * time.Second
	defaultTaskSupplementReadyInterval = 50 * time.Millisecond
	taskSupplementAckTimeout           = 5 * time.Second
)

// taskSupplementSignals routes best-effort WebSocket hints to one exact
// in-flight task. A one-slot channel coalesces duplicate hints. Creating the
// slot before provider launch preserves a hint sent after the server marks the
// task running but before the provider confirms an active turn.
type taskSupplementSignals struct {
	mu     sync.Mutex
	byTask map[string]chan struct{}
}

func (s *taskSupplementSignals) subscribe(taskID string) (<-chan struct{}, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byTask == nil {
		s.byTask = make(map[string]chan struct{})
	}
	ch := s.byTask[taskID]
	if ch == nil {
		ch = make(chan struct{}, 1)
		s.byTask[taskID] = ch
	}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.byTask[taskID] == ch {
			delete(s.byTask, taskID)
		}
	}
}

func (s *taskSupplementSignals) notify(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A nil channel ignores stale or unnegotiated hints without allocating state.
	select {
	case s.byTask[taskID] <- struct{}{}:
	default:
	}
}

func (d *Daemon) effectiveTaskSupplementPollInterval() time.Duration {
	if d.taskSupplementPollInterval > 0 {
		return d.taskSupplementPollInterval
	}
	return defaultTaskSupplementPollInterval
}

func (d *Daemon) effectiveTaskSupplementReadyInterval() time.Duration {
	if d.taskSupplementReadyInterval > 0 {
		return d.taskSupplementReadyInterval
	}
	return defaultTaskSupplementReadyInterval
}

func waitTaskSupplement(ctx context.Context, wakeup <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wakeup:
		return true
	case <-timer.C:
		return true
	}
}

func formatTaskSupplementInstruction(authorName, content string) string {
	authorName = strings.Join(strings.Fields(authorName), " ")
	if authorName == "" {
		authorName = "a user"
	}
	return fmt.Sprintf(`[ADDITIONAL GUIDANCE] Human %s added guidance while you were working.

Treat this as additional guidance for the same active task, not as a replacement:
- Preserve and complete the original objective.
- Incorporate this guidance into the work and the turn's single final response.
- Do not send a separate acknowledgement.
- Replace or cancel the original objective only if the human explicitly asks for replacement or cancellation.

Human message:
%s`, strconv.Quote(authorName), content)
}

func taskSupplementEndpointUnsupported(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusNotFound || reqErr.StatusCode == http.StatusPreconditionFailed)
}

func taskSupplementFailureReason(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return protocol.TaskSupplementFailureTimeout
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return protocol.TaskSupplementFailureTurnEnded
	}
	if strings.Contains(strings.ToLower(err.Error()), "turn") && strings.Contains(strings.ToLower(err.Error()), "not started") {
		return protocol.TaskSupplementFailureTurnNotStarted
	}
	return protocol.TaskSupplementFailureProviderRejected
}

// runTaskSupplementLoop serially claims and acknowledges durable additions for
// one negotiated run. It performs no HTTP request until the provider confirms a live
// turn, wakes immediately on a content-free WebSocket hint, and otherwise uses
// the same five-second cadence as task cancellation polling.
func (d *Daemon) runTaskSupplementLoop(ctx context.Context, session *agent.Session, taskID string, wakeup <-chan struct{}, taskLog *slog.Logger) {
	if session == nil || session.Supplement == nil || session.SupplementReady == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if !session.SupplementReady() {
			if !waitTaskSupplement(ctx, wakeup, d.effectiveTaskSupplementReadyInterval()) {
				return
			}
			continue
		}

		claimCtx, cancelClaim := context.WithTimeout(ctx, 3*time.Second)
		supplement, claimErr := d.client.ClaimTaskSupplement(claimCtx, taskID)
		cancelClaim()
		if claimErr != nil || supplement == nil {
			if taskSupplementEndpointUnsupported(claimErr) {
				return
			}
			if claimErr != nil {
				taskLog.Debug("additional message claim failed", "error", claimErr)
			}
			if !waitTaskSupplement(ctx, wakeup, d.effectiveTaskSupplementPollInterval()) {
				return
			}
			continue
		}

		// The adapter owns transport deadlines. Hook-based providers wait for a
		// safe boundary, which can follow a long-running tool; run cancellation
		// still aborts that wait and prevents late delivery.
		injectErr := session.Supplement(ctx, formatTaskSupplementInstruction(supplement.AuthorName, supplement.Content))
		reason := taskSupplementFailureReason(ctx, injectErr)
		if injectErr != nil {
			// Raw provider/Go diagnostics remain local. Workspace-visible state is
			// restricted to the stable reason code sent below.
			taskLog.Warn("additional message injection failed", "comment_id", supplement.CommentID, "reason", reason, "error", injectErr)
		}

		ackCtx, cancelAck := context.WithTimeout(context.WithoutCancel(ctx), taskSupplementAckTimeout)
		ackErr := d.client.AckTaskSupplement(ackCtx, taskID, supplement.CommentID, injectErr == nil, reason)
		cancelAck()
		if ackErr != nil {
			taskLog.Warn("additional message acknowledgement failed", "comment_id", supplement.CommentID, "error", ackErr)
		}
		// Do not sleep after a claim: drain already-ordered pending rows one at a
		// time. When empty, the branch above returns to the bounded wait.
	}
}
