# Runtime Pool and Session Affinity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` task-by-task. Every production change uses `superpowers:test-driven-development`; read `/Users/zxx/.codex/plugins/cache/openai-curated-remote/superpowers/6.2.0/skills/test-driven-development/writing-good-tests.md` before writing tests. Each task gets a fresh implementation subagent, then a spec-compliance review, then a code-quality review before the next task starts.

**Goal:** Add an opt-in, capability-based Runtime Pool that assigns an eligible Runtime at each Agent invocation, preserves per-Agent Provider Session affinity, and routes imported Extension Squads through the existing Multica task/daemon/CLI lifecycle without changing fixed Runtime behavior.

**Architecture:** Pool Agents first persist a workspace-scoped `waiting_runtime` Task. A bounded PostgreSQL allocator selects an authorized, compatible, alive Runtime and moves the Task into the existing `queued -> claim -> execute` lifecycle; continuations are pinned to their Session Runtime, and Chat resolves only one execution head at a time. Daemon machine capacity, CLI protocol, Sidecar context, Native Squad delegation, Session resume, and all fixed Agent paths remain existing Multica mechanisms.

**Tech Stack:** Go 1.26.1, PostgreSQL migrations and row locking, sqlc 1.31.1, Redis/Noop Runtime liveness, Multica HTTP/WebSocket protocol, TypeScript, React, React Native/Expo, pnpm/Corepack, Vitest, Node test runner, and the existing Go `platform-agent-cli` App Server adapter.

**Primary Spec:** `docs/superpowers/specs/2026-08-09-runtime-pool-session-affinity-design.md`

## Global Constraints

- Repository root is `/Users/zxx/Documents/技术学习/multica`; all Go commands run from `/Users/zxx/Documents/技术学习/multica/server`, which is the Go module root.
- Use `/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go` and a verified `/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc` reporting `v1.31.1`; never edit generated sqlc files manually.
- DB-backed tests must use the migrated integration database. A missing database is a test failure, never `t.Skip`; run `make migrate-up` from the repository root before their RED and GREEN runs.
- `fixed` stays the default. Existing Agents, Releases, enqueue paths, claim ordering, retries, Session resume, Runtime deletion, API compatibility, and UI gates must remain behavior-equivalent.
- Pool is explicit. Never infer it from `runtime_id IS NULL`, Provider name, or `runtime_config.platform_agent`; scheduler and claim SQL must not contain `provider = 'platform-agent-cli'`.
- Agent execution mode and Runtime location mode are distinct: `AgentExecutionMode = RuntimeLocationMode | "pool"`; `RuntimeLocationMode = "local" | "cloud"`.
- Placement scans exactly one `placement_workspace_id`, use `WaitingTaskScanLimit=64`, `RuntimeScanLimit=128`, `AssignmentBatchLimit=8`, and never call Redis while holding a database row lock.
- Every new transaction takes only the needed subsequence of `Comment -> Follow-up Obligation -> Workspace Member -> Agent Runtime -> Chat Session -> Agent -> Agent Task`; multiple rows of one type use UUID order. Allocator and Pool fresh/stale claims use `Member -> Runtime -> optional ChatSession -> Agent -> Task`; Runtime delete uses `Runtime -> optional ChatSession -> Agent -> Task`; the shared Pool Task factory re-reads and locks placement Member, then uses `Member -> ChatSession -> Agent -> Task` for Chat and `Member -> Agent -> Task` otherwise; Chat terminal/cancel/late-pin uses `ChatSession -> Agent -> Task`; obligation processing uses `Comment -> Obligation -> Member -> optional Runtime -> optional ChatSession -> Agent -> Task`; Member revoke uses `Member -> Runtime -> affected ChatSession -> Agent -> Task`. All in-scope paths migrate to these orders; `NOWAIT`/`SKIP LOCKED` is only a bounded race/retry mechanism at the final Task CAS, never permission to keep a reverse lock order. No Pool create path may authorize from a transaction-external `requireWorkspaceMember` snapshot.
- First-time placement requires strict Runtime idle. Pinned placement ignores busy and enters that Runtime's normal queue. Daemon semaphore remains the only machine-capacity authority.
- A Pool Task is claimable only by its assigned Runtime. Singular and batch claims preserve Agent max concurrency, Issue/Chat/Quick Create serialization, priority, FIFO, lease, and payload semantics.
- CLI App Server methods/events, Extension Sidecar, Skill/Command materialization, and Native Squad delegation protocol do not change.
- Every server routing task adds fixed-mode negative controls. Every task ends with the exact focused test, broader package verification, exact `git add`, and one commit.
- Action sizing is literal. For every numbered acceptance case, perform its own 2-5 minute loop: (1) add one named RED test, (2) run the exact `-run '^TestName$'` command and record the expected failure, (3) add the smallest production branch, and (4) rerun that exact command GREEN. A table-driven `cases` slice may share setup, but it never replaces these numbered RED/GREEN loops; commit only after every named case is GREEN.
- Every DB-backed RED/GREEN command block explicitly exports `DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'`; its test helper must call `t.Fatal` when the variable is missing or PostgreSQL cannot be reached. A newly added Pool test must never inherit a legacy helper that calls `t.Skip`.

## File and Dependency Map

| Unit | Responsibility | First task |
| --- | --- | --- |
| `server/internal/runtimepool` | Requirements, scheduler contracts, bounded allocator | 1, 3, 5 |
| `server/internal/runtimeaccess` | One owner/admin/public/private authorization policy | 3 |
| Migrations and `pkg/db/queries` | Routing snapshots, obligations, indexes, CAS, claim | 1, 2, 4-12 |
| `internal/service.TaskService` | Pool seam, entry routing, events, terminal wakes | 3, 7-12 |
| Handler/Router/Sweeper | Registration, production DI, APIs, lifecycle triggers | 4, 7, 13-14 |
| Core/Views/Mobile | Wire schemas, runnable gates, waiting UX | 15-17 |
| Acceptance harness | Two real Daemons, real CLI proxy, Leader delegation | 18 |

Dependencies are strict: 1 -> 2 -> 3 -> 4 -> 5 -> 6 -> 7; tasks 8-12 consume the wired scheduler; 13-14 complete server contracts before 15-17 consume them; 18 runs only after all prior tasks pass.

---

### Task 1: Persist routing contracts, requirements, and durable obligations

**Files:**
- Create: `server/migrations/267_runtime_pool_contract.up.sql`
- Create: `server/migrations/267_runtime_pool_contract.down.sql`
- Create: `server/migrations/268_comment_followup_id_unique.up.sql`, `server/migrations/268_comment_followup_id_unique.down.sql`
- Create: `server/migrations/269_comment_followup_primary_key.up.sql`, `server/migrations/269_comment_followup_primary_key.down.sql`
- Create: `server/migrations/270_comment_followup_agent_comment_unique.up.sql`, `server/migrations/270_comment_followup_agent_comment_unique.down.sql`
- Create: `server/migrations/271_comment_followup_fifo_index.up.sql`, `server/migrations/271_comment_followup_fifo_index.down.sql`
- Create: `server/internal/runtimepool/requirements.go`
- Create: `server/internal/runtimepool/requirements_test.go`
- Create: `server/internal/pooltestdb/postgres.go`, `server/internal/pooltestdb/postgres_test.go`
- Modify: `server/internal/migrations/migrations_lint_test.go`
- Modify: `server/internal/migrations/platform_extension_release_migration_test.go`
- Create: `server/internal/migrations/runtime_pool_contract_migration_test.go`
- Modify: `server/cmd/migrate/main.go`
- Modify: `server/cmd/migrate/migrate_invalid_index_test.go`
- Modify by sqlc generation: every actual diff under `server/pkg/db/generated/` (never hand-edit generated files)

**Interfaces:**
- Consumes: existing Agent, `agent_runtime`, `agent_task_queue`, `platform_extension_release`, and migration 261 terminal timestamp contract.
- Produces: `runtimepool.ParseRequirements(json.RawMessage) (Requirements, error)`, `runtimepool.CanonicalRequirements(Requirements) (json.RawMessage, error)`, `pooltestdb.Open`, `pooltestdb.OpenWithSearchPath`, binding/affinity/capability constants, all Pool columns, and `agent_comment_followup_obligation` keyed by `(agent_id, comment_id)`.

- [ ] **Step 1: Write the RED contracts.** Add table-driven parser cases for unknown/trailing fields, duplicate object keys (including escaped-equivalent keys), wrong schema, empty/duplicate/unsorted/invalid capabilities, 32/33 items, 128/129 bytes, and canonical JSON 4096/+1 bytes. Add migration tests for this truth table:

```text
fixed nonterminal: runtime_id required; affinity=none; placement/requester NULL; never waiting_runtime
pool waiting/deferred: runtime_id NULL; placement/requester required
pool queued/dispatched/running/waiting_local_directory: runtime_id required
terminal: completed_at required; nonterminal: completed_at NULL
affinity: unresolved+NULL | none+NULL | pinned+runtime | removed+NULL
unresolved: Pool Chat waiting/deferred + chat_predecessor_pending only
removed: newly cancelled Pool call + completed_at + session_runtime_removed only
explicit_fresh_session: runtime_binding_mode=pool AND affinity=none only; fixed rejects it
release: reservation(NULL,NULL), fixed(squad,runtime), pool(squad,NULL); unknown mode always rejected
```

Copy this first RED into `requirements_test.go`; extend its `cases` slice with the matrix above in separate 2-5 minute edits:

```go
package runtimepool

import (
    "encoding/json"
    "testing"
)

func TestParseRequirementsRejectsNonCanonicalCapabilities(t *testing.T) {
    cases := []struct{ name, raw string }{
        {"unsorted", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["z/v1","a/v1"]}`},
        {"duplicate", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["a/v1","a/v1"]}`},
        {"empty", `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":[""]}`},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if _, err := ParseRequirements(json.RawMessage(tc.raw)); err == nil {
                t.Fatalf("ParseRequirements(%s) succeeded; want error", tc.raw)
            }
        })
    }
}
```

Copy this migration preflight RED into `platform_extension_release_migration_test.go` before adding any new CHECK:

```go
func TestRuntimePoolContractMigrationPreflightsTerminalTimestamps(t *testing.T) {
    raw, err := os.ReadFile(filepath.Join(realMigrationsDir(t), "267_runtime_pool_contract.up.sql"))
    if err != nil { t.Fatal(err) }
    sql := string(raw)
    preflight := strings.Index(sql, "runtime_pool_terminal_timestamp_preflight")
    addCheck := strings.Index(sql, "ADD CONSTRAINT agent_task_queue_terminal_completed_at_check")
    validate := strings.Index(sql, "VALIDATE CONSTRAINT agent_task_queue_terminal_completed_at_check")
    if preflight < 0 || addCheck <= preflight || validate <= addCheck {
        t.Fatalf("preflight/add/validate order is invalid: %d/%d/%d", preflight, addCheck, validate)
    }
    if !strings.Contains(sql[addCheck:validate], "NOT VALID") {
        t.Fatal("terminal timestamp CHECK must be added NOT VALID before validation")
    }
}
```

Add `TestRuntimePoolContractTruthTable` in the same file. Use `pooltestdb.Open(t)`, insert one fixture row per truth-table branch in an isolated transaction, and assert valid rows commit while invalid rows return SQLSTATE `23514`. Numbered RED/GREEN loops are: (1) fixed lifecycle, (2) Pool lifecycle, (3) all four affinity pairs, (4) unresolved Chat-only, (5) removed cancellation, (6) explicit fresh (`pool+none` succeeds; `fixed+none` fails), and (7) reservation/fixed/pool Release shapes. Run each loop with `-run '^TestRuntimePoolContractTruthTable/<case>$'` before proceeding.

Add `TestRuntimePoolObligationConcurrentIndexHooks` in `migrate_invalid_index_test.go`. For migrations 268, 270, and 271, create the named INVALID index fixture, run the registered pre-migration hook, and prove the invalid relation is removed while a valid relation is preserved. This RED must fail because the three hooks are initially absent.

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool ./internal/pooltestdb ./internal/migrations ./cmd/migrate -run 'Requirements|RuntimePoolContract|PlatformExtensionRelease|PoolTestDatabase|RuntimePoolObligationConcurrentIndexHooks' -count=1
```

Expected: FAIL with `undefined: ParseRequirements` and missing `runtime_binding_mode`/`placement_workspace_id` migration assertions.

- [ ] **Step 3: Implement the minimal contract.** Define exact constants and types below, add the strict CHECKs above, add `agent_runtime.capabilities TEXT[] NOT NULL DEFAULT '{}'`, create the obligation heap in 267, then add its ID uniqueness/PK, `(agent_id,comment_id)` uniqueness, and FIFO `(updated_at,id)` index through migrations 268-271 below.

```go
package runtimepool

import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "regexp"
    "sort"
)

type Requirements struct {
    SchemaVersion   string   `json:"schema_version"`
    CapabilitiesAll []string `json:"capabilities_all"`
}
const RequirementsSchemaV1 = "multica.runtime-requirements/v1"
const CapabilityExtensionExecuteV1 = "multica.extension.execute/v1"
const BindingFixed = "fixed"
const BindingPool = "pool"
const SessionAffinityUnresolved = "unresolved"
const SessionAffinityNone = "none"
const SessionAffinityPinned = "pinned"
const SessionAffinityRemoved = "removed"
const StatusWaitingRuntime = "waiting_runtime"
const MaxCapabilities = 32
const MaxCapabilityBytes = 128
const MaxCanonicalRequirementsBytes = 4096

var capabilityNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,127}$`)

func validateRequirements(value Requirements) error {
    if value.SchemaVersion != RequirementsSchemaV1 { return errors.New("unsupported requirements schema") }
    if len(value.CapabilitiesAll) == 0 || len(value.CapabilitiesAll) > MaxCapabilities {
        return errors.New("capabilities_all must contain 1..32 items")
    }
    if !sort.StringsAreSorted(value.CapabilitiesAll) { return errors.New("capabilities_all must be sorted") }
    for i, capability := range value.CapabilitiesAll {
        if len(capability) > MaxCapabilityBytes || !capabilityNameRE.MatchString(capability) {
            return errors.New("invalid capability")
        }
        if i > 0 && capability == value.CapabilitiesAll[i-1] { return errors.New("duplicate capability") }
    }
    canonical, err := json.Marshal(value)
    if err != nil { return err }
    if len(canonical) > MaxCanonicalRequirementsBytes { return errors.New("canonical requirements exceed 4096 bytes") }
    return nil
}

func ParseRequirements(raw json.RawMessage) (Requirements, error) {
    if err := rejectDuplicateObjectKeys(raw); err != nil { return Requirements{}, err }
    dec := json.NewDecoder(bytes.NewReader(raw))
    dec.DisallowUnknownFields()
    var value Requirements
    if err := dec.Decode(&value); err != nil { return Requirements{}, err }
    if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) { return Requirements{}, errors.New("trailing JSON value") }
    if err := validateRequirements(value); err != nil { return Requirements{}, err }
    return value, nil
}

func CanonicalRequirements(value Requirements) (json.RawMessage, error) {
    if err := validateRequirements(value); err != nil { return nil, err }
    return json.Marshal(value)
}
```

`rejectDuplicateObjectKeys` must scan decoded JSON tokens recursively, compare decoded object-key strings, and reject a repeated key before typed decoding. It must therefore reject both literal duplicates and escaped-equivalent spellings such as `"capabilities_all"` plus `"\u0063apabilities_all"`; raw substring matching is forbidden.

Create the one shared fail-fast integration DB helper below and use it from every new Pool DB test. It deliberately has no skip path and checks reachability before returning:

```go
package pooltestdb

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

func Open(t *testing.T) *pgxpool.Pool {
    t.Helper()
    dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
    if dsn == "" { t.Fatal("DATABASE_URL is required for Pool DB tests") }
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    pool, err := pgxpool.New(ctx, dsn)
    if err != nil { t.Fatalf("open Pool test database: %v", err) }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        t.Fatalf("reach Pool test database: %v", err)
    }
    t.Cleanup(pool.Close)
    return pool
}
```

`OpenWithSearchPath(t, schemas...)` uses the same required `DATABASE_URL`, parses it with `pgxpool.ParseConfig`, sets a sanitized `search_path` RuntimeParam, and then calls the same fail-fast open/ping path. Its subprocess tests prove both missing and unreachable URLs fail rather than skip; a live test proves the requested private schema is current. Pool migration tests must use these helpers instead of the legacy `cmd/migrate` helper that calls `t.Skip`.

`postgres_test.go` deletes `DATABASE_URL`, asserts `Open` fails through a subprocess test helper, and uses the configured URL to assert a one-second `SELECT 1` succeeds. This helper is the only permitted DB bootstrap for new tests in Tasks 1-14.

Add the preflight exactly once, immediately before the historical terminal-timestamp CHECK, and do not update historical rows. Then copy the complete column and CHECK body below into `267_runtime_pool_contract.up.sql`; do not replace any predicate with a looser one:

```sql
DO $runtime_pool_terminal_timestamp_preflight$
DECLARE terminal_without_completed bigint; nonterminal_with_completed bigint;
BEGIN
  SELECT count(*) FILTER (WHERE status IN ('completed','failed','cancelled') AND completed_at IS NULL),
         count(*) FILTER (WHERE status NOT IN ('completed','failed','cancelled') AND completed_at IS NOT NULL)
  INTO terminal_without_completed, nonterminal_with_completed
  FROM agent_task_queue;
  IF terminal_without_completed <> 0 OR nonterminal_with_completed <> 0 THEN
    RAISE EXCEPTION 'runtime_pool_terminal_timestamp_preflight terminal_without_completed=% nonterminal_with_completed=%',
      terminal_without_completed, nonterminal_with_completed USING ERRCODE='23514';
  END IF;
END $runtime_pool_terminal_timestamp_preflight$;

ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_terminal_completed_at_check
  CHECK ((status IN ('completed','failed','cancelled')) = (completed_at IS NOT NULL)) NOT VALID;
ALTER TABLE agent_task_queue VALIDATE CONSTRAINT agent_task_queue_terminal_completed_at_check;

ALTER TABLE agent ADD COLUMN runtime_binding_mode text NOT NULL DEFAULT 'fixed';
ALTER TABLE agent ADD COLUMN runtime_requirements jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE agent_runtime ADD COLUMN capabilities text[] NOT NULL DEFAULT '{}';
ALTER TABLE agent_task_queue ADD COLUMN runtime_binding_mode text NOT NULL DEFAULT 'fixed';
ALTER TABLE agent_task_queue ADD COLUMN runtime_requirements jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE agent_task_queue ADD COLUMN placement_workspace_id uuid;
ALTER TABLE agent_task_queue ADD COLUMN runtime_requester_user_id uuid;
ALTER TABLE agent_task_queue ADD COLUMN session_affinity_state text NOT NULL DEFAULT 'none';
ALTER TABLE agent_task_queue ADD COLUMN session_affinity_runtime_id uuid;
ALTER TABLE agent_task_queue ADD COLUMN explicit_fresh_session boolean NOT NULL DEFAULT false;
ALTER TABLE platform_extension_release ADD COLUMN runtime_binding_mode text NOT NULL DEFAULT 'fixed';
ALTER TABLE platform_extension_release ADD COLUMN runtime_requirements jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent DROP CONSTRAINT agent_runtime_mode_check;
ALTER TABLE agent ADD CONSTRAINT agent_runtime_binding_mode_check CHECK (
  (runtime_binding_mode='fixed' AND runtime_mode IN ('local','cloud')) OR
  (runtime_binding_mode='pool' AND runtime_id IS NULL AND runtime_mode='pool')
);
ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check CHECK (
  status IN ('waiting_runtime','queued','deferred','dispatched','running',
             'waiting_local_directory','completed','failed','cancelled')
);
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_affinity_pair_check CHECK (
  (session_affinity_state='unresolved' AND session_affinity_runtime_id IS NULL) OR
  (session_affinity_state='none' AND session_affinity_runtime_id IS NULL) OR
  (session_affinity_state='pinned' AND session_affinity_runtime_id IS NOT NULL) OR
  (session_affinity_state='removed' AND session_affinity_runtime_id IS NULL)
);
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_fixed_snapshot_check CHECK (
  runtime_binding_mode<>'fixed' OR
  (session_affinity_state='none' AND session_affinity_runtime_id IS NULL AND
   placement_workspace_id IS NULL AND runtime_requester_user_id IS NULL)
);
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_explicit_fresh_check CHECK (
  NOT explicit_fresh_session OR (runtime_binding_mode='pool' AND session_affinity_state='none')
);
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_unresolved_check CHECK (
  session_affinity_state<>'unresolved' OR
  (runtime_binding_mode='pool' AND chat_session_id IS NOT NULL AND
   status IN ('waiting_runtime','deferred') AND runtime_id IS NULL AND
   wait_reason IS NOT NULL AND wait_reason='chat_predecessor_pending')
);
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_removed_check CHECK (
  session_affinity_state<>'removed' OR
  (runtime_binding_mode='pool' AND status='cancelled' AND completed_at IS NOT NULL AND
   runtime_id IS NULL AND wait_reason IS NOT NULL AND wait_reason='session_runtime_removed')
);
ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_active_requires_runtime;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_routing_lifecycle_check CHECK (
  (runtime_binding_mode='fixed' AND status<>'waiting_runtime' AND
    ((status IN ('queued','deferred','dispatched','running','waiting_local_directory') AND runtime_id IS NOT NULL) OR
     status IN ('completed','failed','cancelled'))) OR
  (runtime_binding_mode='pool' AND placement_workspace_id IS NOT NULL AND runtime_requester_user_id IS NOT NULL AND
    ((status IN ('waiting_runtime','deferred') AND runtime_id IS NULL) OR
     (status IN ('queued','dispatched','running','waiting_local_directory') AND runtime_id IS NOT NULL) OR
     status IN ('completed','failed','cancelled')))
);
ALTER TABLE platform_extension_release DROP CONSTRAINT platform_extension_release_check;
ALTER TABLE platform_extension_release ADD CONSTRAINT platform_extension_release_runtime_binding_mode_check CHECK (
  runtime_binding_mode IN ('fixed','pool')
);
ALTER TABLE platform_extension_release ADD CONSTRAINT platform_extension_release_runtime_routing_check CHECK (
  (squad_id IS NULL AND runtime_id IS NULL) OR
  (runtime_binding_mode='fixed' AND squad_id IS NOT NULL AND runtime_id IS NOT NULL) OR
  (runtime_binding_mode='pool' AND squad_id IS NOT NULL AND runtime_id IS NULL)
);

