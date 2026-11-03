package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/pkg/codexappserver"
)

// SpareConditions says whether a provider may have a spare now and what a spare
// depends on. The zero value means it may not.
type SpareConditions struct {
	// Ready is set when the CLI is resolved and the user is signed in.
	Ready bool
	// Executable is the resolved CLI.
	Executable string
	// Binary identifies the CLI as path and version. A spare built from another
	// is discarded, and whatever failed for one is retried after an upgrade.
	Binary string
	// Identity covers what else a parked process has frozen: the signed-in user
	// and the environment.
	Identity string
	// ConfigFiles are the provider's user configuration; a change to any of
	// them, such as a new login, discards the spare.
	ConfigFiles []string
}

// CodexSpareLauncher starts a Codex process that serves no conversation yet.
type CodexSpareLauncher struct {
	Conditions func(ctx context.Context) SpareConditions
	// Start launches app-server outside any project directory, with env added.
	Start func(ctx context.Context, conditions SpareConditions, handler codex.Handler, env []string) (CodexServer, error)
}

// ClaudeSpareLauncher starts an adapter that parks a Claude Code process.
type ClaudeSpareLauncher struct {
	Conditions func(ctx context.Context) SpareConditions
	Start      StartClaudeAdapter
	// Fields are the prewarm request's fields: what a session cannot change.
	Fields func(conditions SpareConditions) map[string]any
}

// adapterCapabilities is implemented by adapters that report what they support;
// one that does not, or does not list prewarm and claim, predates spares.
type adapterCapabilities interface {
	Supports(capability string) bool
}

// errSpareUnsupported means this executable cannot serve spares; retrying it
// cannot help until it changes.
var errSpareUnsupported = errors.New("local conversation: this provider executable cannot serve spares")

// frameSwitch stands between a provider process and whoever owns it now: a
// process binds its handler when it starts, and a spare is not owned until it is
// claimed. Frames go to parked until hold keeps them for whoever adopts next.
type frameSwitch[T any] struct {
	mu      sync.Mutex
	parked  func(T)
	holding bool
	held    []T
	target  func(T)
}

func (f *frameSwitch[T]) deliver(frame T) {
	f.mu.Lock()
	switch {
	case f.target != nil:
		target := f.target
		f.mu.Unlock()
		target(frame)
	case f.holding:
		f.held = append(f.held, frame)
		f.mu.Unlock()
	default:
		parked := f.parked
		f.mu.Unlock()
		if parked != nil {
			parked(frame)
		}
	}
}

func (f *frameSwitch[T]) setParked(parked func(T)) {
	f.mu.Lock()
	f.parked = parked
	f.mu.Unlock()
}

// hold keeps frames from now on for the next owner, so one that fails to bind
// leaves no trace of them.
func (f *frameSwitch[T]) hold() {
	f.mu.Lock()
	f.holding = true
	f.mu.Unlock()
}

// adopt hands the frames to target. The lock stays held while they replay, so a
// frame arriving now cannot overtake them; target must not wait on this reader.
func (f *frameSwitch[T]) adopt(target func(T)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, frame := range f.held {
		target(frame)
	}
	f.held, f.holding, f.target = nil, false, target
}

// parkedSpare is a running process that belongs to no conversation.
type parkedSpare struct {
	pool       *sparePool
	provider   string
	created    time.Time
	conditions SpareConditions
	stamp      string
	stop       func()
	done       <-chan struct{}
	// taken closes when the spare leaves the pool, for any reason.
	taken chan struct{}

	// Codex.
	server      CodexServer
	token       string
	codexFrames *frameSwitch[codexappserver.Frame]

	// Claude.
	adapter      ClaudeAdapter
	claudeFrames *frameSwitch[claude.Output]
}

// reject discards a claimed spare that could not be bound. Nothing has reached
// the model, so the caller just starts the conversation normally.
func (p *parkedSpare) reject(reason string) {
	p.pool.mark(p.provider).MarkReason(timing.StageSpareRejected, reason)
	p.stop()
}

