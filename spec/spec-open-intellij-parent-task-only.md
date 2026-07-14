# 父 Issue 顶部 Open IntelliJ 仅打开本 Issue Workdir Spec

## 背景

Issue 详情页顶部属性栏提供 “Open working directory in IntelliJ IDEA” 按钮。当前实现会在父 issue 页面加载子 issue 的 task history，并在点击按钮时优先选择子 issue 的最新 task：

```ts
const taskId = pickOpenIdeTaskId(childTasks) ?? pickOpenIdeTaskId(tasks);
```

这会导致用户在父 issue 页面点击按钮时，IDEA 打开的却是子 issue 的 workdir。例如：

- 父 issue：`JTC-214 / 670a60e4-7bc7-4697-a490-e2d253b9a2f2`
- 子 issue：`JTC-224 / 0d71dd3b-e91d-4fe3-a1d9-c3574129bf5d`
- 父 issue 自身 workdir：`...\d3a721a8...\670a60e4\workdir\sqs-harness-module-workorder`
- 当前实际打开路径：`...\d3a721a8...\0d71dd3b\workdir\sqs-harness-module-workorder`

这个行为容易被理解为 workdir 中途变化，实际是父 issue 顶部按钮自动选中了子 issue task。

## 目标

在父 issue 顶部属性栏点击 Open IntelliJ 时，只打开当前 issue 自己的最新可打开 task workdir，不再自动选择任何子 issue task。

## 非目标

- 不改变子 issue 自己详情页的 Open IntelliJ 行为。用户进入子 issue 页面点击按钮时，仍打开子 issue 自己的 workdir。
- 不改变 `github_repo` 模式下追加仓库目录名的行为。IDEA 仍应打开 `work_dir/<repoName>`，而不是空的 task workdir 根目录。
- 不改变 daemon command、IDEA 启动、runtime owner 权限校验、daemon offline 校验。
- 不移除后端对历史 `task_id` 请求的兼容性，除非实现阶段确认需要安全收紧。

## 当前行为

### 前端

位置：`packages/views/issues/components/issue-detail.tsx`

`OpenIssueInIntelliJButton` 接收：

- `issueId`
- `childIssueIds`
- `childIssueIdsLoading`

它会：

1. 查询当前 issue 的 tasks。
2. 查询每个 child issue 的 tasks。
3. 合并 child tasks。
4. 优先选择 child task，否则选择当前 issue task。
5. 调用 `api.openIssueInIde(issueId, "intellij_idea", { taskId })`。

### 后端

位置：`server/internal/handler/open_ide.go`

当请求携带 `task_id` 时，后端允许两类 task：

1. `task.issue_id == route issue id`
2. `task.issue.parent_issue_id == route issue id`

因此父 issue 页面传入子 issue task id 时，后端会接受并创建 `open_intellij` command，payload 的 `issue_id/task_id/work_dir` 都会指向子 issue task。

## 期望行为

### 父 issue 页面

给定父 issue `P`，其存在一个子 issue `C`，并且二者都有可打开 task：

- 点击 `P` 顶部属性栏 Open IntelliJ：
  - 必须选择 `P` 自己的最新可打开 task。
  - 必须调用 `api.openIssueInIde(P.id, "intellij_idea", { taskId: parentTaskId })`。
  - 不应查询或依赖 `C` 的 task history。
  - 不应传入 `C` 的 task id。

如果 `P` 没有可打开 task，但 `C` 有可打开 task：

- 点击 `P` 顶部属性栏 Open IntelliJ：
  - 不应回退到 `C`。
  - 应由后端返回 `409 no_eligible_task`，前端显示 “当前 issue 没有可打开的工作目录”。

### 子 issue 页面

给定子 issue `C`：

- 点击 `C` 顶部属性栏 Open IntelliJ：
  - 只选择 `C` 自己的最新可打开 task。
  - 行为与普通 issue 一致。

## 方案

### 前端变更

文件：`packages/views/issues/components/issue-detail.tsx`

1. 从 `OpenIssueInIntelliJButton` props 中移除 `childIssueIds` 和 `childIssueIdsLoading`。
2. 删除 `useQueries` 查询 child issue tasks 的逻辑。
3. 将 task 选择逻辑改为只看当前 issue tasks：

```ts
const taskId = pickOpenIdeTaskId(tasks);
```

4. `handleOpen` 的 loading guard 只依赖当前 issue tasks：

```ts
if (pending || tasksLoading) return;
```

5. 按钮禁用状态只依赖当前 issue tasks：