CREATE TABLE agent_comment_followup_obligation (
  id uuid NOT NULL DEFAULT gen_random_uuid(), issue_id uuid NOT NULL, agent_id uuid NOT NULL,
  comment_id uuid NOT NULL, comment_updated_at timestamptz NOT NULL, head_sha text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
```

Migration 267 contains no index declaration, inline `PRIMARY KEY`, or inline `UNIQUE`. Add the indexes/constraint in separate one-statement migrations so every physical index build is concurrent:

```sql
-- 268 up (one statement)
CREATE UNIQUE INDEX CONCURRENTLY idx_agent_comment_followup_obligation_id
  ON agent_comment_followup_obligation(id);
-- 269 up (reuses 268; does not build an index)
ALTER TABLE agent_comment_followup_obligation
  ADD CONSTRAINT agent_comment_followup_obligation_pkey PRIMARY KEY USING INDEX idx_agent_comment_followup_obligation_id;
-- 270 up (one statement)
CREATE UNIQUE INDEX CONCURRENTLY idx_agent_comment_followup_obligation_agent_comment
  ON agent_comment_followup_obligation(agent_id,comment_id);
-- 271 up (one statement)
CREATE INDEX CONCURRENTLY idx_agent_comment_followup_obligation_fifo
  ON agent_comment_followup_obligation(updated_at ASC,id ASC);
```

Each matching down file also contains one statement: 271/270 use `DROP INDEX CONCURRENTLY`, 269 drops only the PK constraint, and 268 uses `DROP INDEX CONCURRENTLY IF EXISTS`. Migration 267 down first raises `runtime pool rows exist; rollback refused` if any Agent, Task, or Release is Pool, then drops the obligation table, both new Release checks, the remaining named checks, and columns in reverse order. It restores the migration-251 `agent_task_queue_active_requires_runtime` predicate, restores `platform_extension_release_check`, restores `agent_task_queue_status_check` without `waiting_runtime`, and restores `agent_runtime_mode_check` for `local|cloud`; it does not rerun the terminal preflight and drops `agent_task_queue_terminal_completed_at_check` last. `runtime_pool_contract_migration_test.go` asserts 267 has no index/inline-key syntax and 268-271 each have exactly one top-level statement with the required `CONCURRENTLY`/`USING INDEX` contract.

Register `cleanupInvalidConcurrentIndexHook` for migrations 268, 270, and 271 using their exact index names. The SQL remains fail-closed without `IF NOT EXISTS`; the hook only removes a verified INVALID index so an interrupted concurrent build can retry automatically.

```sql
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_active_requires_runtime
  CHECK (runtime_id IS NOT NULL OR completed_at IS NOT NULL) NOT VALID;
```

- [ ] **Step 4: Run GREEN after verifying the pinned sqlc and generating models.** Do not download tools in this task; if the pinned binary is absent or reports another version, stop with that explicit prerequisite failure.

```bash
test -x /Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc version
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool ./internal/pooltestdb ./internal/migrations ./pkg/db/generated ./cmd/migrate -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/runtimepool ./internal/pooltestdb ./internal/migrations ./pkg/db/generated ./cmd/migrate
```

Expected: sqlc reports `v1.31.1`; all tests and vet PASS.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/migrations/267_runtime_pool_contract.up.sql server/migrations/267_runtime_pool_contract.down.sql server/migrations/268_comment_followup_id_unique.up.sql server/migrations/268_comment_followup_id_unique.down.sql server/migrations/269_comment_followup_primary_key.up.sql server/migrations/269_comment_followup_primary_key.down.sql server/migrations/270_comment_followup_agent_comment_unique.up.sql server/migrations/270_comment_followup_agent_comment_unique.down.sql server/migrations/271_comment_followup_fifo_index.up.sql server/migrations/271_comment_followup_fifo_index.down.sql server/internal/runtimepool/requirements.go server/internal/runtimepool/requirements_test.go server/internal/pooltestdb/postgres.go server/internal/pooltestdb/postgres_test.go server/internal/migrations/migrations_lint_test.go server/internal/migrations/platform_extension_release_migration_test.go server/internal/migrations/runtime_pool_contract_migration_test.go server/cmd/migrate/main.go server/cmd/migrate/migrate_invalid_index_test.go server/pkg/db/generated
git commit -m "feat(server): persist runtime pool contracts"
```

---

### Task 2: Add rolling-safe workspace, pending, and occupancy indexes

**Files:**
- Create: `server/migrations/272_pending_issue_agent_pool_v3.up.sql`, `server/migrations/272_pending_issue_agent_pool_v3.down.sql`
- Create: `server/migrations/273_drop_pending_issue_agent_v2.up.sql`, `server/migrations/273_drop_pending_issue_agent_v2.down.sql`
- Create: `server/migrations/274_chat_pending_pool_v4.up.sql`, `server/migrations/274_chat_pending_pool_v4.down.sql`
- Create: `server/migrations/275_drop_chat_pending_v3.up.sql`, `server/migrations/275_drop_chat_pending_v3.down.sql`
- Create: `server/migrations/276_waiting_runtime_workspace_index.up.sql`, `server/migrations/276_waiting_runtime_workspace_index.down.sql`
- Create: `server/migrations/277_runtime_pool_occupancy_index.up.sql`, `server/migrations/277_runtime_pool_occupancy_index.down.sql`
- Create: `server/migrations/278_runtime_pool_deferred_due_index.up.sql`, `server/migrations/278_runtime_pool_deferred_due_index.down.sql`
- Create: `server/migrations/279_runtime_pool_rollback_guard.up.sql`, `server/migrations/279_runtime_pool_rollback_guard.down.sql`
- Create: `server/internal/migrations/runtime_pool_indexes_test.go`
- Modify: `server/internal/migrations/migrations_lint_test.go`
- Modify: `server/cmd/migrate/main.go`
- Modify: `server/cmd/migrate/migrate_invalid_index_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/service/duplicate_pending_task_test.go`

**Interfaces:**
- Consumes: Task 1 statuses and `placement_workspace_id`.
- Produces: issue unique v3, Chat non-unique v4, workspace waiting order, capacity-bearing lookup, per-Workspace Pool deferred due lookup, invalid concurrent-index recovery hooks, v3 duplicate-error compatibility, and down-first rollback refusal while Pool rows exist.

- [ ] **Step 1: Write RED index assertions.** Assert exact names/predicates and that each `CONCURRENTLY` migration contains one statement:

```sql
idx_one_pending_task_per_issue_agent_v3: existing v2 predicate OR status='waiting_runtime'
idx_agent_task_queue_chat_pending_v4: chat_session_id + priority DESC + created_at ASC + id ASC; six pending statuses
idx_agent_task_queue_waiting_runtime_workspace: placement_workspace_id + priority DESC + created_at ASC + id ASC WHERE status='waiting_runtime'
idx_agent_task_queue_runtime_capacity: runtime_id WHERE status IN ('queued','deferred','dispatched','running','waiting_local_directory')
idx_agent_task_queue_pool_deferred_due: placement_workspace_id + fire_at ASC + id ASC WHERE runtime_binding_mode='pool' AND status='deferred'
```

Copy this first RED into `runtime_pool_indexes_test.go`:

```go
package migrations

import (
    "os"
    "strings"
    "testing"
)

func TestRuntimePoolIndexDeferredIsWorkspaceBounded(t *testing.T) {
    raw, err := os.ReadFile("../../migrations/278_runtime_pool_deferred_due_index.up.sql")
    if err != nil { t.Fatal(err) }
    sql := string(raw)
    for _, fragment := range []string{"CREATE INDEX CONCURRENTLY", "placement_workspace_id", "fire_at ASC", "runtime_binding_mode = 'pool'", "status = 'deferred'"} {
        if !strings.Contains(sql, fragment) { t.Fatalf("migration missing %q\n%s", fragment, sql) }
    }
}
```

In the same file add DB-backed `TestRuntimePoolIndexesAgainstDatabase`: use `pooltestdb.Open(t)`, insert two Issue waiting rows to prove the v3 unique index rejects the second and names v3, insert two Chat waiting rows and assert `pg_index.indisunique = false`, assert every new index is `indisvalid`, recursively inspect `EXPLAIN (FORMAT JSON)` with `enable_seqscan=off` to prove the exact waiting, due-deferred, capacity, and six-status Chat queries use an eligible new index beneath any `Limit`/`Sort`/`LockRows`, and run the 279 down body under a savepoint for fixed-only, Pool Agent, Pool Task, and Pool Release cases. Static text assertions alone do not satisfy this task.

Add `TestRuntimePoolConcurrentIndexHooks` in `migrate_invalid_index_test.go`: create an INVALID index fixture for each of migrations 272, 274, 276, 277, and 278, invoke the registered pre-migration hook, and assert only the named INVALID index is removed while a valid index is preserved. Extend `duplicate_pending_task_test.go` so `isDuplicatePendingTaskErr` recognizes v1, v2, and v3 but rejects unrelated `23505` errors.

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/migrations ./cmd/migrate ./internal/service -run 'RuntimePoolIndex|RollbackGuard|RuntimePoolConcurrentIndexHooks|DuplicatePendingTask' -count=1
```

Expected: FAIL listing absent v3/v4/workspace/capacity/deferred indexes.

- [ ] **Step 3: Implement rolling migrations.** Build v3/v4 before dropping v2/v3. Migration 278 creates the single-statement concurrent deferred index. Make 279 up exactly `SELECT 1;`; make 279 down raise before earlier downs when any Agent, Task, or Release is Pool.

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_pool_deferred_due
    ON agent_task_queue (placement_workspace_id, fire_at ASC, id ASC)
    WHERE runtime_binding_mode = 'pool' AND status = 'deferred';

-- 279_runtime_pool_rollback_guard.down.sql
DO $$ BEGIN
IF EXISTS (SELECT 1 FROM agent WHERE runtime_binding_mode='pool')
OR EXISTS (SELECT 1 FROM agent_task_queue WHERE runtime_binding_mode='pool')
OR EXISTS (SELECT 1 FROM platform_extension_release WHERE runtime_binding_mode='pool')
THEN RAISE EXCEPTION 'runtime pool rows exist; rollback refused'; END IF;
END $$;
```

Register `cleanupInvalidConcurrentIndexHook` for 272, 274, 276, 277, and 278 before any later migration drops an old index. The 273/275 down migrations recreate the exact prior index without `IF NOT EXISTS`, so rollback fails closed instead of silently accepting an INVALID object. Add v3 to `isDuplicatePendingTaskErr` before migration 272 is enabled. Production rollout is explicitly two-phase: first deploy the v3-compatible binary everywhere with schema migration execution held, drain all older binaries, then apply 272-279 and enable Pool writers. Never apply 272 while a v2-only binary can still serve requests.

- [ ] **Step 4: Run GREEN and query-plan checks.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/migrations -run 'RuntimePoolIndex|RollbackGuard' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/migrations -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./cmd/migrate ./internal/service -run 'RuntimePoolConcurrentIndexHooks|DuplicatePendingTask' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/migrations ./cmd/migrate ./internal/service
```

Expected: tests prove uniqueness/non-uniqueness, workspace-bounded waiting/deferred planner eligibility, and 279 down refusal before index removal; all PASS.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/migrations/272_pending_issue_agent_pool_v3.up.sql server/migrations/272_pending_issue_agent_pool_v3.down.sql server/migrations/273_drop_pending_issue_agent_v2.up.sql server/migrations/273_drop_pending_issue_agent_v2.down.sql server/migrations/274_chat_pending_pool_v4.up.sql server/migrations/274_chat_pending_pool_v4.down.sql server/migrations/275_drop_chat_pending_v3.up.sql server/migrations/275_drop_chat_pending_v3.down.sql server/migrations/276_waiting_runtime_workspace_index.up.sql server/migrations/276_waiting_runtime_workspace_index.down.sql server/migrations/277_runtime_pool_occupancy_index.up.sql server/migrations/277_runtime_pool_occupancy_index.down.sql server/migrations/278_runtime_pool_deferred_due_index.up.sql server/migrations/278_runtime_pool_deferred_due_index.down.sql server/migrations/279_runtime_pool_rollback_guard.up.sql server/migrations/279_runtime_pool_rollback_guard.down.sql server/internal/migrations/runtime_pool_indexes_test.go server/internal/migrations/migrations_lint_test.go server/cmd/migrate/main.go server/cmd/migrate/migrate_invalid_index_test.go server/internal/service/task.go server/internal/service/duplicate_pending_task_test.go
git commit -m "feat(server): index runtime pool scheduling"
```

---

### Task 3: Define scheduler, authorization, event, and TaskService seams

**Files:**
- Create: `server/internal/runtimeaccess/policy.go`, `server/internal/runtimeaccess/policy_test.go`
- Create: `server/internal/runtimepool/contracts.go`
- Create: `server/internal/service/runtime_pool_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/pkg/protocol/events.go`

**Interfaces:**
- Consumes: Task 1 requirements and generated DB models.
- Produces: `runtimeaccess.CanUse(member db.Member, runtime db.AgentRuntime) bool`, `runtimepool.LivenessReader`, `runtimepool.AssignRequest/AssignResult`, `service.RuntimePoolScheduler`, `TaskService.AssignPoolWorkspace`, `TaskService.WakePoolWorkspace`, `TaskService.SweepRuntimePool`, and `protocol.EventTaskWaitingRuntime`.

- [ ] **Step 1: Write RED seam tests.** Use a fake scheduler to assert assignment results broadcast queued tasks once, waiting focus broadcasts `task:waiting_runtime` once, nil scheduler fails closed, and authorization exactly permits owner/admin, public, or private owner.

```go
package runtimeaccess

import (
    "testing"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimeAccessCanUseRuntime(t *testing.T) {
    owner := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
    caller := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
    workspace := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
    member := db.Member{WorkspaceID: workspace, UserID: caller, Role: "member"}
    runtime := db.AgentRuntime{WorkspaceID: workspace, OwnerID: owner, Visibility: "private"}
    if CanUse(member, runtime) { t.Fatal("private runtime accepted for a non-owner member") }
    runtime.Visibility = "public"
    if !CanUse(member, runtime) { t.Fatal("public runtime rejected for a workspace member") }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimeaccess ./internal/service ./pkg/protocol -run 'RuntimeAccess|RuntimePoolSeam|WaitingRuntimeEvent' -count=1
```

Expected: FAIL with undefined policy, scheduler field, and event constant.

- [ ] **Step 3: Implement the seams.** Use these bounded contracts; event publication remains in TaskService after allocator commit, never inside SQL transactions.

```go
// server/internal/runtimeaccess/policy.go
package runtimeaccess

import db "github.com/multica-ai/multica/server/pkg/db/generated"

func CanUse(member db.Member, runtime db.AgentRuntime) bool {
    if !member.UserID.Valid || member.WorkspaceID != runtime.WorkspaceID { return false }
    if member.Role == "owner" || member.Role == "admin" { return true }
    if runtime.OwnerID.Valid && member.UserID == runtime.OwnerID { return true }
    return runtime.Visibility == "public"
}

// server/internal/runtimepool/contracts.go
package runtimepool

import (
    "context"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const WaitingTaskScanLimit = 64
const RuntimeScanLimit = 128
const AssignmentBatchLimit = 8
type AssignRequest struct { WorkspaceID, FocusTaskID pgtype.UUID; Limit int }
type AssignResult struct { Assigned []db.AgentTaskQueue; PromotedWaiting []db.AgentTaskQueue }
type LivenessReader interface { Available() bool; IsAliveBatch(context.Context, []string) (map[string]bool, bool) }
```

Append this exact seam to `internal/service/task.go` and add the shown field to the existing `TaskService` struct:

```go
type RuntimePoolScheduler interface {
    AssignWaiting(context.Context, runtimepool.AssignRequest) (runtimepool.AssignResult, error)
    SweepWaiting(context.Context, int) ([]runtimepool.AssignResult, error)
}

// inside type TaskService struct
RuntimePool RuntimePoolScheduler
```

- [ ] **Step 4: Run GREEN.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimeaccess ./internal/service ./pkg/protocol -run 'RuntimeAccess|RuntimePoolSeam|WaitingRuntimeEvent' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/runtimeaccess ./internal/service ./pkg/protocol
```

Expected: PASS; fake scheduler verifies event order and nil fail-closed behavior.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/runtimeaccess/policy.go server/internal/runtimeaccess/policy_test.go server/internal/runtimepool/contracts.go server/internal/service/runtime_pool_test.go server/internal/service/task.go server/pkg/protocol/events.go
git commit -m "feat(server): define runtime pool seams"
```

---

### Task 4: Register presence-aware capabilities and enforce safe capability changes

**Files:**
- Create: `server/internal/runtimepool/capabilities.go`, `server/internal/runtimepool/capabilities_test.go`
- Create: `server/pkg/protocol/runtime_registration.go`, `server/pkg/protocol/runtime_registration_test.go`
- Modify: `server/internal/daemon/daemon.go`, `server/internal/daemon/types.go`, `server/internal/daemon/daemon_test.go`
- Modify: `server/internal/handler/daemon.go`, `server/internal/handler/daemon_test.go`
- Modify: `server/internal/handler/runtime.go`, `server/internal/handler/runtime_test.go`
- Modify: `server/internal/handler/runtime_update.go`, `server/internal/handler/runtime_update_test.go`
- Modify: `server/pkg/db/queries/runtime.sql`
- Modify by sqlc generation: `server/pkg/db/generated/runtime.sql.go`, `server/pkg/db/generated/models.go`

**Interfaces:**
- Consumes: Task 3 authorization/wake seam and Task 1 capability constants.
- Produces: typed `protocol.RuntimeRegistration` with `Capabilities *[]string`, omitted-versus-empty semantics, capability downgrade CAS/rejection, and post-commit capability-addition wake.

- [ ] **Step 1: Write RED registration and downgrade tests.** Cover legacy Platform omission derives one capability; explicit `[]` remains empty; other omitted Provider remains empty; known built-ins/custom profiles send an explicit list; failed-profile registration preserves stored capabilities. Cover queued Pool dependents requeued, pinned wait reason, in-flight downgrade returning `409 RUNTIME_CAPABILITY_IN_USE`, and addition waking once.

```go
package protocol

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestRuntimeRegistrationCapabilitiesPresence(t *testing.T) {
    empty := []string{}
    omitted, err := json.Marshal(RuntimeRegistration{Name: "legacy", Type: "platform-agent-cli"})
    if err != nil { t.Fatal(err) }
    explicit, err := json.Marshal(RuntimeRegistration{Name: "new", Type: "platform-agent-cli", Capabilities: &empty})
    if err != nil { t.Fatal(err) }
    if strings.Contains(string(omitted), `"capabilities"`) { t.Fatalf("omitted payload=%s", omitted) }
    if !strings.Contains(string(explicit), `"capabilities":[]`) { t.Fatalf("explicit payload=%s", explicit) }
}
```

Copy this normalization RED into `internal/runtimepool/capabilities_test.go`; extend `cases` one invalid boundary at a time (`33` unique items, `129` bytes, uppercase/space/non-ASCII):

```go
package runtimepool

import (
    "reflect"
    "testing"
)

func TestNormalizeAdvertisedCapabilitiesSortsAndDeduplicates(t *testing.T) {
    got, err := NormalizeAdvertisedCapabilities([]string{"z/v1", "a/v1", "a/v1"})
    if err != nil { t.Fatal(err) }
    want := []string{"a/v1", "z/v1"}
    if !reflect.DeepEqual(got, want) { t.Fatalf("got %v, want %v", got, want) }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool ./pkg/protocol ./internal/daemon ./internal/handler -run 'RuntimeRegistrationCapabilities|NormalizeAdvertisedCapabilities|CapabilityDowngrade|CapabilityAdditionWake' -count=1
```

Expected: FAIL because map payloads cannot encode a capability array/presence and registration overwrites without dependency checks.

- [ ] **Step 3: Implement typed generation and transaction rules.** Platform built-in/profile advertises `multica.extension.execute/v1`; upgraded unsupported built-ins/profiles advertise explicit empty; legacy omission is derived only server-side for exact Platform Provider. Normalize every present array using the same 128-byte grammar and 32-unique-item bound before storage. Built-in registration and Runtime visibility mutation lock Runtime first. The sole pre-Runtime lock exception is custom or failed-profile registration, which must use Profile `KEY SHARE` -> Runtime -> Agent -> Task so it stays compatible with profile deletion's Profile `UPDATE` -> Runtime order. In either path, only after Runtime is locked may the transaction run `ListPoolCapabilityDependentIDs` without `FOR UPDATE`, derive distinct Agent/Task UUIDs from that post-Runtime-lock snapshot, lock Agents in UUID order, and finally re-read/lock those exact Tasks in UUID order. This closes the old-ID window while preserving Runtime -> Agent -> Task after the optional Profile lock; never discover an Agent while holding a Task lock and never take Task -> Agent. Diff capabilities, requeue queued dependents, preserve pinned affinity, reject dispatched/running/waiting-local dependents, and commit before wake. Capability/visibility/owner expansion wakes the Workspace after commit. Runtime visibility/owner tightening in `handler/runtime.go` and `runtime_update.go` follows the Runtime-lock-first snapshot and Runtime -> Agent -> Task order: queued Tasks are requeued; any newly unauthorized in-flight Task returns `409 RUNTIME_ACCESS_IN_USE` and rolls the whole Runtime change back. Both the normal runtimes loop and the `failed_profiles` loop immediately return the stable capability/access in-use 409; other failed-profile recording errors remain best-effort. Member revoke remains a separate safety path in Task 7: it cancels all newly unauthorized nonterminal Pool Tasks and never returns 409. Claim defense is completed in Task 6.

Start with this copyable wire type in `pkg/protocol/runtime_registration.go`, then add the presence boundary in `internal/handler/daemon.go` before the SQL transaction:

```go
package protocol

type RuntimeRegistration struct {
    Name string `json:"name"`
    Type string `json:"type"`
    Version string `json:"version"`
    Status string `json:"status"`
    ProfileID string `json:"profile_id,omitempty"`
    Capabilities *[]string `json:"capabilities,omitempty"`
}
```

```go
package runtimepool

import (
    "errors"
    "sort"
)

func NormalizeAdvertisedCapabilities(advertised []string) ([]string, error) {
    normalized := append([]string(nil), advertised...)
    sort.Strings(normalized)
    out := normalized[:0]
    for _, capability := range normalized {
        if len(capability) > MaxCapabilityBytes || !capabilityNameRE.MatchString(capability) {
            return nil, errors.New("invalid runtime capability")
        }
        if len(out) == 0 || capability != out[len(out)-1] { out = append(out, capability) }
    }
    if len(out) > MaxCapabilities { return nil, errors.New("runtime capabilities exceed 32 unique items") }
    return out, nil
}

func ContainsAllCapabilities(have, required []string) bool {
    set := make(map[string]struct{}, len(have))
    for _, capability := range have { set[capability] = struct{}{} }
    for _, capability := range required {
        if _, ok := set[capability]; !ok { return false }
    }
    return true
}
```

```go
func effectiveRegisteredCapabilities(provider string, advertised *[]string) ([]string, error) {
    if advertised != nil { return runtimepool.NormalizeAdvertisedCapabilities(*advertised) }
    if provider == "platform-agent-cli" {
        return []string{runtimepool.CapabilityExtensionExecuteV1}, nil
    }
    return []string{}, nil
}
```

```sql
-- name: LockRuntimeForCapabilityRegistration :one
SELECT * FROM agent_runtime WHERE id=sqlc.arg(runtime_id)::uuid FOR UPDATE;
-- name: ListPoolCapabilityDependentIDs :many
SELECT id AS task_id, agent_id FROM agent_task_queue
WHERE runtime_binding_mode='pool'
  AND status IN ('waiting_runtime','queued','dispatched','running','waiting_local_directory','deferred')
  AND (runtime_id=sqlc.arg(runtime_id)::uuid OR session_affinity_runtime_id=sqlc.arg(runtime_id)::uuid)
ORDER BY id;
-- name: LockPoolCapabilityDependentAgents :many
SELECT * FROM agent
WHERE id = ANY(sqlc.arg(agent_ids)::uuid[])
ORDER BY id FOR UPDATE;
-- name: LockPoolCapabilityDependents :many
SELECT * FROM agent_task_queue
WHERE id = ANY(sqlc.arg(task_ids)::uuid[])
ORDER BY id FOR UPDATE;
-- name: RequeuePoolTaskAfterCapabilityDowngrade :one
UPDATE agent_task_queue SET status='waiting_runtime',runtime_id=NULL,wait_reason=sqlc.arg(reason)::text
WHERE id=sqlc.arg(task_id)::uuid AND status='queued' AND runtime_binding_mode='pool' RETURNING *;
```

- [ ] **Step 4: Run GREEN after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool ./pkg/protocol ./internal/daemon ./internal/handler -run 'RuntimeRegistrationCapabilities|NormalizeAdvertisedCapabilities|CapabilityDowngrade|CapabilityAdditionWake' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/runtimepool ./pkg/protocol ./internal/daemon ./internal/handler
```

Expected: PASS, including no heartbeat extension on rejected downgrade.

Execute the cases as individual loops before the combined GREEN: `TestRuntimeRegistrationCapabilitiesPresence`, `TestNormalizeAdvertisedCapabilitiesSortsAndDeduplicates`, `TestCapabilityDowngradeRequeuesQueued`, `TestCapabilityDowngradeRejectsInFlight`, `TestRuntimeAccessTighteningRequeuesQueued`, and `TestRuntimeAccessTighteningRejectsInFlight`, each with `go test <owning packages> -run '^<name>$' -count=1`. The last two deliberately prove the 409 branch is not reused for Member revoke.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/runtimepool/capabilities.go server/internal/runtimepool/capabilities_test.go server/pkg/protocol/runtime_registration.go server/pkg/protocol/runtime_registration_test.go server/internal/daemon/daemon.go server/internal/daemon/types.go server/internal/daemon/daemon_test.go server/internal/handler/daemon.go server/internal/handler/daemon_test.go server/internal/handler/runtime.go server/internal/handler/runtime_test.go server/internal/handler/runtime_update.go server/internal/handler/runtime_update_test.go server/pkg/db/queries/runtime.sql server/pkg/db/generated/runtime.sql.go server/pkg/db/generated/models.go
git commit -m "feat(server): register runtime capabilities"
```

---

### Task 5: Implement the bounded atomic allocator

**Files:**
- Create: `server/internal/runtimepool/scheduler.go`, `server/internal/runtimepool/scheduler_test.go`
- Modify: `server/pkg/db/queries/agent.sql`, `server/pkg/db/queries/chat.sql`, `server/pkg/db/queries/runtime.sql`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`, `server/pkg/db/generated/chat.sql.go`, `server/pkg/db/generated/runtime.sql.go`

**Interfaces:**
- Consumes: Task 2 indexes, Task 3 scheduler/liveness/access contracts, Task 4 capabilities.
- Produces: `runtimepool.NewScheduler(q, tx, liveness) *Scheduler`, workspace-bounded `AssignWaiting`, strict-idle first placement, pinned placement, and exact assignment CAS.

**SQL contracts:**

```text
ListWaitingPoolTasks(placement_workspace_id uuid, scan_limit int32) -> []AgentTaskQueue
ListPoolRuntimeCandidates(workspace_id uuid, requester_user_id uuid, requirements_all text[], runtime_limit int32) -> []db.ListPoolRuntimeCandidatesRow generated here as `{AgentRuntime db.AgentRuntime; FixedBindingCount int64}`
GetPinnedPoolRuntimeCandidate(workspace_id uuid, runtime_id uuid, requester_user_id uuid, requirements_all text[]) -> db.AgentRuntime; does not apply idle filtering
LockPoolPlacementMember(workspace_id uuid, requester_user_id uuid) -> db.Member
LockPoolRuntimeForPlacement(runtime_id uuid) -> db.AgentRuntime
LockPoolChatSessionForPlacement(chat_session_id uuid) -> db.ChatSession
LockPoolAgentForPlacement(agent_id uuid) -> db.Agent
IsPoolChatExecutionHead(chat_session_id uuid, task_id uuid) -> bool
CountRuntimeCapacityBearingTasks(runtime_id uuid) -> int64
AssignWaitingPoolTask(task_id uuid, runtime_id uuid, placement_workspace_id uuid) -> AgentTaskQueue
ListRuntimePoolSweepWorkspaces(after_workspace_id uuid, workspace_limit int32) -> []uuid from workspace PK order
PromoteDuePoolDeferredTasksForWorkspace(placement_workspace_id uuid, now timestamptz, promote_limit int32) -> []AgentTaskQueue
```

The task/runtime list queries always include their workspace equality and `LIMIT sqlc.arg(..._limit)`; the Task query orders `priority DESC, created_at ASC, id ASC`. The fresh Runtime query performs authorization, capability, and capacity-bearing `NOT EXISTS` filtering plus the complete owner-local, `last_seen_at DESC`, fixed-binding-count, `created_at`, `id` order before `LIMIT`; the pinned query is separate and deliberately does not apply idle filtering. Redis liveness then filters that bounded order without re-sorting; when Redis is unavailable/Noop it accepts only rows whose DB heartbeat is within 150 seconds. The periodic cursor reads at most 32 Workspace primary keys, then promotes at most 64 due Pool deferred rows inside each selected Workspace using the Task 2 deferred index. Lock queries are invoked in Member -> Runtime -> optional ChatSession -> Agent -> Task order and return `sql.ErrNoRows` on a raced deletion/revocation.

- [ ] **Step 1: Write RED allocator tests.** Cover owner-local before shared; busy local selects shared; private/cross-workspace/capability/offline/stale exclusion; Redis alive and 150-second DB fallback; pinned busy queues original Runtime; pinned unavailable waits with stable reason; unresolved Chat tail excluded; two allocators cannot double-assign; Member revoke race fails closed; fixed enqueue can follow a committed Pool assignment.

```text
ListWaitingPoolTasks(workspace_id, limit=64): priority DESC, created_at ASC, id ASC; excludes unresolved
ListPoolRuntimeCandidates(workspace_id, limit=128): returns only workspace rows; no Provider predicate
assignment transaction: Member -> Runtime -> optional ChatSession -> Agent -> Task; lock-time revalidation; LIMIT 8 assignments
```

Copy this first RED into `scheduler_test.go`; add the larger allocator matrix as adjacent table-driven tests:

```go
package runtimepool

import (
    "os"
    "strings"
    "testing"
)

func TestSchedulerCandidateOrderPrecedesLimit(t *testing.T) {
    raw, err := os.ReadFile("../../pkg/db/queries/runtime.sql")
    if err != nil { t.Fatal(err) }
    start := strings.Index(string(raw), "-- name: ListPoolRuntimeCandidates")
    if start < 0 { t.Fatal("ListPoolRuntimeCandidates query missing") }
    query := string(raw[start:])
    order, limit := strings.Index(query, "ORDER BY"), strings.Index(query, "LIMIT sqlc.arg(runtime_limit)")
    if order < 0 || limit <= order { t.Fatalf("ORDER/LIMIT positions %d/%d", order, limit) }
    for _, fragment := range []string{"ar.runtime_mode='local'", "ar.last_seen_at DESC", "fixed_binding_count ASC", "ar.created_at ASC", "ar.id ASC"} {
        if !strings.Contains(query[order:limit], fragment) { t.Fatalf("candidate order missing %q", fragment) }
    }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool -run 'Scheduler|WorkspaceBounded|ConcurrentAssignment|Pinned' -count=1
```

Expected: FAIL with missing `NewScheduler`/queries; concurrency test observes no assignment implementation.

- [ ] **Step 3: Implement the minimal allocator.** Batch Redis liveness outside transactions. In the short transaction lock/revalidate Member, Runtime, optional Chat head, Agent, then Task using `SKIP LOCKED`; for `none` require zero capacity-bearing rows, for `pinned` skip busy check; update only `id + waiting_runtime + runtime_id IS NULL + pool`. Use `NewScheduler(q, tx, liveness)` and `AssignWaiting(ctx, AssignRequest)` as the only production entry; one call scans one Workspace and commits no more than eight assignments.

Start `scheduler.go` with this constructor and order-preserving liveness filter, then add one lock/revalidation action per edit. PostgreSQL is the sole ranker; Go must never re-sort the result:

```go
package runtimepool

import (
    "context"
    "sync"
    "time"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"
    "github.com/multica-ai/multica/server/internal/util"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type TxStarter interface { Begin(context.Context) (pgx.Tx, error) }
type Scheduler struct {
    q *db.Queries
    tx TxStarter
    liveness LivenessReader
    sweepMu sync.Mutex
    sweepCursor pgtype.UUID
}

func NewScheduler(q *db.Queries, tx TxStarter, liveness LivenessReader) *Scheduler {
    return &Scheduler{q: q, tx: tx, liveness: liveness}
}

func filterAliveInOrder(in []db.ListPoolRuntimeCandidatesRow, alive map[string]bool, authoritative bool, now time.Time) []db.ListPoolRuntimeCandidatesRow {
    out := make([]db.ListPoolRuntimeCandidatesRow, 0, len(in))
    for _, candidate := range in {
        isAlive := alive[util.UUIDToString(candidate.AgentRuntime.ID)]
        if !authoritative {
            isAlive = candidate.AgentRuntime.LastSeenAt.Valid && !candidate.AgentRuntime.LastSeenAt.Time.Before(now.Add(-150*time.Second))
        }
        if isAlive { out = append(out, candidate) }
    }
    return out
}
```

`LivenessReader.IsAliveBatch` is called before `Begin`. Its boolean is `authoritative`: Redis success returns `true` even when the map is empty; Redis error and Noop return `false`, which selects the 150-second DB-heartbeat branch above. Fetch and filter liveness completely outside the placement transaction and assert with a query-spy that no liveness call occurs after the first row lock.

```sql
-- name: ListWaitingPoolTasks :many
SELECT * FROM agent_task_queue
WHERE placement_workspace_id=sqlc.arg(placement_workspace_id)::uuid
  AND runtime_binding_mode='pool' AND status='waiting_runtime'
  AND session_affinity_state IN ('none','pinned')
ORDER BY priority DESC,created_at ASC,id ASC
LIMIT sqlc.arg(scan_limit);

-- name: ListPoolRuntimeCandidates :many
SELECT sqlc.embed(ar), count(fixed_agent.id)::bigint AS fixed_binding_count
FROM agent_runtime ar
JOIN member m ON m.workspace_id=ar.workspace_id AND m.user_id=sqlc.arg(requester_user_id)::uuid
LEFT JOIN agent fixed_agent ON fixed_agent.runtime_id=ar.id
  AND fixed_agent.runtime_binding_mode='fixed' AND fixed_agent.archived_at IS NULL
WHERE ar.workspace_id=sqlc.arg(workspace_id)::uuid AND ar.status='online'
  AND ar.capabilities @> sqlc.arg(requirements_all)::text[]
  AND (m.role IN ('owner','admin') OR ar.owner_id=sqlc.arg(requester_user_id)::uuid OR ar.visibility='public')
  AND NOT EXISTS (
    SELECT 1 FROM agent_task_queue occupied
    WHERE occupied.runtime_id=ar.id
      AND occupied.status IN ('queued','deferred','dispatched','running','waiting_local_directory')
  )
GROUP BY ar.id
ORDER BY
  CASE WHEN ar.runtime_mode='local' AND ar.owner_id=sqlc.arg(requester_user_id)::uuid THEN 0 ELSE 1 END,
  ar.last_seen_at DESC,
  fixed_binding_count ASC,
  ar.created_at ASC,ar.id ASC
LIMIT sqlc.arg(runtime_limit);

-- name: LockPoolAgentForPlacement :one
SELECT * FROM agent WHERE id=sqlc.arg(agent_id)::uuid FOR UPDATE;

-- name: ListRuntimePoolSweepWorkspaces :many
SELECT id FROM workspace
WHERE sqlc.narg(after_workspace_id)::uuid IS NULL
   OR id > sqlc.narg(after_workspace_id)::uuid
ORDER BY id ASC LIMIT sqlc.arg(workspace_limit);

-- name: PromoteDuePoolDeferredTasksForWorkspace :many
UPDATE agent_task_queue
SET status='waiting_runtime',
    wait_reason=CASE WHEN session_affinity_state='unresolved'
      THEN 'chat_predecessor_pending' ELSE 'no_eligible_runtime' END
WHERE id IN (
  SELECT id FROM agent_task_queue
  WHERE placement_workspace_id=sqlc.arg(placement_workspace_id)::uuid
    AND runtime_binding_mode='pool' AND status='deferred' AND fire_at <= sqlc.arg(now)::timestamptz
    AND session_affinity_state<>'unresolved'
  ORDER BY fire_at ASC,id ASC LIMIT sqlc.arg(promote_limit)
  FOR UPDATE SKIP LOCKED
) RETURNING *;

-- name: AssignWaitingPoolTask :one
WITH candidate AS (
  SELECT id FROM agent_task_queue
  WHERE id=sqlc.arg(task_id)::uuid AND status='waiting_runtime' AND runtime_id IS NULL
    AND runtime_binding_mode='pool' AND placement_workspace_id=sqlc.arg(placement_workspace_id)::uuid
    AND session_affinity_state IN ('none','pinned')
  FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue task
SET runtime_id=sqlc.arg(runtime_id)::uuid, status='queued', wait_reason=NULL
FROM candidate
WHERE task.id=candidate.id
RETURNING task.*;
```

Run these cases as distinct loops before the combined command: (1) `TestSchedulerCandidateOrderPrecedesLimit` proves the busy `NOT EXISTS` and every rank key occur before `LIMIT`; (2) `TestSchedulerLivenessRedisAndFallback` proves Redis success, Redis empty, Redis error, and Noop/150-second fallback outside locks; (3) `TestSchedulerDoesNotPromoteUnresolvedChatTail` proves a due unresolved row stays `deferred + chat_predecessor_pending`; (4) `TestSchedulerConcurrentAssignmentCAS` proves the final `SKIP LOCKED` CTE assigns once; and (5) `TestSchedulerPinnedBusyRuntime` proves pinned placement ignores occupancy. Run each as `go test ./internal/runtimepool -run '^<name>$' -count=1`. `AssignWaiting` returns all committed assignments plus `PromotedWaiting` rows that were promoted but remained unassigned; Task 7 publishes both results after commit.

- [ ] **Step 4: Run GREEN and race tests after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/runtimepool -run 'Scheduler|WorkspaceBounded|ConcurrentAssignment|Pinned' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -race ./internal/runtimepool -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/runtimepool
```

Expected: PASS; query counter proves no global waiting scan and no Redis under lock.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/runtimepool/scheduler.go server/internal/runtimepool/scheduler_test.go server/pkg/db/queries/agent.sql server/pkg/db/queries/chat.sql server/pkg/db/queries/runtime.sql server/pkg/db/generated/agent.sql.go server/pkg/db/generated/chat.sql.go server/pkg/db/generated/runtime.sql.go
git commit -m "feat(server): allocate pool runtimes atomically"
```

---

### Task 6: Make singular and batch claim Runtime-targeted

**Files:**
- Create: `server/internal/service/task_runtime_targeted_claim_test.go`
- Modify: `server/internal/service/task.go`, `server/internal/service/task_batch_claim_test.go`
- Modify: `server/internal/handler/daemon_batch_claim_test.go`, `server/internal/handler/daemon_test.go`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`

**Interfaces:**
- Consumes: assigned Pool tasks from Task 5 and runtime access/capability checks.
- Produces: SQL `ClaimAgentTaskForRuntime(agent_id, runtime_id, prepare_lease_secs)` and `ReclaimStaleDispatchedTaskForAgentRuntime(agent_id, runtime_id, ...)`, whose inner queries select the one global eligible head for the Agent before the outer Runtime CAS; service helpers `claimTaskForAgentRuntime` and `reclaimStaleTaskForAgentRuntime` are used by both singular and batch entrypoints. Batch candidate iteration deduplicates only by `agent_id`.

- [ ] **Step 1: Write RED claim races.** Create one Pool Agent with queued tasks on Runtime A and B and put the global higher-priority/FIFO head on B. An A claim must return nil rather than skipping to its lower A task; B claims the head, and A becomes claimable only on a later attempt allowed by existing Agent capacity/serialization. Assert no task is dropped, batch dedupe remains `agent_id`, and max concurrency plus Issue/Chat/Quick serialization span both Runtimes. Run the same cases through stale-dispatch reclaim and capability/permission downgrade races.

```go
package service

import (
    "os"
    "strings"
    "testing"
)

func TestRuntimeTargetedClaimFiltersOnlyOuterCAS(t *testing.T) {
    raw, err := os.ReadFile("../../pkg/db/queries/agent.sql")
    if err != nil { t.Fatal(err) }
    sql := string(raw)
    start := strings.Index(sql, "-- name: ClaimAgentTaskForRuntime")
    if start < 0 { t.Fatal("ClaimAgentTaskForRuntime query missing") }
    query := sql[start:]
    if next := strings.Index(query[1:], "\n-- name:"); next >= 0 { query = query[:next+1] }
    if strings.Count(query, "runtime_id = sqlc.arg(runtime_id)") != 1 {
        t.Fatal("runtime_id must appear once in the outer UPDATE CAS, never in the global-head subquery")
    }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'RuntimeTargetedClaim|BatchClaimPool|ClaimSerializationAcrossRuntimes' -count=1
```

Expected: FAIL because the Runtime-targeted query is absent; the current Agent-only claim can dispatch B during A's claim and the Go guard discards it.

- [ ] **Step 3: Implement the Runtime-targeted CAS.** Refactor, do not replace, the current `ClaimTask` transaction and post-commit lifecycle. Copy every existing eligibility predicate/order into a global-head CTE filtered by `agent_id` but not Runtime. The outer UPDATE alone requires the requested `runtime_id`; a wrong Runtime gets no row and cannot skip to a lower task. Singular and batch callers use the helper, and batch continues to dedupe by `agent_id`, never `(runtime_id, agent_id)`. Pool fresh claim and stale-dispatch reclaim each run in one short transaction: unlocked candidate preview; then Member -> Runtime -> optional ChatSession -> Agent; lock and re-read the current global head Task; re-read workspace/access/status/heartbeat/capability/Chat head, routing snapshots and Agent capacity; finally dispatch/reclaim that exact locked row with a CAS over `id + agent + runtime + status + placement/requester/requirements/affinity snapshots`. Never authorize from the preview `candidate`. Invalid queued Pool tasks atomically return to `waiting_runtime`: `none` uses `no_eligible_runtime`, while `pinned` uses the matching `session_runtime_*` reason. Invalid stale dispatched tasks are not re-delivered and take the existing recovery/cancel path. Fixed tasks retain Agent -> Task locking and use the outer Runtime CAS without Pool snapshots. The refactor must preserve claim-time direct-chat reanchor inside the transaction and the existing post-commit `captureTaskDispatched`, Agent-status reconcile, dispatch event, slow metrics, EmptyClaim and finalize/token behavior.

After copying the existing SQL predicate verbatim, introduce these service seams and replace singular, batch, fresh, and stale call sites one at a time. Preview queries only yield `(agent_id,runtime_id)` attempts; no preview Task is passed into the correctness helper. `GetGlobalEligibleAgentHeadSnapshot` is run inside the transaction before locks to discover the required lock keys, and again after `Member -> Runtime -> optional ChatSession -> Agent`; if the Task ID, placement/requester/requirements/affinity snapshot, Chat ID, or Runtime assignment differs, return no claim and let the bounded caller retry. The final Runtime-targeted SQL locks/CASes that current global head Task. Fresh and stale share the same lock/revalidation helper:

```go
type runtimeClaimKind uint8
const (
    freshRuntimeClaim runtimeClaimKind = iota
    staleRuntimeReclaim
)

func (s *TaskService) claimTaskForAgentRuntime(ctx context.Context, agentID, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
    return s.claimOrReclaimTaskForAgentRuntime(ctx, agentID, runtimeID, freshRuntimeClaim)
}

func (s *TaskService) reclaimStaleTaskForAgentRuntime(ctx context.Context, agentID, runtimeID pgtype.UUID) (*db.AgentTaskQueue, error) {
    return s.claimOrReclaimTaskForAgentRuntime(ctx, agentID, runtimeID, staleRuntimeReclaim)
}

func (s *TaskService) claimOrReclaimTaskForAgentRuntime(
    ctx context.Context, agentID, runtimeID pgtype.UUID, kind runtimeClaimKind,
) (*db.AgentTaskQueue, error) {
    var claimed *db.AgentTaskQueue
    err := s.runInTx(ctx, func(qtx *db.Queries) error {
        before, err := currentGlobalEligibleHead(ctx, qtx, agentID, kind)
        if errors.Is(err, pgx.ErrNoRows) { return nil }
        if err != nil { return err }
        if before.RuntimeBindingMode == runtimepool.BindingPool {
            member, err := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{
                WorkspaceID: before.PlacementWorkspaceID, RequesterUserID: before.RuntimeRequesterUserID,
            })
            if err != nil { return err }
            runtime, err := qtx.LockPoolRuntimeForClaim(ctx, runtimeID)
            if err != nil { return err }
            if before.ChatSessionID.Valid {
                if _, err := qtx.LockPoolChatSessionForClaim(ctx, before.ChatSessionID); err != nil { return err }
                head, err := qtx.IsPoolChatExecutionHead(ctx, db.IsPoolChatExecutionHeadParams{ChatSessionID: before.ChatSessionID, TaskID: before.ID})
                if err != nil { return err }
                if !head { return nil }
            }
            agent, err := qtx.GetAgentForClaimUpdate(ctx, agentID)
            if err != nil { return err }
            current, err := currentGlobalEligibleHead(ctx, qtx, agentID, kind)
            if errors.Is(err, pgx.ErrNoRows) { return nil }
            if err != nil { return err }
            if !sameRoutingSnapshot(before, current) { return nil }
            if reason := validatePoolClaimSnapshot(member, runtime, agent, current); reason != "" {
                if kind == staleRuntimeReclaim {
                    return s.recoverStaleDispatchedTaskInTx(ctx, qtx, current, reason)
                }
                _, err = qtx.RequeuePoolTaskAfterClaimRevalidation(ctx, db.RequeuePoolTaskAfterClaimRevalidationParams{ID: current.ID, RuntimeID: runtimeID, WaitReason: pgtype.Text{String: reason, Valid: true}})
                return err
            }
            running, err := countClaimCapacity(ctx, qtx, agentID, current.ID, kind)
            if err != nil || running >= int64(agent.MaxConcurrentTasks) { return err }
        } else {
            agent, err := qtx.GetAgentForClaimUpdate(ctx, agentID)
            if err != nil { return err }
            running, err := countClaimCapacity(ctx, qtx, agentID, before.ID, kind)
            if err != nil || running >= int64(agent.MaxConcurrentTasks) { return err }
        }
        task, err := claimCurrentGlobalHead(ctx, qtx, kind, before, runtimeID)
        if errors.Is(err, pgx.ErrNoRows) { return nil }
        if err != nil { return err }
        if err := s.reanchorClaimedDirectChatInput(ctx, qtx, task); err != nil { return err }
        claimed = &task
        return nil
    })
    if err != nil { return nil, err }
    return claimed, nil
}

func currentGlobalEligibleHead(ctx context.Context, qtx *db.Queries, agentID pgtype.UUID, kind runtimeClaimKind) (db.AgentTaskQueue, error) {
    if kind == staleRuntimeReclaim {
        return qtx.GetGlobalEligibleStaleAgentHeadSnapshot(ctx, db.GetGlobalEligibleStaleAgentHeadSnapshotParams{
            AgentID: agentID, ClaimRecoverySecs: claimRecoveryDuration.Seconds(),
        })
    }
    return qtx.GetGlobalEligibleAgentHeadSnapshot(ctx, agentID)
}

func countClaimCapacity(ctx context.Context, qtx *db.Queries, agentID, currentTaskID pgtype.UUID, kind runtimeClaimKind) (int64, error) {
    if kind == staleRuntimeReclaim {
        return qtx.CountOtherAgentCapacityForStaleReclaim(ctx, db.CountOtherAgentCapacityForStaleReclaimParams{
            AgentID: agentID, ExcludedTaskID: currentTaskID,
        })
    }
    return qtx.CountRunningTasks(ctx, agentID)
}

func sameRoutingSnapshot(a, b db.AgentTaskQueue) bool {
    return a.ID == b.ID && a.AgentID == b.AgentID && a.RuntimeID == b.RuntimeID &&
        a.ChatSessionID == b.ChatSessionID && a.PlacementWorkspaceID == b.PlacementWorkspaceID &&
        a.RuntimeRequesterUserID == b.RuntimeRequesterUserID && bytes.Equal(a.RuntimeRequirements, b.RuntimeRequirements) &&
        a.SessionAffinityState == b.SessionAffinityState && a.SessionAffinityRuntimeID == b.SessionAffinityRuntimeID
}

func claimCurrentGlobalHead(ctx context.Context, qtx *db.Queries, kind runtimeClaimKind, head db.AgentTaskQueue, runtimeID pgtype.UUID) (db.AgentTaskQueue, error) {
    if kind == staleRuntimeReclaim {
        return qtx.ReclaimStaleDispatchedTaskForAgentRuntime(ctx, db.ReclaimStaleDispatchedTaskForAgentRuntimeParams{
            AgentID: head.AgentID, RuntimeID: runtimeID,
            ClaimRecoverySecs: claimRecoveryDuration.Seconds(), PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
            ExpectedPlacementWorkspaceID: head.PlacementWorkspaceID,
            ExpectedRuntimeRequesterUserID: head.RuntimeRequesterUserID,
            ExpectedRuntimeRequirements: head.RuntimeRequirements,
            ExpectedSessionAffinityState: head.SessionAffinityState,
            ExpectedSessionAffinityRuntimeID: head.SessionAffinityRuntimeID,
        })
    }
    return qtx.ClaimAgentTaskForRuntime(ctx, db.ClaimAgentTaskForRuntimeParams{
        AgentID: head.AgentID, RuntimeID: runtimeID, PrepareLeaseSecs: prepareLeaseDuration.Seconds(),
        ExpectedPlacementWorkspaceID: head.PlacementWorkspaceID,
        ExpectedRuntimeRequesterUserID: head.RuntimeRequesterUserID,
        ExpectedRuntimeRequirements: head.RuntimeRequirements,
        ExpectedSessionAffinityState: head.SessionAffinityState,
        ExpectedSessionAffinityRuntimeID: head.SessionAffinityRuntimeID,
    })
}

func validatePoolClaimSnapshot(member db.Member, runtime db.AgentRuntime, agent db.Agent, task db.AgentTaskQueue) string {
    pinned := task.SessionAffinityState == runtimepool.SessionAffinityPinned
    if member.WorkspaceID != task.PlacementWorkspaceID || runtime.WorkspaceID != task.PlacementWorkspaceID || agent.WorkspaceID != task.PlacementWorkspaceID {
        if pinned { return "session_runtime_unauthorized" }
        return "no_eligible_runtime"
    }
    if !runtimeaccess.CanUse(member, runtime) {
        if pinned { return "session_runtime_unauthorized" }
        return "no_eligible_runtime"
    }
    if runtime.Status != "online" || !runtime.LastSeenAt.Valid || runtime.LastSeenAt.Time.Before(time.Now().Add(-150*time.Second)) {
        if pinned { return "session_runtime_offline" }
        return "no_eligible_runtime"
    }
    requirements, err := runtimepool.ParseRequirements(task.RuntimeRequirements)
    if err != nil || !runtimepool.ContainsAllCapabilities(runtime.Capabilities, requirements.CapabilitiesAll) {
        if pinned { return "session_runtime_capability_mismatch" }
        return "no_eligible_runtime"
    }
    return ""
}
```

`claimCurrentGlobalHead` is a two-branch adapter: fresh calls `ClaimAgentTaskForRuntime` and stale calls `ReclaimStaleDispatchedTaskForAgentRuntime`, passing every field in `before` as expected CAS input. Both queries repeat the existing full eligibility/serialization predicates and select the global head without a Runtime predicate; only their outer update tests `runtime_id`. After commit, both singular and batch use the existing common lifecycle functions in this order: `captureTaskDispatched`, Agent-status reconciliation, dispatch/status event, analytics/slow metrics, then existing token/finalize response handling. Empty claims keep the existing EmptyClaim result. No event or finalization happens inside the lock transaction.

`recoverStaleDispatchedTaskInTx` is the extracted existing stale recovery/cancel branch: it never rewrites a stale `dispatched` row to `waiting_runtime`, never returns it to the Daemon, and preserves the current recovery/cancel status, terminal timestamp, analytics, and post-commit event behavior. Fresh invalid rows alone use `RequeuePoolTaskAfterClaimRevalidation`. For affinity `none`, every access/offline/capability failure uses `no_eligible_runtime`; only pinned rows use a `session_runtime_*` reason.

```sql
-- name: GetGlobalEligibleAgentHeadSnapshot :one
SELECT q.* FROM agent_task_queue q
WHERE q.agent_id=sqlc.arg(agent_id)::uuid AND q.status='queued'
  AND q.session_affinity_state<>'unresolved'
  AND NOT EXISTS (
    SELECT 1 FROM agent_task_queue active
    WHERE active.agent_id=q.agent_id
      AND active.status IN ('dispatched','running','waiting_local_directory')
      AND (
        (q.issue_id IS NOT NULL AND active.issue_id=q.issue_id) OR
        (q.chat_session_id IS NOT NULL AND active.chat_session_id=q.chat_session_id) OR
        (q.issue_id IS NULL AND q.chat_session_id IS NULL AND q.autopilot_run_id IS NULL AND
         active.issue_id IS NULL AND active.chat_session_id IS NULL AND active.autopilot_run_id IS NULL)
      )
  )
ORDER BY q.priority DESC,q.created_at ASC,q.id ASC LIMIT 1;

-- name: GetGlobalEligibleStaleAgentHeadSnapshot :one
SELECT q.* FROM agent_task_queue q
WHERE q.agent_id=sqlc.arg(agent_id)::uuid AND q.status='dispatched' AND q.started_at IS NULL
  AND q.dispatched_at < now() - make_interval(secs => sqlc.arg(claim_recovery_secs)::double precision)
  AND (q.prepare_lease_expires_at IS NULL OR q.prepare_lease_expires_at < now())
ORDER BY q.priority DESC,q.dispatched_at ASC,q.id ASC LIMIT 1;

-- name: CountOtherAgentCapacityForStaleReclaim :one
SELECT count(*)::bigint FROM agent_task_queue
WHERE agent_id=sqlc.arg(agent_id)::uuid
  AND id<>sqlc.arg(excluded_task_id)::uuid
  AND status IN ('dispatched','running','waiting_local_directory');

-- name: ClaimAgentTaskForRuntime :one
WITH global_head AS (
  SELECT q.id
  FROM agent_task_queue q
  WHERE q.agent_id=sqlc.arg(agent_id)::uuid AND q.status='queued'
    AND q.session_affinity_state<>'unresolved'
    AND NOT EXISTS (
      SELECT 1 FROM agent_task_queue active
      WHERE active.agent_id=q.agent_id
        AND active.status IN ('dispatched','running','waiting_local_directory')
        AND (
          (q.issue_id IS NOT NULL AND active.issue_id=q.issue_id)
          OR (q.chat_session_id IS NOT NULL AND active.chat_session_id=q.chat_session_id)
          OR (
            q.issue_id IS NULL AND q.chat_session_id IS NULL AND q.autopilot_run_id IS NULL
            AND active.issue_id IS NULL AND active.chat_session_id IS NULL AND active.autopilot_run_id IS NULL
          )
        )
    )
  ORDER BY q.priority DESC,q.created_at ASC,q.id ASC
  LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue q
SET status='dispatched',dispatched_at=now(),prepare_lease_expires_at=now() + make_interval(secs => sqlc.arg(prepare_lease_secs)::double precision)
FROM global_head h
WHERE q.id=h.id AND q.agent_id=sqlc.arg(agent_id)::uuid
  AND q.runtime_id = sqlc.arg(runtime_id)::uuid AND q.status='queued'
  AND q.placement_workspace_id IS NOT DISTINCT FROM sqlc.narg(expected_placement_workspace_id)::uuid
  AND q.runtime_requester_user_id IS NOT DISTINCT FROM sqlc.narg(expected_runtime_requester_user_id)::uuid
  AND q.runtime_requirements = sqlc.arg(expected_runtime_requirements)::jsonb
  AND q.session_affinity_state = sqlc.arg(expected_session_affinity_state)::text
  AND q.session_affinity_runtime_id IS NOT DISTINCT FROM sqlc.narg(expected_session_affinity_runtime_id)::uuid
RETURNING q.*;

-- name: ReclaimStaleDispatchedTaskForAgentRuntime :one
WITH global_stale_head AS (
  SELECT q.id FROM agent_task_queue q
  WHERE q.agent_id=sqlc.arg(agent_id)::uuid AND q.status='dispatched'
    AND q.started_at IS NULL
    AND q.dispatched_at < now() - make_interval(secs => sqlc.arg(claim_recovery_secs)::double precision)
    AND (q.prepare_lease_expires_at IS NULL OR q.prepare_lease_expires_at < now())
  ORDER BY q.priority DESC,q.dispatched_at ASC,q.id ASC
  LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE agent_task_queue q
SET dispatched_at=now(),
    prepare_lease_expires_at=now() + make_interval(secs => sqlc.arg(prepare_lease_secs)::double precision)
FROM global_stale_head h
WHERE q.id=h.id AND q.agent_id=sqlc.arg(agent_id)::uuid
  AND q.runtime_id = sqlc.arg(runtime_id)::uuid AND q.status='dispatched' AND q.started_at IS NULL
  AND q.placement_workspace_id IS NOT DISTINCT FROM sqlc.narg(expected_placement_workspace_id)::uuid
  AND q.runtime_requester_user_id IS NOT DISTINCT FROM sqlc.narg(expected_runtime_requester_user_id)::uuid
  AND q.runtime_requirements = sqlc.arg(expected_runtime_requirements)::jsonb
  AND q.session_affinity_state = sqlc.arg(expected_session_affinity_state)::text
  AND q.session_affinity_runtime_id IS NOT DISTINCT FROM sqlc.narg(expected_session_affinity_runtime_id)::uuid
RETURNING q.*;
```

For the batch path, `ListQueuedClaimCandidatesByRuntimes` and the stale preview query may filter the Daemon's Runtime set only to discover attempts; process their global priority/FIFO output with one `map[agent_id]struct{}` and call the transaction helpers above. The helper SQL, not the preview, is the correctness boundary. Add a query-counter assertion that singular and batch both perform the same lock/revalidation sequence and that the stale batch never re-delivers one Agent through two Runtimes.

- [ ] **Step 4: Run GREEN and race tests after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'RuntimeTargetedClaim|BatchClaimPool|ClaimSerializationAcrossRuntimes|ClaimTaskForRuntime' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -race ./internal/service ./internal/handler -run 'RuntimeTargetedClaim|BatchClaimPool' -count=1
```

Expected: PASS for two simultaneous Runtime claims, existing lease/payload tests, and all negative controls.

Run the focused loops separately before the combined command: `TestRuntimeTargetedClaimFiltersOnlyOuterCAS`, `TestRuntimeTargetedClaimSingularTwoRuntimes`, `TestRuntimeTargetedClaimBatchTwoRuntimes`, `TestRuntimeTargetedStaleReclaimTwoRuntimes`, `TestRuntimeTargetedStaleReclaimMaxOneExcludesItself`, `TestRuntimeTargetedFreshInvalidRequeues`, `TestRuntimeTargetedStaleInvalidUsesRecoveryCancel`, `TestRuntimeTargetedClaimRevalidatesLockedHeadSnapshot`, `TestRuntimeTargetedClaimPreservesDirectChatReanchor`, and `TestRuntimeTargetedClaimDowngradeRace`, using `go test ./internal/service ./internal/handler -run '^<name>$' -count=1` (and `-race` for the concurrency tests).

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/service/task_runtime_targeted_claim_test.go server/internal/service/task.go server/internal/service/task_batch_claim_test.go server/internal/handler/daemon_batch_claim_test.go server/internal/handler/daemon_test.go server/pkg/db/queries/agent.sql server/pkg/db/generated/agent.sql.go
git commit -m "fix(server): target claims to assigned runtime"
```

---

### Task 7: Wire one production scheduler and all generic wake triggers

**Files:**
- Create: `server/cmd/server/router_runtime_pool_test.go`, `server/cmd/server/runtime_pool_sweeper_test.go`
- Modify: `server/cmd/server/router.go`, `server/cmd/server/runtime_sweeper.go`
- Modify: `server/internal/service/task.go`, `server/internal/service/runtime_pool_test.go`, `server/internal/service/task_cancel_finalize_test.go`
- Modify: `server/internal/handler/daemon.go`, `server/internal/handler/daemon_test.go`
- Modify: `server/internal/handler/runtime_update.go`, `server/internal/handler/runtime_update_test.go`
- Modify: `server/internal/handler/workspace.go`, `server/internal/handler/workspace_revoke.go`, `server/internal/handler/workspace_test.go`
- Modify: `server/internal/handler/invitation.go`, `server/internal/handler/invitation_test.go`
- Modify: `server/pkg/db/queries/agent.sql`, `server/pkg/db/queries/chat.sql`, `server/pkg/db/queries/squad.sql`, `server/pkg/db/queries/autopilot.sql`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`, `server/pkg/db/generated/chat.sql.go`, `server/pkg/db/generated/squad.sql.go`, `server/pkg/db/generated/autopilot.sql.go`

**Interfaces:**
- Consumes: Task 3 TaskService seam and Task 5 concrete scheduler.
- Produces: exactly one scheduler built after Router chooses Redis/Noop liveness, terminal/cancel/capacity/authorization wakes, and periodic bounded waiting plus due-deferred recovery in the existing 30-second sweeper.

- [ ] **Step 1: Write RED lifecycle tests.** Assert Router's scheduler sees the same final `h.LivenessStore`; `handler.New` does not create a competing scheduler; completed/failed/cancelled tasks wake their placement Workspace after commit; Redis dead->alive, offline->online registration, or DB-heartbeat recovery wakes once after commit while ordinary alive heartbeats do not; private->public, ownership/access expansion, invite acceptance, and role restoration wake. Runtime access tightening requeues queued but returns 409 with in-flight; Member revoke instead locks `Member -> Runtime -> affected ChatSession -> Agent -> Task` and cancels every newly unauthorized nonterminal row. Assert each sweeper tick advances a 32-Workspace cursor, wraps after the last UUID, promotes at most 64 due Pool deferred Tasks per Workspace to `waiting_runtime`, invokes the allocator, emits one queued event/notify per assignment even for a multi-assignment batch, and leaves fixed deferred promotion on its existing Runtime claim path.

Audit every existing “pending/active” query as an explicit acceptance list. Add `waiting_runtime` to Issue pending uniqueness/count/list/cancel/archive, Chat pending/head/list/cancel, Squad working/waiting aggregate, Autopilot pending/run status, comment coalescing eligibility, and workspace/member-revoke selection. Do not add it to `CountRunningTasks`, claim/stale-claim candidates, prepare-lease TTL, or Runtime capacity-bearing counts. `deferred` keeps its existing per-query meaning. Capture this as table-driven `TestWaitingRuntimeQueryMatrix`; each query name is a named subtest and must match literal SQL.

```go
package service

import (
    "context"
    "testing"
    "github.com/multica-ai/multica/server/internal/runtimepool"
)

type sweepRecorder struct{ limit int }
func (r *sweepRecorder) AssignWaiting(context.Context, runtimepool.AssignRequest) (runtimepool.AssignResult, error) {
    return runtimepool.AssignResult{}, nil
}
func (r *sweepRecorder) SweepWaiting(_ context.Context, limit int) ([]runtimepool.AssignResult, error) {
    r.limit = limit
    return nil, nil
}

func TestRuntimePoolSweepUsesWorkspaceBound(t *testing.T) {
    recorder := &sweepRecorder{}
    svc := &TaskService{RuntimePool: recorder}
    if err := svc.SweepRuntimePool(context.Background(), 32); err != nil { t.Fatal(err) }
    if recorder.limit != 32 { t.Fatalf("limit=%d; want 32", recorder.limit) }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./cmd/server ./internal/service ./internal/handler -run 'RuntimePoolWiring|RuntimePoolWake|RuntimePoolSweep|RuntimeAccessChange' -count=1
```

Expected: FAIL because `TaskService.RuntimePool` is nil and terminal/permission paths emit no wake.

- [ ] **Step 3: Implement production order and triggers.** In `router.go`, finish the Redis/Noop branch first, then construct `runtimepool.NewScheduler(queries,pool,h.LivenessStore)` and assign only `h.TaskService.RuntimePool`. Add post-commit calls to the shared TaskService helper; registration compares the persisted prior status/liveness outcome so only dead->alive/offline->online/recovered DB fallback wakes, not every heartbeat receipt. Extend the existing sweeper instead of adding a daemon/goroutine. `Scheduler.SweepWaiting(ctx, 32)` serializes its in-process cursor, reads at most 32 Workspace UUIDs, wraps once to UUID zero, promotes at most 64 rows per selected Workspace with the Task 5 single-Workspace SQL, and then calls `AssignWaiting` for that same Workspace. A failure leaves the cursor at the last successfully processed Workspace so the next tick retries. Permission reductions follow the two distinct Task 4 contracts, never one shared outcome.

```go
func (s *TaskService) SweepRuntimePool(ctx context.Context, workspaceLimit int) error {
    if s == nil || s.RuntimePool == nil { return nil }
    results, err := s.RuntimePool.SweepWaiting(ctx, workspaceLimit)
    for _, result := range results { s.publishPoolAssignmentResult(ctx, result) }
    return err
}

func (s *TaskService) publishPoolAssignmentResult(ctx context.Context, result runtimepool.AssignResult) {
    for _, task := range result.Assigned {
        s.BroadcastTaskQueued(ctx, task)
        s.NotifyTaskEnqueued(ctx, task)
    }
    for _, task := range result.PromotedWaiting {
        s.broadcastTaskEvent(ctx, protocol.EventTaskWaitingRuntime, task)
    }
}

func (s *Scheduler) SweepWaiting(ctx context.Context, workspaceLimit int) ([]AssignResult, error) {
    s.sweepMu.Lock()
    defer s.sweepMu.Unlock()
    workspaces, err := s.q.ListRuntimePoolSweepWorkspaces(ctx, db.ListRuntimePoolSweepWorkspacesParams{AfterWorkspaceID: s.sweepCursor, WorkspaceLimit: int32(workspaceLimit)})
    if err != nil { return nil, err }
    if len(workspaces) == 0 && s.sweepCursor.Valid {
        s.sweepCursor = pgtype.UUID{}
        workspaces, err = s.q.ListRuntimePoolSweepWorkspaces(ctx, db.ListRuntimePoolSweepWorkspacesParams{AfterWorkspaceID: s.sweepCursor, WorkspaceLimit: int32(workspaceLimit)})
        if err != nil { return nil, err }
    }
    results := make([]AssignResult, 0, len(workspaces))
    for _, workspaceID := range workspaces {
        promoted, err := s.q.PromoteDuePoolDeferredTasksForWorkspace(ctx, db.PromoteDuePoolDeferredTasksForWorkspaceParams{
            PlacementWorkspaceID: workspaceID, Now: pgtype.Timestamptz{Time: time.Now(), Valid: true}, PromoteLimit: 64,
        })
        if err != nil { return results, err }
        result, err := s.AssignWaiting(ctx, AssignRequest{WorkspaceID: workspaceID, Limit: AssignmentBatchLimit})
        if err != nil { return results, err }
        result.PromotedWaiting = mergeStillWaiting(promoted, result.PromotedWaiting, result.Assigned)
        results = append(results, result)
        s.sweepCursor = workspaceID
    }
    return results, nil
}

// server/cmd/server/runtime_sweeper.go; call this from the existing ticker case.
func sweepRuntimePool(ctx context.Context, taskSvc *service.TaskService) {
    if taskSvc == nil { return }
    if err := taskSvc.SweepRuntimePool(ctx, 32); err != nil {
        slog.Warn("runtime pool sweep failed", "error", err)
    }
}

// router order
if rdb != nil { h.LivenessStore = handler.NewRedisLivenessStore(rdb) }
h.TaskService.RuntimePool = runtimepool.NewScheduler(queries, pool, h.LivenessStore)
```

- [ ] **Step 4: Run GREEN.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./cmd/server ./internal/service ./internal/handler -run 'RuntimePoolWiring|RuntimePoolWake|RuntimePoolSweep|RuntimeAccessChange|CancelTask' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./cmd/server ./internal/service ./internal/handler
```

Expected: PASS; Noop and Redis paths each construct one scheduler and no trigger wakes before its transaction commits.

Run each lifecycle loop directly: `TestRuntimePoolWiringUsesFinalLiveness`, `TestRuntimePoolSweepPublishesPartialCommittedResultsOnError`, `TestRuntimePoolSweepPromotedUnassignedEmitsWaiting`, `TestRuntimePoolWakeDeadToAlive`, `TestRuntimePoolSweepCursorWrap`, `TestRuntimeAccessTighteningOutcomes`, `TestRuntimeMemberRevokeCancelsWithout409`, and `TestWaitingRuntimeQueryMatrix`, using `go test ./cmd/server ./internal/service ./internal/handler -run '^<name>$' -count=1`.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/cmd/server/router_runtime_pool_test.go server/cmd/server/runtime_pool_sweeper_test.go server/cmd/server/router.go server/cmd/server/runtime_sweeper.go server/internal/service/task.go server/internal/service/runtime_pool_test.go server/internal/service/task_cancel_finalize_test.go server/internal/handler/daemon.go server/internal/handler/daemon_test.go server/internal/handler/runtime_update.go server/internal/handler/runtime_update_test.go server/internal/handler/workspace.go server/internal/handler/workspace_revoke.go server/internal/handler/workspace_test.go server/internal/handler/invitation.go server/internal/handler/invitation_test.go server/pkg/db/queries/agent.sql server/pkg/db/queries/chat.sql server/pkg/db/queries/squad.sql server/pkg/db/queries/autopilot.sql server/pkg/db/generated/agent.sql.go server/pkg/db/generated/chat.sql.go server/pkg/db/generated/squad.sql.go server/pkg/db/generated/autopilot.sql.go
git commit -m "feat(server): wire runtime pool lifecycle"
```

---

### Task 8: Resolve Session affinity, fresh reruns, deletion history, and analytics

**Files:**
- Create: `server/internal/service/task_pool_affinity_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/handler/issue.go`
- Create: `server/internal/handler/issue_runtime_pool_test.go`
- Modify: `server/internal/handler/runtime.go`, `server/internal/handler/runtime_unbind_delete_test.go`, `server/internal/handler/runtime_unbind_preserves_data_test.go`
- Modify: `server/pkg/db/queries/agent.sql`, `server/pkg/db/queries/chat.sql`, `server/pkg/db/queries/runtime.sql`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`, `server/pkg/db/generated/chat.sql.go`, `server/pkg/db/generated/runtime.sql.go`

**Interfaces:**
- Consumes: Task 1 affinity fields and Task 5 pinned allocator.
- Produces: `TaskService.ResolvePoolTaskPlacement`, explicit fresh rerun support, removed-history deletion transaction, same-Runtime claim resume, and Pool analytics that never report `pool` as a physical location.

- [ ] **Step 1: Write RED affinity matrix.** Assert precedence `explicit fresh -> exact rerun -> retry/parent -> legacy force fresh -> Issue/Chat history`; pinned source, missing source Runtime becomes observable cancelled/removed; fixed retry unchanged. Assert deletion cancels pinned waiting calls, clears Runtime soft references on resumable terminal Pool history without changing that history's terminal status or affinity state, and a later follow-up becomes a new cancelled/removed call. Assert deleted Pool Runtime analytics location is empty/unknown, never `pool`.

```text
fresh=true => none even with rerun_of_task_id
rerun/retry source alive => pinned(source.runtime_id)
source pointer removed => cancelled + removed + session_runtime_removed
legacy force_fresh without source => none
```

Copy this precedence RED into `task_pool_affinity_test.go`; append the remaining matrix rows as cases:

```go
package service

import (
    "context"
    "testing"
)

func TestResolvePoolAffinityExplicitFreshWins(t *testing.T) {
    svc := &TaskService{}
    got, err := svc.ResolvePoolTaskPlacement(context.Background(), PoolPlacementRequest{ExplicitFreshSession: true})
    if err != nil { t.Fatal(err) }
    if got.State != "none" || got.RuntimeID.Valid { t.Fatalf("placement=%+v; want none", got) }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolAffinity|ExplicitFresh|RuntimeUnbind.*Pool|PoolDeletedRuntimeAnalytics' -count=1
```

Expected: FAIL because affinity snapshots/removal markers do not exist and analytics falls back to `agent.runtime_mode`.

- [ ] **Step 3: Implement exact resolution/deletion.** Persist resolution inside the shared task-creation transaction and consume the already locked Agent/ChatSession rather than loading an unlocked snapshot. Runtime delete follows Runtime -> ChatSession UUID order -> Agent UUID order -> Task UUID order, cancels already-waiting pinned calls as `removed`, leaves terminal history's status and affinity state unchanged while clearing its Runtime soft reference, and lets the next call materialize a new cancelled/removed Task. Exact rerun/retry follows only its named source Task: `affinity=removed`, `affinity=pinned` with a cleared Runtime, or a terminal Pool source whose Runtime soft reference was cleared resolves `removed` even when it never produced Session/workdir. Ordinary Issue/Chat history pins only when `session_id` or `work_dir` exists. For Chat, the locked `chat_session` pointers and Runtime are authoritative; consult the latest Task only when both Session pointers are absent. Claim suppresses prior Session only for `explicit_fresh_session`; retry/rerun can override legacy force-fresh. Add `fresh_session` to the rerun request DTO: fixed Agents return `400 FRESH_SESSION_REQUIRES_POOL`; Pool writes `explicit_fresh_session=true`. Analytics falls back to Agent location only for fixed tasks.

```go
type PoolPlacementRequest struct {
    ExplicitFreshSession bool
    ForceFreshSession bool
    RerunOfTaskID pgtype.UUID
    RetryOfTaskID pgtype.UUID
    ParentTaskID pgtype.UUID
    AgentID pgtype.UUID
    IssueID pgtype.UUID
    ChatSessionID pgtype.UUID
}
type PoolPlacement struct { State string; RuntimeID pgtype.UUID; WaitReason string }

type RerunIssueRequest struct {
    TaskID pgtype.UUID `json:"task_id,omitempty"`
    FreshSession bool `json:"fresh_session,omitempty"`
}

// In the rerun handler, after loading the target Agent and before creating a Task:
// if request.FreshSession && agent.RuntimeBindingMode != runtimepool.BindingPool,
// write HTTP 400 with {"error":{"code":"FRESH_SESSION_REQUIRES_POOL"}}.

func (s *TaskService) placementFromPointers(ctx context.Context, sessionID, workDir pgtype.Text, runtimeID pgtype.UUID, historicalPinned bool) (PoolPlacement, error) {
    hasPointer := sessionID.Valid || workDir.Valid || historicalPinned
    if hasPointer && runtimeID.Valid {
        if _, err := s.Queries.GetAgentRuntime(ctx, runtimeID); err == nil {
            return PoolPlacement{State: "pinned", RuntimeID: runtimeID}, nil
        } else if !errors.Is(err, pgx.ErrNoRows) { return PoolPlacement{}, err }
    }
    if hasPointer {
        return PoolPlacement{State: "removed", WaitReason: "session_runtime_removed"}, nil
    }
    return PoolPlacement{State: "none"}, nil
}

func (s *TaskService) ResolvePoolTaskPlacement(ctx context.Context, in PoolPlacementRequest) (PoolPlacement, error) {
    if in.ExplicitFreshSession { return PoolPlacement{State: "none"}, nil }
    for _, sourceID := range []pgtype.UUID{in.RerunOfTaskID, in.RetryOfTaskID, in.ParentTaskID} {
        if !sourceID.Valid { continue }
        source, err := s.Queries.GetAgentTask(ctx, sourceID)
        if err != nil { return PoolPlacement{}, fmt.Errorf("load affinity source task: %w", err) }
        runtimeID := source.SessionAffinityRuntimeID
        if !runtimeID.Valid { runtimeID = source.RuntimeID }
        sourceRuntimeWasRemoved := source.RuntimeBindingMode == "pool" && source.CompletedAt.Valid && !runtimeID.Valid
        return s.placementFromPointers(ctx, source.SessionID, source.WorkDir, runtimeID, source.SessionAffinityState == "pinned" || sourceRuntimeWasRemoved)
    }
    if in.ForceFreshSession { return PoolPlacement{State: "none"}, nil }
    if in.IssueID.Valid {
        prior, err := s.Queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{AgentID: in.AgentID, IssueID: in.IssueID})
        if errors.Is(err, pgx.ErrNoRows) { return PoolPlacement{State: "none"}, nil }
        if err != nil { return PoolPlacement{}, err }
        return s.placementFromPointers(ctx, prior.SessionID, prior.WorkDir, prior.RuntimeID, false)
    }
    if in.ChatSessionID.Valid {
        session, err := s.Queries.GetChatSession(ctx, in.ChatSessionID)
        if err == nil && (session.SessionID.Valid || session.WorkDir.Valid) {
            return s.placementFromPointers(ctx, session.SessionID, session.WorkDir, session.RuntimeID, false)
        }
        if err != nil && !errors.Is(err, pgx.ErrNoRows) { return PoolPlacement{}, err }
        prior, err := s.Queries.GetLastChatTaskSession(ctx, in.ChatSessionID)
        if errors.Is(err, pgx.ErrNoRows) { return PoolPlacement{State: "none"}, nil }
        if err != nil { return PoolPlacement{}, err }
        return s.placementFromPointers(ctx, prior.SessionID, prior.WorkDir, prior.RuntimeID, false)
    }
    return PoolPlacement{State: "none"}, nil
}
```

Use these locks from `runtime.go` before any mutation of Pool history:

```sql
-- name: LockPoolChatSessionsForRuntimeDelete :many
SELECT * FROM chat_session WHERE runtime_id=sqlc.arg(runtime_id)::uuid ORDER BY id FOR UPDATE;
-- name: LockPoolAgentsForRuntimeDelete :many
SELECT * FROM agent a
WHERE a.id IN (
  SELECT q.agent_id FROM agent_task_queue q
  WHERE q.runtime_id=sqlc.arg(runtime_id)::uuid OR q.session_affinity_runtime_id=sqlc.arg(runtime_id)::uuid
)
ORDER BY a.id FOR UPDATE;
-- name: LockPoolTasksForRuntimeDelete :many
SELECT * FROM agent_task_queue
WHERE runtime_id=sqlc.arg(runtime_id)::uuid OR session_affinity_runtime_id=sqlc.arg(runtime_id)::uuid
ORDER BY id FOR UPDATE;
```

- [ ] **Step 4: Run GREEN after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolAffinity|ExplicitFresh|RuntimeUnbind.*Pool|PoolDeletedRuntimeAnalytics|ClaimTask.*Session' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/service ./internal/handler
```

Expected: PASS for success/offline/unauthorized/capability/removed/fresh and fixed negative controls.

Run these exact loops before the combined command: `TestResolvePoolAffinityExplicitFreshWins`, `TestResolvePoolAffinityExactRerunDeletedRuntimeIsRemoved`, `TestResolvePoolAffinityExactRetryDeletedRuntimeIsRemoved`, `TestResolvePoolAffinityOrdinaryHistoryNeedsSessionPointer`, `TestResolvePoolAffinityChatSessionPointersAreAuthoritative`, `TestRerunFixedFreshSessionReturns400`, and `TestRuntimeUnbindPoolLockOrder`, each with `go test ./internal/service ./internal/handler -run '^<name>$' -count=1`.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/service/task_pool_affinity_test.go server/internal/service/task.go server/internal/handler/issue.go server/internal/handler/issue_runtime_pool_test.go server/internal/handler/runtime.go server/internal/handler/runtime_unbind_delete_test.go server/internal/handler/runtime_unbind_preserves_data_test.go server/pkg/db/queries/agent.sql server/pkg/db/queries/chat.sql server/pkg/db/queries/runtime.sql server/pkg/db/generated/agent.sql.go server/pkg/db/generated/chat.sql.go server/pkg/db/generated/runtime.sql.go
git commit -m "feat(server): preserve pool session affinity"
```

---

### Task 9: Route Issue, Mention, Quick Create, deferred, retry, and rerun entries

**Files:**
- Create: `server/internal/service/task_pool_factory.go`
- Create: `server/internal/service/task_pool_entries_test.go`
- Modify: `server/internal/service/agent_ready.go`, `server/internal/service/task.go`, `server/internal/service/issue.go`, `server/internal/service/issue_trigger.go`
- Modify: `server/internal/handler/issue.go`, `server/internal/handler/issue_trigger.go`, `server/internal/handler/issue_child_done.go`, `server/internal/handler/comment.go`
- Create: `server/internal/handler/comment_runtime_pool_test.go`
- Modify: `server/internal/handler/comment_trigger_outcomes_test.go`
- Modify: `server/internal/handler/issue_agent_create_e2e_test.go`, `server/internal/handler/agent_builder_test.go`
- Modify: `server/pkg/db/queries/agent.sql`
- Modify: `server/internal/runtimepool/scheduler.go`, `server/internal/runtimepool/scheduler_test.go`
- Create: `server/internal/runtimepool/quick_create.go`, `server/internal/runtimepool/quick_create_test.go`
- Modify: `server/pkg/agent/version.go`, `server/pkg/agent/version_test.go`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`

**Interfaces:**
- Consumes: Task 7 wired scheduler and Task 8 placement resolver.
- Produces: shared Pool task creation for assignee, mention, Leader/member mention, Quick Create, media/deferred promotion, automatic retry, manual rerun; `IsAgentRoutable` means fixed-bound or Pool.

- [ ] **Step 1: Write RED entry matrix.** For every listed entry assert Pool persists `waiting_runtime` with immutable workspace/requester/requirements, then calls the shared allocator; `removed` is returned as cancelled; deferred promotes to waiting before allocation. Race sender Member revoke against create and assert zero surviving `waiting_runtime`/`unresolved` rows. Quick Create Pool requests bypass the pre-assignment fixed-Runtime version check but the allocator excludes candidates below `MinQuickCreateCLIVersion` (and `MinQuickCreateFieldsCLIVersion` when fields are present). Assert fixed-unbound is rejected, fixed-bound copies Runtime, and Mika/Builder fixed-only gates remain closed.

```go
package service

import (
    "testing"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPoolEntryAgentRoutable(t *testing.T) {
    cases := []struct{name string; agent db.Agent; want bool}{
        {"pool unbound", db.Agent{RuntimeBindingMode: "pool"}, true},
        {"fixed bound", db.Agent{RuntimeBindingMode: "fixed", RuntimeID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}}, true},
        {"fixed unbound", db.Agent{RuntimeBindingMode: "fixed"}, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := IsAgentRoutable(tc.agent); got != tc.want { t.Fatalf("got %v; want %v", got, tc.want) }
        })
    }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolEntry|PoolDeferred|PoolRetry|PoolRerun|PoolMention|FixedUnbound|Builder.*Pool' -count=1
```

Expected: FAIL at existing `runtime_id required`/fixed readiness gates for Pool Agents.

- [ ] **Step 3: Implement one mode branch at the lowest shared enqueue helpers.** Pool always inserts waiting first through one transaction-aware factory. It locks/reloads placement Member, then `Member -> ChatSession -> Agent -> Task` for Chat or `Member -> Agent -> Task` otherwise; it must not consume a handler's transaction-external `requireWorkspaceMember` result. Only `pgx.ErrNoRows` from the Member lookup maps to `ErrPoolPlacementMemberRequired`; connection/SQL errors propagate. The factory derives canonical requirements, Workspace and owner fallback from the locked Agent, resolves affinity using the locked ChatSession, and passes a routing snapshot into an entry-specific insert callback. The callback starts from that entry's existing sqlc params, so comment/coalesced IDs, originator/accountable identity, overlay/apps, Squad/leader, retry/rerun/parent lineage, chat input ownership, media marker, context and explicit-fresh fields remain byte-equivalent. Immediate assignment uses `AssignPoolWorkspace` and publishes all returned assignments. `placement principal = originator else locked Agent owner else fail closed`. Member revoke takes `Member -> Runtime -> affected ChatSession -> Agent -> Task` and cancels every now-unauthorized nonterminal Pool row, including unresolved tails. Pool Quick Create carries a typed context marker; candidate validation factors the existing CLI version parser into `agentpkg.ReadRuntimeCLIVersion` and fails closed before assignment. Do not copy selection SQL into handlers.

```go
func IsAgentRoutable(agent db.Agent) bool {
    switch agent.RuntimeBindingMode {
    case runtimepool.BindingPool:
        return true
    case runtimepool.BindingFixed:
        return agent.RuntimeID.Valid
    default:
        return false
    }
}
```

Add the public `createPoolTask` wrapper below in `task_pool_factory.go`, and extract its post-lock placement/snapshot/insert portion into `createPoolTaskLocked(ctx,qtx,input,member,chat,agent)`. Every Issue, Mention, Quick Create, retry/rerun and Squad entry constructs only `PoolTaskCreateInput`. Obligation processing in Task 12 calls the locked form so create and obligation deletion remain atomic:

```go
package service

type PoolTaskCreateInput struct {
    AgentID, IssueID, ChatSessionID, AutopilotRunID pgtype.UUID
    TriggerCommentID pgtype.UUID
    CoalescedCommentIDs []pgtype.UUID
    SquadID, DelegatedFromTaskID, ParentTaskID, RetryOfTaskID, RerunOfTaskID pgtype.UUID
    OriginatorUserID, AccountableUserID, RuleVersionID pgtype.UUID
    RuntimeMCPOverlay, RuntimeConnectedApps, Context json.RawMessage
    TriggerEvidence, HandoffNote, MediaMarker string
    Priority int32
    ExplicitFreshSession, ForceFreshSession, IsLeaderTask bool
    Placement PoolPlacementRequest
    Insert func(context.Context, *db.Queries, PoolRoutingSnapshot) (db.AgentTaskQueue, error)
}

func (s *TaskService) createPoolTask(ctx context.Context, in PoolTaskCreateInput) (db.AgentTaskQueue, error) {
    var created db.AgentTaskQueue
    err := s.runInTx(ctx, func(qtx *db.Queries) error {
        preview, err := qtx.GetAgent(ctx, in.AgentID) // unlocked; discovers lock keys only
        if err != nil { return err }
        requester := in.OriginatorUserID
        if !requester.Valid { requester = preview.OwnerID }
        if !requester.Valid { return ErrPoolPlacementMemberRequired }
        member, err := qtx.LockPoolPlacementMember(ctx, db.LockPoolPlacementMemberParams{WorkspaceID: preview.WorkspaceID, RequesterUserID: requester})
        if errors.Is(err, pgx.ErrNoRows) { return ErrPoolPlacementMemberRequired }
        if err != nil { return err }
        if in.ChatSessionID.Valid {
            if _, err := qtx.LockPoolChatSessionForCreate(ctx, in.ChatSessionID); err != nil { return err }
        }
        agent, err := qtx.LockPoolAgentForTaskCreate(ctx, in.AgentID)
        if err != nil { return err }
        lockedRequester := in.OriginatorUserID
        if !lockedRequester.Valid { lockedRequester = agent.OwnerID }
        if agent.WorkspaceID != preview.WorkspaceID || lockedRequester != requester || member.UserID != lockedRequester ||
            member.WorkspaceID != agent.WorkspaceID || agent.RuntimeBindingMode != runtimepool.BindingPool {
            return ErrPoolPlacementMemberRequired
        }
        txService := *s
        txService.Queries = qtx
        placement, err := txService.ResolvePoolTaskPlacement(ctx, in.Placement)
        if err != nil { return err }
        requirements, err := runtimepool.ParseRequirements(agent.RuntimeRequirements)
        if err != nil { return err }
        canonical, err := runtimepool.CanonicalRequirements(requirements)
        if err != nil { return err }
        routing := NewPoolRoutingSnapshot(agent, member, lockedRequester, canonical, placement)
        // NewPoolRoutingSnapshot maps placement=removed to
        // status=cancelled, completed_at=now, runtime_id=NULL and
        // wait_reason=session_runtime_removed; every insert callback persists it atomically.
        created, err = in.Insert(ctx, qtx, routing)
        return err
    })
    if err != nil { return db.AgentTaskQueue{}, err }
    if created.Status == "cancelled" { return created, nil }
    if _, err := s.AssignPoolWorkspace(ctx, created.PlacementWorkspaceID, created.ID); err != nil { return created, err }
    return created, nil
}
```

The struct above intentionally carries every existing entry datum: attribution and accountable IDs, overlay/connected apps, trigger and coalesced comments, Squad/delegation/parent/retry/rerun lineage, Chat identity, media/deferred marker, Quick Create context, priority and explicit/legacy fresh flags. Each callback copies its existing sqlc params and replaces only routing fields. Add `createPoolTaskLocked(ctx,qtx,in,member,chat,agent)` containing the canonical-requirements, placement, removed-cancellation, and insert portion; Task 12 calls this locked form after its own ordered locks, while the public wrapper above owns lock acquisition and post-commit assignment. No handler fabricates a reduced insert.

In `runtimepool/quick_create.go`, decode the Task context into a typed envelope. An absent `type` is ordinary work; exact `type:"quick_create"` must have the supported schema and fields; malformed JSON, a malformed typed marker, or an unsupported Quick Create schema returns an error and therefore excludes the Runtime. Do not turn parse failure into `recognized=false`:

```go
type QuickCreateContext struct {
    Type string `json:"type"`
    SchemaVersion string `json:"schema_version"`
    Priority string `json:"priority,omitempty"`
    DueDate string `json:"due_date,omitempty"`
}

var ErrUnsupportedQuickCreateSchema = errors.New("unsupported Quick Create context schema")

func ParseQuickCreateContext(raw json.RawMessage) (QuickCreateContext, bool, error) {
    var marker struct { Type string `json:"type"` }
    if err := json.Unmarshal(raw, &marker); err != nil { return QuickCreateContext{}, false, err }
    if marker.Type == "" { return QuickCreateContext{}, false, nil }
    if marker.Type != "quick_create" { return QuickCreateContext{}, false, nil }
    var value QuickCreateContext
    dec := json.NewDecoder(bytes.NewReader(raw)); dec.DisallowUnknownFields()
    if err := dec.Decode(&value); err != nil { return QuickCreateContext{}, true, err }
    if value.SchemaVersion != "multica.quick-create/v1" { return QuickCreateContext{}, true, ErrUnsupportedQuickCreateSchema }
    return value, true, nil
}
```

For Quick Create candidate validation, add this exact fail-closed predicate to Task 5 before the Runtime lock; repeat it after the Runtime lock using current metadata:

```go
func runtimeSupportsPoolQuickCreate(task db.AgentTaskQueue, runtime db.AgentRuntime) bool {
    quick, recognized, err := runtimepool.ParseQuickCreateContext(task.Context)
    if err != nil { return false }
    if !recognized { return true }
    version := agentpkg.ReadRuntimeCLIVersion(runtime.Metadata)
    minimum := agentpkg.MinQuickCreateCLIVersion
    if quick.Priority != "" || quick.DueDate != "" { minimum = agentpkg.MinQuickCreateFieldsCLIVersion }
    return agentpkg.CheckMinCLIVersionFor(version, minimum) == nil
}
```

- [ ] **Step 4: Run GREEN after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolEntry|PoolDeferred|PoolRetry|PoolRerun|PoolMention|FixedUnbound|Builder.*Pool' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'Enqueue|Mention|QuickCreate|Deferred|Retry|Rerun' -count=1
```

Expected: PASS; fixed snapshots and all existing attribution/overlay assertions are byte-equivalent.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/service/task_pool_factory.go server/internal/service/task_pool_entries_test.go server/internal/service/agent_ready.go server/internal/service/task.go server/internal/service/issue.go server/internal/service/issue_trigger.go server/internal/handler/issue.go server/internal/handler/issue_trigger.go server/internal/handler/issue_child_done.go server/internal/handler/comment.go server/internal/handler/comment_runtime_pool_test.go server/internal/handler/comment_trigger_outcomes_test.go server/internal/handler/issue_agent_create_e2e_test.go server/internal/handler/agent_builder_test.go server/internal/runtimepool/scheduler.go server/internal/runtimepool/scheduler_test.go server/internal/runtimepool/quick_create.go server/internal/runtimepool/quick_create_test.go server/pkg/agent/version.go server/pkg/agent/version_test.go server/pkg/db/queries/agent.sql server/pkg/db/generated/agent.sql.go
git commit -m "feat(server): route native pool task entries"
```

---

### Task 10: Route Native Squad and Autopilot without fixed-Runtime assumptions

**Files:**
- Create: `server/internal/handler/squad_runtime_pool_test.go`
- Modify: `server/internal/handler/squad.go`, `server/internal/handler/squad_member_status_test.go`, `server/internal/handler/squad_assign_trigger_test.go`
- Modify: `server/internal/service/autopilot.go`, `server/internal/service/autopilot_squad_test.go`
- Modify: `server/internal/handler/autopilot.go`, `server/internal/handler/autopilot_private_leader_test.go`
- Modify: `server/pkg/db/queries/squad.sql`, `server/pkg/db/queries/autopilot.sql`
- Modify by sqlc generation: `server/pkg/db/generated/squad.sql.go`, `server/pkg/db/generated/autopilot.sql.go`

**Interfaces:**
- Consumes: `IsAgentRoutable` and Pool entry helper from Task 9.
- Produces: Pool-routable Leader/member gates, waiting-aware Squad status, owner-principal Autopilot `run_only`, and no `RuntimeID.Valid` decision in Squad lifecycle.

- [ ] **Step 1: Write RED Squad/Autopilot tests.** Assert a Pool Leader can be selected/updated without pausing Autopilot; initial Squad invocation creates only Leader Task; agent-authored member mention creates a member Task with `delegated_from_task_id` and `squad_id`; waiting members report working/waiting consistently. Autopilot uses Agent owner when no human and fails closed with no owner. Fixed-unbound still blocks.

```go
package service

import (
    "testing"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotRuntimePoolPlacementUsesAgentOwner(t *testing.T) {
    owner := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
    got, err := autopilotPlacementPrincipal(db.Agent{OwnerID: owner})
    if err != nil { t.Fatal(err) }
    if got != owner { t.Fatalf("principal=%v; want owner", got) }
    if _, err := autopilotPlacementPrincipal(db.Agent{}); err == nil { t.Fatal("ownerless agent accepted") }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler ./internal/service -run 'SquadRuntimePool|SquadMemberStatus|AutopilotRuntimePool|PoolLeader' -count=1
```

Expected: FAIL at `newLeader.RuntimeID.Valid`, status SQL omission, and Autopilot runtime requirement.

- [ ] **Step 3: Implement centralized routability.** Replace Squad-specific Runtime checks with the shared predicate; include `waiting_runtime` in `squad.sql`; route Leader/member Tasks through Task 9; keep actual delegation as the existing agent-authored `@mention` path. Autopilot never supplies a synthetic user.

```go
// server/internal/service/autopilot.go (package service)
var ErrAutopilotPlacementPrincipal = errors.New("autopilot agent has no owner")

func autopilotPlacementPrincipal(agent db.Agent) (pgtype.UUID, error) {
    if !agent.OwnerID.Valid { return pgtype.UUID{}, ErrAutopilotPlacementPrincipal }
    return agent.OwnerID, nil
}

func IsSquadLeaderRoutable(agent db.Agent) bool { return IsAgentRoutable(agent) }
```

- [ ] **Step 4: Run GREEN after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler ./internal/service -run 'SquadRuntimePool|SquadMemberStatus|AutopilotRuntimePool|PoolLeader|Squad.*Trigger' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/handler ./internal/service
```

Expected: PASS; fixed Squad/Autopilot tests remain unchanged.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/handler/squad_runtime_pool_test.go server/internal/handler/squad.go server/internal/handler/squad_member_status_test.go server/internal/handler/squad_assign_trigger_test.go server/internal/service/autopilot.go server/internal/service/autopilot_squad_test.go server/internal/handler/autopilot.go server/internal/handler/autopilot_private_leader_test.go server/pkg/db/queries/squad.sql server/pkg/db/queries/autopilot.sql server/pkg/db/generated/squad.sql.go server/pkg/db/generated/autopilot.sql.go
git commit -m "feat(server): route pool squads and autopilots"
```

---

### Task 11: Serialize Pool Chat at the execution head

**Files:**
- Create: `server/internal/service/task_pool_chat_head_test.go`
- Modify: `server/internal/service/task.go`, `server/internal/service/task_complete_race_test.go`, `server/internal/service/task_cancel_finalize_test.go`
- Modify: `server/internal/handler/chat.go`, `server/internal/handler/chat_pending_tasks_test.go`, `server/internal/handler/chat_draft_restore_race_test.go`
- Modify: `server/pkg/db/queries/chat.sql`, `server/pkg/db/queries/agent.sql`
- Modify by sqlc generation: `server/pkg/db/generated/chat.sql.go`, `server/pkg/db/generated/agent.sql.go`

**Interfaces:**
- Consumes: Task 5 head check, Task 8 affinity, shared Task 9 placement-Member lock, and ChatSession -> Agent -> Task terminal lock order.
- Produces: first Pool Chat execution head, `unresolved` tails, terminal affinity handoff, and identical allocator/UI head order.

- [ ] **Step 1: Write RED concurrency matrix.** Concurrently send three first-session messages. Assert only the resolved head becomes eligible; a later high-priority or media-ready tail remains `waiting_runtime/deferred + unresolved + chat_predecessor_pending` even with Runtime B idle. Resolve exactly one tail only after the head transaction locks ChatSession -> Agent -> Task and persists Session/workdir. Table rows, each implemented as one RED/GREEN loop: success; resume-safe failure; resume-unsafe failure; queued-head cancel; running cancel plus late pin; automatic retry ahead of user tail; unresolved second/third-tail cancel (`unresolved -> none`, no head advance); removed Runtime (`cancelled + completed_at + session_runtime_removed`); terminal racing a third send; Runtime delete.

```text
head success with session/runtime => next pinned(head runtime)
head terminal without resumable session => next none
head/runtime removed => next cancelled/removed
until head terminal => every tail unresolved and allocator-ineligible
```

Copy this first RED into `task_pool_chat_head_test.go`; add success/failure/cancel races as table cases around the same helper:

```go
package service

import (
    "testing"
    "github.com/jackc/pgx/v5/pgtype"
)

func TestNextPoolChatAffinityPinsPersistedSessionRuntime(t *testing.T) {
    runtimeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
    got := nextPoolChatAffinity(pgtype.Text{String: "session-1", Valid: true}, pgtype.Text{}, runtimeID, false)
    if got.State != "pinned" || got.RuntimeID != runtimeID { t.Fatalf("placement=%+v", got) }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolChatHead|PoolChatTail|ChatSessionAffinityHandoff' -count=1
```

Expected: FAIL because both first-session Tasks resolve `none` and may choose different Runtimes.

- [ ] **Step 3: Implement one Chat head definition.** The shared Pool Chat creation transaction locks/revalidates placement Member -> ChatSession -> Agent -> Task; if a nonterminal predecessor exists, persist unresolved, commit, and only then invoke the allocator, whose independent transaction uses Member -> Runtime -> ChatSession -> Agent -> Task. Add one `GetPoolChatExecutionHead` query and a `PoolChatHeadSelector.Get(ctx,chatSessionID)` adapter; pending API, cancel/edit, allocator head check, and workspace aggregate must call that adapter rather than own SQL. An existing resolved nonterminal row is always first; only when none exists may `retry_of_task_id IS NOT NULL`, then `priority DESC, created_at ASC, id ASC` choose an unresolved successor. Terminal/cancel/late-pin transaction uses ChatSession -> Agent -> Task, writes Session before resolving one deterministic tail with ordinary `FOR UPDATE` and without `SKIP LOCKED`, persists removed as cancelled with `completed_at`, and wakes after commit. Sender revoke racing create must leave neither waiting nor unresolved rows because create revalidates the locked Member.

Use this pure terminal decision in `task.go`, then call the exact SQL once from each terminal path:

```go
func nextPoolChatAffinity(sessionID, workDir pgtype.Text, runtimeID pgtype.UUID, runtimeRemoved bool) PoolPlacement {
    if runtimeRemoved { return PoolPlacement{State: "removed", WaitReason: "session_runtime_removed"} }
    if sessionID.Valid || workDir.Valid {
        if runtimeID.Valid { return PoolPlacement{State: "pinned", RuntimeID: runtimeID} }
        return PoolPlacement{State: "removed", WaitReason: "session_runtime_removed"}
    }
    return PoolPlacement{State: "none"}
}
```

```sql
-- name: GetPoolChatExecutionHead :one
SELECT * FROM agent_task_queue
WHERE chat_session_id=sqlc.arg(chat_session_id)::uuid
  AND runtime_binding_mode='pool'
  AND status IN ('waiting_runtime','deferred','queued','dispatched','running','waiting_local_directory')
ORDER BY CASE WHEN session_affinity_state<>'unresolved' THEN 0 ELSE 1 END,
         CASE WHEN retry_of_task_id IS NOT NULL THEN 0 ELSE 1 END,
         priority DESC,created_at ASC,id ASC
LIMIT 1;

-- name: LockPoolAgentForChatMutation :one
SELECT * FROM agent WHERE id=sqlc.arg(agent_id)::uuid FOR UPDATE;

-- name: ResolveNextPoolChatTail :one
UPDATE agent_task_queue
SET session_affinity_state=sqlc.arg(affinity_state)::text,
    session_affinity_runtime_id=sqlc.narg(affinity_runtime_id)::uuid,
    wait_reason=sqlc.narg(wait_reason)::text,
    status=CASE WHEN sqlc.arg(affinity_state)::text='removed' THEN 'cancelled' ELSE status END,
    completed_at=CASE WHEN sqlc.arg(affinity_state)::text='removed' THEN now() ELSE completed_at END
WHERE id=(
  SELECT id FROM agent_task_queue
  WHERE chat_session_id=sqlc.arg(chat_session_id)::uuid
    AND runtime_binding_mode='pool' AND session_affinity_state='unresolved'
    AND status IN ('waiting_runtime','deferred')
  ORDER BY CASE WHEN retry_of_task_id IS NOT NULL THEN 0 ELSE 1 END,
           priority DESC,created_at ASC,id ASC LIMIT 1 FOR UPDATE
) RETURNING *;

-- name: CancelUnresolvedPoolChatTail :one
UPDATE agent_task_queue
SET status='cancelled',completed_at=now(),session_affinity_state='none',
    session_affinity_runtime_id=NULL,wait_reason=NULL
WHERE id=sqlc.arg(task_id)::uuid AND chat_session_id=sqlc.arg(chat_session_id)::uuid
  AND runtime_binding_mode='pool' AND status IN ('waiting_runtime','deferred')
  AND session_affinity_state='unresolved' AND wait_reason='chat_predecessor_pending'
RETURNING *;
```

- [ ] **Step 4: Run GREEN and race tests after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler -run 'PoolChatHead|PoolChatTail|ChatSessionAffinityHandoff|ChatPending' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -race ./internal/service ./internal/handler -run 'PoolChat|ChatDraftRestore' -count=1
```

Expected: PASS for success/failure/cancel/concurrent sends; no tail can bind before head terminal.

Implement and run the three-message cases as separate loops: `TestPoolChatThreeMessagesSuccess`, `TestPoolChatThreeMessagesResumeSafeFailure`, `TestPoolChatThreeMessagesResumeUnsafeFailure`, `TestPoolChatThreeMessagesQueuedHeadCancel`, `TestPoolChatThreeMessagesRunningCancelLatePin`, `TestPoolChatThreeMessagesRetryPrecedesTail`, `TestPoolChatThreeMessagesHighPriorityTailCannotJump`, `TestPoolChatThreeMessagesMediaTailCannotJump`, `TestPoolChatUnresolvedCancelDoesNotAdvance`, and `TestPoolChatRemovedCancelsResolvedTail`. Use `go test ./internal/service ./internal/handler -run '^<name>$' -count=1` for each; run the success/cancel/terminal races again with `-race`.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/service/task_pool_chat_head_test.go server/internal/service/task.go server/internal/service/task_complete_race_test.go server/internal/service/task_cancel_finalize_test.go server/internal/handler/chat.go server/internal/handler/chat_pending_tasks_test.go server/internal/handler/chat_draft_restore_race_test.go server/pkg/db/queries/chat.sql server/pkg/db/queries/agent.sql server/pkg/db/generated/chat.sql.go server/pkg/db/generated/agent.sql.go
git commit -m "feat(server): serialize pool chat affinity"
```

---

### Task 12: Make unauthorized queued-comment follow-up durable

**Files:**
- Create: `server/internal/service/comment_followup.go`, `server/internal/service/comment_followup_test.go`
- Modify: `server/internal/service/task.go`
- Modify: `server/internal/handler/comment.go`, `server/internal/handler/comment_merge_failclosed_test.go`, `server/internal/handler/comment_reconcile_test.go`
- Modify: `server/internal/handler/agent.go`, `server/internal/handler/issue.go`
- Modify: `server/cmd/server/runtime_sweeper.go`, `server/cmd/server/runtime_pool_sweeper_test.go`
- Create: `server/pkg/db/queries/comment_followup.sql`
- Modify: `server/pkg/db/queries/agent.sql`, `server/pkg/db/queries/runtime.sql`
- Create by sqlc generation: `server/pkg/db/generated/comment_followup.sql.go`
- Modify by sqlc generation: `server/pkg/db/generated/agent.sql.go`, `server/pkg/db/generated/runtime.sql.go`, `server/pkg/db/generated/models.go`

**Interfaces:**
- Consumes: Task 1 durable table, Task 3 access policy, and Task 7 periodic/terminal hooks.
- Produces: `UpsertCommentFollowupObligation`, bounded unlocked `ListCommentFollowupObligations`, ordered `LockCommentForFollowup` then `LockCommentFollowupObligation`, `TaskService.ProcessCommentFollowups`, and delete-only-after-merge-or-new-Task semantics.

**SQL contracts:**

```text
UpsertCommentFollowupObligation(issue_id uuid, agent_id uuid, comment_id uuid, comment_updated_at timestamptz, head_sha text) -> obligation
ListCommentFollowupObligations(after_updated_at timestamptz, after_id uuid, scan_limit int32) -> []db.AgentCommentFollowupObligation ordered updated_at,id without row locks; wrap after empty page
LockCommentForFollowup(comment_id uuid) -> db.Comment FOR UPDATE
LockCommentFollowupObligation(agent_id uuid, comment_id uuid) -> obligation FOR UPDATE
DeleteCommentFollowupObligation(agent_id uuid, comment_id uuid, comment_updated_at timestamptz, head_sha text) -> affected row
DeleteCommentFollowupObligationInvalid(agent_id uuid, comment_id uuid) -> affected row
CommentExists(comment_id uuid) -> bool
LockMemberForCommentFollowup(workspace_id uuid, requester_user_id uuid) -> db.Member
LockRuntimeForPoolCommentMerge(runtime_id uuid) -> db.AgentRuntime via ordered FOR UPDATE
LockChatSessionForCommentFollowup(chat_session_id uuid) -> db.ChatSession via ordered FOR UPDATE
LockAgentForCommentFollowup(agent_id uuid) -> db.Agent
LockPoolTaskForCommentMerge(task_id uuid) -> db.AgentTaskQueue via FOR UPDATE NOWAIT
MergeCommentIntoPreclaimPoolTask(task_id uuid, expected_status text, requester_user_id uuid, attribution/overlay/apps values) -> db.AgentTaskQueue
Create the follow-up Task through Task 9's transaction-aware Pool factory and its entry-specific comment insert callback; do not add a bypass insert query
```

- [ ] **Step 1: Write RED durable/race tests.** Cover waiting merge rewrites requester; queued private Runtime permits authorized merge; unauthorized merge leaves Task snapshot unchanged and upserts obligation; Runtime delete lock causes `NOWAIT` rollback plus obligation; comment edit updates CAS; comment/Agent/Issue deletion clears it. Race terminal/cancel before and after upsert, allocator race, restart/sweeper replay, and duplicate processors; each accepted comment creates or merges exactly once.

```go
package service

import (
    "os"
    "strings"
    "testing"
)

func TestCommentFollowupLocksCommentBeforeObligation(t *testing.T) {
    raw, err := os.ReadFile("../../pkg/db/queries/comment_followup.sql")
    if err != nil { t.Fatal(err) }
    sql := string(raw)
    for _, fragment := range []string{"ON CONFLICT (agent_id, comment_id)", "-- name: ListCommentFollowupObligations", "-- name: LockCommentForFollowup", "-- name: LockCommentFollowupObligation"} {
        if !strings.Contains(sql, fragment) { t.Fatalf("query missing %q", fragment) }
    }
    listStart := strings.Index(sql, "-- name: ListCommentFollowupObligations")
    lockStart := strings.Index(sql, "-- name: LockCommentForFollowup")
    if strings.Contains(sql[listStart:lockStart], "FOR UPDATE") { t.Fatal("scanner must not lock obligation before Comment") }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler ./cmd/server -run 'CommentFollowupObligation|PoolCommentMerge|CommentFollowupSweep' -count=1
```

Expected: FAIL because queued unauthorized comments have no durable row and cancel can bypass completion reconciliation.

- [ ] **Step 3: Implement the durable processor and safe locks.** The periodic scanner keeps a mutex-protected `(updated_at,id)` cursor, reads a bounded page without locking, advances after every attempted ID, and wraps once only after an empty page, so a locked/blocked oldest row cannot starve later obligations. For each ID, a short transaction locks Comment -> Obligation, verifies Comment/Issue/Agent Workspace equality, resolves the human originator by following `comment.source_task_id` through the existing Task attribution chain (cycle/missing/no-human fails closed), then locks Member -> optional Runtime -> optional ChatSession -> Agent -> final Task. The pre-Agent pending row is used only to obtain Runtime/Chat lock keys. Immediately after the Agent lock, re-read the current `(issue,agent)` pending row and the shared Task 11 Chat head. If pending existence, Task ID/status, Runtime ID, ChatSession ID, or head ID differs from the pre-lock snapshot, roll the whole transaction back and retry it from Comment lock; only a stable post-Agent snapshot may reach final Task merge/create. Reload attribution, overlay and connected apps from the locked current Comment/source chain; never inherit the rejected Task identity. If the initial Comment lock returns `pgx.ErrNoRows`, roll back and run a cleanup transaction that locks the obligation and deletes it only when `CommentExists(comment_id)=false`. A version mismatch refreshes `comment_updated_at` and current Issue HEAD and keeps the obligation. An active Task with a different HEAD keeps it pending. When the `(issue,agent)` slot is idle, advance the locked obligation to current HEAD and call Task 9's `createPoolTaskLocked` with the already locked Member/ChatSession/Agent and the normal comment insert callback; conditionally delete only in the same transaction after merge/create succeeds and all four CAS values still match. Runtime/ChatSession/Agent use ordinary ordered `FOR UPDATE`; only the final Task uses `NOWAIT`. The request path that first discovers unauthorized queued merge rolls back the Task mutation and, before returning deferred, starts a separate transaction that locks and re-reads the current Comment `FOR UPDATE`, validates Comment/Issue/Agent Workspace equality, then calls `UpsertCommentFollowupObligation` in **Comment -> Obligation** order. No initial obligation upsert may execute without the Comment lock: this lock is the Workspace-delete barrier, and a writer released behind it must be visible to the delete transaction's following statement. Terminal/cancel wakes `(issue,agent)` after commit; periodic scan recovers missed notifications.

Start the service with this compiling bounded claim helper, then add one reconciliation outcome per table case:

```go
package service

import (
    "context"
    "errors"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgconn"
    "github.com/jackc/pgx/v5/pgtype"
    "github.com/multica-ai/multica/server/internal/runtimeaccess"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errFollowupPending = errors.New("comment follow-up remains pending")
var errFollowupCASLost = errors.New("comment follow-up CAS lost")
var errFollowupCommentMissing = errors.New("comment follow-up Comment missing")
var errFollowupLockKeysChanged = errors.New("comment follow-up lock keys changed")

type commentFollowupCursor struct { UpdatedAt pgtype.Timestamptz; ID pgtype.UUID }

// Add followupMu sync.Mutex and followupCursor commentFollowupCursor to TaskService.
func listCommentFollowups(ctx context.Context, q *db.Queries, cursor commentFollowupCursor, limit int32) ([]db.AgentCommentFollowupObligation, error) {
    return q.ListCommentFollowupObligations(ctx, db.ListCommentFollowupObligationsParams{
        AfterUpdatedAt: cursor.UpdatedAt, AfterID: cursor.ID, ScanLimit: limit,
    })
}

func (s *TaskService) ProcessCommentFollowups(ctx context.Context, limit int32) error {
    s.followupMu.Lock()
    defer s.followupMu.Unlock()
    obligations, err := listCommentFollowups(ctx, s.Queries, s.followupCursor, limit)
    if err != nil { return err }
    if len(obligations) == 0 && s.followupCursor.ID.Valid {
        s.followupCursor = commentFollowupCursor{}
        obligations, err = listCommentFollowups(ctx, s.Queries, s.followupCursor, limit)
        if err != nil { return err }
    }
    for _, obligation := range obligations {
        s.followupCursor = commentFollowupCursor{UpdatedAt: obligation.UpdatedAt, ID: obligation.ID}
        if err := s.processCommentFollowup(ctx, obligation.AgentID, obligation.CommentID); err != nil {
            if errors.Is(err, errFollowupPending) || errors.Is(err, errFollowupCASLost) || isLockNotAvailable(err) { continue }
            return err
        }
    }
    return nil
}

func (s *TaskService) processCommentFollowup(ctx context.Context, agentID, commentID pgtype.UUID) error {
    for attempt := 0; attempt < 3; attempt++ {
        err := s.processCommentFollowupOnce(ctx, agentID, commentID)
        if !errors.Is(err, errFollowupLockKeysChanged) { return err }
    }
    return errFollowupPending
}

func (s *TaskService) processCommentFollowupOnce(ctx context.Context, agentID, commentID pgtype.UUID) error {
    err := s.runInTx(ctx, func(qtx *db.Queries) error {
        comment, err := qtx.LockCommentForFollowup(ctx, commentID)
        if errors.Is(err, pgx.ErrNoRows) { return errFollowupCommentMissing }
        if err != nil { return err }
        obligation, err := qtx.LockCommentFollowupObligation(ctx, db.LockCommentFollowupObligationParams{AgentID: agentID, CommentID: commentID})
        if err != nil { return err }
        issue, err := qtx.GetIssue(ctx, obligation.IssueID)
        if err != nil { return err }
        agent, err := qtx.GetAgent(ctx, obligation.AgentID)
        if err != nil { return err }
        if comment.IssueID != issue.ID || comment.WorkspaceID != issue.WorkspaceID || agent.WorkspaceID != issue.WorkspaceID {
            _, err := qtx.DeleteCommentFollowupObligationInvalid(ctx, db.DeleteCommentFollowupObligationInvalidParams{AgentID: agentID, CommentID: commentID})
            return err
        }
        if comment.UpdatedAt != obligation.CommentUpdatedAt {
            head, err := qtx.GetIssueReviewHeadSha(ctx, issue.ID)
            if err != nil { return err }
            _, err = qtx.RefreshCommentFollowupObligation(ctx, db.RefreshCommentFollowupObligationParams{AgentID: agentID, CommentID: commentID, CommentUpdatedAt: comment.UpdatedAt, HeadSha: head})
            return err
        }
        originator, err := s.ResolveHumanOriginatorForComment(ctx, qtx, comment)
        if err != nil { return err }
        member, err := qtx.LockMemberForCommentFollowup(ctx, db.LockMemberForCommentFollowupParams{WorkspaceID: issue.WorkspaceID, RequesterUserID: originator})
        if err != nil { return err }
        pending, err := qtx.GetPendingTaskForCommentFollowup(ctx, db.GetPendingTaskForCommentFollowupParams{IssueID: issue.ID, AgentID: agentID})
        if err != nil && !errors.Is(err, pgx.ErrNoRows) { return err }
        previewPendingFound := err == nil
        var previewHeadID pgtype.UUID
        var runtime db.AgentRuntime
        if previewPendingFound && pending.RuntimeID.Valid {
            runtime, err = qtx.LockRuntimeForPoolCommentMerge(ctx, pending.RuntimeID)
            if err != nil { return err }
            if !runtimeaccess.CanUse(member, runtime) { return errFollowupPending }
        }
        var chat db.ChatSession
        if previewPendingFound && pending.ChatSessionID.Valid {
            chat, err = qtx.LockChatSessionForCommentFollowup(ctx, pending.ChatSessionID)
            if err != nil { return err }
            head, err := qtx.GetPoolChatExecutionHead(ctx, pending.ChatSessionID)
            if err != nil { return err }
            if head.ID != pending.ID { return errFollowupPending }
            previewHeadID = head.ID
        }
        lockedAgent, err := qtx.LockAgentForCommentFollowup(ctx, agentID)
        if err != nil { return err }
        currentPending, currentErr := qtx.GetPendingTaskForCommentFollowup(ctx, db.GetPendingTaskForCommentFollowupParams{IssueID: issue.ID, AgentID: agentID})
        if currentErr != nil && !errors.Is(currentErr, pgx.ErrNoRows) { return currentErr }
        currentPendingFound := currentErr == nil
        if !sameFollowupPendingLockKeys(pending, previewPendingFound, currentPending, currentPendingFound) {
            return errFollowupLockKeysChanged
        }
        if currentPendingFound && currentPending.ChatSessionID.Valid {
            head, err := qtx.GetPoolChatExecutionHead(ctx, currentPending.ChatSessionID)
            if err != nil { return err }
            if head.ID != currentPending.ID || (previewHeadID.Valid && head.ID != previewHeadID) {
                return errFollowupLockKeysChanged
            }
        }
        return s.reconcileLockedCommentFollowup(ctx, qtx, comment, obligation, currentPending, originator, member, chat, lockedAgent)
    })
    if errors.Is(err, errFollowupCommentMissing) {
        return s.deleteMissingCommentObligation(ctx, agentID, commentID)
    }
    return err
}

func sameFollowupPendingLockKeys(a db.AgentTaskQueue, aFound bool, b db.AgentTaskQueue, bFound bool) bool {
    if aFound != bFound { return false }
    if !aFound { return true }
    return a.ID == b.ID && a.AgentID == b.AgentID && a.IssueID == b.IssueID &&
        a.RuntimeID == b.RuntimeID && a.ChatSessionID == b.ChatSessionID && a.Status == b.Status
}

func (s *TaskService) deleteMissingCommentObligation(ctx context.Context, agentID, commentID pgtype.UUID) error {
    return s.runInTx(ctx, func(qtx *db.Queries) error {
        obligation, err := qtx.LockCommentFollowupObligation(ctx, db.LockCommentFollowupObligationParams{AgentID: agentID, CommentID: commentID})
        if errors.Is(err, pgx.ErrNoRows) { return nil }
        if err != nil { return err }
        exists, err := qtx.CommentExists(ctx, obligation.CommentID)
        if err != nil { return err }
        if exists { return errFollowupPending }
        affected, err := qtx.DeleteCommentFollowupObligationInvalid(ctx, db.DeleteCommentFollowupObligationInvalidParams{AgentID: agentID, CommentID: commentID})
        if err != nil { return err }
        if affected != 1 { return errFollowupCASLost }
        return nil
    })
}

func (s *TaskService) reconcileLockedCommentFollowup(ctx context.Context, qtx *db.Queries, comment db.Comment, obligation db.AgentCommentFollowupObligation, pending db.AgentTaskQueue, originator pgtype.UUID, member db.Member, chat db.ChatSession, agent db.Agent) error {
    currentHead, err := qtx.GetIssueReviewHeadSha(ctx, obligation.IssueID)
    if err != nil { return err }
    if pending.ID.Valid {
        locked, err := qtx.LockPoolTaskForCommentMerge(ctx, pending.ID)
        if err != nil { return err }
        lockedHead, err := ExtractTaskHeadSHA(locked.Context)
        if err != nil { return err }
        if lockedHead != obligation.HeadSha { return nil }
        if _, err := qtx.MergeCommentIntoPreclaimPoolTask(ctx, db.MergeCommentIntoPreclaimPoolTaskParams{
            TaskID: locked.ID, CommentID: comment.ID, RequesterUserID: originator,
            ExpectedStatus: locked.Status, ExpectedHeadSha: obligation.HeadSha,
        }); err != nil { return err }
    } else {
        if currentHead != obligation.HeadSha {
            refreshed, err := qtx.RefreshCommentFollowupObligation(ctx, db.RefreshCommentFollowupObligationParams{
                AgentID: obligation.AgentID, CommentID: obligation.CommentID,
                CommentUpdatedAt: comment.UpdatedAt, HeadSha: currentHead,
            })
            if err != nil { return err }
            obligation = refreshed
        }
        if _, err := s.createPoolTaskLocked(ctx, qtx, PoolTaskCreateInput{
            AgentID: obligation.AgentID, IssueID: obligation.IssueID,
            OriginatorUserID: originator, TriggerCommentID: comment.ID,
            Insert: commentFollowupInsertCallback(comment, obligation),
        }, member, chat, agent); err != nil { return err }
    }
    affected, err := qtx.DeleteCommentFollowupObligation(ctx, db.DeleteCommentFollowupObligationParams{
        AgentID: obligation.AgentID, CommentID: obligation.CommentID,
        CommentUpdatedAt: obligation.CommentUpdatedAt, HeadSha: obligation.HeadSha,
    })
    if err == nil && affected != 1 { return errFollowupCASLost }
    return err
}

func isLockNotAvailable(err error) bool {
    var pgErr *pgconn.PgError
    return errors.As(err, &pgErr) && pgErr.Code == "55P03"
}
```

```sql
-- name: UpsertCommentFollowupObligation :one
INSERT INTO agent_comment_followup_obligation
  (issue_id,agent_id,comment_id,comment_updated_at,head_sha)
VALUES
  (sqlc.arg(issue_id)::uuid,sqlc.arg(agent_id)::uuid,sqlc.arg(comment_id)::uuid,
   sqlc.arg(comment_updated_at)::timestamptz,sqlc.arg(head_sha)::text)
ON CONFLICT (agent_id,comment_id) DO UPDATE
SET comment_updated_at=EXCLUDED.comment_updated_at,head_sha=EXCLUDED.head_sha,updated_at=now()
RETURNING *;

-- name: ListCommentFollowupObligations :many
SELECT * FROM agent_comment_followup_obligation
WHERE sqlc.narg(after_updated_at)::timestamptz IS NULL
   OR (updated_at,id) > (sqlc.narg(after_updated_at)::timestamptz,sqlc.narg(after_id)::uuid)
ORDER BY updated_at ASC,id ASC LIMIT sqlc.arg(scan_limit);

-- name: LockCommentForFollowup :one
SELECT * FROM comment WHERE id=sqlc.arg(comment_id)::uuid FOR UPDATE;

-- name: LockCommentFollowupObligation :one
SELECT * FROM agent_comment_followup_obligation
WHERE agent_id=sqlc.arg(agent_id)::uuid AND comment_id=sqlc.arg(comment_id)::uuid
FOR UPDATE;

-- name: CommentExists :one
SELECT EXISTS(SELECT 1 FROM comment WHERE id=sqlc.arg(comment_id)::uuid);

-- name: DeleteCommentFollowupObligationInvalid :execrows
DELETE FROM agent_comment_followup_obligation
WHERE agent_id=sqlc.arg(agent_id)::uuid AND comment_id=sqlc.arg(comment_id)::uuid;

-- name: RefreshCommentFollowupObligation :one
UPDATE agent_comment_followup_obligation
SET comment_updated_at=sqlc.arg(comment_updated_at)::timestamptz,
    head_sha=sqlc.arg(head_sha)::text,updated_at=now()
WHERE agent_id=sqlc.arg(agent_id)::uuid AND comment_id=sqlc.arg(comment_id)::uuid
RETURNING *;

-- name: DeleteCommentFollowupObligation :execrows
DELETE FROM agent_comment_followup_obligation
WHERE agent_id=sqlc.arg(agent_id)::uuid AND comment_id=sqlc.arg(comment_id)::uuid
  AND comment_updated_at=sqlc.arg(comment_updated_at)::timestamptz
  AND head_sha=sqlc.arg(head_sha)::text;

-- name: LockMemberForCommentFollowup :one
SELECT * FROM member WHERE workspace_id=sqlc.arg(workspace_id)::uuid
  AND user_id=sqlc.arg(requester_user_id)::uuid FOR UPDATE;
-- name: LockRuntimeForPoolCommentMerge :one
SELECT * FROM agent_runtime WHERE id=sqlc.arg(runtime_id)::uuid FOR UPDATE;
-- name: LockChatSessionForCommentFollowup :one
SELECT * FROM chat_session WHERE id=sqlc.arg(chat_session_id)::uuid FOR UPDATE;
-- name: LockAgentForCommentFollowup :one
SELECT * FROM agent WHERE id=sqlc.arg(agent_id)::uuid FOR UPDATE;
-- name: LockPoolTaskForCommentMerge :one
SELECT * FROM agent_task_queue WHERE id=sqlc.arg(task_id)::uuid FOR UPDATE NOWAIT;
```

- [ ] **Step 4: Run GREEN and race tests after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/service ./internal/handler ./cmd/server -run 'CommentFollowupObligation|PoolCommentMerge|CommentFollowupSweep' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -race ./internal/service ./internal/handler -run 'CommentFollowup|PoolCommentMerge' -count=1
```

Expected: PASS; cancellation, terminal, delete-lock, and replay races converge with one follow-up.

Run the durable cases separately before the combined command: `TestCommentFollowupLocksCommentBeforeObligation`, `TestCommentFollowupInitialUpsertLocksComment`, `TestCommentFollowupFairCursorWraps`, `TestCommentFollowupAgentAuthoredUsesHumanSourceOriginator`, `TestCommentFollowupOptionalChatRevalidatesHead`, `TestCommentFollowupRetriesWhenPendingLockKeysChangeAfterAgentLock`, `TestCommentFollowupRuntimeUsesBlockingLock`, `TestCommentFollowupMissingCommentDeletesObligation`, `TestCommentFollowupDeleteUsesFourValueCAS`, `TestCommentFollowupCreatesThroughLockedFactory`, and `TestCommentFollowupCancelTerminalRace`, using `go test ./internal/service ./internal/handler ./cmd/server -run '^<name>$' -count=1`; repeat initial-upsert barrier, cursor, pending-key, duplicate processor, and cancel races with `-race`.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/service/comment_followup.go server/internal/service/comment_followup_test.go server/internal/service/task.go server/internal/handler/comment.go server/internal/handler/comment_merge_failclosed_test.go server/internal/handler/comment_reconcile_test.go server/internal/handler/agent.go server/internal/handler/issue.go server/cmd/server/runtime_sweeper.go server/cmd/server/runtime_pool_sweeper_test.go server/pkg/db/queries/comment_followup.sql server/pkg/db/queries/agent.sql server/pkg/db/queries/runtime.sql server/pkg/db/generated/comment_followup.sql.go server/pkg/db/generated/agent.sql.go server/pkg/db/generated/runtime.sql.go server/pkg/db/generated/models.go
git commit -m "feat(server): persist comment followup obligations"
```

---

### Task 13: Expose complete Server Agent, Task, and Chat wire contracts

**Files:**
- Create: `server/internal/handler/runtime_pool_wire_test.go`
- Modify: `server/internal/handler/agent.go`, `server/internal/handler/agent_test.go`
- Modify: `server/internal/handler/chat.go`, `server/internal/handler/chat_pending_tasks_test.go`
- Modify: `server/pkg/protocol/events.go`
- Modify: `server/internal/realtime/hub.go`, `server/internal/realtime/hub_test.go`

**Interfaces:**
- Consumes: persisted routing fields and waiting reasons from Tasks 1-12.
- Produces: Agent `runtime_binding_mode`, `runtime_requirements`, `runtime_routable`; Task, `QueuedChatTaskResponse`, workspace pending-Chat item and Realtime payload `runtime_binding_mode`, `status`, `wait_reason`, `session_affinity_state`; `runtime_id:""` compatibility while unassigned.

- [ ] **Step 1: Write RED exact-JSON tests.** Assert Pool Agent is unbound but routable, fixed-bound routable, fixed-unbound not routable; AgentTaskResponse and both pending Chat response shapes expose waiting state/reason/mode while encoding NULL Runtime as empty string.

```json
{"runtime_binding_mode":"pool","runtime_requirements":{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]},"runtime_bound":false,"runtime_routable":true}
{"runtime_id":"","runtime_binding_mode":"pool","status":"waiting_runtime","wait_reason":"no_eligible_runtime","session_affinity_state":"none"}
```

Copy this first RED into `runtime_pool_wire_test.go`; add Agent and pending-Chat shapes as neighboring table cases:

```go
package handler

import (
    "testing"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimePoolWireKeepsEmptyRuntimeForWaitingPoolTask(t *testing.T) {
    task := db.AgentTaskQueue{RuntimeBindingMode: "pool", Status: "waiting_runtime", WaitReason: pgtype.Text{String: "no_eligible_runtime", Valid: true}}
    runtimeID, mode, reason := taskRuntimeWire(task)
    if runtimeID != "" || mode != "pool" || reason != "no_eligible_runtime" {
        t.Fatalf("wire=(%q,%q,%q)", runtimeID, mode, reason)
    }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler -run 'RuntimePoolWire|AgentResponseRuntime|PendingChatRuntime' -count=1
```

Expected: FAIL because current response structs omit routing/waiting fields.

- [ ] **Step 3: Implement response mapping only.** Extend `AgentResponse`, `AgentTaskResponse`, `taskToResponse`, both existing pending Chat DTOs (`QueuedChatTaskResponse` and workspace aggregate item), and the waiting/queued Realtime payload with `session_affinity_state` alongside mode/status/reason. Derive `runtime_routable` from fixed-bound-or-Pool, not Provider/config, and do not make `runtime_id` nullable. Exact JSON tests cover root Task, queued Chat and workspace aggregate shapes plus the event allowlist/hub payload.

```go
func taskRuntimeWire(task db.AgentTaskQueue) (runtimeID, mode, waitReason string) {
    if !task.WaitReason.Valid { return uuidToString(task.RuntimeID), task.RuntimeBindingMode, "" }
    return uuidToString(task.RuntimeID), task.RuntimeBindingMode, task.WaitReason.String
}

func agentRuntimeRoutable(agent db.Agent) bool {
    return agent.RuntimeBindingMode == "pool" || agent.RuntimeID.Valid
}
```

- [ ] **Step 4: Run GREEN.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler -run 'RuntimePoolWire|AgentResponseRuntime|PendingChatRuntime|AgentTask' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/handler
```

Expected: PASS; fixed response fields remain unchanged except additive keys.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/handler/runtime_pool_wire_test.go server/internal/handler/agent.go server/internal/handler/agent_test.go server/internal/handler/chat.go server/internal/handler/chat_pending_tasks_test.go server/pkg/protocol/events.go server/internal/realtime/hub.go server/internal/realtime/hub_test.go
git commit -m "feat(server): expose runtime pool wire state"
```

---

### Task 14: Import new Extension Releases as Pool-native Squads

**Files:**
- Create: `server/internal/handler/testdata/platform_extensions/fixed-mapping.golden.json`
- Modify: `server/internal/handler/platform_extension.go`, `server/internal/handler/platform_extension_contract.go`
- Modify: `server/internal/handler/platform_extension_import_test.go`, `server/internal/handler/platform_extension_contract_test.go`, `server/internal/handler/platform_extension_allocator_test.go`
- Modify: `server/pkg/db/queries/platform_extension.sql`
- Modify by sqlc generation: `server/pkg/db/generated/platform_extension.sql.go`

**Interfaces:**
- Consumes: Pool Agent creation/routing and Release schema from Tasks 1 and 9.
- Produces: discriminated server `RuntimePolicy`, new Release `pool + runtime:null`, exact extension capability requirement, and byte-compatible fixed Release decoding.

- [ ] **Step 1: Write RED import/mapping tests.** Import with zero Runtimes must succeed; Agents are `pool`, `runtime_id=NULL`, all Agent-Skill bindings remain; no allocator query runs at import. Assert exact Pool JSON and fixed golden JSON; old resources without `runtime_policy` decode fixed and retain Runtime.

```json
{"runtime_policy":{"mode":"pool","requirements":{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}},"runtime":null}
```

Copy this exact mapping RED into `platform_extension_contract_test.go`:

```go
package handler

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestPlatformExtensionPoolMappingHasNullRuntime(t *testing.T) {
    raw, err := json.Marshal(PlatformExtensionMappingResponse{RuntimePolicy: poolExtensionRuntimePolicy(), Runtime: nil})
    if err != nil { t.Fatal(err) }
    if !strings.Contains(string(raw), `"mode":"pool"`) || !strings.Contains(string(raw), `"runtime":null`) {
        t.Fatalf("mapping=%s", raw)
    }
}
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler -run 'PlatformExtension.*Pool|PlatformExtension.*FixedCompatibility|PlatformExtension.*NoRuntime' -count=1
```

Expected: FAIL with `PLATFORM_RUNTIME_UNAVAILABLE` and non-null Runtime mapping requirement.

- [ ] **Step 3: Implement Pool importer and discriminated mapping.** Remove import-time Runtime enumeration/lock/allocation. Dedicated helper creates Pool Agents with unchanged `runtime_config.platform_agent`, prompts, skills, commands, Leader, and Squad instructions. `platformExtensionMappingFromRelease` branches on stored release mode; only fixed requires Runtime.

```go
type RuntimePolicy struct {
    Mode string `json:"mode"`
    Requirements json.RawMessage `json:"requirements,omitempty"`
}
type PlatformExtensionMappingResponse struct {
    RuntimePolicy RuntimePolicy `json:"runtime_policy"`
    Runtime *PlatformRuntimeResponse `json:"runtime"`
}

func poolExtensionRuntimePolicy() RuntimePolicy {
    requirements, err := runtimepool.CanonicalRequirements(runtimepool.Requirements{
        SchemaVersion: runtimepool.RequirementsSchemaV1,
        CapabilitiesAll: []string{runtimepool.CapabilityExtensionExecuteV1},
    })
    if err != nil { panic("static extension requirements are invalid: " + err.Error()) }
    return RuntimePolicy{Mode: "pool", Requirements: requirements}
}
```

- [ ] **Step 4: Run GREEN after sqlc generation.**

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./internal/handler -run 'PlatformExtension' -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./internal/handler
```

Expected: PASS for no-Runtime import, idempotency/rollback, Pool JSON, and fixed golden.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add server/internal/handler/testdata/platform_extensions/fixed-mapping.golden.json server/internal/handler/platform_extension.go server/internal/handler/platform_extension_contract.go server/internal/handler/platform_extension_import_test.go server/internal/handler/platform_extension_contract_test.go server/internal/handler/platform_extension_allocator_test.go server/pkg/db/queries/platform_extension.sql server/pkg/db/generated/platform_extension.sql.go
git commit -m "feat(server): import pool-native extensions"
```

---

### Task 15: Model Pool contracts in Core and Realtime

**Files:**
- Modify: `packages/core/types/agent.ts`, `packages/core/types/chat.ts`, `packages/core/types/index.ts`, `packages/core/types/events.ts`
- Modify: `packages/core/api/schemas.ts`, `packages/core/api/schemas.test.ts`, `packages/core/api/client.ts`, `packages/core/api/client.test.ts`
- Modify: `packages/core/extensions/types.ts`, `packages/core/extensions/schemas.ts`, `packages/core/extensions/schemas.test.ts`, `packages/core/extensions/api-client.test.ts`
- Modify: `packages/core/agents/runtime-binding.ts`, `packages/core/agents/runtime-binding.test.ts`, `packages/core/agents/derive-presence.ts`, `packages/core/agents/derive-presence.test.ts`
- Modify: `packages/core/realtime/use-realtime-sync.ts`, `packages/core/realtime/use-realtime-sync.test.ts`

**Interfaces:**
- Consumes: exact Server JSON from Tasks 13-14.
- Produces: separate execution/location types, strict Pool/fixed Extension union, waiting Task schema, `isAgentRunnable`, retained `isAgentRuntimeBound` for links, and waiting->queued Realtime transition.

- [ ] **Step 1: Write RED schema/helper tests.** Parse exact server fixtures; reject mixed Pool-with-Runtime and fixed-without-Runtime mapping; assert `RuntimeDevice.runtime_mode` rejects `pool`, Pool Agent is runnable, fixed-unbound is not, waiting is pending-not-running, and Realtime preserves wait reason then clears it on queued.

```ts
import { describe, expect, it } from "vitest";
import type { Agent } from "../types";
import { buildRerunIssueBody } from "../api/client";
import { isAgentRunnable, isAgentRuntimeBound } from "./runtime-binding";

describe("runtime pool binding", () => {
  it("allows invocation without presenting a runtime link", () => {
    const agent = {runtime_binding_mode: "pool", runtime_routable: true, runtime_bound: false, runtime_id: ""} as Agent;
    expect(isAgentRunnable(agent)).toBe(true);
    expect(isAgentRuntimeBound(agent)).toBe(false);
  });
});

describe("rerunIssue fresh session body", () => {
  it("sends audit source and explicit fresh flag together", () => {
    expect(buildRerunIssueBody({taskId: "task-1", freshSession: true})).toEqual({task_id: "task-1", fresh_session: true});
  });
});
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/core exec vitest run api/schemas.test.ts api/client.test.ts extensions/schemas.test.ts extensions/api-client.test.ts agents/runtime-binding.test.ts agents/derive-presence.test.ts realtime/use-realtime-sync.test.ts
```

Expected: FAIL because `pool`/`waiting_runtime` and new wire keys are rejected or collapsed.

- [ ] **Step 3: Implement strict types/schemas.** Use discriminated unions keyed by `runtime_policy.mode`; type Agent and RuntimeDevice with different modes; keep a deprecated alias only where required for source compatibility. `packages/core/types/chat.ts` gives pending Chat rows the typed `AgentTaskStatus`, `wait_reason`, `runtime_binding_mode`, and `session_affinity_state` fields used by head-only UI. Invocation uses an explicitly boolean `isAgentRunnable`; Runtime navigation uses bound helper. `rerunIssue` accepts the backward-compatible string or options object and sends `fresh_session` in the actual JSON body; the Server fixed-mode 400 is preserved by the client error path.

```ts
export type RuntimeLocationMode = "local" | "cloud";
export type AgentExecutionMode = RuntimeLocationMode | "pool";

export const isAgentRunnable = (a: Agent) =>
  a.runtime_binding_mode === "pool" ? a.runtime_routable === true : a.runtime_bound === true;

export interface RerunIssueOptions { taskId?: string; freshSession?: boolean }

export function buildRerunIssueBody(input: string | RerunIssueOptions = {}): {task_id?: string; fresh_session?: boolean} {
  const options = typeof input === "string" ? {taskId: input} : input;
  return {
    ...(options.taskId ? {task_id: options.taskId} : {}),
    ...(options.freshSession === true ? {fresh_session: true} : {}),
  };
}

// APIClient method body
async rerunIssue(issueId: string, input: string | RerunIssueOptions = {}): Promise<AgentTask> {
  return this.fetch(`/api/issues/${issueId}/rerun`, {
    method: "POST",
    body: JSON.stringify(buildRerunIssueBody(input)),
  });
}
```

- [ ] **Step 4: Run GREEN/package gates.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/core exec vitest run api/schemas.test.ts api/client.test.ts extensions/schemas.test.ts extensions/api-client.test.ts agents/runtime-binding.test.ts agents/derive-presence.test.ts realtime/use-realtime-sync.test.ts
corepack pnpm --filter @multica/core test
corepack pnpm --filter @multica/core typecheck
corepack pnpm --filter @multica/core lint
```

Expected: all PASS with no `any` escape for routing unions.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add packages/core/types/agent.ts packages/core/types/chat.ts packages/core/types/index.ts packages/core/types/events.ts packages/core/api/schemas.ts packages/core/api/schemas.test.ts packages/core/api/client.ts packages/core/api/client.test.ts packages/core/extensions/types.ts packages/core/extensions/schemas.ts packages/core/extensions/schemas.test.ts packages/core/extensions/api-client.test.ts packages/core/agents/runtime-binding.ts packages/core/agents/runtime-binding.test.ts packages/core/agents/derive-presence.ts packages/core/agents/derive-presence.test.ts packages/core/realtime/use-realtime-sync.ts packages/core/realtime/use-realtime-sync.test.ts
git commit -m "feat(core): model runtime pool routing"
```

---

### Task 16: Enable Pool Squad/Chat entries and waiting UX in Views/Web

**Files:**
- Create: `packages/views/agents/runnable-options.ts`, `packages/views/agents/runnable-options.test.ts`
- Modify: `packages/views/extensions/extensions-page.tsx`, `packages/views/extensions/extensions-page.test.tsx`
- Modify: `packages/views/modals/create-squad.tsx`, `packages/views/modals/create-squad.test.tsx`
- Modify: `packages/views/modals/quick-create-issue.tsx`, `packages/views/modals/quick-create-issue.test.tsx`
- Modify: `packages/views/autopilots/components/pickers/agent-picker.tsx`
- Create: `packages/views/autopilots/components/pickers/agent-picker.runtime-pool.test.tsx`
- Modify: `packages/views/chat/components/chat-window.tsx`
- Create: `packages/views/chat/components/chat-window-runtime-pool.test.tsx`
- Modify: `packages/views/chat/components/task-status-pill.tsx`, `packages/views/chat/components/runtime-required-banner.tsx`, `packages/views/chat/components/new-chat-button.tsx`, `packages/views/chat/components/use-chat-controller.ts`
- Modify: `packages/views/chat/components/task-status-pill.test.ts`, `packages/views/chat/components/chat-new-chat-button.test.tsx`, `packages/views/chat/components/use-chat-controller.test.tsx`
- Modify: `packages/views/issues/components/pickers/assignee-picker.tsx`, `packages/views/issues/components/pickers/assignee-picker.keyboard.test.tsx`
- Modify: `packages/views/editor/extensions/mention-suggestion.tsx`, `packages/views/editor/extensions/mention-suggestion.test.tsx`
- Modify: `packages/views/issues/components/task-status-icon.tsx`, `packages/views/issues/surface/activity.ts`, `packages/views/issues/components/execution-log-section.tsx`, `packages/views/issues/components/comment-card.tsx`
- Create: `packages/views/issues/components/task-status-icon.test.tsx`
- Create: `packages/views/issues/components/execution-log-section.runtime-pool.test.tsx`, `packages/views/issues/components/comment-card.runtime-pool.test.tsx`
- Modify: `packages/views/agents/components/agent-detail-page.tsx`, `packages/views/agents/components/agent-detail-page.test.tsx`, `packages/views/agents/components/agents-page.tsx`, `packages/views/agents/components/agents-page.test.tsx`
- Modify: `packages/views/agents/components/agent-activity-hover-content.tsx`, `packages/views/agents/components/agent-activity-hover-content.test.tsx`
- Modify: `packages/views/locales/en/extensions.json`, `packages/views/locales/zh-Hans/extensions.json`, `packages/views/locales/ja/extensions.json`, `packages/views/locales/ko/extensions.json`
- Modify: `packages/views/locales/en/issues.json`, `packages/views/locales/zh-Hans/issues.json`, `packages/views/locales/ja/issues.json`, `packages/views/locales/ko/issues.json`
- Modify: `packages/views/locales/en/chat.json`, `packages/views/locales/zh-Hans/chat.json`, `packages/views/locales/ja/chat.json`, `packages/views/locales/ko/chat.json`
- Modify: `apps/desktop/src/renderer/src/routes.test.tsx`, `apps/web/app/[workspaceSlug]/(dashboard)/extensions/page.test.tsx`

**Interfaces:**
- Consumes: Core helpers/contracts from Task 15.
- Produces: Pool-runnable Agent/Squad/Chat/Quick Create/Autopilot pickers, Runtime Pool release display, Agent list/detail routing state, waiting reasons/cancel/recovery, four-locale parity, and Runtime links only for bound tasks/Agents.

- [ ] **Step 1: Write RED UI tests.** Assert `create-squad`, floating Chat, Quick Create, and Autopilot include Pool Agents but exclude fixed-unbound; assignee/mention match; extension shows `Runtime Pool / assigned at invocation`; waiting task shows reason, cancel, and fresh-session action for removed/unauthorized without a fake Runtime link. Agent activity hover reads local/cloud/provider from the assigned Task Runtime, shows unknown after deletion, and never presents `pool` as a physical location.

```tsx
import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import { runnableAgents } from "./runnable-options";

describe("runnableAgents", () => {
  it("keeps pool-unbound and drops fixed-unbound agents", () => {
    const pool = {id: "pool", archived_at: null, runtime_binding_mode: "pool", runtime_routable: true} as Agent;
    const fixed = {id: "fixed", archived_at: null, runtime_binding_mode: "fixed", runtime_bound: false} as Agent;
    expect(runnableAgents([fixed, pool]).map((agent) => agent.id)).toEqual(["pool"]);
  });
});
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/views exec vitest run agents/runnable-options.test.ts extensions/extensions-page.test.tsx modals/create-squad.test.tsx modals/quick-create-issue.test.tsx autopilots/components/pickers/agent-picker.runtime-pool.test.tsx chat/components/chat-window-runtime-pool.test.tsx chat/components/task-status-pill.test.ts chat/components/chat-new-chat-button.test.tsx chat/components/use-chat-controller.test.tsx issues/components/pickers/assignee-picker.keyboard.test.tsx editor/extensions/mention-suggestion.test.tsx issues/components/task-status-icon.test.tsx issues/components/execution-log-section.runtime-pool.test.tsx issues/components/comment-card.runtime-pool.test.tsx agents/components/agent-activity-hover-content.test.tsx agents/components/agent-detail-page.test.tsx agents/components/agents-page.test.tsx locales/parity.test.ts
```

Expected: FAIL because filters still check `runtime_id`/`isAgentRuntimeBound` and waiting state has no rendering.

- [ ] **Step 3: Implement invocation gates only.** Use the shared `runnableAgents` helper in pickers/actions and retain `isAgentRuntimeBound` for detail links. Map stable reasons to copy/action; activity hover receives the assigned Task Runtime and never falls back from a Pool Task to `agent.runtime_mode`. Implement one 2-5 minute surface at a time: Squad, Chat, Quick Create, Autopilot, assignee, mention, waiting/recovery, activity hover, Agent pages, Extension, locales, routes. Do not add ordinary-Agent Pool configuration UI.

```tsx
import { isAgentRunnable } from "@multica/core/agents";
import type { Agent } from "@multica/core/types";

export function runnableAgents(agents: Agent[]): Agent[] {
  return agents.filter((agent) => !agent.archived_at && isAgentRunnable(agent));
}
```

- [ ] **Step 4: Run GREEN and Web/desktop type gates.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/views exec vitest run agents/runnable-options.test.ts extensions/extensions-page.test.tsx modals/create-squad.test.tsx modals/quick-create-issue.test.tsx autopilots/components/pickers/agent-picker.runtime-pool.test.tsx chat/components/chat-window-runtime-pool.test.tsx chat/components/task-status-pill.test.ts chat/components/chat-new-chat-button.test.tsx chat/components/use-chat-controller.test.tsx issues/components/pickers/assignee-picker.keyboard.test.tsx editor/extensions/mention-suggestion.test.tsx issues/components/task-status-icon.test.tsx issues/components/execution-log-section.runtime-pool.test.tsx issues/components/comment-card.runtime-pool.test.tsx agents/components/agent-activity-hover-content.test.tsx agents/components/agent-detail-page.test.tsx agents/components/agents-page.test.tsx locales/parity.test.ts
corepack pnpm --filter @multica/views test
corepack pnpm --filter @multica/views typecheck
corepack pnpm --filter @multica/views lint
corepack pnpm --filter @multica/desktop exec vitest run src/renderer/src/routes.test.tsx
corepack pnpm --filter @multica/web exec vitest run 'app/[workspaceSlug]/(dashboard)/extensions/page.test.tsx'
corepack pnpm --filter @multica/web test
corepack pnpm --filter @multica/web typecheck
corepack pnpm --filter @multica/web lint
corepack pnpm --filter @multica/desktop typecheck
corepack pnpm --filter @multica/desktop lint
```

Expected: all PASS; Web and Desktop consume the same Views contract.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add packages/views/agents/runnable-options.ts packages/views/agents/runnable-options.test.ts packages/views/extensions/extensions-page.tsx packages/views/extensions/extensions-page.test.tsx packages/views/modals/create-squad.tsx packages/views/modals/create-squad.test.tsx packages/views/modals/quick-create-issue.tsx packages/views/modals/quick-create-issue.test.tsx packages/views/autopilots/components/pickers/agent-picker.tsx packages/views/autopilots/components/pickers/agent-picker.runtime-pool.test.tsx packages/views/chat/components/chat-window.tsx packages/views/chat/components/chat-window-runtime-pool.test.tsx packages/views/chat/components/task-status-pill.tsx packages/views/chat/components/runtime-required-banner.tsx packages/views/chat/components/new-chat-button.tsx packages/views/chat/components/use-chat-controller.ts packages/views/chat/components/task-status-pill.test.ts packages/views/chat/components/chat-new-chat-button.test.tsx packages/views/chat/components/use-chat-controller.test.tsx packages/views/issues/components/pickers/assignee-picker.tsx packages/views/issues/components/pickers/assignee-picker.keyboard.test.tsx packages/views/editor/extensions/mention-suggestion.tsx packages/views/editor/extensions/mention-suggestion.test.tsx packages/views/issues/components/task-status-icon.tsx packages/views/issues/surface/activity.ts packages/views/issues/components/execution-log-section.tsx packages/views/issues/components/comment-card.tsx packages/views/issues/components/task-status-icon.test.tsx packages/views/issues/components/execution-log-section.runtime-pool.test.tsx packages/views/issues/components/comment-card.runtime-pool.test.tsx packages/views/agents/components/agent-activity-hover-content.tsx packages/views/agents/components/agent-activity-hover-content.test.tsx packages/views/agents/components/agent-detail-page.tsx packages/views/agents/components/agent-detail-page.test.tsx packages/views/agents/components/agents-page.tsx packages/views/agents/components/agents-page.test.tsx packages/views/locales/en/extensions.json packages/views/locales/zh-Hans/extensions.json packages/views/locales/ja/extensions.json packages/views/locales/ko/extensions.json packages/views/locales/en/issues.json packages/views/locales/zh-Hans/issues.json packages/views/locales/ja/issues.json packages/views/locales/ko/issues.json packages/views/locales/en/chat.json packages/views/locales/zh-Hans/chat.json packages/views/locales/ja/chat.json packages/views/locales/ko/chat.json apps/desktop/src/renderer/src/routes.test.tsx 'apps/web/app/[workspaceSlug]/(dashboard)/extensions/page.test.tsx'
git commit -m "feat(views): expose runtime pool execution"
```

---

### Task 17: Enable Pool entries and waiting Realtime on Mobile

**Files:**
- Modify: `apps/mobile/data/schemas.ts`
- Create: `apps/mobile/data/runtime-pool-schema.test.ts`
- Modify: `apps/mobile/lib/is-agent-runtime-bound.ts`
- Create: `apps/mobile/lib/is-agent-runtime-bound.test.ts`
- Modify: `apps/mobile/components/issue/mention-suggestion-bar.tsx`, `apps/mobile/components/issue/pickers/mention-picker-body.tsx`, `apps/mobile/components/issue/pickers/assignee-picker-body.tsx`, `apps/mobile/components/chat/agent-picker-sheet.tsx`
- Create: `apps/mobile/components/issue/mention-suggestion-bar.runtime-pool.test.tsx`, `apps/mobile/components/issue/pickers/mention-picker-body.runtime-pool.test.tsx`, `apps/mobile/components/issue/pickers/assignee-picker-body.runtime-pool.test.tsx`, `apps/mobile/components/chat/agent-picker-sheet.runtime-pool.test.tsx`
- Modify: `apps/mobile/data/realtime/use-chat-session-realtime.ts`, `apps/mobile/data/realtime/use-issue-realtime.ts`, `apps/mobile/data/realtime/use-presence-realtime.ts`
- Modify: `apps/mobile/data/realtime/chat-ws-updaters.ts`, `apps/mobile/data/realtime/chat-ws-updaters.test.ts`, `apps/mobile/data/realtime/issue-ws-updaters.ts`, `apps/mobile/data/realtime/issue-ws-updaters.test.ts`
- Modify: `apps/mobile/components/issue/run-row.tsx`, `apps/mobile/components/chat/status-pill.tsx`
- Create: `apps/mobile/components/issue/run-row-runtime-pool.test.tsx`, `apps/mobile/components/chat/status-pill-runtime-pool.test.tsx`
- Modify: `apps/mobile/app/(app)/[workspace]/issue/[id]/runs.tsx`, `apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx`
- Create: `apps/mobile/data/realtime/runtime-pool-cancel.test.ts`

**Interfaces:**
- Consumes: Core/Server waiting and routability contracts.
- Produces: Mobile Pool-runnable helper, non-collapsing `waiting_runtime` schema, picker parity, waiting/cancel UI, and waiting->queued Realtime updates.

- [ ] **Step 1: Write RED Mobile tests.** Assert `AgentTaskSchema` preserves `waiting_runtime` and reason instead of `.catch("queued")`; Pool Agent is callable/unbound; fixed-unbound blocked; all four pickers use callable semantics; Chat/Issue updaters receive waiting then queued without reporting running.

```ts
import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import { isAgentRuntimeBound, isAgentRuntimeCallable } from "./is-agent-runtime-bound";

describe("mobile runtime routing", () => {
  it("calls pool-unbound agents without claiming they are bound", () => {
    const agent = {runtime_binding_mode: "pool", runtime_routable: true, runtime_bound: false} as Agent;
    expect(isAgentRuntimeCallable(agent)).toBe(true);
    expect(isAgentRuntimeBound(agent)).toBe(false);
  });
});
```

- [ ] **Step 2: Run RED.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/mobile exec vitest run data/runtime-pool-schema.test.ts lib/is-agent-runtime-bound.test.ts components/issue/mention-suggestion-bar.runtime-pool.test.tsx components/issue/pickers/mention-picker-body.runtime-pool.test.tsx components/issue/pickers/assignee-picker-body.runtime-pool.test.tsx components/chat/agent-picker-sheet.runtime-pool.test.tsx data/realtime/chat-ws-updaters.test.ts data/realtime/issue-ws-updaters.test.ts data/realtime/runtime-pool-cancel.test.ts components/issue/run-row-runtime-pool.test.tsx components/chat/status-pill-runtime-pool.test.tsx
```

Expected: FAIL because waiting collapses to queued and Pool Agents fail the bound check.

- [ ] **Step 3: Implement Mobile parity.** Add a callable helper alongside the bound helper; use callable in invocation pickers and bound only for Runtime links. Add the waiting event/status to schemas and both realtime hooks/updaters. Treat `waiting_runtime` as cancellable pending in Issue runs and Chat, render the stable reason instead of "Queued", and keep it out of running/stop semantics.

```ts
import type { Agent } from "@multica/core/types";

export const isAgentRuntimeCallable = (agent: Agent) =>
  agent.runtime_binding_mode === "pool" ? agent.runtime_routable === true : isAgentRuntimeBound(agent) === true;
```

- [ ] **Step 4: Run GREEN/package gates.**

```bash
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/mobile exec vitest run data/runtime-pool-schema.test.ts lib/is-agent-runtime-bound.test.ts components/issue/mention-suggestion-bar.runtime-pool.test.tsx components/issue/pickers/mention-picker-body.runtime-pool.test.tsx components/issue/pickers/assignee-picker-body.runtime-pool.test.tsx components/chat/agent-picker-sheet.runtime-pool.test.tsx data/realtime/chat-ws-updaters.test.ts data/realtime/issue-ws-updaters.test.ts data/realtime/runtime-pool-cancel.test.ts components/issue/run-row-runtime-pool.test.tsx components/chat/status-pill-runtime-pool.test.tsx
corepack pnpm --filter @multica/mobile test
corepack pnpm --filter @multica/mobile typecheck
corepack pnpm --filter @multica/mobile lint
```

Expected: all PASS and no status fallback rewrites waiting to queued.

- [ ] **Step 5: Commit.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add apps/mobile/data/schemas.ts apps/mobile/data/runtime-pool-schema.test.ts apps/mobile/lib/is-agent-runtime-bound.ts apps/mobile/lib/is-agent-runtime-bound.test.ts apps/mobile/components/issue/mention-suggestion-bar.tsx apps/mobile/components/issue/mention-suggestion-bar.runtime-pool.test.tsx apps/mobile/components/issue/pickers/mention-picker-body.tsx apps/mobile/components/issue/pickers/mention-picker-body.runtime-pool.test.tsx apps/mobile/components/issue/pickers/assignee-picker-body.tsx apps/mobile/components/issue/pickers/assignee-picker-body.runtime-pool.test.tsx apps/mobile/components/chat/agent-picker-sheet.tsx apps/mobile/components/chat/agent-picker-sheet.runtime-pool.test.tsx apps/mobile/data/realtime/use-chat-session-realtime.ts apps/mobile/data/realtime/use-issue-realtime.ts apps/mobile/data/realtime/use-presence-realtime.ts apps/mobile/data/realtime/chat-ws-updaters.ts apps/mobile/data/realtime/chat-ws-updaters.test.ts apps/mobile/data/realtime/issue-ws-updaters.ts apps/mobile/data/realtime/issue-ws-updaters.test.ts apps/mobile/components/issue/run-row.tsx apps/mobile/components/chat/status-pill.tsx apps/mobile/components/issue/run-row-runtime-pool.test.tsx apps/mobile/components/chat/status-pill-runtime-pool.test.tsx 'apps/mobile/app/(app)/[workspace]/issue/[id]/runs.tsx' 'apps/mobile/app/(app)/[workspace]/(tabs)/chat.tsx' apps/mobile/data/realtime/runtime-pool-cancel.test.ts
git commit -m "feat(mobile): support runtime pool tasks"
```

---

### Task 18: Prove real two-Daemon Leader-to-member execution and run final gates

**Files:**
- Create: `scripts/runtime-pool-session-acceptance.mjs`, `scripts/runtime-pool-session-acceptance.test.mjs`
- Create: `scripts/runtime-pool-delegating-cli.mjs`, `scripts/runtime-pool-delegating-cli.test.mjs`
- Create: `testdata/extensions/runtime-pool-session-squad.source.json`
- Create: `docs/superpowers/specs/2026-08-09-runtime-pool-session-affinity-acceptance.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: all prior server/client behavior and freshly built `cmd/server`, `cmd/multica`, and `/Users/zxx/Documents/技术学习/platform-agent-cli/cmd/platform-agent-cli` binaries.
- Produces: isolated acceptance evidence from a harness-owned Server, two actual Multica Daemons, and a mock Platform backend adapter while the real CLI remains the executed protocol implementation; no direct member invocation is allowed.

- [ ] **Step 1: Write RED harness tests.** Create an independent fixture with one Leader, two members, and two Skills; assert `runtime_policy=pool` and `runtime=null`. Test a stdio-transparent wrapper that launches the fresh real CLI and delegates each configured member exactly once only for the corresponding Leader `turn/start`. The wrapper must parse the current Issue UUID from the trusted Multica ownership brief in that turn, use task-scoped `MULTICA_SERVER_URL`, `MULTICA_TOKEN`, `MULTICA_AGENT_ID`, and `MULTICA_TASK_ID`, list the Issue comments, and POST `[@Member](mention://agent/<member-id>)\n<!--runtime-pool-e2e:<task-id>:<member-id>-->` only when that exact task marker is absent. Test malformed/missing ownership briefs fail closed before POST, a repeated `turn/start` does not duplicate the comment, profile-derived health-port selection, migrated-DB preflight, temporary profile generation, Server/Daemon readiness, TERM-then-bounded-KILL cleanup, fixture-row cleanup, and temp-HOME removal.

```js
import assert from "node:assert/strict";
import test from "node:test";
import { delegationComment } from "./runtime-pool-delegating-cli.mjs";

test("delegation comment uses the native Agent mention URI", () => {
  assert.equal(
    delegationComment(
      {id: "22222222-2222-4222-8222-222222222222", name: "Reviewer"},
      "11111111-1111-4111-8111-111111111111",
    ),
    "[@Reviewer](mention://agent/22222222-2222-4222-8222-222222222222)\n<!--runtime-pool-e2e:11111111-1111-4111-8111-111111111111:22222222-2222-4222-8222-222222222222-->",
  );
});
```

- [ ] **Step 2: Run RED unit tests.**

```bash
cd /Users/zxx/Documents/技术学习/multica
node --test scripts/runtime-pool-session-acceptance.test.mjs scripts/runtime-pool-delegating-cli.test.mjs
```

Expected: FAIL because the new harness, wrapper, and fixture do not exist.

- [ ] **Step 3: Implement the bounded live harness.** Require only `MULTICA_E2E_DATABASE_URL` plus the three fresh binary paths. Create an isolated Workspace, owner/member identities, revocable tokens, and the Extension fixture in the integration DB. Select a free Server port and spawn the fresh Server with `DATABASE_URL`, `PORT`, `APP_ENV=development`, and a `mkdtemp` HOME. Before starting either Daemon, call the authenticated Extension import endpoint, assert success with zero registered Runtime rows, and verify every imported Agent is `runtime_binding_mode=pool` with `runtime_id=NULL`.

  Create two named profiles under `$HOME/.multica/profiles/<profile>/config.json`. Choose profile suffixes until `healthPortForProfile`'s documented byte-sum mapping yields two distinct available ports; the harness test must implement and lock this mapping against `server/cmd/multica/cmd_daemon.go`. Each config contains exact `server_url`, `workspace_id`, `token`, `device_name`, `runtime_name`, `max_concurrent_tasks:1`, `disable_auto_update:true`, and `disable_auto_reload:true`. Start each foreground Daemon with only supported flags:

  - A argv: `<multica-bin> --profile <owner-profile> daemon start --foreground --no-auto-update --no-auto-reload --daemon-id e2e-owner --max-concurrent-tasks 1`
  - B argv: `<multica-bin> --profile <shared-profile> daemon start --foreground --no-auto-update --no-auto-reload --daemon-id e2e-shared --max-concurrent-tasks 1`

  Give each process a separate `PATH` directory containing an executable named `platform-agent-cli`, and set distinct absolute `MULTICA_WORKSPACES_ROOT` values in the process environments; there are deliberately no `--health-port` or `--workspaces-root` flags on `daemon start`. Set `PLATFORM_AGENT_MODE=mock`; this selects the real CLI's deterministic backend without replacing App Server framing. Runtime A is owner-local/private and its `platform-agent-cli` executable is the transparent wrapper with `RUNTIME_POOL_E2E_REAL_PLATFORM_CLI=<fresh-cli>`; Runtime B is other-owned/public and points directly to the same fresh CLI. The wrapper derives the Issue ID from the current Leader turn, checks the task-ID marker through `GET /api/issues/{issueId}/comments`, and POSTs to `/api/issues/{issueId}/comments` using the task token. It writes no long-lived owner token and never calls a member Task endpoint directly.

  After the pre-Daemon import proof, wait for both profile-derived health ports and registered Runtime rows with normalized capabilities, then invoke the imported Leader. The wrapper posts the native agent-authored mention while Leader still runs on A, so strict-idle excludes A and the member independently selects B. Every readiness/API/DB poll has a 30-second deadline and dumps child stdout/stderr on failure. In `finally`, send TERM and wait 5 seconds, KILL remaining Daemons, then Server; delete fixture rows/tokens in FK-safe order and remove temporary HOME, PATH, and work roots.

Start the wrapper with these copyable helpers, then add stdin/stdout proxying as a separate edit. The exact marker is both the duplicate key in listed Comment content and the HTTP idempotency key; the POST schema is the existing Comment API schema:

```js
const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";

export function parseIssueID(prompt) {
  const matches = [...String(prompt).matchAll(new RegExp(`^Your assigned issue ID is: (${UUID})$`, "gim"))];
  if (matches.length !== 1) throw new Error("trusted ownership brief must contain exactly one Issue UUID");
  return matches[0][1].toLowerCase();
}

export function delegationMarker(taskID, memberID) {
  if (!new RegExp(`^${UUID}$`, "i").test(taskID) || !new RegExp(`^${UUID}$`, "i").test(memberID)) {
    throw new TypeError("task and member IDs must be UUIDs");
  }
  return `runtime-pool-e2e:${taskID.toLowerCase()}:${memberID.toLowerCase()}`;
}

export function delegationComment(member, taskID) {
  if (!member || typeof member.name !== "string") throw new TypeError("member name is required");
  const marker = delegationMarker(taskID, member.id);
  return `[@${member.name}](mention://agent/${member.id})\n<!--${marker}-->`;
}

export async function postDelegationOnce({serverURL, token, prompt, taskID, member, fetchImpl = fetch}) {
  if (!/^https?:\/\//.test(serverURL) || !token) throw new Error("task-scoped server URL and token are required");
  const issueID = parseIssueID(prompt);
  const marker = delegationMarker(taskID, member.id);
  const headers = {authorization: `Bearer ${token}`, "content-type": "application/json"};
  const list = await fetchImpl(`${serverURL}/api/issues/${issueID}/comments`, {headers});
  if (!list.ok) throw new Error(`list comments failed: ${list.status}`);
  const comments = await list.json();
  if (!Array.isArray(comments)) throw new Error("comments response must be an array");
  if (comments.some((comment) => typeof comment?.content === "string" && comment.content.includes(`<!--${marker}-->`))) return false;
  const posted = await fetchImpl(`${serverURL}/api/issues/${issueID}/comments`, {
    method: "POST",
    headers: {...headers, "idempotency-key": marker},
    body: JSON.stringify({content: delegationComment(member, taskID), type: "comment"}),
  });
  if (!posted.ok) throw new Error(`post delegation failed: ${posted.status}`);
  return true;
}
```

Add direct Node tests named `parses one trusted ownership Issue UUID`, `rejects missing or multiple ownership Issue UUIDs`, `lists comments and posts exact comment schema once`, and `existing task/member marker suppresses POST`. The fetch spy must assert the escaped Issue UUID path, task Bearer token, `content-type`, exact `idempotency-key`, mention plus HTML marker body, and exactly zero POSTs on replay.

```text
required live assertions:
import succeeds before Runtime registration; Agents Pool/unbound
Leader Task runs on A and completes with unchanged CLI context
first member Task is created only by the running Leader's comment, linked by delegated_from_task_id/source_task_id/squad_id, and runs on B
Leader follow-up resumes same session/workdir on A even while B is idle
A busy => pinned follow-up queues A; A offline => waiting_runtime; restart A => wake and resume A
a later Leader follow-up delegates the second never-run member through another native comment; that member independently selects authorized B, with no direct member API call
permission/capability loss fails closed; explicit fresh after Runtime removal can select again while history remains
non-platform test Runtime with explicit capability passes allocator integration, but live execution uses Platform adapter
```

- [ ] **Step 4: Run GREEN harness units, build fresh binaries, then LIVE.** The sole external precondition is a migrated integration PostgreSQL URL; the harness owns Server, identity, Runtime/profile, and process cleanup.

```bash
cd /Users/zxx/Documents/技术学习/multica
node --test scripts/runtime-pool-session-acceptance.test.mjs scripts/runtime-pool-delegating-cli.test.mjs
export MULTICA_E2E_DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
export DATABASE_URL="$MULTICA_E2E_DATABASE_URL"
test -n "$MULTICA_E2E_DATABASE_URL"
mkdir -p /private/tmp/multica-runtime-pool-e2e
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -o /private/tmp/multica-runtime-pool-e2e/server ./cmd/server
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -o /private/tmp/multica-runtime-pool-e2e/multica ./cmd/multica
cd /Users/zxx/Documents/技术学习/platform-agent-cli
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -o /private/tmp/multica-runtime-pool-e2e/platform-agent-cli ./cmd/platform-agent-cli
cd /Users/zxx/Documents/技术学习/multica
env RUNTIME_POOL_E2E=1 DATABASE_URL="$DATABASE_URL" MULTICA_E2E_DATABASE_URL="$MULTICA_E2E_DATABASE_URL" MULTICA_E2E_SERVER_BIN=/private/tmp/multica-runtime-pool-e2e/server MULTICA_E2E_MULTICA_BIN=/private/tmp/multica-runtime-pool-e2e/multica MULTICA_E2E_PLATFORM_CLI_BIN=/private/tmp/multica-runtime-pool-e2e/platform-agent-cli node scripts/runtime-pool-session-acceptance.mjs
```

Expected: units PASS; LIVE prints IDs/evidence for imported Release/Squad, Runtime A/B, linked Leader/member Tasks, session/workdir reuse, waiting/wake, permission, removed/fresh, then exits zero with no daemon left running.

- [ ] **Step 5: Run complete repository gates.** No DB-backed suite may be skipped.

```bash
cd /Users/zxx/Documents/技术学习/multica
export DATABASE_URL='postgres://multica:multica@localhost:5432/multica?sslmode=disable'
make migrate-up
cd /Users/zxx/Documents/技术学习/multica/server
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./...
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test -race ./internal/runtimepool ./internal/runtimeaccess ./internal/service ./internal/handler ./cmd/server -count=1
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./...
/Users/zxx/Documents/技术学习/.tools/sqlc-v1.31.1/bin/sqlc generate
git diff --exit-code -- pkg/db/generated
cd /Users/zxx/Documents/技术学习/multica
corepack pnpm --filter @multica/core test
corepack pnpm --filter @multica/core typecheck
corepack pnpm --filter @multica/core lint
corepack pnpm --filter @multica/views test
corepack pnpm --filter @multica/views typecheck
corepack pnpm --filter @multica/views lint
corepack pnpm --filter @multica/web test
corepack pnpm --filter @multica/web typecheck
corepack pnpm --filter @multica/web lint
corepack pnpm --filter @multica/mobile test
corepack pnpm --filter @multica/mobile typecheck
corepack pnpm --filter @multica/mobile lint
corepack pnpm --filter @multica/desktop test
corepack pnpm --filter @multica/desktop typecheck
corepack pnpm --filter @multica/desktop lint
cd /Users/zxx/Documents/技术学习/platform-agent-cli
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go test ./...
/Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go vet ./...
cd /Users/zxx/Documents/技术学习/multica
git diff --check
```

Expected: every command exits zero; a second sqlc generation has no generated diff; migration 279 down test rejects while Pool rows exist.

- [ ] **Step 6: Commit acceptance assets.**

```bash
cd /Users/zxx/Documents/技术学习/multica
git add scripts/runtime-pool-session-acceptance.mjs scripts/runtime-pool-session-acceptance.test.mjs scripts/runtime-pool-delegating-cli.mjs scripts/runtime-pool-delegating-cli.test.mjs testdata/extensions/runtime-pool-session-squad.source.json docs/superpowers/specs/2026-08-09-runtime-pool-session-affinity-acceptance.md README.md
git commit -m "test(e2e): prove runtime pool squad delegation"
```

## Final SDD Review Gate

- [ ] Spec-compliance reviewer maps every requirement in sections 1-14 of the primary Spec to a passing test/task and reports no omitted Server, Squad, Web, or Mobile entry.
- [ ] Code-quality reviewer verifies bounded workspace scans, no Provider-special scheduler branch, no Redis under DB locks, global lock order, Runtime-targeted CAS, durable obligation convergence, and fixed-mode negative controls.
- [ ] Re-run `git diff --check`, `git status --short`, sqlc idempotence, all Task 18 gates, and the LIVE harness after review fixes.
- [ ] Record final commit SHAs, migration range 267-279, LIVE Runtime/Task/Session IDs, and exact Multica versus `platform-agent-cli` change lists in the acceptance document.
