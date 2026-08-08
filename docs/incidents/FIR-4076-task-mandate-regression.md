# FIR-4076 Task Mandate regression

## Summary

The production release in PR #2841 included commit `5892f38dc`, the production
copy of FIR-4076 commit `40c65f95b`. It added Task Mandate and Permissions
checks to the task-token REST handlers for `add_comment` and `update_issue`, and
expanded the same enforcement helper used by `create_issue`.

The release blocked normal agent work. A comment-triggered task on FIR-4076 was
unable to reply to its own triggering comment: `POST /api/issues/{id}/comments`
returned HTTP 403 with `task_mandate_denied` for `add_comment`. The same task's
Capabilities result reported `create_issue`, `update_issue`, and `add_comment`
as denied by the runtime Task Mandate.

## Timeline

- 2026-07-30 11:36 CEST: PR #2818 merged to `main` as `40c65f95b`.
- 2026-07-31 12:02 CEST: production PR #2841 merged as `9e0640567`; its release
  branch contained the FIR-4076 change as `5892f38dc`.
- 2026-07-31 12:49 CEST: a member reported that the permission failures had
  returned.
- 2026-07-31 12:51 CEST: a reply from the task created by that comment was
  rejected with `task_mandate_denied` for `add_comment`.
- 2026-07-31: `5892f38dc` was selected for rollback without reverting the other
  changes in PR #2841.

## Root cause

The handler treated absence from the immutable Task Mandate as a hard denial
for ordinary issue writes. The mandate issued to a real comment-triggered task
did not contain `add_comment`, even though replying to the triggering comment on
the task's own issue is part of the task's normal execution contract. The new
pre-mutation check therefore denied a valid action before the existing
`AllowTaskScopeForIssue` resource boundary could admit it.

The implementation tests constructed mandates containing the actions under
test. They proved the new gate in isolation but did not reproduce the mandate
actually issued to a comment-triggered production task. That made the tests
green while the real task path was unusable.

## Rollback

Revert only `5892f38dc`. Keep the earlier FIR-4076 compatibility hotfix and its
resource-scope protections, and keep the unrelated FIR-3819 and FIR-4196
changes released in PR #2841.

The rollback regression test uses a real task token and verifies that the task
can update and comment on its own issue while the existing scope test still
rejects writes to another issue.

## Follow-up rule

Do not reintroduce Task Mandate enforcement for `add_comment` or `update_issue`
until the task-issuance path and an end-to-end comment-trigger test prove that a
real issued mandate includes every ordinary action required to complete the
task. Tests that manually seed the expected capability are not sufficient for
this contract.
