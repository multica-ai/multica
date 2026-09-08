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
// reach the prompt (avatar, visibility, status, max_concurrent_tasks, runtime
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

// AgentConfigChanged reports whether a candidate session recorded under prior
// must not be resumed by a run whose configuration digests to current.
//
// An empty prior is "unknown", not "different": rows written before the digest
// column existed carry no value, and a claim whose digest write lost its CAS
// carries none either. Both resume, so shipping the gate does not cold-start
// every live conversation at once — each pair starts comparing from its next
// run, once one task has recorded a digest.
func AgentConfigChanged(prior, current string) bool {
	prior = strings.TrimSpace(prior)
	current = strings.TrimSpace(current)
	if prior == "" || current == "" {
		return false
	}
	return prior != current
}

func writeDigestField(h interface{ Write([]byte) (int, error) }, field string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(field))
}
