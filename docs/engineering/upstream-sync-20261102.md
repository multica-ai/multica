# v0.7.2 upstream sync — 2026-11-02

本记录对应 OL-1177，执行架构审阅报告第二节 A/B 必做项及最终修正版 D1–D8。合并、锁文件和全量 sqlc 生成物在同一分支/PR 内交付；不拆单。本阶段交付合入 `main` 的非草稿 PR，本机安装、生产部署、发布和更新源启用不在此次操作范围。

## 基线与结果身份

| 项目 | 精确身份 |
| --- | --- |
| fork 基线 `origin/main` | `ad268d6cd4375b8ed7fcec02a02b607ceb8cfc81` |
| 上游正式 tag `v0.7.2` | `f05a525ac8e82f5e9169ad078e44f166fad6d5c8` |
| merge base（v0.7.0） | `dbdbb299164347586e2650bb67684a490726a798` |
| 合并方式 | 在上述 fork 基线执行 `git merge --no-ff f05a525ac8e82f5e9169ad078e44f166fad6d5c8`；禁止 rebase；不带 tag 后提交 |
| 结果身份 | 两父提交的第一父为上列 fork 基线，第二父为精确 tag；最终 SHA 随 PR/OL-1177 交付评论记录，审阅必须绑定该 SHA |
| PR / CI / 审阅记录 | OL-1177 交付评论及关联 PR；最终 head 的 CI required 和两位专家顺序审阅是合入门禁 |

`git merge-tree --write-tree --name-only <fork> <upstream>` 复核为 94 个文本冲突（55 个代码/配置、39 个 docs），均为 content 冲突；不把干净文本合并视为语义通过。`v0.7.0..v0.7.2` 的上游增量为 109 个提交。下面的比对采用上述不可变 SHA 与本次结果树；验证结果只以末节实测记录为准。

## 最终裁决 D1–D8

| 决策 | 本次执行语义 |
| --- | --- |
| D1 | 采用精确 v0.7.2 tag 的两父 `--no-ff` 合并，不挑选 tag 后提交；OL-1122 以 `cancelled --duplicate-of OL-1177` 收口，平台动作以交付评论为准。 |
| D2 | 采用 MUL-8200 / #9472 的派发链结算。仅 issue 进入 blocked 不再视为失败；活跃派发链期间 issue 提前进入交付状态不结算。派发成功消费配额，失败释放；保留实际结算时的 `first_terminal_status` 和 `errRunAlreadyTerminal`。 |
| D3 | 外部授权会话关闭新增飞书图片/文件回复与 issue 引用链接改写。成员绑定会话保留上游能力；所有媒体异步上传、发送、重试、失败通知重新核验安装/会话/冻结授权。 |
| D4 | 成员重跑外部会话来源 run 时，沿用原 `conversation_root_task_id`、originator/accountable grantor，并在产生副作用前重验；`fresh` 只改变会话恢复，不改变授权主体。 |
| D5 | 保留 zh-Hant，新增 Web/Desktop 及移动端品牌覆盖；回退链为 `zh-Hant → zh-Hans → en`，fork 未翻译命名空间显示简中。 |
| D6（修正版） | 54 个 fork 文件由 lucide-react 迁至 `@multica/ui/icons`；8 个缺失名称复用下表上游近似图标，不绘制新图标，不保留第二套图标依赖，不放宽 eslint。 |
| D7 | GA 保留上游挂载但保持休眠，用户可见标签改为 Labrastro；新增部署配置禁设 `GOOGLE_ANALYTICS_ID` 断言，包括空值透传。品牌守卫不放宽。 |
| D8 | 保留上游 ⌘K 行为：未指定对象时选择最近使用或第一个在线智能体，并立即创建/启动运行。 |

| fork 图标名称 | 复用的上游图标 |
| --- | --- |
| `QrCode` | `MessagesSquare` |
| `Waypoints` | `Network` |
| `CirclePlay` | `Play` |
| `OctagonAlert` | `CircleAlert` |
| `ArrowRightFromLine` | `LogOut` |
| `FileClock` | `History` |
| `PackagePlus` | `Package` |
| `PackageX` | `Unlink` |

### Autopilot 的结算与首终态

派发链以 run 保存的 `task_id` 为起点，包含 `retry_of_task_id` 的后继；同 issue 上无关 mention/comment 任务不替代该链。活跃派发链优先于 issue 的提前交付状态；无派发和取消路径沿用上游判定，自定义状态按 done/closed 生命周期处理。blocked 本身不失败：链成功时即使 issue 为 blocked 仍记成功并消费配额，状态卡显示结算时的 blocked。

`server/pkg/db/queries/autopilot.sql` 的 `UpdateAutopilotRunTerminalWithQuota` 在同一条 SQL 的 `updated_run` CTE 内，用 issue 当前状态构建 `jsonb_build_object('first_terminal_status', i.status)`，仅在 run 仍为 pending/issue_created/running 时更新。后续 CTE 锁定仍 reserved 的 reservation、完成 consume/release 并更新 period 计数。状态快照、run 终态和配额结算原子完成，不采用服务层先读状态、再写快照的两步方式。重复终态返回零行并映射 `errRunAlreadyTerminal`，不得重新发布、再次计费或覆盖第一次结果；run_only 保留任务输出，skipped 保留已有结果。

`autopilotRunCompletion` / `autopilotRunFailure` 改为 service 内参数结构；不恢复 #9671 删除的无配额终态查询。补偿扫描同时覆盖 completed/failed，沿用已持久化 cursor key；只允许该派发链的最后一次尝试结算，移除原 issue 全局最新失败任务门控，避免无关 mention 遮蔽派发失败。新增 `TestAutopilotDispatchSettlementFreezesIssueStatus` 覆盖 blocked/in_review/done × completed/failed、派发活跃时不结算、后续 issue 取消/重复事件不改写冻结值；实际运行结果见验证表。

## 94 个文本冲突裁决

以下编号来自原始两父合并，后续为静默冲突新增的修复文件不混入这 94 项。

