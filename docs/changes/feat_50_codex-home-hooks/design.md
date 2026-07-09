<!-- doc-init template version: v1.0 -->
# Design: codex-home-hooks

- **Owner**: 技术方案官 (on behalf of 用户)
- **Reviewers**: 编排官, 用户, 开发官
- **创建日期**: 2026-07-09
- **基于 proposal**: [proposal.md](./proposal.md)
- **关联 spec (spec-delta)**: [specs/codex-home-hooks/spec.md](./specs/codex-home-hooks/spec.md)
- **关联 Issue**: RAS-50
- **Constitution check**: 本仓库不存在 `docs/overview/constitution.md`（doc-init 宪法文件未建立），故无宪法红线可对照；本 change 亦未引入任何需要宪法豁免的破坏性变更。若后续建立 constitution，本 change 的隔离边界主张（§7）应作为 candidate 红线纳入。
- **参考实现**: GitHub PR #3112，仅 `6f3e36e3d` / `9e031356f` / `aab3a43d6` 三个 hook 提交有效（其余 daemon token / system comment / control-plane 改动不照搬）

## 1. 概述

在 Codex per-task `CODEX_HOME` 准备阶段（`server/internal/daemon/execenv/codex_home.go` 的 `prepareCodexHomeWithOpts`）新增 **hook 继承**，使 Multica 启动的 Codex session 能加载用户全局 `~/.codex/hooks.json` 与 `~/.codex/hooks/` helper，同时完整保持既有 per-task 隔离设计。

核心思路 = 把参考 PR #3112 的三个 hook 提交**抽取 + rebase 到最新 `main`**，并在此基础上**按 Codex 对抗式评审的正确性意见做 5 处强化**（fail-closed stale 清理、写前 TOML parse 校验、trust-sync 置于 config 变更末尾、Windows junction 安全清理、日志只记路径/计数）。方案由三个可独立测试的动作组成：

1. **暴露 hooks 资源**：`hooks.json`（文件，optional symlink）+ `hooks/`（目录，optional symlink），源缺失/类型不符/不可验证时 **fail closed 清 stale**，不建空资源。
2. **映射 hook trust state 路径**：把 shared `config.toml` 中 shared hooks 源路径对应的 `[hooks.state."..."]` block，以 per-task hooks 路径为 key **幂等重建**写入 per-task `config.toml`；不误映射 `plugin@local:...`。
3. **不写 `[features] hooks = true`**：经双信道独立核实，当前 Codex（0.141.0）`hooks` 已是 `stable`、默认 `true`，无需该 flag（详见 §3 Q1）。

**幂等性机制**：每次 `prepare` / `reuse` 都是"从 shared 现状全量重新推导" —— config.toml 先被 `syncCopiedFile` 从 shared 重拷，hooks 资源按 shared 现状重建/清理，trust block 先移除旧 task 映射再按 shared 重建。因此无论运行多少次，per-task 状态恒等于"shared 当前状态的映射"，天然无残留、无重复。

## 2. 架构与方案

### 2.1 现状（`origin/main` head `3f02083fe`，已核对）

`prepareCodexHomeWithOpts` 现有处理（`codex_home.go:61`）：
- `codexSymlinkedDirs = ["sessions"]` → `ensureDirSymlink`（源不存在则创建）
- `codexSymlinkedFiles = ["auth.json"]` → `ensureSymlink`
- `codexCopiedFiles = ["config.json","config.toml","instructions.md"]` → `syncCopiedFile`（reuse 时重拷/删除跟随 shared，MUL-2646）
- `sanitizeCopiedCodexConfig` / `syncCodexModelCatalog` / `exposeSharedCodexPluginCache`
- `ensureCodexSandboxConfig` / `ensureCodexMultiAgentConfig` / `ensureCodexMemoryConfig`（各自幂等改写 config.toml 的 managed block）
- 跨平台 link 助手：`createFileLink` / `createDirLink`（`codex_home_link.go` 非 Windows 直接 `os.Symlink`；`codex_home_link_windows.go` symlink 失败则 `mklink /J` junction 或 copy fallback）

