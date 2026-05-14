// Package notify provides the out-of-platform push bridge for cascade
// events (PUL-102 PR6). Sends failure / loop-guard / plan-amended /
// completion notifications to Slack and Telegram so the user gets
// pinged while off-platform (mobile, in transit). Falls back to a
// multica issue comment with an @mention when push channels fail
// three times.
//
// PR6 ships the bridge with env-var-configured channels and the
// retry/fallback contract. PR4's worker calls Bridge.Send when the
// orchestration emits an event that warrants a push.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// EventType enumerates cascade events that warrant a push. Pinned
// to a Go-side constant (not just string) so the worker cannot
// emit a typo that would slip through into the message body.
type EventType string

const (
	EventLoopGuardTripped EventType = "loop_guard_tripped"
	EventPlanAmended      EventType = "plan_amended"
	EventPlanCompleted    EventType = "plan_completed"
	EventStuck24h         EventType = "stuck_24h"
	EventRebaseConflict   EventType = "rebase_conflict"
	EventMissingAssignee  EventType = "missing_assignee"
)

// Event is the input to Bridge.Send. All fields except Type and
// IssueID are optional; the notifier shapes message text accordingly.
type Event struct {
	Type       EventType
	IssueID    string // multica UUID
	IssuePUL   string // human "PUL-102"
	IssueTitle string

	// Source PR fields (omit empty). Loop-guard and rebase-conflict
	// events carry these; plan_amended and plan_completed don't.
	PRNumber int
	PRURL    string
	HeadSHA  string

	// HumanContext is an optional one-line narrative the worker can
	// pass — e.g. "fixed 3 times on different head_sha". Renders as
	// a Slack section / Telegram paragraph.
	HumanContext string
}

// Channel is the abstraction every push transport (Slack, Telegram,
// future PagerDuty etc.) implements. Send returns nil on accepted
// delivery, an error otherwise. Idempotency is the channel's
// responsibility — if Slack/Telegram dedupe by message hash, that
// happens server-side at the destination.
type Channel interface {
	// Name identifies the channel in logs and metrics: "slack",
	// "telegram", etc.
	Name() string

	// Send delivers the event. The implementation is responsible
	// for formatting the event into the channel's native message
	// shape (Slack blocks, Telegram parseMode=MarkdownV2, …).
	Send(ctx context.Context, e Event) error
}

// CommentPoster is the multica-side fallback surface. When all push
// channels fail their retry budget, the Bridge calls PostComment to
// surface the event on the issue itself — that way the user always
// sees the signal, even if Slack/Telegram are wedged.
//
// Implemented by PR4's worker against the multica API; PR6 tests
// supply a fake.
type CommentPoster interface {
	PostComment(ctx context.Context, issueID string, body string) error
}

// Bridge fans an event out to every configured Channel, retries on
// transient failure, and falls back to a multica issue comment when
// the retry budget is exhausted.
type Bridge struct {
	channels      []Channel
	commentPoster CommentPoster
	retryDelays   []time.Duration // [1m, 5m, 15m] in production
	logger        *slog.Logger
}

// New constructs a Bridge from the supplied channels and fallback.
// Pass an empty channels slice to disable push entirely — every
// event then goes straight to the comment fallback (still useful:
// keeps the on-issue audit trail intact even without Slack/Telegram).
//
// retryDelays is exposed for tests; production callers should use
// DefaultRetryDelays.
func New(channels []Channel, commentPoster CommentPoster, retryDelays []time.Duration, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	if len(retryDelays) == 0 {
		retryDelays = DefaultRetryDelays
	}
	return &Bridge{
		channels:      channels,
		commentPoster: commentPoster,
		retryDelays:   retryDelays,
		logger:        logger,
	}
}

// DefaultRetryDelays implements the plan's 1m → 5m → 15m backoff.
// After three attempts the Bridge falls back to a multica comment.
var DefaultRetryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}

