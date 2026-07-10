# Agent Office — versioning + governance for agent context

**Issue:** FIR-1775. **Status:** Phases 1–2 shipped; Phase 3 building (context
lint + repo-file drift lint shipped; managed region + CI write-gate shipped,
§10; skill MCP gaps closed, §5). **Owner:** Mia.

The real solution (not an MVP): bring *all* agent context under the same
versioning + governance machinery that skills already have, exposed via **REST
API + MCP + CLI** — because those three are one code path in this codebase (MCP
and CLI are thin clients over the REST API; see
`server/internal/cerebro/clitools/mcp_tools_skill_governance.go`).

## 1. What we are versioning

An agent's runtime context is a **composite**, not a single blob. The daemon
injects these fields at claim time (`server/internal/handler/daemon.go`
`ClaimTaskByRuntime`):

- `instructions` (persona) — the big behavior contract
- bound skills (`agent_skill` join) — the composition layer
- `model`, `thinking_level`
- `mcp_config`, `custom_args`, `runtime_config`, `persona_sandbox`
- `custom_env` — **secret values are NOT versioned**; we snapshot key *names*
  only (metadata), values stay in the env endpoint / Infisical / agentvault.

A version snapshot captures all of the above as one JSONB document.

## 2. Composition: shared modules ARE skills

