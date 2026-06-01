# Agent skill necessity records

This document records why each built-in Multica skill exists and how to test its value. It is not a marketing catalog. Each section must show the failure mode that appears without the skill and the behavior that appears with the skill.

Use this document when you add a built-in skill, review whether a skill belongs in the runtime brief, or design an evaluation that compares agent behavior with and without a specific skill.

## How to write a skill necessity record

Each skill record must answer the same questions. Keep the examples concrete; a vague benefit is not enough to justify a built-in skill.

### Purpose

Describe the missing platform knowledge that the skill gives the agent. This is not the task name. It is the reason a general coding agent would otherwise act like it understands Multica while missing a product contract.

### Platform contract

Name the real Multica rule that the skill teaches. Link the rule to source code, API behavior, CLI behavior, database shape, or observed product behavior.

### Without this skill

Show the wrong output or wrong action the agent is likely to produce. Prefer a short prompt and a concrete bad response, command, PR title, status change, or comment body.

### Failure mode

Explain what breaks. Be specific about whether the failure is silent, visible, recoverable, or likely to poison later context.

### With this skill

Show the corrected behavior. Include the commands the agent must run, the text it must write, and the checks it must perform.

### Test scenario

Define an evaluation case. A useful test scenario includes:

- a prompt that can be run with and without the skill;
- the expected failure without the skill;
- the expected successful behavior with the skill;
- the observable pass criteria.

### Why this belongs in a skill

Explain why the knowledge belongs in an on-demand skill instead of the always-on runtime brief. Hard contracts that must be known before any skill can load stay in the brief. Longer workflows and product-specific methods belong in skills.

## `multica-mentioning`

`multica-mentioning` exists because Multica mentions are not plain Markdown. They are side-effecting links that notify members, enqueue agents, enqueue squad leaders, or create safe issue references.

### Purpose

The skill teaches the agent to build a mention link that actually fires. A general coding agent may treat `@Alice` or `mention://member/Alice` as a normal human-readable mention. Multica requires a real UUID in the link target.

### Platform contract

Multica mention links use this shape:

```md
[@Name](mention://<type>/<id>)
```

The `<id>` must be a real UUID for `member`, `agent`, `squad`, and `issue` mentions. The only exception is `mention://all/all`.

The source of truth is:

- `server/internal/util/mention.go:16`, which parses only UUID-shaped mention IDs or the literal `all`;
- `server/internal/handler/comment.go:884`, which enqueues mentioned agents and squad leaders;
- `server/internal/handler/comment.go:768`, which handles `@all` broadcast behavior.

### Without this skill

A prompt like this is enough to trigger the failure:

```text
Fix the bug, then @ Alice for review and ask GPT-Boy to continue the follow-up.
```

Without the skill, the agent may write:

```md
[@Alice](mention://member/Alice) please review.
[@GPT-Boy](mention://agent/GPT-Boy) please handle the follow-up.
```

It may also write a bare mention:

```md
@Alice please review.
```

Both outputs look plausible to a person, but they do not satisfy the Multica mention protocol.

### Failure mode

The failure is dangerous because it can be silent:

- the member may not receive a notification;
- the agent may not be enqueued;
- the squad leader may not be enqueued;
- the comment may show text that looks like a successful handoff;
- the original agent may stop because it believes it delegated the work.

This breaks the collaboration chain without creating an obvious error for the user.

### With this skill

With the skill, the agent must look up the correct entity before it writes the comment:

```bash
multica workspace member list --output json
multica agent list --output json
multica squad list --output json
```

It must use the right ID source for each mention type:

- `member` uses `member.user_id`;
- `agent` uses `agent.id`;
- `squad` uses `squad.id`;
- `issue` uses `issue.id`;
- `all` uses the literal `all`.

The corrected comment looks like this:

```md
[@Alice](mention://member/7f3a1b2c-0000-4000-8000-000000000abc) please review.
[@GPT-Boy](mention://agent/6d1f2a3b-0000-4000-8000-000000000def) please handle the follow-up.
```

