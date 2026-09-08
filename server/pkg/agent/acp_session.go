package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// isACPResumeRejected reports whether an error returned by session/resume or
// session/load means the runtime refused the session id we asked it to restore.
//
// It is deliberately wider than isACPSessionNotFound, and deliberately scoped
// to that one RPC. session/resume and session/load have exactly one job — turn
// a recorded id into a live session — so a rejection there is about the id
// itself far more often than at set_model or prompt time, where the same words
// could be about the model, the provider or the network. Calling this outside
// the resume boundary would lose that context and start discarding healthy
// conversation pointers.
//
// It stays positive evidence, not exclusion. Treating EVERY resume error as a
// rejection is the tempting alternative and it is wrong: session/resume carries
// mcpServers, so an unreachable MCP server fails it, and so do a runtime
// handshake crash, an expired provider credential and a missing cwd. None of
// those is cured by starting over, and shouldRetryWithFreshSession's contract
// is that a false answer means "checked, and this was not a rejection". What
// this widens is the vocabulary, not the burden of proof: instead of three
// literal phrases it accepts any wording that names a session-shaped noun and
// says, right next to it, that the thing is unusable. That is what GH #8116
// slipped through — qodercli answers "Invalid session identifier <id>", which
// no literal in the original set matched, so a conversation pinned to a dead
// id retried it forever.
func isACPResumeRejected(err error) bool {
	if isACPSessionNotFound(err) {
		return true
	}
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if !isACPSessionErrorCode(rpcErr.Code) {
		return false
	}
	// Message and Data only. acpRPCError.Method holds "session/resume", whose
	// own "session" would otherwise satisfy the noun half of every error this
	// RPC can produce and quietly turn the predicate into the exclusion rule
	// the doc above rejects.
	return acpSessionUnusableRe.MatchString(strings.ToLower(rpcErr.Message + " " + rpcErr.Data))
}

// acpSessionUnusableRe matches a session-shaped noun and an "unusable" verdict
// standing next to each other, in either order — the shape every runtime's
// rejection wording has taken so far:
//
//	Invalid session identifier "27d8031c-…"   (qodercli)
//	Session not found                         (Hermes)
//	unknown session <id>                      (Reasonix)
//	No session found with id …                (Kiro, via isACPSessionNotFound)
//
// Adjacency is what keeps it narrow, and it is load-bearing. Both halves appear
// independently in errors that have nothing to do with the session — an invalid
// mcpServers entry says "Invalid params" while the error's `data` echoes the
// sessionId we passed — and a bare AND over the whole string would match that
// pair and discard a healthy pointer. Requiring them within one short,
// separator-free window is what tells "invalid session identifier" apart from
// "invalid params … {sessionId: …}".
var acpSessionUnusableRe = regexp.MustCompile(
	`(session|conversation|thread)[^,;\n]{0,24}(not found|no such|unknown|invalid|expired|does not exist|doesn't exist|no longer|unrecogni[sz]ed)` +
		`|(not found|no such|unknown|invalid|expired|does not exist|doesn't exist|no longer|unrecogni[sz]ed)[^,;\n]{0,24}(session|conversation|thread)`)

// classifyACPResumeFailure turns an error from session/resume or session/load
// into this turn's terminal status, message and resume-rejection flag. Every
// ACP backend that resumes shares it so the six that used to return a bare
// failed Result cannot drift apart from each other again.
//
// Cancellation and timeout are checked FIRST, and that ordering is deliberate.
// A user who cancels mid-resume, or a turn that runs out of budget, must never
// be reported as a rejected session: ResumeRejected licenses the daemon to
// abandon the recorded conversation and re-run the whole task from a fresh one,
// which is the opposite of what a cancel asked for. When a real rejection and a
// cancel race, reading it as a cancel is the recoverable mistake — the pointer
// survives, and the next turn resumes, gets the same rejection with no cancel
// in flight, and self-heals then.
func classifyACPResumeFailure(runCtx context.Context, backend, rpc string, err error, timeout time.Duration, logger *slog.Logger) (status, errText string, rejected bool) {
	switch runCtx.Err() {
	case context.DeadlineExceeded:
		if timeout > 0 {
			return "timeout", fmt.Sprintf("%s timed out after %s during %s", backend, timeout, rpc), false
		}
		return "timeout", fmt.Sprintf("%s timed out during %s", backend, rpc), false
	case context.Canceled:
		return "aborted", "execution cancelled", false
	}
	errText = fmt.Sprintf("%s %s failed: %v", backend, rpc, err)
	if !isACPResumeRejected(err) {
		return "failed", errText, false
	}
	if logger != nil {
		logger.Warn("resumed session rejected by the runtime; the daemon will retry from a fresh session",
			"backend", backend,
			"rpc", rpc,
			"error", err,
		)
	}
	return "failed", errText, true
}
