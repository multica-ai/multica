# Multica x costrict-web 集成改动总结

**分支**: `feat/costrict-integration`  
**提交数**: 18 commits  
**改动规模**: 260 files changed, 9943 insertions(+), 3934 deletions(-)  
**日期**: 2026-05-28

---

## 概述

将 Multica 作为独立子系统接入 costrict-web，实现：
- **统一认证**: 共享 Casdoor OAuth，替换 Multica 原有的自签发 HMAC JWT
- **共享数据库**: 同一 PostgreSQL 实例，通过 `multica_` 表名前缀隔离
- **统一入口**: Nginx 反向代理，Multica 前端部署在 `/tasks/*` 路径下
- **能力共享**: Multica Agent 可调用 costrict-web 的 Skill 市场获取技能

---

## 一、Casdoor 统一认证接入

### 1.1 JWKS Provider
**文件**: `server/internal/auth/jwks.go` + `jwks_test.go`

从 Casdoor 的 `/.well-known/jwks` 端点拉取并缓存 RSA 公钥，用于验证 JWT 签名。

**特性**:
- 5 分钟限频刷新（`minRefresh`），防止频繁请求
- `io.LimitReader` 限制响应体大小（1MB 上限），防止内存耗尽
- `RWMutex` 读写锁保护缓存，`lastFetch` 在请求前设置防止雷群效应
- 自动过滤非 RSA 和非 RS256 的密钥
- `Preload()` 启动时预加载，失败时降级（不阻塞服务启动）

**测试**: 3 个测试全部通过
- `TestJWKSProvider_FetchesAndCachesKeys`: 验证缓存命中不重复请求
- `TestJWKSProvider_UnknownKidTriggersRefresh`: 未知 kid 触发刷新
- `TestJWKSProvider_MinRefreshInterval`: 限频刷新机制

### 1.2 Casdoor JWT 解析器
**文件**: `server/internal/auth/casdoor.go` + `casdoor_test.go`

严格模式解析 Casdoor 签发的 JWT，提取用户信息。

**安全策略**:
- 仅接受 RS256 算法（`jwt.WithValidMethods([]string{"RS256"})`），防止算法混淆攻击
- 必须有过期时间（`jwt.WithExpirationRequired()`）
- 必须有 `kid` header 且能通过 JWKS 解析到对应公钥
- 必须有非空 `sub` claim（Casdoor 用户 ID）

**输出**: `CasdoorUserInfo{SubjectID, Name, PreferredUsername, Email, Phone}`

**测试**: 3 个测试全部通过
- `TestParseCasdoorJWT_ValidToken`: 正常 token 解析
- `TestParseCasdoorJWT_ExpiredToken`: 过期 token 拒绝
- `TestParseCasdoorJWT_WrongAlgorithm`: 错误算法拒绝（如 HS256）

### 1.3 subject_id 数据库列
**文件**: `server/migrations/113_add_subject_id.up.sql` + `.down.sql`

```sql
ALTER TABLE "user" ADD COLUMN subject_id TEXT UNIQUE;
CREATE INDEX idx_user_subject_id ON "user"(subject_id);
```

**设计**:
- `TEXT UNIQUE` 类型，可空（`NULL`）
- PostgreSQL 将多个 `NULL` 视为不同值，不影响非 Casdoor 用户
- 用于 Casdoor 身份（`sub` claim）到 Multica UUID 的映射
- `.down.sql` 使用 `DROP COLUMN IF EXISTS`，安全回滚

### 1.4 Casdoor Auth 中间件
**文件**: `server/internal/middleware/auth_casdoor.go` + `auth_casdoor_test.go`

Chi router 中间件，处理 Casdoor JWT 认证。

