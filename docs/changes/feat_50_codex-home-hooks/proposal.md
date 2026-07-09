<!-- doc-init template version: v1.0 -->
# Proposal: codex-home-hooks

- **Owner**: 需求官 (on behalf of 用户)
- **Reviewers**: 编排官, 用户, 技术方案官
- **创建日期**: 2026-07-09
- **状态**: Proposal
- **关联 Issue**: RAS-50《修复 Multica Codex per-task CODEX_HOME 未继承用户 hooks.json 的问题》
- **参考实现**: GitHub PR #3112（仅 `6f3e36e3d` / `9e031356f` / `aab3a43d6` 三个 hook 相关提交有效，其余 daemon token / system comment / control-plane 改动与本任务无关，不照搬）

## 1. Why（动机）

Multica daemon 启动 Codex 时，为每个 task 构造隔离的 per-task `CODEX_HOME`（而非直接复用用户全局 `~/.codex`），以此隔离跨任务状态并承载 daemon 管理的 sandbox / memory / multi-agent / MCP / skills 配置。当前 `origin/main` 的准备逻辑（`server/internal/daemon/execenv/codex_home.go`，head `3f02083fe` 已确认存在）已处理 `auth.json`（symlink）、`sessions/`（symlink）、`config.json` / `config.toml` / `instructions.md`（copy）、skills（hydrate）、MCP（写入 managed block）。

**痛点**：用户全局 `~/.codex/hooks.json` 及常见的 `~/.codex/hooks/` helper scripts **未被暴露**到 per-task `CODEX_HOME`。因此 Multica app-server 路径启动的 Codex 不会进入用户配置的 Flux hook，上报链路缺失；临时旁路 reporter 只能兜底，不应作为长期主链路。

**期望状态**：在 Codex `CODEX_HOME` 准备阶段补齐 hook 继承，使 Multica 启动的 Codex session 能正常加载用户 hooks，同时**完整保持现有 per-task isolation 设计**。

## 2. What's Changing（高层变更）

| Capability | 变化类型 | 简述 |
|---|---|---|
| `codex-home-hooks` | ADDED | per-task `CODEX_HOME` 准备阶段新增：暴露用户 hooks 资源 + 映射 hook trust state + 幂等保证 hooks feature flag，并保持隔离边界 |

**新增 capability**：
- `codex-home-hooks`：在 Codex per-task home 准备/复用阶段，把用户全局 hooks（`hooks.json` + `hooks/`）作为 optional shared resource 继承进来，并正确处理 trust state 路径映射与 feature 开关，且不越过 isolation 边界。

> 说明：本 capability 是对既有「Codex per-task CODEX_HOME 准备」行为的增量扩展。本仓库 `docs/specs/` 尚未建立该 capability 的 living spec，故本 change 以 spec-delta 的 `ADDED Requirements` 形式落地（详见 `specs/codex-home-hooks/spec.md`）；归档时并入 living spec。

## 3. Out of Scope（明确不做）

- **不改变现有已处理资源的语义**：`auth.json` / `sessions/` 的 symlink、`config.json` / `config.toml` / `instructions.md` 的 copy、skills hydrate、MCP managed block 一律不动。
- **不暴露整个 `~/.codex`** 给 task（只暴露 hooks 相关的两项资源）。
- **不改变 sandbox / memory / multi-agent / MCP / skills / auth / session / config 的既有行为**。
- **不照搬 PR #3112 的非 hook 改动**（daemon token、system comment、control-plane 等）。
- **不新建 Flux reporter 旁路链路**；本 change 只负责让 native hook 链路可用，旁路 reporter 的去留由后续决定。
- **不引入新的用户可见配置项 / 接口**；纯 daemon 内部 home 准备逻辑增强。

## 4. Stakeholders

