---
title: "feat: 支持 2-segment skills.sh URL 批量导入整个仓库的所有 skills"
type: feat
status: active
created: 2026-05-15
plan_depth: standard
---

# feat: 支持 2-segment skills.sh URL 批量导入整个仓库的所有 skills

## Problem Frame

**当前状态**：`skills.sh` URL 导入只支持 3-segment 格式 `https://skills.sh/{owner}/{repo}/{skill-name}`，每次只能导入单个 skill。

**目标**：支持 2-segment 格式 `https://skills.sh/{owner}/{repo}`，触发批量导入该仓库中找到的所有 `SKILL.md`。

**业务价值**：很多仓库包含多个 skills（如 `everyinc/compound-engineering-plugin` 包含 `compound-docs`, `compound-code` 等），用户希望能一次导入整个仓库的所有 skills。

---

## Key Technical Decisions

### 1. 冲突处理策略
**决策**：遇到已存在的 skill（unique violation）时跳过，不中断整个批量导入流程。
**理由**：用户导入整个仓库时，可能之前已经导入了部分 skills。跳过已存在的 skill 而不是让整个批次失败，提供更好的用户体验。

### 2. 返回值设计
**决策**：API 返回导入摘要，包含成功、跳过、失败的数量和详细信息。
**理由**：批量操作需要透明的结果反馈，让用户知道哪些 skills 被导入，哪些被跳过。

### 3. HTTP Timeout 调整
**决策**：批量模式 HTTP timeout 从 30s 提升到 120s。
**理由**：批量导入涉及多次 GitHub API 调用和文件下载，需要更长的超时时间。

### 4. 向后兼容性
**决策**：保持现有 3-segment URL 格式完全兼容，新增 2-segment 格式作为扩展。
**理由**：不能破坏现有的导入功能。

---

## Implementation Units

### U1. 扩展 URL 解析器支持 2-segment 格式

**Goal**：修改 `parseSkillsShParts` 函数，支持 2 或 3 segments 的 URL 格式。

**Requirements**：
- 2 segments (`{owner}/{repo}`) → 标记为批量模式
- 3 segments (`{owner}/{repo}/{skill-name}`) → 现有单 skill 导入逻辑

**Dependencies**：无

**Files**：
- `server/internal/handler/skill.go` — 修改 `parseSkillsShParts` 函数
- `server/internal/handler/skill_test.go` — 添加 2-segment URL 解析测试

**Approach**：
- 新增 `skillsShSpec` 结构体，包含 `Owner`, `Repo`, `SkillName`, `IsBatch` 字段
- 修改 `parseSkillsShParts` 返回 `skillsShSpec` 而不是单独的字符串
- 2 segments 时 `SkillName` 为空，`IsBatch` 为 true
- 3 segments 时保持现有行为

**Test scenarios**：
- Happy path: 解析 2-segment URL `skills.sh/owner/repo` → `IsBatch=true`
- Happy path: 解析 3-segment URL `skills.sh/owner/repo/skill` → `IsBatch=false, SkillName="skill"`
- Edge case: 解析 1-segment URL → 返回错误
- Edge case: 解析 4+ segments URL → 返回错误
- Edge case: 解析空 URL → 返回错误

---

### U2. 实现批量导入函数 `importAllSkillsFromRepo`

**Goal**：实现批量导入整个仓库所有 skills 的核心逻辑。

**Requirements**：
- 获取仓库默认分支
- 使用递归 tree scan (`?recursive=1`) 找到所有 `SKILL.md` 路径
- 下载每个 SKILL.md + 支持文件
- 返回 `[]*importedSkill`

**Dependencies**：U1

**Files**：
- `server/internal/handler/skill.go` — 新增 `importAllSkillsFromRepo` 函数
- `server/internal/handler/skill_test.go` — 添加批量导入测试

**Approach**：
- 复用现有的 `fetchGitHubDefaultBranch`, `fetchRawFile`, `parseSkillFrontmatter`, `collectGitHubFiles` 函数
- 使用 `resolveGitHubSkillDirByName` 的 tree scan 逻辑，但改为扫描所有 SKILL.md
- 对每个找到的 SKILL.md，解析 frontmatter 获取 skill 名称
- 为每个 skill 收集支持文件
- 返回 skills 数组

**Technical design**：
```
importAllSkillsFromRepo(owner, repo) → []*importedSkill
  ├── fetchGitHubDefaultBranch(owner, repo) → defaultBranch
  ├── GitHub API: /repos/{owner}/{repo}/git/trees/{defaultBranch}?recursive=1
  ├── 过滤所有 path 以 "SKILL.md" 结尾的条目
  ├── 对每个 SKILL.md:
  │   ├── 提取目录路径（如 "skills/compound-docs/"）
  │   ├── 下载 SKILL.md 内容
  │   ├── 解析 frontmatter 获取 name, description
  │   ├── collectGitHubFiles() 收集支持文件
  │   ├── 下载每个支持文件
  │   └── 构建 importedSkill 对象
  └── 返回 []*importedSkill
```