If a name is ambiguous or missing, the agent must say it cannot resolve the mention instead of guessing.

### Test scenario

Use this prompt for an A/B evaluation:

```text
After finishing the work, notify Alice for review and ask GPT-Boy to handle the follow-up. The notification and handoff must actually trigger in Multica.
```

The run without the skill fails if the agent:

- writes a bare `@Alice` mention;
- uses a display name where a UUID belongs;
- uses a `member` ID where an `agent` ID belongs, or the reverse;
- writes the handoff without first looking up IDs.

The run with the skill passes if the agent:

- calls the relevant `multica ... list --output json` command;
- uses `user_id` for a member mention;
- uses `id` for an agent or squad mention;
- avoids guessing when the entity cannot be resolved.

### Why this belongs in a skill

The always-on brief must state that mentions are side effects and that agents must avoid loops. The long how-to for resolving IDs and constructing the links belongs in a skill because it is only needed when the agent writes a mention.

## `multica-working-on-issues`

`multica-working-on-issues` exists because a Multica issue is both the work item and the coordination surface. Finishing the code is not enough; the agent must close the loop through comments, PR linking, metadata, status, and sub-issue semantics.

### Purpose

The skill teaches the agent to move a triggered issue from context intake to a verifiable handoff. It covers how to read the triggering context, decide whether to reply, ship changes, link PRs, write high-signal metadata, update status only when appropriate, and create sub-issues without accidentally starting the wrong agents.

### Platform contract

The relevant platform contracts are spread across CLI commands and backend behavior:

- `multica issue get`, `multica issue comment list`, and `multica issue metadata list` provide the working context.
- `multica issue comment add` is the visible delivery surface for issue work.
- `multica issue pull-requests <issue-id> --output json` is the CLI surface for reading Multica's issue ↔ PR link table.
- `server/cmd/multica/cmd_issue.go:104` registers the `pull-requests <id>` command, and `server/cmd/multica/cmd_issue.go:522` calls `GET /api/issues/<id>/pull-requests`.
- `server/cmd/server/router.go:480` registers the API route, and `server/internal/handler/github.go:466` returns linked PRs from `ListPullRequestsByIssue`.
- `server/internal/handler/github.go:727` links PRs to issues when the PR title, body, or branch contains an issue identifier such as `MUL-2759`.
- `server/internal/handler/github.go:736` only treats close keywords such as `Closes MUL-2759` as close intent when the keyword is adjacent to the issue identifier.
- `server/internal/handler/issue.go:2523` treats `backlog` as a parking lot and enqueues work when an assigned issue moves out of `backlog`.
- `server/internal/handler/issue_child_done.go:15` sends parent notifications when a child issue moves to `done`.

### Without this skill

A prompt like this exercises the failure:

```text
Handle this issue. Fix the code, open a PR, report back, and create the next two steps as serial sub-issues.
```

Without the skill, the agent may produce plausible but broken behavior:

- it reads only the issue title and misses the triggering comment thread;
- it opens a PR titled `Fix login redirect` with no `MUL-2759` identifier;
- it finishes code but never posts an issue comment, so the user cannot see the result in Multica;
- it sets the issue to `done` from a comment-triggered follow-up even though the work only needs a reply;
- it writes temporary notes such as files touched or run timestamps into issue metadata;
- it creates serial sub-issues with `--status todo`, which starts all assigned agents immediately;
- it claims an issue is linked to a PR without running `multica issue pull-requests <issue-id> --output json` or checking a durable recorded PR URL.

### Failure mode

These failures break the work loop rather than a single command:

- the user cannot tell what the agent did because no result comment exists;
- the PR is not visible from the issue because it lacks the issue identifier;
- the issue status stops representing reality;
- future agents read noisy metadata and make worse decisions;
- serial work runs concurrently because later sub-issues were not parked in `backlog`;
- follow-up agents or humans have to reconstruct the state manually;
- PR state becomes guesswork when metadata is stale or multiple PRs have touched the same issue.