**认证流程**:
1. 读取 `zgsmAdminToken` cookie（Casdoor 默认 cookie 名）
2. 无 cookie 时 fallback 读 `Authorization: Bearer <token>` header
3. PAT token（`mul_` 前缀）直接放行，不走 Casdoor 验证
4. 调用 `ParseCasdoorJWT` 验证 JWT
5. 查询 `subject_id` 找到对应的 Multica 用户
6. 首次登录自动创建用户（`subjectResolver`）
7. 设置 `X-User-ID` 和 `X-Subject-ID` header 供下游使用

**测试**: 3 个测试全部通过
- `TestCasdoorAuth_ValidCookie`: 有效 cookie 通过
- `TestCasdoorAuth_NoCookie`: 无 cookie 返回 401
- `TestCasdoorAuth_PATPassthrough`: PAT token 直接放行

### 1.5 Router 集成
**文件**: `server/cmd/server/router.go` + `main.go`

**改动**:
- `RouterOptions` 新增字段：`JWKSProvider`、`SubjectResolver`、`CasdoorEnabled`、`SkillProxy`
- `CasdoorAuth` 中间件叠在 `Auth` 之前，两者在同一 `r.Group` 内
- 新增路由：
  - `GET /auth/casdoor/login`: 重定向到 Casdoor OAuth 登录页
  - `GET /auth/casdoor/callback`: OAuth 回调（当前为 501 stub）
- `main.go` 中初始化 JWKS、SubjectResolver、SkillProxy，根据环境变量决定是否启用

**兼容性**:
- 当 `CASDOOR_ENDPOINT` 未设置时，Casdoor 模式不启用，原有 HMAC JWT 认证继续工作
- PAT token 始终可用，不受 Casdoor 影响

### 1.6 前端 SSO 登录模式
**文件**: `packages/views/auth/login-page.tsx`

**改动**:
- `LoginPageProps` 新增：
  - `casdoorEnabled?: boolean`: 是否启用 SSO 模式
  - `casdoorLoginUrl?: string`: SSO 登录 URL（如 `/auth/casdoor/login`）
- 当 `casdoorEnabled && casdoorLoginUrl` 为真时：
  - 隐藏邮箱/验证码登录表单
  - 显示单个 "Sign in with SSO" 按钮
  - 点击后跳转到 `casdoorLoginUrl`
- 保留原有 email/code/Google 登录流程，通过 props 控制显示

**测试**: 7 个测试全部通过（包括 SSO 模式）

### 1.7 API Client CSRF 兼容
**文件**: `packages/core/api/client.ts`

**改动**:
```typescript
function readCsrfToken(): string | null {
  return getCookie("multica_csrf") ?? getCookie("zgsm_csrf");
}
```

**原因**: Casdoor 登录时设置的 CSRF cookie 名为 `zgsm_csrf`（costrict-web 使用），而 Multica 原有为 `multica_csrf`。优先读 Multica 的，fallback 读 costrict 的，处理共享 Casdoor session 的场景。

### 1.8 Auth Store Casdoor 模式
**文件**: `packages/core/auth/store.ts` + `auth-initializer.tsx` + `platform/types.ts` + `platform/core-provider.tsx`

**改动**:
- `CoreProviderProps` 新增 `casdoorMode?: boolean`
- `auth-initializer.tsx` 新增 Casdoor 初始化分支：
  - 调用 `api.getMe()` 验证当前 session
  - 成功后调用 `api.listWorkspaces()` 获取工作空间列表
  - 不依赖 localStorage（Casdoor 模式下 session 由 cookie 管理）
- `auth/store.ts` 的 `logout` 方法在 Casdoor 模式下调用 `/auth/casdoor/logout`

---

## 二、数据库共享（表名前缀）

### 2.1 53 张表加 `multica_` 前缀
**文件**: `server/migrations/114_rename_tables_multica_prefix.up.sql` + `.down.sql`

**改动**:
```sql
ALTER TABLE workspace RENAME TO multica_workspace;
ALTER TABLE issue RENAME TO multica_issue;
ALTER TABLE user RENAME TO multica_user;
-- ... 共 53 张表
```

