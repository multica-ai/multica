package lark

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// ErrDispatcherNotConfigured is surfaced to the connector when handleEvent
// runs on a FeishuRuntime built without a Dispatcher. Returning it (instead
// of silently dropping) lets the connector log and/or disconnect so the
// misconfiguration is visible in production.
var ErrDispatcherNotConfigured = errors.New("lark: dispatcher not configured")

// defaultReplyTimeout caps a single OutcomeReplier.Reply call. The replier
// runs in a detached goroutine off the ACK critical path, so it must stay
// strictly under Lark's 3-second long-conn ACK deadline even when stacked on
// top of dispatch latency.
const defaultReplyTimeout = 2500 * time.Millisecond

// FeishuRuntime is the inbound-processing core of the Feishu adapter: it
// runs the Dispatcher on each decoded event and drives the detached
// OutcomeReplier + typing indicator. It was extracted verbatim from the
// former lark.Hub.handleEvent / scheduleReply so the channel-agnostic
// engine.Supervisor stays free of any platform reply logic — the Supervisor
// only manages the connection; the feishuChannel hands each inbound event to
// this runtime.
//
// The replier is deliberately decoupled from the connector's ACK path: the
// connector ACKs as soon as emit returns, so coupling it to outbound Lark
// HTTP (token mint, card send) would let a slow send stall the ACK past
// Lark's 3s budget. handleEvent returns the verdict synchronously; the reply
// runs in its own goroutine bounded by ReplyTimeout. Drain joins those
// goroutines on shutdown.
type FeishuRuntime struct {
	dispatcher      *Dispatcher
	replier         OutcomeReplier
	typingIndicator *TypingIndicatorManager
	replyTimeout    time.Duration
	logger          *slog.Logger

	// replyWg tracks in-flight outbound reply goroutines (NeedsBinding
	// card, offline notice, …) so Drain can join them at shutdown — a hung
	// outbound Lark HTTP call cannot block exit beyond ReplyTimeout.
	replyWg sync.WaitGroup
}

// FeishuRuntimeConfig tunes the runtime. Zero values default.
type FeishuRuntimeConfig struct {
	ReplyTimeout time.Duration
	Logger       *slog.Logger
}

// NewFeishuRuntime builds a runtime around the inbound Dispatcher. The
// replier defaults to the noop replier (so a deployment that has not wired
// outbound replies still runs the inbound pipeline) — call SetOutcomeReplier
// to install the production one. dispatcher may be nil in tests; handleEvent
// then returns ErrDispatcherNotConfigured.
func NewFeishuRuntime(dispatcher *Dispatcher, cfg FeishuRuntimeConfig) *FeishuRuntime {
	if cfg.ReplyTimeout == 0 {
		cfg.ReplyTimeout = defaultReplyTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &FeishuRuntime{
		dispatcher:   dispatcher,
		replier:      NewNoopOutcomeReplier(cfg.Logger),
		replyTimeout: cfg.ReplyTimeout,
		logger:       cfg.Logger,
	}
}

// SetOutcomeReplier installs the production replier. Must be called BEFORE
// the runtime starts handling events. Nil resets to the noop replier.
func (r *FeishuRuntime) SetOutcomeReplier(rep OutcomeReplier) {
	if rep == nil {
		rep = NewNoopOutcomeReplier(r.logger)
	}
	r.replier = rep
}

// SetTypingIndicatorManager installs the typing-indicator manager. Must be
// called BEFORE the runtime starts handling events. Nil disables it.
func (r *FeishuRuntime) SetTypingIndicatorManager(m *TypingIndicatorManager) {
	r.typingIndicator = m
}

// handleEvent dispatches one inbound message and drives the outbound side
// (typing indicator on ingest, detached OutcomeReplier). It is the seam the
// feishuChannel's emit calls. It returns the dispatch verdict + any infra
// error so the connector can decide whether to reconnect; it MUST return
// promptly because the connector writes the frame ACK as soon as emit
// returns.
func (r *FeishuRuntime) handleEvent(ctx context.Context, inst Installation, msg InboundMessage) (DispatchResult, error) {
	log := r.logger.With("installation_id", uuidString(inst.ID))
	if r.dispatcher == nil {
		log.Warn("lark runtime: dispatcher not configured; dropping event", "event_id", msg.EventID)
		return DispatchResult{}, ErrDispatcherNotConfigured
	}
	res, err := r.dispatcher.Handle(ctx, msg)
	if err != nil {
		log.Error("lark runtime: dispatcher error", "event_id", msg.EventID, "error", err)
		return res, err
	}
	log.Debug("lark runtime: dispatch outcome",
		"event_id", msg.EventID,
		"outcome", string(res.Outcome),
		"drop_reason", string(res.DropReason),
	)
	if res.Outcome == OutcomeIngested && r.typingIndicator != nil {
		// Detached: the typing reaction HTTP call must not block the ACK
		// path. A short timeout keeps the goroutine from hanging.
		go func() {
			addCtx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
			defer cancel()
			r.typingIndicator.Add(addCtx, inst, res.ChatSessionID, msg.MessageID, msg.CreateTime)
		}()
	}
	r.scheduleReply(inst, msg, res)
	return res, nil
}

// scheduleReply detaches the OutcomeReplier from the ACK critical path. The
// reply goroutine uses a fresh context.Background() with a ReplyTimeout
// deadline so it is independent of the inbound emit ctx (which the connector
// cancels as soon as Run exits) — inheriting would kill a still-wanted
// binding card / offline notice for no reason. A noop replier runs inline so
// a deployment without outbound wiring pays no goroutine cost.
func (r *FeishuRuntime) scheduleReply(inst Installation, msg InboundMessage, res DispatchResult) {
	rep := r.replier
	if rep == nil {
		return
	}
	if _, isNoop := rep.(*noopReplier); isNoop {
		rep.Reply(context.Background(), inst, msg, res)
		return
	}
	r.replyWg.Add(1)
	go func() {
		defer r.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
		defer cancel()
		rep.Reply(ctx, inst, msg, res)
		if ctx.Err() == context.DeadlineExceeded {
			r.logger.Warn("lark runtime: outbound reply timed out",
				"event_id", msg.EventID,
				"outcome", string(res.Outcome),
				"timeout", r.replyTimeout.String(),
			)
		}
	}()
}

// Drain flushes any debounced run triggers and then joins every in-flight
// reply goroutine. Call it on shutdown AFTER the engine Supervisor has
// stopped delivering inbound events (so no new triggers/replies are
// scheduled). The flush may itself emit an offline/archived notice, so it
// runs before the reply join. Reply goroutines are each bounded by
// ReplyTimeout, so Drain returns in bounded time even if outbound Lark HTTP
// is wedged.
func (r *FeishuRuntime) Drain() {
	if r.dispatcher != nil {
		r.dispatcher.FlushPendingRuns()
	}
	r.replyWg.Wait()
}
