---
name: multica-project-plans
description: "Use when creating a Multica project plan, snapshotting a PRD issue into a plan, adding plan phases or parts, or linking issues to plan parts."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Project Plans

## Quick start

Project plans decompose a project into ordered phases and parts. A part can
then link the implementation issues that cover it. Plan authoring mutates
durable project state, so do it only when the task calls for that change.

```bash
multica project plan create-from-issue <project-id> <source-issue-id> --kind prd --output json
multica project plan add-phase <project-id> <plan-id> --title "Implementation" --position 0 --output json
multica project plan add-part <project-id> <plan-id> <phase-id> --title "API" --position 0 --output json
multica project plan link-issue <project-id> <plan-id> <part-id> <issue-id> --output json
```

The source issue and every linked issue must belong to the same project as the
plan. `create-from-issue` snapshots the source issue's title, description,
revision, and content digest; later source-issue edits do not rewrite that
snapshot.

## Version availability

The `multica project plan` subcommand ships with the release that lands the
project plans feature. Older binaries report that `multica project plan` is
unknown; upgrade before attempting plan authoring.

## CLI reference

All four subcommands accept `--output json|table`; the default is `json`. Use
JSON whenever another command needs an ID. Table output contains these columns:

- plan creation: `id`, `title`, `origin`;
- phase creation: `id`, `title`, `position`;
- part creation: `id`, `title`, `position`;
- issue linking: `id`, `project_plan_part_id`, `issue_id`.

### Snapshot an issue into a plan

```text
multica project plan create-from-issue <project-id> <source-issue-id> [flags]
```

Flags:

- `--kind <kind>` — plan kind; default `prd`. `prd` is the only supported kind
  in this release.
- `--output json|table` — output format; default `json`.

The command returns the created plan. A project can have only one active plan.

### Add a phase

```text
multica project plan add-phase <project-id> <plan-id> [flags]
```

Flags:

- `--title <title>` — required; an empty value fails with
  `--title is required`.
- `--description <description>` — optional; default empty string.
- `--position <position>` — zero-based phase position; default `0`.
- `--output json|table` — output format; default `json`.

The command returns the created phase.

### Add a part to a phase

```text
multica project plan add-part <project-id> <plan-id> <phase-id> [flags]
```

Flags:

- `--title <title>` — required; an empty value fails with
  `--title is required`.
- `--description <description>` — optional; default empty string.
- `--acceptance-criteria <criteria>` — optional; default empty string.
- `--position <position>` — zero-based position within the phase; default `0`.
- `--output json|table` — output format; default `json`.

The command returns the created part.

### Link an issue to a part

```text
multica project plan link-issue <project-id> <plan-id> <part-id> <issue-id> [flags]
```

Flags:

- `--output json|table` — output format; default `json`.

The command returns the created issue-link record. An issue can be linked to
only one part within a given plan.

## Positions are explicit

Positions are zero-based integers and must be non-negative. Phase positions
are unique within a plan; part positions are unique within a phase. These
commands insert at exactly the requested position and do not shift existing
items.

Omitting `--position` sends `0`. That works for the first phase or the first
part in a phase, but collides once position `0` is occupied. A phase collision
returns `another phase already uses this position`; a part collision returns
`another part already uses this position`. Choose an unused position, normally
the next integer when appending.

## Complete worked example

This Bash sequence takes the project ID, source PRD issue ID, and an existing
implementation issue ID as its three arguments. It parses every created ID from
JSON rather than copying IDs by hand.

