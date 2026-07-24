package daemon

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// chatSuggestTimeout bounds the follow-up suggestion pass. The pass runs in
// the background after the completion callback, so it no longer delays the
// user's turn — the bound just keeps a hung provider from pinning a session's
// suggest slot (and the client-side placeholder) for long.
const chatSuggestTimeout = 20 * time.Second

// chatSuggestPrompt is the entire instruction set for the suggestion pass.
// Unlike the retired runtime-brief section, this is the turn's ONLY task:
// dedicated single-instruction calls follow output formats reliably where
// "when useful, also append X" instructions buried in a long brief decay over
// a conversation (MUL-5149 follow-up; see PR discussion for the field data).
const chatSuggestPrompt = `This is an automated system request, not a user message. Never mention it or its output in future replies.

Based on the conversation so far, produce exactly 3 follow-up actions the user is likely to want next. Write them in the same language the user has been writing in. Each action needs:
- "label": short button text (a few words)
- "prompt": the complete message to send on the user's behalf when clicked (self-contained, imperative)
- "primary": true on exactly one action, the one you most recommend

Output ONLY a JSON array, no prose, no code fences:
[{"label":"...","prompt":"...","primary":true}]

Only when the conversation truly offers nothing worth following up on, output [].`

// chatSuggestHandle wraps the cancel func in a comparable pointer so a
// finishing job only deregisters itself (CompareAndDelete), never a newer
// job that replaced it.
type chatSuggestHandle struct{ cancel context.CancelFunc }

// cancelPendingChatSuggest aborts an in-flight suggestion pass for the given
// chat session. Called when a new turn starts on that session: two resumed
// provider invocations must not race on one session, and suggestions for a
// superseded turn are stale anyway.
func (d *Daemon) cancelPendingChatSuggest(sessionID string) {
	if v, ok := d.chatSuggestCancels.LoadAndDelete(sessionID); ok {
		v.(*chatSuggestHandle).cancel()
	}
}

// chatSuggestJob builds the deferred suggestion pass for a completed direct
// chat turn. reportTaskResult invokes it (on its own goroutine) only after
// the completion callback succeeded — the supplement targets the assistant
// row that callback writes, and the user's next turn must never wait on this.
//
// mainUsage is the main turn's per-model usage: the server's task_usage
// upsert REPLACES counts per (task, provider, model), so the job must
// re-report merged totals, not the suggest delta alone.
func (d *Daemon) chatSuggestJob(task Task, backend agent.Backend, opts agent.ExecOptions, providerSessionID string, mainUsage map[string]agent.TokenUsage, provider string, taskLog *slog.Logger) func() {
	return func() {
		ctx, cancel := context.WithCancel(context.Background())
		handle := &chatSuggestHandle{cancel: cancel}
		d.chatSuggestCancels.Store(task.ChatSessionID, handle)
		defer func() {
			cancel()
			d.chatSuggestCancels.CompareAndDelete(task.ChatSessionID, handle)
		}()

		raw, usage := d.runChatSuggestPass(ctx, backend, opts, providerSessionID, taskLog)
		if ctx.Err() != nil {
			// A newer turn on this session cancelled us: its user message
			// supersedes these suggestions. Skip the supplement — the client
			// placeholder for the old turn resolves via its own timeout.
			taskLog.Debug("chat suggest pass cancelled by newer turn")
			return
		}

		reportCtx, reportCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer reportCancel()
		if len(usage) > 0 {
			merged := mergeUsage(mainUsage, usage)
			entries := make([]TaskUsageEntry, 0, len(merged))
			for model, u := range merged {
				if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheWriteTokens == 0 {
					continue
				}
				entries = append(entries, TaskUsageEntry{
					Provider:         provider,
					Model:            model,
					InputTokens:      u.InputTokens,
					OutputTokens:     u.OutputTokens,
					CacheReadTokens:  u.CacheReadTokens,
					CacheWriteTokens: u.CacheWriteTokens,
				})
			}
			if err := d.client.ReportTaskUsage(reportCtx, task.ID, entries); err != nil {
				taskLog.Warn("chat suggest usage report failed", "error", err)
			}
		}
		// Always send the supplement — an empty raw resolves the client's
		// pending placeholder with "no suggestions this turn".
		if err := d.client.SupplementTaskQuickActions(reportCtx, task.ID, raw); err != nil {
			taskLog.Warn("chat suggest supplement failed", "error", err)
		}
	}
}

// runChatSuggestPass runs one extra provider turn on the just-finished chat
// session and returns its raw text output (the JSON array, hopefully — the
// server parses leniently and treats garbage as "no suggestions"). Best-effort
// by design: every failure path returns "" and the turn proceeds without
// suggestions.
//
// The pass deliberately reuses the main turn's backend and exec options so it
// lands on the same provider, model, and workdir; only the resume pointer,
// timeouts, and prompt differ. Its transcript messages are discarded — the
// suggestion turn must not appear in the task's reported message log.
//
// Known debt: providers whose resume appends in place (rather than forking a
// new session file) keep the suggestion exchange in the session history the
// next user turn resumes. The prompt's leading line instructs the model to
// never surface it.
func (d *Daemon) runChatSuggestPass(ctx context.Context, backend agent.Backend, opts agent.ExecOptions, sessionID string, taskLog *slog.Logger) (raw string, usage map[string]agent.TokenUsage) {
	suggestCtx, cancel := context.WithTimeout(ctx, chatSuggestTimeout)
	defer cancel()

	opts.ResumeSessionID = sessionID
	opts.ResumeExpected = true
	opts.Timeout = chatSuggestTimeout
	opts.IdleWatchdogTimeout = chatSuggestTimeout

	start := time.Now()
	session, err := backend.Execute(suggestCtx, chatSuggestPrompt, opts)
	if err != nil {
		taskLog.Warn("chat suggest pass failed to start", "error", err)
		return "", nil
	}

	// Drain and discard the message stream: backends block on an undrained
	// channel, and none of these messages belong in the task transcript.
	msgDone := make(chan struct{})
	go func() {
		defer close(msgDone)
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		<-msgDone
		if result.Status != "completed" {
			taskLog.Warn("chat suggest pass did not complete",
				"status", result.Status,
				"error", result.Error,
			)
			return "", result.Usage
		}
		taskLog.Debug("chat suggest pass finished",
			"duration", time.Since(start).Round(time.Second).String(),
			"output_bytes", len(result.Output),
		)
		return strings.TrimSpace(result.Output), result.Usage
	case <-suggestCtx.Done():
		taskLog.Warn("chat suggest pass timed out or was cancelled", "timeout", chatSuggestTimeout.String())
		return "", nil
	}
}
