package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
)

// agentConfigDigestVersion names the input set the digest covers. It is part of
// the stored value, so a later version that folds in more inputs never compares
// equal to a v1 row: every live pair takes one cold start on that deploy, which
// is the honest reading of "the configuration that decides the prompt changed".
const agentConfigDigestVersion = "v1"

// AgentConfigDigestAmbiguous marks a task row whose earlier delivery cannot be
// identified, so the row must not vouch for the session it eventually reports.
// StartAgentTask admits whichever delivery calls it first, with no
// claim-generation check, so an earlier one can always be the one that ran.
// Two shapes reach this: a redelivery carrying a different configuration than
// the delivery already recorded, and a reclaim of a row delivered by a server
// that predates the column (where NULL means "unknown", not "never
// delivered"). It is not a digest and never equals one (real values are
// "v1:<hex>"), so the comparison below reads it as "do not resume" without
// needing a special case.
const AgentConfigDigestAmbiguous = "ambiguous:multiple-deliveries"

// AgentConfigDigest fingerprints the agent configuration a claim delivers to
// the model, so the next run on the same (agent, issue) pair can tell whether
// the session it is about to resume was built from the same configuration
// (GH #8070 / MUL-7082).
//
// v1 covers the agent instructions ONLY — the fully composed value the daemon
// receives, after the system-agent instruction layer and any squad-leader
// briefing have been folded in, not the raw agent.instructions column. That is
// the whole point of digesting the delivered payload rather than reading
// updated_at off the agent row: agent.updated_at is bumped by writes that never
// reach the prompt (avatar, visibility, max_concurrent_tasks, runtime
// rebinding), and every one of those would otherwise cold-start every
// conversation the agent owns.
//
// Skills are deliberately NOT in v1. A skill body that changed after the model
// already read it does stay stale in a resumed transcript, but that is a
// separate call with its own trade-off (every skill edit would cold-start every
// conversation), and it is tracked separately rather than folded in here.
//
// The encoding is length-prefixed rather than concatenated so no pair of
// distinct inputs can hash the same once more fields join it.
func AgentConfigDigest(instructions string) string {
	h := sha256.New()
	writeDigestField(h, agentConfigDigestVersion)
	writeDigestField(h, instructions)
	return agentConfigDigestVersion + ":" + hex.EncodeToString(h.Sum(nil))
}

// AgentConfigChanged reports whether a candidate session must not be resumed by
// a run whose delivered configuration digests to current.
//
// Anything that is not exactly this run's digest answers "do not resume", which
// covers all three ways a row fails to vouch for a session: a different
// configuration, AgentConfigDigestAmbiguous, and a row written before the
// column existed. That last one used to resume, on the reasoning that unknown
// is not stale. It is not safe: a pre-column session built under A resumes once
// under B and the resuming task then records B as that session's digest, so the
// session is labelled with a configuration it never saw and every later run
// matches it. The cost of the strict reading is one cold start per live pair on
// the deploy that ships this, and each pair records a real digest from then on.
//
// An empty current is the one case that cannot judge anything: it means the
// claim carried no agent payload at all, so there is nothing to compare and the
// resume decision is left where the earlier gates put it.
func AgentConfigChanged(prior, current string) bool {
	current = strings.TrimSpace(current)
	if current == "" {
		return false
	}
	return strings.TrimSpace(prior) != current
}

func writeDigestField(h interface{ Write([]byte) (int, error) }, field string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(field))
}
