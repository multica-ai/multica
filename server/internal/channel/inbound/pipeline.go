package inbound

import (
	"context"
	"errors"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

// Decision communicates how the Pipeline should proceed after a Step
// completes. Errors travel via the Step.Run error return value, not via
// Decision, so the two exit paths are typed separately (DESIGN §3.1).
type Decision int

const (
	// DecisionContinue advances the Pipeline to the next Step.
	DecisionContinue Decision = iota

	// DecisionSkip terminates the Pipeline without invoking any further
	// Step. Skip is the contract a Step uses to short-circuit cleanly,
	// e.g. dedup detecting a replay or identity-bind detecting an
	// unbound user it has already prompted out-of-band.
	DecisionSkip
)

// String renders Decision as a stable human label so test failures and
// log lines do not print bare integers.
func (d Decision) String() string {
	switch d {
	case DecisionContinue:
		return "Continue"
	case DecisionSkip:
		return "Skip"
	default:
		return "Decision(?)"
	}
}

// Step is the unit of work composed by Pipeline. Each Step has a stable
// Name() — used by Outcome.Terminal so the caller / telemetry can label
// the step that terminated the pipeline — and a Run method that
// processes a single InboundEvent.
//
// Run returns the (possibly mutated) event, a Decision, and an error.
// The pipeline aborts with that error if it is non-nil; otherwise the
// Decision determines whether the next step runs (Continue) or the
// pipeline terminates here (Skip).
type Step interface {
	Name() string
	Run(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Decision, error)
}

type Finalizer interface {
	Finalize(ctx context.Context, evt port.InboundEvent, outcome Outcome, runErr error) error
}

// Outcome captures how a Pipeline.Run invocation finished. It is only
// meaningful when Run returned a nil error; on error the caller MUST
// ignore Outcome (zero value).
//
// Terminal is the Name() of the last Step the Pipeline executed (the
// one that returned the final Decision). Decision is that Step's
// Decision — either DecisionContinue (i.e. all steps ran to completion)
// or DecisionSkip (i.e. that step short-circuited the rest).
type Outcome struct {
	Terminal string
	Decision Decision
}

// Observer receives low-cardinality pipeline telemetry. Implementations must
// not block the hot inbound path.
type Observer interface {
	StepDone(evt port.InboundEvent, step string, decision Decision, duration time.Duration, err error)
	PipelineDone(evt port.InboundEvent, outcome Outcome, duration time.Duration, err error)
}

// Pipeline runs a fixed, ordered list of Steps over a single
// InboundEvent. The list is captured at construction time and is
// immutable thereafter; Pipeline is therefore safe for concurrent use
// as long as every Step it holds is itself safe for concurrent use.
type Pipeline struct {
	steps    []Step
	observer Observer
}

// NewPipeline composes the supplied steps into a Pipeline. Steps run in
// the supplied order; passing zero steps is valid (Run will return a
// zero Outcome and a nil error) but uncommon outside tests.
func NewPipeline(steps ...Step) *Pipeline {
	// Defensive copy — callers occasionally hold the slice they passed
	// in and mutate it later; we don't want that mutation observable
	// from inside Run.
	cp := make([]Step, len(steps))
	copy(cp, steps)
	return &Pipeline{steps: cp}
}

func (p *Pipeline) SetObserver(observer Observer) {
	p.observer = observer
}

// Run executes each Step in order, threading the (possibly mutated)
// event from one Step into the next. The loop has three exit paths:
//
//  1. A Step returns a non-nil error — Run aborts immediately and
//     returns (Outcome{}, err). The zero Outcome is intentional: error
//     and Outcome are mutually exclusive return channels, so callers
//     can write `out, err := p.Run(...); if err != nil { ... }; use out`
//     without worrying that a partially-populated Outcome from the
//     failed step might leak into success-path code.
//
//  2. A Step returns DecisionSkip — Run terminates cleanly with the
//     Outcome describing which Step short-circuited (Outcome.Terminal
//     == s.Name(), Outcome.Decision == DecisionSkip). The remaining
//     Steps are not invoked.
//
//  3. Every Step returns DecisionContinue — Run completes the loop and
//     returns the final Outcome (Terminal == last Step's Name(),
//     Decision == DecisionContinue).
//
// `evt` is rebound on each iteration so a Step can mutate the event
// in-flight (e.g. identity-bind enriching SenderID with a Multica user
// id); the rebinding is how downstream Steps see upstream modifications.
func (p *Pipeline) Run(ctx context.Context, evt port.InboundEvent) (Outcome, error) {
	_, outcome, err := p.RunEvent(ctx, evt)
	return outcome, err
}

func (p *Pipeline) RunEvent(ctx context.Context, evt port.InboundEvent) (port.InboundEvent, Outcome, error) {
	var (
		outcome Outcome
		err     error
		d       Decision
	)
	started := time.Now()
	for i, s := range p.steps {
		stepStarted := time.Now()
		evt, d, err = s.Run(ctx, evt)
		if err != nil {
			failed := Outcome{Terminal: s.Name(), Decision: d}
			p.observeStep(evt, s.Name(), d, time.Since(stepStarted), err)
			finalizeErr := p.finalize(ctx, evt, failed, err, i)
			if finalizeErr != nil {
				err = finalizeErr
			}
			p.observePipeline(evt, failed, time.Since(started), err)
			return evt, Outcome{}, err
		}
		outcome = Outcome{Terminal: s.Name(), Decision: d}
		p.observeStep(evt, s.Name(), d, time.Since(stepStarted), nil)
		if d == DecisionSkip {
			err = p.finalize(ctx, evt, outcome, nil, i)
			p.observePipeline(evt, outcome, time.Since(started), err)
			return evt, outcome, err
		}
	}
	err = p.finalize(ctx, evt, outcome, nil, len(p.steps)-1)
	p.observePipeline(evt, outcome, time.Since(started), err)
	return evt, outcome, err
}

func (p *Pipeline) finalize(ctx context.Context, evt port.InboundEvent, outcome Outcome, runErr error, lastIdx int) error {
	if lastIdx < 0 {
		return runErr
	}
	var finalizerErr error
	for i := lastIdx; i >= 0; i-- {
		f, ok := p.steps[i].(Finalizer)
		if !ok {
			continue
		}
		if err := f.Finalize(ctx, evt, outcome, runErr); err != nil {
			finalizerErr = errors.Join(finalizerErr, err)
		}
	}
	if finalizerErr != nil {
		if runErr != nil {
			return errors.Join(runErr, finalizerErr)
		}
		return finalizerErr
	}
	return runErr
}

func (p *Pipeline) observeStep(evt port.InboundEvent, step string, decision Decision, duration time.Duration, err error) {
	if p.observer != nil {
		p.observer.StepDone(evt, step, decision, duration, err)
	}
}

func (p *Pipeline) observePipeline(evt port.InboundEvent, outcome Outcome, duration time.Duration, err error) {
	if p.observer != nil {
		p.observer.PipelineDone(evt, outcome, duration, err)
	}
}
