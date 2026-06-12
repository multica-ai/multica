# Coding Team — Multica Setup Guide

## Overview

Six agents run the sequential delivery pipeline: read a Multica request or ADO deliverable -> determine planning source -> guided planning when needed -> create/reuse tasks -> plan -> implement -> test -> review -> PR. A seventh Watchdog agent runs stall recovery. The master Multica issue carries all pipeline state; no local state files.

The front of the workflow supports three planning sources:
- Existing ADO child Tasks, when regular planning has already happened in Azure DevOps.
- Guided planning from the ADO deliverable or direct Multica issue content, using a Grill Me-style one-question-at-a-time review before tasks are proposed.
- Orchestrator decomposition from the deliverable when no prior planning exists.

If `deliverable_id` is omitted, the run is Multica-only: no ADO work-item fetches, child Tasks, task creation/linking, Component lookup, or ADO comments. The Orchestrator uses the master issue title/body/comments as the deliverable context, creates Multica child issues only, and defaults to guided planning unless decomposition is explicit.

During ADO-backed planning, the Planner also walks the ADO parent hierarchy from the deliverable to find the nearest Component. The hierarchy is intentionally flexible: the Component may be the direct parent of the deliverable, several parent levels above it, or absent. When found, the Component is used as an ownership signal for choosing the existing code project/module.

---

## Skills to import

Import all 9 skill files into your Multica workspace. Each SKILL.md file in this directory is a standalone skill.

| File | Assign to |
|------|-----------|
| `shared-ado-ops.md` | Pipeline agents that use ADO or repo operations; not Watchdog |
| `shared-state-ops.md` | All coding-team agents, including Watchdog |
| `orchestrator.md` | Coding Team Orchestrator only |
| `planner.md` | Coding Team Planner only |
| `implementer.md` | Coding Team Implementer only |
| `test-writer.md` | Coding Team Test Writer only |
| `reviewer.md` | Coding Team Reviewer only |
| `pr-writer.md` | Coding Team PR Writer only |
| `watchdog.md` | Coding Team Watchdog only |

## Deterministic tools to import

Import these root `dettools/` sources as required deterministic tools. The skills assume these tools are enabled and should stop with an operator-facing error if the deterministic tool plane is unavailable.

| File | Tool name | Assign/enable for |
|------|-----------|-------------------|
| `dettools/pipeline_state_parse.go` | `pipeline_state_parse` | All coding-team agents, including Watchdog |
| `dettools/ado_payload_normalize.go` | `ado_payload_normalize` | Orchestrator, Planner, PR Writer, and any coding-team agent that fetches ADO payloads |
| `dettools/coding_watchdog_analyze.go` | `coding_watchdog_analyze` | Coding Team Watchdog |
| `dettools/coding_comment_extract.go` | `coding_comment_extract` | Planner, Implementer, Test Writer, Reviewer |
| `dettools/coding_plan_validate.go` | `coding_plan_validate` | Planner and Implementer |

From the repo root, import or refresh the workspace-authored tools with:

```bash
for f in dettools/*.go; do
  multica dettool import-file "$f" --output table
done
```

`multica dettool import-file` creates the tool on the first run and updates the
existing tool with the same name after source edits.

`dotnet_test_gate` is a built-in deterministic tool compiled into the daemon, not
a root `dettools/` source to import. Keep it enabled in
`MULTICA_DETTOOLS_ALLOWED` for Coding Team Implementer, Test Writer, and
Reviewer so C# tasks cannot progress past Test or Review without a passing
`dotnet test` result.

The daemon must also have the deterministic tool plane enabled, otherwise agent
backend logs will show `mcp_config=false` and the tools will never appear in the
agent's MCP tool list:

```bash
export MULTICA_DETTOOLS_ENABLED=true
multica daemon restart
```

The daemon binary must include `multica mcp-tools serve`. Verify with:

```bash
multica mcp-tools --help
```

