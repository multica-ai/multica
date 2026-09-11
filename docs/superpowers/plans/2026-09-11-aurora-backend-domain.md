# Aurora 领域骨架 + 账户 — 实现计划（Plan 1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Go 后端建立 Aurora 内容创作领域的最小骨架：注册即自动开通个人空间、可读 16 个 Skill 目录、可创建生成任务行（暂不执行）。

**Architecture:** 新建 `server/internal/aurora/` 存放 Skill 目录常量（单一事实来源）与类型；在 `server/internal/handler/aurora.go` 新增两个 `*Handler` 方法；`aurora_generation` / `aurora_asset` 表用 migration 建好，sqlc 生成查询。执行（Plan 3）与计费（Plan 2）尚未接线。

**Tech Stack:** Go 1.26、chi router、sqlc、pgx/v5、`server/internal/testutil`（`Fixture` + `Call`）。

**Spec:** `docs/superpowers/specs/2026-09-11-aurora-content-creation-app-design.md`（§3、§5、§7、§8）。

**已核实的关键事实（写代码前必读，避免臆造）：**
- Go module path：`github.com/multica-ai/multica/server`（`server/go.mod`）。
- sqlc 生成的 `db` 包 = `github.com/multica-ai/multica/server/pkg/db/generated`；查询写在 `server/pkg/db/queries/*.sql`，跑 `make sqlc` 生成到 `server/pkg/db/generated/`。
- Handler 是 `*Handler` 结构体方法：`func (h *Handler) X(w http.ResponseWriter, r *http.Request)`（见 `server/internal/handler/handler.go:407` `New` 与任意 handler 文件）。
- 已存在的包级 helper（**不要重新定义**）：`writeJSON(w, status, v)`、`writeError(w, status, msg)`（`handler.go:524/563`）；`parseUUIDOrBadRequest(w, s, field) (pgtype.UUID, bool)`（`handler.go:662`）；`parseUUID(s) pgtype.UUID` / `uuidToString(u) string`（`handler.go:607-608`）；`requireUserID(w, r) (string, bool)`（`handler.go:879`）；`requestUserID(r) string`（`handler.go:813`）；`(h *Handler) resolveWorkspaceID(r) string`（`handler.go:895`）。
- 读请求体：`json.NewDecoder(r.Body).Decode(&req)`（无 decodeJSON helper）。
- 测试基建（`server/internal/handler/handler_test.go`）：包级 `testHandler *Handler`、`testPool *pgxpool.Pool`、`testUserID`、`testWorkspaceID`、`dbfx *testutil.Fixture`；`newRequest(method, path, body)` 会设 `X-User-ID`/`X-Workspace-ID` 头；断言用 `testutil.Call(t, testHandler.Method, req).Want(status).JSON(&out)`。测试文件写 `package handler`（与 handler 同包，直接用这些私有变量）。
- migration 文件放 `server/migrations/NNN_<name>.up.sql` / `.down.sql`，由 `server/internal/migrations/migrations.go` 的 `go:embed` 拾取；当前最新序号是 **450**，本计划从 **451** 起。`gen_random_uuid()` 是 PG13+ 内建函数，仓库用 pg17，直接可用。

## Global Constraints

- 数据库**不加外键 / 级联删除/更新**；关系与清理在应用代码显式处理。
- 新建索引必须 `CREATE INDEX CONCURRENTLY`，且每个并发索引单独一个 migration 文件。本计划不新增索引（查询走 PK 或已有 `member.user_id` 索引）；后续「列 generations」端点需要 `aurora_generation(workspace_id)` 索引时再单文件补。
- 代码注释必须英文；Go 遵循 `gofmt`/`go vet`/显式检查 error。
- 从请求边界读 UUID 用 `parseUUIDOrBadRequest`；sqlc 结果回读用 `parseUUID`；不在 handler 里 open-code `INSERT ... RETURNING`（写查询都走 sqlc）。
- 新增端点先写失败测试再实现（TDD）。

---

### Task 1: `aurora_generation` 与 `aurora_asset` 表 + sqlc 查询

**Files:**
- Create: `server/migrations/451_aurora_generation.up.sql`
- Create: `server/migrations/451_aurora_generation.down.sql`
- Create: `server/migrations/452_aurora_asset.up.sql`
- Create: `server/migrations/452_aurora_asset.down.sql`
- Create: `server/pkg/db/queries/aurora.sql`
- 自动生成（不手改）：`make sqlc` 产出 `server/pkg/db/generated/aurora.sql.go` 与 models

