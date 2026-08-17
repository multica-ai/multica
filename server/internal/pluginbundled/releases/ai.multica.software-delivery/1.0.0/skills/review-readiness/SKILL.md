---
name: review-readiness
description: Verify that a software change has enough evidence, rollout planning, and review context to be safely reviewed.
---

# Review readiness

Use this skill when preparing a software change for review or deciding whether it is ready to hand off.

## Check the change

1. State the user-visible or operational outcome in one sentence.
2. Confirm the diff is scoped to that outcome and call out intentional exclusions.
3. Record the tests or other evidence that directly exercise the changed behavior.
4. Identify data migrations, compatibility constraints, rollout steps, and rollback steps.
5. Surface unresolved risks or decisions instead of hiding them in implementation detail.

## Produce the handoff

Return a compact review brief with:

- outcome and scope;
- key architectural choices;
- evidence run and its result;
- rollout and rollback notes;
- open risks or decisions;
- exact files or interfaces where reviewers should focus.

Do not claim readiness when required evidence is missing. Say what is missing and the smallest next action that would provide it.