**说明**:
- PostgreSQL 自动更新外键约束、索引、序列的引用，无需手动修改
- `.down.sql` 提供完整的反向操作（`ALTER TABLE multica_* RENAME TO *`）
- 迁移是可逆的，可以安全回滚

**注意事项**:
- 计划中提到 53 张表，实际迁移包含约 45 个 `ALTER TABLE` 语句（部分表在计划编写后已删除）
- 建议迁移后运行：`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name NOT LIKE 'multica_%'` 确认无遗漏

### 2.2 Agent 审计日志表
**文件**: `server/migrations/115_create_agent_audit_logs.up.sql` + `.down.sql`

```sql
CREATE TABLE multica_agent_audit_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id UUID NOT NULL REFERENCES multica_agent(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  status_code INT,
  error_msg TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_audit_logs_agent_id ON multica_agent_audit_logs(agent_id);
CREATE INDEX idx_agent_audit_logs_created_at ON multica_agent_audit_logs(created_at DESC);
```

**用途**: 记录 Agent 调用 costrict-web Skill API 的审计日志，包括：
- `action`: 操作类型（如 `fetch_skill`、`list_skills`）
- `target_type` + `target_id`: 目标资源（如 `skill:skill-123`）
- `status_code`: HTTP 状态码
- `error_msg`: 错误信息（如有）

### 2.3 sqlc 查询文件更新
**文件**: `server/pkg/db/queries/*.sql`（34 个文件）

**改动**: 所有 SQL 查询中的表名引用更新为 `multica_` 前缀。

**示例**:
```sql
-- 修改前
SELECT * FROM workspace WHERE id = $1;

-- 修改后
SELECT * FROM multica_workspace WHERE id = $1;
```

**新增查询**:
- `user.sql`: `GetUserBySubjectID`、`SetUserSubjectID`
- `agent_audit.sql`: `CreateAgentAuditLog`、`ListAgentAuditLogs`、`CountRecentAgentCalls`、`PruneOldAuditLogs`

### 2.4 sqlc 重新生成
**文件**: `server/pkg/db/generated/`

**改动**: 运行 `make sqlc` 重新生成代码，所有类型名变为 `db.MulticaWorkspace`、`db.MulticaIssue`、`db.MulticaUser` 等。

**影响**: 70+ Go 源文件需要适配新的类型名。

### 2.5 Go 源文件适配
**文件**: `server/` 全局（70+ 文件）

**改动**: 将所有 `db.Workspace`、`db.Issue`、`db.User` 等类型引用更新为 `db.MulticaWorkspace`、`db.MulticaIssue`、`db.MulticaUser`。

**示例**:
```go
// 修改前
func (h *Handler) getWorkspace(ctx context.Context, id string) (*db.Workspace, error) {
    return h.queries.GetWorkspace(ctx, id)
}

// 修改后
func (h *Handler) getWorkspace(ctx context.Context, id string) (*db.MulticaWorkspace, error) {
    return h.queries.GetMulticaWorkspace(ctx, id)
}
```

### 2.6 测试文件修复
**文件**: `server/` 全局（54 个测试文件，984 行改动）

**改动**: 修复测试中硬编码的 SQL 表名。

**示例**:
```go
// 修改前
pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, testSlug)

// 修改后
pool.Exec(ctx, `DELETE FROM multica_workspace WHERE slug = $1`, testSlug)
```

**注意事项**:
- 测试断言中的错误消息字符串不应修改（如 `"update agent avatar"` 不应改为 `"update multica_agent avatar"`）
- 仅修改 SQL 语句中的表名引用

---

## 三、Nginx 统一入口 + basePath

### 3.1 Nginx 反向代理配置
**文件**: `deploy/nginx/costrict.conf`

