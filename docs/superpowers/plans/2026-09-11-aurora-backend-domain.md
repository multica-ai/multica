# Aurora 领域骨架 + 账户 — 实现计划（Plan 1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go 后端建立 Aurora 内容创作领域的最小骨架：注册即自动开通个人空间、可读 16 个 Skill 目录、可创建生成任务行（暂不执行）。

**Architecture:** 新建 `server/internal/aurora/` 存放 Skill 目录常量（单一事实来源）与类型；新建 `server/internal/handler/aurora.go` 承载 `/api/aurora/*` HTTP 处理器；`aurora_generation` / `aurora_asset` 表用 migration 建好，sqlc 生成查询。执行（Plan 3）与计费（Plan 2）尚未接线。

**Tech Stack:** Go 1.26、chi router、sqlc、pgx/v5、`server/internal/testutil`（`Fixture` + `Call`）。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§3、§5、§7、§8）。

**后续计划依赖关系：** Plan 2（计费）依赖本计划的 `aurora_generation` 表与 `status`/`credits_reserved` 字段；Plan 3（执行）依赖 `POST /api/aurora/generations` 建行与 `task_id` 关联；Plan 4（前端）依赖本计划的 API 契约（Task 2/3 的响应 JSON 形状）。

## Global Constraints

以下约束对本计划每个任务默认成立（摘自 spec 与 CLAUDE.md）：

- 数据库**不加外键 / 级联删除/更新**；关系与清理在应用代码显式处理。
- 新建索引必须 `CREATE INDEX CONCURRENTLY`（或 `CREATE UNIQUE INDEX CONCURRENTLY`），且每个并发索引单独一个 migration 文件（不能与其它语句同文件、不能在事务里）。
- migration 文件命名为 `server/migrations/NNN_<name>.up.sql` / `.down.sql`，NNN 为递增序号（当前最新是 450，本计划从 451 起）。
- 代码注释必须英文。
- Go 遵循 `gofmt`、`go vet`、显式检查 error。
- Handler 里从请求边界读 UUID 用 `parseUUIDOrBadRequest`；sqlc 结果回读用 `parseUUID`；不要在 handler 里 open-code `INSERT ... RETURNING`。
- DB-backed Go 测试用 `server/internal/testutil` 的 `Fixture`（`User`/`Workspace`/`Member`/`Runtime`/`Agent`/`Task`/`Insert`）与 `Call(t, h, req).Want(status).JSON(&out)`；不要手写 `INSERT...RETURNING` + `t.Cleanup(DELETE...)` 组合，也不要手写 `httptest.NewRecorder()` 四件套。
- 所有 API 响应要 schema 化；Go 侧每个端点配 malformed-response 测试。
- Workspace 作用域查询必须带 `workspace_id` 过滤。
- 新增端点先写失败测试再实现（TDD）。

---

### Task 1: `aurora_generation` 与 `aurora_asset` 表

**Files:**
- Create: `server/migrations/451_aurora_generation.up.sql`
- Create: `server/migrations/451_aurora_generation.down.sql`
- Create: `server/migrations/452_aurora_asset.up.sql`
- Create: `server/migrations/452_aurora_asset.down.sql`
- Create: `server/pkg/db/queries/aurora.sql`
- Modify: 运行 `make sqlc` 后生成 `server/pkg/db/generated/*`（自动，不手改）

**Interfaces:**
- Consumes: 现有 `workspace` / `user` / `member` / `agent_task_queue` 表（已存在）。
- Produces:
  - 表 `aurora_generation`：`id UUID PK`、`workspace_id UUID NOT NULL`、`user_id UUID NOT NULL`、`skill_id TEXT NOT NULL`、`prompt TEXT NOT NULL`、`status TEXT NOT NULL`（`queued | running | completed | failed`）、`task_id UUID`（可空，关联 `agent_task_queue.id`，Plan 3 填充）、`credits_reserved BIGINT NOT NULL DEFAULT 0`、`credits_charged BIGINT NOT NULL DEFAULT 0`、`error TEXT`、`created_at timestamptz NOT NULL DEFAULT now()`、`updated_at timestamptz NOT NULL DEFAULT now()`。
  - 表 `aurora_asset`：`id UUID PK`、`generation_id UUID NOT NULL`（应用层关联，无 FK）、`workspace_id UUID NOT NULL`、`kind TEXT NOT NULL`（`image | video | text | document`）、`media_url TEXT`、`format TEXT`、`created_at timestamptz NOT NULL DEFAULT now()`。
  - sqlc 查询（`aurora.sql`）：`CreateAuroraGeneration`（INSERT）、`GetAuroraGeneration`（按 id+workspace_id SELECT）。

