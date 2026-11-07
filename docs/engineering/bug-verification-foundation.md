# Bug 验证基础能力评估

> **已被取代的部分：** 宿主已改为只提供操作和取数、接口互不依赖（见[接口契约 v2](verification-foundation-contracts.md)）。本文中由验证包承担断言、步骤采集、验证记录和证据报告的设计不再实现，判断由调用方智能体完成。

范围：本仓库交付可在不同设备使用的自动化验证能力。总架构以[自动化验证架构](automation-runtime-foundation.md)为准；本文保留 Bug 验证场景、代码证据和验收约束。

首版部署已收窄为专用、干净的 Mac 原生验证；数据优先使用用户授权的当前真实数据，只读或真实快照副本模式按场景选择。本文关于同机多实例、状态根隔离和 VM 的内容保留为后续选项，不是当前实施前提；历史 fixture/账号建议以总方案为准。

本文是设计提案，尚未实现验证运行器。评估基于远端 `origin/main` 的 `be88001167e7316d5516105547747846721b82e8`，以及本地会话 `c9d873a6-dfd3-40c3-a7f1-227fc92b72b8` 的全部 34 条消息。会话中的历史任务统计、截图故障和机器成本没有在本轮重新调查，不能当作当前运行状态。

结论：可以成为仓库的基础能力。统一验证包承接场景执行、环境隔离、驱动、断言与证据报告，供修复员和验证员共用。当前最关键的完整客户端前置工作是 Desktop 运行时隔离。

## 已确认的需求

- 验证员根据 PR 实际改动决定验证范围，分诊仅作参考。
- 修复员提供复现脚本并先执行；验证员复核原始复现步骤，至少补一个边界场景。
- 纯渲染层使用浏览器和 fixture；涉及 Electron 的行为才构建客户端。同一版本产物供多个场景复用。
- 有界面现象时报告必须包含截图；没有界面现象时使用日志或测试输出。验证员实际打开截图检查。
- 用户只阅读报告，不承担额外点击应用的验收步骤。
- 新增脚手架和场景不接入 CI。保留现有 CI，不改动已有测试的执行范围。
- 仅用户主动要求才可跳过验证；原会话明确约定“帮我合入”也算。跳过需要保留原话、未覆盖部分和 CI 状态。
- 真实智能体冒烟测试仍需明确授权。跳过验证不等于授权调用真实智能体，也不改变仓库权限或分支保护。

## 当前实现与证据

以下路径均相对于仓库根目录；链接指向本次评估的 checkout。

| 证据 | 实现位置 | 已确认的行为 | 对方案的影响 |
| --- | --- | --- | --- |
| E1 | [环境管理](../../scripts/dev-env.sh)、[环境测试](../../scripts/dev-env.test.sh) | 按 checkout 分配 API、Web、Desktop renderer 端口和数据库名；数据库通过应用使用的 `DATABASE_URL` 创建；API 校验 pid、commit、启动时间 | 复用生命周期和身份校验，不再新写服务启动器 |
| E2 | [Desktop 启动](../../apps/desktop/scripts/dev.mjs)、[worktree 环境](../../apps/desktop/scripts/worktree-dev-env.mjs)、[主进程](../../apps/desktop/src/main/index.ts) | 开发实例通过 `DESKTOP_APP_SUFFIX` 区分 app 名称、`userData` 和单实例锁，通过 `DESKTOP_RENDERER_PORT` 区分端口 | 窗口能独立，但不证明 daemon、账号和本地会话独立 |
| E3 | [Desktop 运行时注册](../../apps/desktop/src/main/runtime-instance.ts)、[注册装配](../../apps/desktop/src/main/desktop-runtime-instance.ts)、[Go 注册协议](../../server/internal/runtimeinstance/registry.go)、[注册测试](../../apps/desktop/src/main/runtime-instance.test.ts) | 使用真实 `homedir()` 下 `.multica/desktop-runtime`，固定 `instanceId=desktop`、`profile=desktop-runtime`；后启动的 Desktop 覆盖注册，旧 owner 被拒绝 | 完整客户端试验可能接管用户正在使用的运行时；只传 app suffix 或 CLI profile 不够 |
| E4 | [浏览器配置](../../playwright.config.ts)、[引用场景](../../e2e/local-resource-mentions.spec.ts)、[性能场景说明](../../e2e/perf/README.md) | 已有无后端 fixture 场景和页面截图；默认 Playwright 项目只有 Chromium | 浏览器路径有基础，但没有统一的 Electron 完整应用验证入口 |
| E5 | [双版本对照脚本](../../scripts/perf-compare.mjs)、[生命周期测试](../../scripts/perf-compare.test.sh) | 同一份场景和 fixture 跑两个版本；记录 SHA、fixture digest、构建耗时和缓存命中；拒绝旧报告，并清理进程组 | 复用其比较和报告约束；脚本目前专用于性能测试，不能直接宣称是通用验证器 |
| E6 | [原生预览冒烟](../../apps/desktop/scripts/side-preview-smoke.cjs) | 临时 `userData` 的小型 Electron 集成测试，通过单独入口加载模块 | 可参考模块集成方式；不等于已验证真实 Desktop 的完整启动链 |
| E7 | [任务评论 CLI](../../server/cmd/multica/cmd_issue.go)、[附件测试](../../server/cmd/multica/cmd_attachment_test.go) | 评论支持 `--content-file` 与重复 `--attachment`，图片引用可随评论展示 | 初期直接复用评论与附件，不必先新增报告数据库实体 |
| E8 | [Computer Use 实现](../../apps/desktop/src/computer-native/driver.ts)、[能力说明](computer-use.md) | 内置工具只排除托管当前会话的 Multica 进程，要求系统权限和真实用户授权 | 改用 cua-driver 后可在后台操作验证环境中的 Multica 实例；用它验收原生外观尚未经过人工验证 |

