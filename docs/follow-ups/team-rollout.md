# Team rollout — 决策记录与 follow-up 清单

> 开发期工作文档(非 apps/docs 官方文档)。记录 team 功能的已定模型、正在做的事、以及容易被忘掉的后续项。
> 最后更新:2026-07-03(pr-4784-fix 分支讨论)

## 已定模型(核心原则)

**一切关联只在"创建时刻"起作用(做默认值),之后互不约束。**

- issue ↔ team:一对一,唯一被强制的归属;team 决定 identifier 命名空间(编号 per-team 分配)。
- 父 issue ↔ 子 issue:创建子 issue 时默认继承父的 team,之后各自独立;移动父 issue 子 issue 不跟随。(与 Linear 一致:官方文档明确 sub-issue 可属于任意 team,继承仅发生在创建时。)
- project ↔ team:创建 issue 时用于推断默认 team;之后 issue 换 team **project 保留不动**(比 Linear 更简 —— Linear 移动时会自动移除不兼容 project,因为它有 per-team workflow/labels/cycles,我们没有)。
- assignee 与 team 完全解耦:assignee 池 = workspace 成员 + agents,不按 team 过滤(v1 刻意如此,成员管理闭环缺失时过滤只会把人筛没)。
- 权限:team 是组织标签,不是可见性边界;issue 可见性仍只由 workspace membership 决定。

**换 team 的清理准则**(将来加 team 级属性时照此办理):
只有"取值空间由 team 决定"的属性才需要在换 team 时清理/映射。
当前唯一此类属性是编号/identifier(用重编号 + 别名表解决);status/priority/labels/project 均为 workspace 级,换 team 一概不动。

## 正在实现(move to team,本分支)

1. `issue_identifier_alias` 表:换 team 重编号后,旧 identifier(如 `MUL-3`)仍可解析到该 issue(Linear `previousIdentifiers` 的轻量版)。两个解析点加 fallback:
   - `handler.go` `resolveIssueByIdentifier`(CLI / API 按 identifier 查)
   - `github.go` `lookupIssueByIdentifier`(分支名/PR → issue 自动关联,不加会直接坏)
2. `UpdateIssue` 支持 `team_id`:校验目标 team active → `IncrementTeamIssueCounter` 重新取号 + team 内重新定位 → 写旧号别名 → 广播。
3. 删除与新模型矛盾的校验(同一状态不允许 A 路径能到、B 路径 400):
   - 创建/改 parent 时的父子同 team 校验(`ErrCrossTeamChild`)
   - 创建时显式 team × project 关联校验(`ErrProjectTeamMismatch`)
   - 改 issue project 时"project 与 issue 团队无关联" 400
   - project 移除 team 时"还有 issue" 409
4. 前端换 team 入口:右键/三点共用菜单体(`issue-actions-menu-items.tsx`)加 Team 子菜单(第一位);issue 详情右栏 Properties 首行加 Team;创建弹窗解除 `allowedTeamIds` 限制与 sub-issue 锁定,降级为默认值种子。
5. 换 team 不做乐观编号(新号由服务端分配),等响应/invalidate。

## Follow-ups(防遗忘清单)

### 缓存正确性(move to team 落地后从"优化"变"必须")
- [ ] WS `issue:updated` 增加 `teamChanged` flag(后端 meta 目前只有 assignee/status/project)。
- [ ] `packages/core/issues/surface/membership.ts` 增加 team 维度(`IssueChangedDims`/`listFilterDependsOn`/`issueMatchesListFilter` 目前都不含 team);cache-coordinator 注释已预留插槽(`cache-coordinator.ts` "and future team")。

