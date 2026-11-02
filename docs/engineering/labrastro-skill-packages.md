# Skill packages: OL-103 server contract

OL-103 implements the server portion of OL-102 against fork baseline
`803cfcf18a7db98ee45e7ac10367b12a4c773711`. OL-104 owns the shared web/desktop
flows; OL-105 owns the CLI. This contract does not install plugins, runtimes,
dependencies, hooks, commands, or executable bits. Existing skill-local text
support files, including scripts and `agents/openai.yaml`, remain supported.

## Request and response contract

Use the existing authenticated API client and `X-Workspace-ID`. All IDs below
are workspace-scoped UUIDs. Members may read the tree, import packages and
manage custom folders. Package importer or owner/admin permission is needed
for package mutations. Skill creator or owner/admin permission is separately
required to adopt, update, move, detach, or delete a skill. Overwriting a skill
from a different source remains creator-only, including for administrators.
All 14 new routes reject external-conversation tokens with HTTP 403.

The schemas and inferred types are in
`packages/core/api/labrastro-skill-schemas.ts`, re-exported by
`@multica/core/api/schemas`. Consumers must use `parseWithFallback` with `null`
for malformed package/tree/write responses and display an indeterminate
result, not claim success or automatically repeat a write. Unknown candidate
states cannot be selected; unknown item statuses make the result unsuccessful.

### Preview and apply

`POST /api/skill-packages/preview`:

```json
{"url":"https://github.com/addyosmani/agent-skills"}
```

Accepted sources: HTTPS GitHub repository or `tree/<ref>/<directory>` URLs,
and two-component `https://skills.sh/owner/repo` URLs. No credentials, query,
fragment, or blob URL. Ref names containing slashes use the existing GitHub
ref resolver. The preview's canonical URL includes the resolved ref.

Example response (IDs, token and digest abbreviated):

```json
{
  "preview_id":"signed-opaque-token",
  "source":{
    "url":"https://github.com/addyosmani/agent-skills/tree/main",
    "owner_repo":"addyosmani/agent-skills",
    "subdirectory":"",
    "ref":"main",
    "revision":"request-local-commit-sha"
  },
  "candidates":[{
    "path":"skills/shipping-and-launch",
    "name":"shipping-and-launch",
    "description":"...",
    "default_selected":true,
    "state":"new",
    "can_write":true,
    "digest":"sha256",
    "file_count":4,
    "bytes":12345,
    "shared_files":["_shared/references/definition-of-done.md"],
    "diagnostics":[]
  }],
  "diagnostics":[]
}
```

`file_count` counts supporting files; `bytes` includes primary content and
supporting files. Both reflect final rewritten content. `shared_files` lists
materialized `_shared/` output paths. A repository-root candidate has `path:""`.
There are no content bodies in the preview. `skill_id` and `conflict` appear
when relevant. Candidate states: `new`, `adoptable`, `changed`, `unchanged`,
`removed`, `conflict`, `failed`. A common-prefix change that requires moving
an imported skill also marks it `changed`; the same skill permission gate
applies to that change.

Preview is strictly read-only, including package candidates and timestamps.
Manifest `skills` lists or directory values select defaults. A missing manifest
or valid manifest object without the optional `skills` key selects all discovered
candidates. An explicit empty list selects none. Malformed JSON, a non-object
manifest, a `skills` value other than a string/list of strings (including `null`
or null list entries), or paths outside the repository select none and report
`manifest_invalid`.
Ordinary paths win over same-name dot-directory mirrors. Other duplicates
remain visible. Failed candidates are unselected and carry diagnostics.

`can_write` describes the existing target operation, not whether rename may
create a separate skill. For `adoptable`/`changed`/`unchanged`, it means the caller
is the skill creator or an administrator. For `name_conflict`, it means the
caller is the conflicting skill's creator **and** that skill is not already in
a package, so overwrite is allowed. Rename does not require `can_write:true`.
For a new candidate it is true; failed, removed, ambiguous-source and
already-packaged candidates have it false. Apply always rechecks permissions.

`POST /api/skill-packages/apply`:

```json
{
  "url":"https://github.com/addyosmani/agent-skills/tree/main",
  "preview_id":"signed-opaque-token",
  "skills":["skills/shipping-and-launch"],
  "on_conflict":"skip"
}
```

`skills` and `all:true` are mutually exclusive. Omitted `skills` uses preview
defaults; `skills:[]` explicitly selects nothing. `all:true` selects every
candidate, including failed candidates so their failures enter the report.
`on_conflict` is `skip` (default), `rename`, or `overwrite`. Source identity and
ownership come from the server; client IDs, contents or permission claims are
not accepted. Unknown request fields are rejected.

Selected writable candidates use the explicit decision table in
`server/internal/handler/labrastro_skill_decisions.go`:

| Candidate state | Conflict | Strategy | Action | Skill permission |
|---|---|---|---|---|
| `new` | none | any | Create | Workspace member |
| `changed`, `adoptable` | none or `forbidden` | any | Update or adopt the same source | Creator or owner/admin, checked under lock |
| `conflict` | `name_conflict` | `skip` | Skip | None |
| `conflict` | `name_conflict` | `rename` | Create with an available name | Workspace member; existing skill is untouched |
| `conflict` | `name_conflict` | `overwrite` | Replace the unbound target | Target creator only |
| `conflict` | `ambiguous_source`, `already_packaged` | `skip` | Skip | None |
| `conflict` | `ambiguous_source`, `already_packaged` | `rename`, `overwrite` | Fail | No write allowed |
| `failed` | none | any | Report source failure | No write allowed |

Default selection skips forbidden same-source changes; explicit selection
reports a permission failure. Removed candidates are retained, unchanged
candidates remain unchanged, and unselected candidates are skipped before this
table is applied. Unknown combinations fail closed. Package authorization is
independent of the skill permission column and is rechecked for each write.
Rename checks availability inside each item's transaction, including collisions
between two newly discovered candidates in the same apply.

Source identities (including legacy skills.sh slug matches) are indexed once
per snapshot. Apply revalidates the full preview under the workspace lock, then
each item rechecks only its target skill state, supporting-file hashes and
placement. No target identity or permission is accepted from the client.

```json
{
  "package":{
    "id":"package-uuid", "workspace_id":"workspace-uuid",
    "owner_repo":"addyosmani/agent-skills", "subdirectory":"",
    "source_url":"https://github.com/addyosmani/agent-skills/tree/main",
    "ref":"main", "root_folder_id":"folder-uuid", "created_by":"user-uuid",
    "revision":1,
    "candidates":[{"path":"skills/shipping-and-launch","name":"shipping-and-launch","description":"...","digest":"sha256"}]
  },
  "results":[{
    "path":"skills/shipping-and-launch", "status":"created",
    "skill_id":"skill-uuid", "retryable":false, "diagnostics":[]
  }],
  "failed":false,
  "diagnostics":[]
}
```

Results cover all candidates, not just the selected set. Item statuses:
`created`, `adopted`, `updated`, `unchanged`, `skipped`, `retained`, `failed`.
Items may include `code` and `reason`. `failed:true` means at least one item
failed; the HTTP status is still 200. CLI must print the entire report before
returning nonzero. `skipped/not_selected` is not a failure. Clients must show
candidate failure diagnostics even when those candidates were not selected.

The package metadata transaction commits before item transactions. Each item
commits content, support files, managed folders and placement together.
Failure leaves that item's prior contents intact; other items remain committed.
An apply with selected items can therefore leave an empty package if all fail.
An empty selection on a new source creates neither package nor folders.

Tokens expire after 15 minutes and bind caller, workspace, resolved source
commit, candidate summaries, permissions and workspace skill/tree state.
Apply rescans before writing. A changed source/target, role change or expired
token returns 409 `preview_stale`. Conservative invalidation also catches an
unrelated skill/tree mutation in the same workspace. Concurrent writes during
an item are rechecked under locks, including its content and placement.
Never replay an old token: preview again, inspect results, and select the
remaining items. There is no background continuation.

Before any writes, an interrupted candidate scan returns 504
`source_timeout` (deadline or request cancellation), including when the last
candidate download fails. A disconnected client may not receive the response.
With a valid, unexpired token, a fingerprint mismatch accompanied by retryable
candidate download failures returns 503 `source_unavailable`: the server could
not complete revalidation and cannot infer a source/permission change from that
failure. Successful scans still detect genuine source, permission and target
changes as 409. Invalid/expired tokens remain 409 even during a source outage.
These pre-write failures leave skills, files, folders, placements and package
metadata untouched.

A matching preview that already contained failed candidates can still apply:
selected failures enter the per-item report and other items may succeed. Once
the package metadata transaction has committed, timeout handling remains in
the item loop and returns the complete HTTP 200 report with `failed:true`;
already committed successes remain saved. Neither error path retries writes.

### Existing packages and rescans

`GET /api/skill-packages` returns `{"packages":[<package>]}`;
`GET /api/skill-packages/{packageId}` returns one package. Candidates are the
last applied path/name/description/digest summary, not a live remote inventory.
Combine it with placements to show candidates that have not been imported.

`POST /api/skill-packages/{packageId}/rescan` with `{}` returns a preview using
the saved URL/ref. To apply, repeat with `{"apply":true,"preview_id":"..."}`.
The same `skills`, `all`, and `on_conflict` fields apply. A URL override may
change ref but not owner/repository/subdirectory; preview displays the proposed
source beside the currently saved package. Apply persists the new ref. Commit
SHAs are only transient validation data, never saved as a long-term version.