| # | 路径 | 裁决 |
| --- | --- | --- |
| 1 | `AGENTS.md` | 保留 fork 两项 retained constraints，并接入上游 speculative-risk buy-in 条款。 |
| 2 | `apps/desktop/package.json` | 版本升至 0.7.2；保留 Labrastro 产品信息、内部发布约束和 fork 依赖。 |
| 3 | `apps/desktop/src/main/daemon-manager.ts` | 合入退出、安装/升级门控；保留 managed CLI、恢复路径、工作区根目录和原生对话框品牌。 |
| 4 | `apps/desktop/src/renderer/src/components/daemon-runtime-card.tsx` | 接入新图标；保留 fork 运行指标、连接与管理流程。 |
| 5 | `apps/docs/content/docs/auth-tokens.zh.mdx` | 采用上游结构，保留 fork 凭据/内部部署说明与授权边界。 |
| 6 | `apps/docs/content/docs/channels.ja.mdx` | 采用上游 Steps/Accordion 组织；保留飞书显式授权、冻结 grantor 和外部文本回复边界。 |
| 7 | `apps/docs/content/docs/channels.ko.mdx` | 采用上游 Steps/Accordion 组织；保留飞书显式授权、冻结 grantor 和外部文本回复边界。 |
| 8 | `apps/docs/content/docs/channels.mdx` | 采用上游 Steps/Accordion 组织；保留飞书显式授权、冻结 grantor 和外部文本回复边界。 |
| 9 | `apps/docs/content/docs/channels.zh.mdx` | 采用上游 Steps/Accordion 组织；保留飞书显式授权、冻结 grantor 和外部文本回复边界。 |
| 10 | `apps/docs/content/docs/cli.ja.mdx` | 采用上游重构和新命令；安装/升级统一内部发布或本地构建，删除上游下载安装指引。 |
| 11 | `apps/docs/content/docs/cli.ko.mdx` | 采用上游重构和新命令；安装/升级统一内部发布或本地构建，删除上游下载安装指引。 |
| 12 | `apps/docs/content/docs/cli.mdx` | 采用上游重构和新命令；安装/升级统一内部发布或本地构建，删除上游下载安装指引。 |
| 13 | `apps/docs/content/docs/cli.zh.mdx` | 采用上游重构和新命令；安装/升级统一内部发布或本地构建，删除上游下载安装指引。 |
| 14 | `apps/docs/content/docs/cloud-quickstart.ja.mdx` | 保留上游分步入门，入口/安装切为 fork 内部部署和内部发布。 |
| 15 | `apps/docs/content/docs/cloud-quickstart.ko.mdx` | 保留上游分步入门，入口/安装切为 fork 内部部署和内部发布。 |
| 16 | `apps/docs/content/docs/cloud-quickstart.mdx` | 保留上游分步入门，入口/安装切为 fork 内部部署和内部发布。 |
| 17 | `apps/docs/content/docs/cloud-quickstart.zh.mdx` | 保留上游分步入门，入口/安装切为 fork 内部部署和内部发布。 |
| 18 | `apps/docs/content/docs/desktop-app.ja.mdx` | 保留上游图示/步骤，使用内部包并说明自动更新默认关闭和手动升级。 |
| 19 | `apps/docs/content/docs/desktop-app.ko.mdx` | 保留上游图示/步骤，使用内部包并说明自动更新默认关闭和手动升级。 |
| 20 | `apps/docs/content/docs/desktop-app.mdx` | 保留上游图示/步骤，使用内部包并说明自动更新默认关闭和手动升级。 |
| 21 | `apps/docs/content/docs/desktop-app.zh.mdx` | 保留上游图示/步骤，使用内部包并说明自动更新默认关闭和手动升级。 |
| 22 | `apps/docs/content/docs/environment-variables.fr.mdx` | 保留上游变量分类/折叠结构；保留 fork 内部地址和自动更新默认关闭约束。 |
| 23 | `apps/docs/content/docs/environment-variables.ja.mdx` | 保留上游变量分类/折叠结构；保留 fork 内部地址和自动更新默认关闭约束。 |
| 24 | `apps/docs/content/docs/environment-variables.ko.mdx` | 保留上游变量分类/折叠结构；保留 fork 内部地址和自动更新默认关闭约束。 |
| 25 | `apps/docs/content/docs/environment-variables.mdx` | 保留上游变量分类/折叠结构；保留 fork 内部地址和自动更新默认关闭约束。 |
| 26 | `apps/docs/content/docs/environment-variables.zh.mdx` | 保留上游变量分类/折叠结构；保留 fork 内部地址和自动更新默认关闭约束。 |
| 27 | `apps/docs/content/docs/github-integration.fr.mdx` | 保留上游 GitHub 配置结构，重放 fork 首次绑定检查与权限说明。 |
| 28 | `apps/docs/content/docs/github-integration.ja.mdx` | 保留上游 GitHub 配置结构，重放 fork 首次绑定检查与权限说明。 |
| 29 | `apps/docs/content/docs/github-integration.ko.mdx` | 保留上游 GitHub 配置结构，重放 fork 首次绑定检查与权限说明。 |
| 30 | `apps/docs/content/docs/github-integration.mdx` | 保留上游 GitHub 配置结构，重放 fork 首次绑定检查与权限说明。 |
| 31 | `apps/docs/content/docs/github-integration.zh.mdx` | 保留上游 GitHub 配置结构，重放 fork 首次绑定检查与权限说明。 |
| 32 | `apps/docs/content/docs/lark-bot-integration.ja.mdx` | 保留上游分步配置，补显式授权、冻结身份、撤权重验和外部文本/链接边界。 |
| 33 | `apps/docs/content/docs/lark-bot-integration.ko.mdx` | 保留上游分步配置，补显式授权、冻结身份、撤权重验和外部文本/链接边界。 |
| 34 | `apps/docs/content/docs/lark-bot-integration.mdx` | 保留上游分步配置，补显式授权、冻结身份、撤权重验和外部文本/链接边界。 |
| 35 | `apps/docs/content/docs/lark-bot-integration.zh.mdx` | 保留上游分步配置，补显式授权、冻结身份、撤权重验和外部文本/链接边界。 |
| 36 | `apps/docs/content/docs/self-host-quickstart.ja.mdx` | 保留上游 Tabs/Steps，自建改为本地源码/镜像构建升级，不给 GHCR 拉取升级命令。 |
| 37 | `apps/docs/content/docs/self-host-quickstart.ko.mdx` | 保留上游 Tabs/Steps，自建改为本地源码/镜像构建升级，不给 GHCR 拉取升级命令。 |
| 38 | `apps/docs/content/docs/self-host-quickstart.mdx` | 保留上游 Tabs/Steps，自建改为本地源码/镜像构建升级，不给 GHCR 拉取升级命令。 |
| 39 | `apps/docs/content/docs/self-host-quickstart.zh.mdx` | 保留上游 Tabs/Steps，自建改为本地源码/镜像构建升级，不给 GHCR 拉取升级命令。 |
| 40 | `apps/docs/content/docs/tutorial.ja.mdx` | 保留上游教程/演示结构，替换内部入口及安装渠道，重放 fork 使用约束。 |
| 41 | `apps/docs/content/docs/tutorial.ko.mdx` | 保留上游教程/演示结构，替换内部入口及安装渠道，重放 fork 使用约束。 |
| 42 | `apps/docs/content/docs/tutorial.mdx` | 保留上游教程/演示结构，替换内部入口及安装渠道，重放 fork 使用约束。 |
| 43 | `apps/docs/content/docs/tutorial.zh.mdx` | 保留上游教程/演示结构，替换内部入口及安装渠道，重放 fork 使用约束。 |
| 44 | `apps/mobile/lib/i18n/resources.test.ts` | 保留移动端资源契约，加入 zh-Hant 及品牌覆盖层断言。 |
| 45 | `apps/web/app/globals.css` | 保留 fork 去营销样式边界，不恢复 custom.css 入口；合入上游字体链。 |
| 46 | `packages/ui/components/ui/dropdown-menu.tsx` | 合入上游菜单尺寸/样式，保留 fork disabled 状态样式。 |
| 47 | `packages/ui/package.json` | 移除 lucide-react，保留 katex 等 fork 依赖；与 workspace catalog/锁文件一起收敛。 |
| 48 | `packages/views/agents/components/agents-page.tsx` | 保留目录树、批量操作等 fork 行为，接入 treeLoading 门控和新图标。 |
| 49 | `packages/views/agents/components/inspector/thinking-picker.tsx` | 保留自适应 thinking 的 props/说明，合入上游模型与 level 回调。 |
| 50 | `packages/views/agents/create/builder-conversation.tsx` | 保留 resumeFollowNonce，合入上游发送尝试状态及新图标。 |
| 51 | `packages/views/chat/chat-page.tsx` | 保留阅读锚点与 WithoutTipAnchors 行为，合入上游图标和布局。 |
| 52 | `packages/views/issues/actions/issue-actions-menu-items.tsx` | 保留 fork 操作集合，接入上游命令栏、运行操作及新图标。 |
| 53 | `packages/views/issues/components/issue-detail.test.tsx` | 保留 fork 已拆分夹具/回归，接入上游本地目录相关用例。 |
| 54 | `packages/views/issues/components/issue-detail.tsx` | 保留 fork 依赖、工程化视图与阅读行为，合入上游详情/运行交互。 |
| 55 | `packages/views/issues/components/issues-header.tsx` | 保留 DAG/成本/YSK 视图信息，接入新图标与轻量列选择器预加载。 |
| 56 | `packages/views/issues/components/wakeups-section.test.tsx` | 保留停止轮询/按需查询断言，接入上游错误与空内容状态断言。 |
| 57 | `packages/views/issues/components/wakeups-section.tsx` | 保留 OL-1145 enabled/停轮询门控，接入 isError/hasContent。 |
| 58 | `packages/views/issues/surface/issue-surface.tsx` | 保留 DAG/视口和 Board 懒加载；常量抽至 board-layout.ts，消除同名静态导入。 |
| 59 | `packages/views/layout/help-launcher.tsx` | 保留 fork 支持入口与去营销边界，换用上游图标。 |
| 60 | `packages/views/layout/join-discord-card.tsx` | 保留 fork 飞书入口/显示门控，QrCode 复用 MessagesSquare。 |
| 61 | `packages/views/layout/use-dashboard-guard.ts` | 保留带编码 next 的登录回跳，合入上游 recent-visits store。 |
| 62 | `packages/views/locales/index.ts` | 双方命名空间取并集；接入 zh-Hant 品牌覆盖层，保留 fork 扩展资源。 |
| 63 | `packages/views/my-issues/components/my-issues-header.tsx` | 保留 fork 我的任务视图及视口行为，接入上游菜单和图标。 |
| 64 | `packages/views/onboarding/steps/step-platform-fork.test.tsx` | 保留 fork 部署入口/安装说明断言，接入上游平台选择行为。 |
| 65 | `packages/views/projects/components/project-detail.tsx` | 保留工程化/DAG 扩展，合入上游项目详情与图标。 |
| 66 | `packages/views/runtimes/components/runtimes-page.tsx` | 保留配额/指标列；表头和云行各补 METRICS_CELL，接入上游图标与运行操作。 |
| 67 | `packages/views/settings/components/code-tab.tsx` | 保留 fork 既有确认交互，接入上游图标。 |
| 68 | `packages/views/skills/components/skills-page.tsx` | 保留目录树/批量操作，接入 treeLoading 门控和新图标。 |
| 69 | `pnpm-lock.yaml` | 根据最终 workspace/package 依赖重建，和移除 lucide-react 及 sqlc 输出在同一合并中交付。 |
| 70 | `scripts/ci-scope.test.mjs` | 接入附件预览路径覆盖，保留 fork E2E/品牌/后端范围契约。 |
| 71 | `server/go.sum` | 接入上游 Go 依赖，保留 fork 两条 x/sys 伪版本 go.mod 条目。 |
| 72 | `server/internal/daemon/client.go` | capabilities 取并集：WorkflowStatusRules、CLICommentSteer、thinking-adapt-v1、you-should-know-v1。 |
| 73 | `server/internal/daemon/dsh_profile.go` | 采用第四态 needs_migration；由启动器解析 home，纯函数检查 manifest，旧桥接不得自动覆盖。 |
| 74 | `server/internal/daemon/execenv/hermes_home.go` | 合入上游 session home 语义，保留 fork installed 参数。 |
| 75 | `server/internal/daemon/execenv/hermes_sessions_test.go` | 按上游持久化 session home 契约调整用例，保留 fork 安装与清理边界。 |
| 76 | `server/internal/daemon/testmain_test.go` | 保留原生媒体探测，合入 Pi 清理夹具；双方 TestMain 初始化并存。 |
| 77 | `server/internal/handler/agent.go` | 合并 ModelOverride；保留 OL-1144 快照瘦身和上游 foreign-chat 脱敏。 |
| 78 | `server/internal/handler/daemon.go` | 保留外部会话 root 授权并先于私聊创建者 404 检查；外部媒体能力为 false，接入上游 daemon 能力。 |
| 79 | `server/internal/handler/issue.go` | 保留 writeFields；将 handoff != nil 作为 workflowAssignee 传至原子更新/nullable 刷新。 |
| 80 | `server/internal/handler/task_lifecycle.go` | 接入按 task 重跑和 fresh 参数；保留依赖错误映射，外部来源重跑保持冻结授权。 |
| 81 | `server/internal/handler/workspace.go` | 合入上游删除步骤，保留 media/YSK 清理。 |
| 82 | `server/internal/integrations/lark/outbound.go` | 合并 botOpenID/workspaceID 签名；以上游租约/附件闭包为骨架，保留多人验证 mention 和 literal 路径。 |
| 83 | `server/internal/qoderruntime/prompt.go` | 接入 Qoder Cloud Agents 命名，保留 Labrastro 用户可见品牌。 |
| 84 | `server/internal/service/agent_ready.go` | 采用上游缺失/需手动迁移提示，保留禁止隐式安装升级语义。 |
| 85 | `server/internal/service/autopilot.go` | 采用派发链结算并替换已删除 sqlc 参数类型；保留首终态幂等与状态卡语义。 |
| 86 | `server/internal/service/autopilot_quota.go` | 使用本地 completion/failure 参数类型；在 guarded SQL 中原子冻结状态和结算配额。 |
| 87 | `server/internal/service/builtin_skills/multica-platform/SKILL.md` | 保留 fork 路由行，合入上游含 --steer 的 mentions 说明。 |
| 88 | `server/internal/service/builtin_skills/multica-platform/references/runtimes.md` | 合入上游运行时说明，保留 fork 品牌和运行约束。 |
| 89 | `server/internal/service/builtin_skills/multica-platform/references/skill-import.md` | 保留 Repository skill packages，合入上游导入流程。 |
| 90 | `server/pkg/agent/antigravity.go` | 保留 OL-1074/OL-1112 终态判定；#9534 的冲突测试预期不恢复。 |
| 91 | `server/pkg/agent/claude.go` | 合入 #9652，并保留 fork rate_limit_event 的订阅配额观测。 |
| 92 | `server/pkg/db/generated/agent.sql.go` | 不手工选择 generated 一侧；从最终查询/schema 全量 make sqlc 生成。 |
| 93 | `server/pkg/db/generated/source_context.sql.go` | 从合并后的查询/schema 全量生成，保留 fork 列。 |
| 94 | `server/pkg/db/queries/source_context.sql` | 合并双方查询说明，供全量 sqlc 使用。 |

