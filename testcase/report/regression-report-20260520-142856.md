## 验收结论

FAIL

本轮基于 PR !178 最新提交 `e97303c3` 重新回归，范围按用户最新要求收敛为 Fork 独有功能保留验证，不再把官方 Multica v0.3.3 onboarding/Gantt/attachment 等纯上游特性纳入 Fork 回归结论。

## 用例覆盖缺口分析

新增/更新用例文件：无。本轮检查后未再修改 testcase。

复用既有用例文件：
- `testcase/browser-regression-guide.md`
- `testcase/case/tc-001-fixed-verification-code-login.md`
- `testcase/case/tc-011-agent-permission-controls.md`
- `testcase/case/tc-039-comment-body-collapse.md`
- `testcase/case/tc-040-weixin-notification-channel.md`
- `testcase/case/tc-041-wiki-creator-activity.md`
- `testcase/case/tc-042-issue-subscription.md`
- `testcase/case/tc-043-mobile-issue-labels-parent.md`
- `testcase/case/tc-044-mobile-inbox-batch.md`
- `testcase/case/tc-045-mobile-label-filter.md`
- `testcase/case/tc-057-mobile-issue-comments-timeline.md`

覆盖分类：
- `covered_existing`: OPE-954 runtime/agent owner 权限收紧由 `tc-011` 覆盖。
- `covered_existing`: OPE-700 长评论折叠由 `tc-039` 覆盖。
- `covered_existing`: OPE-544 WeChat/OpenClaw 通知渠道由 `tc-040` 覆盖。
- `covered_existing`: OPE-843 Wiki 创建者与 activity 由 `tc-041` 覆盖。
- `covered_existing`: OPE-995 Issue 订阅/取消订阅与订阅者排序由 `tc-042` 覆盖。
- `covered_existing`: 移动端 issue 属性/Inbox 批量/标签筛选/评论时间线由 `tc-043`、`tc-044`、`tc-045`、`tc-057` 覆盖。
- `not_browser_applicable`: 官方 v0.3.3 纯上游功能，例如 Project Gantt、onboarding v2、HTML attachment preview、usage 1d、workspace prefix、transcript sort、Add computer 简化等，按本轮用户要求不纳入 Fork 回归范围。
- `not_browser_applicable`: CLI/daemon/backend-only 变更以编译、typecheck、相关单测覆盖，不用浏览器静态 PASS 代替 UI 验证。

前置检查：
- Multica Issue 检索到当前主线 OPE-1005，以及相关近期 Fork 特性 OPE-700、OPE-544、OPE-843、OPE-995、OPE-954、OPE-946、OPE-480、OPE-481、OPE-1000 等；未发现另一个可替代本任务的重复验收 Issue。
- 官方 GitHub Issue 检索 `v0.3.3 regression onboarding runtime skip no-runtime-bootstrap fork` 无匹配。
- 官方源码 worktree `~/Desktop/harness/multica-official-upstream` 已 `git fetch github` 并 checkout 最新 `github/main`。

## 构建与静态验证

- PASS `pnpm typecheck`: 7/7 packages passed。
- PASS `pnpm --filter @multica/web build`。
- PASS `cd server && go build ./...`。
- PASS `pnpm --filter @multica/views exec vitest run onboarding issues/components/comment-card.test.tsx settings/components/workspace-tab.test.tsx`: 10 files / 50 tests passed。

## 浏览器回归环境

- Repo: `/Users/jiangjiangdear/Desktop/harness/ope-1005-upgrade-v0.3.3`
- Branch: `feat/ope-1005-upgrade-v0.3.3`
- Commit: `e97303c3`
- Frontend URL: `http://localhost:3001`
- Docker project: `ope-1005-rerun`
- Ports: frontend `3001`, backend `8081`, postgres `5433`
- Auth: `testcase/auth/auth.json`, `tester@multica.com` + fixed code `888888`
- Browser session: `abtp-ope-1005-rerun-20260520-1405`

Provision note: 初次按 auto-provision 模板把 backend 改成独立 `backend-dev` 容器后，frontend 内置代理无法解析 `backend` DNS，`/api/config` 和 `/auth/send-code` 返回 500。随后恢复为 compose service `backend` 并通过环境变量注入 `APP_ENV=development`，登录链路恢复正常。

## Scenario Results