---

## Create the 7 agents

Create each agent in the Multica UI. Names must match exactly (used for @mention discovery at runtime).

### Coding Team Orchestrator
- **Name:** `Coding Team Orchestrator`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `orchestrator`
- **Instructions:** You manage a multi-agent software delivery pipeline. You fetch work from Azure DevOps, coordinate task agents, and ensure the pipeline completes end-to-end.
- **Max concurrent tasks:** 1

### Coding Team Planner
- **Name:** `Coding Team Planner`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `planner`
- **Instructions:** You explore codebases and produce concrete implementation plans. You are one stage of a multi-agent pipeline — read your task from the Multica issue and hand off to the Implementer when done.
- **Max concurrent tasks:** 1

### Coding Team Implementer
- **Name:** `Coding Team Implementer`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `implementer`
- **Instructions:** You write production code following an implementation plan. You are one stage of a multi-agent pipeline — read the plan from prior comments on your issue and hand off to the Test Writer when done.
- **Max concurrent tasks:** 1

### Coding Team Test Writer
- **Name:** `Coding Team Test Writer`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `test-writer`
- **Instructions:** You write comprehensive unit tests covering all acceptance criteria. You are one stage of a multi-agent pipeline — read implementation context from prior comments on your issue and hand off to the Reviewer when done.
- **Max concurrent tasks:** 1

### Coding Team Reviewer
- **Name:** `Coding Team Reviewer`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `reviewer`
- **Instructions:** You conduct strict code reviews against acceptance criteria and coding standards. You are the final stage before a task is committed — your PASS or FAIL verdict is posted to both the task issue and the master issue.
- **Max concurrent tasks:** 1

### Coding Team PR Writer
- **Name:** `Coding Team PR Writer`
- **Skills:** `shared-ado-ops`, `shared-state-ops`, `pr-writer`
- **Instructions:** You write professional pull request descriptions and create draft PRs in Azure DevOps. You are the final stage of the pipeline — compose and open the PR, then mark the master issue done.
- **Max concurrent tasks:** 1

---

### Coding Team Watchdog
- **Name:** `Coding Team Watchdog`
- **Skills:** `shared-state-ops`, `watchdog`
- **Instructions:** run stall-recovery scans for coding-team master issues, re-issue dropped handoff mentions, post a scan summary, and close the watchdog tick issue.
- **Max concurrent tasks:** 1

---

## Squads guidance

Do **not** assign coding pipeline master or task issues to a Coding Team squad. This pipeline is a deterministic sequential workflow: Orchestrator → Planner → Implementer → Test Writer → Reviewer → PR Writer. Each stage already performs an explicit `@mention` handoff to the next exact agent and depends on that direct routing for idempotency and watchdog recovery.

Role boundaries are non-negotiable. Orchestrator coordinates only, must never edit code, and must not check out the repo before task approval; its only checkout is in Run 2a to create/verify the feature branch. Planner is read-only, owns the first codebase inspection, and must never implement. Implementer is the first code-writing stage and must commit/push before handing off. Test Writer may modify tests and must commit/push before handing off. No stage may clean or delete a workspace with uncommitted or unpushed changes.

Multica Squads are best used for routing/fan-out work where a leader chooses a specialist. Assigning these coding issues to a squad leader would add an extra routing layer and can conflict with the Orchestrator's responsibility to perform pipeline work directly. Continue assigning new coding pipeline master issues to **Coding Team Orchestrator** exactly as described below.

If you want a squad for ad hoc coding questions, create it separately and do not use it for this delivery pipeline's master/task issues unless the skills are redesigned around a squad-leader router.

## Required environment variables

Set both on all 6 agents via the Multica agent `custom_env` field. The skills use inline `AZURE_DEVOPS_EXT_PAT=` prefixes to route the right PAT to the right ADO instance — never let one bleed into the other.