**配置**:
```nginx
upstream multica_ws {
    server 127.0.0.1:8081;
}

upstream multica_api {
    server 127.0.0.1:8081;
}

upstream multica_frontend {
    server 127.0.0.1:3000;
}

upstream costrict_ws {
    server 127.0.0.1:8000;
}

upstream costrict_api {
    server 127.0.0.1:8000;
}

upstream costrict_portal {
    server 127.0.0.1:5173;
}

map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name costrict.ai;

    # Multica WebSocket
    location /tasks/ws {
        proxy_pass http://multica_ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Multica API（路径重写）
    location /tasks/api/ {
        rewrite ^/tasks(/.*)$ $1 break;
        proxy_pass http://multica_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Multica Auth
    location /tasks/auth/ {
        rewrite ^/tasks(/.*)$ $1 break;
        proxy_pass http://multica_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Multica Frontend
    location /tasks {
        proxy_pass http://multica_frontend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # costrict WebSocket
    location /cloud/device {
        proxy_pass http://costrict_ws;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # costrict API
    location /api/ {
        proxy_pass http://costrict_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # costrict Portal
    location / {
        proxy_pass http://costrict_portal;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

**说明**:
- `/tasks/ws` 必须在 `/tasks/api/` 和 `/tasks` 之前声明，Nginx 匹配第一个前缀
- `map $http_upgrade $connection_upgrade` 是 WebSocket 代理的标准模式
- `/tasks/api/` 和 `/tasks/auth/` 使用 `rewrite` 去除 `/tasks` 前缀后再转发
- 当前配置为 HTTP（`listen 80`），TLS 应配置在上游负载均衡器

### 3.2 Next.js basePath 配置
**文件**: `apps/web/next.config.ts`

**改动**:
```typescript
const basePath = process.env.NEXT_PUBLIC_BASE_PATH ?? "/tasks";
```

**说明**:
- 使用 `??`（nullish coalescing）而非 `||`，允许空字符串覆盖
- 默认值为 `/tasks`，与 Nginx 配置一致
- Next.js 会自动为所有路由和静态资源添加 basePath 前缀

### 3.3 默认端口 8081
**文件**: `server/cmd/server/main.go`、`Makefile`、`.env.example`、`server/cmd/migrate/main.go`、`server/cmd/backfill_task_usage_hourly/main.go`、`server/cmd/multica/cmd_setup.go`

**改动**: 将默认端口从 8080 改为 8081，避免与 costrict-web 的 8080 端口冲突。

```go
// main.go
port := getEnv("PORT", "8081")
```

```makefile
# Makefile
PORT ?= 8081
```

---

## 四、Agent 能力共享（Skill Proxy）

### 4.1 Skill Proxy 客户端
**文件**: `server/internal/service/skill_proxy.go` + `skill_proxy_test.go`

HTTP 客户端，从 costrict-web 内部 API 获取 Skill。

**特性**:
- **TTL 缓存**: 默认 5 分钟缓存，缓存命中不消耗限频配额
- **限频**: 60 次/分/agent，滑动窗口算法
- **审计日志**: 记录到 `multica_agent_audit_logs` 表
- **安全防护**:
  - `io.LimitReader(resp.Body, 1<<20)` 限制响应体大小（1MB 上限）
  - 15 秒 HTTP 超时
  - `X-Internal-Secret` header 内部认证

**API**:
```go
type SkillProxy struct { ... }

func NewSkillProxy(baseURL, secret string, cacheTTL time.Duration, queries *db.Queries) *SkillProxy