Requirement C ("compose persona from reusable versioned blocks instead of
copy-paste") needs **no new primitive**. A skill is already a versioned,
governed, MCP/API-addressable markdown block, and the daemon appends every
bound skill to the agent's instructions. So:

- **Shared behavior modules = skills** (already versioned + governed). Editing a
  shared rule = a skill change request — already audited.
- **Per-agent context = the new `agent_context_version` + `agent_change_request`**
  layer (this doc), which records *which* skills are bound plus the per-agent
  fields above.

This unifies the two halves instead of inventing a parallel module system.

## 3. Data model (Phase 1 — migration `9100_cerebro_agent_context_versioning`)

Mirrors the skill model (`9013_skill_ownership`) but with a composite snapshot.

- `agent` gets `context_owner_id`, `context_approver_ids UUID[]`,
  `context_version TEXT DEFAULT '1.0.0'` (governance of who may change context).
- `agent_context_version` — append-only snapshots: `(agent_id, version,
  snapshot JSONB, description, created_by, created_at)`, `UNIQUE(agent_id,
  version)`.
- `agent_change_request` — proposed composite edits: `(agent_id, title,
  description, base_version, proposed_version, proposed_snapshot JSONB, status
  pending|approved|rejected|merged, proposed_by, reviewed_by, reviewed_at,
  review_comment, work_session_id, timestamps)`.
- Backfill: every existing agent gets `context_owner_id = owner_id` and an
  initial `v1.0.0` snapshot of its current composite, so diffs and change
  requests have a base from day one.

`proposed_by` is polymorphic (user or agent) with no FK, matching the end-state
of `skill_change_request` after `9098_cerebro_skill_cr_agent_proposer`.

## 4. Lifecycle (mirrors skill governance, `handler/skill_ownership.go`)

`propose → review → atomic apply + snapshot`. On approve, inside one tx:
1. re-validate `semverGT(proposed, current)` against the fresh row (race-safe),
2. apply the composite to the live `agent` row (instructions, model, bindings…),
3. write `agent_context_version` snapshot,
4. mark the change request `merged`,
5. notify the proposer.

Rollback = propose a change whose snapshot equals an older version (history is
never destroyed), identical to how skills roll back.

## 5. Surfaces (REST + MCP + CLI — one code path)

All logic lands in `server/internal/cerebro/*` (cerebro zone). REST routes are
the single source of truth; MCP tools (`server/internal/cerebro/clitools/`) and
CLI commands (`server/cmd/multica/`) are thin wrappers that call them.

REST (proposed):
- `GET  /api/agents/{id}/context/versions`
- `GET  /api/agents/{id}/context/versions/diff?from=&to=`
- `POST /api/agents/{id}/context/change-requests`
- `GET  /api/agents/{id}/context/change-requests`
- `POST /api/agents/context/change-requests/{crId}/review`
- `PUT  /api/agents/{id}/context/ownership`
- `GET  /api/agents/{id}/context/lint`  (drift/dup lint — requirement D; live)
- `GET  /api/agents/context/lint`       (workspace-wide drift sweep; live)
- `POST /api/agents/context/lint/repo-file` (repo CLAUDE.md/AGENTS.md drift
  lint, §9 move 2; the CLI/agent posts the file content because the server has
  no repo checkout; live)

MCP tools (new): `agent_context_list_versions`, `agent_context_diff`,
`agent_context_propose_change`, `agent_context_list_change_requests`,
`agent_context_review_change_request`, `agent_context_set_ownership`,
`agent_context_lint`. The skill-governance MCP gaps Jesper flagged are closed:
`skill_diff`, `skill_update`, `skill_set_ownership` now have MCP wrappers in
`clitools/mcp_tools_skill_governance_gaps.go` (`skill_audit` was already closed
in `clitools/mcp_tools_skill_metadata.go`, TECH-3077), so skills are fully
MCP-drivable. Inventory parity lives in `runtime/tools_registry.go` +
`permguard/inventory.json`; the two write tools are on the in-app admin
denylist (`runtime/firtal_gateway_bridge.go`) like the other governance writes.

## 6. Upstream-zone touchpoints (fork rules)

Almost everything is cerebro-zone. The unavoidable upstream touches use the
sanctioned `// CEREBRO-PATCH(agent-office):` ≤5-line marker path, documented in
`docs/cerebro-patches.md`:
- `server/cmd/server/router.go` — register the cerebro route group.
- `UpdateAgent` in `server/internal/handler/agent.go` — one hook to
  auto-snapshot on direct edits (Phase 2), so existing edits become versioned.

## 7. Phases

1. **Versioning core** — migration, sqlc, snapshot-on-apply, REST + MCP + CLI for
   versions / change-requests / review / diff / ownership / rollback.
2. **Capture-on-update** — hook `UpdateAgent` so every direct edit snapshots;
   history + rollback for edits made outside the change-request flow.
3. **Composition + drift-lint** — `context/lint` (dead skill refs, duplicated
   rules across layers, empty governance fields, stale repo links); close skill
   MCP gaps; surface shared-skill modules.
4. **Promotion + observability** — draft→test→live for context, and link a
   context version to the agent's subsequent task outcomes (`agent usage/tasks`).

Rough scope: ~10–13 agent sessions total (P1 ~3–4, P2 ~2, P3 ~2–3, P4 ~3–4).

## 8. The interface lives on the agent

Same infrastructure as skills, but its own surface: the version history, the
instructions editor, the propose/review flow render **on the agent's own page**
(`apps/web` agent view, cerebro-prefixed component). Editing an agent's
instructions there cuts a new version through the same governance path — the UI
is a thin client over the REST/MCP layer in §5, not a separate code path.

## 9. The repo CLAUDE.md boundary (FIR-1775 follow-up)

**Problem (Jesper):** agents route behavior rules into per-repo
`CLAUDE.md`/`AGENTS.md` files. Those files are the path of least resistance —
auto-loaded by the agent's own harness from the checked-out repo, and freely
writable by the agent — so harness-level rules (gates, comms style, approval,
persona) leak into them. There they are repo-scoped, ungoverned, invisible to
the agent-office version history, and they drift from the versioned
persona/skills. Note: repo `CLAUDE.md` is NOT injected by the Multica daemon
(`daemon.go` injects `instructions` + skills); the runtime reads it off disk.
So it sits entirely *outside* the versioned harness today.

**The boundary (doctrine):**
- repo `CLAUDE.md` = facts about the **code**: architecture, conventions,
  build/test commands, fork rules, where things live. True regardless of which
  agent touches the repo. Owned by the dev team, versioned in the repo's git.
- versioned harness = how the **agent** behaves: persona, gates, comms,
  approval, and per-repo *agent* guidance. Owned by agent-office governance.
- Rule of thumb: if a line tells the **agent how to act**, it belongs in the
  harness; if it describes the **code**, it stays in repo `CLAUDE.md`.

**Solution — three moves:**
1. **Make the right path easy.** The agent-instructions interface (this build)
   gives agents a proper, governed home for behavior rules, removing the
   incentive to dump them in repo `CLAUDE.md`.
2. **Guard it.** Extend the drift-lint (Phase 3) to scan repo
   `CLAUDE.md`/`AGENTS.md` and flag content that reads like agent-behavior
   (gate/comms/persona language) and belongs in the harness — "CI for agents"
   at the repo layer.
3. **Move per-repo agent guidance into the harness.** Represent repo-specific
   *agent* rules as a versioned, repo-scoped harness binding (a skill scoped to
   a repo), injected when that repo is checked out. repo `CLAUDE.md` then
   *references* the harness ("agents: see harness X") instead of restating it —
   one source of truth, thin repo file.

**Trade-off to decide:** move 3 takes agent-guidance out of the repo where human
devs read it inline. Mitigation: keep a thin reference line in repo `CLAUDE.md`
pointing at the harness binding, so devs still see the pointer.

## 10. Managed region + write-gate (FIR-1775 follow-up, Jesper Q1+Q2)

Jesper's two refinements connect into one mechanism. Keep repo instructions
physically in the repo (the runtime loads them off disk; devs see them), but
make the Agent Office interface the **edit + version surface**, and protect the
result with a gate.

**Managed region (answers Q2 — version in the interface, ship via deploy).**
Split each repo `CLAUDE.md`/`AGENTS.md` into two parts:
- a **human-owned** part — code facts (architecture, build, conventions),
  edited normally via PRs;
- an **Agent Office-managed region**, delimited by markers
  (`<!-- agent-office:start vN -->` … `<!-- agent-office:end -->`), whose
  content is authored + versioned in the interface (a repo-scoped harness
  binding, §9 move 3). On deploy the interface syncs its current version into
  that region and commits, so the guidance **travels with the repo via git** —
  exactly Jesper's "røg de med i deploy", while staying governed/versioned.

**Write-gate (answers Q1 — can we gate writes? yes).** Defense in depth, and we
already own the pattern in this repo (`scripts/validate-cerebro-patches.sh` +
`scripts/cerebro-zones.txt`, run as the `upstream-zone-guard` CI job):
1. **Runtime hook (first line, helpful but bypassable):** a `PreToolUse` hook on
   `Write`/`Edit`/`MultiEdit` matching `**/CLAUDE.md` and `**/AGENTS.md` that
   blocks edits to the managed region and tells the agent to route the rule
   through the agent-instructions interface instead. Catches the common path
   and teaches the right one.
2. **CI guard (the real, un-bypassable gate):** a glob-driven check — same shape
   as the cerebro-zone guard — that **rejects any commit touching the managed
   region unless it came from the interface** (region carries a version marker /
   checksum; interface-authored sync commits are recognised). A shell `echo >>`
   that dodges the runtime hook is still caught here at the git layer.

**Important nuance — don't hard-block the whole file.** The gate targets the
*managed region* and *agent-authored* writes, not the human-owned code-facts part
and not human PRs. A blanket block on all `CLAUDE.md` writes would also stop
legitimate code-convention updates, which belong in the repo. So: gate the
managed region, leave the rest free.

**Shipped (marker format + CI guard + CLI):**
- Marker format (canonical source
  `server/internal/cerebro/agentoffice/managed_region.go`):
  `<!-- agent-office:start vN sha256:<64-hex> -->` … `<!-- agent-office:end -->`.
  The start marker **seals** the body: the checksum is the SHA-256 of the body
  in canonical form (lines strictly between the markers, each stripped of a
  trailing CR and terminated by LF). One region per file, integer version.
- CI guard: `scripts/validate-agent-office-region.sh` (step in the
  `upstream-zone-guard` job, `--test` self-test built in). Rejects: broken
  seal (hand-edit), region removal / deletion of a region-carrying file, body
  change without a version bump, version decrease, malformed markers. Files
  without a region are untouched. Human override:
  `AGENT-OFFICE-ALLOW-REGION:` commit token or
  `AGENT_OFFICE_ALLOW_REGION_EDIT=1`.
- Sanctioned write path: `multica agent context region-sync <file>`
  (`--content-file`/`--content-stdin`, auto-bumps the version) and
  `multica agent context region-verify <file>` (exit 1 on a broken seal).
- Honest limit: the seal proves the region was written by the tooling, not by
  a stray `echo >>` — a determined agent could re-seal by hand. Full
  provenance ("came from the interface") lands with the repo-scoped harness
  binding (§9 move 3), which will record the region version server-side.

**Open (next bricks):** the runtime `PreToolUse` hook (move 1 of the gate),
and the repo-scoped harness binding + deploy-time sync that makes the
interface the actual author of the region content.
