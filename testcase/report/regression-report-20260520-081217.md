# OPE-1005 / PR !178 QA Gate Regression Report

## 结论

FAIL

PR !178 分支 `feat/ope-1005-upgrade-v0.3.3` 已按最新 QA Gate 重新执行覆盖缺口分析、补充用例、编译复核、本地 Docker provision 和 agent-browser 浏览器回归。编译与大部分已执行 UI 路径通过，但 `tc-052-onboarding-v2-questionnaire` 在真实浏览器流程中失败：onboarding runtime 步骤的“暂时跳过”按钮可见且可点击，但点击后无请求、无跳转、无错误提示，用户卡在该步骤。

## 基本信息

- Issue: OPE-1005 `e8d7ff40-2cb7-485d-a06b-fb112a2d6d63`
- PR: https://gitee.com/wujie-agent/multica/pulls/178
- 分支: `feat/ope-1005-upgrade-v0.3.3`
- 验收提交: `a4779911 test: add regression cases for v0.3.3 merge coverage (OPE-1005)`
- Repo: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3`
- 前端 URL: `http://localhost:3001`
- Docker project: `ope-1005`
- Browser session: `abtp-ope-1005-20260520-073226b`
- Auth file: `testcase/auth/auth.json`
- 测试账号: `tester@multica.com`，固定验证码登录 `888888`

## 用例覆盖缺口分析

### covered_existing

- `tc-001-fixed-verification-code-login.md`: 固定验证码登录、认证会话建立。
- `tc-011-agent-permission-controls.md`: Fork 的 runtime 编辑权限收紧，覆盖 OPE-954。
- `tc-039-comment-body-collapse.md`: Fork 长评论折叠/展开，覆盖 OPE-700。
- `tc-040-weixin-notification-channel.md`: Fork OpenClaw WeChat 通知渠道与事件开关，覆盖 OPE-544。
- `tc-041-wiki-creator-activity.md`: Fork Wiki 创建者展示与轻量活动记录，覆盖 OPE-843。
- `tc-042-issue-subscription.md`: Fork Issue 订阅/取消订阅与订阅者排序，覆盖 OPE-995。
- `tc-043-mobile-issue-labels-parent.md`: Fork 移动端 issue 标签、开始日期、父 issue。
- `tc-044-mobile-inbox-batch.md`: Fork 移动端 Inbox 批量已读/归档。
- `tc-045-mobile-label-filter.md`: Fork 移动端 issue 标签筛选。
- `tc-033-mobile-app-core.md`: 既有移动端核心流程可作为 H5 基线补充。
- `tc-035-cli-managed-update.md`: CLI managed update / manifest 路径可覆盖 install 相关基线。
- `testcase/browser-regression-guide.md`: 已包含上述既有用例索引。

### covered_new

本轮基于 PR diff、Issue 描述、PR !178 描述与官方 v0.3.3 用户可见变更新增以下用例并更新索引，已提交并推送到 PR 分支：

- `testcase/case/tc-046-project-gantt-view.md`: Project Gantt 视图和 scheduled issue timeline。
- `testcase/case/tc-047-thread-aware-comments.md`: thread-aware 评论列表与回复归组。
- `testcase/case/tc-048-agent-tasks-panel-search.md`: Agent Tasks panel 和 issue 搜索。
- `testcase/case/tc-049-transcript-sort-direction.md`: agent transcript 排序方向切换。
- `testcase/case/tc-050-workspace-issue-prefix.md`: workspace issue prefix 编辑和新 issue identifier 生效。
- `testcase/case/tc-051-usage-dashboard-time-ranges.md`: Usage dashboard 1d/7d/30d/90d 与周维度聚合入口。
- `testcase/case/tc-052-onboarding-v2-questionnaire.md`: onboarding v2 source/role/use-case 分题式问卷。
- `testcase/case/tc-053-html-attachment-preview.md`: 统一 Attachment 渲染与 HTML 附件预览/#fragment。
- `testcase/case/tc-054-add-computer-dialog.md`: Add a computer 简化对话框。
- `testcase/case/tc-055-my-issues-squad-involvement.md`: My Issues 包含 squad involvement。
- `testcase/case/tc-056-squad-responsive-layout.md`: squad page 响应式布局。
- `testcase/case/tc-057-mobile-issue-comments-timeline.md`: 移动端 issue 详情评论/时间线。
- `testcase/browser-regression-guide.md`: 新增 tc-046 到 tc-057 索引。

提交记录：

```text
a4779911 test: add regression cases for v0.3.3 merge coverage (OPE-1005)
79f790e8 test: add regression testcases for recent Fork features (OPE-1005)
```

### not_browser_applicable