- [ ] **Step 1: 写 migration 文件**

`server/migrations/451_aurora_generation.up.sql`：

```sql
-- Aurora content-creation generation: one row per user submission, keyed to an
-- agent_task_queue row once execution is wired (Plan 3). No foreign key by house
-- rule; workspace membership is re-validated in application code.
CREATE TABLE IF NOT EXISTS aurora_generation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    skill_id TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    task_id UUID,
    credits_reserved BIGINT NOT NULL DEFAULT 0,
    credits_charged BIGINT NOT NULL DEFAULT 0,
    error TEXT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
```

`server/migrations/451_aurora_generation.down.sql`：

```sql
DROP TABLE IF EXISTS aurora_generation;
```

`server/migrations/452_aurora_asset.up.sql`：

```sql
-- Content asset produced by a generation (image/video/text/document). Linked to
-- its generation by application-level id; no FK by house rule.
CREATE TABLE IF NOT EXISTS aurora_asset (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    generation_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL,
    media_url TEXT,
    format TEXT,
    created_at timestamptz NOT NULL DEFAULT now()
);
```

`server/migrations/452_aurora_asset.down.sql`：

```sql
DROP TABLE IF EXISTS aurora_asset;
```

- [ ] **Step 2: 写 sqlc 查询文件**

`server/pkg/db/queries/aurora.sql`：

```sql
-- name: CreateAuroraGeneration :one
INSERT INTO aurora_generation (workspace_id, user_id, skill_id, prompt, status)
VALUES ($1, $2, $3, $4, 'queued')
RETURNING id, workspace_id, user_id, skill_id, prompt, status, task_id,
          credits_reserved, credits_charged, error, created_at, updated_at;

-- name: GetAuroraGeneration :one
SELECT id, workspace_id, user_id, skill_id, prompt, status, task_id,
       credits_reserved, credits_charged, error, created_at, updated_at
FROM aurora_generation
WHERE id = $1 AND workspace_id = $2;
```

- [ ] **Step 3: 运行 sqlc 生成代码**

Run: `make sqlc`
Expected: 生成 `CreateAuroraGeneration` / `GetAuroraGeneration` 与对应 model，无报错。

- [ ] **Step 4: 验证 migration 可应用**

Run: `make test`（跑 migration 相关既有测试，确认 schema 可建）
Expected: 现有测试仍通过；新表被建出（或至少 `make sqlc` 生成的 model 与新表列一致）。

- [ ] **Step 5: Commit**

```bash
git add server/migrations/451_aurora_generation.* server/migrations/452_aurora_asset.* server/pkg/db/queries/aurora.sql server/pkg/db/generated/
git commit -m "feat(aurora): add generation and asset tables"
```

---

### Task 2: Skill 目录（Go 常量 + `GET /api/aurora/skills`）

**Files:**
- Create: `server/internal/aurora/catalog.go`
- Create: `server/internal/aurora/catalog_test.go`
- Create: `server/internal/handler/aurora.go`
- Create: `server/internal/handler/aurora_test.go`
- Modify: `server/cmd/server/router.go`（注册路由）

**Interfaces:**
- Consumes: `testutil.Fixture`、`testutil.Call`、chi router。
- Produces（后续计划依赖的契约）：
  - 类型 `aurora.SkillCatalogEntry{ ID, Name, NameEn, Category, Credits int, Input []string, Output []string, Featured bool }`。
  - 函数 `aurora.Catalog() []SkillCatalogEntry`（返回 16 个条目，顺序固定）。
  - 端点 `GET /api/aurora/skills` → `200 {"skills":[{...}]}`，每个条目字段名：`id`、`name`（中文）、`name_en`、`category`、`credits`、`input`、`output`、`featured`。Plan 4 的 zod schema 与前端消费以这个 JSON 形状为准。

