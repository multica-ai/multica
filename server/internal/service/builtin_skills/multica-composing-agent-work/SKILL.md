---
name: multica-composing-agent-work
description: "Use when choosing or running a Multica workflow that combines agents, skills, models, providers, or thinking modes. Covers solo vs serial vs bounded-parallel topology, cross-provider second opinions, skill/model affinity, runtime and observed-usage checks, staged child issues, artifact handoffs, one-write-owner safety, and the boundary between current routing signals and future automatic selection."
allowed-tools: Bash(multica *)
---

# Composing Agent Work

## Core rule

Use the smallest topology that earns its extra latency and provider-plan usage.
Compose work as:

```text
workload -> skill set -> eligible worker/model config -> artifact -> next stage
```

Multica owns durable state, ordering, and dispatch. One temporary lead owns
synthesis. One executor owns write side effects. Other branches advise or
produce bounded artifacts.

Do not permanently bind a skill to one model. Treat model affinity as a
versioned, evidence-backed preference with an explicit fallback. Do not change
an agent's model, thinking level, skills, or runtime merely to route one task
unless the user explicitly asked for that configuration mutation.

## Choose the topology

### Solo

Use one agent and the minimum applicable skills for routine, low-risk work that
fits one context and does not benefit from an independent view. Solo should be
the default and the majority of tasks.

### Serial

Use serial stages when a later unit depends on an earlier artifact: research ->
plan -> implementation -> verification. Give each stage an input contract and
an output contract. Do not start later stages until their dependencies are
complete and their artifacts have been inspected.

For durable Multica work, create the first stage as `todo` and park later stages
as `backlog`:

```bash
multica issue create --title "Research" --parent <issue-id> --assignee-id <agent-id> --stage 1 --status todo
multica issue create --title "Synthesize plan" --parent <issue-id> --assignee-id <lead-id> --stage 2 --status backlog
multica issue create --title "Implement" --parent <issue-id> --assignee-id <executor-id> --stage 3 --status backlog
multica issue children <issue-id> --output json
```

Promote the next stage only after the current stage barrier closes and the
stated dependency is satisfied. Use `multica-working-on-issues` for the exact
child-status and stage-promotion contract.

### Bounded parallel

Use parallel branches only when the work units are genuinely independent,
latency matters, or independent reasoning is likely to change the decision.
Good cases include ambiguous brainstorming, independent research sources, and
two proposals that should not anchor on each other.

Place parallel children in the same stage and start them together:

```bash
multica issue create --title "Independent proposal A" --parent <issue-id> --assignee-id <agent-a-id> --stage 1 --status todo
multica issue create --title "Independent proposal B" --parent <issue-id> --assignee-id <agent-b-id> --stage 1 --status todo
multica issue create --title "Compare and synthesize" --parent <issue-id> --assignee-id <lead-id> --stage 2 --status backlog
```

Branches must not edit the same files, mutate the same resource, message the
same external recipient, or trigger the same deployment. Prefer read-only
branches. Cap branch count and define the convergence stage before dispatch.

### Cross-provider review

Use one different-provider reviewer for high-blast-radius, ambiguous, or
low-confidence work. The reviewer receives the goal, constraints, relevant
artifact or diff, verification evidence, and authority limits—not hidden
chain-of-thought. Default to one read-only critique round with a structured
verdict:

```text
verdict: approve | changes_required | blocked
must_fix: [...]
concerns: [...]
confidence: low | medium | high
```

The reviewer does not become the executor and does not independently apply the
same side effect.

## Select skills and workers

Read current state before routing:

```bash
multica agent list --output json
multica agent get <agent-id> --output json
multica agent skills list <agent-id> --output json
multica runtime list --output json
multica runtime usage <runtime-id> --days 7 --output json
multica runtime activity <runtime-id> --output json
```

Choose an eligible worker using:

1. required skills, tools, repository access, and authority;
2. measured quality for the workload, not provider reputation alone;
3. configured model and thinking level;
4. online runtime, current agent status, and configured task concurrency;
5. observed token usage, latency needs, and known plan-headroom signals;
6. trust constraints such as protected reviewers or failover exclusions.

Prefer a cheaper/faster validated configuration for bounded routine stages and a
deeper validated configuration for synthesis, architecture, or difficult
verification. Spend two providers only when expected value justifies consuming
both plans.

`runtime usage` reports observed token history. It does not report a
subscription's remaining allowance or reset window. Do not infer plan headroom
from token history alone. Use an explicit monitor/policy signal or user-provided
capacity information when available; otherwise state that plan capacity is
unknown.

## Define every work unit

Before dispatch, record:

- goal and done criteria;
- input artifact and output artifact;
- required skills and tools;
- selected worker plus the reason it is eligible;
- read-only or write-capable authority;
- files/resources it may touch;
- latency or usage budget;
- fallback or stop condition;
- provenance needed by the convergence stage.

Handoffs contain decisions, artifacts, tests, and open risks. Do not pass
credentials, raw hidden reasoning, or unrelated history.

## Side-effect and failure rules

- Assign exactly one write-capable executor per effect surface.
- Keep brainstorm and critique branches read-only unless their scopes are
  disjoint and explicit.
- Never use parallel agents to race the same implementation or deployment.
- Do not advance a serial pipeline after a missing artifact, failed acceptance
  criterion, unavailable required skill, or authority mismatch.
- Protected/authority-sensitive agents keep their configured provider and role;
  do not route around that boundary.
- Capacity exhaustion may select a validated fallback. Authentication,
  permission, data-integrity, and logic failures stop for repair instead of
  switching providers.

## Current capability boundary

Available now: agent/model/thinking/skill inventory, runtime liveness,
historical runtime usage, child issues, ordered stages, durable comments, and
exactly-once guarded child dispatch on provider handoff.

The backend also has a pure deterministic routing policy that can rank
validated model/thinking candidates, enforce required skills/tools/authority,
preserve a supplied provider reserve, consume only promoted evidence-backed
skill affinity, and select a safe topology with one write owner. That policy is
not yet connected to task admission or agent rebinding.

Not automatically exposed by these commands: provider subscription allowance,
reset windows, automatic per-task model rebinding through routing-policy
invocation, or automatic promotion of a new skill/model affinity. Treat live
capacity and independently promoted evidence as router-policy inputs still
requiring collectors and service integration; do not describe adaptive routing
as live.

If the user asks only for a design, return the proposed topology without
creating work. If the user asks to execute, create only the authorized children,
then report their IDs, stages, assignees, and the single side-effect owner.

Source-backed command and behavior map:
`references/composing-agent-work-source-map.md`.
