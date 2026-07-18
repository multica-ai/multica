# GitHub 设置页支持多个安装：设计规格

## 问题

GitHub 设置接口返回 `installations` 数组，数据库也允许同一个 Multica 工作区绑定多个 GitHub App installation。当前设置页却把数组折叠成了一个“已连接”状态：首次连接后隐藏连接入口，把所有账号名称拼成一句话，并让唯一的断开按钮固定操作 `installations[0]`。

结果是：用户无法通过正常界面添加第二个 installation，也无法断开除第一条以外的 installation。

## 范围

本次只修正共享设置页前端，不修改 GitHub App 回调、后端接口、数据库结构、installation 排序、工作区权限或 GitHub 功能开关。

改动范围：

- `packages/views/settings/components/github-tab.tsx`
- `packages/views/settings/components/github-tab.test.tsx`
- 英文、简体中文、日文和韩文设置文案中的 GitHub 部分

## 用户体验

连接区域保留现有说明。存在 installation 时，不再把账号拼成一句话，而是把每条 installation 渲染为独立行。

每行展示：

- GitHub 账号名称；
- 账号类型：个人或组织；
- 后端提供 `connected_by` 时，展示连接人；
- 对工作区所有者和管理员展示该行专属的“断开”操作。

部署已配置 GitHub 集成时，工作区所有者和管理员始终能看到连接操作。没有 installation 时显示“连接 GitHub”；已有一条或多条时显示“连接另一个 GitHub”。该操作继续调用现有签名连接 URL 接口，并在新标签页打开 GitHub App 安装流程。

没有管理权限的成员可以看到相同的 installation 列表，但看不到连接或断开操作，并继续看到现有只读提示。没有 installation 时，仍提示联系管理员连接。

即使工作区的 GitHub 功能总开关已关闭，断开操作仍然可用，因为“隐藏 GitHub 功能”和“解除 installation 绑定”是两个独立意图。

## 状态与数据流

React Query 返回值继续作为唯一服务端状态来源。组件遍历 `installationData.installations` 的全部条目，不再选取所谓的 primary installation。

断开确认框保存用户选中的 installation 行 ID，并从当前数组取得对应行，以便在确认文案中写明将断开的 GitHub 账号。确认后调用现有 `deleteGitHubInstallation(workspaceId, installationRowId)` 接口，使 GitHub 查询失效并重新获取；成功后关闭弹窗，沿用现有成功提示。

连接操作保留现有的加载态、部署配置检查、URL 检查、新标签页打开和错误处理，只是不再与“已连接”状态互斥。

## 错误与边界情况

- installation 数组为空时，展示现有未连接说明。
- 缺少可选的 `connected_by` 时，只省略连接人信息。
- 部署未配置 GitHub 集成时，即使已有 installation，也禁用连接操作并保留现有配置提示。
- 断开失败时保留确认框和选中行，允许用户重试或取消。
- 连接请求进行中时禁用连接按钮；断开请求进行中时禁用所有行的断开按钮，避免重复请求。

## 测试

组件测试通过真实渲染结果和现有 API mock 证明以下行为：

1. 多个 installation 分别渲染为独立行，不再拼成逗号分隔的一句话。
2. 所有者和管理员在已有 installation 时仍能看到“连接另一个 GitHub”。
3. 每行都有自己的断开操作；确认第二行时，接口收到第二行 ID。
4. 确认框明确显示当前选中的 GitHub 账号。
5. 普通成员能看到全部行，但看不到任何管理操作。
6. 单条 installation 仍能正常展示，并且 GitHub 总开关关闭时仍可断开。
7. 现有的空状态、部署未配置、连接人、设置开关和代码仓库跳转测试继续通过。

完成前必须通过该组件的 Vitest 测试和 `packages/views` TypeScript 检查。浏览器验证使用本地工作区中两条模拟 installation，确认两行各自拥有断开操作，同时连接入口仍然存在。

## 不在本次范围

- 选择默认或主要 installation
- 跨 installation 去重 GitHub 账号
- 在 Multica 中修改 GitHub App 的仓库访问范围
- 在 GitHub 侧撤销 GitHub App 授权
- 增加 installation 分页、排序或搜索
- 改变一个 GitHub installation 绑定多个 Multica 工作区的机制