### 2.2 动作 1：暴露 hooks 资源（对应 Requirement「暴露用户 hooks 为 optional shared 资源」）

在 `codex_home.go` 新增两个 optional 资源清单与三个助手（抽取自 `6f3e36e3d`，并按评审强化）：

```go
// 仅当 shared 源存在且类型正确时暴露；否则清 per-task stale，不建空资源。
var codexOptionalSymlinkedDirs  = []string{"hooks"}
var codexOptionalSymlinkedFiles = []string{"hooks.json"}
```

- `ensureOptionalFileSymlink(src, dst)`：`os.Stat(src)`
  - `IsNotExist` → `removeOptionalPath(dst)`（清 stale，无源不建）
  - **非 ENOENT 错误（权限 / I/O）→ `removeOptionalPath(dst)` 后返回该错误（fail closed，见 §3 决策 D2）**
  - 非 regular file（源是目录等类型不符）→ `removeOptionalPath(dst)`
  - 源合法 → 若 dst 已是指向 src 的 symlink 则 no-op；否则移除旧 dst（symlink 用 `os.Remove`，其它用 `os.RemoveAll`）后 `createFileLink(src, dst)`
- `ensureExistingDirSymlink(src, dst)`：目录版，逻辑同上（fail-closed 强化同样适用），refresh 走 `createDirLink`
- `removeOptionalPath(path)`：`Lstat`，symlink → `os.Remove`；否则 `os.RemoveAll`；不存在 → no-op

调用点插入在现有 symlink 循环之后、`logCodexAuthState` 之前：optional dir 循环用 `ensureExistingDirSymlink`，optional file 循环用 `ensureOptionalFileSymlink`，失败仅 `logger.Warn` 不中断（与既有资源同款容错）。

### 2.3 动作 2：映射 hook trust state 路径（对应 Requirement「映射 hook trust state 路径」）

新增 `codex_hook_trust.go`（抽取自 `9e031356f` + `aab3a43d6`），核心 `syncCodexHookTrustStateWithResult(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath)`：

1. 读 per-task `config.toml`（不存在按空串）。
2. `removeHooksStateBlocks(content, taskHooksPath)`：逐行扫描，删除所有 key 以 `taskHooksPath + ":"` 为前缀的 `[hooks.state."..."]` block（block 体 = 到下一个 table header 为止）。→ **幂等的关键：先无条件清旧 task 映射**。
3. 仅当 **shared 与 task 的 `hooks.json` 都是 regular file** 时：读 shared `config.toml`，`extractHooksStateBlocks(shared, sharedHooksPath)` 提取 key 以 `sharedHooksPath + ":"` 为前缀的 block，`appendMappedHooksStateBlocks` 把每个 block 以 `taskHooksPath + <原 suffix>` 为新 key 追加。
4. **[评审强化 D3] 写前 TOML parse 校验**：对最终文本做 `toml.Unmarshal` 校验；**解析失败则不写**（保留原 config.toml 不动）并 `logger.Warn`，把潜在 config.toml 损坏降级为 fail-loud 的 no-op，绝不写出不可解析的 config。
5. 内容较原文无变化则不写；有变化且校验通过则 `os.WriteFile(0o644)`。返回 `{SharedHooksCount, MappedHooksCount, StaleHooksCount, Changed}` 供调用点 `logger.Info` 记录（只记计数与路径，不记内容）。

**trust key 结构（已实测确认）**：Codex 写 `[hooks.state."<hooks.json 绝对路径>:<handler-suffix>"]`，例：
`[hooks.state."/Users/u/.codex/hooks.json:pre_tool_use:0:0"]`，body 为 `trusted_hash = "sha256:..."`。
plugin hook 的 key 形如 `plugin@local:hooks/codex-hooks.json:...`。