- TC-001 fixed verification login | passed | 登录页提交 `tester@multica.com`，固定验证码 `888888` 后进入 onboarding。
- Environment bootstrap | passed | 创建 `OPE 1005 Rerun` workspace，并通过 no-runtime bootstrap 进入 `OPE-1`。该步骤仅用于准备 fresh selfhost 环境，不纳入官方 onboarding 用例验收。
- TC-042 issue subscription | passed | 在新建普通 issue `OPE-2` 上点击 `取消订阅` 后发送 `POST /api/issues/.../unsubscribe` 200，按钮变为 `订阅`；再次点击发送 `POST /api/issues/.../subscribe` 200，按钮变回 `取消订阅`。
- TC-039 comment body collapse | failed | 种子长评论后页面出现 `展开` 控件；`agent-browser click` 和坐标 mouse click 均未触发按钮状态变化；在页面上下文执行同一按钮 `button.click()` 可切到 `收起`，说明组件状态逻辑可变，但真实指针点击路径不满足用例。
- TC-041 wiki creator activity | passed | Wiki 新建页面显示 creator 链接/头像与 `Activity` 区域；编辑内容后新增 activity 记录可见。
- TC-057 mobile issue comments timeline | passed | 390x844 移动视口打开 `OPE-2`，issue 详情显示评论和动态内容。
- TC-045 mobile label filter | passed | 种子标签 `fork-regression-label` 并挂到 `OPE-2`；移动视口 issue 列表可打开筛选菜单，`标签` 子菜单中选中 `fork-regression-label` 后列表只显示 OPE-2。
- TC-043 mobile issue labels/start-date/parent | mixed | 移动详情更多菜单可见 `开始日期` 与 `设置父 issue...` 入口；标签在列表和 issue 数据中可见，但本轮未在移动详情菜单中找到标签编辑入口，未完成“标签编辑”子路径。
- TC-044 mobile inbox batch | blocked | 移动视口 Inbox 菜单显示 `全部标为已读`、`归档全部`、`归档全部已读`、`归档已完成`；fresh 环境无通知数据，未验证批量状态变化。
- TC-040 WeChat notification channel | blocked | Settings -> Notifications 下 `微信（OpenClaw）` 渠道开关可见但 disabled；fresh 环境无 OpenClaw runtime 和 WeChat ID，无法验证绑定/投递。
- TC-011 agent/runtime permission controls | blocked | fresh selfhost 环境无 runtime、agent、多用户/admin fixture，无法执行 owner/admin/non-owner 权限浏览器链路。

Coverage: 7 executed / 11 planned scenarios reached pass or fail conclusion. Blocked scenarios are outside coverage numerator.

Pass rate: 6 / 7 executed non-blocked scenarios passed.

Failed cases:
- TC-039 comment body collapse.

Blocked cases:
- TC-011 agent/runtime permission controls.
- TC-040 WeChat notification binding/delivery.
- TC-043 mobile label editing sub-path.
- TC-044 mobile inbox batch state changes.

## Evidence

- `testcase/report/images/tc-039-comment-collapse-before-expand-20260520-1419.png`
- `testcase/report/images/tc-039-comment-collapse-after-js-expand-pointer-still-no-collapse-20260520-1420.png`
- `testcase/report/images/tc-040-wechat-openclaw-disabled-20260520-1428.png`
- `testcase/report/ope-1005-rerun-backend-20260520-1428.log`
- `testcase/report/ope-1005-rerun-frontend-20260520-1428.log`

## Development Handback

### TC-039 长评论折叠按钮真实点击不生效

复现步骤：
1. 在 PR 分支 `feat/ope-1005-upgrade-v0.3.3` 最新提交 `e97303c3` 启动 selfhost 环境。
2. 使用 `tester@multica.com` + `888888` 登录并进入 workspace。
3. 打开普通 issue `OPE-2`。
4. 创建或准备一条超过折叠阈值的长评论。
5. 页面出现 `展开` 按钮后，用真实指针点击 `展开`。

期望结果：长评论展开，按钮变为 `收起`；再次点击 `收起` 后恢复折叠。

实际结果：`agent-browser click @ref` 和坐标 mouse click 均未触发按钮状态变化，按钮仍显示 `展开` 或 `收起` 不变。

补充观察：在页面上下文执行同一个按钮的 DOM `button.click()` 可以把 `展开` 切到 `收起`，说明 React 状态逻辑存在，但真实 pointer/click 路径没有进入同一处理链路。按钮位于 `pointer-events-none` 的 overlay 容器内，按钮自身带 `pointer-events-auto`。

初步定位：建议检查长评论折叠 overlay 的 pointer-events、绝对定位层级、底部 gradient 遮罩、以及按钮事件绑定/命中区域。当前行为对浏览器自动化真实指针不满足 `tc-039` 的用户点击要求。

## Cleanup

Docker cleanup is required after report generation.
