# GitHub 设置页支持多个安装 Implementation Plan（实现计划）

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 当前任务按用户要求在本会话内联执行。

**目标：** 让一个 Multica 工作区可以在设置页添加、查看并分别断开多个 GitHub App installation。

**架构：** 保持 React Query 返回的 `installations` 数组为唯一服务端状态来源，不增加 store 或后端接口。`GitHubTab` 直接遍历数组生成独立行，连接入口与数组是否为空解耦，断开确认框保存并提交被选中行的数据库 ID。

**技术栈：** React、TypeScript、TanStack Query、Vitest、Testing Library、项目现有 JSON i18n。

## 全局约束

- 只改共享前端 `packages/views`，不修改后端、数据库或 GitHub 回调。
- 普通成员只能查看 installation；只有后端返回 `can_manage: true` 时才能连接或断开。
- GitHub 总开关关闭时仍允许断开 installation。
- 已有 installation 时连接按钮文案为“连接另一个 GitHub”；没有时仍为“连接 GitHub”。
- 每一条断开请求必须使用该行的 `installation.id`，不得使用数组下标或 GitHub numeric installation ID。
- 英文、简体中文、日文和韩文文案必须同步。

---

### 任务 1：用组件测试固定多 installation 行为

**文件：**

- 修改：`packages/views/settings/components/github-tab.test.tsx`
- 测试：`packages/views/settings/components/github-tab.test.tsx`

**接口：**

- 输入：`installationsRef.current.installations` 中的多个 `{ id, account_login, account_type, installation_id, connected_by? }`。
- 输出：独立账号行、“Connect another GitHub”按钮、`Disconnect <login>` 无障碍名称，以及精确的 `deleteGitHubInstallation(workspaceId, rowId)` 调用。

- [ ] **步骤 1：补充测试 fixture 的账号类型**

```ts
installations: [] as {
  id: string;
  account_login: string;
  account_type: "User" | "Organization";
  installation_id?: number;
  connected_by?: string;
}[],
```

所有现有 installation fixture 都显式补上 `account_type`。

- [ ] **步骤 2：新增多行展示和持续连接入口测试**

```ts
it("renders every installation separately and keeps Connect another available", () => {
  installationsRef.current = {
    configured: true,
    can_manage: true,
    installations: [
      {
        id: "inst-user",
        account_login: "octocat",
        account_type: "User",
        installation_id: 41,
      },
      {
        id: "inst-org",
        account_login: "acme-org",
        account_type: "Organization",
        installation_id: 42,
        connected_by: "Jiayuan",
      },
    ],
  };

  render(<GitHubTab />, { wrapper: I18nWrapper });

  expect(screen.getByText("octocat")).toBeTruthy();
  expect(screen.getByText("acme-org")).toBeTruthy();
  expect(screen.getByText("Personal account")).toBeTruthy();
  expect(screen.getByText("Organization")).toBeTruthy();
  expect(screen.queryByText(/octocat, acme-org/)).toBeNull();
  expect(screen.getByRole("button", { name: "Connect another GitHub" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Disconnect octocat" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Disconnect acme-org" })).toBeTruthy();
});
```

- [ ] **步骤 3：把断开测试改成验证第二行 ID 和账号确认文案**

```ts
it("disconnects the selected installation row", async () => {
  const user = userEvent.setup();
  installationsRef.current = {
    configured: true,
    can_manage: true,
    installations: [
      {
        id: "inst-first",
        account_login: "octocat",
        account_type: "User",
        installation_id: 41,
      },
      {
        id: "inst-second",
        account_login: "acme-org",
        account_type: "Organization",
        installation_id: 42,
      },
    ],
  };
  mockDeleteInstallation.mockResolvedValue(undefined);

  render(<GitHubTab />, { wrapper: I18nWrapper });

  await user.click(screen.getByRole("button", { name: "Disconnect acme-org" }));
  expect(screen.getByRole("heading", { name: "Disconnect acme-org?" })).toBeTruthy();
  expect(mockDeleteInstallation).not.toHaveBeenCalled();

  await user.click(screen.getByRole("button", { name: /^Disconnect$/ }));

  await waitFor(() => {
    expect(mockDeleteInstallation).toHaveBeenCalledWith("workspace-1", "inst-second");
  });
});
```

同步更新现有单条 installation、总开关关闭和只读测试，使其使用新的账号类型与 `Disconnect <login>` 无障碍名称。

- [ ] **步骤 4：运行测试并确认 RED**

运行：

```bash
pnpm --filter @multica/views test -- settings/components/github-tab.test.tsx
```

预期：测试因为找不到 “Connect another GitHub”、独立账号类型或 `Disconnect acme-org` 而失败；失败原因必须是功能尚未实现，而不是 fixture、导入或语法错误。

---

### 任务 2：实现逐行展示、持续连接入口和精确断开

**文件：**

