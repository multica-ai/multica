# GOV-12 Reply Admission Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the server-owned Reply Admission Gateway so compliant Markdown replies are admitted, terminal task completion cannot be wedged by a rejected synthesized reply, required requester delivery cannot be suppressed, nested agent threads terminate safely, and idempotency recovery cannot replay paid side effects forever.

**Architecture:** Keep admission decisions server-derived and fail closed for malformed or missing evidence. Make terminal task status independent of optional synthesized-comment delivery, preserve the required requester trigger when admission depended on it, and treat a deterministic permanent attachment mismatch as a durable dead-letter completion rather than replaying every downstream effect.

**Tech Stack:** Go, PostgreSQL, sqlc-generated queries, `go test`, `go vet`.

**Spec:** COM-104 and Claude’s `CHANGES_REQUIRED` review on commit `863e38fdf2da47fdb99584f371518343e364362d`.

## Global Constraints

- Preserve the server-side boundary for `Handler.CreateComment`, `Handler.UpdateComment`, and `TaskService.createAgentComment`.
- Keep direct API/CLI bypass protection and the existing issue-then-parent lock order.
- Never accept a caller-asserted admission classification, delivery receipt, or trigger outcome.
- Terminal task status must reach `completed` even when an optional fallback comment is rejected by reply admission.
- A requester mention required by admission must not be removable by `suppress_agent_ids`.
- Unpinned inventory/idempotency behavior and unrelated daemon configuration failures are outside this revision.

### Task 1: Repair Markdown inline-span scanning

**Files:**
- Modify: `server/internal/replyadmission/reply_admission_test.go`
- Modify: `server/internal/replyadmission/reply_admission.go`

**Interfaces:**
- `Check(Parent, string) error` continues to be the public admission entry point.
- `stripCodeSpans` continues to blank balanced inline/fenced code while preserving surrounding prose.

- [ ] **Step 1: Write the failing regression tests**

Add one test where a substantive reply contains a balanced inline code reference before a real requester mention; it must be admitted. Add one test where an unbalanced backtick appears before a real requester mention; the malformed delimiter is treated as literal text and the mention remains visible. Keep the existing closed code-formatted mention case rejected.

- [ ] **Step 2: Run the focused tests to verify RED**

Run: `go test ./internal/replyadmission -run 'TestCheck(AllowsMentionAfterInlineCode|TreatsUnbalancedInlineCodeAsLiteral|IgnoresCodeFormattedMention)$' -count=1`

Expected: the new balanced-inline and unbalanced-delimiter cases fail because the current scanner never closes `inInline` and blanks the tail.

- [ ] **Step 3: Implement the minimal scanner fix**

Close `inInline` when the current backtick is the closing delimiter. Only enter inline-code mode when a later backtick exists; otherwise emit the backtick as ordinary content. Leave fenced-code and indented-code handling unchanged.

- [ ] **Step 4: Run the focused tests to verify GREEN**

Run: `go test ./internal/replyadmission -run 'TestCheck(AllowsMentionAfterInlineCode|TreatsUnbalancedInlineCodeAsLiteral|IgnoresCodeFormattedMention)$' -count=1`

Expected: all three cases pass.

### Task 2: Decouple terminal completion from rejected fallback delivery

**Files:**
- Modify: `server/internal/service/reply_admission_test.go`
- Modify: `server/internal/service/task.go`

**Interfaces:**
- `prepareCompletionFallbackAdmission` retains its typed reply-admission rejection.
- `CompleteTask` returns the completed task and nil error after a policy-only fallback rejection, while logging the rejected fallback and preserving the task result.

- [ ] **Step 1: Change the existing regression test first**

Rename the current rejection test to describe terminal completion, assert `CompleteTask` returns nil error, assert the task status is `completed`, and continue asserting that no rejected agent fallback comment was persisted.

- [ ] **Step 2: Run the service regression to verify RED**

Run: `go test ./internal/service -run 'TestCompleteTask_(CompletionFallbackAdmissionRejectionCompletesTask|AllowsMentionedOpinionFallback)$' -count=1`

Expected: the rejection case fails because `CompleteTask` currently returns the admission error and rolls the status change back to `running`.

- [ ] **Step 3: Implement the terminal-safe handling**

Inside the completion transaction, classify only `MissingRequesterMentionError` (through wrapped errors) as a non-fatal fallback-policy rejection; retain rollback for database, lock, or other infrastructure errors. Commit the task status without inserting the fallback, then emit a structured warning after commit containing the task, issue, requester, and admission reason.

- [ ] **Step 4: Run the service regression to verify GREEN**

Run: `go test ./internal/service -run 'TestCompleteTask_(CompletionFallbackAdmissionRejectionCompletesTask|AllowsMentionedOpinionFallback)$' -count=1`

Expected: both tests pass and the rejected fallback leaves zero agent-authored fallback comments.

### Task 3: Preserve required requester delivery and terminate nested agent replies