Reimporting the same normalized owner/repository/subdirectory finds this
package, regardless of URL spelling or ref. Default rescan selection is only
changed imported items. New or detached candidates require explicit selection.
Removed source paths are `removed` in preview and `retained/source_removed` in
apply. Source directory renames are removal plus new candidate; the new candidate
may be imported with rename while the old skill remains in place. A same-name
skill alone is a `name_conflict`, even if it belongs to another package or an
old source path in this package. Rename creates a separate skill; overwrite
cannot transfer a packaged target.

Local display-name changes, including suffixes assigned by conflict rename,
do not make an imported candidate `changed`. Content/support-file and layout
changes still do. Content updates and same-source readoption retain that local
name while the source name is unchanged. To detect an upstream rename, apply compares the new source name
with the frontmatter (or inferred directory/repository name) of the skill's
persisted `SKILL.md`. That content only advances after a successful item commit;
the package's candidate summary can advance even when an item is deselected or
fails. Thus a skipped/failed upstream rename is retried on the next rescan.
Upstream name changes preserve skill ID and fail that item if the new name
collides. Local content edits remain unprotected by rescan, as before.

Same-source adoption preserves ID, creator, config outside `origin`, labels
and agent bindings. Legacy skills.sh URLs without `origin.path` are resolved
only when their source URL slug identifies one discovered path/frontmatter
match. Ambiguous discovery is not adopted by guessing from the local name.
A same-source skill already placed in another package must first be detached;
its `already_packaged` conflict is unselected by default, like `ambiguous_source`.

### Tree and folder operations

`GET /api/skill-folders` returns:

```json
{
  "folders":[{
    "id":"folder-uuid", "workspace_id":"workspace-uuid",
    "parent_id":null, "name":"addyosmani/agent-skills",
    "package_id":"package-uuid", "package_path":"",
    "created_at":"2026-10-03T00:00:00Z", "updated_at":"2026-10-03T00:00:00Z"
  }],
  "placements":[{
    "workspace_id":"workspace-uuid", "skill_id":"skill-uuid",
    "folder_id":"folder-uuid", "package_id":"package-uuid",
    "source_path":"skills/shipping-and-launch"
  }],
  "packages":[]
}
```

Folders use adjacency via nullable `parent_id`. Null `package_id/package_path`
denotes a custom folder. Null `package_id/source_path` denotes an ordinary
placement, including a detached skill. No placement means uncategorized.
Missing/deleted skills are excluded by joins and stale placements are cleaned
on the next mutation. Sort folders before skills, each by name, on the client.

| Route | Body | Result / behavior |
|---|---|---|
| POST `/api/skill-folders` | `{"name":"Engineering","parent_id":""}` | 201 folder; empty/omitted parent is root |
| PATCH `/api/skill-folders/{id}` | `{"name":"New name","parent_id":"folder-uuid"}` | 200 folder; omit either field to retain it; `parent_id:""` moves to root |
| DELETE `/api/skill-folders/{id}` | none | `{"deleted":true}`; promote direct contents one level; sibling name collision rejects atomically |
| PUT `/api/skill-placements/{skillId}` | `{"folder_id":"folder-uuid"}` | `{"updated":true}`; empty folder removes placement |
| POST `/api/skill-placements/{skillId}/detach` | none | `{"updated":true}`; preserve folder, remove package/source association |

Names are 1–255 UTF-8 bytes without surrounding whitespace, NUL or line breaks.
Package roots default to `owner/repo`; an existing sibling causes a numeric
suffix. Roots may be renamed or moved into custom folders. Internal managed
folders cannot be rearranged or used as destinations for custom content.
Their paths strip the common parent prefix of all discovered candidates.
Updated placements prune empty managed folders, preserving any detached skills.
All tree mutation transactions prevent cycles and enforce workspace scope.

### Dissolve and delete

`GET /api/skill-packages/{id}/delete-preview` returns
`{preview_id,skill_ids,affected_agents,can_delete}`. Each affected agent row is
`{id,name,skill_id,skill_name}`; an agent with multiple affected bindings has
multiple rows. The token also guards dissolution.

`POST /api/skill-packages/{id}/dissolve` with `{"preview_id":"..."}` removes
package metadata and managed markers, retaining folders, skills and bindings.
It returns `{"deleted":false,"dissolved":true,"skill_count":2}`.

`DELETE /api/skill-packages/{id}` with the same body deletes only currently
package-bound skills, their files/labels/bindings, package metadata, and empty
managed folders. Detached ordinary skills and their ancestor folders survive.
It returns `{"deleted":true,"dissolved":false,"skill_count":2}`. Any skill
permission failure rejects the entire operation before deletion. Both actions
validate the affected set and run atomically. Workspace deletion separately
cleans all three tables and excludes concurrent package writes with a workspace
row lock; no new foreign keys or cascades are introduced.

## Diagnostics and errors

Diagnostics use `{code,path?,target?,message,retryable}`. Old single-skill import
and refresh retain their existing response shape and add optional `diagnostics`
on the returned skill (inside `skill` for structured import results). HTTP fetch
errors retain `error` and add `code`, `diagnostics`, `retryable`.

