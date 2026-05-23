# multica-dag core integration

This fork's goal is an unbounded DAG schema for Multica, informed by:

- the optimized calumalpass DAG core contract,
- Beads/`br`: local-first, dependency-aware issue tracking with SQLite + JSONL and explicit VCS handoff,
- `beads_viewer`/`bv`: deterministic graph sidecar outputs, PageRank/critical-path/cycle analysis, robot-mode APIs for agents,
- mdbase-style typed records and conformance-first promotion.

## Source references

Use OpenSrc for upstream/source navigation and this checkout as the final implementation authority:

```bash
opensrc path multica-ai/multica
opensrc path Dicklesworthstone/beads_viewer
opensrc path Dicklesworthstone/beads_rust
```

Current fork description from GitHub: "A fork of multica that provides an unbounded dag schema, building on the architecture of beads and beads_viewer, as well as mdbase and other infrastructures."

Direct source paths referenced for this slice:

- Multica upstream issue/dependency schema: `server/migrations/001_init.up.sql` (`issue`, `issue_dependency`)
- Multica development rules: `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`
- `beads_viewer` graph/robot architecture: `pkg/analysis/`, `cmd/bv/robot_registry.go`, `tests/e2e/robot_*`
- `br` local-first JSONL/SQLite architecture: `src/storage/schema.rs`, `src/sync/`, `tests/*jsonl*`, `tests/e2e_sync_*`

## Completed implementation

### 1. Schema migration (`server/migrations/108_dag_core.*.sql`)
- `dag_event` — append-only event log with DVT clock
- `dag_record_projection` — materialized record view
- `dag_link_projection` — materialized directed link view
- `dag_fact_projection` — typed facts with deterministic ordering
- `dag_citation_chain_projection` — citation provenance
- `dag_conflict_state` — contradiction tracking
- `dag_schema_dependency` — schema DAG constraints

### 2. Pure Go contract (`server/internal/dagcore`)
- DVT increment/merge/compare
- Event validation (8 operation types)
- Inverse-link detection
- Acyclic schema validation
- Deterministic fact ordering
- Citation-chain validation
- Contradiction detection
- Concurrent single-field write conflict detection

### 3. Graph analysis (`server/internal/daggraph`)
- Cycle detection (DFS-based, deduplicated)
- Topological sort (Kahn's algorithm)
- Critical path computation (longest path to leaf)

### 4. Service layer (`server/internal/service/dag`)
- `AppendEvent()` — validate + persist + project in one transaction
- `DetectConflicts()` — delegate to dagcore
- `ProjectRecord()`, `ProjectLink()`, `ProjectFact()` — upsert projections

### 5. Backfill command (`server/cmd/backfill_dag`)
- One-shot migration from `issue` + `issue_dependency` into DAG event log
- Idempotent via deterministic event IDs
- Fixed: joins `issue` to resolve `workspace_id` for dependencies

### 6. HTTP API (`server/internal/handler/dag`)
- `POST /api/dag/events` — append event
- `GET /api/dag/analysis` — robot-mode deterministic output
  - `cycles`, `topological_order`, `critical_path`
  - `missing_inverse_links`, `node_count`, `edge_count`

### 7. Issue CRUD auto-wiring (`server/internal/handler/issue`)
- `CreateIssue`: emits `OperationCreate` after tx commit
- `UpdateIssue`: emits `OperationUpdate` with changed fields
- `DeleteIssue`: emits `OperationDelete` (tombstone)
- Fire-and-forget goroutine — never blocks HTTP response

### 8. E2E tests (`e2e/dag-api.spec.ts`)
- 4 Playwright tests pass against live backend
- DAG analysis, event append valid/invalid, analysis reflects records

## Contract mapping

| Optimized core concept | Multica integration point |
| --- | --- |
| append-only event | `dag_event` |
| record projection | `dag_record_projection` |
| link projection | `dag_link_projection` |
| fact projection | `dag_fact_projection` |
| citation chain | `dag_citation_chain_projection` |
| conflict state | `dag_conflict_state` |
| schema DAG | `dag_schema_dependency` + `dagcore.ValidateAcyclicSchemas` |
| DVT causality | `dag_event.dvt` + `dagcore.Compare` |

## Beads / bv influence

The dependency graph remains agent-legible and deterministic:

- graph projections exposed via `GET /api/dag/analysis` rather than requiring LLM graph traversal;
- cycle/critical-path analysis is deterministic and robot-mode ready;
- append-only event log preserves auditability;
- VCS/local-first exports remain explicit if JSONL export is added later.

## Future work (next PR)

1. **Frontend integration**: surface DAG analysis in UI (cycle warnings, dependency graph visualization)
2. **Dependency link/unlink API**: when explicit dependency management endpoints are added, wire them to `OperationLink` / `OperationUnlink`
3. **Robot-triage CLI**: if a dedicated CLI is needed, wrap `GET /api/dag/analysis` into a terminal command
4. **Conformance gate**: cross-writer conflict matrix for Obsidian plugin, CLI, TUI, Android, browser extension
