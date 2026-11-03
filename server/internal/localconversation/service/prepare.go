package service

import (
	"context"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// prepareTask is one run of a runner's preparation. Sends and preheat share it.
type prepareTask struct {
	id string
	// preheat marks a task nobody's turn is waiting on yet.
	preheat bool
	// joined is set when a send waits on this task, whose process then serves a
	// turn. Guarded by the runner's mutex.
	joined bool
	done   chan struct{}
	// The fields below are set before done closes.
	err error
	// failKind is the turn error kind a send reports for err.
	failKind string
	// threadID is the Codex thread the task started or resumed.
	threadID string
}

// prepareGate is a runner's preparation state, guarded by the runner's mutex.
// Tasks run on the gate's own context rather than a request's, so abandoning a
// preheat cannot fail a send waiting on the same task, and Close can cancel it.
type prepareGate struct {
	root   context.Context
	cancel context.CancelFunc
	// deadline bounds each task.
	deadline time.Duration
	task     *prepareTask
	closed   bool
	// preheated is set while the process comes from a preheat and has served
	// no turn; lastID is that preheat's task.
	preheated bool
	lastID    string
	// idleSince is when the process last stopped doing anything.
	idleSince time.Time
}

func newPrepareGate() prepareGate {
	root, cancel := context.WithCancel(context.Background())
	return prepareGate{root: root, cancel: cancel, deadline: prepareDeadline}
}

// begin registers a new task and returns the context it runs on.
func (g *prepareGate) begin(preheat bool) (*prepareTask, context.Context, context.CancelFunc) {
	task := &prepareTask{id: "prepare-" + newID(), preheat: preheat, done: make(chan struct{})}
	g.task = task
	ctx, cancel := context.WithTimeout(g.root, g.deadline)
	return task, ctx, cancel
}

// finish ends the running task; the caller closes task.done after unlocking.
func (g *prepareGate) finish(task *prepareTask, now time.Time) {
	g.task = nil
	if task.err == nil {
		g.idleSince = now
		if task.preheat && !task.joined {
			g.preheated, g.lastID = true, task.id
		}
	}
}

// sendReady records how a send found the runner on its trace: joining a preheat
// or using its result counts as preheated. The process serves a turn from here.
func (g *prepareGate) sendReady(trace *timing.Trace, ready bool) {
	switch {
	case g.task != nil && g.task.preheat:
		g.task.joined = true
		trace.SetPrepare(timing.PreparePreheated, g.task.id)
	case ready && g.preheated:
		trace.SetPrepare(timing.PreparePreheated, g.lastID)
	case ready:
		trace.SetPrepare(timing.PrepareHot, "")
	default:
		trace.SetPrepare(timing.PrepareCold, "")
	}
	g.preheated = false
}

// waitPrepare waits for task, or returns early with stopped when the user
// stopped the turn or with the context's error.
func waitPrepare(ctx context.Context, task *prepareTask, stop <-chan struct{}) (stopped bool, err error) {
	select {
	case <-task.done:
		return false, nil
	case <-stop:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// waitClosed gives a cancelled task the chance to stop what it started.
func waitClosed(ctx context.Context, task *prepareTask) {
	if task == nil {
		return
	}
	select {
	case <-task.done:
	case <-ctx.Done():
	}
}

// idleProcess describes a conversation's process that is doing nothing.
type idleProcess struct {
	provider string
	scopeID  int64
	since    time.Time
	// preheated means it has served no turn since it was prepared.
	preheated bool
	// evictable is false when work outlives the turn, such as Claude's
	// background tasks.
	evictable bool
}

// stopSignal lets a turn's stop wake a send waiting for preparation.
type stopSignal struct {
	once sync.Once
	ch   chan struct{}
}

func newStopSignal() *stopSignal { return &stopSignal{ch: make(chan struct{})} }

func (s *stopSignal) fire() {
	if s != nil {
		s.once.Do(func() { close(s.ch) })
	}
}

func (s *stopSignal) wait() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}