| 角色 | 关注点 | Review 必需 |
|---|---|---|
| 技术方案官 | 承接本 proposal 产出 `design.md`，落地到 `codex_home.go` 的具体实现路径 | 是 |
| 开发官 | 实现与单测 | 是 |
| 用户 | 隔离边界红线（不泄露 Flux cookie/token/socket）、验收环境结果 | 是 |
| 编排官 | 流水线编排与阶段验收 | 是 |

## 5. Success Metrics（成功指标）

- **功能可用**：新的 Multica Codex task 使用 per-task `CODEX_HOME` 时，`codex-home/hooks.json` 可见，必要的 `hooks/` helper scripts 可见（0 → 1）。
- **trust 生效**：hook trust state 在 per-task `config.toml` 中对 `codex-home/hooks.json` 路径生效（Codex 不再因 trust key 路径不匹配而拒绝加载用户 hook）。
- **reuse 无 stale**：workdir / env reuse 后，shared hook 的新增 / 修改 / 删除都 100% 反映到 per-task home，不残留 stale hook。
- **测试全绿**：`server/internal/daemon/execenv` 相关 Go 单测全部通过。
- **上报链路收敛（自托管开发测试环境验证）**：新 Codex session 仍保持 `originator=multica-agent-sdk` / `source=vscode`；Flux hook 日志可见 Codex native hook `raw` / `result` / `reportTokenUsage` 链路；Grafana 中 Codex native `used` 与 `reported` 在有使用量窗口内收敛。

## 6. Clarifications（由 Clarify 阶段填充）

> 本 change 的原始 Issue（RAS-50）已是一份完整规格（背景 / 目标 / 实现要求 5 项 / 验收标准俱全），本阶段以其为权威输入规整，无新增待澄清项。技术实现层面的开放问题（如「当前 Codex 版本是否仍需 `features.hooks=true`」）交由技术方案官在 `design.md` 阶段核实并给出结论。

### Q1: 当前 Codex 版本是否仍需要 `[features] hooks = true` 才能启用 hook？
**A**: 待技术方案官核实当前 Codex 版本行为后在 design.md 定论。**若需要**：在 per-task `config.toml` 中以幂等方式保证 `[features] hooks = true`（不破坏已有 `[features]` table、不重复写 table）。**若不需要**：显式说明并跳过，避免无谓写入。
**影响**: 决定 Requirement 3 是否落地为实际写操作，以及对应测试用例是否需要。

## 7. 风险

| 风险 | 可能性 | 影响 | 缓解 |
|---|---|---|---|
| 误映射 `plugin@local:...` 这类 plugin hook trust state | 中 | 高（污染 per-task trust，可能错误信任非用户预期 hook） | Requirement 2 明确只映射 shared hooks path 对应 block，排除 `plugin@local:` 前缀；测试用例专项覆盖 |
| trust key 路径不匹配导致 Codex 静默不加载用户 hook | 高 | 高（功能形同未做） | Requirement 2 做 shared→task hooks path 映射；验收在真实环境确认 native hook 链路 |
| workdir/env reuse 后残留 stale hook（源已删仍加载旧 hook） | 中 | 中（行为不符预期，安全上加载已删除脚本） | Requirement 1 & 2 要求 reuse 时先清理旧 stale link/copy 与 mapped trust block，再按 shared 现状重建；测试覆盖删除后 reuse |
| 破坏现有已处理资源语义 / isolation 边界 | 低 | 高（回归，破坏隔离设计） | Out of Scope 明确边界；Requirement 4 列为红线；改动小而集中，rebase 到最新 main |
| 把 Flux cookie / token / socket 等本机私密写入 git 或 issue metadata | 低 | 高（泄密） | Requirement 4 红线；只暴露 hooks 两项资源，不 dump 私密到版本库或 metadata |
| 破坏已有 `[features]` table 或重复写 table | 中 | 中（config.toml 损坏或冗余） | Requirement 3 要求幂等改写，测试覆盖「已有 table / 无 table / 重复运行」三态 |