- 修改：`packages/views/settings/components/github-tab.tsx`
- 修改：`packages/views/locales/en/settings.json`
- 修改：`packages/views/locales/zh-Hans/settings.json`
- 修改：`packages/views/locales/ja/settings.json`
- 修改：`packages/views/locales/ko/settings.json`
- 测试：`packages/views/settings/components/github-tab.test.tsx`

**接口：**

- 消费：现有 `GitHubInstallation` 的 `id`、`account_login`、`account_type` 和可选 `connected_by`。
- 产出：每行独立渲染；`disconnectTarget: string | null` 仍保存行 ID；`disconnectInstallation` 由当前数组按 ID 派生。

- [ ] **步骤 1：移除 primary installation 假设并派生选中行**

```ts
const connected = installations.length > 0;
const disconnectInstallation =
  installations.find((installation) => installation.id === disconnectTarget) ?? null;
```

删除 `primaryInstallation`，不引入默认 installation 或重新排序。

- [ ] **步骤 2：让连接按钮始终对管理员可见**

```tsx
{canManage && (
  <Button
    size="sm"
    onClick={handleConnect}
    disabled={connecting || !configured}
    title={!configured ? t(($) => $.github.connect_disabled_tooltip) : undefined}
  >
    {connecting
      ? t(($) => $.github.connect_opening)
      : connected
        ? t(($) => $.github.connect_another_github)
        : t(($) => $.github.connect_github)}
  </Button>
)}
```

- [ ] **步骤 3：把 installation 数组渲染为独立行**

```tsx
{connected && (
  <div className="divide-y divide-surface-border rounded-md border">
    {installations.map((installation) => (
      <div
        key={installation.id}
        className="flex items-center justify-between gap-4 px-3 py-3"
      >
        <div className="min-w-0 space-y-0.5">
          <p className="truncate text-sm font-medium">{installation.account_login}</p>
          <p className="text-xs text-muted-foreground">
            {installation.account_type === "Organization"
              ? t(($) => $.github.account_type_organization)
              : t(($) => $.github.account_type_user)}
          </p>
          {installation.connected_by && (
            <p className="text-xs text-muted-foreground">
              {t(($) => $.github.connected_by, { name: installation.connected_by })}
            </p>
          )}
        </div>
        {canManage && (
          <Button
            variant="outline"
            size="sm"
            aria-label={t(($) => $.github.disconnect_account, {
              login: installation.account_login,
            })}
            disabled={disconnecting}
            onClick={() => setDisconnectTarget(installation.id)}
          >
            {t(($) => $.github.disconnect)}
          </Button>
        )}
      </div>
    ))}
  </div>
)}
```

连接说明位于列表上方；已连接时使用新的 `connected_installations` 文案，未连接时保留现有 PR 自动关联说明或联系管理员提示。

- [ ] **步骤 4：让确认框明确显示选中账号**

```tsx
<AlertDialog open={!!disconnectInstallation}>
  <AlertDialogTitle>
    {t(($) => $.github.disconnect_confirm_title, {
      login: disconnectInstallation?.account_login ?? "",
    })}
  </AlertDialogTitle>
  <AlertDialogDescription>
    {t(($) => $.github.disconnect_confirm_description, {
      login: disconnectInstallation?.account_login ?? "",
    })}
  </AlertDialogDescription>
</AlertDialog>
```

保留现有关闭控制、失败重试、成功后 query invalidation 和 toast 行为。

- [ ] **步骤 5：同步四种语言文案**

英文键值：

```json
"connected_installations": "Connected GitHub installations for this workspace.",
"account_type_user": "Personal account",
"account_type_organization": "Organization",
"connect_another_github": "Connect another GitHub",
"disconnect_account": "Disconnect {{login}}",
"disconnect_confirm_title": "Disconnect {{login}}?",
"disconnect_confirm_description": "Multica will stop receiving webhooks for the {{login}} installation. Existing PR mirrors and links are kept; reconnect any time to resume updates."
```

简体中文键值：

```json
"connected_installations": "当前工作区已连接以下 GitHub App 安装。",
"account_type_user": "个人账号",
"account_type_organization": "组织",
"connect_another_github": "连接另一个 GitHub",
"disconnect_account": "断开 {{login}}",
"disconnect_confirm_title": "断开 {{login}}？",
"disconnect_confirm_description": "Multica 将不再接收 {{login}} 这条安装的 webhook。已有的 PR 镜像与关联会保留，再次连接即可恢复同步。"
```

日文键值：

```json
"connected_installations": "このワークスペースには次の GitHub App インストールが接続されています。",
"account_type_user": "個人アカウント",
"account_type_organization": "組織",
"connect_another_github": "別の GitHub を接続",
"disconnect_account": "{{login}} の接続を解除",
"disconnect_confirm_title": "{{login}} の接続を解除しますか？",
"disconnect_confirm_description": "Multica は {{login}} インストールの Webhook 受信を停止します。既存の PR ミラーとリンクは保持され、再接続すると更新を再開できます。"
```

