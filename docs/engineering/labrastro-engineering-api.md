# Project engineering and managed baselines

OL-1189 implements the Strixnova v3.3 contract, with the approved stages-only scope.
This module stores project configuration and immutable Markdown revisions in PostgreSQL. It has no runtime,
claim, skill-binding, Git repository, or model dependency.

All endpoints require workspace membership and a project in the selected
workspace. External conversation credentials are denied by the existing router
policy. Routes use `/api/projects/{id}`:

| Method | Suffix | Result |
| --- | --- | --- |
| GET | `/engineering` | `engineering` and four `chapters`, each with nullable `adopted` and `candidate` |
| PATCH | `/engineering` | Updated `engineering` configuration |
| POST | `/baseline/candidates` | New revision, including structured `anchors_diff` |
| POST | `/baseline/decisions` | Four updated `chapters` and `open_issues` |
| GET | `/baseline/revisions` | `revisions`, `has_more`, `next_before`, or a historical Diff |

Configuration defaults to disabled. Only owner/admin can change `enabled`
or `decider_member_ids`. Other configuration fields are
also writable by deciders. As with project `lead_id` and member actors elsewhere
in the platform, decider IDs are **user IDs**, validated against current workspace
membership. Empty lists use owner/admin and the human project lead; explicit lists
replace the lead default while retaining owner/admin. Configuration remains human-only
(`RequireHumanActor`); removed or unknown request fields return 400 `invalid_request`.

Enabling engineering writes configuration only. All four chapters initially have
`adopted: null` and `candidate: null`; no baseline document or revision is seeded.
Disabling and re-enabling preserve existing history.

`current_stage` is a positive integer, independent of configured stage numbers;
values above five are supported. `stages` is a JSON array, defaulting to `[]`,
with entries `{stage, title, goal}`. A patch replaces the entire array; omission
or `null` leaves it unchanged, while `[]` clears it. There can be at most 50
entries with distinct positive integer stage numbers. Titles are trimmed to
1–60 Unicode characters; goals are trimmed to at most 280 characters and default
to an empty string. Both are single-line, without CR, LF or NUL. Unknown nested
fields return 400 `invalid_request`; invalid stage values return 422
`invalid_request`. Entries are sorted by stage number before saving and returned
as a JSON array, never as base64. No mainline issue field is part of this API.

Candidate requests contain `chapter`, `content_markdown`, `change_summary`, and
`base_revision_id` (null for the first version). Existing candidates can only be
replaced with their exact `replaces_candidate_id`; replaced versions become
`withdrawn`. Optional `source_issue_id` must belong to the same project.

Decisions contain `action: adopt|reject` and one to four `revisions`, each with
`revision_id` and `expected_adopted_revision_id`. All revisions are checked before
any changes commit. Session/PAT callers must be deciders, otherwise they receive
403 `baseline_not_decider`. A task token bound to the selected workspace receives
403 `baseline_human_required` for both adoption and rejection, before the body is
parsed and without changing state. Tasks can still read, propose candidates and
compare history. Cloud PATs, tasks bound to a different workspace and unknown
credentials receive 403 `baseline_credential_denied`.
Audit fields come exclusively from authentication, never request JSON. New
decisions record `session` or `pat`; historical `agent_task` decisions remain
readable and the database credential CHECK is unchanged.

Revision content is limited to 65536 bytes and summaries to 500 Unicode
characters. Oversized content returns 422 `baseline_too_large`; NUL or invalid
UTF-8 returns 422 `baseline_content_invalid` (JSON decoding replaces invalid UTF-8
before parsing). Invalid chapter names return 400 `invalid_request`.

Diff normalization unifies newlines, removes trailing whitespace and collapses
blank lines. Non-empty ATX headings from `#` through `###` start sections;
fenced examples, `####` and lower, empty headings and Setext headings stay in
the body. The text before the first heading is `_preamble` when non-empty.
Each section ends at the next H1–H3 heading, including a child heading.

Normal heading keys collapse internal whitespace. An H3 key is prefixed by the
nearest preceding H1/H2 key as `Parent / Child`; H1/H2 keys have no prefix.
An optional `[prefix-number]` at any heading level provides a stable key across
title changes or moves. Its prefix accepts letters, numbers and underscores;
the suffix is 1–64 characters. Prefixes are unrestricted by chapter.
Non-matching brackets such as `[Draft] Title` remain ordinary titles. Repeated
keys receive ` (2)`, ` (3)`, etc., avoiding collisions with existing keys.

Diffs pair sections by key into `added`, `modified` and `removed`; a plain
heading rename is a removal and addition. Explicit-key title edits appear in
`before`/`after`. New diffs always have `warnings: []` and omit `before_chars`
and `after_chars`. Historical warnings and audit values remain readable.

Diffs are stored on proposal and recomputed at decision time. A changed Diff
returns `409 baseline_base_stale`; refresh and explicitly replace the candidate
before deciding again. Other chapters and workspace issue prefixes no longer
influence the Diff. Some candidates stored under the old algorithm therefore
need replacement after upgrading. Rollback uses a historical body as a new
candidate.

History requires `chapter`, with optional `limit` (default 50, maximum 200) and
`before` revision number. `diff_from` and `diff_to` together return a Diff instead.
`open_issues` uses the same lifecycle-category expansion as issue listing, groups
`started` into `in_flight` and `unstarted` into `queued`, and returns at most 200
items with an explicit `truncated` flag.

Mutations lock workspace lifetime, then the project, then membership and baseline
documents. Project serialization protects first writes and atomic decisions;
document row locks cover multi-chapter decisions. Project/workspace cleanup uses the same transactions as existing deletion. Migration
`9039_labrastro_engineering_tables` creates three tables with no foreign keys;
`9040_labrastro_engineering_config_identity_index` through
`9045_labrastro_engineering_candidate_index` build six separate concurrent indexes.
The migration runner can recover interrupted index builds; down migrations remove
indexes before tables. Migration `9046_labrastro_engineering_decisions`
removes the redundant configuration column and converts historical `task` audit
values to `agent_task`. It installs the new CHECK as NOT VALID before the
backfill, then validates it in the same transaction. Down converts audit values
back and restores the column with a false default; previous switch values
cannot be recovered. Released migration `9039_labrastro_engineering_tables`
remains unchanged.

Migration `9047_labrastro_engineering_stages` adds only the non-null JSONB
`stages` column with default `[]` and `project_engineering_stages_check`.
The CHECK uses CASE to validate the array type before checking its length (50
maximum). It requires no concurrent-index registration. It preserves existing
baselines and audit rows. Apply the migration before deploying the new binary;
old binaries use explicit columns and leave stages intact. To roll back, restore
the old binary before running down; down removes configured stages. Migrations
9039–9046 remain unchanged. Regenerate query code with `make sqlc`.

Before release, count enabled configurations, open candidates and historical
`agent_task` decisions. Review any old candidates that fail the new Diff check;
remove obsolete seeded risk text through a new candidate and human decision.
No existing baseline content is rewritten by the migration.

After commit, `project_engineering:updated` and `project_baseline:changed` publish
only project/chapter identifiers. Shared API clients parse all responses through
Zod and `parseWithFallback`; unknown statuses, warnings and credentials remain
explicitly unknown. The [baseline CLI](labrastro-project-baseline-cli.md) exposes
these APIs; Web/Desktop share the engineering overview and credential badges.