**Files:**
- Modify: `server/internal/replyadmission/reply_admission.go`
- Modify: `server/internal/replyadmission/reply_admission_test.go`
- Modify: `server/internal/service/reply_admission.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/handler/comment.go`
- Modify: `server/internal/handler/reply_admission_server_test.go`

**Interfaces:**
- Extend the server-derived `replyadmission.Parent` context with whether the direct parent is itself a reply.
- `Evaluate` exempts every response whose direct parent is itself a reply, preserving a terminating move for nested agent threads.
- `triggerTasksForComment` protects the parent requester when the admitted reply required that requester mention, ignoring only that matching suppression entry.

- [ ] **Step 1: Add failing tests**

Add a unit test proving a five-word nested acknowledgement such as `Understood, no further action needed` is admitted when the direct parent is an agent-authored reply. Update the HTTP positive admission test to decode `trigger_outcomes` and assert the required requester has a delivered non-blocked outcome even when `suppress_agent_ids` includes that requester.

- [ ] **Step 2: Run the focused tests to verify RED**

Run: `go test ./internal/replyadmission -run 'TestCheckNestedReply' -count=1` and `go test ./internal/handler -run 'TestCreateComment_AgentOpinionReplyWithRequesterMentionIsAccepted' -count=1`

Expected: the nested reply is rejected and the suppressed requester has no delivered trigger outcome under the current implementation.

- [ ] **Step 3: Implement the server-derived context and suppression protection**

Populate the new parent-reply field at every production admission call site from the stored `ParentID`. In post-commit trigger routing, evaluate the stored parent/comment pair and remove only the required requester ID from the suppression set before filtering; all other suppressed targets remain suppressed.

- [ ] **Step 4: Run the focused tests to verify GREEN**

Run: `go test ./internal/replyadmission -run 'TestCheck(NestedReply|AllowsMentionAfterInlineCode|TreatsUnbalancedInlineCodeAsLiteral|IgnoresCodeFormattedMention)$' -count=1` and `go test ./internal/handler -run 'TestCreateComment_AgentOpinionReplyWithRequesterMentionIsAccepted' -count=1`

Expected: nested replies terminate without a tag-back loop, required requester delivery survives suppression, and ordinary suppression tests remain green.

### Task 4: Dead-letter permanent idempotency attachment failures

**Files:**
- Modify: `server/internal/handler/comment.go`
- Modify: `server/internal/handler/comment_idempotency_recovery.go`
- Modify: `server/internal/handler/comment_idempotency_recovery_test.go`
- Modify: `server/internal/handler/reply_admission_server_test.go`

**Interfaces:**
- `runCommentPostCommitSideEffects` returns both `complete` and a terminal `deadLettered` result.
- A deterministic attachment-count mismatch is terminal only when all non-attachment side effects completed; transient query and downstream errors remain retryable. The terminal decision is logged as a dead-letter and uses the existing completion marker to exclude future sweeper retries.
- Create, replay, and sweeper paths mark the idempotency row complete when either all effects complete or the attachment request is durably dead-lettered.

- [ ] **Step 1: Write the recovery replay regression test**

Create an idempotent comment mentioning one test agent and referencing a nonexistent attachment UUID. Reconcile once, complete the queued trigger task, age any claim if needed, reconcile again, then assert exactly one trigger task and a non-null `side_effects_completed_at` marker.

- [ ] **Step 2: Run the recovery regression to verify RED**

Run: `go test ./internal/handler -run 'TestReconcilePendingCommentIdempotencyDeadLettersPermanentAttachmentMismatch$' -count=1`

Expected: the current implementation leaves the marker NULL and creates a second trigger after the second recovery pass.

- [ ] **Step 3: Implement the bounded dead-letter result**

Track the permanent attachment mismatch separately from non-attachment failures, return `deadLettered=true` only when the latter are clear, log the terminal reason, and let all three idempotency call sites mark the row complete without rerunning it.

- [ ] **Step 4: Run recovery and existing idempotency tests**

Run: `go test ./internal/handler -run 'Test(ReconcilePendingCommentIdempotency|CreateComment_IdempotencyKey)' -count=1`

Expected: normal recovery still completes, concurrent/replayed creates remain single-row, and permanent attachment mismatch does not replay triggers.

### Task 5: Full verification and review handoff

**Files:**
- Modify: none beyond Tasks 1–4.

- [ ] **Step 1: Format and inspect the diff**

Run: `gofmt -w server/internal/replyadmission/reply_admission.go server/internal/replyadmission/reply_admission_test.go server/internal/service/reply_admission.go server/internal/service/reply_admission_test.go server/internal/service/task.go server/internal/handler/comment.go server/internal/handler/reply_admission_server_test.go server/internal/handler/comment_idempotency_recovery.go server/internal/handler/comment_idempotency_recovery_test.go` and `git diff --check`.

- [ ] **Step 2: Run the complete affected suite**

Run: `go test ./internal/replyadmission ./internal/service ./internal/handler ./cmd/server` and `go vet ./internal/replyadmission ./internal/service ./internal/handler ./cmd/server`.

