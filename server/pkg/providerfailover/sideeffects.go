package providerfailover

// SideEffects is a snapshot of everything observable that a failed run may have
// already done, gathered at decision time. It is the "side-effect ledger
// checked before fallback": a cross-provider re-run reuses the same workdir and
// re-fires @mention triggers, so any evidence that the primary already acted
// makes a handoff unsafe.
//
// This is stricter than the same-provider fresh-session retry (which gates only
// on observed tool calls): a GPT→Claude handoff cannot inherit the primary's
// session, so it re-plans from scratch on top of whatever the primary left
// behind. Fail-closed — any single positive signal blocks the handoff.
//
// IMPORTANT: presence of a signal here is authoritative (any true value blocks a
// handoff), but ABSENCE is NOT proof of a clean run. Only DeliveredCommentIDs
// and AgentCommented are reliably reconstructable from server-persisted state.
// ObservedToolCalls, HeadSHAAdvanced, and PartialOutput are daemon-side signals
// that are not plumbed to the server fail path today, so they read as
// zero/false even when the run did act. That incompleteness is why active-mode
// handoffs additionally require Input.SideEffectsComplete (see policy.go): a
// missing signal must never be mistaken for a proven-empty surface.
type SideEffects struct {
	// ObservedToolCalls is the number of tool uses the daemon observed during
	// the failed run. >0 means the run may have mutated the workdir, posted a
	// comment, or pushed code. NOT currently plumbed to the server fail path —
	// gated behind SideEffectsComplete rather than trusted as 0.
	ObservedToolCalls int
	// DeliveredCommentIDs counts comments the run committed to delivering
	// (agent_task_queue.delivered_comment_ids). Server-persisted and reliable.
	// A duplicate delivery has no idempotency key.
	DeliveredCommentIDs int
	// AgentCommented is true when the agent posted at least one comment on the
	// issue during the run (HasAgentCommentedSince). Server-persisted, reliable.
	AgentCommented bool
	// HeadSHAAdvanced is true when commits/PR activity moved the reviewed head
	// SHA during the run — i.e. the run wrote code. NOT currently plumbed to the
	// server fail path — gated behind SideEffectsComplete.
	HeadSHAAdvanced bool
	// PartialOutput is true when the run produced partial user-facing output
	// before failing. Re-running would risk a contradictory second answer. NOT
	// currently plumbed to the server fail path — gated behind SideEffectsComplete.
	PartialOutput bool
}

// HasObservableSideEffects reports whether the primary run left any evidence of
// action in the fields we can observe. When true, a handoff is declined; when
// false, the surface is only PROVEN clean if Input.SideEffectsComplete is set.
func (s SideEffects) HasObservableSideEffects() bool {
	return s.ObservedToolCalls > 0 ||
		s.DeliveredCommentIDs > 0 ||
		s.AgentCommented ||
		s.HeadSHAAdvanced ||
		s.PartialOutput
}

// SideEffectEvidence is the daemon-observed side-effect evidence for a failed
// provider run, carried on the fail callback (TaskFailRequest.FailoverEvidence)
// so the server can decide whether a cross-provider failover is safe. It is the
// wire object that closes the gap sideeffects.go documents: the daemon-side
// signals the server cannot reconstruct after the fact.
//
// It exists because absence of a side effect is only trustworthy when the
// daemon actually observed the whole run. ObservedToolCalls and
// PartialUserOutput read as zero/false both on a genuinely clean run and on a
// run the server never saw; Complete is the daemon's explicit assertion "I
// drained this run to its terminal end and these counts are the full picture."
// Old daemons omit the object entirely (nil on the server), so completeness is
// never claimed and active failover stays fail-closed
// (ReasonSideEffectsUnproven).
//
// The proof boundary is deliberately coarse: ANY tool call is treated as a
// possible worktree/head mutation. Every agent mutation — file writes, shell
// commands, git operations, comment posts, code pushes — happens THROUGH an
// observed tool call, so a complete evidence object with ObservedToolCalls == 0
// and no partial user output is a sound proof that the run changed nothing,
// INCLUDING the reviewed head SHA. That is why the server needs no separate
// head-movement signal: a non-zero tool count already blocks the handoff, and a
// zero count already covers head changes.
type SideEffectEvidence struct {
	// ObservedToolCalls is the full count of tool_use messages the daemon
	// observed during the run. >0 blocks the handoff (see HasObservableSideEffects).
	ObservedToolCalls int `json:"observed_tool_calls"`
	// PartialUserOutput is true when the run streamed any user-facing text
	// before failing. Re-running would risk a contradictory second answer.
	PartialUserOutput bool `json:"partial_user_output"`
	// Complete asserts the daemon observed the run to its terminal end, so the
	// two fields above are the complete side-effect surface. Only when this is
	// true (and the surface is empty) may active mode hand off.
	Complete bool `json:"complete"`
}
