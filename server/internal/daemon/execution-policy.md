# NEX-1565 admission foundation

This change is not an H3 mitigation on its own. The production server does not
emit `execution_policy`. Do not deploy it as a security fix or advertise isolated
execution capability based on this change.

An authenticated claim may carry `execution_policy: "restricted"`. The daemon
rejects it before repo preparation, credential injection, provider discovery,
or environment setup. The same check runs immediately before backend Execute,
including resume fallback. No provider is treated as isolated. Unknown strings
and the empty string are rejected; omitted and null policies preserve existing
claims. `unrestricted` is a server decision, not proof of owner authorization.
Neither prompts nor metadata are inspected to infer permission.

The sink tests deliberately use a backend that writes an ephemeral marker when
started, regardless of the attack prompt. Restricted claims must make zero
Execute calls and leave no marker across repeated attempts, even with a resume
ID and permissive custom arguments. Unrestricted and legacy claims run normally.
This is admission testing, not an OS isolation or authenticated owner-action test.

## Required before activation

- Persist immutable origin on relay messages, sessions, issues and task lineage.
  Derive it from an authenticated dedicated relay principal; never accept an
  owner_direct string as authority. Mixed external context stays restricted.
- Existing task-scoped `mat_` credentials already exist in this source. Extend
  their authorization using server task origin, including issue/comment/attachment
  writes and downstream reads. Parent lineage alone cannot prevent persisted
  content from being read by an unrestricted task. Shared filesystem access also
  requires isolation, not just API provenance.
- Gate server claim, retry, rerun and reassignment. Withhold restricted tasks from
  every daemon until the server can attest support. A self-reported capability
  flag alone is insufficient. The legacy nil branch must never be used for relay
  or unknown-origin tasks. Old daemons ignore the new field entirely.
- Connect AIDO's dedicated structured relay input and rotate to fresh restricted
  sessions. Stop and reconcile queued/running legacy relay tasks before cutover.
  Do not resume their old native sessions or reuse writable workdirs as trusted.
- Verify authenticated owner instructions on a clean path, plus existing issue
  execution and retries, against a temporary DB and mock action sink. The local
  unrestricted test is not evidence of authenticated owner authorization.
- Provide a separate exact-payload, expiring, single-use owner action executor
  if restricted tasks must perform actions. Until then restricted tasks cannot
  execute any backend. OS isolation tests are a separate release gate.

No daemon restart or binary replacement is part of this patch. Before rollout,
record the target SHA/runtime/PID, affected queue, in-flight cancellation policy,
and recovery procedure in the issue. Recovery parks restricted work; it must not
route restricted claims to an older unrestricted daemon.