**Interfaces:**
- Consumes: 已存在的 `workspace` / `user` / `member` 表。
- Produces（后续任务/计划依赖）：
  - 表 `aurora_generation` 列：`id uuid PK`、`workspace_id uuid`、`user_id uuid`、`skill_id text`、`prompt text`、`status text`、`task_id uuid NULL`、`credits_reserved bigint`、`credits_charged bigint`、`error text NULL`、`created_at timestamptz`、`updated_at timestamptz`。
  - 表 `aurora_asset` 列：`id uuid PK`、`generation_id uuid`、`workspace_id uuid`、`kind text`、`media_url text NULL`、`format text NULL`、`created_at timestamptz`。
  - sqlc 查询：`CreateAuroraGeneration`（`:one` INSERT）、`GetAuroraGeneration`（`:one` SELECT by id+workspace_id）、`CountWorkspacesForUser`（`:one`，Task 4 用）。

- [ ] **Step 1: 写 migration 文件**

`server/migrations/451_aurora_generation.up.sql`：

```sql
-- Aurora content-creation generation: one row per user submission, keyed to an
-- agent_task_queue row once execution is wired (Plan 3). No foreign key by house
-- rule; workspace membership is re-validated in application code on every write.
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

-- name: CountWorkspacesForUser :one
SELECT count(*) FROM member WHERE user_id = $1;
```

- [ ] **Step 3: 运行 sqlc 生成代码**

Run: `cd server && make sqlc`
Expected: 生成 `CreateAuroraGeneration`、`GetAuroraGeneration`、`CountWorkspacesForUser` 与 `AuroraGeneration`/`AuroraAsset` model。确认 `CreateAuroraGenerationParams` 的字段名是 `WorkspaceID`/`UserID`/`SkillID`/`Prompt`（sqlc 对 `workspace_id`→`WorkspaceID` 的 snake_case→CamelCase 转换），`UserID`/`WorkspaceID` 类型为 `pgtype.UUID`。若字段名与计划不一致，以 `make sqlc` 生成为准改后续代码。

- [ ] **Step 4: 验证 migration 可应用**

Run: `cd server && go test ./internal/migrations/ -run . -count=1`
Expected: migration lint/apply 相关测试通过（新表可建）。

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
- Produces：
  - `aurora.SkillCatalogEntry{ ID, Name, NameEn, Category string; Credits int; Input, Output []string; Featured bool }`（json tag 见下）。
  - `aurora.Catalog() []SkillCatalogEntry`（16 条，固定顺序）。
  - `aurora.Exists(id string) bool`。
  - 端点 `GET /api/aurora/skills` → `200 {"skills":[{...}]}`，条目字段：`id`、`name`、`name_en`、`category`、`credits`、`input`、`output`、`featured`。Plan 4 的 zod schema 与前端消费以这个 JSON 为准。

- [ ] **Step 1: 写 catalog 失败测试**

`server/internal/aurora/catalog_test.go`（package `aurora_test`）：

```go
package aurora_test

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/aurora"
)

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

func TestExists(t *testing.T) {
	if !aurora.Exists("xhs-image") {
		t.Fatal("Exists(xhs-image) = false, want true")
	}
	if aurora.Exists("nope") {
		t.Fatal("Exists(nope) = true, want false")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/aurora/ -run 'TestCatalogHasSixteenEntriesAndUniqueIDs|TestExists'`
