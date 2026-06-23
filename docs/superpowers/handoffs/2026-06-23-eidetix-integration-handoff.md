# Eidetix Integration — Team Handoff

> Regenerated 2026-06-23. The original handoff lived in `/tmp` and was lost on a
> reboot; this copy is version-controlled so it survives. Reference-first: the
> authoritative detail lives in the linked specs/runbooks, not here.

## What this is

Eidetix is a partner-hosted knowledge-graph MCP (SSE/streamable-http at
`https://eidetix.nodeops.xyz/mcp/sse`, Bearer-token auth where **the token selects
the graph**). This branch integrates it into Multica so agents get shared context,
v0-scoped to marketing workflows.

Branch: `feat/eidetix-context-integration` — **28 commits, all local, not pushed, no PR.**
Cleanly ahead of `main` (main fully contained, no rebase needed).

## Three work streams

### 1. Backend integration — COMPLETE, committed
- Migration `120_eidetix_project_config` + sqlc queries (sticky COALESCE upsert:
  token rotation preserves label/endpoint).
- `MULTICA_EIDETIX_SECRET_KEY` secretbox (AES-256-GCM), independent of Lark key.
- Owner/admin REST handlers: `/api/projects/{id}/eidetix` GET/PUT/PATCH/DELETE.
- CLI: `multica project eidetix set/show/clear/enable/disable` (`--token-stdin`).
- `multica-eidetix` loop skill in its own embed (NOT in `BuiltinSkills()`).
- `applyEidetixToClaim` merges the server entry at the task-claim chokepoint
  (`server/internal/handler/daemon.go`). Fail-open everywhere.