### 两个会改变设计的边界

**数据库端口和数据库隔离是不同问题。** E1 仍以共享 PostgreSQL 为开发模型；端口分配器没有自动为 PostgreSQL 分配主机端口。当前数据库连接路径比原会话描述更严格，但不代表已自动解决其他项目占用 5432 的问题。根本方案是一次配置正确的共享实例与连接串，再由现有环境管理创建每个 checkout 的独立数据库；兼容路径是使用已授权、可达的 PostgreSQL 并配置 `.env.worktree`。不能要求每个任务临时起数据库容器，也不能连接未知数据库后尝试改密码或清理它。

**窗口隔离和运行时隔离是不同问题。** E2 → E3 的调用路径为：设置 app 名称和 `userData` → 获得窗口锁 → 加载配置 → `desktopRuntimeRegistry().register(apiUrl)` → 覆盖机器级注册。第一个跨实例共享写入是运行时注册，而非最后出现的连接报错。E3 的测试明确覆盖“另一个 Desktop 启动后接管”，所以这是现有产品契约，不应作为意外 Bug 直接改掉。

## 能力边界与目标结构

验证场景调用统一验证包，开发入口可放在 `scripts/verification/`，手动运行的场景放在 `verification/scenarios/`，均为拟新增路径。验证包管理环境和证据生命周期。场景留在默认 `e2e/` 目录之外，避免被现有 Playwright 或 CI 自动发现。成熟场景将来是否迁入常规回归测试，单独决定。

| 模块 | 输入与职责 | 沿用或新增 |
| --- | --- | --- |
| 验证计划 | 固定 base/head SHA、场景、fixture、需要的能力和判断依据 | 新增本地文件协议；初期由验证员填写 |
| 环境适配 | 浏览器 fixture、Electron、后端服务各自准备和自检 | 复用 E1；新增 Electron 运行时隔离及驱动适配 |
| 场景执行 | 同一份脚本和 fixture 对照两个版本，补充边界断言 | 复用 E4/E5 的模式，不把性能指标写死到通用运行器 |
| 证据采集 | 截图、错误、断言、进程和构建身份、trace、耗时 | 统一落盘和索引；原始日志先检查再分享 |
| 报告生成 | 校验证据完整性，生成 JSON、Markdown 与可离线阅读的 HTML | 新增，复用 E7 交付图片和文件 |
| 工作流解释 | 判定通过、不通过、无法验证或用户跳过 | 保留在角色约定；本轮不修改云端定义或任务状态 |

能力选择应是矩阵，而非简单的 L1 到 L4 晋级：

| 变化涉及的行为 | 必需环境 | 证明范围 |
| --- | --- | --- |
| 布局、样式、共享组件 | 浏览器 + 真实渲染代码 + fixture | 渲染和交互；不证明 IPC 或真实后端 |
| preload、IPC、窗口、文件处理 | Electron + 真实 main/preload/renderer；按需要使用本地数据 | 跨进程链路；调用拦截只证明参数和分发 |
| 纯 API / 数据一致性 | 隔离 API 与数据库、集成测试 | 不为没有客户端现象的问题强制启动 Electron |
| 客户端与后端联动 | Electron + 隔离 API 与数据库 | 端到端数据和界面状态 |
| 真实 provider 行为 | 相应环境 + 单独获得的真实智能体测试授权 | 真实会话行为，不能被 fake provider 替代 |
| 原生菜单外观、签名、更新等 | 对应 OS、必要时真实发行包和专用原生验证设施 | 开发 Electron、DOM 截图或拦截记录均不能替代 |

浏览器 fixture 应尽量挂载真实 Desktop renderer 或实际共用的组件。不能只因 Web 页面通过，就证明 Desktop 独有路由、平台适配和 preload 正常。