Expected: 编译失败（`aurora` 包不存在）。

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
		{ID: "product-image", Name: "商品图制作", NameEn: "Product Image", Category: "image", Credits: 860, Input: []string{"text", "image"}, Output: []string{"image"}},
		{ID: "text-image", Name: "文字生成图片", NameEn: "Text to Image", Category: "image", Credits: 680, Input: []string{"text"}, Output: []string{"image"}},
		{ID: "image-edit", Name: "图片修改", NameEn: "Image Edit", Category: "image", Credits: 520, Input: []string{"text", "image"}, Output: []string{"image"}},
		{ID: "id-photo", Name: "证件照制作", NameEn: "ID Photo", Category: "image", Credits: 360, Input: []string{"image", "text"}, Output: []string{"image"}},
		{ID: "image-video", Name: "图片生成视频", NameEn: "Image to Video", Category: "video", Credits: 1880, Input: []string{"image", "text"}, Output: []string{"video"}},
		{ID: "text-video", Name: "文字生成视频", NameEn: "Text to Video", Category: "video", Credits: 1680, Input: []string{"text"}, Output: []string{"video"}},
		{ID: "video-captions", Name: "视频剪辑与字幕", NameEn: "Video Captions", Category: "video", Credits: 980, Input: []string{"video", "text"}, Output: []string{"video"}},
		{ID: "avatar-video", Name: "数字人口播", NameEn: "Avatar Video", Category: "video", Credits: 1480, Input: []string{"text", "image", "audio"}, Output: []string{"video"}},
		{ID: "xhs-copy", Name: "小红书文案", NameEn: "Xiaohongshu Copy", Category: "content", Credits: 260, Input: []string{"text", "document"}, Output: []string{"text"}},
		{ID: "resume", Name: "简历制作", NameEn: "Resume", Category: "office", Credits: 420, Input: []string{"text", "document"}, Output: []string{"pdf", "text"}},
		{ID: "document-summary", Name: "文件总结", NameEn: "Document Summary", Category: "office", Credits: 380, Input: []string{"document", "text"}, Output: []string{"text"}},
		{ID: "transcription", Name: "录音转文字", NameEn: "Transcription", Category: "office", Credits: 300, Input: []string{"audio", "video"}, Output: []string{"text"}},
		{ID: "ppt", Name: "PPT 制作", NameEn: "PPT", Category: "office", Credits: 820, Input: []string{"text", "document", "spreadsheet", "image"}, Output: []string{"pptx", "pdf"}},
		{ID: "excel", Name: "Excel 数据分析", NameEn: "Excel Analysis", Category: "office", Credits: 460, Input: []string{"spreadsheet", "text"}, Output: []string{"xlsx", "pdf", "text"}},
	}
}

// Exists reports whether id names a catalog entry.
func Exists(id string) bool {
	for _, e := range Catalog() {
		if e.ID == id {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/aurora/ -run 'TestCatalogHasSixteenEntriesAndUniqueIDs|TestExists'`
Expected: PASS。

- [ ] **Step 5: 写 handler 失败测试**

`server/internal/handler/aurora_test.go`（package `handler`）：

```go
package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListAuroraSkills(t *testing.T) {
	req := newRequest(http.MethodGet, "/api/aurora/skills", nil)
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
	}](t, testHandler.ListAuroraSkills, req, http.StatusOK)

	if len(out.Skills) != 16 {
		t.Fatalf("expected 16 skills, got %d", len(out.Skills))
	}
}
```

- [ ] **Step 6: 实现 handler + 路由**

`server/internal/handler/aurora.go`：

```go
package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/aurora"
)

// ListAuroraSkills returns the full skill catalog. Mounted behind workspace
// resolution in the router; a GET carries no body.
func (h *Handler) ListAuroraSkills(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"skills": aurora.Catalog()})
}
```

路由注册 `server/cmd/server/router.go`：找到现有 workspace 作用域分组（含 `RequireWorkspaceMember` 的 `r.Group`），在其内部加：

```go
r.Get("/api/aurora/skills", h.ListAuroraSkills)
```

（具体挂载点以 `router.go` 现有 `r.Get("/api/..." ...)` 分组为准；若该分组用 `h` 接收者即直接 `h.ListAuroraSkills`，用 `handler` 变量则对应调整。）

- [ ] **Step 7: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestListAuroraSkills`
Expected: PASS。

- [ ] **Step 8: Commit**

```bash
git add server/internal/aurora/ server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): serve skill catalog from Go constant"
```

---

### Task 3: `POST /api/aurora/generations`（建任务行）

**Files:**
- Modify: `server/internal/handler/aurora.go`（新增 handler + 响应 DTO）
- Modify: `server/internal/handler/aurora_test.go`（新增测试）
- Modify: `server/cmd/server/router.go`（注册路由）

**Interfaces:**
- Consumes: Task 1 的 `h.Queries.CreateAuroraGeneration`。
- Produces: 端点 `POST /api/aurora/generations`，body `{"skillId":"xhs-image","prompt":"..."}` → `201 {"generation":{"id","skillId","prompt","status":"queued","creditsReserved":0}}`。Plan 3 接执行，Plan 2 填 `creditsReserved`。