```tsx
disabled={pending || tasksLoading}
```

6. 在 `IssueDetail` 中调用按钮时，不再传入 `childIssueIds` 和 `childIssueIdsLoading`。

### 后端兼容与收紧建议

最小实现可以只改前端。这样父 issue 顶部按钮不再传子 issue task id，问题即可消失。

建议同时评估后端是否收紧：

文件：`server/internal/handler/open_ide.go`

当前：

```go
func openIdeTaskBelongsToRouteIssue(task openIdeTask, issue db.Issue) bool {
	if uuidToString(task.IssueID) == uuidToString(issue.ID) {
		return true
	}
	return task.ParentIssueID.Valid && uuidToString(task.ParentIssueID) == uuidToString(issue.ID)
}
```

建议目标：

```go
func openIdeTaskBelongsToRouteIssue(task openIdeTask, issue db.Issue) bool {
	return uuidToString(task.IssueID) == uuidToString(issue.ID)
}
```

取舍：

- 只改前端：风险低，兼容旧客户端或手写 API 传子 task id 的行为。
- 前后端都改：语义更严格，彻底保证 `/api/issues/{parent}/ide/open` 不能打开 child task，但可能改变已有 API 兼容行为。

本 spec 推荐先做前端最小修复；后端收紧作为单独决策点，审阅后再决定是否纳入同一 PR。

## 测试计划

### 前端单测

文件：`packages/views/issues/components/issue-detail.test.tsx`

1. 更新现有测试：`prefers a child issue task when opening IntelliJ IDEA from a parent issue`
   - 改名为：`does not use child issue tasks when opening IntelliJ IDEA from a parent issue`
   - 设置父 issue 和子 issue 都有可打开 task。
   - 期望调用：

```ts
expect(mockApiObj.openIssueInIde).toHaveBeenCalledWith(
  "issue-1",
  "intellij_idea",
  { taskId: "parent-task" },
);
```

2. 新增测试：父 issue 无 task，子 issue 有 task 时，不应传 child task。
   - 父 issue `listTasksByIssue("issue-1")` 返回 `[]`。
   - 子 issue即使存在，也不应影响按钮选择。
   - 点击后期望调用：

```ts
expect(mockApiObj.openIssueInIde).toHaveBeenCalledWith(
  "issue-1",
  "intellij_idea",
  { taskId: undefined },
);
```

如果 API client 当前会省略 undefined taskId，也可断言第三参是 `{ taskId: undefined }` 或调整为不传 taskId，按现有 client 形态决定。

3. 保留现有测试：当前 issue 多个 task 时选择最新当前 issue task。

### 后端测试（仅当决定收紧后端）

文件：`server/internal/handler/open_ide_test.go`

新增或修改测试：

- 父 issue route 传入子 issue task id 时返回 `409 no_eligible_task` 或 `404` 语义。
- 子 issue route 传入自身 task id 仍成功。

如果本次只做前端最小修复，则不需要改后端测试。

## 验收标准

- 在父 issue `JTC-214 / 670a60e4...` 页面点击顶部 Open IntelliJ，不会再打开 `...\0d71dd3b\workdir\...`。
- 父 issue 顶部按钮只会打开 `...\670a60e4\workdir\sqs-harness-module-workorder`，前提是父 issue 自己有可打开 task。
- 如果父 issue 自己没有可打开 task，即使子 issue 有 task，也显示 no workdir 错误，不自动跳到子 issue。
- 子 issue `JTC-224 / 0d71dd3b...` 页面点击按钮时，仍可打开 `...\0d71dd3b\workdir\sqs-harness-module-workorder`。
- 前端测试覆盖父/子 task 选择边界。

## 风险

- 某些用户可能已经习惯在父 issue 页面直接打开子 issue 的执行目录。该行为虽然方便，但语义不清，且已经造成 workdir “变化”的误解。
- 如果只改前端，旧客户端或手写 API 仍可以通过父 issue route 传 child task id 打开子 issue workdir。若需要强一致，应同时收紧后端。
- 如果后端同步收紧，可能影响旧版本桌面端或自动化脚本；需要确认是否有依赖该兼容行为的客户端。

## 推荐决策

推荐本轮先执行前端最小修复：

1. 父 issue 顶部按钮不查询 child issue tasks。
2. 父 issue 顶部按钮只传当前 issue 最新 task id。
3. 修改前端测试，删除“优先 child task”的预期。

后端 `openIdeTaskBelongsToRouteIssue` 是否收紧，单独评审决定。
