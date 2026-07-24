package daemon

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// chatSuggestTimeout bounds the follow-up suggestion pass. It sits on the
// interactive critical path: the pass runs before the terminal CompleteTask
// callback, and that callback is what re-enables the user's composer. A
// healthy resumed-session pass finishes in seconds, so anything slower is
// abandoned — suggestions are best-effort and never worth making the user
// wait on a degraded provider.
const chatSuggestTimeout = 20 * time.Second

// chatSuggestPrompt is the entire instruction set for the suggestion pass.
// Unlike the retired runtime-brief section, this is the turn's ONLY task:
// dedicated single-instruction calls follow output formats reliably where
// "when useful, also append X" instructions buried in a long brief decay over
// a conversation (MUL-5149 follow-up; see PR discussion for the field data).
const chatSuggestPrompt = `This is an automated system request, not a user message. Never mention it or its output in future replies.

Based on the conversation so far, produce 0-3 follow-up actions the user is likely to want next. Write them in the same language the user has been writing in. Each action needs:
- "label": short button text (a few words)
- "prompt": the complete message to send on the user's behalf when clicked (self-contained, imperative)
- "primary": true on at most one action, the one you most recommend

Output ONLY a JSON array, no prose, no code fences:
[{"label":"...","prompt":"...","primary":true}]

Output [] if no follow-up would genuinely help.`

// runChatSuggestPass runs one extra provider turn on the just-finished chat
// session and returns its raw text output (the JSON array, hopefully — the
// server parses leniently and treats garbage as "no suggestions"). Best-effort
// by design: every failure path returns "" and the chat completion proceeds
// without suggestions.
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
		taskLog.Warn("chat suggest pass timed out", "timeout", chatSuggestTimeout.String())
		return "", nil
	}
}