**plugin 排除是"按构造正确"**：映射只匹配前缀 `sharedHooksPath + ":"`（一个绝对路径 + 冒号）。`plugin@local:...` 不以该绝对路径开头，天然不被 extract，也不被 remove。无需额外黑名单。

**调用点顺序 [评审强化 D4]**：把 trust sync 调用放在 **所有 config.toml 变更之后**（即 `ensureCodexSandboxConfig` / `MultiAgent` / `Memory` 之后，`return nil` 之前），而非参考 PR 的"sanitize 之后、ensure 之前"。理由：sandbox/multi-agent/memory 三个 ensure 各自重写自己的 managed block，若 trust 先写、ensure 后写，需依赖"ensure 保留无关 block"的隐含契约；把 trust 放最后彻底消除该顺序耦合，且 trust sync 读到的是最终 config，映射结果最稳。

### 2.4 动作 3：hooks feature flag —— 不实现（对应 Requirement「幂等保证 hooks feature flag」的 IF 分支）

Q1 结论（§3）：当前 Codex 默认启用 hooks，**不写** `[features] hooks = true`。Requirement 3 落到"IF 不需要 THEN 不写入该键"分支，实现中无对应代码，spec 中该 Requirement 的三个 features 测试 Scenario 相应**不落地**（design 记录核实结论即满足 spec 的 IF 分支）。

### 2.5 数据流 / 控制流（prepare 一次的时序）

```
prepareCodexHomeWithOpts(codexHome, opts, logger)
 ├─ MkdirAll(codexHome)
 ├─ [existing] ensureDirSymlink   sessions
 ├─ [NEW]      ensureExistingDirSymlink  hooks/        ← 动作1(dir)
 ├─ [existing] ensureSymlink      auth.json
 ├─ [NEW]      ensureOptionalFileSymlink hooks.json    ← 动作1(file)
 ├─ [existing] logCodexAuthState
 ├─ [existing] syncCopiedFile     config.json/config.toml/instructions.md   ← config.toml 从 shared 重拷
 ├─ [existing] sanitizeCopiedCodexConfig / syncCodexModelCatalog / exposeSharedCodexPluginCache
 ├─ [existing] ensureCodexSandboxConfig / ensureCodexMultiAgentConfig / ensureCodexMemoryConfig  ← 各改 config.toml managed block
 └─ [NEW]      syncCodexHookTrustStateWithResult(...)  ← 动作2，置于所有 config 变更之后；含写前 TOML 校验 + Info 日志
```

### 2.6 接口契约

无对外接口 / 无新增用户可见配置项。纯 daemon 内部 home 准备逻辑增强。新增函数均为包内非导出（`ensureOptionalFileSymlink` / `ensureExistingDirSymlink` / `removeOptionalPath` / `syncCodexHookTrustState*` 等）。

## 3. 关键决策

