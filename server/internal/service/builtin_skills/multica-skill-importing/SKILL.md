---
name: multica-skill-importing
description: "Use when asked to import/install a specific skill into the Multica workspace from a hosted URL or a local archive."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Importing skills into Multica

Import only when the user gave a skill URL, slug, or clear import intent.
Do not pick what skill the user needs — external search may produce candidate
URLs, but import starts only once a concrete target is known. Source-traced in
`references/skill-importing-source-map.md`.

## Invariant

A skill is installed only via the workspace import endpoint (skill DB),
driven by the CLI:

```bash
multica skill import --url <url> --output json
multica skill import --file <path-to.skill> --output json
```

CLI defaults `--on-conflict fail`. URL import sends `POST /api/skills/import`
(`Content-Type: application/json`, body `{ "url": "<url>", "on_conflict":
"fail" }`); `--file` hits the same route via `multipart/form-data` (a `file`
part with the archive bytes + `on_conflict`). `--url`/`--file` mutually
exclusive; exactly one required.

Never treat a local installer like `npx skills add` as the final Multica
install — it writes to an external/local skill environment, not the
workspace DB.

## URL sources

```bash
multica skill import --url clawhub.ai/owner/skill --output json
multica skill import --url skills.sh/owner/repo/skill --output json
multica skill import --url github.com/owner/repo --output json
multica skill import --url github.com/owner/repo/tree/main/path/to/skill --output json
multica skill import --url github.com/owner/repo/blob/main/path/to/SKILL.md --output json
```

- `detectImportSource` hosts: `clawhub.ai`, `skills.sh`, `github.com`.
- GitHub URL: bare `owner/repo`, `/tree/{ref}/...` dir, or `/blob/{ref}/.../SKILL.md`.
- Bare ClawHub slug (no host) accepted; any other host → 400 naming sources.

## Local archive import

`multica skill import --file <path> --output json` imports from a local
archive: a `.skill` file (the standard zip that Anthropic's
skill-creator `package_skill` produces) or a plain `.zip` of a skill folder.
Server accepts `multipart/form-data` on the same route, roots at the
shallowest `SKILL.md` (root-level or nested `my-skill/` wrapper), takes
name/description from frontmatter (fallback: wrapper dir, then filename), and
carries supporting files, dropping `SKILL.md`, `__MACOSX` entries and
anything escaping the bundle (zip-slip). Limits: per-file 1 MiB, per-bundle 8 MiB, 256 files;
`--url` imports capped at 16 MiB.

## Result envelope

Treat the response as source of truth:

```json
{
  "status": "created|updated|conflict|skipped|failed",
  "reason": "...",
  "skill": { "...": "SkillWithFilesResponse created/updated" },
  "existing_skill": { "id": "...", "name": "...", "can_overwrite": true }
}
```

For `created`/`updated`, `skill` is the `SkillWithFilesResponse`
(`SkillResponse` + `files` array). Report: `status`/`reason`;
`skill.id`/`name`/`description`; `skill.config.origin` (provenance, possibly
absent); `skill.files` count; `created_at`/`updated_at`;
`existing_skill.id`/`name` when `conflict`, `skipped`, or `failed`. Read the
returned fields — don't guess.

## Agent-skill binding

Binding is separate and mutable: `add` preserves (`AddAgentSkills`),
`set` is the replacement path — replaces all (`SetAgentSkills`, i.e.
replace-all, no legacy merge):

```bash
multica agent skills add <agent-id> --skill-ids <skill-id> --output json
multica agent skills list <agent-id> --output json
```

After the final `multica agent skills list <agent-id> --output json`, verify
the target skill id is present before claiming the skill is available.

## Reserved supporting filename

`SKILL.md` is reserved for primary content (`IsReservedContentPath`,
case-insensitive: `./SKILL.md`, `sub/../SKILL.md` caught). A supporting file
named `SKILL.md` is silently dropped on import — absent from returned
`files`; rename it. (The hard 400 fires only on `PUT /api/skills/{id}/files`.)

## Same-name conflicts: `--on-conflict`

`multica skill import --url <url>` ≡ `--on-conflict fail`: on conflict it
prints a structured `conflict` result and exits non-zero; nothing is created
or updated. Explicit strategy only when asked:

- `fail` (default): `status: conflict`; reason suggests overwrite/rename.
- `overwrite`: update in place only for the original creator — preserves
  skill ID, `created_by`, bindings; non-creators get 400/403.
- `rename`: keep existing; import copy under `-2`/`-3` suffix.
- `skip`: leave untouched; `status: skipped`.

```bash
multica skill import --url https://skills.sh/acme/repo/review-helper --output json
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict overwrite --output json
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict rename --output json
multica skill import --url https://skills.sh/acme/repo/review-helper --on-conflict skip --output json
```

legacy: clients without `on_conflict` keep the old contract — duplicate
import returns `409` with `{ "error": "a skill with this name already exists",
"existing_skill": { "id": "<skill-id>", "name": "<skill-name>" } }`.
Current CLI normalizes it to `status: conflict` and exits non-zero under
`fail`. `existing_skill` = source of truth; fetch via `multica skill get
<skill-id> --output json`. Older servers: bare `409` string with no
`existing_skill` — find the skill yourself (`multica skill list --output json`,
`multica skill get <skill-id> --output json`).

## Incorrect → Correct

- Incorrect: `--on-conflict overwrite` on a skill you did not create — fails;
  skill untouched.
- Incorrect: `set` to add — wipes other assignments; use `add`.
- Correct: `multica skill import --url https://skills.sh/owner/repo/skill --output json`;
then `multica agent skills add <agent-id> --skill-ids <skill-id> --output json`
+ `multica agent skills list <agent-id> --output json`. Fetch details:
`multica skill get <skill-id> --output json`.

## References

- `references/skill-importing-source-map.md` — behavior mapped to `file:line`
  in `server/`, with a verification command to re-derive lines.
