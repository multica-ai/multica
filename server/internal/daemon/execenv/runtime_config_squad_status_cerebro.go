package execenv

import "fmt"

// CEREBRO-PATCH(squad-parent-status): MUL-5156 — align a squad leader's parent
// issue status arc with the ordinary agent-managed model. Upstream carries this
// in runtime_config_sections.go's writeWorkflowComment (step 9) and
// writeWorkflowAssignment (step 8), both gated on IsSquadLeader. The fork's
// runtime_config.go inlines those workflows into buildMetaSkillContent rather
// than the two upstream helper functions, so the leader carve-outs live here as
// small helpers the inlined workflow calls, keeping the upstream-file diff to a
// single marked call site each (the fork's cerebro-sibling extraction pattern —
// see runtime_config_no_busywait_cerebro.go).
//
// The contract, matching upstream fcb370e:
//   - Comment-triggered: the default "do not change status unless the comment
//     asks" rule contradicts the Squad Operating Protocol's "Own the parent
//     issue status" grant on the @mention-dispatch shape (no child-done system
//     comment ever carries the ask). Leaders get a named exception; ordinary
//     agents keep the absolute rule.
//   - Assignment-triggered: leaders share the opening in_progress step (already
//     emitted earlier in the workflow) but must NOT treat the first dispatch as
//     completion — no unconditional in_review on that turn. They move the parent
//     to in_review later, when a re-trigger confirms the overall goal is met.

// cerebroCommentStatusStep returns step 9 of the comment-triggered workflow.
// For a squad leader it defers to the Squad Operating Protocol's status grant
// (which only appears when the issue is assigned to this squad); otherwise the
// no-status-change rule is absolute.
func cerebroCommentStatusStep(isSquadLeader bool) string {
	if isSquadLeader {
		return "9. Do NOT change the issue status unless the comment explicitly asks for it — **or** a section in your instructions explicitly grants you ownership of this issue's status (the Squad Operating Protocol's \"Own the parent issue status\" responsibility). That section only appears when this issue is assigned to your squad; when it is there, treat it as a standing instruction and move the parent to `in_review` on the turn you confirm the overall goal is met, without waiting to be asked. When it is absent, the rule above is absolute.\n\n"
	}
	return "9. Do NOT change the issue status unless the comment explicitly asks for it\n\n"
}

// cerebroAssignmentStatusStep returns step 8 of the assignment-triggered
// workflow. A squad leader's first assignment turn is only a dispatch, so it
// must not flip the parent to in_review; an ordinary agent completes with
// in_review.
func cerebroAssignmentStatusStep(issueID string, isSquadLeader bool) string {
	if isSquadLeader {
		return fmt.Sprintf("8. After this initial dispatch, leave the parent issue `in_progress` — do NOT run `multica issue status %s in_review` or `done` on this turn. Dispatching members is not completion. You will be re-triggered when members post updates or a stage closes; only then, if the overall goal is met, move the parent to `in_review`.\n", issueID)
	}
	return fmt.Sprintf("8. When done, run `multica issue status %s in_review` unless your Agent Identity forbids issue status changes; if it does, skip this step.\n", issueID)
}