| 决策点 | 选择 | 备选 | 理由 | 关联 |
|---|---|---|---|---|
| **D1 Q1: 是否写 `[features] hooks = true`** | **不写** | 幂等写 true | 双信道独立核实：Codex 0.141.0 `hooks` 已 `stable`/默认 `true`；强写会覆盖用户显式 `features.hooks=false`，破坏用户配置语义 | Requirement 3 走 IF-不需要分支 |
| **D2 非 ENOENT stat 错误的处理** | **fail closed：清 stale 后返回错误** | 保留 dst 仅返回错误（参考 PR 行为） | 参考 PR 在权限/I/O 错误时不清 dst，旧 per-task hook 可能残留并被加载，违隔离红线；隔离优先于可用性，宁可本次不加载 hook 也不加载 stale | 评审阻断项#2 |
| **D3 trust 写前 TOML 校验** | **写前 `toml.Unmarshal` 校验，失败则不写+告警** | 直接写文本（参考 PR 行为） | 逐行文本解析对多行 body / 续行以 `[` 开头等边界可能截断而破坏 config.toml；校验把"静默损坏"降为"fail-loud no-op"，符合正确性红线 | 评审高风险#5 |
| **D4 trust sync 时序** | **置于所有 config.toml 变更之后** | 参考 PR：sanitize 后、ensure 前 | 消除"ensure 是否保留无关 block"的顺序耦合；读到最终 config，映射最稳 | 评审中风险#9 |
| **D5 Windows dir 清理** | **区分 link/junction（`os.Remove`）与真实目录（`os.RemoveAll`）** | 非 symlink 一律 `RemoveAll`（参考 PR） | 防 `os.RemoveAll` 误递归删 junction 指向的 shared `~/.codex/hooks`；per-task 从不自建真实 hooks 目录，故真实目录才 RemoveAll | 评审阻断项#3 |
| **D6 解析实现** | **沿用逐行文本 + 前缀匹配（保原格式）+ D3 校验兜底** | 全量 TOML parse→改结构→序列化 | 全量序列化会重排/丢注释，破坏用户 config.toml 可读性；文本操作保格式，D3 校验保证不产出坏 TOML | 评审高风险#5/#6 |

### Q1 核实（proposal §6 / spec Requirement 3 的开放点）——**结论：不需要 `[features] hooks = true`**

**核实方式（双独立信道，互证）**：

- **信道 A（本 runtime，独立于 shared 配置）**：新建空 `CODEX_HOME`（无任何 config.toml），`CODEX_HOME=<tmp> codex features list` → `hooks  stable  true`。空 home 排除了本机 `~/.codex/config.toml` 的干扰，证明 `true` 来自**默认值**而非配置。
- **信道 B（Codex 评审方独立复核）**：`codex features list` → `hooks stable true`；且 `codex features list --disable hooks` → `hooks stable false`，证明该 flag 仍可被用户显式关闭。

**依据**：当前 `codex-cli 0.141.0` 的 `hooks` 是 **stable** feature，effective 默认 `true`（另注：`plugin_hooks` 为 `removed`，与本 change 无关）。故 daemon 无需写入该 flag。**反向论证**：既然 `features.hooks` 仍可被用户设 `false`，daemon 强写 `true` 会覆盖用户显式禁用意图 —— 属配置语义破坏，正确的做法就是不写、尊重 shared config 的既有值（`syncCopiedFile` 已把 shared 的 `[features]` 原样拷入 per-task）。

**影响**：Requirement 3 落到"不写入"分支；实现零代码；spec 中 features 相关 3 个 Scenario 不落地。若未来 daemon 需支持"hooks 尚处 under-development 的旧 Codex"，可按 `opts.CodexVersion` 版本门控补写（当前不做，YAGNI）。

## 4. 影响分析

### 4.1 受影响的 capability

| Capability | 影响类型 | 需更新 spec |
|---|---|---|
| `codex-home-hooks` | ADDED | 已由 spec-delta 覆盖；Requirement 3 依 Q1 结论落"不写入"分支（design 记录即满足），其余 3 条 Requirement 全量实现 |
| Codex per-task CODEX_HOME 准备（既有） | 增量扩展，语义不变 | 否（auth/session/config/skills/MCP/sandbox/memory/multi-agent 均不动） |

### 4.2 受影响的接口

| 接口 | 影响 | 兼容性 |
|---|---|---|
| daemon 内部 `prepareCodexHomeWithOpts` | 新增两段资源暴露 + 一次 trust sync 调用 | 向后兼容；无 hooks 的环境完全 no-op（不建空资源、不写 config） |
| 对外 / 用户可见配置 | 无 | 无新增配置项 / CLI / API |

### 4.3 受影响的运维

