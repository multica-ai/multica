---
name: multica-skill-authoring
description: Use when a user asks to create, edit, or maintain a Multica workspace skill. Teaches the current CLI workflow for SKILL.md content, metadata, supporting files, verification, and the future bundle-dir path without treating one-off notes as durable skills.
user-invocable: false
allowed-tools: Bash(multica *)
---

# Authoring Multica skills

Use this skill when the user asks to create, update, or maintain a Multica
workspace skill. This is different from finding an existing skill
(`multica-skill-discovery`) or importing a known URL (`multica-skill-importing`).

## The invariant

A Multica skill is durable workspace behavior. It should capture a reusable method,
platform rule, or tool workflow that future agents should apply on demand.

Do not create a skill for one-run progress, temporary TODOs, PR numbers, issue
numbers, session summaries, transient decisions, credentials, API keys, or other
secrets. Those belong in issue comments, issue metadata, project docs, or not at
all.

## Authoring standard

Every `SKILL.md` must have clear frontmatter and body guidance:

```md
---
name: short-slug
description: Use when ...
---

# Skill title

Use this skill when ...
```

The `description` is the trigger contract. Write it as a concise "Use when ..."
sentence so the agent can decide whether to load the skill before seeing the
body.

The body should cover:

- when to use the skill and when not to use it;
- the exact commands or APIs to run;
- verification steps that prove the work succeeded;
- failure modes and recovery rules;
- source of truth links to code, API, CLI, docs, or product behavior.

Keep the main body focused. Put large examples, templates, or reference material
in supporting files instead of bloating `SKILL.md`.

## Current create flow

Until bundle directory support exists, create the workspace skill from explicit
CLI fields:

```bash
multica skill create --name <name> --description <description> --content <path-or-text> --output json
```

Read the JSON response and keep the returned `id`. Do not claim the skill exists
until the create command succeeds.

If the content lives in a local `SKILL.md`, read the file first and pass its full
content as the `--content` value. The current CLI does not yet have
`--content-file` or `--bundle-dir`, so large content may require a wrapper script
or shell-safe command construction.

## Current update flow

Update an existing skill with the skill id or supported identifier:

```bash
multica skill update <skill-id> --content <path-or-text> --output json
```

Use `--name`, `--description`, or `--config` only when those fields actually need
to change. Avoid rewriting unrelated fields.

After update, verify by reading it back:

```bash
multica skill get <skill-id> --output json
```

Compare the returned `name`, `description`, `content`, `config`, and `files`
against what you intended.

## Supporting files

Use supporting files for reusable references, templates, scripts, and assets that
are too large or too specific for the main `SKILL.md`.

Current workaround for supporting files:

```bash
multica skill files upsert <skill-id> --path <relative-path> --content <path-or-text>
multica skill files delete <skill-id> <file-id>
multica skill get <skill-id> --output json
```

Recommended relative paths are stable, portable paths such as:

```text
references/<topic>.md
templates/<name>.md
scripts/<name>.sh
assets/<name>.<ext>
```

Do not store secrets in supporting files. Do not store one-off PR numbers, issue
numbers, run timestamps, or temporary session state. If the fact will be stale in
a week, it is not skill content.

## Future bundle directory path

When the CLI supports bundle directories, prefer the atomic local bundle flow:

```bash
multica skill create --bundle-dir <dir> --output json
multica skill update <skill-id> --bundle-dir <dir> --output json
```

Expected bundle shape:

```text
<dir>/SKILL.md
<dir>/references/...
<dir>/templates/...
<dir>/scripts/...
<dir>/assets/...
```

Until `--bundle-dir` lands, use the current workaround: create/update the main
content, then upsert supporting files one by one, then verify by reading it back.

## Quality gate

Before creating or updating a skill, check:

1. Is this reusable across future runs?
2. Is the trigger condition clear from the description alone?
3. Does it cite a real source of truth instead of relying on vibes?
4. Does it include verification steps?
5. Does it avoid secrets, temporary progress, PR numbers, issue numbers, and stale
   session notes?
6. Are large examples moved into supporting files?

If the answer is no, do not create the skill yet. Write an issue comment or a doc
instead.

## Source of truth

- `server/cmd/multica/cmd_skill.go` implements `multica skill create`,
  `multica skill update`, `multica skill get`, and `multica skill files upsert`.
- `server/internal/handler/skill.go` implements the workspace skill API.
- `docs/agent-skills/skill-necessity.md` records why built-in platform skills
  exist and how to evaluate whether they work.
