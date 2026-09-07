# Importing skills

Use this when the user already provided a skill URL, slug, or a clear intent to
import a specific skill into the current Multica workspace. It does not help you
decide WHICH skill the user needs — if they only described a capability and no
URL is known, external search may produce candidates, but importing starts only
once a URL or concrete target exists.

- [The invariant](#the-invariant)
- [Supported URL source families](#supported-url-source-families)
- [Local archive import](#local-archive-import)
- [Direct URL flow](#direct-url-flow)
- [Additive add vs replace-all set](#additive-add-vs-replace-all-set)
- [Reserved SKILL.md supporting file](#reserved-skillmd-supporting-file)
- [Same-name conflicts: --on-conflict](#same-name-conflicts---on-conflict)
- [Updating an already-imported skill](#updating-an-already-imported-skill)
- [Reading a skill back](#reading-a-skill-back)
- [Incorrect to correct](#incorrect-to-correct)

## The invariant

A skill is installed for Multica only when it exists in the current workspace's
skill database. The single supported path that puts it there is the workspace
import endpoint. It accepts either a hosted URL or an uploaded local archive
(`.skill` / `.zip`), driven by this CLI:

```bash
multica skill import --url <url> --output json              # hosted source
multica skill import --file <path-to.skill> --output json   # local archive
```

The CLI defaults to `--on-conflict fail`. A URL import sends:

```text
POST /api/skills/import
Content-Type: application/json
body: { "url": "<url>", "on_conflict": "fail" }
```

A `--file` import hits the same route as `multipart/form-data` with a `file`
part (the `.skill` / `.zip` bytes) and an `on_conflict` field. `--url` and
`--file` are mutually exclusive; exactly one is required.

Do not finish with `npx skills add`. That installs into an external/local skill
environment, not the Multica workspace DB, so Multica cannot manage or bind it.

## Supported URL source families

The import endpoint accepts these hosts (and `www.` variants). Pass any of these
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

## Local archive import

`multica skill import --file <path> --output json` imports a skill from a local
archive instead of a hosted URL. A `.skill` file is a standard zip — the format
Anthropic's skill-creator `package_skill` produces — and a plain `.zip` of a
skill folder works too. The server:

- accepts the upload as `multipart/form-data` (a `file` part plus an optional
  `on_conflict` field) on the same `POST /api/skills/import` route;
- decompresses it and roots on the shallowest `SKILL.md`, so both a root-level
  `SKILL.md` and the nested `my-skill/SKILL.md` wrapper layout are accepted;
- takes the name/description from `SKILL.md` frontmatter, falling back to the
  wrapper directory name and then the uploaded filename;
- carries the supporting files — dropping any `SKILL.md`, dotfiles, `__MACOSX`,
  license files, and binary assets — under the same per-file (1 MiB),
  per-bundle (8 MiB), and file-count (256) caps as URL imports, and rejects path
  traversal (zip-slip);
- returns the same structured result envelope and honors the same
  `--on-conflict` strategies as URL imports.

The upload itself is capped at 16 MiB (compressed). Any source that is not a
local archive still goes through `--url`.

## Direct URL flow

1. When the request contains a concrete URL, the import endpoint can be called
directly; search is not required by the API:

```bash
multica skill import --url <url> --output json
```

2. Treat the response as the source of truth. Current CLI imports use the
structured import result envelope:

```json
{
  "status": "created|updated|conflict|skipped|failed",
  "reason": "...",
  "skill": { "...": "the workspace skill, with its files, when created/updated" },
  "existing_skill": { "id": "...", "name": "...", "can_overwrite": true }
}
```

For `created` / `updated`, `skill` is the workspace skill record plus a `files`
array of its supporting files. Report the relevant fields:

- `status` and `reason` when present.
- `skill.id` / `skill.name` / `skill.description`.
- `skill.config.origin` (provenance: which source the skill was imported from —
  set only when the source supplied an origin, so treat it as possibly absent).
- `skill.files` / files count.
- `skill.created_at` / `skill.updated_at`.
- `existing_skill.id` / `existing_skill.name` when status is `conflict`,
  `skipped`, or `failed` due to an existing skill.

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

`multica agent skills add` is additive: the server inserts the assignments
without clearing existing ones.

`multica agent skills set` is replace-all: the server clears every current
assignment, then re-adds exactly the ids you pass. `set` is the replacement
path. Passing only one id to `set` leaves the agent with only that one skill and
drops every previous assignment.

## Reserved SKILL.md supporting file

A skill's primary content is its `SKILL.md`. That filename is reserved: the
daemon writes the primary content to `SKILL.md` itself when preparing the
execution environment, so a *supporting* file may not also be named `SKILL.md`.
The check cleans the path and is case-insensitive, so `./SKILL.md` and
`sub/../SKILL.md` are caught too.

Practical effect when importing or creating a skill: if the manifest lists a
supporting file named `SKILL.md`, the server silently drops it — the import still
succeeds, but that entry will be absent from the returned `files`. So if a
supporting file you expected is missing, check whether it was named `SKILL.md`;
rename it to a non-reserved path. (The hard `400` rejection — "SKILL.md is reserved
for the primary skill content" — only fires on the dedicated single-file endpoint
`PUT /api/skills/{id}/files`, not on import.)

## Same-name conflicts: --on-conflict

Default behavior is safe: `multica skill import --url <url>` is equivalent to
`--on-conflict fail`. If the imported skill name already exists, the command
prints a structured `conflict` result and exits non-zero; no skill is created or
updated.

Choose an explicit strategy only when the user asked for it or the intent is
clear:

- `--on-conflict fail` (default): do nothing on conflict; report `status:
  conflict` with a reason that suggests overwrite or rename.
- `--on-conflict overwrite`: update the existing same-name skill in place, but
  only if the current user is the skill's original creator. This preserves the
  skill ID, `created_by`, `created_at`, and agent-skill bindings; it replaces
  description, content, provenance config, and supporting files. Non-creators get
  `status: failed`.
- `--on-conflict rename`: create a new skill with an automatic suffix such as
  `-2` / `-3`; the existing skill is untouched.
- `--on-conflict skip`: leave the existing skill untouched and report `status:
  skipped`.

Concrete examples:

```bash
# Safe default. Fails with status=conflict if review-helper already exists.
multica skill import --url https://skills.sh/acme/repo/review-helper --output json

# Replace the existing same-name skill, preserving its ID and agent bindings.
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict overwrite --output json

# Keep the existing skill and import a copy such as review-helper-2.
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict rename --output json

# Batch-friendly behavior: leave the existing skill alone and mark it skipped.
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict skip --output json
```

Legacy compatibility: clients that do not send `on_conflict` keep the old
contract. A duplicate import returns `409` and the body carries the existing
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

Current CLI normalizes that legacy shape into `status: conflict` and exits
non-zero for the default `fail` strategy. Treat `existing_skill.id` and
`existing_skill.name` as the source of truth, then fetch details if needed:

```bash
multica skill get <skill-id> --output json
```

Older servers may return a `409` whose body is only a string like `a skill with
this name already exists`, with no `existing_skill` key. Recover by finding the
existing workspace skill yourself:

```bash
multica skill list --output json
multica skill get <skill-id> --output json
```

Then report that the skill already exists and include its `id` / `name`. Do not
retry in a loop, and do not create a second skill under a different name just to
dodge the conflict.

## Updating an already-imported skill

When the goal is to pull the latest version of a skill that is already in the
workspace, prefer `refresh` over re-typing the import URL:

```bash
multica skill refresh <skill-id> --output json
```

It sends `POST /api/skills/{id}/refresh` (empty body). The server re-downloads
the bundle from the provenance stored in the skill's `config.origin`
(`github`, `skills_sh`, or `clawhub` with its `source_url`) and overwrites the
skill in place:

- Preserved: skill ID, `created_by`, `created_at`, agent-skill bindings, and
  labels — agents keep the skill without re-adding it.
- Replaced: name (an upstream rename is adopted), description, `SKILL.md`
  content, provenance config, and the full supporting-file set.
- Permission: the skill's creator or a workspace `owner`/`admin` — broader than
  `import --on-conflict overwrite`, which stays creator-only.

The success response is the plain skill-with-files shape (same as
`multica skill get --with-content`), not the import result envelope. Failure
modes to report instead of retrying in a loop:

- `422`: the skill has no refreshable provenance (created manually, imported
  from a local archive, or copied from a local runtime). Re-import instead.
- `409`: the upstream renamed the skill and another workspace skill already
  uses that name; nothing was changed.
- `403`: caller is neither the creator nor a workspace admin.
- `502` / `503` / `504` / `413`: upstream fetch failed (gone, unavailable,
  timed out, or now exceeds import caps); the skill is left untouched.

## Reading a skill back

`multica skill get` and `multica skill files list` return **metadata only** —
path, byte size and content hash per file, plus the size and hash of the
SKILL.md body. That is what the conflict and verification steps above need, and
it is what keeps a large skill inspectable at all: inlining every body made a
~600KB skill impossible to fetch over a slow link.

```bash
multica skill get <skill-id> --output json                  # metadata
multica skill get <skill-id> --with-content --output json   # bodies inlined
multica skill files list <skill-id>                         # paths and sizes
```

Reach for `--with-content` only when you are going to read the content. To find
out why a skill is large, the default output already answers it: the `size`
column names the file.

## Incorrect to correct

Incorrect (bypasses Multica):

```bash
npx skills add https://skills.sh/owner/repo/skill
```

The skill may exist locally, but Multica cannot manage it as a workspace skill.

Incorrect agent binding for a normal add (replaces every existing assignment):
using `set` with only the new skill id wipes the agent's other skills. For an
add, use `add`.

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
