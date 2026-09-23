package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// InteractiveBackend is optional: ordinary backends retain their one-shot
// execution contract. An interactive execution still has exactly one Result.
// Interrupting an activity does not publish that terminal result.
type InteractiveBackend interface {
	ExecuteInteractive(context.Context, string, ExecOptions) (*Session, error)
}

type InteractionState string

const (
	InteractionStarting      InteractionState = "starting"
	InteractionWorking       InteractionState = "working"
	InteractionInterrupting  InteractionState = "interrupting"
	InteractionAwaitingInput InteractionState = "awaiting_input"
	InteractionFinished      InteractionState = "finished"
)

type InteractionSnapshot struct {
	State    InteractionState `json:"state"`
	Activity uint64           `json:"activity"`
}

func (s InteractionSnapshot) Valid() bool {
	if s.Activity == 0 {
		return false
	}
	switch s.State {
	case InteractionStarting, InteractionWorking, InteractionInterrupting, InteractionAwaitingInput, InteractionFinished:
		return true
	default:
		return false
	}
}

type InteractionCommand struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // input, interrupt, idle finish, or agent complete; cancellation stays run-scoped
	Activity uint64 `json:"activity"`
	Text     string `json:"text,omitempty"`
}

type InteractionReceipt struct {
	Outcome         string              `json:"outcome"`
	Snapshot        InteractionSnapshot `json:"snapshot"`
	ClearedSteering []string            `json:"cleared_steering,omitempty"`
	ClearedFollowUp []string            `json:"cleared_follow_up,omitempty"`
}

var (
	ErrInteractionClosed = errors.New("interactive execution has finished")
	ErrInteractionStale  = errors.New("activity changed; refresh before sending")
	ErrInteractionBusy   = errors.New("another interaction is pending")
)

type interactionRequest struct {
	command InteractionCommand
	done    chan struct{}
	receipt InteractionReceipt
	err     error
}

// LiveControl serializes commands with the backend's terminal boundary. Only
// the provider loop consumes requests. A timed-out caller must retry with the
// SAME ID: cancellation of the HTTP request cannot retract a native RPC.
type LiveControl struct {
	beforeFinish func() bool
	mu           sync.Mutex
	snapshot     InteractionSnapshot
	requests     map[string]*interactionRequest
	queue        chan *interactionRequest
	pending      *interactionRequest
}

func newLiveControl() *LiveControl {
	return &LiveControl{
		snapshot: InteractionSnapshot{State: InteractionStarting, Activity: 1},
		requests: make(map[string]*interactionRequest),
		queue:    make(chan *interactionRequest, 1),
	}
}

func (c *LiveControl) Snapshot() InteractionSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

func (c *LiveControl) Submit(ctx context.Context, command InteractionCommand) (InteractionReceipt, error) {
	if err := ctx.Err(); err != nil {
		return InteractionReceipt{}, err
	}
	if command.ID == "" || len(command.ID) > 128 {
		return InteractionReceipt{}, errors.New("a bounded command id is required")
	}
	if command.Kind != "input" && command.Kind != "interrupt" && command.Kind != "finish" && command.Kind != "complete" {
		return InteractionReceipt{}, errors.New("unsupported interaction command")
	}
	if command.Kind == "input" && (strings.TrimSpace(command.Text) == "" || len(command.Text) > 1024*1024) {
		return InteractionReceipt{}, errors.New("input must contain between 1 and 1048576 bytes")
	}
	c.mu.Lock()
	request, exists := c.requests[command.ID]
	if exists && request.command != command {
		c.mu.Unlock()
		return InteractionReceipt{}, errors.New("command id was already used with different input")
	}
	if !exists {
		if c.snapshot.State == InteractionFinished {
			c.mu.Unlock()
			return InteractionReceipt{}, ErrInteractionClosed
		}
		if command.Activity != c.snapshot.Activity {
			c.mu.Unlock()
			return InteractionReceipt{}, ErrInteractionStale
		}
		if c.pending != nil || c.snapshot.State == InteractionStarting {
			c.mu.Unlock()
			return InteractionReceipt{}, ErrInteractionBusy
		}
		if command.Kind == "finish" && c.snapshot.State != InteractionAwaitingInput {
			c.mu.Unlock()
			return InteractionReceipt{}, errors.New("interrupt or wait for the reply before finishing the run")
		}
		// Bound retained deduplication state; never evict an old ID and risk
		// delivering its input twice during a long-lived run.
		if len(c.requests) >= 1024 {
			c.mu.Unlock()
			return InteractionReceipt{}, fmt.Errorf("interaction limit reached; finish this run before continuing")
		}
		request = &interactionRequest{command: command, done: make(chan struct{})}
		c.requests[command.ID] = request
		c.pending = request
		c.queue <- request
	}
	c.mu.Unlock()
	select {
	case <-request.done:
		return request.receipt, request.err
	case <-ctx.Done():
		return InteractionReceipt{}, ctx.Err()
	}
}

func (c *LiveControl) setState(state InteractionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.State == InteractionFinished {
		return
	}
	if state == InteractionWorking && c.snapshot.State == InteractionAwaitingInput {
		c.snapshot.Activity++
	}
	c.snapshot.State = state
}

func (c *LiveControl) acknowledge(r *interactionRequest, receipt InteractionReceipt, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != r {
		return
	}
	receipt.Snapshot = c.snapshot
	r.receipt, r.err = receipt, err
	c.pending = nil
	close(r.done)
}

// nextActivity is called only when starting a new native activity, not when
// steering the current one. A command accepted at natural completion also
// starts a new activity, even though the UI never entered awaiting_input.
func (c *LiveControl) nextActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshot.State != InteractionFinished {
		c.snapshot.Activity++
		c.snapshot.State = InteractionWorking
	}
}

// finish is atomic with Submit. A queued command wins over natural completion;
// the provider loop must resolve it before trying to finish again.
func (c *LiveControl) finish() bool {
	if c.beforeFinish != nil && !c.beforeFinish() {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil {
		return false
	}
	c.snapshot.State = InteractionFinished
	return true
}

func (c *LiveControl) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot.State = InteractionFinished
	if c.pending != nil {
		c.pending.receipt = InteractionReceipt{Outcome: "failed", Snapshot: c.snapshot}
		c.pending.err = ErrInteractionClosed
		close(c.pending.done)
		c.pending = nil
	}
}