## 干净合并中的必做语义收敛

- 外部评论在解析 `steer_task_ids` UUID 前强制令 `steer=false`、`steer_task_ids=nil`；派发层 `steerCommentAgentTriggers` 再阻断外部会话补充运行，返回的 `can_supplement=false`。正常跟进任务仍携带冻结 grantor 和 conversation root。
- 飞书 Patcher 对外部会话不查询/生成出站附件、不读取工作区链接元数据、不重写 issue 引用；literal 文本路径保留已验证的 mention。`outbound.go` 合成 `botOpenID, workspaceID, payload` 参数，保留上游投递租约、仅附件回复及发送闭包。`outbound_media_delivery` 在每次外发操作前核验 installation 的 active/app ID/tenant/region/workspace、active session、delivery 绑定的 installation/chat、task agent，并调用 `AuthorizeConversationTask`；外部/带 Grant 的配置缺 checker 时拒绝。
- `GET /api/tasks/{taskId}/messages` 先检查 fork 外部会话 root，再执行上游私聊创建者 404。OL-1144 快照继续清空所有任务 result，并仅对他人私聊清空 error；普通任务列表继续采用上游私聊脱敏。
- DSH 的 probe 与自动安装共用 missing/ready/unknown/needs_migration 四态。检查 home 来自启动器解析；纯 manifest 检查不读取 daemon 自身 DSH_HOME 替代执行环境。旧桥接/依赖需要手动迁移，不能因存在 profile bundle 就当作缺失覆盖；unknown 不等同 missing。
- OL-1145：`WARM_ON_IDLE=[]`，保留悬停预取。wakeups 仅在 open 且 workspace/issue 有效时查询；关闭状态且已知数据为空后停轮询，运行详情按展开按需加载且不另开周期轮询；同时保留上游 isError/hasContent。agents/skills 保留 treeLoading 门控。
- 保留 Board 懒加载，将布局常量移入 `board-layout.ts`；运行时表头与云行保留两个指标占位；property picker 的显式 nativeButton 优先于宿主推导，修复 label-picker 类型与目录树测试 mock 的 search 字段。
- Go 冲突外调用方传递 `workflowAssignee=handoff != nil`，移除已删除的 `ProjectWorkflowsV1` flag 引用；Hermes 新增第九个参数的调用方补 nil，保留 installed 语义。
- **#9534 已被 OL-1074 / OL-1112 决策表第 3 行替代**。撤销其三处 Antigravity 测试预期变更；不把历史 active、更新的部分输出或 cancelled 状态误判为成功。Claude 合入 #9652，同时保留 rate_limit_event。
- 全量 `make sqlc` 覆盖所有生成物，特别是新 `task_cleanup.sql.go` 的 RETURNING 任务必须含 `dependency_admission` 和 `conversation_root_task_id`。不得手改生成结构体补列。
- 引导页将 fork DefaultCloudHost 识别为 cloud；CLOUD 常量由 `@multica/core/deployment` 派生，保留内部地址。OpenCode 默认 ACP，回退 `MULTICA_OPENCODE_TRANSPORT=run` 只取 daemon 进程环境且影响整机，agent custom_env 的 MULTICA_* 仍剥离；测试分别覆盖 run 与 ACP。
- 品牌源/覆盖层修正涵盖 zh-Hant、Qoder Cloud Agents、GA、桌面原生对话框、Go 提示及内置技能；不扩大守卫例外。E2E 的 lucide SVG 选择器迁至 icon SVG。
- 文档采用 #9498 的 Steps/Tabs/Accordion/演示骨架，重放内部发布地址、自动更新默认关闭、本地构建/升级、GitHub 首次绑定和飞书显式授权。同步检查干净合并的法文与开发/移动构建页，删除上游 brew tap、raw GitHub installer、官方下载安装页、GHCR 拉取升级指引。`scripts/check-docs-distribution.mjs` 及其测试纳入 `test:brand`。

