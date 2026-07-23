// Package availabilityevidence records what an agent can ACTUALLY do on a
// runtime, as opposed to what configuration claims it can do.
//
// The runtime self-report answers "what does this runtime say it has?". That
// question has no truth value: a tool can be allowed by policy, listed
// in the tool matrix, and still be absent from the runtime that must execute it.
// Nothing detects the difference, so the claim and the reality drift apart
// silently. This package replaces the claim with evidence at three levels:
//
//	declared   — configuration says so. Grants nothing and proves nothing.
//	discovered — a real probe found the capability on the runtime's live surface.
//	verified   — a test call proved the gate BOTH lets the right caller through
//	             AND turns the wrong caller away.
//
// Only verified is reality. Requiring proof in both directions is deliberate: a
// capability that answers everyone is not working access control, and a
// capability that answers no one is not access at all. One proof alone cannot
// tell those two failures apart from a working gate, so neither alone earns
// verified.
//
// The package decides nothing and grants nothing. It observes, classifies, and
// reports. It holds no database or policy dependency, and it never calls a gate
// itself — the caller supplies observations.
package availabilityevidence

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Level is the strength of the evidence behind one capability on one runtime.
type Level string

const (
	// LevelDeclared means only configuration claims the capability exists.
	LevelDeclared Level = "declared"
	// LevelDiscovered means a probe found it on the runtime's live surface.
	LevelDiscovered Level = "discovered"
	// LevelVerified means a test call proved both access and refusal.
	LevelVerified Level = "verified"
)

// IsReality reports whether the level may be presented as what the agent can
// actually do. Only verified qualifies; everything else is an unproven claim.
func (l Level) IsReality() bool { return l == LevelVerified }

// RuntimeType is one of the three runtime families an agent can run on. A
// capability is evidenced per runtime, never globally: the same canonical
// capability can be real on one runtime and absent on another, and that
// difference is exactly what this package exists to expose.
type RuntimeType string

const (
	// RuntimeFirtalGateway is the in-process Firtal Gateway tool registry.
	RuntimeFirtalGateway RuntimeType = "firtal-gateway"
	// RuntimeClaudeCode is Claude Code over the Multica MCP server.
	RuntimeClaudeCode RuntimeType = "claude-code"
	// RuntimeLocal is any other local runtime over the Multica MCP server.
	RuntimeLocal RuntimeType = "local"
)

// RuntimeTypes returns every runtime family that must be evidenced, in stable
// order. A probe that does not cover all of these is incomplete by definition —
// tests assert against this list so a new runtime family cannot be added
// without forcing the coverage question.
func RuntimeTypes() []RuntimeType {
	return []RuntimeType{RuntimeFirtalGateway, RuntimeClaudeCode, RuntimeLocal}
}

// Outcome is what a gate actually did when the probe called it.
type Outcome string

const (
	// OutcomeAllowed means the gate permitted the call.
	OutcomeAllowed Outcome = "allowed"
	// OutcomeDenied means the gate refused the call.
	OutcomeDenied Outcome = "denied"
)

// Subject is the caller a probe makes a test call as. Authorized states what the
// probe expects the gate to do, so a gate that does the opposite is recorded as
// a contradiction rather than quietly counted as proof.
type Subject struct {
	Name       string
	Authorized bool
}

// Proof is one recorded test call: who called, and what the gate did.
type Proof struct {
	Subject    Subject
	Outcome    Outcome
	Detail     string
	ObservedAt time.Time
}

// Observation is the raw result of probing one capability on one runtime.
// Present says the capability was found on the live surface; Proofs are the
// test calls made against it.
type Observation struct {
	CapabilityID string
	RuntimeType  RuntimeType
	Present      bool
	Proofs       []Proof
}

// Evidence is one classified capability on one runtime: the level it earned and
// the reason it earned no more. Reason is always populated so a reader never has
// to guess why something is not verified.
type Evidence struct {
	CapabilityID string
	RuntimeType  RuntimeType
	Level        Level
	Reason       string
	Proofs       []Proof
}

