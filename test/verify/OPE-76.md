# OPE-76 浏览器验收测试用例

> Issue: 【变更】创建Agent时，禁止使用其他人的Runtime
> PR: https://gitee.com/wujie-agent/multica/pulls/9
> 测试环境: Docker Compose self-host (localhost:3000 / localhost:8080)
> 前置条件: 至少注册两个用户（User A = Owner, User B = 非 Owner），每个用户至少有一个自己的 Runtime

---

## 前置准备

1. `cp .env.example .env`，设置 `APP_ENV=development`（启用 888888 master code）
2. `docker compose -f docker-compose.selfhost.yml up -d --build`
3. 确认 http://localhost:3000 可访问
4. 注册/登录 User A（owner@test.com / 888888）
5. 创建至少 1 个 Runtime（命名为 "Runtime-A"）
6. 注册/登录 User B（member@test.com / 888888）
7. 创建至少 1 个 Runtime（命名为 "Runtime-B"）
8. 邀请 User B 加入 User A 的 workspace（或确保在同一 workspace）

---

## TC-01: 创建Agent对话框 - Runtime筛选

**对应需求**: 第1点 - 去掉Mine/All中的ALL标签

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 点击 "Create Agent" 按钮 | 弹出创建Agent对话框 |
| 2 | 查看 Runtime 选择器 | 仅显示 User A 自己的 Runtime，**无** Mine/All 切换标签 |
| 3 | 确认看不到 User B 的 Runtime | Runtime 列表中不包含 Runtime-B |
| 4 | User B 登录，重复步骤 1-3 | 仅显示 Runtime-B，不包含 Runtime-A |

**判定标准**: ✅ 两个用户各自只看到自己的 Runtime，无 Mine/All 切换

---

## TC-02: Agent编辑页 - Settings中Runtime选择器

**对应需求**: 第3点 - Settings中Runtime选择器去掉Mine/All切换

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 进入自己 Agent 的详情页 | 显示 Agent 详情 |
| 2 | 切换到 Settings Tab | Runtime 选择器仅显示 User A 自己的 Runtime |
| 3 | 确认无 Mine/All 切换 | Runtime 下拉无筛选标签 |

**判定标准**: ✅ Settings 中 Runtime 选择器只显示 owner 自己的 Runtime

---

## TC-03: Agents列表 - Mine/All筛选

**对应需求**: 第5点 - Agents列表增加Mine/All筛选

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 进入 Agents 列表页 | 页面正常显示 |
| 2 | 查看筛选区域 | 有 Mine / All 切换按钮，默认选中 Mine |
| 3 | 确认 Mine 模式下 | 仅显示 owner_id === User A 的 Agent |
| 4 | 切换到 All | 显示 workspace 内所有 Agent（包括 User B 创建的） |
| 5 | User B 登录，进入 Agents 列表 | 默认 Mine 下只看到自己的 Agent |
| 6 | User B 切换到 All | 看到所有 Agent |

**判定标准**: ✅ Mine/All 筛选正确过滤 Agent 列表

---

## TC-04: Agent右键菜单 - Duplicate功能

**对应需求**: 第5点 - 右键上下文菜单+Duplicate

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 切换到 All 视图 | 看到其他用户的 Agent |
| 2 | 右键点击 User B 的 Agent | 弹出上下文菜单，包含 "Duplicate" 选项 |
| 3 | 点击 "Duplicate" | 创建新 Agent，名称为原名称 + "Copy" 后缀 |
| 4 | 查看新 Agent 的 Owner | Owner 为 User A（当前操作者） |
| 5 | 查看新 Agent 配置 | Instructions / Skills / Custom Args / Model 等被复制 |
| 6 | 查看新 Agent 的 Environment | **Key 被复制，Value 为空** |
| 7 | 查看新 Agent 的 Runtime | 绑定到 User A 自己的第一个可用 Runtime，不是原始 Agent 的 Runtime |
| 8 | 右键点击自己（User A）的 Agent | 同样弹出菜单，Duplicate 可用 |

**判定标准**: ✅ Duplicate 复制配置但 ENV 只复制 key，Runtime 绑定到自己的

---

## TC-05: Agent详情页 - Owner权限控制

**对应需求**: 第6点 - Agent设置页面权限控制

**5a: Owner视角（User A 访问自己的 Agent）**

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 进入自己的 Agent 详情页 | 所有 Tab 可见且可编辑 |
| 2 | Settings Tab | 可以修改 Runtime 等配置 |
| 3 | Environment Tab | 可以查看和编辑 key/value |
| 4 | Custom Args Tab | 可以编辑 |
| 5 | Instructions Tab | 可以编辑 |
| 6 | Skills Tab | 可以编辑 |
| 7 | 顶部操作栏 | Archive / Restore / Delete 按钮可见 |

**5b: 非Owner视角（User B 访问 User A 的 Agent）**

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User B 切换到 All 视图，点击 User A 的 Agent | 进入 Agent 详情页 |
| 2 | Settings Tab | **可见但只读**，无法修改任何字段 |
| 3 | Custom Args Tab | **可见但只读** |
| 4 | Instructions Tab | **可见但只读** |
| 5 | Skills Tab | **可见但只读** |
| 6 | Environment Tab | **可见**，但 value 显示为 `****`（脱敏） |
| 7 | 尝试编辑任何字段 | 无法输入/修改，字段为禁用状态 |
| 8 | 顶部操作栏 | Archive / Restore / Delete 按钮**不可见** |

**判定标准**: ✅ 非 Owner 所有 Tab 可读不可写，ENV value 脱敏，操作按钮隐藏

---

## TC-06: 后端权限校验 - UpdateAgent

**对应需求**: 第2、4点 - 后端校验

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User B 通过 API 尝试修改 User A 的 Agent 的 runtime_id | 返回 403 Forbidden |
| 2 | User B 通过 API 尝试修改 User A 的 Agent 的 instructions | 返回 403 Forbidden |
| 3 | User A 修改自己的 Agent 的 runtime_id 为自己的 Runtime | 返回 200 成功 |
| 4 | User A 尝试修改自己的 Agent 的 runtime_id 为 User B 的 Runtime | 返回 403 Forbidden |

**判定标准**: ✅ 非 Owner 无法通过 API 绕过权限校验

---

## TC-07: 后端权限校验 - CreateAgent Runtime所有权

**对应需求**: 第2点 - CreateAgent接口runtime所有权校验

| 步骤 | 操作 | 预期结果 |
|------|------|----------|
| 1 | User A 创建 Agent 时绑定自己的 Runtime-A | 返回 200 成功 |
| 2 | User A 创建 Agent 时尝试绑定 User B 的 Runtime-B | 返回 403 Forbidden |

**判定标准**: ✅ 创建 Agent 时无法绑定他人的 Runtime

---

## 验收总结

| 用例 | 需求覆盖 | 优先级 |
|------|----------|--------|
| TC-01 | 第1点：创建页Runtime筛选 | P0 |
| TC-02 | 第3点：Settings Runtime筛选 | P0 |
| TC-03 | 第5点：Agents列表Mine/All | P0 |
| TC-04 | 第5点：Duplicate功能 | P0 |
| TC-05 | 第6点：详情页权限控制 | P0 |
| TC-06 | 第2、4点：后端UpdateAgent校验 | P1 |
| TC-07 | 第2点：后端CreateAgent校验 | P1 |

**全部 P0 通过即为验收通过。P1 可通过 API 调用补充验证。**