- [ ] **Step 1: 写失败测试**

`server/internal/handler/aurora_test.go` 追加：

```go
func TestCreateAuroraGeneration(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/aurora/generations", map[string]string{
		"skillId": "xhs-image",
		"prompt":  "生成一张新加坡亲子游封面",
	})
	out := testutil.Decode[struct {
		Generation struct {
			ID              string `json:"id"`
			SkillID         string `json:"skillId"`
			Prompt          string `json:"prompt"`
			Status          string `json:"status"`
			CreditsReserved int64  `json:"creditsReserved"`
		} `json:"generation"`
	}](t, testHandler.CreateAuroraGeneration, req, http.StatusCreated)

	if out.Generation.Status != "queued" {
		t.Fatalf("expected queued, got %q", out.Generation.Status)
	}
	if out.Generation.SkillID != "xhs-image" {
		t.Fatalf("expected skillId xhs-image, got %q", out.Generation.SkillID)
	}
}

func TestCreateAuroraGenerationRejectsUnknownSkill(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/aurora/generations", map[string]string{
		"skillId": "nope",
		"prompt":  "x",
	})
	testutil.Call(t, testHandler.CreateAuroraGeneration, req).Want(http.StatusBadRequest)
}

func TestCreateAuroraGenerationRejectsMissingPrompt(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/aurora/generations", map[string]string{
		"skillId": "xhs-image",
	})
	testutil.Call(t, testHandler.CreateAuroraGeneration, req).Want(http.StatusBadRequest)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/handler/ -run TestCreateAuroraGeneration`
Expected: 编译失败（`CreateAuroraGeneration` 方法不存在）。

- [ ] **Step 3: 实现 handler**

`server/internal/handler/aurora.go` 追加：

```go
import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/aurora"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AuroraGenerationResponse is the consumer-facing shape of a generation row.
type AuroraGenerationResponse struct {
	ID              string `json:"id"`
	SkillID         string `json:"skillId"`
	Prompt          string `json:"prompt"`
	Status          string `json:"status"`
	CreditsReserved int64  `json:"creditsReserved"`
}

// CreateAuroraGeneration creates a queued generation row for the current user
// in the resolved workspace. Execution is wired in Plan 3; credits reservation
// lands in Plan 2.
func (h *Handler) CreateAuroraGeneration(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	var req struct {
		SkillID string `json:"skillId"`
		Prompt  string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SkillID == "" || req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "skillId and prompt are required")
		return
	}
	if !aurora.Exists(req.SkillID) {
		writeError(w, http.StatusBadRequest, "unknown skill")
		return
	}

	row, err := h.Queries.CreateAuroraGeneration(r.Context(), db.CreateAuroraGenerationParams{
		WorkspaceID: workspaceID,
		UserID:      userUUID,
		SkillID:     req.SkillID,
		Prompt:      req.Prompt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create generation")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"generation": AuroraGenerationResponse{
		ID:              uuidToString(row.ID),
		SkillID:         row.SkillID,
		Prompt:          row.Prompt,
		Status:          row.Status,
		CreditsReserved: row.CreditsReserved,
	}})
}
```

> 注意：`db.CreateAuroraGenerationParams` 的字段名以 Task 1 Step 3 `make sqlc` 生成为准（预期 `WorkspaceID`/`UserID`/`SkillID`/`Prompt`）；`row.CreditsReserved` 为 `int64`（bigint）。若生成类型字段名不同，按生成结果对齐即可，不要改 SQL 语义。

路由注册 `server/cmd/server/router.go`（同 Task 2 分组内）：

