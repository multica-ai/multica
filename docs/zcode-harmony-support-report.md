# ZCode 与鸿蒙支持调研报告

- 日期：2026-09-22
- 范围：本仓库（`multica-ai/multica` 的 checkout）对 **ZCode（Z.ai 编程 agent）运行时**和**移动端鸿蒙（HarmonyOS）平台**的支持情况，以及对 GitHub 上游仓库全部 fork 的实地扫描结论。

## 结论速览

| 问题 | 结论 |
| --- | --- |
| 本项目支持 ZCode 吗？ | **当前代码不支持**（上游 `main` 未合并），但社区已有成熟实现：上游未合并 PR [#6987](https://github.com/multica-ai/multica/pull/6987)，以及两个已并入各自 `main` 的 fork。 |
| 移动端支持鸿蒙吗？ | **不支持，且没有任何 fork 实现**。5100 个 fork 全量验证 0 命中；移动端至今仅 iOS。 |

## 调研方法

1. 本地 checkout：检索 `ZCode`/`zcode`、`harmony`/`ohos`/`鸿蒙`，并核对 `server/pkg/agent/agent.go` 的 `SupportedTypes`、`scripts/agent-cli-command-names.txt`、`apps/mobile/` 配置。
2. GitHub 上游：拉取全部 **5100 个 fork 列表**，对每个 fork 的默认分支抓取 `scripts/agent-cli-command-names.txt`（查 zcode）与 `apps/mobile/package.json`（查 `react-native-harmony`/`ohos` 依赖）。首轮扫描有瞬时失败被误判为"无"的缺陷，已用带重试的脚本**全量复核一遍**（补回了漏检的 `Git-on-my-level/multica`），最终两个维度均 5100/5100 覆盖。
3. 上游 PR/Issue 搜索：`zcode` 相关 3 个 PR + 2 个已关闭 Issue；`harmony`/`ohos`/`鸿蒙` 相关 0。
4. 对 PR 源头分支定向验证。

局限：默认分支 + 已知 PR 分支全覆盖；未提 PR 且藏在非默认分支的私人改动无法覆盖。

## ZCode 详情

ZCode 是 **Z.ai（智谱）的 GLM 编程 agent CLI**。上游 `main` 仍只有 26 个受支持工具（`agent-cli-command-names.txt` 实际 26 行），ZCode 相关改动全部停留在 PR 或 fork 中。

### 已发现的实现（5 处）

| # | 位置 | 状态 | 方案 | 规模 |
| --- | --- | --- | --- | --- |
| 1 | 上游 PR [#6987](https://github.com/multica-ai/multica/pull/6987)（`lianxin255/multica` @ `feat/zcode-acp-runtime`） | **开启中，未合并**（2026-08-14 创建，09-20 仍活跃） | 作为 ACP runtime family；迁移 469；`zcode.go`（588 行）驱动 ACP 桥；含 `server/cmd/zcodee2e` 与 492 行测试；四语言文档/落地页 | 39 文件，+1524/−22 |
| 2 | [`Git-on-my-level/multica`](https://github.com/Git-on-my-level/multica)（main 分支） | 已并入 fork | #6987 的移植（`FORK.md` 明载）；驱动社区桥 `zcode-acp-server`（[william0wang/zcode-acp](https://github.com/william0wang/zcode-acp)，npm 包，≥0.2.0），复用现有 hermes ACP 传输；迁移 9018 | 后端 + 测试 + e2e + 四语言文档 |
| 3 | [`cfgxy/multica-cn`](https://github.com/cfgxy/multica-cn)（main 分支） | 已并入 fork | 独立实现：驱动 ZCode 自带的 `zcode-acp` CLI（`zcode-acp acp`），探测 `MULTICA_ZCODE_PATH` / `MULTICA_ZCODE_MODEL`；迁移 907（连同 DeerFlow 一起加入白名单，并迁移历史 shim profile 身份） | 后端 + 单元/集成测试 + logo + 四语言落地页（"28 款工具"） |
| 4 | 上游 PR [#6755](https://github.com/multica-ai/multica/pull/6755)（`tornado404/multica`） | 已关闭，未合并 | 自有后端 + 流式 JSON 事件（`zcode_stream.go` 383 行）；迁移 310 | 33 文件，18 commits |
| 5 | 上游 PR [#6982](https://github.com/multica-ai/multica/pull/6982)（`tornado404/multica` @ `feat/zcode-app-runtime`） | 已关闭，未合并 | 走 ZCode 原生 app-server 会话协议（`zcode_appserver.go` 1053 行）；迁移 327 | 34 文件，38 commits |

另有已关闭的 Issue [#5361](https://github.com/multica-ai/multica/issues/5361)、[#5976](https://github.com/multica-ai/multica/issues/5976) 提出过该需求。

### 技术方案对比

- **ACP 路线（#6987 / Git-on-my-level / cfgxy）**：ZCode 本身不直接讲 ACP，需要桥接层。区别在桥的选择：`zcode-acp-server`（社区 npm 桥，包装桌面版运行时）vs `zcode-acp`（ZCode 自带 CLI 子命令）。ACP 路线能最大化复用现有 ACP 传输与会话/取消/推理档位逻辑，是唯一进入上游评审的方案。
- **流式 JSON 路线（#6755）**：解析 CLI 输出流，耦合度高，已被作者放弃。
- **app-server 路线（#6982）**：直连官方会话协议，实现量最大（1000+ 行），亦已被放弃。

## 鸿蒙详情

- 移动端现状：Expo SDK 55 + React Native 0.83.6，`apps/mobile/app.config.ts` 仅 `ios` 配置，构建脚本仅 `scripts/ios-run.sh`，README 标题即 "Multica Mobile (iOS)"。连 Android 都未启用，更无鸿蒙。
- Fork 扫描：全部 5100 个 fork 的 `apps/mobile/package.json` 无 `react-native-harmony`/`ohos`/`openharmony` 依赖，**0 命中**。
- 上游讨论：搜 `harmony`/`ohos`/`鸿蒙` 的 issue/PR 均 0 命中（仅有的 "harmony" 字样来自 DevEco——华为的编程 agent CLI，属桌面端 agent 运行时，与移动端平台无关）。
- 障碍：鸿蒙上的 React Native 需要社区分支 `react-native-harmony`（OpenHarmony 定制），与 Expo 的 prebuild / expo-router / nativewind 体系不兼容；引入它等于把移动端从 Expo 体系中剥离开，属于平台级工程，不是加依赖能解决的。

## 建议

1. **ZCode**：跟进或摘取 PR #6987（ACP 路线，唯一活跃上游方案）；若不愿等待评审，可参照 `cfgxy/multica-cn` 的 `zcode-acp` 方案移植到本 checkout——改动面为 `server/pkg/agent/`（新后端 + `SupportedTypes` + 探测）、一条迁移、`packages/core/runtimes/display.ts` 等前端展示位，与该仓库既有 runtime 的接入模式一致。
2. **鸿蒙**：~~短期不可行~~ **已实施第一阶段（2026-09-23）**：新建独立 RNOH 工作区 `apps/mobile-harmony`（不复用 Expo 体系，规避了本报告指出的 Expo/RNOH 不兼容障碍）。垂直切片已在本地 DevEco 模拟器验证：邮箱验证码登录、工作区选择、只读收件箱（含实时 WS）。技术要点：RNOH 0.77-stable 矩阵（RN 0.77.1/React 19.1）、NativeWind v4 实证可用、oh-tpl 原生库经 `PackageProvider.ets` 适配器注册、令牌暂存 AsyncStorage（Asset Store Kit 迁移列入路线图）。剩余功能与路线图见 `apps/mobile-harmony/README.md`。

## 证据清单（已实际抓取核验）

- 上游 `main`：`scripts/agent-cli-command-names.txt`（26 行，无 zcode）、`server/pkg/agent/agent.go` `SupportedTypes`（26 项）、`apps/mobile/package.json`（无鸿蒙依赖）。
- PR 元数据与文件列表：#6987 / #6755 / #6982 经 GitHub API 核验；#6755 源头分支已删除。
- Fork 命中：`cfgxy/multica-cn`、`Git-on-my-level/multica` 的 `agent-cli-command-names.txt`、`zcode.go`、迁移文件（907 / 9018）均实际读取核验；后者浅克隆全仓复核。
- Fork 扫描原始结果：zcode 全量复核 5100/5100 = 2 命中；harmony 全量复核 5100/5100 = 0 命中。