韩文键值：

```json
"connected_installations": "이 워크스페이스에 연결된 GitHub App 설치입니다.",
"account_type_user": "개인 계정",
"account_type_organization": "조직",
"connect_another_github": "다른 GitHub 연결",
"disconnect_account": "{{login}} 연결 해제",
"disconnect_confirm_title": "{{login}} 연결을 해제할까요?",
"disconnect_confirm_description": "Multica가 {{login}} 설치의 Webhook 수신을 중단합니다. 기존 PR 미러와 연결은 유지되며, 다시 연결하면 업데이트를 재개할 수 있습니다."
```

- [ ] **步骤 6：运行聚焦测试并确认 GREEN**

运行：

```bash
pnpm --filter @multica/views test -- settings/components/github-tab.test.tsx
```

预期：该文件全部测试通过，0 个失败。

- [ ] **步骤 7：提交功能改动**

仅暂存上述组件、测试和四个 locale 文件，通过 `git-guard pre-commit` 后提交：

```bash
git commit -m "fix(settings): support multiple GitHub installations"
```

---

### 任务 3：自动化验证与浏览器回归

**文件：**

- 验证：`packages/views/settings/components/github-tab.test.tsx`
- 验证：`packages/views/settings/components/github-tab.tsx`
- 验证：四个 `packages/views/locales/*/settings.json`

**接口：**

- 自动化输入：`@multica/views` 的单测与 TypeScript 编译。
- 浏览器输入：本地 `github-5599` 工作区中已存在的两条模拟 installation。
- 输出：两个独立账号行、持续可见的连接入口、两个精确断开入口；测试期间不确认断开，保留两条本地数据。

- [ ] **步骤 1：运行完整 views 单测**

```bash
pnpm --filter @multica/views test
```

预期：全部测试通过，0 个失败。

- [ ] **步骤 2：运行 views 类型检查**

```bash
pnpm --filter @multica/views typecheck
```

预期：退出码 0，无 TypeScript 错误。

- [ ] **步骤 3：检查 JSON 与 diff**

```bash
node -e 'for (const f of process.argv.slice(1)) JSON.parse(require("node:fs").readFileSync(f, "utf8"))' packages/views/locales/en/settings.json packages/views/locales/zh-Hans/settings.json packages/views/locales/ja/settings.json packages/views/locales/ko/settings.json
git diff --check
```

预期：两个命令退出码均为 0。

- [ ] **步骤 4：使用 @电脑 做本地浏览器回归**

在 `http://localhost:3000/github-5599/settings?tab=github` 验证：

1. `debug-user-installation` 和 `debug-org-installation` 各占一行；
2. 两行分别显示“个人账号”和“组织”；
3. 页面存在“连接另一个 GitHub”；
4. 存在“断开 debug-user-installation”和“断开 debug-org-installation”两个无障碍操作；
5. 打开第二行确认框后，标题包含 `debug-org-installation`；
6. 取消确认框，不删除本地 installation。

- [ ] **步骤 5：提交验证后产生的必要修正**

如果浏览器验证发现问题，先新增或调整失败测试，再按 RED-GREEN 修正并重新执行任务 3 的全部验证。若没有代码修正，则不创建空提交。

---

### 任务 4：推送并创建 Pull Request

**文件：**

- 审查：本分支相对 `origin/main` 的全部提交和 diff。

**接口：**

- 输入：已通过任务 3 全部验证的本地分支。
- 输出：远端分支和一个指向 `multica-ai/multica` 的 Pull Request。

- [ ] **步骤 1：完成提交前审计**

```bash
git status --short
git diff origin/main...HEAD --stat
git log --oneline origin/main..HEAD
```

预期：只包含 #5599 的规格、组件、测试和四个 locale 文件；`.git-safe-ops.json` 与其他既有未跟踪文件不进入提交。

- [ ] **步骤 2：运行推送安全检查并推送**

```bash
git-guard pre-push --branch fix/5599-github-multi-installations
git push -u origin fix/5599-github-multi-installations
```

预期：安全检查通过，远端分支创建成功。

- [ ] **步骤 3：创建 PR**

```bash
gh pr create \
  --base main \
  --head fix/5599-github-multi-installations \
  --title "fix(settings): support multiple GitHub installations" \
  --body '## Summary
- render every GitHub App installation as an independently managed row
- keep the connect flow available so admins can add another installation
- disconnect the selected row and identify its account in the confirmation dialog

## Verification
- `pnpm --filter @multica/views test`
- `pnpm --filter @multica/views typecheck`
- local Chrome regression with two seeded installations

Closes #5599'
```

PR 正文必须包含：问题根因、前端修复范围、权限行为、单测/类型检查/浏览器验证结果，并使用 `Closes #5599` 自动关联 issue。