## 外部会话路由与请求语义

固定基线到结果的 router inventory 仅新增 `POST /api/tasks/{taskId}/rerun`。user-auth 白名单仍 60 条（37 GET、下表 23 条写/查询），deny 从 435 变为 436；daemon-auth 42、public/capability 24、plugin-auth 9 条仍属于独立认证，不视为外部会话白名单。未登记路由默认拒绝。请求 inventory 补入新 `steer=bool[steer]`；`steer_task_ids` 在 fork 基线已存在，本次必须同时强制清空这两个入口。

共同授权前提：workspace/调用者来自认证上下文，正文不能指定新 grantor；所有被接受的 issue/comment/attachment/project/assignee 等 UUID 必须通过各自现有作用域与调用检查。外部 task 继续使用持久化的 conversation ancestry 和冻结 grantor；撤权、跨 workspace、来源证据缺失均拒绝。派发、子任务、重试、wakeup、延迟派发不能改用 runtime owner。下表逐项列出正文、默认值、校验和副作用；“无变化”指与 fork 基线相比，不代表绕过这些共同前提。

为避免在两个 create/update 入口漏字段，完整请求集合如下（精确类型/JSON tag 以 `labrastro-external-requests.tsv` 为门禁）：

- **C（创建）**：title、description、status、priority、assignee_type/id、parent_issue_id、project_id、stage、start_date、due_date、attachment_ids、label_ids、properties、origin_type/id、allow_duplicate，以及 blocked_by、expected_dependency_version、dependency_override。字段无增删；默认 status=todo、priority=none、allow_duplicate=false。title 必填，stage 若传入须 ≥1；重复 JSON key 拒绝；parent/project/label/附件与指派目标按 workspace/类型/调用权限校验。properties 的共享校验变化见第 20 行。
- **U（更新）**：title/title_base、description/description_base、expected_revision、status、priority、assignee_type/id、parent_issue_id、project_id、stage、position、start_date、due_date、attachment_ids、duplicate_of_issue_id、enter_workflow_step、handoff_note、stop_previous_assignee_runs、suppress_run，以及 blocked_by、expected_dependency_version、dependency_override。字段无增删；bool 默认 false，省略可选字段保留既有值；revision/base、依赖版本、作用域、状态/指派/流程校验保留。外部请求中只要出现 stop_previous_assignee_runs 键就拒绝（包括 false/null），不因默认值扩大取消权限。

