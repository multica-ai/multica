# OL-25 repair contract v1: implementation and evidence map

This change implements the approved OL-25 repair contract v1 and the subsequent
11 specification gaps reproduced against PR #20 at `e8acb2c71a300ec6bfb9f1926cb8047beb170fb7`.
The baseline run failed all 14 `TestV1Review*` top-level tests at their behavioral
assertions. These tests now live in the normal package suites; only fixture
assembly was adapted. Acceptance is based on the final commit's attached test
results, not on this document claiming a pass.

## Boundaries and decisions

The repeated regressions came from incomplete production-path assertions and
different rules at configuration, diagnostic sending, background sending and
historical replay. The implementation keeps the existing automation state
machine and centralizes the missing invariants at four boundaries:

- **Source sharing authority.** `authorization.go` uses the resolved human
  member, existing `autopilotauth.CanWriteAutopilot`, and exact workspace /
  automation / installation / target approval. Configuration writes re-read
  authority in short workspace -> automation -> installation lock transactions
  after remote verification. Approval revoke and queued cancellation commit
  together, scoped to the exact approval ID. Every new shard uses the shared
  live gate, including diagnostics and manual retries. An accepted request
  cannot be recalled.
- **Persistent source and first terminal state.** Terminal failure without a
  replacement result preserves existing evidence. Either committed task/run
  link is sufficient; current configuration never proves historical mode.
  Unprovable history gets an empty, zero-shard `suppressed/source_unresolved`
  decision. Task outcomes are rechecked against the latest persisted attempt in the
  run's own dispatch chain. Unrelated issue tasks do not change that outcome.
  Completed dispatches settle successfully even when the issue is blocked;
  settlement freezes the issue status for outbound cards.
  First terminal writes and quota settlement retain their existing atomic
  guard and idempotent no-op behavior.
- **Complete compensation cycles.** Each of the three scanners freezes an
  immutable source-table upper ID, advances after each completed page with a
  generation CAS, and resumes across ticks and downtime. A cycle resets only
  after reaching its bound. Late old keys and newly eligible rows enter the
  next cycle. The 20,000-row tick budget and short transactions remain. Real
  errors are returned after all scanner classes get an opportunity to run;
  another replica advancing the cursor is benign contention.
- **Delivery ownership and cleanup.** Every new send and terminal/retry write
  requires a live token and unexpired lease, checked against the database
  clock. Late accepted receipts may update existing rows on a bounded detached
  context, without acquiring new send authority. Diagnostics persist their
  human actor separately in `requested_by`, and return live state after lease
  loss. Shared transactional lifecycle hooks remove approvals and stop queued
  sends on source archive, installation revoke and runtime removal.

The member binding proves address ownership; it is not source sharing consent.
The frozen target remains the approval identity after a route edit. Approval
creation and all approval management endpoints judge the acting human rather
than the runtime owner. `source_ref` remains a feedback locator, not authority.

## Acceptance map

Paths below are relative to `server/`. `MD`, `HTTP`, and `SYNC` abbreviate
`internal/messagedelivery`, `internal/handler`, and `internal/service`.