```bash
set -euo pipefail

project_id="${1:?usage: plan-example PROJECT_ID SOURCE_ISSUE_ID EXISTING_ISSUE_ID}"
source_issue_id="${2:?usage: plan-example PROJECT_ID SOURCE_ISSUE_ID EXISTING_ISSUE_ID}"
existing_issue_id="${3:?usage: plan-example PROJECT_ID SOURCE_ISSUE_ID EXISTING_ISSUE_ID}"

plan_json="$(
  multica project plan create-from-issue \
    "$project_id" "$source_issue_id" \
    --kind prd \
    --output json
)"
plan_id="$(jq -er '.id' <<<"$plan_json")"

phase_json="$(
  multica project plan add-phase \
    "$project_id" "$plan_id" \
    --title "Implementation" \
    --description "Build and verify the planned surface" \
    --position 0 \
    --output json
)"
phase_id="$(jq -er '.id' <<<"$phase_json")"

api_part_json="$(
  multica project plan add-part \
    "$project_id" "$plan_id" "$phase_id" \
    --title "Build the API" \
    --description "Implement the server contract" \
    --acceptance-criteria "The API passes its contract tests" \
    --position 0 \
    --output json
)"
api_part_id="$(jq -er '.id' <<<"$api_part_json")"

docs_part_json="$(
  multica project plan add-part \
    "$project_id" "$plan_id" "$phase_id" \
    --title "Document the workflow" \
    --description "Explain the supported agent workflow" \
    --acceptance-criteria "An agent can complete the workflow from the documentation" \
    --position 1 \
    --output json
)"
docs_part_id="$(jq -er '.id' <<<"$docs_part_json")"

link_json="$(
  multica project plan link-issue \
    "$project_id" "$plan_id" "$api_part_id" "$existing_issue_id" \
    --output json
)"
link_id="$(jq -er '.id' <<<"$link_json")"

jq -n \
  --arg plan_id "$plan_id" \
  --arg phase_id "$phase_id" \
  --arg api_part_id "$api_part_id" \
  --arg docs_part_id "$docs_part_id" \
  --arg link_id "$link_id" \
  '{plan_id: $plan_id, phase_id: $phase_id, parts: [$api_part_id, $docs_part_id], link_id: $link_id}'
```

Save it as `plan-example`, make it executable, and run it with three real IDs:

```bash
./plan-example "$PROJECT_ID" "$SOURCE_PRD_ISSUE_ID" "$EXISTING_IMPLEMENTATION_ISSUE_ID"
```

## Reading a plan

There is no `multica project plan get`, `list`, or other plan-read subcommand at
this source revision. Do not invent one. Read the plan in the project's Plan
views in the web UI, or use the authenticated read API:

- `GET /api/projects/{project-id}/plan` — the project's active plan;
- `GET /api/projects/{project-id}/plans/{plan-id}` — a specific retained plan
  version, scoped to that project.

Both routes return the complete overview: `plan`, `rollup`, `phases` (with
nested parts and live linked-issue details), `dependencies`, and
`uncovered_parts`. The CLI authoring responses contain only the object just
created, so retain their JSON IDs as shown above.

## Failure modes

- **Feature flag off:** Project plans default off. When
  `FF_PROJECT_PLANS` does not enable `project_plans`, the HTTP routes deliberately
  return `404` with `project plan not found`. Enable `FF_PROJECT_PLANS=true` on
  the server, then retry; changing only the client cannot enable the feature.
- **Wrong project:** Plan reads and writes are scoped by both project and plan
  ID. A plan from another project returns `project plan not found`; a source
  issue from another project returns `source issue not found in this project`;
  an issue linked from another project returns `issue not found`. Use the
  project that owns the plan and all involved issues; cross-project plan links
  are not supported.
- **An active plan already exists:** Retrying a source snapshot, or snapshotting
  any other source into a project that already has an active plan, returns
  `409` with `this project already has an active plan`. The invariant is one
  active plan per project, not one plan per source issue. Read and reuse the
  active plan, or use the authenticated API to replace or remove it;
  this CLI revision exposes no supersede or delete subcommand.
- **Phase not found:** `add-part` returns `404` with `phase not found` when the
  phase ID does not exist in that plan. Use the `id` returned by `add-phase` for
  the same plan.
- **Part not found:** `link-issue` returns `404` with `part not found` when the
  part ID does not exist in that plan. Use the `id` returned by `add-part` for
  the same plan.
- **Issue already linked:** Linking an issue that is already attached to any
  part in the plan returns `409` with
  `this issue is already linked to a part in this plan`. Keep the existing link,
  or unlink it through the authenticated API before relinking; this CLI
  revision exposes no unlink subcommand.
- **Position collision:** Reusing a sibling position returns `409` with the
  phase or part collision message documented above. Retry with an unused
  zero-based position.

## Side effects

Every documented subcommand mutates durable workspace state. Creating from an
issue creates the active plan and a permanent source snapshot; adding phases
or parts changes its structure; linking changes coverage and rollups shown in
the Plan views.
