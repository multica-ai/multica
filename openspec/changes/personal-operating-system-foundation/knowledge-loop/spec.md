# Knowledge Loop Spec

## 背景

Multica 已经有 Skill、issue description、comment、attachment、workspace context 等知识承载物。本能力不新建 Wiki，而是把已有知识对象接入执行闭环，让人和 agent 能复用团队上下文。

## 范围

Phase 1 包含：

- 将 `workspace.context` 注入 agent task context。
- 明确 Skill 是团队知识和 agent 指令的共同载体。
- 为后续 Skill visibility/search 留出设计边界。

Phase 1 不包含：

- 独立 Wiki。
- RAG 或全文语义搜索。
- issue/comment 自动总结成知识。
- 附件二进制导入导出。

## 当前状态

- 证据：`openspec/changes/m7-knowledge-management/spec.md` `2.2 推荐对象模型：Skill 作为团队知识单元`
- 当前行为：M7 已建议 Skill 作为团队知识单元。
- 当前缺口：M7 仍是纯 spec，没有实现任务入口。

- 证据：`server/internal/handler/skill.go` `CreateSkill` / `UpdateSkill` / `ImportSkill`
- 当前行为：Skill CRUD 和导入已存在。
- 当前缺口：Skill 没有 `visibility` 字段，也没有 team knowledge 搜索语义。

- 证据：`apps/workspace/src/shared/types/agent.ts` `Skill` / `CreateSkillRequest` / `UpdateSkillRequest`
- 当前行为：前端 Skill 类型没有 visibility。
- 当前缺口：无法区分 `agent_only` 和 `team`。

- 证据：`apps/workspace/src/features/search/use-search-results.ts` `useSearchResults`
- 当前行为：全局搜索只覆盖 issues、projects、members。
- 当前缺口：team skills 不可搜索。

- 证据：`server/internal/handler/daemon.go` `ClaimTaskByRuntime`
- 当前行为：claim response 已包含非空 `workspace.context` 为 `workspace_context`。
- 当前缺口：无。

- 证据：`server/internal/daemon/daemon.go` `runTask`
- 当前行为：daemon 已把 `task.WorkspaceContext` 传入 `execenv.TaskContextForEnv`。
- 当前缺口：无。

- 证据：`server/internal/daemon/execenv/runtime_config.go` `buildMetaSkillContent`
- 当前行为：非空 workspace context 已注入 provider runtime config；空白 context 不输出占位段落。
- 当前缺口：无。

- 证据：`server/internal/daemon/execenv/context.go` `renderIssueContext`
- 当前行为：非空 workspace context 已写入 `.agent_context/issue_context.md`；空白 context 不输出占位段落。
- 当前缺口：无。

## 缺口

1. Skill 对人类不可发现，不适合作为团队知识入口。
2. Issue/comment 中的决策沉淀没有轻量路径。
3. Data manifest 目前主要覆盖 issue，不覆盖 Skill 知识。

## 推荐功能切片

### K1. Workspace Context 注入 Agent

目标：agent 执行 issue 时能读取 workspace 背景。

当前状态：已完成。

最小行为：

- task claim 或 daemon prompt 构建链路传递 workspace context。
- prompt 中将 workspace context 放在独立段落。
- 空 context 不输出占位噪音。

完成定义：

- daemon 生成的 issue context 包含 workspace context。
- 现有 skill 注入行为不变。

验证记录：

- `cd server && go test ./internal/daemon/execenv`

### K2. Skill Visibility

目标：区分 agent-only skill 和 team-visible skill。

最小行为：

- Skill 增加 `visibility` 概念，推荐枚举：`agent_only`、`team`。
- 旧数据默认 `agent_only`，避免突然暴露 agent 指令。
- `/skills` UI 能展示和切换 visibility。

完成定义：

- API、DB、前端类型一致。
- `team` skill 可作为知识展示对象。

### K3. Skill Search

目标：让 team skills 可被人找到。

最小行为：

- `/skills` 页面内搜索 name/description/content。
- Global Search 纳入 `team` skills。
- 搜索结果跳转到稳定 Skill detail URL。

完成定义：

- 搜索不会返回 `agent_only` skill。
- Global Search 仍保留 issue/project/member 行为。

## 交接说明

实现阶段必须先做 K1，再考虑 K2/K3。K1 是 Phase 1 最小闭环的一部分；K2/K3 属于 Phase 2 起点，可以在 Attention/Energy P1 完成后推进。
