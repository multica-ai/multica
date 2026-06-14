---
name: multica-eidetix
description: "Use when working any issue whose project is bound to an Eidetix shared-context graph. Teaches the read-before-acting and write-after-deciding loop against the eidetix MCP server so a team of agents shares one cited memory instead of each starting cold."
user-invocable: false
allowed-tools: mcp__eidetix__recall, mcp__eidetix__search, mcp__eidetix__get_graph, mcp__eidetix__get_graph_expanded, mcp__eidetix__get_content, mcp__eidetix__resolve_entities, mcp__eidetix__get_schema, mcp__eidetix__ingest_traces
---

# Eidetix shared context

This project is wired to an Eidetix knowledge graph through the `eidetix` MCP
server. Eidetix is the team's shared, cited memory: decisions, brand and voice
facts, campaign outcomes, and entity relationships that prior agents recorded.
You both **read** from it before acting and **write** to it after you learn
something durable. Treat it as a colleague's notes, not as ground truth — cite
what informs your work, and verify before you rely on a claim.

The `eidetix` tools only exist when the project is bound; if you do not see
them, this project has no shared graph and you work from the issue alone. Never
let an Eidetix call failure block the task — if a call errors, continue without
it.

## Before you act — recall first

1. `recall` the issue's topic in one shot — it returns a cited document plus the
   relevant graph sections. Start here for almost every issue.
2. `search` for narrower phrasings when `recall` is thin or you need a specific
   prior artifact.
3. `resolve_entities` for the people, tools, products, or campaigns named in the
   issue, to get their canonical graph identity before you reason about them.
4. `get_graph` / `get_graph_expanded` to widen from a resolved entity to its
   neighbours; `get_content` to read the full source behind a cited snippet.

Fold what you find into your plan and cite it in your work ("per the brand voice
note in shared context, …"). Do not re-derive what the graph already records.

## After meaningful work — ingest what's durable

When you make a decision, establish a fact, or produce an outcome that the
**next** agent would want to know:

1. Call `get_schema` FIRST — it tells you the trace shape Eidetix expects. Never
   call `ingest_traces` without it.
2. Call `ingest_traces` with the durable facts: decisions and their rationale,
   brand/voice/positioning facts, campaign results, canonical entity facts.

Ingest durable knowledge, not transients. Worth writing: "We standardized CTA
copy to X because Y." Not worth writing: "Ran a draft, will revise." Avoid
duplicating an entry the graph already holds — if `recall`/`search` already
surfaced the fact, do not re-ingest it.

## Boundaries

- The graph is shared across every agent on this project. Write facts, not
  half-finished scratch.
- Do not ingest secrets, credentials, or tokens.
- Eidetix is advisory. If it is unavailable, note that you proceeded without
  shared context and continue.

See `references/eidetix-tools-source-map.md` for the full tool contract.