// Classify turns a raw observation into evidence. It is pure and total: every
// observation yields exactly one level, and the level never exceeds what the
// proofs support.
//
// A contradicted proof (the gate allowed an unauthorized caller, or denied an
// authorized one) never counts toward verified. It is a finding in its own
// right, so it caps the capability at discovered and says so.
func Classify(o Observation) Evidence {
	e := Evidence{
		CapabilityID: o.CapabilityID,
		RuntimeType:  o.RuntimeType,
		Proofs:       append([]Proof(nil), o.Proofs...),
	}

	if !o.Present {
		e.Level = LevelDeclared
		e.Reason = "no probe found this capability on the runtime's live surface"
		return e
	}

	var access, refusal bool
	var contradictions []string
	for _, p := range o.Proofs {
		switch {
		case p.Subject.Authorized && p.Outcome == OutcomeAllowed:
			access = true
		case !p.Subject.Authorized && p.Outcome == OutcomeDenied:
			refusal = true
		case p.Subject.Authorized && p.Outcome == OutcomeDenied:
			contradictions = append(contradictions,
				fmt.Sprintf("authorized subject %q was denied", p.Subject.Name))
		case !p.Subject.Authorized && p.Outcome == OutcomeAllowed:
			contradictions = append(contradictions,
				fmt.Sprintf("unauthorized subject %q was allowed", p.Subject.Name))
		}
	}

	if len(contradictions) > 0 {
		e.Level = LevelDiscovered
		e.Reason = "the gate contradicted its policy: " + strings.Join(contradictions, "; ")
		return e
	}
	switch {
	case access && refusal:
		e.Level = LevelVerified
		e.Reason = "a test call proved both access and refusal"
	case access:
		e.Level = LevelDiscovered
		e.Reason = "access was proved but refusal was not — the gate is unproven"
	case refusal:
		e.Level = LevelDiscovered
		e.Reason = "refusal was proved but access was not — the capability is unproven"
	default:
		e.Level = LevelDiscovered
		e.Reason = "found on the runtime's surface but no test call was made"
	}
	return e
}

// Ledger collects evidence for many capabilities across many runtimes. The zero
// value is ready to use. It is not safe for concurrent writes.
type Ledger struct {
	entries map[ledgerKey]Evidence
}

type ledgerKey struct {
	capability string
	runtime    RuntimeType
}

// Record classifies an observation and stores it. A later observation for the
// same capability and runtime replaces the earlier one — a probe re-run reports
// the current truth, never an accumulation of stale claims.
func (l *Ledger) Record(o Observation) Evidence {
	e := Classify(o)
	if l.entries == nil {
		l.entries = make(map[ledgerKey]Evidence)
	}
	l.entries[ledgerKey{capability: o.CapabilityID, runtime: o.RuntimeType}] = e
	return e
}

// All returns every recorded piece of evidence, ordered by capability then
// runtime.
func (l *Ledger) All() []Evidence {
	out := make([]Evidence, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CapabilityID != out[j].CapabilityID {
			return out[i].CapabilityID < out[j].CapabilityID
		}
		return out[i].RuntimeType < out[j].RuntimeType
	})
	return out
}

// Reality returns only the evidence strong enough to present as what the agent
// can actually do. This is the single read a caller should use when answering
// "what can I do?" — everything it omits is an unproven claim.
func (l *Ledger) Reality() []Evidence {
	var out []Evidence
	for _, e := range l.All() {
		if e.Level.IsReality() {
			out = append(out, e)
		}
	}
	return out
}

// Lookup returns the evidence for one capability on one runtime. A capability
// never probed is reported as declared with an explicit reason rather than as
// missing, so an absent probe can never read as an absent capability.
func (l *Ledger) Lookup(capabilityID string, rt RuntimeType) Evidence {
	if e, ok := l.entries[ledgerKey{capability: capabilityID, runtime: rt}]; ok {
		return e
	}
	return Evidence{
		CapabilityID: capabilityID,
		RuntimeType:  rt,
		Level:        LevelDeclared,
		Reason:       "no probe has run for this capability on this runtime",
	}
}
