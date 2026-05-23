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

Current fork description from GitHub: “A fork of multica that provides an unbounded dag schema, building on the architecture of beads and beads_viewer, as well as mdbase and other infrastructures.”

Direct source paths referenced for this slice:

- Multica upstream issue/dependency schema: `server/migrations/001_init.up.sql` (`issue`, `issue_dependency`)
- Multica development rules: `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`
- `beads_viewer` graph/robot architecture: `pkg/analysis/`, `cmd/bv/robot_registry.go`, `tests/e2e/robot_*`
- `br` local-first JSONL/SQLite architecture: `src/storage/schema.rs`, `src/sync/`, `tests/*jsonl*`, `tests/e2e_sync_*`

## Initial implementation slice

The first slice is deliberately small and non-invasive:

1. `server/migrations/108_dag_core.*.sql` adds an append-only DAG event substrate and projection tables.
2. `server/internal/dagcore` adds pure Go contract logic for:
   - DVT increment/merge/compare,
   - event validation,
   - inverse-link detection,
   - acyclic schema validation,
   - deterministic fact ordering,
   - grounded contradiction detection.

This does **not** replace Multica issues or task queues yet. Product tables can project into `dag_event` later once conformance tests prove the semantics.

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

The dependency graph must remain agent-legible and deterministic:

- use graph projections rather than asking agents to infer dependencies from raw rows;
- expose cycle/critical-path/PageRank style analysis in a later robot API instead of relying on LLM graph traversal;
- keep VCS/local-first exports explicit if JSONL export is added; do not auto-run git;
- preserve `.beads` lessons: SQLite/Postgres for query speed, JSONL-like append logs for collaboration/auditability, robot-mode outputs for agents.

## Next implementation steps

1. Add repository/service methods to append DAG events transactionally.
2. Backfill/project existing `issue` + `issue_dependency` rows into DAG records/links.
3. Add graph analysis APIs for cycle detection and dependency-aware planning.
4. Add agent-facing CLI/API output shaped like `bv --robot-triage`: concise, deterministic, status-tagged graph insights.
5. Only then connect DAG state to UI flows.
