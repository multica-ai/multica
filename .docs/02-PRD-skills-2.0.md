# PRD: Skills 2.0 — Closed Loop + Versioning

**Status**: Design. No code written.
**Owner**: TBD (you, the implementing agent)
**Companion docs**: read `00-CONTEXT-PRIMER.md` first if you haven't.
**Estimated scope**: 7 PRs, ~10 sprints / ~2.5 months for one developer full-time.

---

## 1. One-paragraph summary

Evolve Multica's `skill` system into a complete lifecycle: agents can autonomously **propose** new skills and **patch** existing ones (with surgical fuzzy find/replace) at the end of complex tasks; agent-authored skills enter `pending_review` until promoted by a human or a curator; every mutation creates an append-only revision so any version can be restored; a workspace-scoped **curator** runs on a schedule, deterministically transitioning unused skills `active→stale→archived` and optionally invoking an LLM-based consolidation phase; a static regex scanner ported from Hermes blocks dangerous content before it ever reaches the database; pinning protects critical skills from any auto-transition. Existing skills, agents, and consumption flows continue to work unchanged.

---

## 2. Origin & motivation

We studied Hermes Agent (Nous Research's agent at `repos/hermes-agent/`), which has a true **closed learning loop** for skills:

- After complex tasks, the agent autonomously calls `skill_manage(action='create')` (Hermes `tools/skill_manager_tool.py`).
- A "skill nudge" mechanism in `run_agent.py` increments a counter every iteration and triggers the prompt at configurable intervals (`skills.creation_nudge_interval`, default 10).
- During idle periods (>2h since last activity, >7d since last curator run), Hermes spawns a background-review fork that runs `agent/curator.py:CURATOR_REVIEW_PROMPT`.
- The curator does TWO phases: (1) **deterministic** state-machine transitions (active→stale→archived based on usage timestamps) — pure function, no LLM; (2) **LLM-driven** consolidation — finds prefix clusters of similar skills and merges them into umbrella skills.
- Every operation honors `pinned` skills (never archived).
- Provenance is tracked via Python `ContextVar` (`tools/skill_provenance.py`) so the system knows whether a write was foreground (user-directed) or background (curator-fork).
- A static regex scanner (`tools/skills_guard.py`) blocks `dangerous` content (rm -rf /, exfil patterns, persistence backdoors) before write — gated by `skills.guard_agent_created` config.
- Hermes does NOT preserve per-edit history — only coarse tar.gz snapshots before each curator run (`agent/curator_backup.py`).

**Multica today** has none of this:
- `skill` is a flat table with one mutable `content` field. No history. No lifecycle. No agent-side mutation flow. No curator. No scanner.
- `LoadAgentSkills` (in `server/internal/service/task.go:1152-1182`) just dumps active skills into the task response.

**This PRD adds the closed loop AND fixes the gap Hermes leaves open** (per-edit versioning) by using an append-only `skill_revision` table instead of tarball snapshots. Result: every mutation is auditable and individually restorable, not just "rollback to last curator run".

---

## 3. Goals & non-goals

### Goals
1. Append-only `skill_revision` table; `multica skill history` and `revert --to-rev N` work for every skill.
2. Agent-callable `propose` and `patch` endpoints (via `mdt_*` daemon token).
3. `pending_review` lifecycle for agent-authored skills; promote/reject by human or by curator.
4. Curator service with two phases (deterministic + optional LLM), workspace-scoped, scheduled.
5. Pinning protects skills from auto-transitions.
6. Static regex scanner ported from Hermes blocks dangerous content.
7. Telemetry: `skill_usage_event` powers curator decisions + product analytics.
8. Backward-compatible: every existing skill keeps working with no migration on the agent's side.
9. **Reuse the revisioning helper, fuzzy patch helper, and provenance helper from the Second Brain PRD** (`01-PRD-second-brain.md` §5.4–5.5 and §5.3 service patterns). If Second Brain hasn't been built yet, build the helpers in this PR.

### Non-goals (this PRD)
- Multi-skill consolidation that requires graph reasoning beyond prefix clustering (defer to v2).
- Skill marketplace / cross-workspace skill sharing.
- Skill versioning visible *to the agent* in real-time mid-run (the agent always sees the current revision; it never accesses history programmatically).
- Replacing the existing `multica skill` CLI commands (we extend, never remove).
- Auto-translating skills (LLM-driven multi-language SKILL.md) — out of scope.
- Skill-level ACL (today: workspace membership = full access; same here).

---

## 4. Current state of Multica (load-bearing facts)

Read these in `repos/multica/`:

| File | What's there |
|---|---|
| `server/migrations/008_structured_skills.up.sql` | The current schema: `skill`, `skill_file`, `agent_skill` (junction). No version columns; no audit. |
| `server/pkg/db/queries/skill.sql` | CRUD only. `UpdateSkill` does COALESCE-based partial update (destructive). |
| `server/pkg/db/generated/skill.sql.go` | sqlc-generated types. |
| `server/internal/handler/skill.go` | 11 endpoints (list, get, create, update, delete, import, files CRUD, attach to agent). All workspace-scoped via `loadSkillForUser()`. |
| `server/internal/service/task.go:1152-1182` | `LoadAgentSkills(ctx, agentID)` — fetches skills + files for the task response. |
| `server/internal/daemon/types.go:76-80` | `SkillData` struct delivered to the daemon. |
| `server/cmd/multica/cmd_skill.go` | CLI commands (list/get/create/update/delete/import + files) — already complete for humans. |
| `apps/web/.../views/skills/` (and adjacent agent views) | Existing skill UI: create/edit/attach/detach. No history. |

**Key observation**: the existing schema's `(workspace_id, name)` UNIQUE constraint is the natural anchor for revisions — revisions belong to a `skill_id` which has a stable name within a workspace.

---

## 5. Proposed design

### 5.1 Schema

#### Migration: `XXX_skill_revisions_and_lifecycle.up.sql`

```sql
-- Append-only revision history for every skill mutation.
CREATE TABLE skill_revision (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    revision_number INT  NOT NULL,
    parent_revision UUID REFERENCES skill_revision(id),
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    content         TEXT NOT NULL DEFAULT '',
    config          JSONB NOT NULL DEFAULT '{}',
    files_snapshot  JSONB NOT NULL DEFAULT '[]',  -- [{path, content}]; full inline copy
    -- Provenance (matches workspace_document_revision exactly — keep formats identical)
    author_type     TEXT NOT NULL CHECK (author_type IN
        ('human','agent_foreground','agent_background','curator','import')),
    author_id       UUID,
    task_id         UUID REFERENCES agent_task_queue(id),
    operation       TEXT NOT NULL CHECK (operation IN
        ('create','edit','patch','consolidate','revert','rename')),
    patch_diff      JSONB,                          -- for operation='patch': {find, replace}
    change_summary  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (skill_id, revision_number)
);
CREATE INDEX idx_skill_revision_skill ON skill_revision(skill_id, revision_number DESC);
CREATE INDEX idx_skill_revision_task ON skill_revision(task_id);

-- Lifecycle + curator state on the existing skill table.
ALTER TABLE skill
    ADD COLUMN lifecycle_state TEXT NOT NULL DEFAULT 'active'
        CHECK (lifecycle_state IN ('pending_review','active','stale','archived','deprecated')),
    ADD COLUMN current_revision_id UUID REFERENCES skill_revision(id),
    ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN archived_at TIMESTAMPTZ,
    ADD COLUMN last_used_at TIMESTAMPTZ,
    ADD COLUMN use_count INT NOT NULL DEFAULT 0,
    ADD COLUMN provenance TEXT NOT NULL DEFAULT 'human'
        CHECK (provenance IN ('human','agent','curator','imported','bundled'));

CREATE INDEX idx_skill_lifecycle ON skill(workspace_id, lifecycle_state)
    WHERE archived_at IS NULL;
CREATE INDEX idx_skill_pending_review ON skill(workspace_id)
    WHERE lifecycle_state = 'pending_review' AND archived_at IS NULL;

-- Telemetry — every skill load + every reference recorded.
CREATE TABLE skill_usage_event (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id    UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    task_id     UUID REFERENCES agent_task_queue(id),
    agent_id    UUID REFERENCES agent(id),
    event_type  TEXT NOT NULL CHECK (event_type IN
        ('listed','viewed','referenced','patched_by_agent')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_skill_usage_skill_time
    ON skill_usage_event(skill_id, created_at DESC);
CREATE INDEX idx_skill_usage_workspace_time
    ON skill_usage_event(workspace_id, created_at DESC);

-- Curator runs.
CREATE TABLE curator_run (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    triggered_by  TEXT NOT NULL CHECK (triggered_by IN ('cron','manual','threshold')),
    status        TEXT NOT NULL CHECK (status IN
        ('running','succeeded','failed','rolled_back')),
    auto_transitions_applied INT NOT NULL DEFAULT 0,
    consolidations_applied   INT NOT NULL DEFAULT 0,
    snapshot_skill_revisions JSONB NOT NULL DEFAULT '[]',
        -- [{skill_id, revision_id_before}] for rollback
    error         TEXT,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_curator_run_workspace_time
    ON curator_run(workspace_id, started_at DESC);

-- Workspace-level curator config.
ALTER TABLE workspace
    ADD COLUMN curator_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN curator_interval_hours INT NOT NULL DEFAULT 168,  -- weekly
    ADD COLUMN curator_min_idle_hours INT NOT NULL DEFAULT 2,
    ADD COLUMN curator_stale_after_days INT NOT NULL DEFAULT 30,
    ADD COLUMN curator_archive_after_days INT NOT NULL DEFAULT 90,
    ADD COLUMN skills_guard_enabled BOOLEAN NOT NULL DEFAULT true,
        -- if true, scanner runs on agent-authored skill content
    ADD COLUMN skills_agent_auto_promote BOOLEAN NOT NULL DEFAULT false;
        -- if true, skip pending_review for agent skills (NOT recommended in v1)
```

Down migration mirrors with reverse DROPs/REMOVE COLUMNs.

#### Bootstrap of revision 1 for existing skills
A separate one-shot migration (or service init step) inserts revision 1 for every pre-existing skill:

```sql
INSERT INTO skill_revision (skill_id, revision_number, name, description, content, config,
                             files_snapshot, author_type, operation, change_summary)
SELECT s.id, 1, s.name, s.description, s.content, s.config,
       COALESCE(jsonb_agg(jsonb_build_object('path', f.path, 'content', f.content))
                FILTER (WHERE f.id IS NOT NULL), '[]'),
       'import', 'create', 'Imported pre-Skills-2.0 baseline'
FROM skill s
LEFT JOIN skill_file f ON f.skill_id = s.id
GROUP BY s.id;

UPDATE skill s
SET current_revision_id = (
    SELECT id FROM skill_revision r
    WHERE r.skill_id = s.id AND r.revision_number = 1
);
```

This makes the "history" tab work immediately on day 1 without forcing humans to re-save anything.

### 5.2 SQL queries (sqlc)

Create `server/pkg/db/queries/skill_revision.sql`, `server/pkg/db/queries/skill_usage.sql`, `server/pkg/db/queries/curator.sql`. Edit `server/pkg/db/queries/skill.sql` to add lifecycle queries and have UpdateSkill include the new columns.

Required new queries (signatures):

```sql
-- skill_revision.sql
-- name: InsertSkillRevision :one
INSERT INTO skill_revision (skill_id, revision_number, parent_revision, name, description,
    content, config, files_snapshot, author_type, author_id, task_id, operation,
    patch_diff, change_summary)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: GetMaxSkillRevisionNumber :one
SELECT COALESCE(MAX(revision_number),0)::int FROM skill_revision WHERE skill_id = $1;

-- name: ListSkillRevisions :many
SELECT id, revision_number, author_type, author_id, task_id, operation,
       change_summary, created_at
FROM skill_revision WHERE skill_id = $1 ORDER BY revision_number DESC;

-- name: GetSkillRevision :one
SELECT * FROM skill_revision
WHERE skill_id = $1 AND revision_number = $2;

-- skill.sql additions
-- name: ListSkillsByLifecycle :many
SELECT * FROM skill WHERE workspace_id = $1 AND lifecycle_state = $2 AND archived_at IS NULL;

-- name: ListPendingReviewSkills :many
SELECT * FROM skill WHERE workspace_id = $1 AND lifecycle_state = 'pending_review' AND archived_at IS NULL;

-- name: SetSkillLifecycle :exec
UPDATE skill SET lifecycle_state = $2 WHERE id = $1;

-- name: SetSkillPinned :exec
UPDATE skill SET pinned = $2 WHERE id = $1;

-- name: ArchiveSkill :exec
UPDATE skill SET archived_at = now(), lifecycle_state = 'archived' WHERE id = $1;

-- name: TouchSkillUsage :exec
UPDATE skill SET last_used_at = now(), use_count = use_count + 1 WHERE id = $1;

-- name: UpdateSkillCurrentRevision :exec
UPDATE skill SET current_revision_id = $2 WHERE id = $1;

-- skill_usage.sql
-- name: RecordSkillUsage :exec
INSERT INTO skill_usage_event (skill_id, workspace_id, task_id, agent_id, event_type)
VALUES ($1, $2, $3, $4, $5);

-- name: ListSkillUsageBetween :many
SELECT skill_id, COUNT(*) AS uses
FROM skill_usage_event
WHERE workspace_id = $1 AND created_at BETWEEN $2 AND $3
GROUP BY skill_id;

-- name: SkillsWithoutUsageSince :many
SELECT s.id, s.name, s.last_used_at
FROM skill s
WHERE s.workspace_id = $1 AND s.archived_at IS NULL AND s.pinned = false
  AND COALESCE(s.last_used_at, s.created_at) < $2;

-- curator.sql
-- name: CreateCuratorRun :one
INSERT INTO curator_run (workspace_id, triggered_by, status, snapshot_skill_revisions)
VALUES ($1, $2, 'running', $3) RETURNING *;

-- name: UpdateCuratorRunCounts :exec
UPDATE curator_run
SET auto_transitions_applied = $2, consolidations_applied = $3
WHERE id = $1;

-- name: FinishCuratorRun :exec
UPDATE curator_run SET status = $2, error = $3, finished_at = now() WHERE id = $1;

-- name: ListCuratorRuns :many
SELECT * FROM curator_run WHERE workspace_id = $1 ORDER BY started_at DESC LIMIT $2;

-- name: GetCuratorRun :one
SELECT * FROM curator_run WHERE id = $1;
```

After writing, `make sqlc`, commit `server/pkg/db/generated/`.

### 5.3 Service layer

Create `server/internal/service/skill_revision.go`, `server/internal/service/curator.go`, `server/internal/service/skill_scanner.go`. Extend `server/internal/service/skill.go`.

Key methods (signatures):

```go
// Extends existing SkillService.

func (s *SkillService) Propose(
    ctx context.Context,
    workspaceID uuid.UUID,
    payload SkillPayload, // {name, description, content, files, tags?, config?}
    summary string,
) (*Skill, error)
// Provenance from ctx; lifecycle = pending_review unless workspace.skills_agent_auto_promote
// Runs scanner first; if dangerous, return 422 with the threat report.

func (s *SkillService) Patch(
    ctx context.Context,
    skillID uuid.UUID,
    findText, replaceText, summary string,
) (*Skill, error)
// Uses textpatch.FuzzyReplace (shared with Second Brain).
// Records operation='patch' with patch_diff = {find, replace}.

func (s *SkillService) Revert(
    ctx context.Context,
    skillID uuid.UUID,
    revisionNumber int,
    summary string,
) (*Skill, error)

func (s *SkillService) Pin(ctx context.Context, skillID uuid.UUID) error
func (s *SkillService) Unpin(ctx context.Context, skillID uuid.UUID) error

func (s *SkillService) Archive(
    ctx context.Context,
    skillID uuid.UUID,
    reason string,
) error

func (s *SkillService) Promote(
    ctx context.Context,
    skillID uuid.UUID,
) error
// pending_review → active. Records revision with operation='consolidate', author=human/curator.

// CuratorService is new.
type CuratorService struct {
    Queries *db.Queries
    DB      *pgxpool.Pool
    Skills  *SkillService
    Tasks   *TaskService     // to spawn the curator-agent task in phase 2
    Bus     *eventbus.Bus
    Logger  *log.Logger
}

func (c *CuratorService) Run(
    ctx context.Context,
    workspaceID uuid.UUID,
    triggeredBy string, // 'cron' | 'manual' | 'threshold'
) (*CuratorRun, error)
// Phase 1: SkillsWithoutUsageSince(now-staleAfterDays) → set lifecycle='stale' (skip pinned)
//          SkillsWithoutUsageSince(now-archiveAfterDays) AND lifecycle='stale' → Archive
//          Each transition: writes skill_revision (operation='consolidate', author='curator')
// Phase 2 (optional): if workspace has > N pending_review skills OR clusters detected,
//          spawn a "curator-agent" task (a special seeded agent named 'curator', model=Sonnet)
//          with an issue body that lists the skills and a prompt derived from
//          repos/hermes-agent/agent/curator.py:CURATOR_REVIEW_PROMPT (lines 329-444).
//          That agent runs as a normal Multica task; its writes pass through the
//          standard Propose/Patch/Archive APIs with provenance = agent_background.

func (c *CuratorService) Rollback(
    ctx context.Context,
    runID uuid.UUID,
) error
// For each (skill_id, revision_id_before) in snapshot_skill_revisions:
//   Create a new revision (operation='revert', author='curator') restoring that content.

// SkillScanner is new.
type SkillScanner struct {
    patterns []ThreatPattern // loaded from a hardcoded list (see §5.6)
}

type ScanVerdict string
const (
    VerdictSafe      ScanVerdict = "safe"
    VerdictCaution   ScanVerdict = "caution"
    VerdictDangerous ScanVerdict = "dangerous"
)

type ScanReport struct {
    Verdict  ScanVerdict
    Findings []ScanFinding // {pattern_name, snippet, line, severity}
}

func (s *SkillScanner) Scan(content string, files []SkillFile) ScanReport
```

**Critical**: as in Second Brain, every mutation method writes the revision and updates the `skill` row in a single transaction (`pgxpool.BeginTx`).

### 5.4 Provenance helper

Same as in `01-PRD-second-brain.md` §5.4. If not yet built, implement `server/internal/middleware/provenance.go`. The agent_background variant is set when the request includes `X-Curator-Run-ID` header (which the curator-agent task sets when it calls Propose/Patch/Archive).

### 5.5 Fuzzy patch helper

Same as in `01-PRD-second-brain.md` §5.5. `server/internal/textpatch/fuzzy.go`. Reuse for both `multica skill patch` and `multica doc patch`.

### 5.6 Skill scanner — port of Hermes `tools/skills_guard.py`

**Read** `repos/hermes-agent/tools/skills_guard.py` (lines 1–250) carefully.

Implement `server/internal/service/skill_scanner.go` in Go. The scanner is purely **regex-based**, no LLM. Threat patterns to port (each as a `ThreatPattern{Name, Regex, Severity}`):

| Category | Examples | Severity |
|---|---|---|
| Exfiltration | `curl|wget` with `$ENV`/env vars; reads of `~/.ssh`, `~/.aws`, `~/.kube`, `~/.docker`, `.env*` files; DNS tunneling patterns | dangerous |
| Injection | shell command injection markers; ignore-previous-instructions | dangerous |
| Destructive | `rm\s+-rf\s+/`, `dd if=/dev/zero`, fork bombs | dangerous |
| Persistence | edits to `bashrc`, `zshrc`, cron, systemd unit creation | caution |
| Network callback | `nc\s+-l`, `bash\s+-i\s+>&\s*/dev/tcp/` | dangerous |
| Obfuscation | `base64 -d` followed by `bash`/`sh`, eval-of-decoded patterns | dangerous |

**Verdict mapping** (mirror Hermes):
- `dangerous` finding → verdict `dangerous`
- `caution` finding → verdict `caution`
- nothing matches → verdict `safe`

**Trust matrix** for what to do with the verdict, decided in the service layer:
- `provenance='bundled'` (shipped with multica): all verdicts allowed (trusted by design).
- `provenance='imported'` (from URL): allow `safe`+`caution`, block `dangerous`.
- `provenance='agent'` (created by agent): if `workspace.skills_guard_enabled = true`, allow `safe`+`caution`, block `dangerous`. Otherwise allow all.
- `provenance='human'` (created via UI/CLI by user): always allow (humans aren't the threat model — they can already run shell commands).

When blocked: HTTP 422 with the `ScanReport` in the body so the agent (or human) can see why and adjust.

**Test coverage required**: at least one test case per threat pattern + safe baselines.

### 5.7 HTTP handlers

Edit `server/internal/handler/skill.go` to add:

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/skills/propose` | daemon ✓ user ✓ | Body: `{name, description, content, tags, files, summary}`. Returns 422 if scanner blocks. |
| POST | `/api/skills/{id}/patch` | daemon ✓ user ✓ | Body: `{find, replace, summary}` |
| POST | `/api/skills/{id}/revisions` | daemon ✓ user ✓ | Full overwrite + revision; body: `{content, summary}` |
| GET | `/api/skills/{id}/revisions` | daemon ✓ user ✓ | List revisions |
| GET | `/api/skills/{id}/revisions/{n}` | daemon ✓ user ✓ | Get rev N |
| POST | `/api/skills/{id}/revert` | user ✓ daemon ✗ | Body: `{revisionNumber, summary}` |
| POST | `/api/skills/{id}/pin` | user ✓ daemon ✗ | Pin |
| POST | `/api/skills/{id}/unpin` | user ✓ daemon ✗ | Unpin |
| POST | `/api/skills/{id}/archive` | user ✓ daemon ✓ | Body: `{reason}` (agent can request archive; lifecycle goes to archived) |
| POST | `/api/skills/{id}/promote` | user ✓ daemon ✗ | pending_review → active |
| GET | `/api/skills?lifecycle=pending_review` | user ✓ daemon ✓ | Filter |
| GET | `/api/skills/pending-review/queue` | user ✓ | Convenience: docs await in pending_review |

Create `server/internal/handler/curator.go`:

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/api/workspaces/{ws}/curator/runs` | user ✓ | Trigger now |
| GET | `/api/workspaces/{ws}/curator/runs?limit=N` | user ✓ | History |
| GET | `/api/workspaces/{ws}/curator/runs/{id}` | user ✓ | Detail |
| POST | `/api/workspaces/{ws}/curator/runs/{id}/rollback` | user ✓ | Revert all changes from this run |
| GET | `/api/workspaces/{ws}/curator/config` | user ✓ | Get knobs (interval, thresholds, scanner toggle) |
| PUT | `/api/workspaces/{ws}/curator/config` | user ✓ | Update knobs |

Register in `server/cmd/server/router.go`. Update `server/internal/middleware/daemon_auth.go` to allow daemon tokens on the routes marked "daemon ✓".

### 5.8 Daemon-side: usage event recording

Edit `server/internal/handler/daemon.go:ClaimTaskByRuntime` (or wherever `LoadAgentSkills` is called):

After loading skills, record one `skill_usage_event` per loaded skill with `event_type='listed'` + `task_id`. Do this asynchronously (`go` with bounded worker pool) — don't block the claim response.

Optionally, instrument the agent CLI to send `event_type='referenced'` when the skill is read (would require hooking into the agent's tool invocations — defer; `'listed'` is enough for curator decisions).

### 5.9 Skill nudge in CLAUDE.md

Edit `server/internal/daemon/execenv/runtime_config.go:buildMetaSkillContent()`. After the existing skills section (and after the Second Brain sections if that PRD landed first), append:

```markdown
## Skills hygiene

If during this task you developed a reusable approach (a recipe, a script, a
checklist, a debugging pattern), consider proposing it as a skill so future
runs can reuse it:

  multica skill propose --name <kebab-case-name> --content-stdin <<'SKILL'
  ... markdown body ...
  SKILL

Or patch an existing skill if you found a better way:

  multica skill patch <skill-id> --find "..." --replace "..." --summary "..."

Skills you propose enter `pending_review` and require approval before being
loaded into future tasks. The proposal will be visible to the workspace owner.
Don't propose trivial or task-specific knowledge — only things genuinely
reusable across multiple future tasks.
```

This is the **non-LLM equivalent of Hermes's skill nudge**. We rely on Claude/Codex/etc. to pick up the cue from this static instruction. If adoption is low (measured via `skill_usage_event` + count of proposals/week), revisit with a dynamic post-task review fork (more complex; defer).

### 5.10 CLI commands

Edit `server/cmd/multica/cmd_skill.go`. Add subcommands:

```
multica skill propose --name <n> [--description D] [--tags t1,t2] --content-stdin
multica skill patch <id> --find "..." --replace "..." [--summary "..."]
multica skill revise <id> --content-stdin --summary "..."
multica skill history <id> [--output json]
multica skill show <id> --rev N
multica skill diff <id> --from N --to M
multica skill revert <id> --to-rev N [--summary "..."]
multica skill pin <id>
multica skill unpin <id>
multica skill archive <id> [--reason "..."]
multica skill promote <id>          # human-only (CLI rejects mdt_*)
multica skill list [--lifecycle pending_review|stale|active|archived] [--pinned]
multica skill pending [--output json]    # convenience for "what needs my review"

multica curator run                  # workspace-scoped
multica curator runs [--limit N]
multica curator rollback <run-id>
multica curator config show / set ...
```

Existing commands (`list`, `get`, `create`, `update`, `delete`, `import`, `files ...`) remain unchanged. New commands inherit the same auth pattern (token from `~/.multica/config.json`).

### 5.11 Frontend

In `apps/web/.../views/skills/` (or shared `packages/views/skills/`):

1. **Skill detail page** — add tabs: "Content" (existing) | "Files" (existing) | "History" (new) | "Usage" (new).
   - History tab: list of revisions with author badge (human/agent/curator), summary, timestamp; click → side-by-side diff with current; "Restore this version" button.
   - Usage tab: chart of usage events (last 30/90 days) + table of which tasks loaded this skill.
2. **Skill list page** — filter by lifecycle; visible badge for `pending_review` / `stale` / `archived`. Pin toggle in row actions.
3. **Pending review queue** — new page `/skills/pending` showing cards: name, proposed by (agent/run), description, content preview, "Promote" / "Reject" buttons.
4. **Provenance pill** on every skill card: human / agent / curator / imported / bundled.
5. **Curator page** `/curator`:
   - Runs table (status, started_at, finished_at, transitions_count, consolidations_count)
   - Run detail: list of skills affected with their before/after revision; "Rollback this run" button (with confirmation)
   - Config form (intervals, thresholds, scanner toggle, auto-promote toggle)

---

## 6. Phased rollout (PRs)

| PR | Title | Files touched | Approx LOC |
|---|---|---|---|
| **B1** | `feat(skill): schema for revisions + lifecycle, bootstrap rev 1` | new migration, queries, sqlc, service writes revision on every legacy update — **transparent, no user-visible feature** | ~700 |
| **B2** | `feat(skill): history API + UI tab` | history endpoint, frontend tab with diff + restore | ~600 |
| **B3** | `feat(skill): propose + patch endpoints, scanner, pending review queue` | new handlers, scanner port, UI promote/reject | ~1500 |
| **B4** | `feat(skill): CLI for new operations` | cmd_skill.go additions | ~400 |
| **B5** | `feat(daemon): skill nudge in CLAUDE.md` | runtime_config.go (1 paragraph) | ~50 |
| **B6** | `feat(curator): scheduler + phase 1 (auto-transitions) + UI` | service/curator.go (phase 1 only), handler, scheduler in cmd/server, /curator UI | ~1200 |
| **B7** | `feat(curator): phase 2 LLM consolidation + budget cap + rollback` | service/curator.go phase 2, budget enforcement, rollback endpoint, curator-agent seed | ~900 |

Sequence the PRs strictly: B1 establishes revision writes on every legacy update path so by the time B2 adds history UI, every skill already has at least one revision (revision 1 from bootstrap or revision N from organic edits since B1).

---

## 7. Test plan

### Unit (Go)
- `service/skill_test.go`: every method (Propose, Patch, Revert, Pin, Unpin, Archive, Promote) round-trips; revision created with correct provenance.
- `service/skill_scanner_test.go`: one positive + one negative case per threat pattern; verdict matrix (5 provenance × 3 verdict = 15 cases) returns the right allow/block.
- `service/curator_test.go`: phase 1 promotes/demotes correctly; pinned skills are skipped; rollback restores; idle threshold respected.
- `textpatch/fuzzy_test.go`: same as Second Brain.
- `middleware/provenance_test.go`: same as Second Brain; plus `agent_background` triggered by `X-Curator-Run-ID` header.

### Integration (Go, with test Postgres)
- Bootstrap migration: applying it to an existing DB with 5 skills creates exactly 5 revisions.
- Workspace isolation: user in WS-A cannot see/promote a pending skill in WS-B.
- Curator: run on a workspace with 1 stale skill → 1 transition; lifecycle changes; revision created.
- ClaimTaskByRuntime emits `skill_usage_event` rows.

### CLI (shell)
- `multica skill propose` round-trips; lifecycle = pending_review; `multica skill list --lifecycle pending_review` shows it.
- `multica skill patch` works with fuzzy match; rejects with 422 if find text missing.
- `multica skill revert --to-rev N` restores content.
- `multica curator run` returns a CuratorRun ID; `runs` lists it.

### E2E (Playwright)
- Human creates a skill via UI → revision 1 visible.
- Human assigns issue to an agent; agent runs `multica skill propose ...`; human sees pending review queue with the new skill.
- Human clicks "Promote" → lifecycle becomes active; revision created.
- Human triggers curator manually → at least one stale skill is archived; rollback restores it.
- Scanner blocks: agent attempts `multica skill propose` with `rm -rf /` in content; CLI exit code != 0; UI does not show skill.

### Curator integration
- Spin up a workspace with curator_enabled=true; mock 'time advance'; verify cron triggers.
- LLM phase 2 mocked: don't actually call Claude in tests; assert that the curator-agent task gets enqueued with the right prompt structure.

---

## 8. Acceptance criteria

After all 7 PRs merge, the system MUST:

1. Preserve every prior version of every skill (existing + new) in `skill_revision`. `multica skill history` shows them; `revert --to-rev N` restores.
2. Allow agents (via `mdt_*`) to call `propose` and `patch`. Agent-authored skills enter `pending_review` (unless `workspace.skills_agent_auto_promote=true`).
3. Reject dangerous content from agent-authored skills via the regex scanner; allow safe and caution.
4. Provide a workspace owner UI to see pending skills, promote/reject, and view full content + provenance + summary.
5. Run a workspace-scoped curator on schedule (default weekly) and on demand. Phase 1 (deterministic) transitions skills active→stale→archived per workspace knobs; pinned skills are protected.
6. Optionally run phase 2 (LLM consolidation) if enabled; spawned curator-agent task makes its mutations through the same Propose/Patch/Archive API with provenance=`agent_background`.
7. Allow rollback of any curator run via `/api/curator/runs/{id}/rollback` — creates revert revisions for every skill changed.
8. Inject the skill nudge instruction into CLAUDE.md/AGENTS.md/GEMINI.md so agents know how to propose.
9. Record `skill_usage_event` on every task that loads skills.
10. Pass all unit, integration, CLI round-trip, and E2E tests.
11. Existing flows (load skills into agent, attach skill to agent, etc.) unchanged.
12. Update `CONTRIBUTING.md` (or new `docs/skills-2.0.md`) with: lifecycle states diagram; example agent flow; example curator config.

---

## 9. Reference implementations to study

(All paths under `repos/hermes-agent/` unless noted.)

- **Skill manager actions**: `tools/skill_manager_tool.py` (lines 1–945). Read all 6 actions. Note the YAML frontmatter validation (lines 217–253), the `_apply_patch` fuzzy logic (465–560), the rollback-on-block pattern after scanner failure (408–411, 451–456, 552–555).
- **Curator (background self-improvement)**: `agent/curator.py` — note `should_run_now()` gates (lines 198–248), `apply_automatic_transitions()` (255–295) for phase 1, and `CURATOR_REVIEW_PROMPT` (329–444) for phase 2 prompt template.
- **Curator backups (the pattern we replace with append-only revisions)**: `agent/curator_backup.py`. Read to understand what they preserve so our revisions cover the same ground.
- **Provenance via ContextVar**: `tools/skill_provenance.py` — they use Python `ContextVar`; our equivalent is the request-scoped middleware setting `agent_background` when `X-Curator-Run-ID` is present.
- **Scanner**: `tools/skills_guard.py` — port the THREAT_PATTERNS list and the trust matrix to Go.
- **Skill nudge mechanism**: `run_agent.py` lines 1704, 1809, 10869, 13919–13923 — counter `_iters_since_skill`, default interval 10. Multica's static-instruction approach is simpler; the dynamic version (count tool calls per task and spawn a review subtask) is the fallback if static doesn't work (defer).
- **Lifecycle states**: `tools/skill_usage.py:40-43` — STATE_ACTIVE, STATE_STALE, STATE_ARCHIVED.
- **Skill loading at session start (compact index)**: `agent/prompt_builder.py:712-860` — note the on-disk snapshot caching (would be useful if our index render gets slow).

For Multica:
- **Existing skill schema**: `repos/multica/server/migrations/008_structured_skills.up.sql`
- **Existing skill handler**: `repos/multica/server/internal/handler/skill.go`
- **Existing skill loading**: `repos/multica/server/internal/service/task.go:1152-1182` (`LoadAgentSkills`)
- **Existing CLI**: `repos/multica/server/cmd/multica/cmd_skill.go`

---

## 10. Open questions (decide before B1 ships)

1. **Curator-agent seed**: which existing agent template do we use? Recommendation: create a new agent named `curator` in the seed migration with `model='claude-sonnet-4-6'`, instructions hardcoded from a derivative of `CURATOR_REVIEW_PROMPT`, `skills` empty (the curator doesn't load skills — it manages them).
2. **Auto-promote default**: stay false (recommended for safety) or true (faster value)? Recommendation: false in v1; can be flipped per-workspace later.
3. **Scanner bypass for trusted users**: workspace owners might hit false positives. Recommendation: a `force=true` query param on `propose`/`patch` that requires user-token (not daemon) and logs a `skill_scanner_override` audit event.
4. **Files in revisions**: `files_snapshot JSONB` inline vs content-addressable hash table. Inline is simpler; content-addressable saves storage when files barely change. Recommendation: inline in v1; revisit if measured pain.
5. **Curator budget**: token cap per workspace per month for phase 2? Recommendation: yes, with default $5/mo configurable; set conservatively.
6. **Pending-review notifications**: workspace owner gets a digest when skills enter pending. Recommendation: piggyback on existing notification pipeline if it exists; else add it later.
7. **Multi-tenant scanner customization**: allow workspace owner to add their own ban patterns? Recommendation: NO in v1 — keep the pattern set hardcoded; revisit if demand surfaces.
8. **Bootstrap revision provenance**: legacy skills with no recorded creator — what `author_id` to set on the bootstrap revision? Recommendation: NULL author_id, `author_type='import'`, `change_summary='Pre-Skills-2.0 baseline'`.
9. **Patch operation conflict**: two agents patch the same skill simultaneously — what happens? Recommendation: a `baseRevisionId` parameter on patch; if mismatch, 409 with current rev for the agent to retry. (Same pattern as Second Brain).
10. **Renaming a skill**: does it create a revision with `operation='rename'`? Recommendation: yes — preserves history that the skill was once called X.

---

## 11. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Agents flood pending_review with low-quality proposals | Workspace owner has reject button; curator phase 2 can also auto-archive pending older than X days; future rate-limit per-task |
| Scanner false positives block legitimate skill | `force=true` user-only override; scanner verdict + report in 422 response so user knows what tripped |
| Curator phase 2 destroys nuance via over-consolidation | Pin protects; rollback restores; bias prompt toward "merge only if redundancy is clear" |
| LLM-driven curator costs explode | Budget cap per workspace per month; refuses to start phase 2 if exceeded |
| Per-skill revision storage explodes | Inline files in revisions are the worst case; defer content-addressable until measured pain |
| Backward-compat: existing tools that called PUT /skill/{id} without revision-summary | Service layer infers `change_summary='Updated'` if missing |
| Agent stuck in loop trying to patch with bad fuzzy match | 422 response with reason; agent will re-read content and try a different find string. If it keeps failing, the per-task tool-call cap eventually stops it (existing Multica protection) |
| pending_review skills accidentally loaded into agents | `LoadAgentSkills` filters `lifecycle_state='active'` — only active skills go to agents |

---

## 12. Out of scope (future PRDs)

- Cross-workspace skill marketplace.
- LLM-based scanner (semantic threat detection rather than regex).
- Skill A/B testing (two revisions of the same skill served to different runs).
- Auto-translation of skills.
- Real-time co-editing in UI.
- Skill dependencies (skill A requires skill B).
- Per-skill ACL (today: workspace membership = full access).
- Hub / community skills (`agentskills.io` interop) — defer; format is already SKILL.md frontmatter so future-compatible.

---

## 13. Definition of done

- All 7 PRs merged to `main`.
- All tests in §7 passing in CI.
- Manual smoke: a fresh workspace, a human creates 2 skills (one pinned), runs an agent on a complex task, agent proposes 1 new skill, owner promotes via UI, curator runs (manual trigger) and archives 1 unused legacy skill, owner rolls back the curator run, archived skill returns.
- `CONTRIBUTING.md` has a "Skills 2.0" section with a state diagram (active/stale/archived/pending_review) + the agent flow.
- A migration runbook for self-hosters: `make migrate-up` then verify count of `skill_revision` rows == count of `skill` rows (one bootstrap revision per existing skill).

You're done. Now read `repos/multica/CLAUDE.md` for the project's house style, then `repos/multica/CONTRIBUTING.md` for dev setup, then start with PR B1.