### 根本方案与兼容方案

| 方案 | 做法 | 覆盖与代价 |
| --- | --- | --- |
| 根本方案，推荐目标 | 建立显式开发/验证环境上下文，将状态根、运行时注册、profile、端口、构建身份和归属贯穿 Desktop 与 Go；默认生产实例继续使用现有固定注册 | 能在用户继续使用正式客户端时验证完整应用；需同步修改 TS/Go 协议、路径解析和测试，不能只加一个环境变量 |
| 兼容方案，可先落地 | 浏览器 fixture + 临时目录下的专用 Electron 模块集成入口，复用真实模块与受控依赖，但不进入完整 Desktop 启动链 | 快速覆盖渲染和部分 IPC/菜单逻辑；报告明确完整启动、本地会话和原生外观未覆盖 |
| 兼容方案，完整应用试点 | 在独立 OS 测试用户或专用测试设备运行现有应用 | 保留当前生产运行时协议，部署和账号准备成本更高；需要预置环境及实际隔离验证 |

根本方案限定于开发/验证环境，不默认推广为全产品多实例运行时系统；现有生产实例的发现协议继续保持兼容。Desktop、Go 注册、profile 存储及相关路径必须一起设计，不能更改 `HOME`、绕过 owner 校验或只修改 bundle ID 来规避现有限制。

原生外观需要独立解决。优先使用真实 Electron 主进程断言和窗口内容截图证明逻辑；只有缺失部分不属于任务验收要求时，才可注明未覆盖。原生外观就是核心要求时，缺少系统级证据必须判无法验证。要做到用户只读报告且所有原生任务均可验证，后续需要专用测试设备上的受控截图/交互设施，不能直接复用当前禁止自操作的 Computer Use，也不能暗中放开其策略。

## 一次验证的执行和状态流

1. 验证员读取 PR diff 与原始复现描述，冻结基线与候选 SHA，记录能力要求和复核理由。
2. 运行器建立唯一 run ID，分别记录场景、fixture 和构建输入的摘要。禁止读取上一轮产物充作本轮证据。
3. 按所需能力执行环境自检：服务身份、登录、fixture、窗口就绪和必要权限。自检失败单独分类，不等同于产品断言失败。
4. 对照阶段使用相同脚本与数据：基线必须在目标断言处重现原问题；编译失败、端口失败或未登录不算“已复现”。
5. 候选必须通过该断言及验证员增加的边界场景。没有观察到原问题，仅能说明候选当前行为，不能补写成已证明修复。
6. 有界面现象采集定位到原问题的前后截图；纯 UI 问题覆盖浅色、深色。失败采集现场，无法截图则写明阻塞。无界面问题保存日志和测试输出。
7. 验证员打开截图、审查断言和未覆盖范围，再生成结论。脚本成功或图片文件存在均不能代替这一环节。
8. 生成本地报告，停止并核实本轮拥有的进程已退出；保留证据和清理失败记录。交付到任务是独立动作，依赖当前任务的实际授权。

拟议运行状态：`planned → preparing → checking → running_base → running_head → collecting → finished`。任一步骤可进入 `failed` 或 `cancelled`，并记录阶段与原因。这些是本地运行状态，不是新增任务工作流状态。

报告结论与执行状态分开：

| 结论 | 条件 |
| --- | --- |
| `passed` | 原问题在基线目标断言处出现，候选消失；要求范围的相关检查和证据齐全 |
| `failed` | 环境有效，候选仍违反验收断言；有可定位的失败证据 |
| `inconclusive` | 基线无法复现、关键数据缺失、必需能力不可用或脚手架失败；列出尝试与缺失条件 |
| `waived` | 用户主动跳过，记录请求来源、原话、未覆盖项及当前 CI 状态；不伪造通过证据 |

例如基线在启动前崩溃、候选正常启动，不能仅凭两次退出码得出特定右键 Bug 已修复。CI 有失败时要在报告中展示，人工跳过本次验证也不把 CI 改写为通过。

### 报告与构建复用协议

每个报告至少记录：仓库与 PR、base/head SHA、实际构建身份、场景与 fixture 摘要、验证员的范围依据、执行命令和退出码、目标断言、环境自检、截图及步骤/主题/时间、日志、耗时、边界场景、未覆盖项、结论和用户跳过记录（如适用）。每个附件与本轮 run ID 绑定，不仅依赖固定文件名。

截图用窗口内容或页面 API 生成，不声称包含系统原生菜单；菜单结构使用主进程记录补充。交付报告必须在正文展示关键图片，日志与 trace 可作为附件。截图数据使用 fixture 或专门测试账号，运行身份中不得包含密钥。