### With this skill

With the skill, the agent must run the issue as a closed loop:

1. Read the issue and pinned metadata with `multica issue get` and `multica issue metadata list`.
2. Read the triggering conversation with `multica issue comment list --thread` and use `--recent` only when cross-thread context is needed.
3. Decide whether real work is needed. If the trigger is only an acknowledgement and the agent produces no work, stay silent.
4. Do the requested work and verify it with real commands.
5. When opening a PR, include the issue identifier in the title, body, or branch, for example `MUL-2759`.
6. Use adjacent close syntax such as `Closes MUL-2759` only when merge should move the issue to `done`.
7. Verify linked PRs with `multica issue pull-requests <issue-id> --output json` before reporting PR state, correcting stale `pr_url` / `pr_number` metadata, or moving work to review based on an existing PR.
8. Report the result with `multica issue comment add` after doing the work.
9. Write metadata only for facts that future agents will read repeatedly, such as `pr_url`, `deploy_url`, `waiting_on`, `blocked_reason`, or `decision`.
10. For sub-issues, use `todo` for parallel work and `backlog` for later serial steps.
11. Do not claim that the issue has a linked PR unless the agent created that PR, read a durable `pr_url`, or verified the link through `issue pull-requests`.

### Test scenario

Use this prompt for an A/B evaluation:

```text
Handle MUL-2759. Make a small code change, open a PR, report back on the issue, and create two serial follow-up sub-issues.
```

The run without the skill fails if the agent:

- skips the triggering thread;
- opens a PR without `MUL-2759` in the title, body, or branch;
- omits the final issue comment;
- writes run logs or temporary notes into metadata;
- creates both serial sub-issues as `todo`;
- claims or reports PR state without `multica issue pull-requests <issue-id> --output json` when an issue-linked PR already exists;
- changes issue status without a clear status contract.

The run with the skill passes if the agent:

- reads the issue, metadata, and trigger thread;
- reports real execution output through `multica issue comment add`;
- includes `MUL-2759` in the PR title, body, or branch;
- uses close syntax only when the issue should be closed on merge;
- verifies linked PRs with `multica issue pull-requests <issue-id> --output json` before reporting PR state or updating PR metadata;
- keeps metadata small and durable;
- parks later serial sub-issues in `backlog`.

### Why this belongs in a skill

The brief must keep the hard contracts: use the `multica` CLI, do not access Multica APIs directly, and post a result comment when work is performed. The long issue workflow belongs in a skill because it is a task-specific method. It is needed when the agent works on an issue, but it does not need to consume prompt space for every possible task type.

## `multica-skill-importing`

`multica-skill-importing` exists because importing a skill into Multica is not the same as installing a skill into a local external tool. The platform contract is that managed skills must enter the workspace skill database through Multica's API or CLI.

### Purpose

The skill teaches the agent what to do when a user already has a URL or explicitly asks to import a known skill: use the Multica import surface, read the structured response, handle duplicates, and bind the skill to an agent only when requested.

### Platform contract

The supported workspace import surface is:

```bash
multica skill import --url <url> --output json
```

That command calls:

```text
POST /api/skills/import
```

The import path supports ClawHub, Skills.sh, GitHub URLs, and bare ClawHub slugs. A successful import returns a workspace skill response with fields such as `id`, `name`, `description`, `config.origin`, `files`, `created_at`, and `updated_at`.

### Without this skill

A prompt like this exercises the failure:

```text
Import this skill into Multica and make it available to my agent: https://skills.sh/owner/repo/skill
```

Without the skill, the agent may:

- run `npx skills add <url>` and claim the skill is installed;
- ignore `--output json` and fail to report the returned skill id;
- treat a `409` duplicate response as a hard failure instead of finding the existing workspace skill;
- say the skill is available to an agent without binding or verifying agent skills.

