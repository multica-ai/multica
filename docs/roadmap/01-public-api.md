# Feature 1: Public API + MCP Server

## Problem

Multica har et fuldt internt REST API (173 routes, JWT-auth), men det er ikke dokumenteret,
har ingen API-key auth, og er ikke tænkt som public. Der er ingen MCP-server — agenter
interagerer med platformen via CLI-kald til Bash (ineffektivt, token-tungt, fejlbehæftet).

## What Exists Today

- 173 REST routes i `server/cmd/server/router.go`
- Konsistente JSON response structs i `server/internal/handler/`
- JWT-auth + PAT-system (`mul_*` tokens) allerede implementeret
- CLI wrapper der kalder API'et over HTTP (`server/internal/cli/`)
- Agent-kontekst hentes via `multica issue get` → Bash tool → JSON parse

## Scope

### Phase 1: Public API (1-2 uger)

- **OpenAPI spec** — auto-generér fra router + response structs, eller skriv manuelt
- **API-key auth** — PAT-systemet virker allerede (`mul_*` tokens), tilføj rate limiting
- **Versionering** — prefix routes med `/api/v1/` (eller behold eksisterende `/api/` da det er v1)
- **Docs** — hosted API docs (Scalar, Swagger UI, eller Fumadocs-integration)
- **CORS** — allerede implementeret, tilføj wildcard-support for public API

### Phase 2: MCP Server (1 uge)

Ny MCP server der eksponerer Multica-operationer som tools:

```
Tools:
- multica_issue_get(id) → issue details
- multica_issue_list(filters) → issue list
- multica_issue_create(title, description, ...) → new issue
- multica_issue_update(id, fields) → updated issue
- multica_issue_comment_add(issue_id, content) → new comment
- multica_issue_comment_list(issue_id) → comments
- multica_issue_status(id, status) → updated status
- multica_workspace_get() → workspace details
- multica_agent_list() → agents
```

Implementation:
- Tynd wrapper over eksisterende handler-funktioner
- Auth via PAT token (env var eller MCP config)
- Kan køres som standalone process eller integreres i serveren

### Impact

- Agenter bruger MCP tools i stedet for Bash → CLI → HTTP → JSON parse
- Færre tokens per operation (tool_use vs. Bash output parsing)
- Mere reliable (structured responses vs. stdout parsing)
- Tredjeparts-integrationer mulige (Zapier, n8n, custom workflows)

## Architecture Notes

- API structs eksisterer allerede — `IssueResponse`, `CommentResponse`, etc.
- PAT-auth middleware eksisterer (`server/internal/middleware/daemon_auth.go`)
- MCP-serveren kan genbruge `handler.Handler` methods direkte
- Ingen breaking changes til eksisterende frontend/CLI

## Files to Modify

- `server/cmd/server/router.go` — rate limiting middleware
- `server/internal/handler/` — OpenAPI annotations
- Ny: `server/cmd/mcp/` — MCP server binary
- Ny: `docs/api/` — API documentation