证据列的 **R** 指真实 router/policy/request inventory；**W** 指现有协作写入、dependency/workflow、child authority、wakeup/recovery 回归；**S** 指新增 steer 回归；**F** 指新增 frozen-rerun 回归；**M** 指媒体授权回归。列出覆盖不等于已跑通过，执行状态统一见末节。

| # | Method / route → handler | 请求、默认和校验：基线 → 结果 | 写入/派发与 actor/workspace 边界 | 回归证据 |
| --- | --- | --- | --- | --- |
| 1 | `POST /api/issues/query` → `QueryIssues` | `map[string]string` 无新增字段/默认；接入上游 identifier 数字解析规范化，保留过滤值校验。 | 只查询当前授权 workspace，不生成任务/变更配置；过滤的 actor/assignee 不成为执行主体。 | R、查询既有回归 |
| 2 | `POST /api/issues` → `CreateIssue` / `prepareIssueCreate` | C；共享属性校验见第 20 行；依赖字段仍经写入 envelope，不能被嵌入 JSON 解码吞掉。 | 原子创建 issue、labels、properties、附件和可选依赖；指派/子任务遵守 invoke 检查并继承 grant/root，origin 字段不取代授权主体。 | R、W |
| 3 | `POST /api/issues/with-dependencies` → `CreateIssueWithDependencies` | C；WritesEnabled、依赖版本/override/环与跨域校验不变。 | 同第 2 行，但明确走依赖事务入口；issue 与真实边原子提交，不恢复已退役派发期准入。 | R、W |
| 4 | `PUT /api/issues/{id}` → `UpdateIssue` | U；保留 writeFields，并将 `handoff != nil` 传给原子更新/nullable 刷新，防止隐式交接抹掉 assignee。 | issue、指派/晋级、现有流程交接与派发；不会授予流程配置权限，不得取消成员运行；派发仍采用冻结授权。 | R、W |
| 5 | `PATCH /api/issues/{id}/with-dependencies` → `UpdateIssueWithDependencies` | U；与第 4 行同一 workflowAssignee 修复，保留依赖版本/override 及 WritesEnabled 校验。 | issue/依赖图/交接在事务内更新，不能跨 workspace 连边或借派发更换 grantor。 | R、W |
| 6 | `POST /api/issues/{id}/comments` → `CreateComment` | 原 content、type、parent_id、attachment_ids、suppress_agent_ids、steer_task_ids 保留；新增 steer 默认 false；type 默认 comment，content 清理 NUL 后非空、类型/parent/附件校验保留。外部两 steer 字段在 UUID 解析前清空。 | 建评论、附件/mention 与普通跟进派发；不能 supplement 成员 run；返回 can_supplement=false，跟进 run 保留 conversation root/grantor。 | R、W、S |
| 7 | `PUT /api/comments/{commentId}` → `UpdateComment` | content、content_base、expected_revision、attachment_ids、suppress_agent_ids 无变化；保留内容/修订/附件校验；reconcile 传空 commentSteerRequest。 | 限当前外部 agent 自己 authored 的评论，不把 grantor 的管理员身份扩成编辑他人权限；编辑/mention 对账及后继任务继承授权。 | R、W、S |
| 8 | `DELETE /api/comments/{commentId}` → `DeleteComment` | 无 body，ID/作者/作用域校验不变。 | 删除评论并失效关联待执行任务；外部作者边界不扩大。 | R、W |
| 9 | `DELETE /api/comments/{commentId}/keep-replies` → `DeleteComment` | 无 body，沿用评论删除校验，路由选择 keep-replies 分支。 | 保留回复地删除目标评论，关联任务处理同既有语义；不取得其他作者权限。 | R、W |
| 10 | `POST /api/comments/{commentId}/resolve` → `ResolveComment` | 无 body，comment/issue/workspace 校验不变。 | 记录 resolve 与事件；actor 来自上下文，不新增派发/调度配置入口。 | R、W |
| 11 | `DELETE /api/comments/{commentId}/resolve` → `UnresolveComment` | 无 body，作用域与状态校验不变。 | 撤销 resolve 并发布既有事件，身份边界与第 10 行一致。 | R、W |
| 12 | `POST /api/comments/{commentId}/reactions` → `AddReaction` | `emoji:string` 无变化；空值/合法性与 comment 作用域校验保留。 | 以认证 actor 添加 reaction，不能指定第三人 user/agent。 | R、W |
| 13 | `DELETE /api/comments/{commentId}/reactions` → `RemoveReaction` | `emoji:string` 无变化，校验同第 12 行。 | 删除当前 actor 的 reaction，不删除任意其他 actor 的行。 | R、W |
| 14 | `POST /api/issues/{id}/reactions` → `AddIssueReaction` | `emoji:string` 无变化，issue/workspace 与 emoji 校验保留。 | 以认证 actor 添加 issue reaction，不新增执行主体。 | R、W |
| 15 | `DELETE /api/issues/{id}/reactions` → `RemoveIssueReaction` | `emoji:string` 无变化，校验同第 14 行。 | 删除当前 actor 的 issue reaction。 | R、W |
| 16 | `POST /api/issues/{id}/labels` → `AttachLabel` | `label_id:string` 无变化；UUID、所属 workspace 与 issue label 类型校验保留。 | 仅附加已有 label 并产生既有事件；不允许创建/配置 label。 | R、W |
| 17 | `DELETE /api/issues/{id}/labels/{labelId}` → `DetachLabel` | 无 body，issue/label UUID 与作用域校验不变。 | 仅移除关联，不删除标签定义。 | R、W |
| 18 | `PUT /api/issues/{id}/metadata/{key}` → `SetIssueMetadataKey` | `value:JSON` 无变化；key/value、key 数量和 8KB 总量限制保留。 | wakeupWrite 在 issue/workspace 范围原子更新、记录 revision/事件；不允许借 metadata 指定 grantor。 | R、W |
| 19 | `DELETE /api/issues/{id}/metadata/{key}` → `DeleteIssueMetadataKey` | 无 body，key/issue/workspace 校验不变。 | 删除指定 metadata 键，保留既有 revision/wakeup actor 来源。 | R、W |
| 20 | `PUT /api/issues/{id}/properties/{propertyId}` → `SetIssueProperty` | `value:JSON` 无变化；共享 ValidateValue 新增 multi_select 去重后 ≤50，错误提示最多列 20 选项；定义选项上限 50→200。multi_actor ≤20 和 16KB properties bag 保留。 | 当前 workspace 中既有定义的值写入；定义配置路由仍 deny，不能借值写入新增选项/主体/调度。创建 C 也使用此校验。 | R、W、issueproperty 校验用例 |
| 21 | `DELETE /api/issues/{id}/properties/{propertyId}` → `DeleteIssueProperty` | 无 body，property/issue/workspace/定义校验不变。 | 仅移除该 issue 值及既有事件，不删除/变更定义。 | R、W |
| 22 | `POST /api/upload-file` → `UploadFile` | multipart file、issue_id、comment_id、task_id、chat_session_id 无变化；大小、MIME、绑定对象及上传者作用域校验保留。 | 内部附件上传/关联并不授予外部飞书媒体投递；D3 单独关闭新增出站链，已有异步投递每次重验。 | R、W、M |
| 23 | `DELETE /api/attachments/{id}` → `DeleteAttachment` | 无 body，attachment UUID、workspace、上传者/既有管理员分支校验不变。 | 删除授权范围附件，不改变其他资源配置或扩大外部出站能力。 | R、W |