| Code | Meaning and recovery |
|---|---|
| `tree_incomplete` | Package scan fails on truncation; use an actual smaller subtree. Legacy GitHub single import may succeed via its existing directory crawl with this warning. |
| `tree_unavailable`, `source_unavailable`, `source_timeout` | Source cannot be read; retry with a fresh preview. skills.sh tree request failure remains fatal/retryable. |
| `required_reference_unavailable`, `support_file_unavailable` | A required external reference or package support download failed; item fails, never saves a partial bundle. |
| `limit_exceeded` | 1 MiB per final file; 4,096 lines per Markdown paragraph starting with a possible reference definition (first non-whitespace byte `[`); 256 support files, 8 MiB support contents per skill; shrink source/bundle or split the indicated paragraph. |
| `shared_path_conflict` | `_shared` destination conflicts with existing or rewritten content; fix source before retrying. |
| `cross_skill_reference` | Other skills' SKILL.md files are not copied; reference remains unchanged. |
| `filtered_reference` | External directory, symlink/submodule, binary or license asset skipped. References between original skill files and their own directories remain unchanged without a warning. Package discovery skips nonregular SKILL.md entries with a path-specific diagnostic; direct single-skill import rejects a nonregular primary SKILL.md. |
| `invalid_source` | Invalid source URL/subdirectory or an unsafe repository-tree path, including names containing `:` or `\`. HTTP 400, not retryable; correct the source or select a portable subtree. |
| `path_outside_repository` | Reference escapes repository; unchanged. Missing example paths and remote links are also unchanged. |
| `manifest_invalid`, `dot_directory_duplicate` | Invalid manifest disables defaults; a same-name dot-directory mirror is omitted in favor of the ordinary path. |
| `preview_stale`, `source_changed` | Repreview; no blind write based on stale source, permissions or targets. |
| `name_conflict`, `ambiguous_source`, `already_packaged` | Choose rename/authorized overwrite, resolve ambiguity, or detach first. |
| `forbidden`, `permission_changed` | Check skill and package permissions separately. Default unauthorized adoption/update is skipped; explicit selection fails. |
| `folder_conflict`, `placement_conflict`, `item_failed`, `operation_failed`, `candidate_failed` | Item/transaction failed; check reason and retryable flag, then repreview. |
| `not_selected`, `source_removed` | Skipped selection or retained removed source; no failure. |

Request errors: 400 for invalid JSON/fields/UUIDs/source; 403 for denied role or
ownership; 404 for missing/cross-workspace objects; 409 for stale preview,
uniqueness/cycle/managed-tree conflicts; 413 for limits; 502/503 for source
failure; 504 for the source deadline; 500 for unexpected storage failure.
Some legacy validation errors only have `error`; do not assume a code exists.
Tree-specific codes include `folder_cycle`, `managed_folder`, `managed_skill`.
`retryable` is advice to repreview, never permission to replay a write.

Package discovery reports nonregular primary files without following them, so
one symlink does not hide the other valid skills in a package and the omission
is visible in preview diagnostics. Unsafe tree paths reject the source instead
of silently dropping potentially required support files.

## Bounded work and integration points

Package preview, apply and rescan share the existing **45-second** request
context. A file download still has a 30-second HTTP-client limit and at most
1 MiB of content. Single-skill import and refresh use their existing paths and
limits. The CLI and Next rewrite proxy allow 60 seconds for the server response.
The proxy setting applies to all configured rewrites, not just package APIs.

Markdown reference scanning checks cancellation while opening nested containers
within a line, as well as between lines and files. A paragraph with more than
4,096 source lines fails with `limit_exceeded` only if its first non-whitespace
byte is `[`, which can begin a reference definition. This check uses Goldmark's
paragraph contents and whitespace rules, including inside lists and blockquotes.
It bounds reference-definition extraction, whose repeated line copying is
otherwise quadratic. Ordinary prose, tables and quoted text with a different
first byte remain unrestricted by this paragraph limit; fenced and indented
code blocks are exempt. The check is conservative: an opening `[` still triggers
the limit even if it does not form a valid definition. Blank lines separate
paragraphs. The non-retryable diagnostic identifies the file and the paragraph's
starting line. Package previews retain the failed candidate; single-skill import
uses its existing HTTP 413 response. Canceled scans retain the existing timeout
classification and return no partial spans.

A package request resolves the ref to a commit **on every call**, then reads the
current tree. Ref/commit resolution is never cached. Full scans use one
recursive tree; scoped scans traverse ancestors nonrecursively and request only
the chosen subtree recursively. Shared paths outside that scope use synchronized,
request-local directory lookups. All raw and tree downloads share the request's
eight-slot limit. Source limits remain 1,024 candidates, 256 tree requests and
32 MiB per API JSON response.

### Slow sources: verified content cache and bounded candidates

`labrastro_skill_blob_cache.go` keeps immutable raw bodies in process memory,
keyed by workspace ID, normalized owner/repo and the tree entry's Git blob SHA.
A miss downloads from the fixed commit URL and checks Git's `blob <length>\0`
SHA-1 identity before insertion. Hash mismatches and download failures are not
cached; they retain the existing retryable source-failure classification. Keys
never share content across workspaces or repositories. Concurrent requests for
the same key share one download; a waiting request can cancel independently.
The downloading request owns the network context: if it is canceled, its
waiters receive the failure too, and a later request can retry. No network work
is detached from a request and no write is automatically retried.

The process-wide LRU holds at most **64 MiB of raw bodies** and **16,384 entries**
(the latter also bounds metadata for empty blobs). Entries expire 30 minutes
after insertion, longer than the 15-minute preview token lifetime; a cache hit
does not extend the TTL. LRU pressure can evict entries earlier. There is no
disk cache, database schema change, new token payload or persisted candidate
body. A restart or another server instance is a cold cache miss and follows the
same fixed-commit download, hash check and permission/fingerprint validation.

Candidate bundles run with at most **four workers** and are released after their
summaries/digests are collected. Results are placed in sorted path order,
independent of completion order. Each candidate retains the existing 1 MiB
primary plus 8 MiB/256 supporting-file budget. The bounded *logical content
working set* is therefore the shared 64 MiB cache plus up to four times 9 MiB
per active scan (100 MiB for one scan before overlap). This is **not an RSS or
Go heap ceiling**: tree/summary/LRU metadata, in-flight raw buffers, string
copies, reference rewriting, transport buffers and GC add allocations, and
concurrent requests add their own candidate working sets. The entry and byte
limits are process-wide, not a separate 64 MiB allowance for every workspace.

Apply still validates **all candidates** and the fresh database/permission
snapshot before writing. After a successful preview, its network work normally
consists of confirming the ref/commit and reading the tree: cached blobs avoid
raw downloads, but candidate parsing, reference completion, fingerprints and
transactions still run. Selection alone does not shrink the scan. A preview
that reaches 45 seconds writes nothing; its completed, validated blobs remain
available for a user-initiated retry in the same process. The retry only
fetches missing/changed/expired/evicted blobs. This improves slow-source progress
without guaranteeing completion: tree/ref requests, changed sources, cache
pressure, very large bundles, or landing on a different instance may still
consume the whole budget. Once writes begin, the response retains successful
items and reports remaining timeout failures; a lost response remains
indeterminate and must not trigger an automatic write retry.

A `tree/<ref>/<subdirectory>` URL narrows the actual tree and candidate set but
changes package identity (workspace + owner/repo + subdirectory). It is a
different package, not a continuation of the repository-root package; existing
packaged skills cannot silently move to it. See the validation record for fixed
snapshot equivalence, cold/warm timings and the actual 45-second retry probe.

Fork migrations are `9001`–`9008`. `9001` creates the three tables; each later
file creates one concurrent index and has a paired down migration. The fork
migrator registration provides interrupted-index repair. This range reduces
near-term collisions; future upstream syncs must still check numeric prefixes.

### Upstream touchpoints

This is a maintenance checklist across the three fork PRs, not a claim that
the frontend/CLI files are in PR #58. Server entries describe #58; CLI entries
were checked against #59 at `b124382c3e23a2ddf0d577f439c3a33b20fa4c60`
(tree `4e42ab6146182a2f049c78d6ac6cf4c0f42882dc`, identical to `b31a975f`).
Frontend entries were checked against #60 at
`0dab3c06b4e0f1a7775bc37d86ec11c497753ffe`
(tree `9f324eb837ad58ee20f0d51409f9d062d317d1ed`, identical to `497db0ac`).
Their PR descriptions and actual diffs from the shared `28eb1e838` base were
checked; both retain 60-second client/proxy budgets. This documents source
alignment, not combined runtime acceptance or independent approval of #58.

OL-107 then moved the fork logic of `client.ts`, `skills-page.tsx`,
`skill-list-actions.tsx`, `refresh-skill-dialog.tsx` and
`create-skill-dialog.tsx` into fork modules; their rows below list the
remaining mounts. In those five files every original upstream line is still
byte-identical to `803cfcf18`, except the modified lines named in each row
(`git diff 803cfcf18 -- <file>` shows the rest as pure insertions).

Handwritten hooks in existing upstream files:

| File | Required change |
|---|---|
| `server/internal/handler/skill.go` | Tree mode/SHA, regular-file filtering, resolved skills.sh path/ref, shared completion calls, additive diagnostics, package hints |
| `server/internal/handler/skill_refresh.go` | Preserve diagnostics in success/failure responses |
| `server/internal/handler/skill_create.go` | Extract transaction-injected overwrite helper; existing wrapper keeps commit behavior |
| `server/internal/handler/skill_import_archive.go` | Reject sibling SKILL.md roots; nested content behavior retained |
| `server/cmd/server/router.go` | One fork route registrar call |
| `server/internal/handler/workspace.go` | One transactional fork tree cleanup step |
| `packages/core/api/schemas.ts`, `packages/core/types/agent.ts` | Optional old-response diagnostics and exported new schemas |
| `packages/core/api/client.ts` | Skill integration uses two mounts: the `installLabrastroApi` import and its call after the class (OL-130 aggregates the fork modules and checks method-name collisions; see [frontend API touchpoints](labrastro-frontend-api.md#upstream-touchpoints)). The 15 workspace-pinned folder/package/placement methods (zod/`parseWithFallback`, null on malformed responses) and `importSkillParsed` (`Promise<Skill \| null>`, so a malformed import stays indeterminate) live in fork `packages/core/api/labrastro-skill-api.ts`, typed onto `ApiClient` by module augmentation. Upstream `importSkill` keeps its `Promise<Skill>` contract |
| `packages/core/package.json`, `packages/core/skills/index.ts` | Export the new `skills/package-queries` entry and query hooks |
| `packages/core/types/index.ts` | Export `SkillImportDiagnostic` for UI consumers |
| `packages/views/skills/components/skills-page.tsx` | `useSkillsPageFolderExplorer` call, `treeActions` context field, `listModel` / `organizeRows` and `renderRow` list mounts, `folderActions` / `notice` / `dialogs` slots, and the filtered `visibleCount`. The virtualizer counts the combined folder/skill/candidate rows and uses each row's parent-context composite key for both `getItemKey` and React; selection uses only deduplicated visible `listModel.skillRows`. `NameCell` accepts a flat-view path hint and tree depth. In-place expansion, workspace-scoped local persistence, depth-first row modeling, folder/package menus, package candidates, detach and dialogs live in fork `skills-page-folder-explorer.tsx`; folder form/move/delete dialogs live in `skill-folder-dialogs.tsx`. Modified upstream lines: the `NameCell` signature and optional path branch (the no-path span stays byte-identical), virtualizer `count` / `getItemKey`, create/row detail destinations without folder query propagation, selection and select-all inputs, `showEmpty` (the upstream empty workspace remains; folders keep their rows), the loading condition (includes the folder-tree request), `visibleCount`, and the empty-list text; the one-line error-state header gains `onImportPackage`, the list fragment's `<>` / `</>` tags become the `div` list wrapper, and the virtual-row body reads and dispatches through `listModel.rows`. The row JSX retains its original indentation; `git diff` and `git diff -w` agree |
| `packages/views/skills/components/skill-list-toolbar.tsx` | Optional `folderActions` mount on the right for root folder creation and expand/collapse-all controls. Search/filter views show the flat-results hint there; narrowed views retain the original matching/total count. Modified upstream lines: the props signature and right-action mount |
| `packages/views/skills/components/skill-list-actions.tsx` | `treeActions?: SkillTreeActions` context field; `SkillTreeRowMenuItems` and `SkillTreeBatchMoveAction` mounts (fork `skill-tree-actions.tsx`); `useBatchRefreshNotices` mounts in `UpdateSkillsDialog` (fork `refresh-skill-notices.tsx`). Modified upstream line: `const refreshed = await api.refreshSkill(...)` keeps the result |
| `packages/views/skills/components/create-skill-dialog.tsx` | `useImportNotices` mounts in the URL and local forms, the unreadable-result guard and the `multiple_skills` prepare-error key. Modified upstream lines: the `importSkillParsed` call and the archive error expression, now prefixed by `localArchiveImportError` so local `multiple_skills` and server sibling-archive errors share one localized recovery from `fork-ui:skills.create.local.multiple_skills`. Notice view and wording live in fork `create-skill-notices.tsx` |
| `packages/views/skills/components/refresh-skill-dialog.tsx` | `useRefreshSkillNotices` mounts only: hook call, `hold` after the detail-cache update, and the notice-view return; notice state and dismissal live in fork `refresh-skill-notices.tsx` |
| `packages/views/agents/components/skill-picker-list.tsx` | Thin tree-mode dispatch and optional batch-toggle props; tree implementation lives in fork `skills/components/skill-picker-tree.tsx`, retaining the upstream flat picker |
| `packages/views/agents/components/skill-add-dialog.tsx`, `skill-multi-select.tsx` (same directory) | Opt in to the tree picker and batch toggle using the caller's filtered skills |
| `packages/views/locales/index.ts`, `packages/views/i18n/resources-types.ts` | Register the `skill-packages` namespace and translation typing |
| `packages/views/skills/lib/utils.ts` | `isMultipleSkillsError` recognizes the server archive rejection for localization |
| `packages/core/skills/pack-archive.ts` | Reject sibling SKILL.md roots before local-folder packing discards them; normalize case/separators, retain nested-root handling |
| `apps/web/next.config.ts` | Set `experimental.proxyTimeout: 60_000` for all configured rewrites, above the 45-second backend deadline |
| `server/cmd/multica/cmd_skill.go` | Display additive diagnostics in legacy single-import and refresh table output; JSON response shape remains unchanged |
| `server/internal/service/builtin_skills/multica-platform/references/skill-import.md` | Package CLI entry, selection/rescan defaults, complete partial-failure reports and no blind write retry |

The five upstream `packages/views/locales/*/skills.json` files keep the now-unrendered `detail.all_skills` key so those lines stay byte-identical to `803cfcf18`; OL-1053 restored it after OL-1029 briefly dropped it. Dead-key cleanup covers only the fork-created `skill-packages.json` files, never upstream locale files.

New handwritten modules include the server `labrastro_skill_*.go` handlers and
migrator hooks, `packages/core/api/labrastro-skill-schemas.ts` (extended by #60),
`packages/core/skills/package-queries.ts`, package/folder/move dialogs and tree
models under `packages/views/skills/`, the shared
`packages/views/common/folder-explorer/` model and components,
`skill-diagnostics-notice.tsx`, the OL-107 extraction modules
(`packages/core/api/labrastro-skill-api.ts`, `skill-tree-actions.tsx`,
`refresh-skill-notices.tsx`, `create-skill-notices.tsx`), the OL-1029 explorer
modules (`skills-page-folder-explorer.tsx`, `skill-folder-dialogs.tsx`), the five
`skill-packages.json` locale files, and
`server/cmd/multica/cmd_labrastro_skill_package.go` (registered with `init()`;
`cli.AtLeastAPITimeout(60*time.Second)` on both HTTP client and request context).
They are feature modules, not generated output. Their adjacent tests cover the
hooks above; #59 also updates `server/internal/service/builtin_skills_test.go`.

The fork `package-apply-stage.tsx` distinguishes stale source/preview errors
(`preview_stale`, `source_changed`), source-read errors (`source_timeout`,
`source_unavailable`, `tree_unavailable`), other coded server failures, and
unreadable/null/body-less responses. Its five locale files provide separate
titles. Structured server details remain visible; explicit repreview and query
invalidation remain, with no automatic write retry or blanket 5xx zero-write
claim. Single and batch refresh diagnostics live in fork
`refresh-skill-notices.tsx`, mounted from the upstream dialogs listed above;
batch results include diagnostic `path`/`target`.

The fork-created `server/cmd/server/testdata/labrastro-external-routes.tsv`
records the 14 deny-by-default skill-package routes alongside other fork routes.
It is not an upstream file.

New fork tests include `apps/web/next.config.test.ts` (60,000 ms and API rewrite),
the core package API/query tests, tree/model/dialog/picker suites,
`common/folder-explorer/` model and component tests,
`skills-page.folder-explorer.test.tsx` (in-place multi-level rows, workspace-scoped
expansion persistence, managed creation, candidates, flat paths and tree-error
recovery), the two
binding-entry tests, and `create-skill-notices.test.tsx`,
`refresh-skill-notices.test.tsx` and `refresh-skill-notices.batch.test.tsx`.
OL-107 moved those three suites' cases verbatim out of
`create-skill-dialog.test.tsx`, `refresh-skill-dialog.test.tsx` and
`skill-list-actions.test.tsx`, which match `803cfcf18` again. Extended
upstream suites are `pack-archive.test.ts` (sibling-root cases) and
`skills-page.source-link.test.tsx` (the folder-tree query, query-client and
`package-queries` mocks the page now needs) in their implementation
directories. The `labrastro-skill-schemas.ts` inferred-type extensions are
changes to an existing **fork** file, not an upstream hook. OL-107 finished
the isolation the picker started: `skills-page`, `skill-list-actions`,
`refresh-skill-dialog`, `create-skill-dialog` and `client.ts` keep only the
mounts listed above.

Generated sqlc output is separate from those handwritten hooks:

| Generated file | Input and purpose |
| --- | --- |
| `server/pkg/db/generated/models.go` | Three models from `server/migrations/9001_labrastro_skill_packages.up.sql`; no upstream skill columns |
| `server/pkg/db/generated/labrastro_skill_package.sql.go` | Queries/types from `server/pkg/db/queries/labrastro_skill_package.sql` |

`server/sqlc.yaml` reads the full `server/migrations/` schema and
`server/pkg/db/queries/` directory. Regenerate from the repository root with
`make sqlc` (the Makefile pins sqlc v1.31.1), then inspect the generated diff.
Indexes come from migrations 9002–9008, each with its paired down migration.
This cache/parallelism revision changes no SQL or sqlc inputs and does not regenerate
files as a substitute for handler/contract verification.

Folder mutations have no WebSocket events. Existing skill created/updated/
deleted events remain. UI mutations invalidate workspace-specific skill/tree/
package queries and refresh on window focus. Binding remains explicit use of
the existing agent-skill endpoints and must respect caller-filtered skills.

## Rollback

Normal application rollback reverts feature code and **keeps** all three
`labrastro_skill_*` tables, their migration records and the ordinary skill/file
rows. Older servers and daemons can read the completed skills. Down migrations
drop organization metadata and are reserved for isolated tests or an explicit
data-cleanup plan; dropping tables does not restore content.

Take an operation-time backup before any action that can replace content:
package `overwrite`, same-source adoption, default `rescan --apply` (including
reimport of an existing package), explicitly selected changed items,
single-skill overwrite import, and single/batch refresh from source. Default
rescan does not protect local edits. Reverting code, dissolving a package or
detaching a skill cannot undo those replacements.

**Observed rollback hazard:** OL-106 ran the old server at `803cfcf18` against
the new database. Reads included `_shared/*`, but an old-server “update from
source” removed those files and restored repository-relative links such as
`../../references/...`. Read compatibility does not imply that old refresh
preserves completion. After rollback, pause single/batch source refresh and
covering imports for completed skills, or recover their contents from backup.
There is no automatic undo.

### Backup scope and commands

Use a database backup taken before the operation; a plain SKILL.md/zip export
alone does not retain IDs, ownership, bindings, labels or package organization.
The minimum logical set is `skill` (including content/config/creator),
`skill_file` (including every `_shared/*` row), `agent_skill`,
`skill_to_label`, and all three `labrastro_skill_*` tables. Those rows refer
to workspace/user/agent identities and label definitions in `issue_label`;
restore them only with those matching records. A whole-database archive is
the recommended default because it also preserves those dependencies and
`schema_migrations`. For deployments with external uploads, retain the normal
object-store backup separately; it is outside this skill-content backup.

The following commands are an operator runbook, not authorization to touch a
connected workspace. Populate `SKILL_BACKUP_SOURCE` using a dedicated database
service/credential, and keep credentials out of shell history/logs. Pause
affected writers, record the application commit and PostgreSQL version, and
restrict archive access; a full archive also contains unrelated workspace data.

```sh
set -eu
umask 077
: "${SKILL_BACKUP_SOURCE:?set the authorized source connection/service}"
: "${SKILL_BACKUP_DIR:?set a new private backup directory}"
mkdir -p "$SKILL_BACKUP_DIR"
pg_dump --dbname="$SKILL_BACKUP_SOURCE" --format=custom --no-owner --no-acl \
  --file="$SKILL_BACKUP_DIR/before-skill-operation.dump"
pg_restore --list "$SKILL_BACKUP_DIR/before-skill-operation.dump" \
  > "$SKILL_BACKUP_DIR/archive.list"
sha256sum "$SKILL_BACKUP_DIR/before-skill-operation.dump" \
  > "$SKILL_BACKUP_DIR/archive.sha256"
```

Verify the archive list includes the seven skill/organization tables and their
data sections. A selective `pg_dump -t skill -t skill_file -t agent_skill
-t skill_to_label -t labrastro_skill_folder -t labrastro_skill_package
-t labrastro_skill_placement` is only an alternative when matching identity,
label and schema backups already exist; it does not follow those dependencies
and is not a standalone disaster-recovery backup.

### Isolated restore rehearsal

Use a newly created database in a separate local PostgreSQL cluster with the
same required extensions. Never run restore/cleanup commands against the live
workspace database. This example deliberately creates a new database without
`--clean` or `--create`; choose an unused local port and database name:

```sh
set -eu
unset PGSERVICE PGSERVICEFILE PGDATABASE PGOPTIONS
export PGHOST=127.0.0.1 PGPORT=55483 PGUSER=postgres
: "${SKILL_RESTORE_DB:?set a new isolated rehearsal database name}"
createdb --template=template0 "$SKILL_RESTORE_DB"
pg_restore --dbname="$SKILL_RESTORE_DB" --exit-on-error --single-transaction \
  --no-owner --no-acl "$SKILL_BACKUP_DIR/before-skill-operation.dump"
psql --dbname="$SKILL_RESTORE_DB" --no-psqlrc --set=ON_ERROR_STOP=1 \
  --command="SELECT count(*) FROM skill; SELECT count(*) FROM skill_file WHERE path LIKE '_shared/%'; SELECT count(*) FROM labrastro_skill_placement;"
```

Compare row counts and hashes of complete rows (including contents/config,
paths, IDs, creator, labels, bindings and candidate metadata) with the backup
snapshot. In this isolated copy, change a skill and remove its shared files,
then restore the archive into a **second fresh database** and verify exact
recovery. Run a rollback-version server on isolated ports with all production
credentials/integrations removed; verify skill reads, and reproduce the refresh
hazard only on a disposable copy. The validation record identifies what was
actually rehearsed.

Promoting restored data is a separate maintenance decision: pause writers,
assess writes since the backup, and review either whole-database replacement or
a transactionally consistent selective restore of the affected IDs and related
rows. A full restore can lose later unrelated writes; app rollback alone
cannot promise restoration. No release, deployment, upstream PR or real
workspace mutation is included in this work.