**Test scenarios**：
- Happy path: 仓库包含 3 个 skills → 返回 3 个 importedSkill
- Happy path: 仓库只包含 1 个 skill（根目录 SKILL.md）→ 返回 1 个 importedSkill
- Edge case: 仓库不包含任何 SKILL.md → 返回空数组，不报错
- Error path: GitHub API 失败 → 返回错误
- Error path: 仓库 tree 被截断 (truncated=true) → 返回错误或部分结果（需要评估）

---

### U3. 修改 ImportSkill Handler 支持批量模式

**Goal**：修改 `ImportSkill` handler，检测批量模式并循环导入每个 skill。

**Requirements**：
- 检测批量模式 vs 单 skill 模式
- 批量模式下调用 `importAllSkillsFromRepo`
- 循环调用 `createSkillWithFiles` 写入每个 skill
- 冲突处理：跳过已存在的 skill
- 返回导入摘要

**Dependencies**：U1, U2

**Files**：
- `server/internal/handler/skill.go` — 修改 `ImportSkill` handler
- `server/internal/handler/skill.go` — 新增批量导入响应结构体

**Approach**：
- 新增 `ImportSkillBatchResponse` 结构体：
  ```go
  type ImportSkillBatchResponse struct {
      Total   int                    `json:"total"`
      Created []*SkillResponse       `json:"created"`
      Skipped []SkippedSkillInfo     `json:"skipped"`
      Failed  []FailedSkillInfo      `json:"failed"`
  }
  ```
- 在 `ImportSkill` 中：
  1. 解析 URL 获取 `skillsShSpec`
  2. 如果 `IsBatch == true`：
     - 调用 `importAllSkillsFromRepo`
     - 循环每个 skill，调用 `createSkillWithFiles`
     - 捕获 unique violation 错误，记录到 Skipped 列表
     - 其他错误记录到 Failed 列表
     - 返回批量响应
  3. 如果 `IsBatch == false`：
     - 保持现有逻辑不变

**Test scenarios**：
- Happy path: 批量导入 3 个 skills，全部成功 → 返回 3 created
- Happy path: 批量导入 3 个 skills，1 个已存在 → 返回 2 created, 1 skipped
- Error path: 批量导入 3 个 skills，1 个失败 → 返回 2 created, 1 failed
- Integration: 批量导入后，所有 skills 可通过 list API 查询到

---

### U4. 添加端到端测试

**Goal**：为批量导入功能添加完整的测试覆盖。

**Requirements**：
- 单元测试：URL 解析、批量导入函数
- 集成测试：完整的导入流程（mock GitHub API）

**Dependencies**：U1, U2, U3

**Files**：
- `server/internal/handler/skill_test.go` — 添加批量导入相关测试

**Approach**：
- 使用 httptest 创建 mock HTTP server 模拟 GitHub API
- 测试 2-segment URL 解析
- 测试批量导入函数
- 测试 ImportSkill handler 的批量模式
- 测试冲突处理（跳过已存在的 skill）

**Test scenarios**：
- Happy path: 完整的 2-segment URL 导入流程
- Edge case: 空仓库（无 SKILL.md）
- Error path: GitHub API 超时
- Error path: 网络错误
- Conflict: 部分 skills 已存在

---

## Scope Boundaries

### In Scope
- 支持 `skills.sh` 的 2-segment URL 格式
- 批量导入整个仓库的所有 skills
- 冲突处理（跳过已存在的 skill）
- 返回导入摘要

### Out of Scope
- `clawhub.ai` 或 `github.com` 的批量导入（后续可扩展）
- 异步任务 ID（对于大仓库的进度反馈）
- 增量导入（只导入新增的 skills）
- 批量删除或更新 skills

---

## Risks

| Risk | Impact | Likelihood | Mitigation |
|------|--------|------------|------------|
| 大仓库 tree 被截断 (truncated=true) | High | Low | 记录警告，返回已找到的 skills，不报错 |
| 批量导入超时 | Medium | Medium | 提升 timeout 到 120s，监控实际耗时 |
| 部分 skill 导入失败导致状态不一致 | Medium | Low | 逐个导入，失败不影响其他 skills |
| 前端未适配批量响应格式 | Medium | Low | API 保持向后兼容，单 skill 导入返回原有格式 |

---

## Verification

1. **单元测试**：所有新增函数有单元测试覆盖
2. **集成测试**：mock GitHub API 测试完整导入流程
3. **手动测试**：
   - 使用 `skills.sh/everinc/compound-engineering-plugin` 测试批量导入
   - 验证所有 skills 被正确导入
   - 验证已存在的 skills 被跳过
   - 验证导入摘要格式正确
4. **回归测试**：现有 3-segment URL 导入功能不受影响

---

## Deferred to Follow-Up Work

- `clawhub.ai` 和 `github.com` 的批量导入支持
- 异步任务 ID 和大仓库进度反馈
- 增量导入（只导入新增的 skills）
- 前端 UI 适配批量导入结果展示