- **Critical fix (d971c3e50):** `buildEidetixServerEntry` emits BOTH
  `type:"http"` (Claude Code's `--mcp-config` requirement) AND
  `transport:"streamable-http"` (OpenClaw). Without `type:"http"` Claude Code
  silently drops the remote server — caught only by the live smoke test.
- Design: `docs/superpowers/specs/2026-06-13-eidetix-context-design.md`
- Plan: `docs/superpowers/plans/2026-06-14-eidetix-context-integration.md`

### 2. Frontend config panel — COMPLETE, committed, NOT click-tested live
- Owner/admin-only collapsible "Eidetix" section in the project sidebar
  (`packages/views/projects/components/eidetix-section.tsx`, wired into
  `project-detail.tsx` after Resources).
- Core: `EidetixConfigSchema` + `EMPTY_EIDETIX_CONFIG`, 4 ApiClient methods
  (token write-only — only sent in PUT body, never fetched/rendered, proven by a
  `.loose()`-passthrough test), `projectEidetixOptions`, optimistic toggle mutation.
- i18n in all 4 locales (en/zh-Hans/ja/ko — parity test enforces 4).
- Plan deviation that recurs: `useCurrentMember` had to be added to the
  `@multica/core/permissions` barrel.
- Design: `docs/superpowers/specs/2026-06-16-eidetix-frontend-design.md`
- Plan: `docs/superpowers/plans/2026-06-16-eidetix-frontend.md`

### 3. Eval — RAN (36 runs, real Marketing graph, Claude Code)
Results: `docs/superpowers/specs/2026-06-16-eidetix-eval-results.md`. Harness: `evals/eidetix/`.
- WITH vs WITHOUT: quality **4.61 vs 3.67**, grounding **0.85 vs 0.47**.
- Control task t6 = **0 lift** (integrity check passed — eval isn't just rewarding verbosity).
- Signature win t3 (contamination rewrite): grounding **0.97 vs 0.03** — the agent
  cannot infer the NodeOps banned-term list without the graph.
- Pairwise = **tie**. Traced and understood: grounding judge rewards brand-fidelity
  (WITH uses canonical assets verbatim); pairwise "which is punchier" rewards
  free-form craft (WITHOUT goes off-brand and wins head-to-head). **Eidetix
  optimizes fidelity/grounding/consistency, NOT creative punch.** Grounding is the
  right metric for the stated goal; pairwise is the wrong yardstick at n=6.
- Perf cost: WITH ~37% slower (+45s median), output +58%, cache-read +91%,
  cache-write ~2×, turns +75% — overhead dominated by recall-loop cache traffic.
- Skill-fix follow-up (v2 subset): the "post clean deliverable, keep recall
  internal" fix cut WITH output bloat ~55% but the "flips pairwise" hypothesis was
  FALSIFIED (0.556→0.0). Kept the skill fix for the token win; documented honestly.

## Open items (in recommended order)

1. **Push + PR.** Branch is local-only. Decide push target (origin = `multica-ai/multica`
   production per global git rules; or `naman485` fork per past workflow). No PR exists.
2. **Live UI smoke.** Click-test the config panel. Blocked in dev: Next 16 dev
   `/api`+`/auth` rewrite to `localhost:8080` hangs (30s timeout) while the backend
   works directly. Web auth needs a verify-code JWT + `multica_logged_in` cookie —
   a raw `mul_` PAT / localStorage token is not enough. This is an env/dev-proxy
   issue, not a feature defect.
3. **Provider-transport verification** (manual). Codex/Hermes/Gemini/kiro/kimi
   remote-MCP support. Runbook: `docs/superpowers/runbooks/2026-06-14-eidetix-provider-verification.md`.
   Only Claude Code + OpenClaw are known-good. A stdio-only backend rejects the entry.
4. **`ingest_traces` write-path.** Eval exercised only the read side. Measure the
   write path (needs the partner trace schema — call `get_schema` first).
5. **Concurrency-cap-4 production note.** One token per project is shared across all
   agents on the project. >4 concurrent marketing agents → the 5th+ Eidetix session
   hits the partner's concurrency cap and fails open (task doesn't break; that call
   just gets no shared memory). Revisit post-v0 if >4 concurrent becomes routine.

## Gotchas

- **sqlc is pinned to v1.31.1.** A different local version rewrites the version
  header on all ~40 generated files and CI fails on drift. Install v1.31.1 before `make sqlc`.
- **Handler tests gate on `DATABASE_URL`** (`postgres://multica:multica@localhost:5432/multica?sslmode=disable`).
  TestMain `os.Exit(0)`s when the DB is down, so a "skip" can masquerade as a pass.
  Always confirm real PASS lines.
- A stale Docker `multica-backend-1` (weeks-old image, no eidetix code) will grab
  `:8080` and silently shadow the branch server — eidetix routes 404. `docker stop multica-backend-1`
  before running the branch server (both share `multica-postgres-1`).

## Secrets handling (hard constraint)

- Eidetix token values are secrets. NEVER write them to specs, git, memory, logs,
  or any read-exposed path (including Bash command lines). Only the encrypted DB
  column and the gitignored `.env` (`MULTICA_EIDETIX_MARKETING_TOKEN`). Pipe via
  stdin / read from `.env`, never echo.
- `MULTICA_EIDETIX_SECRET_KEY` lives only in the gitignored `.env`. Production
  needs it set as a CreateOS secret before the merged code can decrypt configs.

## Eidetix interface (non-secret)

- 8 tools. Read: `recall`, `search`, `get_graph`, `get_graph_expanded`,
  `get_content`, `resolve_entities`. Write: `get_schema` (call before ingest),
  `ingest_traces`.
- Partner-confirmed limits (2026-06-15): no rate limits; concurrency cap = 4;
  latency <1s/req. Marketing token has all-8-tool (read+write) scope.

## Local scratch state (ephemeral — may be gone)

Throwaway local account `eidetix-eval@local.test`, workspace `de9acf8a` (slug
`eidetix-eval`), project `28944d30`, agent `912b3f36`, migration 120. The
`/tmp/eidetix-eval/` scratch (creds, run logs) does NOT survive reboot — only
`venv/` remained as of 2026-06-23. The branch DB (`multica-postgres-1`) persists.
