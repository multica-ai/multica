package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

// claudeUsageProbeRequestID is this run's id for the one `get_usage` control
// request the probe sends. Scoped to a single CLI process, so a constant is
// enough to tell our answer from the initializer's.
const claudeUsageProbeRequestID = "multica-context-window"

// claudeUsageProbe asks the CLI for the context window it is actually using.
//
// The window reaches no stream event: the init message names the model and no
// window, the result's usage carries none, and the `[1m]` modifier that would
// imply one is stripped before the CLI reports a model anywhere. The control
// protocol does carry it — `get_usage` answers with a per-model record whose
// `contextWindow` is the number the CLI renders its own percentage from — so
// the probe asks once per run rather than deriving a window from a model id.
//
// `detached` and `skip_behaviors` keep the question out of the turn's way: the
// answer is produced from the session totals when it is sent and never queues
// behind the prompt. A CLI too old for either flag ignores them, and one too
// old for the request answers with an error, which reads here as "no window".
//
// Asking once was not enough. The answer carries a window only once the
// session has a per-model usage record, and a run can reach its first
// assistant event before the record exists — a one-shot ask then gets an
// empty answer and the run keeps a derived window for good. So the probe
// keeps asking on later responses until one answer carries a window, bounded
// so a CLI that never answers costs a handful of frames rather than one per
// response.
//
// Driven entirely by the stream loop — `ask` and `windowFrom` are called from
// that one goroutine — so the counters need no lock.
type claudeUsageProbe struct {
	asks     int
	answered bool
	replied  bool
}

// claudeUsageProbeMaxAsks bounds the retries. Three covers "the first
// response had no usage record yet" without turning a silent CLI into a
// frame per response.
const claudeUsageProbeMaxAsks = 3

func newClaudeUsageProbe() *claudeUsageProbe {
	return &claudeUsageProbe{}
}

// answeredWindow reports whether an answer has already carried a window, so
// the caller knows the window will not improve by waiting.
func (p *claudeUsageProbe) answeredWindow() bool { return p.answered }

// sawReply reports whether the CLI answered this probe at all, window or not.
// A CLI too old for the request answers with an error, which is still an
// answer: waiting any longer for a window would only delay the run.
func (p *claudeUsageProbe) sawReply() bool { return p.replied }

// spent reports whether the retries are used up with no window — the caller
// then stops holding measurements for an answer that is not coming.
func (p *claudeUsageProbe) spent() bool { return !p.answered && p.asks >= claudeUsageProbeMaxAsks }

// ask sends the request unless a window has already been answered or the
// retries are spent.
func (p *claudeUsageProbe) ask(w io.Writer, logger *slog.Logger) {
	if p.answered || p.asks >= claudeUsageProbeMaxAsks {
		return
	}
	p.asks++
	if _, err := io.WriteString(w, claudeUsageProbeRequest()+"\n"); err != nil && logger != nil {
		// The window is display metadata: a run must not fail because the
		// CLI would not take the question.
		logger.Debug("claude context window request not sent", "error", err)
	}
}

// windowFrom reads the window out of this probe's answer, or 0 for any other
// control response, an error answer, or a session with no per-model record yet.
func (p *claudeUsageProbe) windowFrom(msg claudeSDKMessage) int64 {
	if len(msg.Response) == 0 {
		return 0
	}
	var envelope struct {
		Subtype   string `json:"subtype"`
		RequestID string `json:"request_id"`
		Response  struct {
			Session struct {
				ModelUsage map[string]struct {
					ContextWindow int64 `json:"contextWindow"`
				} `json:"model_usage"`
			} `json:"session"`
		} `json:"response"`
	}
	if err := json.Unmarshal(msg.Response, &envelope); err != nil {
		return 0
	}
	if envelope.RequestID != claudeUsageProbeRequestID {
		return 0
	}
	p.replied = true
	if envelope.Subtype != "success" {
		return 0
	}
	// One entry per model the session used. They share a window in practice,
	// and when they do not the largest is the only one every prompt so far
	// fits in — a smaller one would read as over-full.
	var window int64
	for _, record := range envelope.Response.Session.ModelUsage {
		if record.ContextWindow > window {
			window = record.ContextWindow
		}
	}
	if window > 0 {
		p.answered = true
	}
	return window
}

// claudeUsageProbeRequest is the exact frame the probe writes — the one place
// the wire shape is written down, so a reader sees the protocol without
// running it.
func claudeUsageProbeRequest() string {
	return fmt.Sprintf(
		`{"type":"control_request","request_id":%q,"request":{"subtype":"get_usage","detached":true,"skip_behaviors":true}}`,
		claudeUsageProbeRequestID,
	)
}
