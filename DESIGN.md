# Squad Capability & Route — Design Document

Issue: [#4106](https://github.com/multica-ai/multica/issues/4106)

## Problem

Squads are isolated — each knows its own scope, but no one else does. When a user
doesn't know which squad handles a task, or when Squad A encounters something
outside its expertise, there's no way to answer "which squad handles this?"
programmatically.

## Solution

Two new primitives on the `multica squad` command family:

1. **`squad capability`** — squad leaders declare what their squad can do
2. **`squad route`** — keyword-matching discovery to find the right squad

## Data Model

A single `capability` JSONB column on the existing `squad` table:

```sql
ALTER TABLE squad ADD COLUMN capability JSONB NOT NULL DEFAULT '{}';
```

Stored shape:

```json
{
  "domains": ["strategic_decision", "tech_architecture"],
  "keywords": ["决策分析", "多选项对比", "加权评分", "架构评审"],
  "description": "对多选项进行结构化分析并输出明确推荐"
}
```

Design decisions:

- **JSONB, not normalized tables.** Capability data is read-heavy, write-rare,
  and always consumed as a unit. Normalizing into `squad_capability_domains`,
  `squad_capability_keywords` tables adds JOIN complexity for no benefit at
  this scale (<50 squads per workspace).
- **On the squad row, not a separate table.** Capability is 1:1 with squad —
  it's the squad's public self-description. A separate table would require
  FK + JOIN for every `route` call, adding latency and code paths.
- **Default `'{}'`.** Squads that haven't set capability carry an empty object.
  `route` appends them to a "not yet declared" section at the bottom.

## API Surface

### Capability CRUD

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `PUT` | `/api/squads/{id}/capability` | `SetSquadCapability` | Upsert capability (idempotent) |
| `GET` | `/api/squads/{id}/capability` | `GetSquadCapability` | Read single squad's capability |
| `DELETE` | `/api/squads/{id}/capability` | `DeleteSquadCapability` | Reset to `{}` |
| `GET` | `/api/squads/capabilities` | `ListSquadCapabilities` | All squads' capabilities in workspace |

All endpoints are workspace-scoped (middleware enforces this). Write endpoints
require `owner` or `admin` role — same as squad update/delete.

### Route

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `POST` | `/api/squads/route` | `RouteSquad` | Keyword-match a query against all squad capabilities |

Request body:

```json
{"query": "帮我分析一下转行 AI Infra 应该选哪个方向"}
```

Response: ranked list with scores, plus an "undeclared" section.

## Keyword Matching Algorithm

1. Normalize the query: lowercase, split by whitespace + common CJK delimiters
2. For each squad with a non-empty `capability`:
   - Score += 10 × number of `keywords` that appear in the query
   - Score += 5 × number of `domains` that match substrings in the query
   - Bonus: +3 for each keyword that is a case-insensitive substring match
3. Rank squads by descending score
4. Append an "undeclared" section listing squads with empty capability

This covers 70%+ of matching scenarios for workspaces with <50 squads. No
embedding model, no vector store, zero new dependencies.

## CLI Surface

### `multica squad capability`

```
USAGE
  multica squad capability <command> [flags]

COMMANDS
  set:     Set (or update) a squad's capability
  get:     Get a squad's capability
  list:    List capabilities of all squads in the workspace
  delete:  Clear a squad's capability (reset to empty)
```

Examples:

```bash
multica squad capability set <squad-id> \
  --domains "strategic_decision,tech_architecture" \
  --keywords "决策分析,多选项对比,加权评分,架构评审" \
  --description "对多选项进行结构化分析并输出明确推荐"

multica squad capability get <squad-id>

multica squad capability list

multica squad capability delete <squad-id>
```

### `multica squad route`

```bash
multica squad route "帮我分析一下转行 AI Infra 应该选哪个方向"
```

Output:

```
匹配结果:
  #1  顶层专家团       评分 94  决策分析, AI战略, 技术架构
  #2  后端架构组       评分 52  技术选型, 架构设计
  #3  AI 应用开发组    评分 31  AI工程

推荐: 顶层专家团

以下 squad 尚未声明能力:
  - 测试组
  - 前端组
```

## Implementation Map

```
server/
  migrations/
    120_squad_capability.up.sql      # ADD COLUMN capability JSONB
    120_squad_capability.down.sql    # DROP COLUMN capability
  pkg/db/queries/squad.sql           # +SetSquadCapability, +ListSquadsWithCapability
  pkg/db/generated/
    models.go                         # Squad.Capability []byte (sqlc regenerated)
    squad.sql.go                      # Generated query code
  internal/handler/
    squad_capability.go              # Set/Get/Delete/ListSquadCapability
    squad_route.go                   # RouteSquad + keyword matching engine
  cmd/server/router.go               # Register new routes
  cmd/multica/
    cmd_squad_capability.go          # CLI: squad capability subcommands
    cmd_squad_route.go               # CLI: squad route command
    cmd_squad.go                     # Register capability + route under squadCmd
```

## Progressive Extension Path

Each is an independent, optional follow-up PR:

1. *(this PR)* Capability CRUD + keyword-match route
2. LLM semantic matching for higher accuracy (when demand proven)
3. Auto-generate capability from squad name + leader instructions
4. Hook into issue assignment flow (auto-suggest squad on assign)
5. Route outcome tracking (satisfaction stats)

## Non-Goals (for this PR)

- Embedding-based semantic matching
- Auto-generated capabilities from squad metadata
- Issue assignment integration
- Leader briefing integration
- Any frontend changes