func (sp *SkillProxy) FetchSkill(ctx context.Context, id, agentID string) (*Skill, error)
func (sp *SkillProxy) ListSkills() ([]Skill, error)
func (sp *SkillProxy) InvalidateCache(id string)
```

**测试**: 4 个测试全部通过
- `TestSkillProxy_FetchSkill`: 正常获取 skill
- `TestSkillProxy_CachesResults`: 缓存命中
- `TestSkillProxy_RateLimit`: 限频机制（60 次 OK，第 61 次失败，不同 agent 独立计数）
- `TestSkillProxy_ListSkills`: 列表接口

### 4.2 Skill Proxy API Handler
**文件**: `server/internal/handler/skill_proxy.go`

HTTP handler，暴露给前端调用。

**路由**:
- `GET /api/agent-skills`: 列出所有 skill
- `GET /api/agent-skills/{id}?agent_id=<uuid>`: 获取单个 skill

**参数验证**:
- `id`（路径参数）: 必填
- `agent_id`（查询参数）: 必填，必须是合法的 UUID 格式

**错误处理**:
- 400: 缺少参数或 `agent_id` 格式错误
- 429: 限频触发
- 502: 上游 API 调用失败

**TODO**: 当前未验证 `agent_id` 的所有权（即 authenticated user 是否拥有该 agent），待 agent auth 完整接入后补充。

### 4.3 Router 集成
**文件**: `server/cmd/server/router.go`

**改动**:
```go
if opts.SkillProxy != nil {
    skillProxyHandler := handler.NewSkillProxyHandler(opts.SkillProxy)
    r.Get("/api/agent-skills", skillProxyHandler.ListAgentSkills)
    r.Get("/api/agent-skills/{id}", skillProxyHandler.GetAgentSkill)
}
```

**启用条件**: `COSTRICT_API_INTERNAL` 和 `COSTRICT_INTERNAL_SECRET` 环境变量设置时创建 SkillProxy。

---

## 五、测试验证

### 5.1 Go 测试
```bash
go build ./...                          # ✅ 编译通过
go test ./internal/auth/...             # ✅ JWKS + Casdoor JWT 测试全部通过
go test ./internal/service/...          # ✅ Skill Proxy 测试全部通过
go test ./internal/middleware/...       # ✅ Casdoor Auth 中间件测试全部通过
```

**DB 依赖测试**: 需要 Docker Compose 环境运行 `make migrate-up` 后才能通过。

### 5.2 前端测试
```bash
pnpm typecheck                          # ✅ 6/6 通过
pnpm --filter @multica/web exec vitest run app/\(auth\)/login/page.test.tsx  # ✅ 7/7 通过
pnpm test                               # ✅ 除 3 个 sidebar 测试外全部通过
```

**已知问题**: `packages/views/layout/app-sidebar.test.tsx` 的 3 个测试在 `main` 分支也失败，非本次改动引入。

### 5.3 代码审查
**结果**: Approved with minor fixes

**已修复**:
- ✅ I-1: Skill proxy 响应体 `io.LimitReader` 限制（1MB 上限）
- ✅ I-2: `agent_id` UUID 格式校验

**待处理**:
- ⏳ I-3: 自动创建的用户邮箱标记为未验证
- ⏳ I-4: OAuth 回调实现（当前为 501 stub）

---

## 六、待上线前完成

### 6.1 OAuth 回调实现（高优先级）
**文件**: `server/internal/handler/casdoor_stub.go`

当前 `/auth/casdoor/callback` 返回 501 stub。需要实现：
1. 接收 Casdoor 回调的 `code` 参数
2. 用 `code` 换取 access token
3. 用 access token 获取用户信息
4. 创建或查找 Multica 用户
5. 签发 Multica session cookie
6. 重定向到前端

### 6.2 自动创建用户邮箱处理（中优先级）
**文件**: `server/cmd/server/main.go`（`subjectResolver`）

当前自动创建的用户邮箱为 `subject_id@casdoor.local`，这不是可投递的邮箱地址。建议：
- 设置 `email_verified = false` 标记
- 或使用空邮箱，要求用户补全资料后才能接收邮件

### 6.3 完整测试验证（高优先级）
在有 Docker Compose 的环境运行：
```bash
make migrate-up
go test ./... -count=1
```

验证所有 DB 依赖测试通过。

### 6.4 部署验证
1. 启动 docker-compose（PostgreSQL + Casdoor + costrict-web + Multica）
2. 运行迁移：`make migrate-up`
3. 启动后端：`make server`
4. 启动前端：`pnpm dev:web`
5. 测试 Casdoor 登录流程
6. 测试 Nginx 路由（`/tasks`、`/tasks/api`、`/tasks/ws`）
7. 测试跨应用导航（costrict portal → Multica）

---

## 七、架构决策回顾

### 7.1 为什么选择独立子系统而非嵌入模块？
- Multica 使用 Chi router（Go），costrict-web 使用 Gin router（Go），两者不兼容
- Multica 使用 sqlc（类型安全的 SQL 生成），costrict-web 使用 GORM（ORM），迁移成本高
- 独立子系统允许两个服务独立部署、独立扩展、独立回滚

### 7.2 为什么选择共享数据库而非独立数据库？
- 两个系统需要共享用户身份（Casdoor subject_id → Multica user_id）
- 共享数据库简化了跨系统查询（如 costrict-web 查询 Multica 的 agent 使用情况）
- 表名前缀隔离足够安全，PostgreSQL 的 schema 隔离或独立数据库会增加运维复杂度

### 7.3 为什么选择 Nginx 统一入口而非前端集成？
- Multica 前端使用 Next.js（React），costrict-web 前端使用 SolidJS，框架不兼容
- Nginx 反向代理允许两个前端独立部署，用户无感知
- `/tasks/*` 路径清晰划分了两个系统的边界

### 7.4 为什么选择内部 HTTP API 而非 gRPC？
- Skill 市场是低频调用（Agent 启动时获取一次，后续缓存）
- HTTP + JSON 更容易调试和监控
- `X-Internal-Secret` header 足够保护内部 API，无需 gRPC 的 mTLS

---

## 八、文件清单

### 新增文件（核心）
```
server/internal/auth/jwks.go
server/internal/auth/jwks_test.go
server/internal/auth/casdoor.go
server/internal/auth/casdoor_test.go
server/internal/middleware/auth_casdoor.go
server/internal/middleware/auth_casdoor_test.go
server/internal/service/skill_proxy.go
server/internal/service/skill_proxy_test.go
server/internal/handler/skill_proxy.go
server/internal/handler/casdoor_stub.go
server/migrations/113_add_subject_id.up.sql
server/migrations/113_add_subject_id.down.sql
server/migrations/114_rename_tables_multica_prefix.up.sql
server/migrations/114_rename_tables_multica_prefix.down.sql
server/migrations/115_create_agent_audit_logs.up.sql
server/migrations/115_create_agent_audit_logs.down.sql
server/pkg/db/queries/agent_audit.sql
deploy/nginx/costrict.conf
```

### 修改文件（核心）
```
server/cmd/server/main.go
server/cmd/server/router.go
server/internal/middleware/workspace_test.go
server/pkg/db/queries/user.sql
server/pkg/db/queries/*.sql（34 个文件）
server/pkg/db/generated/*.go（sqlc 重新生成）
packages/views/auth/login-page.tsx
packages/core/api/client.ts
packages/core/auth/store.ts
packages/core/platform/auth-initializer.tsx
packages/core/platform/core-provider.tsx
packages/core/platform/types.ts
apps/web/next.config.ts
.env.example
Makefile
```

### 修改文件（全局）
```
server/（70+ Go 源文件，适配 db.Multica* 类型名）
server/（54 个测试文件，修复硬编码 SQL 表名）
```

---

## 九、相关文档

- **设计规格**: `docs/superpowers/specs/2026-05-28-multica-costrict-integration-design.md`
- **实现计划**: `docs/superpowers/plans/2026-05-28-multica-costrict-integration.md`
- **项目约定**: `CLAUDE.md`

---

**文档版本**: 1.0  
**最后更新**: 2026-05-28  
**作者**: Claude Opus 4.7
