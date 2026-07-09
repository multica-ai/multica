<!-- doc-init template version: v1.0 -->
# Capability Delta: codex-home-hooks

- **Change**: codex-home-hooks
- **Owner**: 需求官 (on behalf of 用户)
- **基于 living spec 版本**: 无（`docs/specs/` 尚未建立本 capability 的 living spec；本 change 首次以 ADDED 需求引入，归档时并入 living spec）
- **实现载体**: `server/internal/daemon/execenv/codex_home.go`（及其 `createFileLink` / `createDirLink` 助手，位于 `codex_home_link.go` / `codex_home_link_windows.go`）
- **测试载体**: `server/internal/daemon/execenv/`（`execenv_test.go` / `codex_home_link_test.go` 或同包新增测试）

> Delta 按 Requirement 粒度区分动作。本 change 全部为 ADDED。
> 各 Scenario 的 `TBD(...)` 占位已由开发官（RAS-50 开发测试阶段）替换为真实测试标识（`server/internal/daemon/execenv/` 下的 `codex_home_hooks_test.go` / `codex_hook_trust_test.go`）。Requirement「幂等保证 hooks feature flag」依 design.md Q1 结论落「不写入」分支，其 3 个 Scenario 标注「不适用」。

## ADDED Requirements

### Requirement: 暴露用户 hooks 为 optional shared 资源

WHERE 用户全局 `~/.codex/hooks.json` 存在且为常规文件 THE SYSTEM SHALL 在 per-task `CODEX_HOME` 中以 `codex-home/hooks.json` 暴露该文件，优先使用 symlink（复用现有 `createFileLink` 的 Windows copy fallback 能力）。

WHERE 用户全局 `~/.codex/hooks/` 存在且为目录 THE SYSTEM SHALL 在 per-task `CODEX_HOME` 中以 `codex-home/hooks/` 暴露该目录，优先使用 symlink（复用现有 `createDirLink` 的 Windows fallback 能力）。

IF 源 `~/.codex/hooks.json` / `~/.codex/hooks/` 不存在或类型不符（如期望文件却为目录、期望目录却为文件）THEN THE SYSTEM SHALL 不在 per-task home 创建空资源，且清理 per-task 中旧的 stale link/copy，避免 workdir reuse 后继续加载已删除的 hook。

#### Scenario: 首次 prepare 暴露 hooks.json 与 hooks/ helper
- **GIVEN** 一个含 `hooks.json` 与 `hooks/<helper>.sh` 的 fake shared home
- **WHEN** `prepareCodexHome` 执行
- **THEN** per-task `codex-home/hooks.json` 可见，且 `codex-home/hooks/<helper>.sh` 可见（symlink 或 Windows fallback copy）

**覆盖测试**: `server/internal/daemon/execenv/codex_home_hooks_test.go::TestPrepareCodexHomeExposesHooks`

#### Scenario: 源 hooks 删除后 reuse 清理 stale 资源
- **GIVEN** 一次已暴露 hooks 的 per-task home，随后 shared `hooks.json` / `hooks/` 被删除
- **WHEN** 再次 prepare / reuse 同一 per-task home
- **THEN** per-task 中对应的 `hooks.json` / `hooks/` stale link/copy 被清理，不再存在

**覆盖测试**: `server/internal/daemon/execenv/codex_home_hooks_test.go::TestPrepareCodexHomeClearsStaleHooksOnReuse`

#### Scenario: 源不存在时不建空资源
- **GIVEN** 一个不含任何 hooks 的 fake shared home
- **WHEN** `prepareCodexHome` 执行
- **THEN** per-task home 中不出现空的 `hooks.json` 或空 `hooks/` 目录

**覆盖测试**: `server/internal/daemon/execenv/codex_home_hooks_test.go::TestPrepareCodexHomeSkipsMissingHooks`（另：非 ENOENT stat 错误 fail-closed 清 stale 由 `codex_home_hooks_test.go::TestOptionalSymlinkFailsClosedOnStatError` 覆盖，对应 design D2）

### Requirement: 映射 hook trust state 路径

WHEN 准备或复用 per-task `CODEX_HOME` 且 shared `config.toml` 含与 shared hooks 源路径对应的 `[hooks.state."<shared-hooks-path>"]` trust block THE SYSTEM SHALL 从 shared `config.toml` 提取该 trust block，并以 per-task 实际加载路径（`codex-home/hooks.json`）为 key 映射写入 per-task `config.toml`。

WHEN 每次 prepare / reuse THE SYSTEM SHALL 先移除 per-task `config.toml` 中旧的 task-hooks mapped trust block，再按 shared config 当前状态重建，保证同步幂等（重复运行不追加重复 block）。

IF shared hooks（及其 trust state）缺失 THEN THE SYSTEM SHALL 清理 per-task `config.toml` 中已 mapped 的 trust state。

IF trust block 的 key 属于 `plugin@local:...` 这类 plugin hook trust state THEN THE SYSTEM SHALL 不将其映射到 per-task hooks path（只映射用户 shared `hooks.json` 源路径对应的 block）。

#### Scenario: shared trust state 映射到 task hooks path
- **GIVEN** shared `config.toml` 含 `[hooks.state."<shared ~/.codex/hooks.json path>"]` trust block
- **WHEN** `prepareCodexHome` 执行
- **THEN** per-task `config.toml` 出现以 `codex-home/hooks.json` 路径为 key 的等价 trust block

**覆盖测试**: `server/internal/daemon/execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustStateMapsSharedHooksJSONSource`、`codex_hook_trust_test.go::TestPrepareCodexHomeMapsCodexHookTrustStateFromSharedConfig`（prepare 级端到端）