### 两个 rerun 入口的 denied 语义

| 路由 | 成员 API 语义变化 | 外部会话边界 / 回归 |
| --- | --- | --- |
| `POST /api/issues/{id}/rerun` → `RerunIssue` | 既有请求 `task_id` 可选，空 body 指向当前 assignee；本次共享 rerun 实现默认 fresh=false，不再一律强制新会话。先加载授权 issue，历史/当前目标均经 canInvoke。 | 维持 deny，外部 task 凭据返回 403 `external_conversation_forbidden`。成员传外部 source task 时走 D4：来源再授权、沿用 root 和冻结 grantor，在取消 pending/新建任务前拒绝失效授权。 |
| `POST /api/tasks/{taskId}/rerun` → `RerunTaskByUser` | 新增；body fresh 默认 false。task 必须同 workspace 且关联 issue，非法 UUID/非 issue run 为 400，找不到为 404；加载 issue 后走同一 rerun 实现。 | inventory 新增 deny；fresh=true/false 均不扩大外部 task 权限。`TestExternalConversationTaskRerunRouteDenied` 与 `TestExternalConversationMemberRerunKeepsFrozenGrant` 覆盖拒绝、fresh 两值、有/无触发评论、独立操作成员与 grantor、撤销安装不产生 pending 变动。 |

`TestExternalConversationCannotSteerMemberTurn` 使用支持 steer 的 claude provider，同时覆盖命名目标和全局 steer：外部评论没有 supplement、保留后继 root/grantor 与评论证据，且 can_supplement=false；同一 run 接收成员的同样请求时必须成功绑定。`TestConversationClaimOmitsFileDeliveryCapability` 先声明适配器支持文件，再验证外部 claim=false、普通 claim=true。`TestExternalConversationReplyKeepsTextOnlyBoundary` / `TestMediaCredentialsRecheckFrozenConversationGrant` 覆盖 D3 文本边界及撤权重验；这组新回归必须与已有 route/request inventory、workflow/dispatch/child authority/wakeup/recovery/transcript 回归一起执行。

成员重跑的回归以不同于 grantor 的成员评论作为继承提示上下文，冻结授权仍优先于该评论；撤权后同时检查任务总数与首轮 queued 任务的状态不变。普通评论触发继续记录 comment 证据和 comment_source/delegation 来源；仅重跑通过显式 rerun_of_task_id 选择冻结来源优先，避免将普通评论误记为 issue_assignment。

create_issue 状态卡同时写明 run completed/failed 和任务的 status at settlement。失败运行搭配 done、成功运行搭配 in_progress 都按两项独立事实展示；缺失历史状态快照时明确说明 unavailable，不读取后来状态，不再使用 first terminal state 文案。

## 迁移身份与执行顺序

下表为 Git blob ID，不是迁移版本号或数据库执行 checksum。fork/upstream 列均由上述精确提交读取，结果列用 `git hash-object --no-filters` 得到。全量同名 migration 对比中，上游原件与结果不同的历史迁移仍只有 467 的 up/down；相对 fork 基线新增的 migration 只有 593/594 的四个文件。467 的 merge-base blob 与下表上游列相同，其余下表文件在 merge base 中不存在。