Expected: exit 0 for all affected packages; unrelated daemon configuration leakage remains separately reported if reproduced.

- [ ] **Step 3: Verify the exact candidate**

Run: `git rev-parse HEAD` and `git diff --stat`, then record the new commit SHA and the fresh test results in the COM-104 review thread for Claude’s complete re-review.

## Review Revision: 2026-09-03

Claude’s re-review of `8a0b02524f8ca1b9edd8b175a615be4a61f2676e` found three remaining correctness gaps. This revision preserves the server boundary while narrowing the loop escape, making Markdown masking deterministic, and keeping a visible issue record when optional fallback delivery is rejected.

### Task 6: Narrow nested reply termination without disabling review admission

**Files:**
- Modify: `server/internal/replyadmission/reply_admission.go`
- Modify: `server/internal/replyadmission/reply_admission_test.go`

Allow a direct reply to an agent reply to terminate only when the response is short and introduces no new request. A longer substantive response and a new opinion/review request must continue through the requester-mention gate.

- [x] **Step 1: Write failing regression tests**

Add tests proving that a bounded five-word no-new-request termination is admitted, while a 900-word nested review and a nested new opinion request still return `MissingRequesterMentionError`.

- [x] **Step 2: Run the nested tests to verify RED**

Run: `go test ./internal/replyadmission -run 'TestCheckNested' -count=1`

Expected: the current unconditional `IsReply` return admits the long review and new opinion request, so the new tests fail.

- [x] **Step 3: Implement the bounded termination predicate**

Use a fixed word bound and reject responses containing a new request marker before returning the acknowledgement classification. Preserve normal root admission and all existing exact acknowledgement behavior.

- [x] **Step 4: Run the nested tests to verify GREEN**

Run: `go test ./internal/replyadmission -run 'TestCheckNested' -count=1`

Expected: only the bounded no-new-request termination is exempt; long nested reviews and nested opinion requests still require the parent requester mention.

### Task 7: Pair equal-length Markdown code delimiters

**Files:**
- Modify: `server/internal/replyadmission/reply_admission.go`
- Modify: `server/internal/replyadmission/reply_admission_test.go`

Replace the look-ahead parity heuristic with one-pass equal-length backtick-run pairing. A run closes only a code span opened with the same run length; an unclosed opening run is restored as literal text so mentions after it remain visible. Keep fenced and indented code behavior unchanged.

- [x] **Step 1: Write failing parity regression tests**

Add one test where a stray single backtick precedes a real mention and a later legitimate code reference; the mention must remain admitted. Add one test where the same stray tick precedes a deliberately code-formatted mention; that mention must remain rejected.

- [x] **Step 2: Run the parser tests to verify RED**

Run: `go test ./internal/replyadmission -run 'TestCheck(CodeSpan|AllowsMentionAfterInlineCode|TreatsUnbalancedInlineCodeAsLiteral|IgnoresCodeFormattedMention)' -count=1`

Expected: the two stray-tick tests fail under the current any-later-backtick heuristic.

- [x] **Step 3: Implement equal-run pairing**

Scan backtick runs as delimiters, pair only equal-length runs, blank only the paired span contents, and copy unmatched runs and their following text unchanged. Continue to recognize fenced code before inline spans.

- [x] **Step 4: Run the parser tests to verify GREEN**

Run the same focused parser command and confirm balanced, unbalanced, stray-tick, fenced, indented, and code-formatted mention cases all pass.

### Task 8: Preserve visible completion output after fallback rejection

**Files:**
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/service/reply_admission_test.go`

When a synthesized fallback is rejected only for missing requester admission, keep the task terminal transition and insert one `system`-typed issue comment with the admission reason inside the same completion transaction. Do not insert the rejected agent-authored fallback or invoke admission again for the system notice.

- [x] **Step 1: Extend the service regression first**

Change the completion test to assert one issue comment remains, with `author_type = 'system'`, a visible admission reason, and no agent-authored fallback.

- [x] **Step 2: Run the service regression to verify RED**

Run: `go test ./internal/service -run 'TestCompleteTask_CompletionFallbackAdmissionRejectionCompletesTask$' -count=1`

Expected: the current implementation completes with zero comments, so the new visibility assertions fail.

- [x] **Step 3: Insert the system notice atomically**

Create a system comment on the task issue using the existing task source context, reference the required requester mention, and preserve the existing post-commit warning. The notice must be system-typed and therefore not create an agent reply loop.

- [x] **Step 4: Run service and affected verification**

Run: `go test ./internal/service -run 'TestCompleteTask_CompletionFallbackAdmissionRejectionCompletesTask$' -count=1` followed by `go test ./internal/replyadmission ./internal/service ./internal/handler ./cmd/server -count=1`, `go vet ./internal/replyadmission ./internal/service ./internal/handler ./cmd/server`, and `go build ./cmd/server`.

Expected: all affected checks pass; unrelated full-suite environment failures remain separately documented.