- [ ] **Step 1: 写 catalog 失败测试**

`server/internal/aurora/catalog_test.go`（package `aurora_test`）：

```go
package aurora_test

import (
	"testing"

	"<module>/server/internal/aurora"
)

// A catalog test needs no DB and no DOM; the aurora package is pure data.
func TestCatalogHasSixteenEntriesAndUniqueIDs(t *testing.T) {
	cat := aurora.Catalog()
	if len(cat) != 16 {
		t.Fatalf("expected 16 skills, got %d", len(cat))
	}
	seen := map[string]bool{}
	for _, e := range cat {
		if e.ID == "" {
			t.Fatal("empty skill id")
		}
		if seen[e.ID] {
			t.Fatalf("duplicate skill id %q", e.ID)
		}
		seen[e.ID] = true
		if e.Credits <= 0 {
			t.Fatalf("skill %q has non-positive credits %d", e.ID, e.Credits)
		}
		if e.Name == "" || e.NameEn == "" {
			t.Fatalf("skill %q missing bilingual name", e.ID)
		}
	}
}
```

> 注意：把 `<module>` 换成仓库真实 module path（读 `server/go.mod` 的 `module` 行，例如 `github.com/multica/multica/server` 之类，按实际填写）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/aurora/ -run TestCatalogHasSixteenEntriesAndUniqueIDs`
Expected: 编译失败（`aurora` 包不存在）或 `Catalog` undefined。

- [ ] **Step 3: 实现 catalog**

`server/internal/aurora/catalog.go`：

```go
// Package aurora holds the Aurora content-creation domain's static data.
// The skill catalog is the single source of truth for the consumer-facing
// tool directory; the frontend renders whatever this returns and never keeps
// its own parallel copy.
package aurora

// SkillCatalogEntry is one consumer-facing skill in the directory.
type SkillCatalogEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`     // Chinese display name
	NameEn   string   `json:"name_en"`  // English display name
	Category string   `json:"category"` // image | video | content | office
	Credits  int      `json:"credits"`
	Input    []string `json:"input"`
	Output   []string `json:"output"`
	Featured bool     `json:"featured"`
}

// Catalog returns the 16 skills in fixed display order.
func Catalog() []SkillCatalogEntry {
	return []SkillCatalogEntry{
		{ID: "poster", Name: "海报制作", NameEn: "Poster", Category: "image", Credits: 760, Input: []string{"text", "image"}, Output: []string{"image"}, Featured: true},
		{ID: "xhs-image", Name: "小红书图片", NameEn: "Xiaohongshu Image", Category: "image", Credits: 620, Input: []string{"text", "image"}, Output: []string{"image"}, Featured: true},
		// ... 其余 14 个条目按 spec §Skill 目录的顺序补齐：product-image, text-image,
		// image-edit, id-photo, image-video, text-video, video-captions, avatar-video,
		// xhs-copy, resume, document-summary, transcription, ppt, excel。每个都带 Name/NameEn/
		// Category/Credits/Input/Output，Featured 按 aurora page.tsx 的 featured 标记。
	}
}
```

> 16 个条目的 `credits` / `input` / `output` 值直接抄 `aurora-ai-agents/lib/skill-runtime.ts`（spec 附录未含，参考源仓库该文件第 26–235 行）。`NameEn` 为新增的英文显示名，需逐条给出；`Featured` 与 `aurora-ai-agents/app/page.tsx` 的 `featured: true` 一致（`xhs-image`、`poster`）。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/aurora/ -run TestCatalogHasSixteenEntriesAndUniqueIDs`
Expected: PASS。

- [ ] **Step 5: 写 handler 失败测试**

`server/internal/handler/aurora_test.go`：

```go
package handler_test

