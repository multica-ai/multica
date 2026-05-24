# Architecture Spec

## Goal
One Multica issue can own a PR review window: per-PR workers resolve or reject review comments in isolated sessions, and one orchestrator posts a single summary showing which PRs are ready and which need human follow-up.

## Context
Today the GitHub integration mirrors PR metadata, links PRs to issues, and stores check-suite rollups, but it does not mirror PR review comments. The existing squad/task system already supports the important execution primitives for this feature: squad leaders coordinate work, leader tasks are role-tagged, and task queue rows can force fresh sessions. This spec starts with a reversible prompt/template implementation that uses runtime GitHub access for review-comment input and Multica issue comments as the report surface. A later backend adapter can persist review comments if the MVP shows that runtime fetches are unreliable or too hard to audit.

Code references: the current GitHub docs say only the PR itself is mirrored and later GitHub comments are ignored (`apps/docs/content/docs/github-integration.mdx:21`, `apps/docs/content/docs/github-integration.mdx:53`). Linked PR listing is backed by PR/check-suite queries, not review-comment queries (`server/pkg/db/queries/github.sql:94`). Squad leader coordination is already injected into leader claims (`server/internal/handler/squad_briefing.go:11`), and task rows already carry `force_fresh_session` and `is_leader_task` (`server/pkg/db/queries/agent.sql:86`).

## Proposed Design
Use the existing squad model as the orchestration boundary.

The orchestrator is an agent created from the PR Review Orchestrator template, typically acting as a squad leader. It owns the review-window issue, discovers the PR list, delegates exactly one PR per worker session, waits for worker result contracts, and posts the final report on the parent issue.

Each worker is an agent created from the PR Review Worker template. A worker handles one PR only. It checks out the repository at the PR head SHA, fetches review comments from GitHub at runtime, classifies each comment into the four-state contract, fixes valid comments when possible, and replies with a structured result. Workers do not aggregate across PRs.

Reversible decision: PR review-comment input is runtime GitHub access via `gh` or GitHub API in the worker session for the MVP. This avoids adding schema before the team proves the workflow reduces human touchpoints. If auditability or offline retry requires persistence, add a normalized adapter that stores GitHub review comments in Multica.

Reversible decision: the final report lands as a Multica issue comment on the orchestration issue. This is the only surface every worker can write to today and it matches the requirement that Stometa opens one place for the summary. Posting back to PR comments remains a later product decision.

## Data Model
No schema changes in the MVP.

Existing fields used:
- `agent_task_queue.force_fresh_session`: manual rerun and future task creation paths can force a clean agent session.
- `agent_task_queue.is_leader_task`: preserves the squad leader role so leader self-trigger guards can avoid loops.
- `issue.assignee_type = 'squad'`: makes the parent issue a coordination surface while workers run as separate agent tasks.
- `comment.content`: stores worker result contracts and the final summary report.

Future adapter, if needed:
- `github_pull_request_review_comment`: normalized immutable source comments keyed by workspace, repo, PR number, GitHub comment ID, head SHA, path, line, author login, body, state, and timestamps.
- `pr_review_resolution`: worker-produced resolution keyed by source comment ID, task ID, status, evidence, commit URL, and verification output reference.

Migration plan for the future adapter: backfill only currently open linked PRs; leave historical MVP comments as issue timeline artifacts rather than parsing them into tables.

## Runtime Flow
1. User opens or reuses a Multica issue for the review window and assigns it to a squad whose leader is the PR Review Orchestrator.
2. Orchestrator reads the issue, linked PRs, and any user-supplied PR URLs. If no PRs are discoverable, it asks for the PR list and records no action.
3. Orchestrator creates one worker task per PR. The preferred route is one `todo` child issue assigned to an appropriate worker agent, because child assignment creates an isolated session and avoids duplicate mention triggers. If the team uses a single parent thread, the orchestrator may delegate by one mention per PR.
4. Each worker fetches PR metadata and review comments from GitHub, checks out the repo at the PR head SHA, and evaluates comments against the full code context.
5. Worker applies fixes for valid comments when safe, commits/pushes to the PR branch when credentials permit it, runs verification, and posts the worker result contract.
6. Orchestrator re-wakes on worker updates, validates that every PR has a worker result, and posts the final summary on the parent issue.
7. PRs with only `fixed` and `rejected` comments are listed as ready. PRs with `attempted_unresolved`, `needs_human`, missing worker results, failed checkout, failed GitHub fetch, or failed verification are listed as needing human follow-up.

## Observability
Use existing Multica task and activity surfaces:
- Worker and orchestrator task lifecycle is visible through `agent_task_queue` task events.
- Squad leader decisions are recorded through `multica squad activity`.
- The parent issue timeline contains the final report and worker result links.
- Each worker result must include checkout ref, commit URL when changed, verification commands, and pass/fail output summary.

