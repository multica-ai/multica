# Multica Browser Regression Guide

This is the project-level source of truth for Multica browser regression. Tester agents should keep generic QA ability in their own instructions, but read and follow this file for Multica-specific workflow, environment, fixtures, and reporting rules.

## 1. Regression inputs

- Standalone cases live under `testcase/case/tc-*.md`.
- Authentication state, when available, lives under `testcase/auth/`.
- Reports and screenshots should be written under `testcase/report/` and `testcase/report/images/`.
- Selection files, when generated, should live under `testcase/selection/selection-YYYYMMDD-HHMMSS.json`.

For PR / upstream-merge / release acceptance, inspect all of the following before running browser cases:

1. Multica Issue description and comments.
2. PR title, body, branch, and diff.
3. Recent Fork-specific features included in the release or merge window.
4. Existing standalone testcase files under `testcase/case/`.

## 2. Current-change selection rules

Default to impacted regression, not full-suite regression.

Selection priority:

1. Explicit testcase files requested by the user.
2. Latest `testcase/selection/selection-*.json`, if present.
3. Cases whose YAML/frontmatter or selection metadata marks them as `new`, `updated`, or `impacted`.
4. If no metadata exists, choose affected cases from PR/Issue/diff analysis and mark `selection_mode: impacted_from_diff` in the report.
5. Run full suite only when explicitly requested or when impacted selection cannot be determined.

For every user-visible or Fork-specific change, classify coverage as:

- `covered_existing`: covered by existing testcase files.
- `covered_new`: covered by testcase files added/updated in this task.
- `impacted`: existing case affected by this change and selected for execution.
- `not_browser_applicable`: backend/infra/internal-only, with reason.
- `blocked`: cannot assess, with missing input.

Do not declare PASS if coverage is missing or unassessed.

## 3. Correct environment and evidence

UI/browser acceptance must validate the correct build:

- Use the current PR branch, current task worktree, acceptance worktree, or target release branch.
- Do not use an unrelated `main` dev server as evidence for PR behavior.
- The report must include tested URL, worktree path, branch name, commit SHA and/or PR number.
- Screenshots, videos, and report files used as evidence must be uploaded to the Multica Issue when the task runs on Multica. A local path alone is not enough.
- If the correct worktree/branch cannot be started or opened, mark the UI check as BLOCKED/PARTIAL and explain why. Do not replace browser validation with diff review or unit tests.

## 4. Local self-host environment

When no frontend target URL is provided, provision or use a local Multica self-host instance for browser regression.

Expected local profile conventions:

```bash
multica --profile local daemon status --output json
multica --profile local daemon restart
```

Safe cleanup policy:

- Preserve Docker volumes, database data, images, and local profile state by default.
- Do not reset/truncate local data unless the user explicitly asks.
- Remove only temporary dev containers and local report artifacts that were already uploaded, if cleanup is required.

## 5. Agent runtime self-healing

Some Multica cases require real agent task runs. If a testcase is blocked only because no Agent Runtime is connected or no task run data exists, do not mark BLOCKED immediately.

For local/self-host regression, first attempt self-healing:

```bash
multica --profile local daemon status --output json
# If daemon is not running, unhealthy, or no local runtimes are registered:
multica --profile local daemon restart
multica --profile local daemon status --output json
```

Proceed only if the daemon reports `status: running` and the local workspace has registered runtimes/agents.

For cloud/shared workspaces:

- Prefer Guodage-owned test agents/runtimes or agents explicitly assigned for the task.
- Do not mutate agents/runtimes owned by unclear or unrelated users.
- If ownership is unclear and no safe test agent exists, report the blocker.

## 6. Fixture recipe: creating agent task runs

Cases such as TC-018, TC-019, TC-028, TC-031, TC-036, notification delivery cases, and runtime-dependent cases need real task runs.

If no suitable issue with task runs exists, create data before declaring BLOCKED:

1. Create or select a disposable test issue in the test workspace.
2. Ensure at least one suitable test agent exists and has an online runtime.
3. Trigger the agent with a markdown mention comment, not plain text:

   ```markdown
   [@agent_name](mention://agent/<agent_uuid>) 请回复一句简短测试消息，用于生成回归测试 task run。
   ```

4. Wait for the task to complete.
5. For cases requiring multiple runs, repeat the mention, trigger multiple agents, or use:

   ```bash
   multica issue rerun <issue_id_or_identifier> --output json
   ```

6. Reopen the issue detail page and verify the timeline / execution log / retry UI.

Do not mark Agent-run-dependent cases as BLOCKED until these setup steps were attempted and the exact failure is reported.

## 7. PASS / BLOCKED / FAIL discipline

PASS is allowed only when all applicable checks passed and required browser evidence exists.

BLOCKED means a required input, environment, URL, credential, local repo, branch, runtime, browser tool, or fixture could not be obtained after the documented setup attempts.

FAIL means the product behavior does not satisfy the testcase or PR requirement.

Do not report `PASS` by excluding BLOCKED cases unless the user explicitly accepts the risk. Use `PARTIAL PASS` or `PASS with accepted blockers` and list every blocked case.

## 8. Required report format

Multica Issue completion comments must be human-readable and include:

```markdown
## 验收结论：PASS / PARTIAL PASS / BLOCKED / FAIL

### 环境信息
- 测试 URL：...
- Worktree：...
- Branch：...
- Commit / PR：...

### 用例覆盖检查
- 新增/更新用例文件：...
- 复用既有用例文件：...
- 无需新增用例的依据：...
- Selection mode：...

### 执行结果
- 编译/类型检查/后端测试：...
- 浏览器回归：...
- 报告/截图/附件：...

### 失败或阻塞项
- Case：...
- 原因：...
- 已尝试的自愈/造数步骤：...

### 交回开发的问题（如有）
- 复现步骤：...
- 期望结果：...
- 实际结果：...
- 初步定位：...
```

A one-line PASS, or a PASS without testcase coverage analysis and evidence, is not acceptable.