import (
	"net/http"
	"testing"

	"<module>/server/internal/handler"
	"<module>/server/internal/testutil"
)

func TestAuroraSkillsReturnsCatalog(t *testing.T) {
	h := handler.AuroraSkills() // 见 Step 6 的签名
	req := testutil.JSONRequest(http.MethodGet, "/api/aurora/skills", nil)

	out := testutil.Decode[struct {
		Skills []struct {
			ID       string   `json:"id"`
			Name     string   `json:"name"`
			NameEn   string   `json:"name_en"`
			Category string   `json:"category"`
			Credits  int      `json:"credits"`
			Input    []string `json:"input"`
			Output   []string `json:"output"`
			Featured bool     `json:"featured"`
		} `json:"skills"`
	}](t, h, req, http.StatusOK)

	if len(out.Skills) != 16 {
		t.Fatalf("expected 16 skills, got %d", len(out.Skills))
	}
}

// Malformed-response test: the endpoint takes no body and ignores a bad one
// rather than crashing; a GET with a JSON body must still return the catalog.
func TestAuroraSkillsIgnoresMalformedBody(t *testing.T) {
	h := handler.AuroraSkills()
	req := testutil.JSONRequest(http.MethodGet, "/api/aurora/skills", "{not json")
	testutil.Call(t, h, req).Want(http.StatusOK)
}
```

- [ ] **Step 6: 实现 handler + 路由**

`server/internal/handler/aurora.go`：

```go
package handler

import (
	"encoding/json"
	"net/http"

	"<module>/server/internal/aurora"
)

// AuroraSkills returns the full skill catalog. It is unauthenticated-safe in
// shape but is mounted behind workspace resolution; a GET carries no body.
func AuroraSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"skills": aurora.Catalog()})
	}
}

// writeJSON is a local helper mirroring the repo's existing JSON writers; reuse
// the handler package's existing response helper if one is already exported.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

> 若 handler 包已有一个统一的 JSON 写回 helper（如 `render.JSON` / `writeJSON`），复用那个，不要新写 `writeJSON`。读 `server/internal/handler/` 现有文件确认。

路由注册 `server/cmd/server/router.go`（在 workspace 路由分组内新增，具体挂载点参照现有 `/api/...` 分组）：

```go
r.Group(func(r chi.Router) {
	r.Use(middleware.RequireWorkspaceMember())
	r.Get("/api/aurora/skills", handler.AuroraSkills())
})
```

> 具体分组与中间件名以 `router.go` 现有写法为准（`ResolveWorkspaceIDFromRequest` + `RequireWorkspaceMember`），本片段是形状示意，不是可直接粘贴。

- [ ] **Step 7: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestAuroraSkills`
Expected: 两个测试 PASS。

- [ ] **Step 8: Commit**

```bash
git add server/internal/aurora/ server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): serve skill catalog from Go constant"
```

---

### Task 3: `POST /api/aurora/generations`（建任务行）

**Files:**
- Modify: `server/internal/handler/aurora.go`（新增 handler）
- Modify: `server/internal/handler/aurora_test.go`（新增测试）
- Modify: `server/cmd/server/router.go`（注册路由）

**Interfaces:**
- Consumes: Task 1 的 sqlc 查询 `CreateAuroraGeneration`；`testutil.Fixture`（建 workspace/user/member）。
- Produces: 端点 `POST /api/aurora/generations`，body `{"skillId":"xhs-image","prompt":"..."}` → `201 {"generation":{"id","skillId","prompt","status":"queued","creditsReserved":0}}`。Plan 3 会在此基础上接执行；Plan 2 会填 `creditsReserved`。

- [ ] **Step 1: 写失败测试**

`server/internal/handler/aurora_test.go` 追加：

```go
func TestAuroraCreateGeneration(t *testing.T) {
	// 需要一个绑定 workspace 的 Fixture。DB-backed 测试的 TestMain 已在 testutil 中
	// 提供；这里沿用仓库现有 handler 测试的 setup 方式（读 aurora_test.go 同目录的
	// 其它 *_test.go 看 TestMain / fixture 初始化怎么写）。
	// 伪代码：fx := 已有 fixture；用 fx.User / fx.Workspace / fx.Member 建好。
	h := handler.AuroraCreateGeneration(querier, fx) // 见 Step 3 签名
	req := testutil.JSONRequest(http.MethodPost, "/api/aurora/generations",
		map[string]string{"skillId": "xhs-image", "prompt": "生成一张新加坡亲子游封面"})
	req = testutil.WithHeaders(req, "X-Workspace-ID", fx.WorkspaceID)

	out := testutil.Decode[struct {
		Generation struct {
			ID              string `json:"id"`
			SkillID         string `json:"skillId"`
			Prompt          string `json:"prompt"`
			Status          string `json:"status"`
			CreditsReserved int64  `json:"creditsReserved"`
		} `json:"generation"`
	}](t, h, req, http.StatusCreated)

	if out.Generation.Status != "queued" {
		t.Fatalf("expected queued, got %q", out.Generation.Status)
	}
	if out.Generation.SkillID != "xhs-image" {
		t.Fatalf("expected skillId xhs-image, got %q", out.Generation.SkillID)
	}
}

