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
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	// No isACPSessionErrorCode gate here, unlike isACPSessionNotFound, and that
	// is a deliberate narrowing of what this fix depends on. The reported
	// qodercli frame is invalid_params (-32602) — inside the accepted set — but
	// that rests on the reporter's capture rather than anything we can
	// re-derive: a qodercli without a login answers session/resume with an auth
	// error long before it ever looks the id up, so the rejection path is
	// unreachable locally. Gating on the code would make the whole fix miss its
	// own bug if a runtime picks a code outside the set, and the code buys
	// nothing at this boundary anyway: a runtime that states the session is
	// unusable has said so whatever number it attaches. The wording carries the
	// decision — which is what isACPSessionNotFound's own doc already concedes
	// about the generic -32000 and -32603.
	//
	// Message and Data only. acpRPCError.Method holds "session/resume", whose
	// own "session" would otherwise satisfy the noun half of every error this
	// RPC can produce and quietly turn the predicate into the exclusion rule
	// the doc above rejects.
	text := strings.ToLower(rpcErr.Message + " " + rpcErr.Data)
	// Delete request-shaped complaints before matching rather than returning
	// false on sight of one. A message can carry both — "invalid session
	// request: session not found" — and vetoing the whole string would throw
	// away a real rejection sitting next to a complaint about our own call.
	//
	// This runs BEFORE both wording checks, which is why the isACPSessionNotFound
	// literals are consulted through acpSessionNotFoundWording rather than by
	// calling the helper on the raw error: "unknown session" is one of those
	// literals, so "unknown session capability requested" would otherwise be
	// answered true before the scrub ever ran.
	text = acpSessionRequestComplaintRe.ReplaceAllString(text, " ")
	return acpSessionNotFoundWording(text) || acpSessionUnusableRe.MatchString(text)
}

// acpSessionUnusableRe matches a runtime saying the session id itself is no
// good. Two shapes, because that is how the wordings actually read:
//
//	Invalid session identifier "27d8031c-…"      (qodercli — verdict, then noun)
//	unknown session <id>                         (Reasonix — verdict, then noun)
//	Session not found                            (Hermes — noun, then verdict)
//	session ses_abc does not exist               (noun, id, then verdict)
//	No session found with id ses_abc             (Kiro — the "no <noun> found" shape)
//
// The verdict-first form demands the noun IMMEDIATELY after the verdict, and the
// noun-first form allows only an "id"/"identifier" word and one id-shaped token
// in between. That tightness is the whole safety argument, and a looser window
// is not a detail: the natural English for errors this RPC really does produce
// puts the same two halves a few words apart while meaning something entirely
// different — "unknown error while loading session", "invalid token for session
// abc", "invalid credentials for this session", "cwd does not exist for session
// abc". Those are auth and infrastructure failures, which Result.ResumeRejected
// is explicitly documented never to flag, and matching one does not merely waste
// a retry: it retires a live conversation's pointer and forks it irreversibly.
// TestIsACPResumeRejected pins every one of them as a negative.
//
// The "no <noun> found" alternative is deliberately spelled out here even
// though isACPSessionNotFound already matches that literal: that helper keeps
// its error-code gate, so relying on it for this shape would quietly reinstate
// the code dependency this predicate exists without. Kiro's real frame is
// -32603 and would pass the gate anyway; the point is that the wording alone
// decides, for every shape and not just the ones in the regex.
var acpSessionUnusableRe = regexp.MustCompile(
	`(no such|unknown|invalid|expired|unrecogni[sz]ed|nonexistent|missing)\s+(session|conversation|thread)` +
		`|no\s+(session|conversation|thread)\s+found` +
		`|(session|conversation|thread)(\s+(id|identifier))?(\s+"?[\w-]+"?)?\s+(is\s+)?(not found|does not exist|doesn't exist|no longer exists|expired|invalid|unknown|unrecogni[sz]ed)`)

// acpSessionRequestComplaintRe matches the one verdict-first shape that is NOT
// about the recorded session: a generic noun after it turns the phrase into a
// complaint about the call we just made. "Invalid session parameters: cwd must
// be absolute", "invalid session request" and "unknown session capability
// requested" all say our request was malformed, not that the transcript is
// gone — and a fresh session would fail identically, after the pointer had
// already been retired. Reaching one of these needs a client-side bug first,
// which is why it is a stop-list rather than a redesign.
var acpSessionRequestComplaintRe = regexp.MustCompile(
	`(no such|unknown|invalid|expired|unrecogni[sz]ed|nonexistent|missing)\s+(session|conversation|thread)\s+(params?|parameters?|request|requests|config|configuration|option|options|capability|capabilities|mode|setup)`)

// setupFailureWithholdsSessionID reports whether a run that failed during setup
// — after session/new, before session/prompt — must report an empty SessionID
// instead of the id it just created.
//
// True exactly when the session is fresh. Such a session has no transcript
// behind it: no prompt was ever sent, so there is no conversation to continue
// and withholding the id costs one extra session/new next turn. Publishing it
// bets that every ACP runtime persists a never-prompted session, and qodercli
// does not — it exits without writing one, leaving the daemon pinned to an id
// that does not exist and every later message in that chat failing to resume it
// forever (GH #8116).
//
// A RESUMED session is the opposite case and keeps today's behaviour: its
// transcript predates this run, so the id stays unless the runtime actively
// rejected it.
func setupFailureWithholdsSessionID(opts ExecOptions) bool {
	return opts.ResumeSessionID == ""
}

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
