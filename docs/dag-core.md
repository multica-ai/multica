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

Tracked in workspace `pex` (DAG Core Architecture project `c1c11059-5712-47eb-b6b8-fc5fd9b23be1`):

### Issues

| Stream | Issue | ID | Status | Assignee |
|--------|-------|-----|--------|----------|
| **Parent Tracker** | DAG Core Deferred Work Tracker | `873a848e-529e-4bd0-86de-d2f715ecf2a4` | in_progress | — |
| **Frontend Viz** | DAG Cycle Warning Component | `3c093d68-66f6-4431-ae06-96319d439c89` | todo | dag-frontend-viz |
| **Frontend Viz** | Dependency Graph Visualization | `4f9e4a06-bcf3-4f5a-9133-c466f9fe396c` | todo | dag-frontend-viz |
| **Frontend Viz** | Topological Order Display | `cb3ea0e4-380b-4745-a56e-73965ea140a0` | todo | dag-frontend-viz |
| **Dependency API** | Issue Dependency Link Endpoint | `822a8edd-3518-4c7b-be52-38e40c3ed611` | todo | dag-dependency-linker |
| **Dependency API** | Issue Dependency Unlink Endpoint | `8aafa260-b9e1-4871-b3cf-b5cedbd8dcc7` | todo | dag-dependency-linker |
| **Dependency API** | Dependency Auto-Wiring on Issue CRUD | `dca82f53-8d7e-4243-8717-d3c013084cf3` | todo | dag-dependency-linker |
| **Robot CLI** | Robot-Triage Command | `f9b1bacb-3738-4831-aed8-b933a76cbe90` | todo | dag-robot-cli |
| **Robot CLI** | DAG Event Append Command | `03828e8f-8491-46ef-9c13-26ca1d1d51ac` | todo | dag-robot-cli |
| **Conformance** | Cross-Writer Conflict Matrix | `836c101c-2d26-47dc-9d78-f758655f75b7` | todo | dag-conformance-guard |
| **Conformance** | Idempotent Update Tests | `228ac5a8-34bd-4c81-b9b9-7077dbc7796c` | todo | dag-conformance-guard |
| **Conformance** | Replay Determinism Verification | `9c5d2e21-2b28-4799-b71e-0b16f0399fab` | todo | dag-conformance-guard |
| **Schema** | dag_schema_dependency Enforcement | `d5327983-0238-45d4-840e-573773091db9` | todo | dag-schema-validator |
| **Schema** | Acyclic Validation API | `b25dbb3f-5ce1-46e0-b042-9dccde99ad03` | todo | dag-schema-validator |

### Agents & Squads

| Agent | Squad | Skill |
|-------|-------|-------|
| `dag-frontend-viz` (e3719959...) | Frontend DAG Squad (d8643887...) | dag-frontend-rendering |
| `dag-dependency-linker` (01adc5b0...) | Dependency API Squad (5932f17e...) | dag-event-projection |
| `dag-robot-cli` (d8de2912...) | Robot CLI Squad (54c06f17...) | dag-robot-triage |
| `dag-conformance-guard` (ff89d28a...) | Conformance Gate Squad (26c936ab...) | dag-conformance-testing |
| `dag-schema-validator` (dbc83423...) | Schema Validation Squad (ab5d656e...) | dag-schema-validation |
| `dag-graph-renderer` (a2f23a10...) | Graph Rendering Squad (3962128f...) | dag-graph-analysis |

### Autopilots

| Autopilot | Agent | Project |
|-----------|-------|---------|
| dag-frontend-viz-autopilot (5ef92acd...) | dag-frontend-viz | DAG Core Architecture |
| dag-dependency-api-autopilot (3d0ed381...) | dag-dependency-linker | DAG Core Architecture |
| dag-robot-cli-autopilot (b143b809...) | dag-robot-cli | DAG Core Architecture |
| dag-conformance-autopilot (c3103ba7...) | dag-conformance-guard | DAG Monitoring & Constraints |
| dag-schema-monitor-autopilot (a5f7de0d...) | dag-schema-validator | DAG Monitoring & Constraints |
| dag-graph-render-autopilot (ffcb2cfd...) | dag-graph-renderer | DAG Monitoring & Constraints |

### Work streams

1. **Frontend integration**: surface DAG analysis in UI (cycle warnings, dependency graph visualization)
2. **Dependency link/unlink API**: when explicit dependency management endpoints are added, wire them to `OperationLink` / `OperationUnlink`
3. **Robot-triage CLI**: wrap `GET /api/dag/analysis` into a terminal command
4. **Conformance gate**: cross-writer conflict matrix for Obsidian plugin, CLI, TUI, Android, browser extension
5. **Schema enforcement**: runtime validation of `dag_schema_dependency` constraints
