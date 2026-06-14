# Eidetix tools — source-traced contract

The source of truth for these tools is the partner-hosted Eidetix MCP server
(`https://eidetix.nodeops.xyz/mcp/sse`), not this repository. Multica injects
the `eidetix` MCP server into an agent's `mcp_config` at task-claim time when
the issue's project has an enabled Eidetix binding
(`server/internal/handler/eidetix.go` → `applyEidetixToClaim`). The Bearer token
selects the graph; there is no namespace parameter.

## Read tools

| Tool | Contract |
|------|----------|
| `recall` | One-shot cited recall for a topic. Returns a document plus the relevant graph sections. The default entry point. |
| `search` | Narrower query over recorded knowledge. Use when `recall` is thin. |
| `resolve_entities` | Maps names (people, tools, products, campaigns) to canonical graph entities. |
| `get_graph` | Returns the subgraph around an entity. |
| `get_graph_expanded` | Wider neighbourhood expansion from an entity. |
| `get_content` | Returns the full source behind a cited snippet. |

## Write tools

| Tool | Contract |
|------|----------|
| `get_schema` | Returns the trace schema. MUST be called before `ingest_traces`. |
| `ingest_traces` | Persists new observation traces (durable facts) into the graph. |

## Multica-side integration points

- Merge + skill injection: `server/internal/handler/eidetix.go`
  (`applyEidetixToClaim`, `mergeEidetixServer` in `eidetix_merge.go`).
- Per-project binding storage: `eidetix_project_config` table
  (`server/migrations/120_eidetix_project_config.up.sql`).
- Admin config CLI: `multica project eidetix set/show/clear/enable/disable`
  (`server/cmd/multica/cmd_project_eidetix.go`).