#### Scenario: 映射逻辑幂等
- **GIVEN** 已完成一次 trust state 映射的 per-task `config.toml`
- **WHEN** 再次 prepare / reuse（shared config 未变）
- **THEN** per-task `config.toml` 中 mapped trust block 数量不变，不重复追加

**覆盖测试**: `server/internal/daemon/execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustStateMapsSharedHooksJSONSource`（含连跑两次不重复追加断言）、`codex_home_hooks_test.go::TestPrepareCodexHomeHookTrustCoexistsWithManagedBlocks`（prepare/reuse 每块唯一）

#### Scenario: shared trust state 变更后 reuse 刷新
- **GIVEN** 已映射的 per-task home，随后 shared `config.toml` 的 trust block 内容变更
- **WHEN** reuse 同一 per-task home
- **THEN** per-task mapped trust block 刷新为 shared 最新内容（旧 block 被替换）

**覆盖测试**: `server/internal/daemon/execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustStateRefreshesMappedBlocksFromSharedConfig`、`codex_hook_trust_test.go::TestReuseRefreshesCodexHookTrustStateFromSharedConfig`（Reuse 级刷新）

#### Scenario: shared hooks 缺失时清理 mapped trust state
- **GIVEN** 已映射 trust state 的 per-task home，随后 shared hooks 及其 trust state 被移除
- **WHEN** 再次 prepare / reuse
- **THEN** per-task `config.toml` 中的 mapped trust block 被清理

**覆盖测试**: `server/internal/daemon/execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustStateClearsMappedBlocksWhenHooksMissing`

#### Scenario: 不误映射 plugin hook trust state
- **GIVEN** shared `config.toml` 同时含用户 hooks 的 trust block 与 `plugin@local:...` 的 trust block
- **WHEN** `prepareCodexHome` 执行
- **THEN** per-task `config.toml` 只出现用户 hooks path 的映射，`plugin@local:...` trust state 不被映射

**覆盖测试**: `server/internal/daemon/execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustStateSkipsPluginTrustState`（另 `TestSyncCodexHookTrustStateMapsSharedHooksJSONSource` 亦断言混合场景下 plugin 不被映射）

### Requirement: 幂等保证 hooks feature flag

WHERE 当前 Codex 版本需要 `[features] hooks = true` 才能启用 hook（由技术方案官在 design 阶段核实）THE SYSTEM SHALL 在 per-task `config.toml` 中以幂等方式保证 `[features] hooks = true`，不破坏已有 `[features]` table 内的其他键，且不重复写入 `[features]` table。

IF 当前 Codex 版本不再需要该 feature flag THEN THE SYSTEM SHALL 不写入该键（在 design.md 记录核实结论）。

#### Scenario: 已有 [features] table 时就地补键
- **GIVEN** per-task `config.toml` 已含 `[features]` table 且带有其他键但无 `hooks`
- **WHEN** 保证 feature flag 逻辑执行
- **THEN** `hooks = true` 被补入既有 `[features]` table，其他键保留，未新建重复 table

**覆盖测试**: 不适用（依 design.md Q1 结论：Codex 0.141.0 `hooks` 已 stable/默认 true，daemon 不写 `[features] hooks=true`，本 Requirement 落「不写入」分支，无对应实现与测试）

#### Scenario: 无 [features] table 时新建
- **GIVEN** per-task `config.toml` 不含 `[features]` table
- **WHEN** 保证 feature flag 逻辑执行
- **THEN** 新建 `[features]` table 且含 `hooks = true`

**覆盖测试**: 不适用（依 design.md Q1 结论，不写入 features flag，见上）

#### Scenario: 重复运行幂等
- **GIVEN** 已保证 `[features] hooks = true` 的 per-task `config.toml`
- **WHEN** 再次运行保证逻辑
- **THEN** `config.toml` 无重复 `[features]` table 或重复键，内容稳定

**覆盖测试**: 不适用（依 design.md Q1 结论，不写入 features flag，见上）

### Requirement: 保持 Codex per-task 隔离边界

THE SYSTEM SHALL 只暴露 hooks 相关的 `hooks.json` 与 `hooks/` 两项资源，不得把整个 `~/.codex` 目录暴露给 task。

THE SYSTEM SHALL 不把用户 hook 工具链的本机私密（cookie、token、socket 等）信息写入 git 版本库或 issue metadata。

THE SYSTEM SHALL 不改变现有 `auth` / `session` / `config` / `skills` / `MCP` / `sandbox` / `memory` / `multi-agent` 的既有语义。

#### Scenario: 隔离边界不被扩大
- **GIVEN** 完成 hook 继承后的 per-task `CODEX_HOME`
- **WHEN** 检查 per-task home 内容与既有资源处理
- **THEN** per-task home 仅新增 hooks.json / hooks/ 两项资源；auth.json/sessions/config/skills/MCP 等既有资源的暴露方式与语义未变；无整份 `~/.codex` 暴露，无私密信息落入版本库

**覆盖测试**: `server/internal/daemon/execenv/codex_home_hooks_test.go::TestPrepareCodexHomeDoesNotExposeWholeCodexHome`

## 关联

- 关联 Issue: RAS-50
- 关联 proposal: [../../proposal.md](../../proposal.md)
- 关联 design: [../../design.md](../../design.md)（由技术方案官后续产出）
- 参考实现: GitHub PR #3112（仅 `6f3e36e3d` / `9e031356f` / `aab3a43d6` 三个 hook 提交有效）