| Variable | ADO instance | Used for |
|----------|-------------|----------|
| `ADO_PAT_INCYCLE` | `https://dev.azure.com/incyclesoftware` | Work items, boards, comments (project: `ineight`) |
| `ADO_PAT_INEIGHT` | `https://dev.azure.com/{code_org}` | Git repo, pull requests for the configured code repo |

PAT scopes required:
- `ADO_PAT_INCYCLE` — **Work Items:** Read & Write
- `ADO_PAT_INEIGHT` — **Code:** Read & Write; **Pull Requests:** Read & Write

---

## Connect your repositories

Add every repo you want this pipeline to support to the Multica workspace so agents can check it out. For example:
```
https://dev.azure.com/ineight/Platform/_git/AgenticAI
```

---

## Triggering a pipeline run

1. Create a new Multica issue with this description:
   ```json
   {
     "deliverable_id": "47369",
     "code_org": "ineight",
     "code_project": "Platform",
     "repo_name": "AgenticAI",
     "repo_url": "https://dev.azure.com/ineight/Platform/_git/AgenticAI",
     "base_branch": "develop",
     "planning_mode": "auto"
   }
   ```
   The config may be fenced JSON or the leading top-level JSON object before the prose request. `deliverable_id` is optional. If it is present, the run may use ADO work items. If it is absent, the run is Multica-only and uses the issue body as the request. A config-only JSON block with `planning_source: "guided_plan"` is a request for guided planning, not existing pipeline state. `code_org` defaults to `ineight`, `code_project` defaults to `Platform`, `repo_name` defaults to `AgenticAI`, `repo_url` defaults to `https://dev.azure.com/ineight/Platform/_git/AgenticAI`, `base_branch` defaults to `develop`, and `planning_mode` defaults to `auto`.

   For new issues, set the repo fields explicitly so the pipeline can support multiple repositories. If `repo_url` is supplied, the Orchestrator preserves that repo path and embeds `ADO_PAT_INEIGHT` before checkout. If `repo_url` is omitted, it constructs `https://dev.azure.com/{code_org}/{code_project}/_git/{repo_name}` and embeds `ADO_PAT_INEIGHT`.

   `planning_mode` values:

   | Value | Use when |
   |-------|----------|
   | `auto` | With `deliverable_id`, use existing ADO tasks when present; otherwise guided planning. Without `deliverable_id`, guided planning from Multica issue content. |
   | `ado_existing` | Regular planning has already created ADO child Tasks and the pipeline should use those exact tasks. |
   | `guided` | The Orchestrator should question the plan one decision at a time, then create tasks after approval. |
   | `decompose` | The Orchestrator should use the original automatic decomposition path. |

   Accepted aliases: `planning_mode: "guided_plan"` means `guided`; `planning_source: "guided_plan"` also requests guided planning.

   Legacy `use_existing_tasks: true` still works as an alias for `planning_mode: "ado_existing"`, but new issues should use `planning_mode`.

   Guided planning does not require a task object in the Multica issue. With `deliverable_id`, the Orchestrator pulls ADO work-item information. Without `deliverable_id`, it uses the Multica issue title/body/comments. Comments are authoritative when they narrow, expand, or contradict the deliverable fields.

   Do not add or request a `## Guided Plan Ready` section for guided planning. That older artifact-based flow has been replaced by ADO-derived guided planning.

   Guided planning follows the Grill Me interaction pattern: the Orchestrator asks one question at a time, includes its recommended answer, and records the answer in master issue state. For Multica-only runs, the first Orchestrator run must ask one guided-planning question and stop before task synthesis or Planner handoff. The Orchestrator may use ADO context when available, but it must not check out or inspect the repo during guided planning; Planner owns codebase exploration after approval.

2. Assign the issue directly to **Coding Team Orchestrator**. Do not assign coding pipeline issues to a squad.