| migration 文件（位于 `server/migrations/`） | fork 基线 blob | v0.7.2 原件 blob | 结果 blob |
| --- | --- | --- | --- |
| `467_autopilot_trigger_creator_from_autopilot.down.sql` | `ee35bfe58e35fccdeee0e32d4ae077fac5136c15` | `1c32fd2368ae0ae78e2bf5dc8886b780cbed69eb` | `ee35bfe58e35fccdeee0e32d4ae077fac5136c15` |
| `467_autopilot_trigger_creator_from_autopilot.up.sql` | `9d9719921186f406855115c4a5a7e73b9c377896` | `40a3888825985738469996a1596110ae62038ac6` | `9d9719921186f406855115c4a5a7e73b9c377896` |
| `593_runtime_profile_add_devin.down.sql` | 不存在 | `9f0e173b2a546ed720712f3efaa91ec4f3ff898a` | `9f0e173b2a546ed720712f3efaa91ec4f3ff898a` |
| `593_runtime_profile_add_devin.up.sql` | 不存在 | `4ff92ae224f745b138213b2ff199f43b89ad50dd` | `4ff92ae224f745b138213b2ff199f43b89ad50dd` |
| `594_agent_task_process_cleanup_index.down.sql` | 不存在 | `e0d5531d0abb1ecadc518fc17e85cb5021e91da7` | `e0d5531d0abb1ecadc518fc17e85cb5021e91da7` |
| `594_agent_task_process_cleanup_index.up.sql` | 不存在 | `d29f7f716e3a7b5d02576b9de466cf581fb254c7` | `d29f7f716e3a7b5d02576b9de466cf581fb254c7` |
| `9009_labrastro_wakeup_conversation_provenance.down.sql` | `e11a3ea58c80ba7a771a7b9b11d576ee456a7d47` | 不存在 | `e11a3ea58c80ba7a771a7b9b11d576ee456a7d47` |
| `9009_labrastro_wakeup_conversation_provenance.up.sql` | `d041acc20cda075e42c4d408577fd02b9e16f703` | 不存在 | `d041acc20cda075e42c4d408577fd02b9e16f703` |
| `9028_labrastro_wakeup_skip_handed_to_agent.down.sql` | `ab838bc34cd37165af3e8209bb3e762faac12711` | 不存在 | `ab838bc34cd37165af3e8209bb3e762faac12711` |
| `9028_labrastro_wakeup_skip_handed_to_agent.up.sql` | `1ab3d16b272fe2073afd083183e4a82aa8697ab2` | 不存在 | `1ab3d16b272fe2073afd083183e4a82aa8697ab2` |

- **467（既有例外）**：保留 OL-110 T14 的 no-backfill。上游根据 autopilot creator 回填旧 trigger 的 created_by；fork 不从该关系推断遗失的授权主体，历史证据不完整的触发继续 fail closed。up/down 均保留 fork 原件，不重写已落库版本、不在本次部署重跑，也不以更改清单暗中恢复上游 DML。
- **593**：runtime_profile CHECK 白名单加 devin，覆盖 fork 现有协议族；DROP/ADD 在同一 ALTER，NOT VALID 避免扫描旧行。down 移除 devin 白名单且仍 NOT VALID，已有行仍可读，新写入受约束。文件保持上游字节。
- **594**：并发建立 `idx_agent_task_queue_process_cleanup`，键为 `(agent_id, runtime_id, completed_at DESC, id DESC)`，谓词为 cleanup_unconfirmed 或 cancelled 且 process_cleanup_pending=true；down 并发删除。迁移器已有该索引的失败清理登记及审计用例；文件保持上游字节。
- **9009/9028**：保留 conversation source-task/grantor provenance 和 handed-to-agent 跳过语义。593/594 不重新定义 capture_issue_wakeup，因此此次无需再加 fork migration 来重放函数；不能通过改旧 migration 来修未来替换。

`capture_issue_wakeup` 的定义清单保持如下，需由 `TestLabrastroWakeupCaptureMigrationInventory` 锁定：

1. `518_wakeup_event_capture.up.sql`
2. `520_collaboration_wakeup_events.up.sql`
3. `523_wakeup_registration_source.up.sql`
4. `530_wakeup_bounded_capture.up.sql`
5. `532_wakeup_actor_capture.up.sql`
6. `579_wakeup_skip_handed_to_agent.up.sql`
7. `9009_labrastro_wakeup_conversation_provenance.up.sql`
8. `9028_labrastro_wakeup_skip_handed_to_agent.up.sql`

| 路径 | 起始 schema 与实际应执行顺序 | 必须验证的结果 | 执行证据 |
| --- | --- | --- | --- |
| 新装 | 空库；上游迁移至 593/594，再运行 fork 9001…9046 | 最终约束/索引与升级一致，wakeup 仍保留来源 task/grantor，未知 legacy 主体拒绝 | 707 条 ledger；无无效索引，与升级 schema 一致 |
| 存量升级 | fork 基线已记录至 9046；迁移器逐文件查 ledger，补跑未记录 593/594，不重跑 467/9009/9028 | 旧函数 provenance 不被覆盖，devin 可写、清理索引存在，既有授权/配额数据不改变 | 705→707；实际只补跑 593/594，来源函数保留 |

实测迁移路径：生产 migrate CLI 在新装库执行全部 707 个版本；另一库先从精确 fork 基线执行 705 个版本（已到 9046），再补跑 593/594 到 707。两库 schema-only dump 除 pg_dump 随机 restrict token 外完全相同，均无无效索引，capture_issue_wakeup 保留 conversation_root_task_id。测试库与现有本机服务独立，不接触生产数据。

## gofmt：11 条上游原件例外

清单 Source 已更新为 `multica-ai/multica@f05a525ac8e82f5e9169ad078e44f166fad6d5c8`。原有 10 条保留，新增 `comment_agent_steer_test.go` 一条；以下每条已逐项比对上游 `git rev-parse <sha>:<path>` 与结果 `git hash-object --no-filters`，blob 相等。任何后续 fork 修改使例外自动失效；不以 fork 内容生成新例外，不豁免目录。格式门禁执行结果仍以末节为准。

| 文件 | v0.7.2 / 结果共同 blob |
| --- | --- |
| `server/internal/auth/cloud_pat_test.go` | `f90234704257dffc4ed7345d9f9f4273d5215fdb` |
| `server/internal/auth/membership_cache_test.go` | `ad7d259dec1d351b8482db2a81ef9e1a942c9fde` |
| `server/internal/integrations/lark/markdown_detect.go` | `fa16c63ab8b87beda8104898b29b06a96e7d340d` |
| `server/internal/integrations/lark/registration_test.go` | `1b6a3136b98605d1148422be5a518f3b6479edc7` |
| `server/internal/integrations/lark/ws_chunk_assembler.go` | `f028826a565d6312232ac4642b865731c40db2bd` |
| `server/internal/integrations/lark/ws_chunk_assembler_test.go` | `ac5ab5e9f87d68db7dbf13445285e2cf9bb03d90` |
| `server/internal/integrations/lark/ws_frame_test.go` | `31e8b12d48ef29d696024552dede3a9c3931fd65` |
| `server/internal/integrations/telegram/outbound.go` | `00ec25c99287b284ca0b85f14015884c486f9f5b` |
| `server/internal/middleware/daemon_auth_test.go` | `624edfd23a69369105668bbbcfe1a90815401662` |
| `server/pkg/featureflag/eval_context_test.go` | `620746cb12de71098d96282a23dc0c6331677b8f` |
| `server/internal/handler/comment_agent_steer_test.go` | `d18353b021e0a9f24d906c9d2776802adca1b322` |