| Contract | Production boundary | Executable evidence |
| --- | --- | --- |
| A1: actual human, all management endpoints | `HTTP/labrastro_message_delivery.go`, `MD/authorization.go` | `TestV1ReviewHTTPApprovalRejectsCollaboratorOnOwnerRuntime`, `TestV1ReviewHTTPApprovalAcceptsOwnerOnMemberRuntime`, `TestContractHTTPApprovalManagementUsesActingHuman` |
| A1: approval scope and atomic revoke | `MD/service.go:RevokeTarget`, `pkg/db/queries/labrastro_message.sql` | `TestV1ReviewHTTPApprovalRevocationStopsQueuedDelivery`, `TestContractHTTPRevokeKeepsOtherInstallationApproval`, `TestContractRevocationRollsBackWhenCancellationFails` |
| A1: enable, diagnostics and each new shard | `MD/service.go:SetRouteEnabled`, `MD/deliveries.go:sendDelivery`, `MD/gates.go:gateClaimed` | `TestV1ReviewEnableRequiresApproval`, `TestV1ReviewTestSendRechecksSource`, `TestV1ReviewRevocationStopsNewShards`, `TestContractMemberBindingIsCheckedBetweenShards`, `TestContractMissingVerifierFailsClosed`, `TestContractTestSendRecoveryUsesPersistedActor` |
| A2: target identity / real request contract | `internal/integrations/lark/labrastro_delivery.go`, `MD/service.go` | `TestReviewCanonicalMemberTargetDedup`, `TestReviewExternalTargetsRequireVerification`, `TestReviewSendTimeVerificationRefusesMovedAnchor`, `TestDeliveryParams_TargetAddressing`, `TestReviewCreateDeliveryUUIDInBody`, `TestReviewCreateDeliveryUUIDInReplyBody` |
| B1: first terminal, success/failure race, quota | `SYNC/autopilot.go`, `SYNC/autopilot_quota.go`, `pkg/db/queries/autopilot.sql` | `TestRereviewFirstTerminalStatusCannotBeOverwrittenConcurrently` (done and cancelled second writers), `TestV1ReviewFailedTerminalPreservesResultWithoutReplacement`, existing `TestAutopilotQuota*` suite |
| B2: private errors / structured state | `MD/service.go:buildDecisionPayload`, `SYNC/autopilot.go:SyncRunFromIssue` | `TestRereviewFailedTaskRawReasonStaysOutOfStatusCard`, `TestReviewFirstTerminalStatusIsFrozen`, `TestBuildCreateIssueContent` |
| B3: persistent mode and both link directions | `MD/service.go:EnqueueRunDeliveries`, `SYNC/autopilot.go:SyncRunFromTask` | `TestRereviewScannerPreservesTaskBackedRunOnly`, `TestRereviewStaleTaskSurvivesModeEdit`, `TestV1ReviewUnknownSourceDoesNotFollowCurrentMode`, `TestContractUnknownSourceIsDurablySuppressed`, `TestV1ReviewScannerRecoversCommittedTaskWithoutRunReverseLink`, `TestContractScannerRecoversRunSideOnlyTaskLink` |
| C1: bounded complete traversal / restart / CAS | `MD/worker.go:advanceScanner`, `pkg/db/queries/labrastro_message.sql` | `TestRereviewStaleIssueScanCrossesPassBudget`, `TestV1ReviewCursorRetainsProgressAcrossDowntime`, `TestContractScanBoundAndGenerationFence` |
| C2: lost terminal event, active retry, completed successor | `SYNC/autopilot.go:SyncRunFromLinkedIssueTask`, `MD/worker.go:syncStaleLinkedTaskFailures` | `TestRereviewScannerRecoversLinkedIssueTaskFailure`, `TestV1ReviewScannerDoesNotReplayFailureAfterSuccessfulRetry`, `TestContractDispatchFailureWinsOverTerminalIssue`, `TestContractScannerRecoversCompletedDispatchAtNonterminalIssueStatus`, `TestContractScannerIgnoresUnrelatedTaskWhenSettlingDispatchFailure` |
| D1: parent deletion and lifecycle | `MD/authorization.go`, `MD/lifecycle/cleanup.go`, `HTTP/autopilot.go`, `HTTP/lark.go`, `SYNC/runtime_teardown.go` | `TestReviewDecisionCannotRecreateDeletedWorkspaceData`, `TestRereviewTestSendCannotRecreateDeletedWorkspace`, `TestV1ReviewApprovalCannotRecreateDeletedWorkspace`, `TestV1ReviewParentRaceAfterValidatedTarget`, `TestContractLifecycleStopsApprovedDeliveries`, workspace deletion manifest suite |
| D2: expired/old owner, late responses, receipt resume | `MD/deliveries.go`, `MD/worker.go`, `pkg/db/queries/labrastro_message.sql` | `TestReviewExpiredWorkerCannotStartSend`, `TestReviewTestSendInterruptedOutcomeCanRecover`, `TestRereviewTestSendCannotOverwriteNewLease`, `TestV1ReviewExpiredWorkerCannotRequeue`, `TestContractLateOutcomesCannotRestoreSendingAuthority`, `TestContractTestSendReturnsLiveStateAfterLostLease`, `TestWorker_ShardResumeAfterTransientFailure` |
| E1: cleanup can invalidate a green body | `MD/delivery_db_test.go:TestMain` | Two same-database suite executions; zero suite workspace/member/agent/user residue; overlay-induced cleanup SQL failure must produce a nonzero exit even with no failing test body |

The rereview's S5 is the same parent-lock gap as its specification item 3.
S6 (return cancellation failures from revoke) is covered by the rollback test.
The earlier eight rereview tests remain in `delivery_rereview_regressions_test.go`;
the third review's 14 tests are in the three `*regressions_test.go` files carrying
`TestV1Review*` names. Fault hooks assert that the injection point was reached.

## Migration and integration

Migration `463_labrastro_message_repair_state` adds a cursor upper bound and a
diagnostic actor, and permits zero-shard unknown sources only when suppressed.
It adds no foreign keys or indexes and does not rewrite applied migrations.
An upgraded cursor restarts once because its old position had no trustworthy
cycle bound. Existing unapproved group/topic routes receive no automatic
approval. Existing diagnostic rows with no actor fail closed when replayed;
request a new diagnostic to establish fresh authority.

The migration was exercised on an empty database and on a database originally
migrated from the previous PR head. Rollback of
`463_labrastro_message_repair_state` deletes only unsent unknown markers, restores the earlier checks, and removes the two new columns. It must
be paired with the older binary, with delivery workers stopped. Reapplying it
rediscovers suppressed unknown markers. It does not restore source evidence or
infer consent that never existed.

PR #20 also integrates current main's project-file migrations without replacing
either concurrent-index recovery map. Two inherited integration test failures
were reconciled: the embedded runtime skill no longer cites a repository-only
path; the workspace deletion manifest explicitly classifies project-file
metadata as retained and inaccessible, matching the existing documented
retention behavior. No project-file production deletion behavior changed.

## Reproduction

Use an isolated PostgreSQL database and keep database-sharing packages serial.
Apply migrations from `server/` with `go run ./cmd/migrate up`, then run:

```sh
go test -p 1 -count=1 -json -timeout=300s \
  ./internal/messagedelivery ./internal/handler ./internal/service \
  ./internal/integrations/lark ./internal/integrations/channel \
  ./internal/integrations/channel/engine ./cmd/migrate ./cmd/server \
  ./internal/migrations
go test -race -p 1 -count=1 -json -timeout=300s ./internal/messagedelivery
go vet ./internal/messagedelivery/... ./internal/handler ./internal/service \
  ./internal/integrations/lark ./internal/integrations/channel \
  ./internal/integrations/channel/engine ./cmd/migrate ./cmd/server
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
git diff --exit-code -- pkg/db/generated
```

The attached final evidence records the exact commit, package results, skips,
migration checks and cleanup fault probe. These are local tests with fake
outbound senders. Real Feishu member/group/topic acceptance still requires an
authorized bot and explicit test targets; no real message, deployment or merge
is part of this repair handoff. CI started by pushing is not awaited.