```go
r.Post("/api/aurora/generations", h.CreateAuroraGeneration)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run TestCreateAuroraGeneration`
Expected: 三个测试 PASS（含 RejectsUnknownSkill / RejectsMissingPrompt）。

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/aurora.go server/internal/handler/aurora_test.go server/cmd/server/router.go
git commit -m "feat(aurora): create generation rows"
```

---

### Task 4: 注册即自动开通个人 workspace

**Files:**
- Modify: `server/internal/handler/auth.go`（`VerifyCode`，约 :376-459）
- Modify: `server/internal/handler/auth.go`（新增 `ensurePersonalWorkspace` 方法，或放同文件末尾）
- Create: `server/internal/handler/auth_personal_workspace_test.go`
- Create: `server/internal/handler/aurora.go`（若把 `ensurePersonalWorkspace` 放 aurora.go 也可；本计划放 `auth.go` 因其属账户域）

**Interfaces:**
- Consumes: Task 1 的 `h.Queries.CountWorkspacesForUser`、`h.TxStarter`、`h.Queries.WithTx`、`db.CreateWorkspaceParams`、`db.CreateMemberParams`、`issuestatus.Ensure`（`server/internal/issuestatus`）。
- Produces: `func (h *Handler) ensurePersonalWorkspace(ctx, userID pgtype.UUID, userName string) error`——用户无 workspace 时，事务内建单成员 owner workspace + 加 owner member + seed issue statuses；已有则直接返回 nil（幂等）。

- [ ] **Step 1: 写失败测试**

`server/internal/handler/auth_personal_workspace_test.go`（package `handler`）：

```go
package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// 直接测 ensurePersonalWorkspace：给一个全新 user，应恰好创建一个 owner workspace。
func TestEnsurePersonalWorkspaceProvisionsOnce(t *testing.T) {
	userID := dbfx.User(t, "Personal WS User", "personal-ws-"+t.Name()+"@multica.ai")
	// dbfx.User 返回 string id；转 pgtype.UUID
	uuid := parseUUID(userID)

	if err := testHandler.ensurePersonalWorkspace(context.Background(), uuid, "Personal WS User"); err != nil {
		t.Fatalf("ensurePersonalWorkspace: %v", err)
	}

	var n int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM workspace WHERE id IN (SELECT workspace_id FROM member WHERE user_id = $1)`,
		userID,
	).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 personal workspace, got %d", n)
	}

	// 幂等：再调一次不应新建第二个。
	if err := testHandler.ensurePersonalWorkspace(context.Background(), uuid, "Personal WS User"); err != nil {
		t.Fatalf("second ensurePersonalWorkspace: %v", err)
	}
	dbfx.QueryRow(t,
		`SELECT count(*) FROM workspace WHERE id IN (SELECT workspace_id FROM member WHERE user_id = $1)`,
		userID,
	).Scan(&n)
	if n != 1 {
		t.Fatalf("second call should not create another workspace, got %d", n)
	}
}

// 已有 workspace 的既有测试用户，不应被再建个人空间。
func TestEnsurePersonalWorkspaceSkipsExisting(t *testing.T) {
	// testUserID 在 TestMain 里已经属于 testWorkspaceID 的 owner。
	before := dbfx.Count(t,
		`SELECT count(*) FROM workspace WHERE id IN (SELECT workspace_id FROM member WHERE user_id = $1)`,
		testUserID,
	)
	if err := testHandler.ensurePersonalWorkspace(context.Background(), parseUUID(testUserID), "Handler Test User"); err != nil {
		t.Fatalf("ensurePersonalWorkspace: %v", err)
	}
	after := dbfx.Count(t,
		`SELECT count(*) FROM workspace WHERE id IN (SELECT workspace_id FROM member WHERE user_id = $1)`,
		testUserID,
	)
	if after != before {
		t.Fatalf("existing user workspace count changed: before=%d after=%d", before, after)
	}
}
```

> `dbfx.User(t, name, email)` 返回 string id（见 `server/internal/testutil/db.go:140`）；`dbfx.Count` 返回 int（`db.go:126`）。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd server && go test ./internal/handler/ -run TestEnsurePersonalWorkspace`
Expected: 编译失败（`ensurePersonalWorkspace` 未定义）。

- [ ] **Step 3: 实现 `ensurePersonalWorkspace`**

`server/internal/handler/auth.go` 末尾追加（import 需加 `"github.com/multica-ai/multica/server/internal/issuestatus"`）：

```go
// ensurePersonalWorkspace gives a newly-created user a single-member owner
// workspace, so Aurora signups land with a place to create generations without
// going through the manual workspace-creation flow. Idempotent: if the user
// already belongs to any workspace it is a no-op. Runs in one transaction so the
// workspace, owner membership and issue-status seed commit or roll back together.
func (h *Handler) ensurePersonalWorkspace(ctx context.Context, userID pgtype.UUID, userName string) error {
	existing, err := h.Queries.CountWorkspacesForUser(ctx, userID)
	if err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}

	name := strings.TrimSpace(userName)
	if name == "" {
		name = "Personal Workspace"
	} else {
		name = name + " 的个人空间"
	}
	slug := "user-" + strings.ReplaceAll(uuidToString(userID), "-", "")[:12]

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	ws, err := qtx.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        name,
		Slug:        slug,
		Description: ptrToText(nil),
		Context:     ptrToText(nil),
		IssuePrefix: defaultIssuePrefixFromSlug(slug),
	})
	if err != nil {
		return err
	}
	if _, err := qtx.CreateMember(ctx, db.CreateMemberParams{
		WorkspaceID: ws.ID,
		UserID:      userID,
		Role:        "owner",
	}); err != nil {
		return err
	}
	if err := issuestatus.Ensure(ctx, qtx, ws.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

> 需要确认几个点（以仓库实际为准，勿臆造）：
> - `CountWorkspacesForUser` 是单参数 `:one` count，sqlc 生成 `func (q *Queries) CountWorkspacesForUser(ctx context.Context, userID pgtype.UUID) (int64, error)`，直接 `existing, err := ...` 取值，无 `.Scan`（对照 Task 1 Step 3 生成签名，若签名不同按生成结果对齐）。
> - `db.CreateWorkspaceParams` 字段名以 `workspace.go:262` 用法为准（`Name/Slug/Description/Context/IssuePrefix`），`ptrToText` 是 handler 包私有 helper（`handler.go:610`）。
> - `defaultIssuePrefixFromSlug` 在 `workspace.go`（`CreateWorkspace` 同文件用到了它），是包内函数。
> - `issuestatus.Ensure(ctx, qtx, ws.ID)` 签名见 `workspace.go:291`。
> - slug 用 `user-` + 去连字符后的 userID 前 12 位，天然唯一；若 `uuidToString` 不可用，用 `userID.String()` 取前缀。

- [ ] **Step 4: 在 `VerifyCode` 接入**

`server/internal/handler/auth.go` `VerifyCode` 内，在 `findOrCreateUser` 成功、`isNew` 判断之后、`issueJWT` 之前（约 :427-431 之间）插入：

```go
	if isNew {
		if err := h.ensurePersonalWorkspace(r.Context(), user.ID, user.Name); err != nil {
			// Personal-workspace provisioning is best-effort: a failure must not
			// block login. The user can still create a workspace manually.
			slog.Warn("failed to provision personal workspace", "error", err, "user_id", uuidToString(user.ID))
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r)))
	}
```

> `user` 类型为 `db.User`；确认 `user.Name` 字段名（`db.User` 有 `Name`，见 models）。若 `findOrCreateUser` 返回的 `user` 没有 `Name` 字段，用空串调用（`ensurePersonalWorkspace` 内部会回落为 "Personal Workspace"）。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd server && go test ./internal/handler/ -run 'TestEnsurePersonalWorkspace'`
Expected: 两个测试 PASS。

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/auth.go server/internal/handler/auth_personal_workspace_test.go
git commit -m "feat(aurora): auto-provision personal workspace on signup"
```

---

## Self-Review

- **Spec 覆盖**：§5 的 `aurora_generation`/`aurora_asset` 表 → Task 1；§8 的 `GET /api/aurora/skills` 单一事实来源 → Task 2；§8 的 `POST /api/aurora/generations` → Task 3；§7 的「注册即开通个人空间」→ Task 4。§4 执行层 → Plan 3；§6 计费 → Plan 2；§8 前端 → Plan 4。**YAGNI 偏离（已在 Plan 1 声明）**：§5 的 `aurora_skill_catalog` 表改为 Go 常量，需后台改价时再加表。
- **占位符**：已消除 `<module>`/`<querier类型>` 等；唯一保留的「以 `make sqlc` 生成为准 / 以 `workspace.go` 用法为准」是**签名核对提示**，不是缺内容——因为 sqlc 生成字段名无法在写计划时 100% 确定，但 SQL 语义已定死，实现者按生成结果对齐即可。
- **类型一致性**：`SkillCatalogEntry` 字段在 catalog（Task 2 Step 3）与 handler 测试（Task 2 Step 5）一致；`CreateAuroraGenerationParams` 字段在 Task 1 SQL 与 Task 3 代码一致；`ensurePersonalWorkspace` 签名在 Task 4 测试与实现一致。

## 执行交接

Plan 1 已补全到零占位。剩余 Plan 2（计费）/Plan 3（执行层）/Plan 4（前端）尚未写。建议先按本计划实现并 `make test` 全绿，再写 Plan 2。
