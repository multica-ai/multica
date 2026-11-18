package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func (b *kagentBackend) runKagent(ctx context.Context, lease *kagent.BindingLease, rec *kagent.LockedRecord, state *kagentConversation, attempt kagentAttempt, recovering bool, input string, messages chan<- Message, terminal *atomic.Bool) Result {
	name, _ := kagent.ResourceName(lease.Desired.AgentID)
	ns := lease.Desired.Namespace
	emit := func(m Message) {
		select {
		case messages <- m:
		default:
		}
	}
	unknown := func() Result {
		emit(Message{Type: MessageError, Content: kagent.ErrUnknownOutcome.Error(), SessionID: state.SessionID})
		return Result{Status: "cleanup_unconfirmed", Error: kagent.ErrUnknownOutcome.Error(), SessionID: state.SessionID}
	}
	fence := lease.Execution()
	// Persist before observing a known task. If the process dies before the
	// response disposition is committed, recovery cannot trust later output.
	fence.OutputUncertain = fence.OutputUncertain || fence.ObservationPending
	get := func(callCtx context.Context) (*kagent.RemoteTask, error) {
		fence.ObservationPending = true
		if err := lease.SaveExecution(fence); err != nil {
			return nil, err
		}
		task, err := b.cfg.Client.Get(callCtx, ns, name, attempt.TaskID, state.SessionID)
		if err != nil {
			fence.OutputUncertain = true
		}
		return task, err
	}
	observe := func(task *kagent.RemoteTask) error {
		fence.SuppressOutput = fence.SuppressOutput || attempt.SuppressOutput || task.Status.State == kagent.StateInputRequired || task.Status.State == kagent.StateAuthRequired
		fence.DeadlineExceeded = fence.DeadlineExceeded || attempt.DeadlineExceeded || ctx.Err() == context.DeadlineExceeded
		fence.ObservationPending = false
		fence.TaskID, fence.SessionID = task.ID, task.ContextID
		return lease.SaveExecution(fence)
	}
	var task *kagent.RemoteTask
	var err error
	if recovering {
		task, err = get(ctx)
	} else {
		task, err = b.cfg.Client.Send(ctx, ns, name, attempt.MessageID, state.SessionID, input)
	}
	if err != nil {
		if recovering && ctx.Err() != nil {
			task = &kagent.RemoteTask{ID: attempt.TaskID, ContextID: state.SessionID}
		} else {
			return unknown()
		}
	}
	// Commit identity and disposition before publishing status or allowing a later
	// turn. Never infer session identity from a task ID or persist diagnostics.
	if err = observe(task); err != nil {
		return unknown()
	}
	state.SessionID = task.ContextID
	attempt.TaskID = task.ID
	attempt.SuppressOutput = fence.SuppressOutput
	attempt.DeadlineExceeded = fence.DeadlineExceeded
	state.Attempts[b.cfg.AttemptID] = attempt
	if err = rec.Save(state); err != nil {
		return unknown()
	}
	emit(Message{Type: MessageStatus, Status: task.Status.State, SessionID: state.SessionID})
	lastState := task.Status.State
	unsupportedInput := fence.SuppressOutput || attempt.SuppressOutput
	ticker := time.NewTicker(b.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if task.Terminal() {
			break
		}
		if ctx.Err() != nil || task.Status.State == kagent.StateInputRequired || task.Status.State == kagent.StateAuthRequired {
			unsupportedInput = unsupportedInput || task.Status.State == kagent.StateInputRequired || task.Status.State == kagent.StateAuthRequired
			if (unsupportedInput && !fence.SuppressOutput) || (ctx.Err() == context.DeadlineExceeded && !fence.DeadlineExceeded) {
				fence.SuppressOutput = unsupportedInput
				fence.DeadlineExceeded = fence.DeadlineExceeded || ctx.Err() == context.DeadlineExceeded
				if err = lease.SaveExecution(fence); err != nil {
					return unknown()
				}
			}
			// Cancellation uses a fresh bounded context and requires an immutable
			// remote terminal state. Input/auth flows have no supported local UI yet.
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			fence.ObservationPending = true
			if err = lease.SaveExecution(fence); err != nil {
				cancel()
				return unknown()
			}
			task, err = b.cfg.Client.Cancel(cleanup, ns, name, attempt.TaskID, state.SessionID)
			if err == nil {
				if err = observe(task); err != nil {
					cancel()
					return unknown()
				}
				unsupportedInput = unsupportedInput || fence.SuppressOutput
			}
			if err != nil || !task.Terminal() {
				task, err = get(cleanup)
				if err == nil {
					if err = observe(task); err != nil {
						cancel()
						return unknown()
					}
				}
			}
			cancel()
			if err != nil || !task.Terminal() {
				return unknown()
			}
			unsupportedInput = unsupportedInput || fence.SuppressOutput
			break
		}
		select {
		case <-ctx.Done():
			continue
		case <-ticker.C:
		}
		task, err = get(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Retain the last known identity for confirmed cancellation next pass.
				task = &kagent.RemoteTask{ID: attempt.TaskID, ContextID: state.SessionID}
				continue
			}
			return unknown()
		}
		if err = observe(task); err != nil {
			return unknown()
		}
		unsupportedInput = unsupportedInput || fence.SuppressOutput
		if task.Status.State != lastState {
			emit(Message{Type: MessageStatus, Status: task.Status.State, SessionID: state.SessionID})
			lastState = task.Status.State
		}
	}
	result := Result{SessionID: state.SessionID}
	// Even a completed cancellation response must not expose diagnostics from an
	// unsupported input/auth flow that preceded it.
	if !unsupportedInput && !fence.OutputUncertain {
		result.Output = task.Text()
	}
	switch task.Status.State {
	case kagent.StateCompleted:
		result.Status = "completed"
	case kagent.StateCanceled:
		result.Status = "cancelled"
		if fence.DeadlineExceeded {
			result.Status = "timeout"
			result.Error = "kagent: execution deadline exceeded; remote task cancellation confirmed"
		}
	case kagent.StateFailed, kagent.StateRejected:
		result.Status = "failed"
		result.Error = fmt.Sprintf("kagent: remote task %s", task.Status.State)
	}
	if unsupportedInput {
		result.Status = "failed"
		result.Error = "kagent: remote input/auth flow is unsupported; remote terminal state confirmed"
		result.Output = ""
	}
	if fence.OutputUncertain && !unsupportedInput && !fence.DeadlineExceeded {
		result.Status, result.Error, result.Output = "failed", "kagent: output unavailable after interrupted observation", ""
	}
	// Bound persisted output. Return the same fixed disposition on the initial
	// delivery and replay rather than silently truncating a successful result.
	if len(result.Output) > 32<<10 {
		result.Status, result.Output, result.Error = "failed", "", "kagent: completed output exceeds durable replay limit"
	}
	attempt.SuppressOutput = unsupportedInput
	attempt.DeadlineExceeded = fence.DeadlineExceeded
	attempt.Settled = true
	attempt.TerminalState = task.Status.State
	attempt.Outcome = &kagentOutcome{Status: result.Status, Error: result.Error, Output: result.Output, SessionID: result.SessionID}
	state.Attempts[b.cfg.AttemptID] = attempt
	if state.ActiveAttempt == b.cfg.AttemptID {
		state.ActiveAttempt = ""
	}
	if err = rec.Save(state); err != nil {
		return unknown()
	}
	if err = lease.SaveExecution(nil); err != nil {
		return unknown()
	}
	terminal.Store(true)
	if result.Output != "" {
		emit(Message{Type: MessageText, Content: result.Output})
	}
	return result
}