构建缓存至少包含 commit、lockfile、相关构建配置与环境、OS/架构、Node/Go/Electron 版本。脏工作区禁用缓存或纳入实际源码摘要。构建产物可复用，`userData`、数据库和会话状态不能因缓存复用。fixture 与场景摘要属于验证结果身份，不能只看 commit 判定两轮结果等价。

初期 Electron 串行运行，记录安装、构建、启动、执行和收集的阶段耗时，再根据数据开放并发。没有本机 Desktop 构建耗时和内存实测，不能沿用历史会话的容量推测作承诺。

## 建议落地顺序与验收

以下是 Bug 验证场景的接入顺序；验证包的建设顺序以总架构为准。

### 第一阶段：仓库运行器与证据报告

先实现计划、浏览器 fixture 适配、前后版本对照、截图和报告，抽取 E5 已验证的生命周期约束。增加按需自检入口，不接入 CI。选择可明确复现的现有渲染问题试点。

验收：同一脚本跑两份构建，基线在指定断言失败、候选通过；浅深色截图可读；启动失败及过期报告不能得到通过结论；取消后无所属进程残留；用户只读报告即可了解证明范围。

### 第二阶段：完整 Desktop 运行时隔离与 Electron 驱动

先补齐环境上下文及 TS/Go 隔离边界，再使用 Playwright Electron 驱动真实主进程、preload 和渲染层。开发构建满足普通行为验证，不默认打安装包；发行相关问题单独使用对应发行产物。后端按能力要求组合，复用 `TestApiClient` 准备测试数据与登录，不要求用户先提供生产账号。

验收：正式实例和测试实例的注册、profile、会话与文件互不影响；测试退出后正式实例仍可工作；主进程错误、renderer 错误和 IPC 证据都能收集；确认实际构建版本；所有测试使用 fake provider，除非另外获得真实调用授权。

CHOV-51 类型问题可作为这一阶段的候选：引用内部元素的右键不能误走文件 IPC，真实文件右键仍有效，左键与 Enter 导航不退化。但需要取得真正存在问题的基线和原始复现数据；当前 main 与此前 main 对比，不保证仍能复现该问题。

### 第三阶段：角色使用与可选产品入口

先将稳定命令交给修复员和验证员，输出评论附件即可。工作流状态按项目当前真实配置映射，不能把 `verifying` 等项目定制状态写死进通用运行器。本轮不变更任何智能体定义或云端工作流。

如后续要支持其他仓库，再将项目资源中的“验证命令/环境说明”与现有执行、附件和报告展示连接起来。其他仓库提供自己的环境适配与场景；不要把 Multica 的 Electron、数据库或 `make up` 约束强加给它们。初期无需新增服务端报告表、专用执行调度器或测试平台 UI。

## 本轮验证与未覆盖范围

本轮仅新增此评估文档，未改产品代码、CI、任务状态或智能体配置，没有启动第二个完整 Desktop 或执行真实智能体。

| 命令 | 结果 | 证明范围 |
| --- | --- | --- |
| `git fetch origin main` | 成功，创建 `codex/verification-foundation` worktree，基于上述 SHA | 来自远端 main，与原 checkout 隔离 |
| `bash scripts/dev-env.test.sh` | 通过 | 临时目录与 fake 命令下的环境注册行为；没有启动数据库或服务 |
| `bash scripts/perf-compare.test.sh` | 通过 | fake 构建/场景下的对照报告及进程清理；未进行真实浏览器性能测试 |
| Desktop Vitest：`scripts/worktree-dev-env.test.mjs`、`src/main/runtime-instance.test.ts` | 2 个文件、27 项通过 | worktree 环境计算与单机运行时接管约束，使用临时目录和 mock CLI 版本 |
| `git diff --check`、新增文档的 `git diff --no-index --check` | 通过 | 文档空白检查；19 个本地引用也已检查，无缺失 |

新 worktree 未安装依赖。最初误用 `node --test` 执行 Vitest 文件失败；随后 `pnpm exec vitest` 因新 checkout 没有可执行入口失败。最终在 `apps/desktop/` 使用现有主 checkout 的已安装运行器完成检查，未复制依赖或环境文件：

```sh
node /Users/zhengweibin/Desktop/chovy-multica/node_modules/vitest/vitest.mjs run scripts/worktree-dev-env.test.mjs src/main/runtime-instance.test.ts
```

因此该结果不是一次全新依赖安装验证。正常安装依赖后的标准命令为 `pnpm --filter @multica/desktop exec vitest run scripts/worktree-dev-env.test.mjs src/main/runtime-instance.test.ts`。

未运行完整类型检查、lint、全量产品测试、真实 Electron 自动化、数据库联调或原生截图。没有重新核验历史任务附件、CI 失败归因及当前 5432 占用。本文对可行性的判断来自实现与上述窄范围测试，完整方案的资源成本和跨平台表现仍需试点确认。