3. The Orchestrator reads the request, fetches ADO only when `deliverable_id` is present, and determines the planning source. For guided planning, it first asks one decision question at a time. Reply with your answer, or reply **use recommendation** to accept the recommended answer.

4. After guided planning is complete, or immediately for existing/decomposition paths, the Orchestrator proposes the task list. Reply **approve** (or provide feedback for revision). The Orchestrator reuses/creates ADO tasks only for ADO-backed runs, creates Multica child issues and the feature branch, then kicks off the Planner on the first task.

5. The pipeline runs automatically through plan → implement → test → review for each task sequentially. During planning, the Planner performs the first repository checkout and uses the deliverable, task details, optional ADO Component, and codebase structure to choose the owning code project/module.

6. When all tasks complete, the Orchestrator posts a summary and asks you to reply **push** to create the draft PR.

7. After you reply **push**, the PR Writer creates a draft PR in Azure DevOps and posts the URL.

---

## Pipeline stage reference

| Stage | What's happening |
|-------|-----------------|
| `guided_planning` | Orchestrator asking one guided planning question at a time before task proposal |
| `awaiting_approval` | Orchestrator waiting for task breakdown approval |
| `implementing` | Task agents working through tasks sequentially |
| `awaiting_push` | All tasks done, waiting for push approval |
| `done` | PR created |

Planning sources: `regular_ado_tasks`, `guided_plan`, `orchestrator_decomposition`.

Task statuses: `pending → awaiting_clarification → planned → implemented → tested → committed / failed`

If the Planner cannot produce an implementation-ready plan from the available ADO, Multica, and codebase context, it sets the task to `awaiting_clarification`, posts detailed questions on the task issue, and alerts the master issue. The pipeline remains paused on that task until the user replies with answers and re-mentions or reassigns the Coding Team Planner.

---

## Watchdog autopilot (recommended)

Multica's `@mention` triggering is occasionally lossy: an agent can finish its work but the next-stage mention comment may not enqueue a new run, and the pipeline stalls. The separate **Coding Team Watchdog** agent detects stalled task issues and re-issues missing handoffs. Schedule it via a Multica autopilot.

### One-time setup

```bash
multica autopilot create \
  --title "Coding Team Watchdog Tick" \
  --agent "Coding Team Watchdog" \
  --mode create_issue \
  --description "Run a stall-recovery scan across all active coding-team master issues. Detect task issues whose handoff @mention was dropped and re-issue them. Close this tick issue when done."

AUTOPILOT_ID=<id from create command>

multica autopilot trigger-add "$AUTOPILOT_ID" \
  --cron "*/10 * * * *" \
  --timezone "UTC"
```

Every 10 minutes the autopilot creates a "Coding Team Watchdog Tick" issue. Coding Team Watchdog scans active master issue state, re-mentions any stalled handoff, posts a scan summary, and closes the tick.

### Idempotency

The Planner, Implementer, Test Writer, and Reviewer all have a Step 0 idempotency check: if their characteristic marker comment already exists on a task issue (`## Implementation Plan`, `## Implementation Complete`, `## Tests Written`, `## Review: PASS`/`FAIL`), they skip the work and only re-emit the handoff @mention. If the Planner marker is `## Planning Blocked: Clarification Needed`, the task stays paused instead of handing off. So a watchdog re-mention on a task whose work was already done — but whose handoff was lost — recovers the pipeline without duplicate commits or duplicate tests.

### Pausing the watchdog

```bash
multica autopilot update <autopilot-id> --status paused
# Resume with --status active
```

---

## Resuming after a pause

The master Multica issue holds all state. If the pipeline was paused (you replied **pause** instead of **push**), simply reply **push** when ready — the Orchestrator will pick up from `awaiting_push`.

If a task agent failed mid-run, the task status in master issue state will show `failed`. The pipeline continues to the next task automatically. Failed tasks are reported in the final summary; you can fix them manually or re-run the deliverable with `planning_mode: "ado_existing"` targeting only the failed work.