If a backend adapter is added later, add logs around GitHub fetch failures and metrics for comments processed per PR, comments fixed, comments rejected, comments needing human review, and worker task failures.

## Security
- Workers use the local runtime's GitHub credentials; Multica must not store GitHub tokens in issue comments or metadata.
- Workers must not paste secrets from test output into reports.
- Orchestrator must not delegate a PR to the same agent that authored the review comments when that author is known.
- Workers must treat GitHub comment bodies as untrusted input: no shell execution from comment text, no unquoted branch/ref interpolation, and no following arbitrary links as commands.
- Existing Multica authz gates still govern who can assign issues, create child issues, and comment on the parent issue.

## Alternatives
- Single agent processes all PRs serially: rejected because later PRs inherit earlier PR context and the issue explicitly requires fresh per-PR sessions.
- Persist GitHub review comments before any workflow ships: rejected for MVP because the current GitHub model intentionally stops at PR metadata/check suites, and runtime fetches are enough to validate the human-touchpoint hypothesis.
- Post one summary per worker/agent: rejected because the user requirement is one Ace/orchestrator summary, not scattered reports.
- PR comments as the only report surface: rejected for MVP because workers and orchestrator already have a common Multica issue surface, while PR write permissions vary by runtime.

## Verification
Run these commands from the repository root:

```bash
cd server && go test ./internal/agenttmpl
cd server && go test ./internal/handler -run 'TestClaimTask_LeaderGetsBriefing|TestCreateIssue_AssignedToSquadEnqueuesLeader|TestSquadMentionTriggersLeader'
rg -n "^TODO_DECISION:" docs/pr-review-closure-architecture.md server/internal/agenttmpl/templates/pr-review-*.json
```

Assertions:
- Template loading passes with both new PR review templates embedded.
- Squad leader briefing behavior still injects coordination instructions.
- Existing squad assignment and mention triggers still enqueue leader tasks.
- No unresolved decision marker remains in this MVP spec or templates.

## Checkpoints

### Checkpoint 01: Document the review-closure architecture

- ID: cp-01
- Type: docs
- Effort: s
- Depends on: none

#### Scope
Add the architecture spec for the PR review closure workflow. This checkpoint defines boundaries, contracts, alternatives, security, and verification. It does not add database tables or backend APIs.

#### Acceptance Criteria
- The spec states the orchestrator and worker responsibilities.
- The spec includes data model, runtime flow, observability, security, alternatives, and verification commands.
- Reversible decisions are explicitly annotated.
- No unresolved decision marker remains for MVP scope.

#### Verification Commands
```bash
rg -n "^TODO_DECISION:" docs/pr-review-closure-architecture.md
```

### Checkpoint 02: Ship reusable PR review agent templates

- ID: cp-02
- Type: backend
- Effort: s
- Depends on: cp-01

#### Scope
Add prompt-only curated templates for the orchestrator and worker roles. This checkpoint reuses the existing agent-template registry and does not introduce a new runtime abstraction.

#### Acceptance Criteria
- The orchestrator template describes PR discovery, per-PR delegation, no self-review routing, and final report format.
- The worker template describes one-PR scope, checkout at head SHA, four-state comment resolution, commit/evidence requirements, and worker result format.
- Template registry validation succeeds.

#### Verification Commands
```bash
cd server && go test ./internal/agenttmpl
```

### Checkpoint 03: Preserve squad routing behavior

- ID: cp-03
- Type: backend
- Effort: s
- Depends on: cp-02

#### Scope
Verify the implementation relies on existing squad leader injection and task routing without changing routing semantics.

#### Acceptance Criteria
- Squad leader claim still receives the squad briefing.
- Assigning an issue to a squad still enqueues the leader.
- Mentioning a squad still triggers the leader.

#### Verification Commands
```bash
cd server && go test ./internal/handler -run 'TestClaimTask_LeaderGetsBriefing|TestCreateIssue_AssignedToSquadEnqueuesLeader|TestSquadMentionTriggersLeader'
```

### Checkpoint 04: Add first-class review-comment persistence if needed

- ID: cp-04
- Type: backend
- Effort: l
- Depends on: cp-01

#### Scope
Optional follow-up after MVP validation. Add a normalized GitHub review-comment adapter and resolution tables only if runtime GitHub fetches fail the auditability or reliability requirements.

#### Acceptance Criteria
- GitHub review comments can be listed from Multica without shelling out to `gh`.
- Worker result status can be tied to a stable source comment ID.
- Existing PR metadata/check suite behavior remains backward-compatible.

#### Verification Commands
```bash
cd server && go test ./internal/handler -run GitHub
cd server && go test ./pkg/db/...
```
