package daemon

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

type taskPhase string

const (
	taskPhaseClaimed            taskPhase = "claimed"
	taskPhasePrepareStarted     taskPhase = "prepare_started"
	taskPhaseSkillsReady        taskPhase = "skills_ready"
	taskPhaseEnvironmentReady   taskPhase = "environment_ready"
	taskPhaseRuntimeStarted     taskPhase = "runtime_started"
	taskPhaseFirstVisibleOutput taskPhase = "first_visible_output"
	taskPhaseFirstToolUse       taskPhase = "first_tool_use"
	taskPhaseTurnCompleted      taskPhase = "turn_completed"
	taskPhaseFinished           taskPhase = "finished"
)

type taskPhaseSample struct {
	Phase        taskPhase
	PhaseElapsed time.Duration
	TotalElapsed time.Duration
}

// taskPhaseRecorder records daemon-local task lifecycle timing. It is
// diagnostic-only: Mark has no error path and callers must not use it to make
// task-state decisions.
type taskPhaseRecorder struct {
	logger *slog.Logger
	now    func() time.Time

	mu       sync.Mutex
	started  time.Time
	previous time.Time
	marked   map[taskPhase]struct{}
}

func newTaskPhaseRecorder(logger *slog.Logger, now func() time.Time) *taskPhaseRecorder {
	if now == nil {
		now = time.Now
	}
	started := now()
	return &taskPhaseRecorder{
		logger:   logger,
		now:      now,
		started:  started,
		previous: started,
		marked:   make(map[taskPhase]struct{}),
	}
}

// Mark records and logs a phase only once. A duplicate mark returns a zero
// sample and does not consume the injected clock.
func (r *taskPhaseRecorder) Mark(phase taskPhase) taskPhaseSample {
	if r == nil {
		return taskPhaseSample{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.marked[phase]; ok {
		return taskPhaseSample{}
	}

	now := r.now()
	sample := taskPhaseSample{
		Phase:        phase,
		PhaseElapsed: now.Sub(r.previous),
		TotalElapsed: now.Sub(r.started),
	}
	r.marked[phase] = struct{}{}
	r.previous = now
	if r.logger != nil {
		r.logger.Info("task phase recorded",
			"task_phase", phase,
			"phase_elapsed_ms", sample.PhaseElapsed.Milliseconds(),
			"total_elapsed_ms", sample.TotalElapsed.Milliseconds(),
		)
	}
	return sample
}

type taskPhaseRecorderContextKey struct{}

func withTaskPhaseRecorder(ctx context.Context, recorder *taskPhaseRecorder) context.Context {
	return context.WithValue(ctx, taskPhaseRecorderContextKey{}, recorder)
}

func taskPhaseRecorderFromContext(ctx context.Context) *taskPhaseRecorder {
	recorder, _ := ctx.Value(taskPhaseRecorderContextKey{}).(*taskPhaseRecorder)
	return recorder
}

func isTaskVisibleOutput(msg agent.Message) bool {
	switch msg.Type {
	case agent.MessageText, agent.MessageThinking:
		return msg.Content != ""
	case agent.MessageToolUse, agent.MessageToolResult, agent.MessageError:
		return true
	default:
		return false
	}
}

func isTaskToolUse(msg agent.Message) bool {
	return msg.Type == agent.MessageToolUse
}