- CLI workspace subtree、`workspace switch`、`workspace current`: CLI 命令行行为，应由 CLI 测试或命令行验收覆盖，不属于浏览器回归。
- `AUTH_TOKEN_TTL`: 后端 auth cookie TTL 配置，浏览器可间接受影响，但核心应由 server/auth 测试覆盖。
- OpenClaw 完整 buffer 解析: 后端 agent/openclaw 解析逻辑，非浏览器 UI。
- AGENTS.md / OpenCode skill 发现锚定 task workdir: daemon/runtime 执行环境逻辑，非浏览器 UI。
- renderer console/crash 转发到 main stderr: desktop 开发模式诊断能力，非 web 浏览器 UI。
- install-agent-runtime docs: 文档内容，非本次 web app 主路径验收重点。
- package/lockfile/SQL generated 文件迁移: 内部实现或构建产物，主要由编译、单测和相关 UI smoke 覆盖。

### blocked / 环境依赖

- `tc-048-agent-tasks-panel-search`: 本地 fresh selfhost 环境无 runtime/agent/task history，只验证到 Agents 空态与 `GET /api/agent-task-snapshot` 200，完整 issue 搜索需要 seeded agent task。
- `tc-049-transcript-sort-direction`: 本地 fresh selfhost 环境无 agent transcript，完整排序切换需要 seeded transcript。
- `tc-055-my-issues-squad-involvement`: 已验证 tab 与 `involves_user_id` 请求，完整 squad-assigned issue 断言需要 seeded squad/agent。
- `tc-056-squad-responsive-layout`: 已验证 desktop/mobile 空态无明显溢出，完整 squad detail 断言需要 seeded squad。
- `tc-040-weixin-notification-channel`: OpenClaw/WeChat 外部绑定环境未提供，文字用例已覆盖但未在本地 selfhost 执行完整外部链路。
- `tc-043` / `tc-044` / `tc-045`: 原生移动端能力未在本轮 Docker web 环境完整执行；H5 mobile 相关以 `tc-057` 补充执行。

## 编译 / 类型检查 / 构建

- PASS `pnpm typecheck`
- PASS `pnpm --filter @multica/web build`
- PASS `cd server && go build ./...`
- PASS Docker Compose build and local provision for `http://localhost:3001`

本地 provision 说明：

- compose project `ope-1005`
- backend port `8081`
- frontend port `3001`
- postgres port `5433`
- backend 替换为 `APP_ENV=development`，日志确认固定验证码启用。
- 回归结束后已清理：无 `ope-1005` label 的容器、volume、network、image 残留。

本地环境观察：

- `GET /api/runtimes/cli-update-manifest` 出现过 502，原因是本机代理 `127.0.0.1:7890 connect refused`，未作为 PR 功能失败处理。
- WebSocket reconnect warning 在本地环境出现，未阻断已验证功能。

## 浏览器回归执行结果

| 用例 | 结果 | 说明 | 证据 |
| --- | --- | --- | --- |
| tc-001 | PASS | 固定验证码登录成功；fresh DB 进入 onboarding。 | `testcase/report/images/tc-001-issue-detail-after-login-20260520-073226.png` |
| tc-046 | PASS | Project 创建后可进入项目详情，切换到 Gantt 视图，`scheduled=true` 请求 200，空态可见。 | `tc-046-project-detail-20260520-073226.png`, `tc-046-project-gantt-empty-20260520-073226.png` |
| tc-047 | PASS | 创建顶层评论和回复；timeline 中 reply `parent_id` 指向 parent，刷新后视觉归组保留。 | `tc-047-thread-aware-comments-20260520-073226.png` |
| tc-048 | BLOCKED | Agents 空态，无 agent/task seed；只验证 `GET /api/agent-task-snapshot` 200。 | `tc-048-agents-empty-20260520-073226.png` |
| tc-050 | PASS | Settings > 通用可编辑 prefix；`TST` 保存成功，新建 issue identifier 为 `TST-2`；已恢复 `OPE`。 | `tc-050-workspace-settings-general-20260520-073226.png` |
| tc-051 | PASS | Usage 页面显示 `按天`、`按周`、`1d`、`7d`、`30d`、`90d`；点击 `1d` 后 daily/by-agent 请求均 200。 | `tc-051-usage-dashboard-20260520-073226.png`, `tc-051-usage-1d-selected-20260520-073226.png` |
| tc-052 | FAIL | onboarding source/role/use-case/workspace 创建通过；runtime 步骤“暂时跳过”点击无效。 | `tc-052-onboarding-runtime-skip-stuck-20260520-073226.png` |
| tc-053 | PASS | HTML 附件统一预览卡和 iframe 内容可见；`/api/attachments/{id}/content` 200，fragment 目标内容可见。 | `tc-053-html-attachment-inline-20260520-073226.png`, `tc-053-html-fragment-after-click-20260520-073226.png`, `tc-053-html-attachment-card-20260520-073226.png` |
| tc-054 | PASS | `/runtimes` 空态和 `添加电脑` 可见；对话框为简化 install/setup 指引。 | `tc-054-runtimes-page-20260520-073226.png`, `tc-054-add-computer-dialog-20260520-073226.png` |
| tc-055 | PARTIAL | `/my-issues` 显示 `我的智能体和小队` tab；点击后 `involves_user_id` 请求 200；完整 squad issue 断言缺 seed。 | `tc-055-my-issues-page-20260520-073226.png`, `tc-055-my-agents-squads-tab-20260520-073226.png` |
| tc-056 | PARTIAL | `/squads` desktop/mobile 空态页面可用，无明显溢出；完整 detail 断言缺 seed。 | `tc-056-squads-desktop-empty-20260520-073226.png`, `tc-056-squads-mobile-empty-20260520-073226.png` |
| tc-057 | PASS | 390x844 mobile viewport 下 issue 详情评论与 timeline 可见，包含 parent/reply 与活动记录。 | `tc-057-mobile-issue-comments-timeline-20260520-073226.png` |