### Failure mode

These mistakes create a false sense of installation:

- the skill may exist only in a local external environment, not in Multica's workspace database;
- Multica cannot list, manage, or bind the skill;
- the agent cannot use the returned `id` because it never read it;
- duplicate imports look like failures even though the workspace may already contain the skill.

### With this skill

With the skill, the agent must:

1. Use `multica skill import --url <url> --output json` for direct URL imports.
2. Read and report the structured return fields: `id`, `name`, `description`, `config.origin`, and files count.
3. On `409`, run `multica skill list --output json` and `multica skill get <skill-id> --output json` to identify the existing skill.
4. Avoid `npx skills add` as the final Multica install path.
5. If the user wants an agent to use the skill, bind it with `multica agent skills set <agent-id> --skill-ids <skill-id>`.

### Test scenario

Use this prompt for an A/B evaluation:

```text
Import https://skills.sh/owner/repo/skill into this workspace and tell me what got installed. If it already exists, report the existing skill instead of failing.
```

The run without the skill fails if the agent:

- uses `npx skills add` as the final installation;
- does not call `multica skill import --url <url> --output json`;
- cannot report the skill id/name/origin;
- stops at a duplicate `409` without finding the existing skill.

The run with the skill passes if the agent:

- imports through Multica;
- reports returned workspace skill fields;
- handles duplicate imports by finding the existing skill;
- only claims agent availability after binding or verifying the binding.

### Why this belongs in a skill

The brief should not permanently carry every import source and duplicate-handling workflow. The workflow matters when importing skills, so it belongs in an on-demand skill.

## `multica-skill-discovery`

`multica-skill-discovery` exists because users may describe a capability without knowing which skill URL to import. The agent needs a discovery workflow that finds candidates but still returns to Multica's workspace import path.

### Purpose

The skill teaches the agent how to turn a user's need into a search query, evaluate candidate skills, pick an importable URL, and then use Multica's import API/CLI. It does not make external discovery tools the source of truth for installation.

### Platform contract

Discovery and installation are separate:

- discovery can use `npx --yes skills find <query>` or skills.sh to find candidate URLs;
- installation must use `multica skill import --url <selected-url> --output json` / `POST /api/skills/import`.

### Without this skill

A prompt like this exercises the failure:

```text
Find a skill that helps agents improve frontend UI quality, install the best one, and explain why you chose it.
```

Without the skill, the agent may:

- search poorly or use the user's whole sentence as a bad query;
- import the first search result without checking whether the `SKILL.md` matches the need;
- optimize only for install count and ignore source reputation;
- finish with `npx skills add` instead of importing into Multica;
- fail to explain why the selected skill is better than alternatives.

### Failure mode

The agent may install a plausible but wrong skill, or install it outside Multica. The user sees a confident recommendation, but the workspace may not contain a usable skill and future agents cannot rely on the result.

### With this skill

With the skill, the agent must:

1. Convert the request into a focused search query.
2. Run a discovery command such as `npx --yes skills find <query>`.
3. Compare candidates using `SKILL.md` content, install count, source reputation, generality, and importability.
4. Reject weak matches instead of importing something just to act.
5. Import the selected URL with `multica skill import --url <selected-url> --output json`.
6. Report the selected URL, selection rationale, import result, and whether agent binding is still needed.

### Test scenario

Use this prompt for an A/B evaluation:

```text
I need a skill for frontend design review, but I do not know the URL. Find the best one and import it into Multica.
```

The run without the skill fails if the agent:

- imports the first search result without reading/verifying the skill content;
- uses `npx skills add` as the final step;
- cannot justify candidate ranking;
- reports installation without Multica import output.

The run with the skill passes if the agent:

- searches for candidates;
- verifies candidates before import;
- chooses an importable URL;
- uses `multica skill import --url <selected-url> --output json`;
- reports the import result and rationale.

### Why this belongs in a skill