- 新增一条 `logger.Info("execenv: codex-home hook trust sync", ...)`（字段：codex_home 路径、shared/mapped/stale 计数、changed），便于排障；**只记路径与计数，绝不记 hook 内容 / token / cookie / socket**（评审中风险#8）。
- 无新增监控 / 告警 / SOP；验收阶段在自托管开发测试环境确认 Flux native hook `raw/result/reportTokenUsage` 链路与 Grafana `used`↔`reported` 收敛（proposal §5，属开发官/验收环节）。

## 5. 测试策略

测试载体：`server/internal/daemon/execenv/`。建议 trust 逻辑单测放 `codex_hook_trust_test.go`（可抽取 `9e031356f`/`aab3a43d6` 测试并按 D2–D5 补强）；prepare 级集成测试放 `execenv_test.go` 或新增 `codex_home_hooks_test.go`。所有用例用 `t.TempDir()` 构造 fake shared home + per-task codex-home，**不依赖本机真实 `~/.codex`**。

| # | 测试类型 | 范围 / 断言 | 关联 Requirement / Scenario |
|---|---|---|---|
| T1 | 单测(prepare) | fake shared 含 `hooks.json`+`hooks/<h>.sh` → per-task 两者可见 | R1 / 首次暴露 |
| T2 | 单测(prepare/reuse) | 先暴露，删 shared `hooks.json`+`hooks/` → 再 prepare，per-task stale 被清 | R1 / 源删除后 reuse 清 stale |
| T3 | 单测(prepare) | shared 无任何 hooks → per-task 不出现空 `hooks.json`/空 `hooks/` | R1 / 源不存在不建空资源 |
| T4 | 单测(prepare) **[D2]** | `os.Stat(src)` 非 ENOENT 错误（如源被替换为无权限项）→ per-task 旧资源被清（fail closed） | R1 强化 / 评审#2 |
| T5 | 单测(trust) | shared config 含 `[hooks.state."<shared hooks>:..."]` → per-task config 出现以 task hooks path 为 key 的等价 block | R2 / 映射到 task path |
| T6 | 单测(trust) | 同输入连跑两次 → mapped block 数量不变、不重复追加 | R2 / 幂等 |
| T7 | 单测(trust) | 映射后改 shared trust body → reuse → per-task block 刷新为新内容 | R2 / 变更后刷新 |
| T8 | 单测(trust) | 已映射后 shared hooks(+trust) 移除 → reuse → per-task mapped block 被清 | R2 / 缺失清理 |
| T9 | 单测(trust) | shared 同含用户 hooks block 与 `plugin@local:...` block → per-task 只出现用户 hooks 映射，plugin 不被映射 | R2 / 不误映射 plugin |
| T10 | 单测(trust) **[D3]** | 构造会导致输出不可解析的边界输入 → 断言不写坏 config（原文保留 + 无 panic） | R2 强化 / 评审#5 |
| T11 | 单测(隔离) | prepare 后断言：仅新增 `hooks.json`/`hooks/` 两项；auth/sessions/config/skills/MCP 暴露方式与语义未变；无整份 `~/.codex` 暴露 | R4 / 隔离边界 |
| T12 | 单测(共存) **[D4]** | reuse 后 sandbox / multi-agent / memory / hook-trust 四个 block 同时存在且各自不重复 | R4 强化 / 评审#9 |
| T13 | 单测(Windows,`//go:build windows`) **[D5]** | junction/copy fallback 下 refresh 不递归删 shared 目标、reuse 不产生多余 churn | R1 / 评审#3 |

> **features.hooks 三态测试（spec R3 的 3 个 Scenario）**：按 Q1 结论不实现该功能，故**不落地**，在归档时于 spec 标注"依 design Q1 结论不适用"。
> **归档前**：开发官把 spec 中各 Scenario 的 `TBD(...)` 占位替换为真实测试标识（形如 `.../execenv/codex_hook_trust_test.go::TestSyncCodexHookTrustState_Idempotent`）。