// Send delivers an event to every channel. Each channel's failures
// retry independently (one channel's outage does not block another
// channel from succeeding). After the retry budget on a given
// channel is exhausted, the Bridge falls back to a multica comment
// for that specific channel's audience. Channel-level errors are
// logged but not returned — the caller (PR4 worker) is a
// best-effort notifier, not a blocker on the cascade itself.
func (b *Bridge) Send(ctx context.Context, e Event) {
	if len(b.channels) == 0 {
		b.fallback(ctx, e, "no channels configured")
		return
	}

	for _, ch := range b.channels {
		b.sendWithRetry(ctx, ch, e)
	}
}

// sendWithRetry tries the channel up to len(retryDelays)+1 times
// (1 initial + N retries). The sleep happens between attempts;
// context cancellation aborts the loop early.
func (b *Bridge) sendWithRetry(ctx context.Context, ch Channel, e Event) {
	var lastErr error
	for attempt := 0; attempt <= len(b.retryDelays); attempt++ {
		if attempt > 0 {
			delay := b.retryDelays[attempt-1]
			select {
			case <-ctx.Done():
				b.logger.Warn("cascade.notify.cancelled",
					"channel", ch.Name(),
					"attempt", attempt,
					"event", e.Type,
				)
				return
			case <-time.After(delay):
			}
		}
		err := ch.Send(ctx, e)
		if err == nil {
			b.logger.Info("cascade.notify.sent",
				"channel", ch.Name(),
				"attempt", attempt+1,
				"event", e.Type,
				"issue_id", e.IssueID,
			)
			return
		}
		lastErr = err
		b.logger.Warn("cascade.notify.retry",
			"channel", ch.Name(),
			"attempt", attempt+1,
			"event", e.Type,
			"error", err,
		)
	}

	// All attempts exhausted — fallback for this channel's audience.
	b.fallback(ctx, e, fmt.Sprintf("channel %s exhausted retries: %v", ch.Name(), lastErr))
}

// fallback posts a multica comment so the user sees the signal even
// when push channels are wedged. Errors from the comment poster are
// logged at Error — at this point we have nothing left to fall back
// to, so visibility matters.
func (b *Bridge) fallback(ctx context.Context, e Event, reason string) {
	if b.commentPoster == nil {
		b.logger.Error("cascade.notify.fallback_unavailable",
			"event", e.Type,
			"issue_id", e.IssueID,
			"reason", reason,
		)
		return
	}
	body := buildFallbackComment(e, reason)
	if err := b.commentPoster.PostComment(ctx, e.IssueID, body); err != nil {
		b.logger.Error("cascade.notify.fallback_failed",
			"event", e.Type,
			"issue_id", e.IssueID,
			"error", err,
		)
		return
	}
	b.logger.Info("cascade.notify.fallback_posted",
		"event", e.Type,
		"issue_id", e.IssueID,
		"reason", reason,
	)
}

// buildFallbackComment renders the comment body for the fallback
// path. Includes the reason so a future investigator can tell the
// difference between "push unconfigured" and "channels exhausted".
func buildFallbackComment(e Event, reason string) string {
	header := fmt.Sprintf("⚠️ Cascade event: **%s**", e.Type)
	if e.IssuePUL != "" {
		header += fmt.Sprintf(" on %s", e.IssuePUL)
	}
	body := header + "\n\n"
	if e.HumanContext != "" {
		body += e.HumanContext + "\n\n"
	}
	if e.PRURL != "" {
		body += fmt.Sprintf("PR: %s\n\n", e.PRURL)
	}
	body += fmt.Sprintf("_Posted via multica comment after push notification fallback: %s_", reason)
	return body
}

// ErrChannelTransient lets a Channel signal "retry me" explicitly.
// Network blips and 5xx from Slack/Telegram match this; permanent
// failures (wrong webhook URL, malformed body) return a plain error
// so the Bridge still surfaces them via the fallback path. Channels
// that don't classify errors will be retried regardless — the
// distinction is observability-only today.
var ErrChannelTransient = errors.New("cascade.notify: transient channel failure")