### Sidebar 重构(方案已定,分两步)
- [ ] 后端 membership 闭环:AcceptInvitation 自动入默认团队;CreateTeam 创建者自动成为 lead;`workspace_team_member.sort_order DOUBLE PRECISION`(fractional ordering,Linear 同构:TeamMembership.sortOrder);`GET /api/teams` 响应加 `is_member`/`sort_order`;`PATCH /api/teams/{id}/membership` 单行改排序。
- [ ] team scope 打通:`packages/core/issues/surface/query-plan.ts:166` 目前对 team scope 抛 `UnsupportedIssueScopeError`;加 `/team/:key/issues|projects|automations` 路由三件套(paths + web + desktop)。
- [ ] sidebar 本体:`Workspace ▾`(Projects/Agents/Squads/Autopilots/Skills/More▾(Usage/Runtimes))+ `Teams ▾`(只显示我加入的、按我的 sort_order、可拖拽 —— 复用 Pinned 组 dnd-kit 基建)+ Settings 底部;全局 Issues 移出导航;折叠态 Zustand persist(按 wsId)。
- [ ] 创建 issue 默认 team 从"默认团队"切到"我的排序第一个 team"(取值函数替换即可,种子链已铺好)。

### 待拍板(讨论过但未定)
- [ ] quick-create 的 `lastTeamId` 记忆与"排序第一 = 默认"规则冲突,推荐删记忆(排序上线时一并处理)。
- [ ] v1 无加入团队机制:"只显示我加入的" + "没有 join API" = 别人建的团队对我不可见。推荐最小闭环:建团队对话框加成员多选 + `POST /api/teams/{id}/members`。

### 将来做 team 级属性时
- [ ] label 下放到 team 维度时:换 team 逻辑里加"移除/映射目标 team 不存在的 label"(参照 Linear:team labels → Removed)。
- [ ] 若引入 per-team workflow/cycle,同理加映射/清理。

### 小项
- [ ] 别名表的锦上添花:搜索输入旧 identifier 也能命中(目前只做解析 fallback)。
- [ ] Team icon/color 自定义上传;落点在 `packages/views/teams/components/team-icon.tsx`(当前蓝色默认块即占位,`bg-blue-500` 待 `team.color` 数据驱动替换)。
- [ ] 批量 move to team(batch update 支持 `team_id`,前端批量工具栏加入口)。
- [ ] 撤销:Linear 移动支持 Cmd+Z,我们无 undo 体系,记为已知差异。
- [ ] `apps/docs/content/docs/developers/conventions.mdx:73` issue 编号章节仍是旧语义(workspace 前缀、最长 10 位、改前缀重编号),需按 team key(≤7 位、per-team 编号、编号不变)重写。
- [ ] issue 详情页 team 字段展示(`team_key`/`team_name` 有类型无 UI 消费点)—— 本次加 Properties 行后即覆盖,验证后可勾掉。

## 全产品面扫描补充(2026-07-03,对照 Linear)

### P1 — 用户会很快撞上
- [ ] 邀请成员时选团队(Linear 同款):Invitation 加 team_ids,AcceptInvitation 按其入队(现在只进默认团队)。成员管理闭环的最大剩余豁口。
- [ ] 全局搜索(⌘K)的 team facet:按 team 过滤/分组搜索结果。
- [ ] 移动端 team parity:identifier 显示、team 筛选、创建归属(mobile 目前只有 schema 字段)。按 apps/mobile/CLAUDE.md 流程单独排期。

### P2 — 架构已留位,按需接线
- [ ] Slack 频道 ↔ team 路由:channel 配置加 team_id,`/issue` 建到对应团队(quick-create 已支持显式 team,纯配置接线;现在一律默认团队)。
- [ ] 订阅整个 team(入队即默认订阅该团队新 issue)—— membership 表已就位,通知系统的自然延伸。
- [ ] team 级 dashboard/usage 统计(数据列已在,加 group-by)。
- [ ] 团队 unarchive(现在只能归档不能恢复;一个端点 + 列表按钮)。
- [ ] GitHub 仓库 ↔ team 关联(Linear 用于自动化路由;暂无必要)。

### 有意不做(决策记录,勿误判为遗漏)
- per-team workflow/状态、team 级 labels、cycles:Linear 换 team 清理复杂度的全部来源;我们 status/labels 为 workspace 级是刻意简化(清理判定准则见上文)。
- 私有团队(team 作为可见性边界):v1 定为纯组织标签;若做,动整个查询层权限模型,属独立大项目。
- squad/agent 不归属 team:workspace 资产、跨团队服务;其名下 issue 各有各的 team,无冲突。