## 6. 兼容性与迁移

- **无破坏性变更**：无 hooks 的现有环境行为不变（全 no-op）；有 hooks 的环境从"hook 不生效"变为"hook 生效"，是纯增益。
- **无数据迁移**：per-task home 每次 prepare 全量重新推导，历史 per-task home 在下次 reuse 时自动收敛到新逻辑（旧 stale 资源被清、trust 被重建）。
- **rebase**：抽取三提交到 `origin/main`（head `3f02083fe`）之上；三提交仅动 `codex_home.go` 与新增 `codex_hook_trust*.go`，与最新 main 的 `codex_home.go` 无结构性冲突（现有函数签名未变）。

## 7. 红线检查

- [x] 本 change 是否触及 constitution.md 红线：**仓库无 constitution.md，无可对照红线**；本 change 主动坚守的隔离边界（下列）应作为未来宪法 candidate。
- [x] 隔离边界红线（Requirement 4，逐条落到实现/日志/测试）：
  - 只暴露 `hooks.json` + `hooks/` 两项，**不暴露整份 `~/.codex`**（T11 断言）。
  - **不把 Flux cookie / token / socket 写入 git 或 issue metadata**；日志只记路径 + 计数（§4.3）。
  - **不改变** auth / session / config / skills / MCP / sandbox / memory / multi-agent 既有语义（T11 断言；新增代码不触碰既有资源分支）。
- [x] 无需红线强制覆盖（§8 无覆盖记录）。

## 8. Clarifications（含红线强制覆盖）

### Q1: 当前 Codex 版本是否仍需要 `[features] hooks = true`？
**A**: 不需要。详见 §3 Q1 核实（双独立信道确认 `hooks` 为 stable/默认 true）。实现中不写该 flag。

### 红线强制覆盖记录
无（本 change 不触及任何需覆盖的红线）。

## 9. 风险

| 风险 | 缓解 |
|---|---|
| trust key 路径规范化与 Codex 实际写入不一致（macOS `/var`↔`/private/var`、symlink、大小写）导致漏映射 | 失败模式为 fail-safe（漏映射 → hook 未 trust → Codex 不加载，绝不误信任）；验收阶段做一次真实集成核对：让 Codex trust 一个 shared hook，比对其写入的 key 与 daemon 计算的 `sharedHooksPath` 是否一致（评审#7） |
| Codex 未来改用 literal-string / 其它合法 TOML key 形式写 `[hooks.state]`，正则失配 | 失败模式 fail-safe（漏映射，非误映射）；已实测当前 0.141.0 为 basic-quoted 形式；如变更由集成核对暴露（评审#6） |
| 逐行文本解析破坏 config.toml | D3 写前 TOML parse 校验兜底：不可解析则不写 + 告警（评审#5） |
| 非 ENOENT stat 错误残留 stale hook | D2 fail closed 清 stale（评审#2） |
| Windows junction 清理误删 shared 目标 | D5 区分 link/junction 与真实目录；T13 Windows 测试（评审#3） |
| trust sync 与 sandbox/multi-agent/memory block 相互覆盖 | D4 置于所有 config 变更之后；T12 共存测试（评审#9） |
| 把 Flux 私密写入版本库 / metadata | 只暴露两项资源、日志只记路径计数；不 dump 内容（Requirement 4 / 评审#8） |

## 10. Codex 交叉评审摘要（强制环节）

