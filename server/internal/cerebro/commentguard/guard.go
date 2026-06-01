// Package commentguard owns Cerebro's "no agent comment without a target"
// guardrail (FIR-2674).
//
// Agents used to post comments with no addressee, so a reader could not tell
// who a comment was for. This guard rejects an agent-authored comment that
// references no target at all. A "target" is any mention: a member, an agent,
// a squad, or an issue. An issue link (mention://issue/...) counts and has no
// side effect, so an agent can always satisfy the rule without waking another
// agent — only an agent mention triggers a run, so the guard never forces a
// loop. Member-authored comments are never gated.
package commentguard

import (
	"github.com/multica-ai/multica/server/internal/util"
)

// MissingTargetMessage is returned to the caller (e.g. the CLI an agent posts
// through) when a comment is rejected, so the agent can re-post with a target.
const MissingTargetMessage = "comment must mention a target — a person, an agent, or an issue (e.g. MUL-123). Comments with no target are not allowed (FIR-2674)."

// Service is the Cerebro comment-target guard. It is stateless; the seam still
// uses a struct so the wiring mirrors the other cerebro handler seams and so a
// nil Service is a safe no-op (guard disabled).
type Service struct{}

// New returns a ready guard.
func New() *Service { return &Service{} }

// RejectComment reports whether a comment must be rejected before it is stored.
// It returns (message, ok): ok=true means the comment passes; ok=false means it
// must be rejected and message explains why.
//
// content must already have had bare issue identifiers (e.g. "MUL-123")
// expanded into mention links, so plain issue references count as a target.
func (s *Service) RejectComment(authorType, content string) (string, bool) {
	// Only agent-authored comments are gated. Members may comment freely.
	if s == nil || authorType != "agent" {
		return "", true
	}
	if len(util.ParseMentions(content)) == 0 {
		return MissingTargetMessage, false
	}
	return "", true
}