// spareSlot is one provider's spare and what is known about starting it.
type spareSlot struct {
	provider   string
	conditions func(ctx context.Context) SpareConditions
	launch     func(ctx context.Context, c SpareConditions) (*parkedSpare, error)

	mu       sync.Mutex
	spare    *parkedSpare
	starting bool
	cancel   context.CancelFunc
	// generation changes when in-flight starts stop being wanted.
	generation  int
	checked     SpareConditions
	checkedAt   time.Time
	failures    int
	nextAttempt time.Time
	// staleStreak counts spares in a row that went stale soon after starting.
	staleStreak int
	// unsupported are executables that will not be started again.
	unsupported map[string]bool
}

// sparePool owns one spare per provider. Spares belong to the service rather
// than to any conversation, and are refilled in the background.
type sparePool struct {
	s      *Service
	slots  map[string]*spareSlot
	root   context.Context
	cancel context.CancelFunc
}

func newSparePool(s *Service, opts Options) *sparePool {
	root, cancel := context.WithCancel(context.Background())
	p := &sparePool{s: s, slots: map[string]*spareSlot{}, root: root, cancel: cancel}
	if l := opts.CodexSpare; l != nil {
		p.slots["codex"] = &spareSlot{provider: "codex", conditions: l.Conditions, unsupported: map[string]bool{}, launch: func(ctx context.Context, c SpareConditions) (*parkedSpare, error) { return p.launchCodex(ctx, l, c) }}
	}
	if l := opts.ClaudeSpare; l != nil {
		p.slots["claude"] = &spareSlot{provider: "claude", conditions: l.Conditions, unsupported: map[string]bool{}, launch: func(ctx context.Context, c SpareConditions) (*parkedSpare, error) { return p.launchClaude(ctx, l, c) }}
	}
	return p
}

func (p *sparePool) mark(provider string) *timing.Trace { return p.s.traces.process("", provider) }

func (p *sparePool) launchCodex(ctx context.Context, l *CodexSpareLauncher, c SpareConditions) (*parkedSpare, error) {
	token, err := contextmcp.NewToken()
	if err != nil {
		return nil, err
	}
	frames := &frameSwitch[codexappserver.Frame]{}
	server, err := l.Start(ctx, c, frames.deliver, []string{ContextTokenEnv + "=" + token})
	if err != nil {
		return nil, err
	}
	frames.setParked(func(frame codexappserver.Frame) {
		if frame.Kind == codexappserver.FrameServerRequest {
			declineCodexRequest(server, frame)
		}
	})
	return &parkedSpare{provider: "codex", server: server, token: token, codexFrames: frames, stop: server.Stop, done: server.Done()}, nil
}

func (p *sparePool) launchClaude(ctx context.Context, l *ClaudeSpareLauncher, c SpareConditions) (*parkedSpare, error) {
	frames := &frameSwitch[claude.Output]{}
	adapter, err := l.Start(ctx, frames.deliver)
	if err != nil {
		return nil, err
	}
	stop := func() {
		closing, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = adapter.Close(closing)
	}
	if able, ok := adapter.(adapterCapabilities); !ok || !able.Supports(claude.CapabilityPrewarm) || !able.Supports(claude.CapabilityClaim) {
		stop()
		return nil, errSpareUnsupported
	}
	if _, err := adapter.Request(ctx, "prewarm", l.Fields(c)); err != nil {
		stop()
		var adapterErr *claude.AdapterError
		if errors.As(err, &adapterErr) && adapterErr.Code == claude.CodePrewarmUnsupported {
			return nil, errSpareUnsupported
		}
		return nil, err
	}
	return &parkedSpare{provider: "claude", adapter: adapter, claudeFrames: frames, stop: stop, done: adapter.Done()}, nil
}

// configStamp changes when a configuration file changes, appears or goes away.
func configStamp(files []string) string {
	var b strings.Builder
	for _, file := range files {
		if info, err := os.Stat(file); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d\n", file, info.ModTime().UnixNano(), info.Size())
		}
	}
	return b.String()
}

// spareStale is why a spare no longer matches the current conditions, or "".
func spareStale(spare *parkedSpare, c SpareConditions, now time.Time) string {
	switch {
	case now.Sub(spare.created) >= spareMaxAge:
		return timing.ReasonMaxAge
	case c.Binary != spare.conditions.Binary:
		return timing.ReasonCLIChanged
	case !c.Ready || c.Identity != spare.conditions.Identity:
		return timing.ReasonEnvironmentChanged
	case configStamp(spare.conditions.ConfigFiles) != spare.stamp:
		return timing.ReasonConfigChanged
	}
	return ""
}