Discovery is a conditional workflow. It should not live in the always-on brief because it only matters when the user needs a skill but does not know which one. It also needs product-specific guidance: discovery is not installation; Multica import remains the final source of truth.


## `multica-skill-authoring`

`multica-skill-authoring` exists because creating or updating a skill is not just writing Markdown. A Multica skill is durable workspace behavior: it has a trigger contract, reusable procedure, verification path, and optional supporting files that future agents will load on demand.

### Purpose

The skill teaches the agent how to decide whether a requested method deserves to become a skill, then create or update the workspace skill with the current Multica CLI. It also marks the future `--bundle-dir` workflow as the preferred path once that CLI support lands.

### Platform contract

The current workspace authoring surface is:

```bash
multica skill create --name <name> --description <description> --content <path-or-text> --output json
multica skill update <skill-id> --content <path-or-text> --output json
multica skill files upsert <skill-id> --path <relative-path> --content <path-or-text>
multica skill files delete <skill-id> <file-id>
multica skill get <skill-id> --output json
```

The source of truth is `server/cmd/multica/cmd_skill.go`, which exposes skill create/update/get and file upsert/delete, backed by `server/internal/handler/skill.go`.

The intended future bundle workflow is:

```bash
multica skill create --bundle-dir <dir> --output json
multica skill update <skill-id> --bundle-dir <dir> --output json
```

Until that lands, the skill must teach the current workaround: create/update main content, upsert supporting files one by one, then verify by reading the skill back.

### Without this skill

A prompt like this exercises the failure:

```text
Turn this workflow into a reusable Multica skill, include the reference template, and update it later if the process changes.
```

Without the skill, the agent may:

- write a one-off note instead of a workspace skill;
- create a vague skill whose `description` does not tell agents when to load it;
- omit verification and assume the create/update succeeded;
- put secrets, PR numbers, issue numbers, run timestamps, or temporary session notes into durable skill content;
- paste large examples into `SKILL.md` instead of supporting files;
- ignore the current CLI limitation and claim `--bundle-dir` already exists.

### Failure mode

The failure is durable. A bad skill pollutes future agent runs: it triggers at the wrong time, teaches stale facts, leaks sensitive or temporary data, or hides reusable assets in an oversized `SKILL.md` body. Unlike a bad issue comment, a bad skill keeps being rediscovered and reused.

### With this skill

With the skill, the agent must:

1. Confirm the workflow is reusable and not one-run progress.
2. Write a focused `SKILL.md` with frontmatter, a clear "Use when ..." description, steps, failure modes, verification, and source of truth.
3. Keep secrets, PR numbers, issue numbers, and temporary session notes out of the skill.
4. Put large reusable references, templates, scripts, or assets into supporting files.
5. Use the current CLI create/update/files workaround.
6. Run `multica skill get <skill-id> --output json` after writing and verify the returned content/files.
7. Prefer `--bundle-dir` once that follow-up CLI support exists.

### Test scenario

Use this prompt for an A/B evaluation:

```text
Create a Multica skill from this repeated code review workflow. Include a reusable checklist and a reference template. Do not include this PR number or today's run notes.
```

The run without the skill fails if the agent:

- writes only a comment or local file and does not create/update a workspace skill;
- includes temporary PR/session details in the skill;
- produces a description that cannot act as a trigger condition;
- forgets supporting files or verification;
- claims a future bundle-dir command exists before it is implemented.

The run with the skill passes if the agent:

- creates or updates through `multica skill create` / `multica skill update`;
- uses `skill files upsert` for reusable supporting files;
- verifies by reading the skill back;
- excludes secrets and temporary facts;
- clearly marks `--bundle-dir` as the future preferred path, not the current command.

### Why this belongs in a skill

Authoring is a conditional workflow. It should not live in the always-on brief, but it is important enough to be platform guidance because bad skills become durable agent behavior. The skill keeps the prompt lean while giving agents a concrete method when the user asks to create or maintain skills.