func TestAuroraCreateGenerationRejectsUnknownSkill(t *testing.T) {
	h := handler.AuroraCreateGeneration(querier, fx)
	req := testutil.JSONRequest(http.MethodPost, "/api/aurora/generations",
		map[string]string{"skillId": "nope", "prompt": "x"})
	req = testutil.WithHeaders(req, "X-Workspace-ID", fx.WorkspaceID)
	testutil.Call(t, h, req).Want(http.StatusBadRequest)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/handler/ -run TestAuroraCreateGeneration`
Expected: 编译失败（`AuroraCreateGeneration` undefined）。

- [ ] **Step 3: 实现 handler**

`server/internal/handler/aurora.go` 追加（沿用仓库现有 handler 的依赖注入方式，读同目录 handler 怎么拿到 sqlc querier 与 workspace 上下文）：

```go
type createGenerationRequest struct {
	SkillID string `json:"skillId"`
	Prompt  string `json:"prompt"`
}

func AuroraCreateGeneration(q <querier类型>) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createGenerationRequest
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.SkillID == "" || req.Prompt == "" {
			writeErr(w, http.StatusBadRequest, "skillId and prompt are required")
			return
		}
		if !aurora.Exists(req.SkillID) {
			writeErr(w, http.StatusBadRequest, "unknown skill")
			return
		}
		// workspaceID / userID 从请求上下文取（仓库已有 helper，如
		// middleware 注入的 workspace id / auth 注入的 user id）。
		row, err := q.CreateAuroraGeneration(r.Context(), <workspaceID>, <userID>, req.SkillID, req.Prompt)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create generation")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"generation": row})
	}
}
```

> `aurora.Exists(id string) bool` 在 Task 2 的 `catalog.go` 补一个函数（遍历 `Catalog()`），并在 `catalog_test.go` 加一条断言 `Exists("xhs-image")==true && Exists("nope")==false`。
> `<querier类型>` / `decodeJSON` / `writeErr` / workspace/user 上下文的取法，以仓库现有 handler 实现为准（读 `server/internal/handler/skill.go` 或 `dashboard.go`），不要臆造签名。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestAuroraCreateGeneration`
Expected: 两个测试 PASS（`TestAuroraCreateGeneration` + `TestAuroraCreateGenerationRejectsUnknownSkill`）。

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/internal/aurora/catalog.go server/cmd/server/router.go
git commit -m "feat(aurora): create generation rows"
```

---

### Task 4: 注册即自动开通个人 workspace

**Files:**
- Modify: `server/internal/handler/auth.go`（`VerifyCode` 建号路径，约 :376 行附近）
- Modify: `server/internal/handler/auth_test.go`（或新建 `server/internal/handler/auth_personal_workspace_test.go`）
- Create: `server/internal/service/personal_workspace.go`（若仓库把 workspace 创建逻辑放 service 层）

**Interfaces:**
- Consumes: 现有 `CreateWorkspace` 逻辑（`server/internal/handler/workspace.go:202` 附近）或 service 层等价物。
- Produces: 函数 `ensurePersonalWorkspace(ctx, userID) (workspaceID, error)`：用户无任何 workspace 时，创建一个单成员 owner workspace（name 用「<name> 的个人空间」或类似，slug 用 `user-<userID>` 前 8 位或 email 前缀去重），并把该用户加为 owner。**幂等**：已有 workspace 则返回现有第一个（或直接返回，不重复建）。

- [ ] **Step 1: 写失败测试**

新建 `server/internal/handler/auth_personal_workspace_test.go`（沿用仓库 auth 测试的 setup）：

```go
func TestVerifyCodeProvisionsPersonalWorkspace(t *testing.T) {
	// 1. 触发邮箱验证码注册（复用现有 auth 测试的 SendCode/VerifyCode 调用方式），
	//    得到一个新建 user。
	// 2. 断言该 user 拥有恰好 1 个 workspace，且是 owner member。
	// 伪代码：
	//   userID := <通过 VerifyCode 注册得到的 user id>
	//   n := fx.Count(t, "SELECT count(*) FROM workspace WHERE id IN (SELECT workspace_id FROM member WHERE user_id=$1)", userID)
	//   if n != 1 { t.Fatalf("expected 1 personal workspace, got %d", n) }
}

