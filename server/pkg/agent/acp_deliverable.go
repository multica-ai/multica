package agent

import (
	"strings"
	"sync"
)

// acpDeliverableTracker splits an ACP turn's text stream into the final
// user-facing answer and the complete transcript.
//
// Result.Output is "final user-facing output selected by the backend" — it
// becomes the channel reply and the auto-generated issue comment, so interim
// narration ("Let me check the logs first…") must not reach it (#6006). ACP
// runtimes emit narration and the final answer as the same AgentMessageChunk
// type, and a tool call is the only boundary they expose, so the deliverable is
// the text emitted after the latest tool call. That boundary is a heuristic
// until the runtimes mark the final answer explicitly.
//
// The full transcript stays available because provider-error detection must
// keep reading every chunk: adapters inject their terminal "API call failed
// after N retries" give-up message as an ordinary agent turn, which can land
// before a tool call and would be invisible in the deliverable alone.
//
// The zero value is ready to use. A tracker is safe for concurrent use:
// backends call observe from the ACP stdout reader goroutine while the session
// goroutine calls result.
type acpDeliverableTracker struct {
	mu sync.Mutex
	// full is every text chunk of the turn, in arrival order.
	full strings.Builder
	// deliverable is the text accumulated since the latest tool call.
	deliverable strings.Builder
	// lastTextBlock is the most recent non-empty text block a tool call
	// displaced; it is what a tool-terminated turn falls back to.
	lastTextBlock string
}

// observe records one streamed message. Backends pass every message they
// accepted for the current turn; message types other than text and tool use
// carry no deliverable signal and are ignored.
func (t *acpDeliverableTracker) observe(msg Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch msg.Type {
	case MessageText:
		t.full.WriteString(msg.Content)
		t.deliverable.WriteString(msg.Content)
	case MessageToolUse:
		if block := t.deliverable.String(); strings.TrimSpace(block) != "" {
			t.lastTextBlock = block
		}
		t.deliverable.Reset()
	}
}

// result returns the deliverable for Result.Output and the full text stream to
// scan for provider errors. A turn that ends on a tool call leaves no trailing
// text block, so the deliverable falls back to the latest non-empty one rather
// than delivering an empty reply.
func (t *acpDeliverableTracker) result() (deliverable, full string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	deliverable = t.deliverable.String()
	if strings.TrimSpace(deliverable) == "" {
		deliverable = t.lastTextBlock
	}
	return deliverable, t.full.String()
}