// check discards spares that went stale and starts missing ones.
func (p *sparePool) check() {
	for _, slot := range p.slots {
		p.checkSlot(slot)
	}
}

func (p *sparePool) checkSlot(slot *spareSlot) {
	now := p.s.opts.Now()
	present := p.s.focus.clientsPresent(now, spareNoClientGrace)
	slot.mu.Lock()
	if slot.spare != nil && !present {
		p.discardLocked(slot, slot.spare, timing.ReasonNoClients)
	}
	if !present {
		slot.mu.Unlock()
		return
	}
	stale := slot.checkedAt.IsZero() || now.Sub(slot.checkedAt) >= spareCheckInterval
	slot.mu.Unlock()
	if stale {
		conditions := slot.conditions(p.root)
		slot.mu.Lock()
		if conditions.Binary != slot.checked.Binary {
			slot.failures, slot.nextAttempt = 0, time.Time{}
		}
		slot.checked, slot.checkedAt = conditions, now
		slot.mu.Unlock()
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.spare != nil {
		if reason := spareStale(slot.spare, slot.checked, now); reason != "" {
			p.discardStaleLocked(slot, slot.spare, reason, now)
		}
	}
	if slot.spare != nil || slot.starting || !slot.checked.Ready || slot.unsupported[slot.checked.Binary] || now.Before(slot.nextAttempt) || p.s.acceptTurn() != nil {
		return
	}
	p.startLocked(slot, slot.checked)
}

// discardLocked takes the spare out of the pool and stops it in the background;
// slot.mu is held.
func (p *sparePool) discardLocked(slot *spareSlot, spare *parkedSpare, reason string) {
	slot.spare = nil
	close(spare.taken)
	p.mark(slot.provider).MarkReason(timing.StageSpareDiscarded, reason)
	if !p.s.goBackground(spare.stop) {
		go spare.stop()
	}
}

// discardStaleLocked discards a spare and, when spares keep going stale soon after
// starting, delays the next one: a large process must not be replaced every time
// something rewrites a file it depends on. slot.mu is held.
func (p *sparePool) discardStaleLocked(slot *spareSlot, spare *parkedSpare, reason string, now time.Time) {
	if now.Sub(spare.created) >= spareStaleSteadyAfter {
		slot.staleStreak = 0
	} else {
		slot.staleStreak++
	}
	// One change is news rather than churn, so its replacement starts straight away.
	if slot.staleStreak > 1 {
		wait := spareMaxAge
		if shift := slot.staleStreak - 2; shift < 16 {
			wait = min(spareStaleBackoffBase<<shift, spareMaxAge)
		}
		if next := now.Add(wait); next.After(slot.nextAttempt) {
			slot.nextAttempt = next
		}
	}
	p.discardLocked(slot, spare, reason)
}

func (p *sparePool) startLocked(slot *spareSlot, conditions SpareConditions) {
	ctx, cancel := context.WithTimeout(p.root, prepareDeadline)
	generation := slot.generation
	slot.starting, slot.cancel = true, cancel
	began := p.s.opts.Now()
	started := p.s.goBackground(func() {
		defer cancel()
		spare, err := slot.launch(ctx, conditions)
		p.started(slot, generation, conditions, began, spare, err)
	})
	if !started {
		slot.starting, slot.cancel = false, nil
		cancel()
	}
}

func (p *sparePool) started(slot *spareSlot, generation int, conditions SpareConditions, began time.Time, spare *parkedSpare, err error) {
	now := p.s.opts.Now()
	slot.mu.Lock()
	defer slot.mu.Unlock()
	slot.starting, slot.cancel = false, nil
	if err != nil {
		if slot.generation == generation {
			p.failedLocked(slot, conditions.Binary, err, now)
		}
		return
	}
	if slot.generation != generation || p.s.acceptTurn() != nil {
		if !p.s.goBackground(spare.stop) {
			go spare.stop()
		}
		return
	}
	spare.pool, spare.created, spare.conditions, spare.taken = p, now, conditions, make(chan struct{})
	spare.stamp = configStamp(conditions.ConfigFiles)
	slot.spare = spare
	p.mark(slot.provider).MarkDuration(timing.StageSpareCreated, "", now.Sub(began))
	if spare.claudeFrames != nil {
		// An unbound error here means the parked process is gone.
		spare.claudeFrames.setParked(func(out claude.Output) {
			if out.Type == "error" {
				p.exited(slot, spare)
			}
		})
	}
	watching := p.s.goBackground(func() {
		select {
		case <-spare.done:
			p.exited(slot, spare)
		case <-spare.taken:
		}
	})
	if !watching {
		slot.spare = nil
		close(spare.taken)
		go spare.stop()
	}
}

// failedLocked counts a failed start: a later attempt waits longer, and enough
// of them in a row, or an executable that cannot serve spares at all, end the
// attempts for that executable. slot.mu is held.
func (p *sparePool) failedLocked(slot *spareSlot, binary string, err error, now time.Time) {
	if errors.Is(err, errSpareUnsupported) {
		slot.unsupported[binary] = true
		p.mark(slot.provider).MarkReason(timing.StageSpareRejected, timing.ReasonUnsupported)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	slot.failures++
	slot.nextAttempt = now.Add(spareBackoffBase << (slot.failures - 1))
	if slot.failures >= spareMaxFailures {
		slot.unsupported[binary] = true
	}
	p.mark(slot.provider).MarkFailed(timing.StageSpareRejected, "provider_error")
	p.s.opts.Logger.Info("local conversations: spare process failed to start", "provider", slot.provider, "failures", slot.failures, "error", err)
}

// exited replaces a spare that went away while parked. One that did not last
// counts as a failed start, so a CLI that crashes on start is not kept restarting.
func (p *sparePool) exited(slot *spareSlot, spare *parkedSpare) {
	now := p.s.opts.Now()
	slot.mu.Lock()
	if slot.spare != spare {
		slot.mu.Unlock()
		return
	}
	if lived := now.Sub(spare.created); lived < spareHealthyAfter {
		p.failedLocked(slot, spare.conditions.Binary, errors.New("spare exited while parked"), now)
	} else {
		slot.failures, slot.nextAttempt = 0, time.Time{}
	}
	p.discardLocked(slot, spare, timing.ReasonExited)
	slot.mu.Unlock()
	p.s.kickMaintenance()
}

// claim takes the provider's spare for a new conversation, or nil. Conditions are
// read again here: a spare is only ever claimed with what it was built from.
func (p *sparePool) claim(ctx context.Context, provider string) *parkedSpare {
	slot := p.slots[provider]
	if slot == nil {
		return nil
	}
	slot.mu.Lock()
	spare := slot.spare
	slot.mu.Unlock()
	if spare == nil {
		return nil
	}
	conditions := slot.conditions(ctx)
	now := p.s.opts.Now()
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.spare != spare {
		return nil
	}
	if reason := spareStale(spare, conditions, now); reason != "" {
		p.discardStaleLocked(slot, spare, reason, now)
		p.s.kickMaintenance()
		return nil
	}
	slot.spare = nil
	close(spare.taken)
	if now.Sub(spare.created) >= spareHealthyAfter {
		slot.failures, slot.nextAttempt = 0, time.Time{}
	}
	p.s.kickMaintenance()
	return spare
}

// stopAll stops every spare and any start underway, leaving later refills to the
// drain state and to Undrain.
func (p *sparePool) stopAll() {
	var stops []func()
	for _, slot := range p.slots {
		slot.mu.Lock()
		slot.generation++
		if slot.cancel != nil {
			slot.cancel()
		}
		if spare := slot.spare; spare != nil {
			slot.spare = nil
			close(spare.taken)
			p.mark(slot.provider).MarkReason(timing.StageSpareDiscarded, timing.ReasonStopped)
			stops = append(stops, spare.stop)
		}
		slot.mu.Unlock()
	}
	var wg sync.WaitGroup
	for _, stop := range stops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop()
		}()
	}
	wg.Wait()
}

func (p *sparePool) close() {
	p.cancel()
	p.stopAll()
}
