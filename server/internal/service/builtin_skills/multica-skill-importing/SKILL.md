---
name: multica-skill-importing
description: Use when a user provides a skill URL, slug, or clear intent to import/install a specific skill into the current Multica workspace. Teaches the workspace import API/CLI path (POST /api/skills/import), the supported URL source families, the SkillWithFilesResponse shape returned on success, duplicate 409 handling with the existing_skill body, additive agent binding vs replace-all, and the reserved SKILL.md supporting-file rule. Do not use it to decide which skill the user needs, and never treat an external local installer like npx skills add as the final Multica install.
user-invocable: false
allowed-tools: Bash(multica *)
---

# Importing skills into Multica

Use this skill when the user already provided a skill URL, slug, or a clear intent
to import a specific skill into the current Multica workspace.

Do not use this skill to decide which skill the user needs. If the user only
describes a capability and no URL is known, external search may produce candidate
URLs, but this import skill starts only once a URL or concrete import target is
known.

Every claim below is traced to source in
`references/skill-importing-source-map.md`. When in doubt, read that file.

## The invariant

A skill is installed for Multica only when it exists in the current workspace's
skill database. The single supported path that puts it there is the workspace
import endpoint, driven by this CLI:

```bash
multica skill import --url <url> --output json
```

That CLI sends:

```text
POST /api/skills/import
body: { "url": "<url>" }
```

Do not finish with `npx skills add`. That installs into an external/local skill
environment, not the Multica workspace DB, so Multica cannot manage or bind it.

## Supported URL source families

`detectImportSource` accepts these hosts (and `www.` variants). Pass any of these
forms to `multica skill import --url <url> --output json`:

```bash
multica skill import --url clawhub.ai/owner/skill --output json
multica skill import --url skills.sh/owner/repo/skill --output json
multica skill import --url github.com/owner/repo --output json
multica skill import --url github.com/owner/repo/tree/main/path/to/skill --output json
multica skill import --url github.com/owner/repo/blob/main/path/to/SKILL.md --output json
```

- `clawhub.ai`, `skills.sh`, `github.com` are the recognized hosts.
- A GitHub URL may be a bare `owner/repo`, a `/tree/{ref}/...` directory, or a
  `/blob/{ref}/.../SKILL.md` file.
- A bare ClawHub slug (no host) is accepted and routed to ClawHub.
- Any other host is rejected with a 400 naming the supported sources.

## Direct URL flow

1. When the request contains a concrete URL, the import endpoint can be called
directly; search is not required by the API:

```bash
multica skill import --url <url> --output json
```

2. Treat a successful response as the source of truth. The body is a workspace
`SkillWithFilesResponse` — it embeds the standard `SkillResponse` and adds the
supporting `files` array. Report the relevant fields:

- `id`
- `name`
- `description`
- `config.origin` (provenance: which source the skill was imported from — set
  only when the source supplied an origin, so treat it as possibly absent)
- `files` / files count
- `created_at` / `updated_at`

Because the response is structured, read these returned fields instead of guessing
whether the import succeeded.

3. Agent-skill binding is a separate mutable operation. `add` preserves existing
assignments and appends the new id:

```bash
multica agent skills add <agent-id> --skill-ids <skill-id> --output json
multica agent skills list <agent-id> --output json
```

After the final `multica agent skills list <agent-id> --output json`, verify the
target skill id is present before claiming the skill is available to that agent.

## Additive add vs replace-all set

`multica agent skills add` is additive: the server inserts the assignments without
clearing existing ones (`AddAgentSkills`).

`multica agent skills set` is replace-all: the server clears every current
assignment, then re-adds exactly the ids you pass (`SetAgentSkills`).
`set` is the replacement path. Passing only one id to `set` leaves the agent with
only that one skill and drops every previous assignment.

## Reserved SKILL.md supporting file

A skill's primary content is its `SKILL.md`. That filename is reserved: the daemon
writes the primary content to `SKILL.md` itself when preparing the execution
environment, so a *supporting* file may not also be named `SKILL.md`
(`IsReservedContentPath`; the check cleans the path and is case-insensitive, so
`./SKILL.md` and `sub/../SKILL.md` are caught too).

Practical effect when importing or creating a skill: if the manifest lists a
supporting file named `SKILL.md`, the server silently drops it — the import still
succeeds, but that entry will be absent from the returned `files`. So if a
supporting file you expected is missing, check whether it was named `SKILL.md`;
rename it to a non-reserved path. (The hard `400` rejection — "SKILL.md is reserved
for the primary skill content" — only fires on the dedicated single-file endpoint
`PUT /api/skills/{id}/files`, not on import.)

## Duplicate imports (409)

A duplicate import returns `409`. On current servers the body carries the existing
workspace skill identity:

```json
{
  "error": "a skill with this name already exists",
  "existing_skill": {
    "id": "<skill-id>",
    "name": "<skill-name>"
  }
}
```

`multica skill import --url <url> --output json` prints that structured conflict
body and exits successfully (exit 0) for this duplicate case. Treat
`existing_skill.id` and `existing_skill.name` as the source of truth, then fetch
details if needed:

```bash
multica skill get <skill-id> --output json
```

Legacy fallback: a legacy server or old CLI may return a `409` whose body is only a
string like `a skill with this name already exists`, with no `existing_skill` key.
The CLI cannot recognize that as the duplicate case, so it exits non-zero. Recover
by finding the existing workspace skill yourself:

```bash
multica skill list --output json
multica skill get <skill-id> --output json
```

Then report that the skill already exists and include its `id` / `name`. Do not
retry in a loop, and do not create a second skill under a different name just to
dodge the conflict.

## Incorrect → correct

Incorrect (bypasses Multica):

```bash
npx skills add https://skills.sh/owner/repo/skill
```

The skill may exist locally, but Multica cannot manage it as a workspace skill.

Incorrect agent binding for a normal add (replaces every existing assignment):

Using `set` with only the new skill id wipes the agent's other skills. For an add,
use `add`.

Correct import:

```bash
multica skill import --url https://skills.sh/owner/repo/skill --output json
```

Agent binding after import, when the caller intentionally wants to mutate that
agent's skill assignments:

```bash
multica agent skills add <agent-id> --skill-ids <skill-id> --output json
multica agent skills list <agent-id> --output json
```

## References

- `references/skill-importing-source-map.md` — every behavior above mapped to
  `file:line` in `server/`, plus the verification command to re-derive the lines.