## 验证与交付记录

本地验证使用 Go 1.27.3、Git 2.47.1 和隔离 PostgreSQL 17。最终 merge SHA、PR Checks、日志/截图及顺序审阅记录随 OL-1177 交付评论交付；本地工作树结果不代替最终 head 的 CI required。

| 检查 | 命令 / 要求 | 结果 |
| --- | --- | --- |
| Git 身份 | 两父 `--no-ff` 合并；第一父为本记录 fork 基线、第二父为精确 tag；无冲突标记 | 交付提交使用这两个固定 parent；不可变 SHA 与核验输出随交付记录 |
| 锁文件 | 联网重算锁文件；`pnpm install --frozen-lockfile --lockfile-only --offline --ignore-scripts` 再校验 | 12 个 workspace 通过，未改依赖图 |
| sqlc 原子性 | `GOTOOLCHAIN=go1.27.3 make sqlc`；再次生成后 generated 无差异 | 通过；88 个生成文件逐字节一致，go mod tidy 无新增差异 |
| 后端全量 | 隔离 PostgreSQL 17，Go 1.27.3；`make test` 的 regular/message-delivery race groups + 完整 agent 分区 | regular 与 message-delivery 全过；其余 agent `-race -p 1 -parallel 1` 全过，4 项 Cursor 在 Linux 6.12.107 guest `-race` 全过 |
| 迁移 | 新装/9046 存量升级；完整 migrations race suite 含 WakeupCaptureMigrationInventory 与索引审计 | 通过，两库最终 schema 相同且均无无效索引 |
| 外部会话 | 真实 router/request inventory 与本记录 R/W/S/F/M；inventory 刷新后正常重跑，不以 UPDATE 模式当验证结果 | 通过；60 allow/436 deny，新 rerun 入口拒绝；steer、重跑冻结授权、媒体边界及撤权回归通过 |
| 后端品牌 | server/：`GOTOOLCHAIN=go1.27.3 go test ./internal/branding` | 通过，亦纳入完整 race suite |
| Go 格式 | `bash scripts/check-gofmt.test.sh`、`bash scripts/check-gofmt.sh` | 通过，11 个例外均为精确上游原件 |
| 前端类型 | `pnpm typecheck` | 10/10 通过，Mobile 独立 typecheck/lint 亦通过 |
| 前端 lint | `pnpm lint` | 通过，保留既有 warning |
| 前端测试 | `VITEST_MAX_WORKERS=4 pnpm test --concurrency=1 --env-mode=loose` | 全部通过；core 3288、views 8493、UI 350、web 346、desktop 941（另 bundle 12）、docs 19、UI Lab 46；Mobile 301 独立通过 |
| 品牌/GA/docs | `pnpm test:brand`；含 GA 部署禁设、docs 发布链接守卫与品牌覆盖层 | 通过；9 个脚本用例及 15 个品牌断言，守卫未放宽 |
| 交叉编译 | Windows amd64/macOS arm64 的 CLI build、agent/daemon test compile | 六项通过；未声称原生安装验证 |
| 生成物/部署脚本 | 附件预览生成器；Compose/Helm 渲染；测试包装、临时数据库/开发环境及 CI 脚本 | 生成器前后 SHA256 一致；74 个 Node 用例、相关 shell 契约通过 |
| 保留 E2E | `bash scripts/test-retained-e2e.sh`；包含 onboarding-smoke、dag-task-lines、issue-dependencies、comments，记录确切 head 和 report artifact | 24/24 通过（1.9m）；额外 Board/tree 视觉场景 1/1 通过，DAG/Board/tree 截图人工核对；隔离迁移、生产构建和服务器关闭均完成，报告随交付 |
| 最终 CI | `CI required` 的两个 validated aggregates | 精确 head 的结果与 run URL 随 PR/交付评论记录；失败不得合入 |
| 审阅 | 非草稿 PR 面向 main；两位专家依次审阅最终 SHA | 随交付提请第一位专家；换 head 后重新审阅，由审阅者合入 |

补充验证：主机为 Linux 5.15，`make test` 原生 agent 阶段的四项 Cursor 用例需要 Linux 6.9 pidfd flags；这四项单独在 Linux 6.12.107 QEMU guest 中运行，其余 agent 在主机使用精确互补的 `-skip` 模式，未修改源码 skip 或断言。主机首次并发执行还有一项 Claude 5 秒启动 deadline 在 Web 编译负载下超时；降低并发后的完整互补 agent suite 通过（100.574s）。QEMU 退出码为 0，测试 helper 均已清理。

文档局部结果：docs typecheck 通过；docs Vitest 4 文件/19 测试通过；250 个 MDX 均解析通过；发布链接守卫及其 3 个测试通过；文档 Tabs/Steps/Accordion 嵌套与 `git diff --check` 通过。这些局部检查不构成原生 Desktop 安装、移动端模拟器或生产部署验证。

CI 首轮的 views 第二分片暴露了消息窗口测试的同步竞态：AnimatePresence 移除 DOM 后，Query 订阅的 passive-effect 清理可能尚未完成。关闭及缓存回收两个场景现在同时等待 DOM 消失与 observer 归零，再检查重开订阅/请求行为；保留原 0/1 断言和默认 timeout，不改产品逻辑。最终 CI 结果按修正后的合并 SHA 记录。

首审修正覆盖 #1–#5：steer 使用真实支持的 provider 和成员正向对照；状态卡分别展示运行结果与结算时任务状态；撤权断言检查 queued 状态；claim 文件能力有开启适配器的正反对照；普通评论保留归因证据，重跑单独优先冻结授权。新状态卡和评论证据断言在旧实现上失败，修正后定向 race 通过。四项独立 overlay 变异均被杀死：同时删除两处 steer 守卫、删除 claim 文件能力守卫、重跑错误采用评论授权、将撤权检查延后至 pending 清理之后。完整包回归与最终 SHA 的 CI 结果随交付评论附上。

继续保留 OL-1144/OL-1146、请求治理 OL-1145、目录树 #214、阅读/视口 #210 等已有契约。DAG stage 顺序、真实依赖边、独立展开、详情返回视口、共享 Web/Desktop 接线和无营销面应由既有回归及审阅确认。部署、迁移上线、发布与更新源启用另行授权。
