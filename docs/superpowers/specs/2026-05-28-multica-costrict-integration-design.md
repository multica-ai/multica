# Multica x costrict-web Integration Design

## Overview

将 Multica（AI-native 任务管理平台）作为独立子系统接入 costrict-web 生态，共享认证、数据库和部分基础设施，通过 Nginx 反向代理统一入口。

## Decision Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Fusion mode | Independent subsystem | Avoids React-vs-SolidJS and Chi-vs-Gin rewrite |
| Auth | Adopt Casdoor OAuth | Single sign-on, reuse costrict-web's JWKS middleware |
| Database | Shared PostgreSQL instance | Natural user data sharing, no sync needed |
| Frontend coexistence | Independent app + Nginx unified entry | Lowest friction, both apps evolve independently |
| Business interop | Agent capability sharing only | Multica agents call costrict-web skills API |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  Nginx (costrict.ai)                  │
│                                                       │
│  /tasks/*        → Multica Frontend  (Next.js :3000) │
│  /tasks/api/*    → Multica Backend   (Go/Chi :8081)  │
│  /tasks/ws       → Multica WebSocket (:8081)          │
│  /*              → costrict Portal   (SolidJS :3001) │
│  /api/*          → costrict API      (Go/Gin :8080)  │
└─────────────────────────────────────────────────────┘
         │                              │
         ▼                              ▼
┌──────────────────┐    HTTP (内部)    ┌──────────────────┐
│  Multica Backend │◄────────────────►│ costrict-web API │
│  (Go + Chi)      │  Agent能力共享    │ (Go + Gin)       │
│  :8081           │                  │  :8080            │
└────────┬─────────┘                  └────────┬─────────┘
         │                                      │
         ▼                                      ▼
    ┌──────────────────────────────────────────────┐
    │      PostgreSQL (shared, pgvector)            │
    │                                              │
    │  Public tables:  users, user_auth_identities  │
    │  Multica tables: multica_issues, multica_*    │
    │  costrict tables: capability_items, repos, .. │
    └──────────────────────────────────────────────┘
         ▲
         │
    ┌────┴─────┐
    │  Casdoor  │ (unified auth)
    │  OAuth    │
    └──────────┘
```

### Key Design Decisions

- Multica backend runs on port 8081 to avoid conflict with costrict-web's 8080.
- Both Go services share one PostgreSQL instance; tables are distinguished by `multica_` prefix.
- Multica's `users` table is deprecated; reads costrict-web's `users` table directly.
- Agent capability sharing uses internal HTTP calls (not through Nginx).

## Authentication Integration

### Backend Auth Middleware Replacement

**Current:** Multica auth middleware validates self-issued JWT.
**Target:** Multica auth middleware validates Casdoor JWKS tokens (same logic as costrict-web's `jwks.go`).

Steps:
1. Port costrict-web's `server/internal/middleware/jwks.go` (JWKS public key cache + JWT verification) into Multica.
2. Multica auth middleware reads the `zgsmAdminToken` cookie.
3. Extract `sub` (subject_id) from JWT, look up user in shared `users` table.
4. Multica's `user_id` type changes from self-owned UUID to Casdoor `subject_id` (TEXT).

### Frontend Login Flow

**Current:** Multica login page → Multica API → self-issued JWT.
**Target:** Multica login → redirect to Casdoor OAuth → costrict-web callback → shared cookie.

Steps:
1. Multica frontend `/login` redirects to costrict-web's `/api/auth/login?redirect_to=/tasks`.
2. After successful login, Casdoor sets `zgsmAdminToken` cookie (domain: `costrict.ai`).
3. Multica frontend auth store reads `zgsmAdminToken` and calls costrict-web's `/api/auth/me` for user info.
4. Logout calls costrict-web's `/api/auth/logout`.

### User Data Unification

- **Deprecate** Multica's `users` table.
- Multica's `issues`, `agents`, etc. reference costrict-web's `users.subject_id` via TEXT foreign keys.
- Migration script maps existing Multica users to Casdoor users (match by email).

## Database Migration Strategy

### Table Naming Convention

```sql
-- costrict-web existing tables (unchanged)
users, user_auth_identities, user_system_roles,
capability_items, capability_registries, repositories, ...

-- Multica new tables (multica_ prefix)
multica_workspaces
multica_workspace_members
multica_issues
multica_issue_comments
multica_agents
multica_inbox_items
multica_labels
multica_milestones
multica_agent_audit_logs
```

### Core Table Adaptation Example

```sql
CREATE TABLE multica_issues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES multica_workspaces(id),
    author_id TEXT NOT NULL,              -- Casdoor subject_id
    assignee_id TEXT,                     -- Casdoor subject_id (human) or agent UUID
    assignee_type VARCHAR(20) DEFAULT 'user', -- 'user' | 'agent'
    title VARCHAR(500) NOT NULL,
    description TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'triage',
    priority INTEGER DEFAULT 0,
    identifier VARCHAR(50) NOT NULL,      -- e.g. "MUL-123"
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

### Migration Phases

1. Convert Multica's sqlc migrations to costrict-web's Goose format with `multica_` prefix.
2. Write data migration script: export from Multica standalone DB, map user_id → subject_id, import to shared DB.
3. Update Multica's sqlc queries to reference new table/column names.

## Agent Capability Sharing

### Integration Pattern

```
┌──────────────────────┐    HTTP (internal)    ┌──────────────────────┐
│  Multica Agent       │ ────────────────────► │  costrict-web API    │
│  Runtime             │                       │                      │
│                      │  GET /api/items/:id   │  Returns skill       │
│  Agent needs to      │  GET /api/items/      │  content + assets    │
│  execute a Skill     │  :id/assets           │                      │
└──────────────────────┘                       └──────────────────────┘
```

### Implementation

1. **Internal API auth:** Multica backend calls costrict-web using `X-Internal-Secret` header (costrict-web's existing internal auth mechanism). No user token needed.
2. **Skill caching:** Multica Agent Runtime caches fetched skill definitions locally (TTL: 5 min) to avoid per-execution cross-service calls.
3. **New Multica endpoints:**
   - `GET /tasks/api/agent-skills` — proxies costrict-web's `/api/items?type=skill`
   - `POST /tasks/api/agent-skills/:id/execute` — entry point for agent skill execution

### Security Boundary

- Agents can only **read** costrict-web skill content (no write access).
- Rate limit: 60 cross-service calls per agent per minute.
- Audit log: all cross-service calls recorded in `multica_agent_audit_logs`.

## Nginx Configuration

```nginx
upstream costrict_portal  { server 127.0.0.1:3001; }
upstream costrict_api     { server 127.0.0.1:8080; }
upstream multica_frontend { server 127.0.0.1:3000; }
upstream multica_api      { server 127.0.0.1:8081; }

server {
    server_name costrict.ai;

    # Multica frontend
    location /tasks {
        proxy_pass http://multica_frontend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # Multica API
    location /tasks/api/ {
        proxy_pass http://multica_api/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_pass_header Set-Cookie;
    }

    # Multica WebSocket (real-time)
    location /tasks/ws {
        proxy_pass http://multica_api/ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # costrict-web API
    location /api/ {
        proxy_pass http://costrict_api/api/;
        proxy_set_header Host $host;
    }

    # costrict-web frontend (default)
    location / {
        proxy_pass http://costrict_portal;
        proxy_set_header Host $host;
    }
}
```

## Multica Frontend Route Prefix

```js
// next.config.js
module.exports = {
  basePath: '/tasks',
}
```

All internal routing auto-prefixed:
- Page routes: `/issues/MUL-123` → `/tasks/issues/MUL-123`
- API calls: `/api/issues` → `/tasks/api/issues`
- Cross-app links: `<a href="/store">` stays absolute (handled by costrict portal)

## Environment Variables

```bash
# Multica backend .env
DATABASE_URL=postgres://user:pass@localhost:5432/costrict   # shared DB
CASDOOR_ENDPOINT=https://casdoor.costrict.ai
CASDOOR_APP_NAME=multica
CASDOOR_ORG_NAME=costrict
PORT=8081
COSTRICT_API_INTERNAL=http://127.0.0.1:8080
COSTRICT_INTERNAL_SECRET=<shared-secret>
```

## Implementation Timeline

| Phase | Scope | Duration | Dependency |
|-------|-------|----------|------------|
| 1: Auth migration | Backend middleware + frontend login/logout | 3 days | None |
| 2: Database migration | Table prefix + sqlc updates + user mapping | 3 days | Phase 1 |
| 3: Nginx + deployment | Reverse proxy + port change + route prefix | 2 days | Phase 1, 2 |
| 4: Agent capability sharing | Internal API + skill cache + audit log | 2 days | Phase 2 |
| 5: Integration testing | E2E auth flow + performance + edge cases | 3 days | Phase 1-4 |
| **Total** | | **~2 weeks** | |

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| Casdoor downtime blocks both systems | JWKS keys cached locally (1h TTL); graceful degradation |
| User ID migration data loss | Dry-run migration on staging; email-based matching with manual review for unmatched |
| Nginx misconfiguration blocks WebSocket | Dedicated `/tasks/ws` location block with upgrade headers |
| Agent cross-service call latency | Local skill cache (5 min TTL); circuit breaker after 3 consecutive failures |
| sqlc migration breaks existing queries | Run all existing Go tests against new schema before deploying |