func TestVerifyCodeDoesNotDuplicatePersonalWorkspace(t *testing.T) {
	// 已有一个 workspace 的 user 再次走 VerifyCode 登录，不应再建第二个。
}
```

> 具体断言用 `fx.Count` / `fx.QueryRow`。auth 测试的 fixture 与调用方式读 `server/internal/handler/auth_test.go`（或仓库中 auth 相关测试），照抄 setup，不要臆造。测试必须先失败（当前 `ensurePersonalWorkspace` 不存在）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/handler/ -run TestVerifyCodeProvisionsPersonalWorkspace`
Expected: 编译失败或断言失败（功能未实现）。

- [ ] **Step 3: 实现**

在 `VerifyCode` 建号成功后调用 `ensurePersonalWorkspace`；实现该函数（复用现有 workspace 创建 + member 加入逻辑，遵循「无 FK、应用层清理、事务包住创建+加成员」）。`ensurePersonalWorkspace` 幂等：先查 user 是否已有 workspace（`SELECT ... FROM member WHERE user_id=$1 LIMIT 1`），有则直接返回，无则创建。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestVerifyCodeProvisionsPersonalWorkspace`
Expected: 两个测试 PASS。

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/auth.go server/internal/handler/auth_personal_workspace_test.go
git commit -m "feat(aurora): auto-provision personal workspace on signup"
```

---

## Self-Review（写完后自查）

- **Spec 覆盖**：§5 的 `aurora_generation`/`aurora_asset` 表 → Task 1；§8 的 `GET /api/aurora/skills` 单一事实来源 → Task 2；§8 的 `POST /api/aurora/generations` → Task 3；§7 的「注册即开通个人空间」→ Task 4。§4 执行层 → Plan 3；§6 计费 → Plan 2；§8 前端 → Plan 4。**YAGNI 偏离**：§5 的 `aurora_skill_catalog` 表改为 Go 常量（本计划已声明），如需改价再加表。
- **类型一致性**：`SkillCatalogEntry` 字段在 catalog（Task 2 Step 3）与 handler 测试（Task 2 Step 5）一致；`CreateAuroraGeneration` 返回列与 `GetAuroraGeneration`（Task 1 Step 2）一致；`AuroraSkills`/`AuroraCreateGeneration` 签名在测试与实现一致。
- **占位符**：`<module>`、`<querier类型>`、`<workspaceID>` 等尖括号标记是**要求实现者按仓库实际填写的点**，不是 TBD——每个都附了「读哪个文件确认」的指引。若你认为这些也算占位符，实现时第一步先把它们替换成真实值。

---

## 执行交接

Plan 1 完成并保存。剩余 Plan 2/3/4 尚未写。建议：先按本计划实现并验证 Plan 1 跑通（`make test` 全绿），再写 Plan 2。