- **评审方**：Codex（`codex exec -s read-only`，`codex-cli 0.141.0`）。**轮数**：1 轮达成一致（无残留分歧）。
- **Codex 总结论**："方向认可（抽三提交做小 PR），但需改后再 rebase" —— 未挑出方向性错误，全部为正确性/安全强化项，与本设计融合后收敛。
- **Q1 独立复核（一致）**：Codex 独立跑 `codex features list` + `--disable hooks`，与我方空-home 信道**互证** `hooks` stable/默认 true → 共同定论**不写** `[features] hooks = true`，并补强"强写会覆盖用户显式禁用"的语义论证。
- **采纳（已折进 §3 决策 D2–D5 与 §5 测试）**：
  1. [阻断#2] 非 ENOENT stat 错误 → fail closed 清 stale（D2 / T4）。
  2. [阻断#3] Windows junction 安全清理，防误删 shared（D5 / T13）。
  3. [阻断#4] 补齐 prepare 级完整测试覆盖（§5 T1–T13）。
  4. [高#5] trust 写前 TOML parse 校验，杜绝写坏 config（D3 / T10）。
  5. [中#8] 日志只记路径/计数，不记内容/私密（§4.3 / §7）。
  6. [中#9] trust sync 置于所有 config 变更之后 + 共存测试（D4 / T12）。
- **部分采纳 / 判为 fail-safe 不阻断（已记 §9 风险 + 验收动作）**：
  - [高#6 literal-key 形式] / [中#7 路径规范化]：二者失败模式均为**漏映射→hook 不被 trust→不加载**（fail-safe，绝非误信任），不构成正确性红线；以"验收阶段真实集成核对 trust key 一致性"闭环，不阻断设计。
- **驳回**：无（Codex 未提出需驳回的误判）。
- **剩余分歧**：无。

## 11. 验收要点（交开发官）

1. **实现边界**：只改 `server/internal/daemon/execenv/codex_home.go` + 新增 `codex_hook_trust.go`（及测试）；**不动** auth/session/config/skills/MCP/sandbox/memory/multi-agent 分支。抽取 PR #3112 三提交后**必须按 D2–D5 强化**，不是原样 cherry-pick。
2. **dev↔test 内循环**：开发官在 worktree 内跑 `go test ./server/internal/daemon/execenv/...`（按 `~/.agents/rules/go-version.md` 用 `go.mod` 声明版本对应的 `~/go/goX.Y.Z/bin/go`）全绿后再交测；测试全部用 `t.TempDir()` fake home，不碰真实 `~/.codex`、不联网。
3. **归档动作**：把 spec-delta 各 Scenario 的 `TBD(...)` 替换为真实测试标识；features.hooks 三个 Scenario 标注"依 design Q1 不适用"。
4. **正确性必查（对应 review-workflow 验收红线）**：T4（fail closed）、T10（不写坏 config）、T9（不误映射 plugin）、T12（block 共存）为**阻断级**用例，缺一不可；happy-path 全绿 ≠ 通过。
5. **真实环境验收（proposal §5，验收/MR 环节）**：自托管开发测试环境确认新 Codex session 仍 `originator=multica-agent-sdk`/`source=vscode`，Flux 日志见 native hook `raw/result/reportTokenUsage`，Grafana `used`↔`reported` 收敛；trust key 一致性做一次真实集成核对（§9 风险闭环）。

## 12. 后续动作建议

- 开发官落地时若在真实集成核对中发现 trust key 与 daemon 计算路径不一致（§9 首行风险），回报编排官，可能需要在 daemon 侧对 `sharedHooksPath` 做与 Codex 一致的路径规范化（如 `filepath.EvalSymlinks`）——此为潜在增量，非本轮阻断项。

## 关联

- 关联 Issue: RAS-50
- 关联 proposal: [proposal.md](./proposal.md)
- 关联 spec-delta: [specs/codex-home-hooks/spec.md](./specs/codex-home-hooks/spec.md)
- 参考实现: GitHub PR #3112（仅 `6f3e36e3d` / `9e031356f` / `aab3a43d6` 三个 hook 提交有效）

## 变更历史

| 日期 | 变更 | 作者 |
|---|---|---|
| 2026-07-09 | 初版：技术设计 + Q1 定论（不写 features.hooks）+ Codex 对抗式评审融合 | 技术方案官 |