执行覆盖统计：

- 计划执行的本轮重点 browser 用例：12
- PASS: 8
- FAIL: 1
- BLOCKED/PARTIAL: 3
- pass_rate: `8/(8+1)`

## 失败用例 handback

### tc-052-onboarding-v2-questionnaire

结论：FAIL，应退回开发处理。

复现步骤：

1. 在 fresh selfhost 环境打开 `http://localhost:3001`。
2. 使用 `tester@multica.com` 和固定验证码 `888888` 登录。
3. 进入 onboarding v2，依次完成 source、role、use-case 分题式问卷。
4. 创建 workspace。
5. 到 runtime connection 步骤后点击“暂时跳过”。

期望结果：

- 点击“暂时跳过”后调用 `POST /api/me/onboarding/no-runtime-bootstrap`。
- 后端创建或复用 runtime-skipped onboarding issue。
- 前端离开 runtime 步骤并进入 workspace/issue 正常体验。

实际结果：

- 按钮可见、enabled，可通过 ref、精确文本和坐标点击。
- 点击后没有 `POST /api/me/onboarding/no-runtime-bootstrap` 网络请求。
- 页面仍停留在 runtime 步骤，无跳转、无 toast、无错误提示。
- 手动在浏览器中带 CSRF 调用该 API 成功返回 200，并创建 issue，说明后端 endpoint 可用，问题更偏前端点击/事件链路。

初步定位：

- 相关源码：
  - `packages/views/onboarding/steps/step-platform-fork.tsx`: “暂时跳过”按钮执行 `onClick={() => onNext(null)}`。
  - `packages/views/onboarding/onboarding-flow.tsx`: `handleRuntimeNext(null)` 应调用 `bootstrapNoRuntimeOnboarding(workspace.id)`。
  - `packages/core/onboarding/store.ts`: `bootstrapNoRuntimeOnboarding` 封装 `POST /api/me/onboarding/no-runtime-bootstrap`。
- 真实浏览器中 `StepPlatformFork` 上的 skip click 没有进入上述 API 调用路径。建议优先排查 runtime step 实际渲染分支是否使用了同一个 handler、按钮 click 是否被父层/overlay/表单状态拦截，以及 `handleRuntimeNext` 是否在 workspace 创建后绑定到了正确的 `workspace.id`。

失败证据：

- `testcase/report/images/tc-052-onboarding-runtime-20260520-073226.png`
- `testcase/report/images/tc-052-onboarding-runtime-skip-stuck-20260520-073226.png`
- 手动 API 验证返回 200，创建 issue id `276e9966-6361-4661-88e4-027066a4f63d`。

## 附件/截图清单

截图目录：

```text
testcase/report/images/
```

关键截图：

- `tc-052-onboarding-runtime-skip-stuck-20260520-073226.png`
- `tc-046-project-gantt-empty-20260520-073226.png`
- `tc-047-thread-aware-comments-20260520-073226.png`
- `tc-050-workspace-settings-general-20260520-073226.png`
- `tc-051-usage-dashboard-20260520-073226.png`
- `tc-053-html-attachment-inline-20260520-073226.png`
- `tc-054-add-computer-dialog-20260520-073226.png`
- `tc-057-mobile-issue-comments-timeline-20260520-073226.png`

## 清理状态

- Browser session `abtp-ope-1005-20260520-073226b`: 已关闭。
- Docker containers with label `com.docker.compose.project=ope-1005`: 无残留。
- Docker volumes with label `com.docker.compose.project=ope-1005`: 无残留。
- Docker networks with label `com.docker.compose.project=ope-1005`: 无残留。
- Docker images with label `com.docker.compose.project=ope-1005`: 无残留。
