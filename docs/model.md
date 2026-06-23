# 工作流接口

## 1、 workflow详情

URI：/api/workflows

Format: 

```json
{
    "id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
    "workspace_id": "2a50366b-8414-4416-a754-44a249f8aea1",
    "title": "cospower全链路",
    "description": "",
    "status": "draft",
    "max_retries": 3,
    "created_by_type": "member",
    "created_by_id": "4358c114-7eb0-4759-8953-2de8e2adcabe",
    "node_count": 6,
    "is_template": false,
    "source_template_id": "f4a5676c-bcdb-4afe-ad3c-1e718456bcf4",
    "created_at": "2026-06-23T02:15:56Z",
    "updated_at": "2026-06-23T02:15:56Z"
}
```

## 2、workflow对应node

URI: /api/workflows/{workflowId}/nodes

Format: 

```json
{
    "nodes": [
        {
            "id": "9d0433e2-7a20-4101-bc30-d15c4414f67d",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "需求分析",
            "description": "",
            "position_x": 100,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "dd0683f4-d72c-4b49-8030-827f5b15df2e",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        },
        {
            "id": "28b89f54-fecf-4d15-af59-2dd43089919d",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "方案设计",
            "description": "",
            "position_x": 380,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "5e2fccac-6257-4ea5-ac7a-a5d8a4765917",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        },
        {
            "id": "b12d4cde-fd73-4c57-9bd6-2ee12ee9bf4b",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "任务拆解",
            "description": "",
            "position_x": 660,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "4348e20d-eadc-4095-ac7a-cd480e927375",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        },
        {
            "id": "7b4a37e5-7f09-4f52-9c62-02fa464aa4a1",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "测试生成",
            "description": "",
            "position_x": 940,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "67cdded4-c49f-4fc3-b7e0-52aa2038db91",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        },
        {
            "id": "cd644b4f-9a57-403e-bcd5-23a56ab92f04",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "编码",
            "description": "",
            "position_x": 1220,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "c0bea924-c78f-43b1-8d50-449ec3c6b4cf",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        },
        {
            "id": "76e660aa-74e6-4067-b509-821f6d670445",
            "workflow_id": "f0712b47-b19b-4a2d-be0b-1f44dc48697c",
            "title": "验证",
            "description": "",
            "position_x": 1500,
            "position_y": 300,
            "format_schema": null,
            "worker_type": "agent",
            "worker_id": "24a981c1-6ea6-4eab-9225-a5fe3da64477",
            "critic_type": "agent",
            "critic_id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
            "critic_api_url": "",
            "sort_order": 0,
            "stage_id": null,
            "created_at": "2026-06-23T02:15:56Z",
            "updated_at": "2026-06-23T02:15:56Z"
        }
    ]
}
```

## 3、Agent的详情

> [!CAUTION]
>
> 这里agent和node缺少关联

URI: /api/agents?workspace_id=2a50366b-8414-4416-a754-44a249f8aea1

Format: 

```json
[
    {
        "id": "dd0683f4-d72c-4b49-8030-827f5b15df2e",
        "workspace_id": "",
        "runtime_id": "",
        "name": "需求分析",
        "description": "需求梳理",
        "instructions": "你是一个需求分析师，按要求进行需求梳理",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "fa87f958-9229-442b-8bc3-4b22a4d6f806",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:24:44Z",
        "updated_at": "2026-06-23T00:41:24Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "5e2fccac-6257-4ea5-ac7a-a5d8a4765917",
        "workspace_id": "",
        "runtime_id": "",
        "name": "方案设计",
        "description": "方案设计",
        "instructions": "",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "365d045e-8487-467f-94e6-8237fa97f4a6",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:26:01Z",
        "updated_at": "2026-06-23T02:06:25Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "67cdded4-c49f-4fc3-b7e0-52aa2038db91",
        "workspace_id": "",
        "runtime_id": "",
        "name": "测试生成",
        "description": "测试生成",
        "instructions": "",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "10a3b7d2-1af0-41bc-9d9c-9ed812e230f4",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:29:43Z",
        "updated_at": "2026-06-17T06:39:28Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "4348e20d-eadc-4095-ac7a-cd480e927375",
        "workspace_id": "",
        "runtime_id": "",
        "name": "任务拆解",
        "description": "任务拆解",
        "instructions": "",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "8fabc295-cd8d-4514-9230-a00bc880a4bb",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:30:03Z",
        "updated_at": "2026-06-22T04:50:38Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "c0bea924-c78f-43b1-8d50-449ec3c6b4cf",
        "workspace_id": "",
        "runtime_id": "",
        "name": "TDD 编码",
        "description": "TDD 编码",
        "instructions": "",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "665b5bbf-b859-498a-826f-9584322c2a42",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:30:20Z",
        "updated_at": "2026-06-18T02:41:50Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "24a981c1-6ea6-4eab-9225-a5fe3da64477",
        "workspace_id": "",
        "runtime_id": "",
        "name": "集成验证",
        "description": "集成验证",
        "instructions": "",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": "5d54963c-9bb5-4373-9e33-721898452d78",
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:30:36Z",
        "updated_at": "2026-06-11T03:26:10Z",
        "archived_at": null,
        "archived_by": null
    },
    {
        "id": "a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6",
        "workspace_id": "",
        "runtime_id": "",
        "name": "审核师",
        "description": "审核验证",
        "instructions": "对前面的任务结果进行审核验证，但当前是测试阶段，直接通过就行",
        "avatar_url": null,
        "runtime_mode": "local",
        "runtime_config": {},
        "custom_env": {},
        "custom_args": [],
        "mcp_config": null,
        "custom_env_redacted": false,
        "mcp_config_redacted": false,
        "visibility": "workspace",
        "status": "idle",
        "max_concurrent_tasks": 6,
        "model": "",
        "thinking_level": "",
        "plugin_id": null,
        "is_builtin": true,
        "owner_id": "f440fd17-6f6a-4708-be0e-b8f83c5d7638",
        "skills": [],
        "created_at": "2026-06-05T07:33:00Z",
        "updated_at": "2026-06-22T04:44:37Z",
        "archived_at": null,
        "archived_by": null
    }
]
```

## 4、plugin详情

URI: /api/plugins/builtin

Format:

```json
{
    "hasMore": false,
    "items": [
        {
            "category": "testing",
            "content": "# cospowers Test Generation — 测试生成 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent derive test strategy, test cases, acceptance checks, regression checks, edge and exception cases, coverage reviews, and test code drafts from requirements, design documents, implementation plans, bug reports, API contracts, or existing code.\n\n## When to use it\n\nUse this plugin when the user asks to design tests, generate test cases, turn acceptance criteria into tests, protect a bug fix with regression tests, identify edge cases, review existing test coverage, or draft test code for a known language and framework.\n\n## Primary entry skill\n\nStart with `test-generation` unless the user explicitly asks for a narrower testing task. This skill is the plugin-level entry point and should decide whether the work needs strategy, cases, acceptance tests, regression tests, edge-case analysis, coverage review, test code generation, or a combination.\n\n## Skill selection guide\n\n- `test-generation`: Use as the default entry point for producing test artifacts from requirements, designs, plans, bug reports, APIs, or code.\n- `test-strategy-generation`: Use for test scope, test levels, risk areas, priorities, environments, data needs, and execution strategy.\n- `test-case-generation`: Use for detailed test cases with preconditions, steps, data, expected results, and traceability.\n- `acceptance-test-generation`: Use for user-facing acceptance checks and business-flow validation.\n- `regression-test-generation`: Use for bug fixes, changed behavior, compatibility risks, and existing behavior protection.\n- `edge-case-test-generation`: Use for boundaries, nulls, invalid input, extremes, permissions, concurrency, failures, and exceptional paths.\n- `test-coverage-review`: Use when existing tests need gap analysis against requirements, design, code, or risk areas.\n- `test-code-generator`: Use only after test scenarios are clear and target language, framework, interfaces, and execution environment are known.\n- `session-context`: Use to preserve testing-session context across multi-step work.\n- `using-test-generation-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n\n## Inputs to collect\n\nCollect requirement documents, design documents, implementation plans, bug reports, API or message contracts, existing test files, target language, test framework, test level, interface type, data setup, environment constraints, external dependencies, risk areas, excluded scope, and expected validation commands.\n\nIf the user provides repository code, inspect the relevant production and test code before generating executable test code. If only documents are available, produce document-level test scenarios and clearly mark assumptions.\n\n## Typical workflow\n\n1. Start from `test-generation` and identify the needed test artifact.\n2. Read requirements, designs, plans, contracts, bug reports, and relevant code or existing tests.\n3. Use `test-strategy-generation` when scope, priorities, environments, or risk-based testing need to be defined first.\n4. Use `test-case-generation`, `acceptance-test-generation`, `regression-test-generation`, and `edge-case-test-generation` to produce concrete scenarios.\n5. Use `test-coverage-review` when the task is to evaluate existing test coverage or find gaps.\n6. Use `test-code-generator` only after scenarios are stable and framework details are known.\n7. Include traceability from tests to requirements, design decisions, defects, or code paths.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating test artifacts. Typical outputs include:\n\n- `docs/tests/test-strategy.md` for test strategy and scope.\n- `docs/tests/test-cases.md` for detailed functional and non-functional test cases.\n- `docs/tests/acceptance-tests.md` for acceptance checks.\n- `docs/tests/regression-tests.md` for regression protection.\n- `docs/tests/coverage-review.md` for coverage gaps and recommendations.\n- `docs/tests/generated-test-code/` for generated test code drafts when requested.\n\nThe exact path may vary if the user requests a different location. Test code should follow local testing standards and match the repository's existing conventions.\n\n## Quality checks\n\nCheck that tests are traceable, executable or clearly marked as scenarios, meaningful, non-duplicative, and aligned with risk. Cover normal flows, boundaries, invalid inputs, permissions, failures, compatibility, concurrency, and non-functional requirements where applicable. For generated code, verify the framework, imports, setup, fixtures, and assertions match the repository context.\n\n## Handoff\n\nHand off to TDD development with generated test cases, test code drafts, target framework, expected failing/passing behavior, setup requirements, and validation commands. Hand off to integration verification when the work needs end-to-end, contract, regression, or release-readiness evidence.\n\n## Operating constraints\n\nUse bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, and examples unless the user provides external handoff documents or repository code. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "db5b17775d229f3e90eb2e24207652d8",
            "createdAt": "2026-06-04T13:17:29.798141Z",
            "createdBy": "system",
            "currentRevision": 2,
            "description": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。",
            "descriptions": {
                "en": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "10a3b7d2-1af0-41bc-9d9c-9ed812e230f4",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "df7e485e-708e-40cb-848d-e26467d1ca4e",
            "metadata": {
                "bundle": {
                    "agents_count": 0,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 10,
                    "skills_namespaces": [
                        "cospowers-test-generation:acceptance-test-generation",
                        "cospowers-test-generation:edge-case-test-generation",
                        "cospowers-test-generation:regression-test-generation",
                        "cospowers-test-generation:session-context",
                        "cospowers-test-generation:test-case-generation",
                        "cospowers-test-generation:test-code-generator",
                        "cospowers-test-generation:test-coverage-review",
                        "cospowers-test-generation:test-generation",
                        "cospowers-test-generation:test-strategy-generation",
                        "cospowers-test-generation:using-test-generation-plugin"
                    ]
                },
                "category": "testing",
                "description": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-test",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-test-generation"
                },
                "name": "cospowers-test-generation",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-test-generation",
            "previewCount": 5,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/10a3b7d2-1af0-41bc-9d9c-9ed812e230f4",
            "slug": "cospowers-test-generation",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-test-generation/.plugin.json",
            "sourceSha": "4e12bf8d12615c4ce0227cfa11ef5cae3a514b7a2b1f135421c711badebef609",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:25.962152Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        },
        {
            "category": "ai-ml",
            "content": "# cospowers TDD Development — TDD 编码 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent implement, fix, debug, and review code using test-driven development and local engineering standards. It supports red-green-refactor execution, implementation-plan execution, systematic debugging, code compliance checks, code review preparation, implementation review, subagent-assisted development, git worktree workflows, and structured commits when explicitly requested.\n\n## When to use it\n\nUse this plugin when the user asks to implement a feature, fix a bug, execute an implementation plan, write code using TDD, debug failing tests or runtime errors, check code compliance, prepare a code review request, coordinate subagents for development, work in isolated worktrees, or create a commit after implementation.\n\n## Primary entry skill\n\nStart with `tdd-implementation` unless the user explicitly asks for a narrower development task. This skill is the plugin-level entry point and should decide whether the work needs strict TDD, plan execution, debugging, review, compliance checking, subagent coordination, worktree isolation, or commit preparation.\n\n## Skill selection guide\n\n- `tdd-implementation`: Use as the default entry point for implementation, bug fixing, and code-change work.\n- `test-driven-development`: Use for strict red-green-refactor: write or confirm a failing test, implement the minimum code, pass tests, then refactor safely.\n- `executing-plans`: Use when the user provides an implementation plan, task graph, issue plan, or design-to-code handoff.\n- `subagent-driven-development`: Use when a complex task can be split into independent, well-scoped implementation or investigation tasks.\n- `systematic-debugging`: Use for failing tests, exceptions, logs, reproduction steps, unclear behavior, or regression diagnosis.\n- `code-compliance-check`: Use after implementation to check local coding standards, testing standards, review rules, and process expectations.\n- `implementation-review`: Use to evaluate whether code changes satisfy requirements, design, plans, and tests.\n- `requesting-code-review`: Use to prepare review materials, summaries, risk notes, and reviewer-facing context.\n- `using-git-worktrees`: Use when isolated or parallel development branches/worktrees are appropriate and authorized.\n- `spec-commit`: Use only when the user explicitly asks to create a commit.\n- `session-context`: Use to preserve development-session context across multi-step work.\n- `using-tdd-development-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n- `agents/code-reviewer.md`: Use as the local code review agent definition when a dedicated review pass is needed.\n\n## Inputs to collect\n\nCollect the implementation goal, requirement or design documents, implementation plan, target files or modules, existing code context, relevant tests, expected behavior, reproduction steps for bugs, failing command output, target language and framework, coding/testing standards, branch/worktree constraints, and whether the user permits code edits, test execution, branch or worktree operations, and commits.\n\nFor commits, require an explicit user request. Do not infer commit permission from implementation permission.\n\n## Typical workflow\n\n1. Start from `tdd-implementation` and identify whether the task is new implementation, bug fix, debugging, plan execution, review, or commit preparation.\n2. Read the relevant requirements, plans, tests, and code before proposing or making code changes.\n3. Use `test-driven-development` when TDD is possible: establish the failing test, make the minimum implementation, run tests, then refactor.\n4. Use `executing-plans` when a plan exists and keep progress aligned to the plan's acceptance criteria and validation commands.\n5. Use `systematic-debugging` when behavior is unclear or verification fails; diagnose root cause before changing approach.\n6. Use `code-compliance-check` and `implementation-review` after changes to verify standards and requirement satisfaction.\n7. Use `requesting-code-review` when preparing handoff for human or agent review.\n8. Use `spec-commit` only after the user explicitly asks for a commit and after checking the final diff and status.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating development artifacts. Typical outputs include:\n\n- Code changes and corresponding tests.\n- Test command results and relevant logs.\n- TDD cycle notes or report when useful.\n- Debugging reports for investigated failures.\n- Code compliance reports.\n- Implementation review reports.\n- Code review request materials.\n- Commit messages or commits only when explicitly requested.\n\nThe exact files changed should be limited to what the user's task requires. Avoid unrelated refactors or speculative abstractions.\n\n## Quality checks\n\nRun the validation commands required by the plan or repository context when permitted. Confirm tests fail for the expected reason before implementation when using TDD, and confirm they pass after the change. Check coding standards, testing standards, security-sensitive behavior, error handling at system boundaries, compatibility risks, and whether the implementation satisfies the original requirement without adding unrequested scope.\n\n## Handoff\n\nHand off to integration verification with the code changes, tests added or updated, commands run, results, known risks, unresolved questions, and any contracts or behavior that should be verified across modules or services.\n\n## Operating constraints\n\nMatch code edits, test execution, branch/worktree operations, and commits to the user's authorization. Commits require an explicit user request. Use bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, `agents/`, and examples unless the user provides external handoff documents or repository code. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "bf878d8163fc65f01a6b360b981f529a",
            "createdAt": "2026-06-04T13:17:29.789468Z",
            "createdBy": "system",
            "currentRevision": 1,
            "description": "AI 驱动的 TDD 编码插件：基于任务、测试、issue 或 bug 执行测试优先开发、调试、代码检查和提交。",
            "descriptions": {
                "en": "AI 驱动的 TDD 编码插件：基于任务、测试、issue 或 bug 执行测试优先开发、调试、代码检查和提交。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "665b5bbf-b859-498a-826f-9584322c2a42",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "b990e7d5-73f6-4628-8efb-94ba92856f63",
            "metadata": {
                "bundle": {
                    "agents_count": 1,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 12,
                    "skills_namespaces": [
                        "cospowers-tdd-development:code-compliance-check",
                        "cospowers-tdd-development:executing-plans",
                        "cospowers-tdd-development:implementation-review",
                        "cospowers-tdd-development:requesting-code-review",
                        "cospowers-tdd-development:session-context",
                        "cospowers-tdd-development:spec-commit",
                        "cospowers-tdd-development:subagent-driven-development",
                        "cospowers-tdd-development:systematic-debugging",
                        "cospowers-tdd-development:tdd-implementation",
                        "cospowers-tdd-development:test-driven-development",
                        "cospowers-tdd-development:using-git-worktrees",
                        "cospowers-tdd-development:using-tdd-development-plugin"
                    ]
                },
                "category": "ai-ml",
                "description": "AI 驱动的 TDD 编码插件：基于任务、测试、issue 或 bug 执行测试优先开发、调试、代码检查和提交。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-tdd",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-tdd-development"
                },
                "name": "cospowers-tdd-development",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-tdd-development",
            "previewCount": 4,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/665b5bbf-b859-498a-826f-9584322c2a42",
            "slug": "cospowers-tdd-development",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-tdd-development/.plugin.json",
            "sourceSha": "6b857d568d116d2802e7664acf59d020e3a8d9ee77b2c3fb76dba7e53c6a6ade",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:23.616267Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        },
        {
            "category": "ai-ml",
            "content": "# cospowers Task Planning — 任务拆解 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent turn requirements, design documents, issues, feature goals, or bug-fix goals into executable implementation plans. It focuses on task decomposition, dependency ordering, task graphs, milestones, execution strategy, subagent dispatch, and worktree planning.\n\n## When to use it\n\nUse this plugin when the user asks to break work into tasks, create an implementation plan, plan a feature or bug fix, identify dependencies, decide what can run in parallel, prepare subagent assignments, plan isolated worktrees, or define milestones before implementation starts.\n\n## Primary entry skill\n\nStart with `task-planning` unless the user explicitly asks for a narrower planning artifact. This skill is the plugin-level entry point and should determine whether the work needs an implementation plan, task graph, execution strategy, milestone plan, subagent plan, worktree plan, or a combination.\n\n## Skill selection guide\n\n- `task-planning`: Use as the default entry point for converting requirements, design, issues, or goals into actionable work.\n- `writing-plans`: Use for implementation plans with concrete steps, target files, acceptance criteria, and validation commands.\n- `task-graph-generation`: Use to expose sequencing, dependencies, blocked work, critical path, and parallelizable tasks.\n- `execution-strategy-selection`: Use to choose serial execution, parallel execution, subagents, worktrees, or a hybrid approach.\n- `subagent-dispatch-planning`: Use when work can be delegated to multiple agents with clear inputs, outputs, and boundaries.\n- `worktree-planning`: Use when isolated branches or worktrees would reduce risk or enable parallel work.\n- `milestone-planning`: Use for staged delivery, checkpoints, phased rollout, or multi-step implementation programs.\n- `session-context`: Use to preserve planning-session context across multi-step work.\n- `using-task-planning-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n\n## Inputs to collect\n\nCollect requirement documents, design documents, issue or bug descriptions, target repository context, affected files or modules if known, constraints, deadlines or milestones if relevant, acceptance criteria, validation commands, test expectations, risks, dependencies, available agents, and whether branch or worktree isolation is allowed.\n\nIf the user provides handoff documents from requirements or solution design, read them and keep every task traceable to the original goals and constraints.\n\n## Typical workflow\n\n1. Start from `task-planning` and identify the planning artifact the user needs.\n2. Read the available requirements, design documents, issues, and repository context before decomposing work.\n3. Use `writing-plans` to produce a concrete implementation plan with tasks, target files, outputs, acceptance criteria, and validation.\n4. Use `task-graph-generation` to identify dependencies, ordering, blockers, and parallelizable work.\n5. Use `execution-strategy-selection` to decide whether work should be serial, parallel, subagent-based, worktree-based, or hybrid.\n6. Use `subagent-dispatch-planning`, `worktree-planning`, or `milestone-planning` when the scope or risk justifies those artifacts.\n7. End with a plan that is specific enough for an implementation agent to execute without re-discovering the entire problem.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating planning artifacts. Typical outputs include:\n\n- `docs/plans/implementation-plan.md` for step-by-step implementation planning.\n- `docs/plans/task-graph.md` for dependency graphs and sequencing.\n- `docs/plans/execution-strategy.md` for execution mode selection and rationale.\n- `docs/plans/subagent-dispatch-plan.md` for delegated task assignments.\n- `docs/plans/worktree-plan.md` for isolated or parallel worktree execution.\n- `docs/plans/milestone-plan.md` for staged delivery and checkpoints.\n\nPlans should include goals, inputs, outputs, target files or modules, dependencies, assumptions, acceptance criteria, validation commands, risk notes, and handoff instructions.\n\n## Quality checks\n\nCheck that every task has a clear outcome, owner or execution mode when relevant, dependency status, validation method, and acceptance criteria. Verify that the plan does not hide blockers, skip required design or testing work, or assign parallel work that depends on unresolved shared state.\n\n## Handoff\n\nHand off to test generation when test strategy, test cases, or test code drafts are needed before implementation. Hand off to TDD development when the implementation plan is ready to execute. Include the implementation plan, task graph, validation commands, risk notes, and any required branch or worktree strategy.\n\n## Operating constraints\n\nThis plugin should generally produce plans and planning artifacts, not modify product code. Use bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, and examples unless the user provides external handoff documents. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "457190330ae86e364b010543cc3ffa6c",
            "createdAt": "2026-06-04T13:17:29.777159Z",
            "createdBy": "system",
            "currentRevision": 1,
            "description": "AI 驱动的任务拆解插件：将需求或设计拆解为实现计划、任务图、里程碑和执行策略。",
            "descriptions": {
                "en": "AI 驱动的任务拆解插件：将需求或设计拆解为实现计划、任务图、里程碑和执行策略。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "8fabc295-cd8d-4514-9230-a00bc880a4bb",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "0f9031a0-2183-4b93-8630-0672df924d43",
            "metadata": {
                "bundle": {
                    "agents_count": 0,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 9,
                    "skills_namespaces": [
                        "cospowers-task-planning:execution-strategy-selection",
                        "cospowers-task-planning:milestone-planning",
                        "cospowers-task-planning:session-context",
                        "cospowers-task-planning:subagent-dispatch-planning",
                        "cospowers-task-planning:task-graph-generation",
                        "cospowers-task-planning:task-planning",
                        "cospowers-task-planning:using-task-planning-plugin",
                        "cospowers-task-planning:worktree-planning",
                        "cospowers-task-planning:writing-plans"
                    ]
                },
                "category": "ai-ml",
                "description": "AI 驱动的任务拆解插件：将需求或设计拆解为实现计划、任务图、里程碑和执行策略。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-plan",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-task-planning"
                },
                "name": "cospowers-task-planning",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-task-planning",
            "previewCount": 8,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/8fabc295-cd8d-4514-9230-a00bc880a4bb",
            "slug": "cospowers-task-planning",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-task-planning/.plugin.json",
            "sourceSha": "65c97d9b9740f8b5ace245e8781f97c32c75645a4744278afb6ac125d92128d0",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:23.589112Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        },
        {
            "category": "ai-ml",
            "content": "# cospowers Solution Design — 方案设计 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent turn approved requirements, PRDs, issue context, existing system context, or design goals into architecture and design artifacts. It covers system design, subsystem design, API and message contracts, architecture review, design change impact analysis, and cross-document consistency checks.\n\n## When to use it\n\nUse this plugin when the user asks to design a system, convert requirements into technical architecture, write subsystem or module design, define APIs or event contracts, review an existing design, analyze the effect of design changes, or check whether requirements, design documents, and contracts agree with each other.\n\n## Primary entry skill\n\nStart with `solution-design` unless the user explicitly asks for a specific design task. This skill is the plugin-level entry point and should route the work toward system design, subsystem design, API contract design, review, change analysis, or consistency checking.\n\n## Skill selection guide\n\n- `solution-design`: Use as the default entry point for architecture and solution design from requirements, PRDs, issues, or system context.\n- `design-spec`: Use for system-level design, including architecture, components, data flow, deployment, dependencies, constraints, risks, and DFX considerations.\n- `subsystem-design-spec`: Use for detailed service, module, subsystem, component, responsibility, interface, and internal behavior design.\n- `api-contract-design`: Use for OpenAPI, AsyncAPI, interface, service boundary, request/response, event, and compatibility contract design.\n- `architecture-review`: Use when reviewing an existing design draft for correctness, completeness, consistency, risks, and feasibility.\n- `design-change-analysis`: Use when architecture, interfaces, data flow, deployment, dependencies, or module responsibilities change.\n- `doc-consistency-check`: Use when requirements, system design, subsystem design, API contracts, or other design documents may conflict.\n- `sysdesign-evaluator`: Use to evaluate system design document quality.\n- `subsystem-evaluator`: Use to evaluate subsystem design document quality.\n- `doc-quality-evaluator`: Use to evaluate document quality and cross-document issues.\n- `session-context`: Use to preserve design-session context across multi-step work.\n- `using-solution-design-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n\n## Inputs to collect\n\nCollect requirement documents, target users and flows, system boundaries, existing architecture, affected modules, data model expectations, API or event needs, deployment environment, external dependencies, scalability and performance targets, security and compliance constraints, compatibility needs, observability expectations, reliability requirements, and known trade-offs.\n\nIf the user provides handoff documents from requirements, planning, testing, or implementation work, read those documents and keep design decisions traceable to them.\n\n## Typical workflow\n\n1. Start from `solution-design` and identify the design scope: system, subsystem, API contract, review, change analysis, or consistency check.\n2. Read provided requirement artifacts and existing system context before proposing a design.\n3. Use `design-spec` for end-to-end architecture and system-level decisions.\n4. Use `subsystem-design-spec` for detailed module or service design after system boundaries are clear.\n5. Use `api-contract-design` when service boundaries, external interfaces, messages, OpenAPI, or AsyncAPI contracts are needed.\n6. Use review, change-analysis, consistency-check, and evaluator skills before treating design output as ready for downstream planning.\n7. Record assumptions, unresolved decisions, risks, alternatives considered, and validation needs.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating design artifacts. Typical outputs include:\n\n- `docs/design/system-design.md` for system-level architecture.\n- `docs/design/subsystem-\u003cname\u003e-design.md` for subsystem or module design.\n- `docs/design/openapi.yaml` for HTTP API contracts.\n- `docs/design/asyncapi.yaml` for event or message contracts.\n- `docs/design/architecture-review-report.md` for review findings.\n- `docs/design/design-change-impact.md` for design change impact analysis.\n- `docs/design/doc-consistency-report.md` for cross-document consistency results.\n\nThe exact path may vary if the user requests a different location, but outputs should be specific enough for task planning, implementation, and testing.\n\n## Quality checks\n\nUse `sysdesign-evaluator`, `subsystem-evaluator`, and `doc-quality-evaluator` where appropriate. Check traceability to requirements, unclear boundaries, missing interfaces, incomplete data flow, unhandled errors, security gaps, performance risks, compatibility risks, deployment assumptions, inconsistent terminology, and contradictions across documents.\n\n## Handoff\n\nHand off to task planning with design documents, contracts, assumptions, dependencies, risks, and validation needs. If test design is needed next, hand off API contracts, acceptance criteria, quality requirements, and risk areas to test generation.\n\n## Operating constraints\n\nUse bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, `evaluators/`, and examples unless the user provides external handoff documents. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "5b37a9c222e92d7abf5892aa72360d7e",
            "createdAt": "2026-06-04T13:17:29.76824Z",
            "createdBy": "system",
            "currentRevision": 1,
            "description": "AI 驱动的方案设计插件：从需求、PRD 或现有系统上下文生成系统设计、子系统设计和 API 契约。",
            "descriptions": {
                "en": "AI 驱动的方案设计插件：从需求、PRD 或现有系统上下文生成系统设计、子系统设计和 API 契约。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "365d045e-8487-467f-94e6-8237fa97f4a6",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "42b031b8-d38e-499e-9cf4-253b4ee4930e",
            "metadata": {
                "bundle": {
                    "agents_count": 0,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 12,
                    "skills_namespaces": [
                        "cospowers-solution-design:api-contract-design",
                        "cospowers-solution-design:architecture-review",
                        "cospowers-solution-design:design-change-analysis",
                        "cospowers-solution-design:design-spec",
                        "cospowers-solution-design:doc-consistency-check",
                        "cospowers-solution-design:doc-quality-evaluator",
                        "cospowers-solution-design:session-context",
                        "cospowers-solution-design:solution-design",
                        "cospowers-solution-design:subsystem-design-spec",
                        "cospowers-solution-design:subsystem-evaluator",
                        "cospowers-solution-design:sysdesign-evaluator",
                        "cospowers-solution-design:using-solution-design-plugin"
                    ]
                },
                "category": "ai-ml",
                "description": "AI 驱动的方案设计插件：从需求、PRD 或现有系统上下文生成系统设计、子系统设计和 API 契约。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-design",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-solution-design"
                },
                "name": "cospowers-solution-design",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-solution-design",
            "previewCount": 16,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/365d045e-8487-467f-94e6-8237fa97f4a6",
            "slug": "cospowers-solution-design",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-solution-design/.plugin.json",
            "sourceSha": "533c1add4c090240226ed2c2e8ee82591b4508c50b7444ba779092f36fc03b2c",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:23.620362Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        },
        {
            "category": "ai-ml",
            "content": "# cospowers Requirements — 需求梳理 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent turn rough ideas, PRDs, issues, bug reports, customer feedback, or requirement changes into structured requirement artifacts. It separates user/business requirements from system requirements, supports requirement quality review, and analyzes requirement change impact.\n\n## When to use it\n\nUse this plugin when the user asks to clarify requirements, write requirement documents, analyze a feature request, transform business goals into system needs, review existing requirement documents, or evaluate the impact of changed scope, rules, acceptance criteria, constraints, or non-functional requirements.\n\n## Primary entry skill\n\nStart with `requirements-intake` unless the user explicitly asks for a narrower requirement task. This skill is the plugin-level intake path and should decide whether to produce user requirements, system requirements, review output, change-impact output, or a combination.\n\n## Skill selection guide\n\n- `requirements-intake`: Use as the default entry point for raw ideas, PRDs, issue descriptions, bug reports, feedback, or mixed requirement inputs.\n- `requirement-analysis`: Use when the task is focused on user/business requirements, user goals, scenarios, business rules, acceptance criteria, and requirement boundaries.\n- `system-requirement-analysis`: Use when the task is focused on system capabilities, constraints, interfaces, quality attributes, security, performance, reliability, maintainability, and other DFX requirements.\n- `requirements-review`: Use when the user already has requirement documents and wants completeness, clarity, consistency, feasibility, or traceability review.\n- `requirements-change-analysis`: Use when requirements have changed and the user needs impact analysis across scope, design, tests, delivery, or risks.\n- `aireq-evaluator`: Use to evaluate user/business requirement documents.\n- `sysreq-evaluator`: Use to evaluate system requirement documents.\n- `session-context`: Use to capture or restore requirement-session context when a multi-step workflow needs continuity.\n- `using-requirements-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n\n## Inputs to collect\n\nCollect the user's goal, target users, business process, pain points, existing PRD or issue text, acceptance criteria, constraints, explicit exclusions, dependencies, affected systems, security/performance/reliability expectations, and whether this is a new requirement or a change to existing requirements.\n\nIf the user provides handoff documents from other plugins or existing repository documents, read those documents and keep analysis grounded in the provided material.\n\n## Typical workflow\n\n1. Start from `requirements-intake` and identify whether the request needs user requirements, system requirements, review, change analysis, or evaluation.\n2. Ask clarifying questions only for missing information that blocks a useful requirement artifact.\n3. Use `requirement-analysis` for business/user-facing requirement structure.\n4. Use `system-requirement-analysis` to derive implementation-facing capabilities, constraints, interfaces, and quality requirements.\n5. Use `requirements-review`, `requirements-change-analysis`, `aireq-evaluator`, or `sysreq-evaluator` when the user asks for review, impact analysis, or quality scoring.\n6. Produce clear requirement documents and note assumptions, open questions, risks, and traceability links.\n7. Hand off the resulting requirement artifacts to solution design when the user is ready for architecture or technical design.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating documents. Typical outputs include:\n\n- `docs/requirements/ai-requirements.md` for user/business requirements.\n- `docs/requirements/system-requirements.md` for system requirements.\n- `docs/requirements/requirements-review-report.md` for requirement review findings.\n- `docs/requirements/requirements-change-impact.md` for change-impact analysis.\n\nThe exact path may vary if the user requests a different location, but the output should remain structured, traceable, and suitable for handoff.\n\n## Quality checks\n\nUse `aireq-evaluator` for user/business requirement quality and `sysreq-evaluator` for system requirement quality. Check for ambiguity, missing actors, missing acceptance criteria, conflicting rules, unstated constraints, unverifiable requirements, incomplete quality attributes, and unclear scope boundaries.\n\n## Handoff\n\nWhen requirements are complete enough, hand off to the solution-design stage with the generated requirement documents, assumptions, unresolved questions, impacted systems, constraints, and acceptance criteria. Do not require another cospowers plugin to be installed; simply provide artifacts that a later design workflow can consume.\n\n## Operating constraints\n\nUse bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, `evaluators/`, and examples unless the user provides external handoff documents. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "70db9781310e72487656159da0a3ba58",
            "createdAt": "2026-06-04T13:17:29.758742Z",
            "createdBy": "system",
            "currentRevision": 1,
            "description": "AI 驱动的需求梳理插件：将原始想法、PRD、issue、需求变更转化为结构化需求和系统需求。",
            "descriptions": {
                "en": "AI 驱动的需求梳理插件：将原始想法、PRD、issue、需求变更转化为结构化需求和系统需求。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "fa87f958-9229-442b-8bc3-4b22a4d6f806",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "724317c0-fc1f-4f64-85bf-cb76499e9ef2",
            "metadata": {
                "bundle": {
                    "agents_count": 0,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 9,
                    "skills_namespaces": [
                        "cospowers-requirements:aireq-evaluator",
                        "cospowers-requirements:requirement-analysis",
                        "cospowers-requirements:requirements-change-analysis",
                        "cospowers-requirements:requirements-intake",
                        "cospowers-requirements:requirements-review",
                        "cospowers-requirements:session-context",
                        "cospowers-requirements:sysreq-evaluator",
                        "cospowers-requirements:system-requirement-analysis",
                        "cospowers-requirements:using-requirements-plugin"
                    ]
                },
                "category": "ai-ml",
                "description": "AI 驱动的需求梳理插件：将原始想法、PRD、issue、需求变更转化为结构化需求和系统需求。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-requirements",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-requirements"
                },
                "name": "cospowers-requirements",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-requirements",
            "previewCount": 20,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/fa87f958-9229-442b-8bc3-4b22a4d6f806",
            "slug": "cospowers-requirements",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-requirements/.plugin.json",
            "sourceSha": "09f9a4fd6bc68fca4c744e759ff5a52b7e11c02db9a60b18481a63eccacac8c3",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:23.613797Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        },
        {
            "category": "ai-ml",
            "content": "# cospowers Integration Verification — 集成验证 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent prove that completed or near-completed work is safe to finish, merge, release, or deliver. It supports integration testing, regression verification, contract verification, end-to-end verification, release readiness checks, final verification before completion, development branch finishing, compliance checks, debugging, review preparation, and structured commits when explicitly requested.\n\n## When to use it\n\nUse this plugin when the user asks to verify completed work, run integration checks, confirm a bug fix did not regress, validate API or message compatibility, test a complete user flow, assess release readiness, finish a development branch, prepare verification evidence, or decide whether work is ready to report as done.\n\n## Primary entry skill\n\nStart with `integration-verification` unless the user explicitly asks for a narrower verification task. This skill is the plugin-level entry point and should decide whether the work needs integration, regression, contract, E2E, release-readiness, final-completion, branch-finishing, debugging, compliance, or review support.\n\n## Skill selection guide\n\n- `integration-verification`: Use as the default entry point for verification after implementation or before delivery.\n- `integration-test-runner`: Use for multi-module, multi-component, service interaction, dependency, environment, or integration-suite checks.\n- `regression-verification`: Use to prove existing behavior and previously fixed bugs remain protected after a change.\n- `contract-verification`: Use for API contracts, message schemas, service boundaries, OpenAPI, AsyncAPI, backward compatibility, and consumer/provider expectations.\n- `e2e-verification`: Use for complete user-facing or business-critical flows across system boundaries.\n- `release-readiness-check`: Use before merge, release, deployment, or delivery to assess readiness, risks, blockers, and required evidence.\n- `verification-before-completion`: Use before declaring a task complete; require concrete command output, inspection evidence, or a clear statement of what could not be verified.\n- `finishing-a-development-branch`: Use for final branch cleanup, status checks, review preparation, and closing verification steps.\n- `systematic-debugging`: Use when verification fails or results are unclear.\n- `code-compliance-check`: Use to check local coding, testing, review, and process standards as part of final verification.\n- `requesting-code-review`: Use to prepare review materials after verification.\n- `doc-quality-evaluator`: Use to evaluate verification, design, or release-readiness documents where document quality matters.\n- `spec-commit`: Use only when the user explicitly asks to create a commit.\n- `session-context`: Use to preserve verification-session context across multi-step work.\n- `using-integration-verification-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n- `agents/code-reviewer.md`: Use as the local code review agent definition when a dedicated review pass is needed.\n\n## Inputs to collect\n\nCollect the change summary, requirements and design artifacts, implementation plan, code diff, test plan, existing test commands, build commands, API or message contracts, affected modules and services, environment availability, external dependency constraints, branch status, prior failures, bug reproduction steps, release criteria, and user authorization for running commands, branch operations, or commits.\n\nIf some environments, services, credentials, or dependencies are unavailable, record the limitation and verify what can be verified locally or by inspection.\n\n## Typical workflow\n\n1. Start from `integration-verification` and identify the verification scope: integration, regression, contract, E2E, release readiness, branch finishing, or final completion.\n2. Read the relevant requirements, design, plan, code diff, tests, contracts, and prior command output before choosing verification steps.\n3. Use `integration-test-runner`, `regression-verification`, `contract-verification`, or `e2e-verification` based on the risk and artifact type.\n4. Use `systematic-debugging` when a verification command fails or evidence contradicts expectations.\n5. Use `code-compliance-check`, `doc-quality-evaluator`, and `requesting-code-review` as supporting checks when the task needs final readiness or review preparation.\n6. Use `release-readiness-check`, `verification-before-completion`, or `finishing-a-development-branch` before merge, release, delivery, or declaring work complete.\n7. Report pass/fail status with evidence, risks, blockers, limitations, and next actions.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating verification artifacts. Typical outputs include:\n\n- `docs/verification/integration-test-report.md` for integration results.\n- `docs/verification/regression-report.md` for regression verification.\n- `docs/verification/contract-verification-report.md` for contract and compatibility evidence.\n- `docs/verification/release-readiness-report.md` for release readiness.\n- `docs/verification/final-verification-report.md` for completion evidence.\n- `docs/verification/branch-finish-report.md` for development branch closing checks.\n\nReports should include the verification scope, commands or inspections performed, relevant output, pass/fail conclusion, risks, blockers, unverified areas, and recommended next actions.\n\n## Quality checks\n\nNever claim completion without evidence. Prefer command output, test results, build results, contract checks, code inspection notes, or documented environment limitations. Check that verification covers changed behavior, surrounding integration points, critical user flows, compatibility boundaries, release criteria, and known risk areas.\n\n## Handoff\n\nHand off the final result with a clear pass/fail conclusion, evidence, unresolved risks, blockers, and next actions. If failures are found, hand off to TDD development or debugging with reproduction steps, failing commands, relevant logs, and suspected impact.\n\n## Operating constraints\n\nMatch command execution, branch operations, environment access, and commits to the user's authorization. Commits require an explicit user request. Use bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, `evaluators/`, `agents/`, and examples unless the user provides external handoff documents or repository code. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
            "contentMd5": "4ba3546fd9154074589d46c518d050fb",
            "createdAt": "2026-06-04T13:17:29.74918Z",
            "createdBy": "system",
            "currentRevision": 2,
            "description": "AI 驱动的集成验证插件：执行集成测试、回归验证、契约验证、发布前检查和分支收尾。",
            "descriptions": {
                "en": "AI 驱动的集成验证插件：执行集成测试、回归验证、契约验证、发布前检查和分支收尾。"
            },
            "evaluation": {},
            "favoriteCount": 0,
            "health": {
                "freshness_label": "active",
                "score": 100,
                "signals": {}
            },
            "id": "5d54963c-9bb5-4373-9e33-721898452d78",
            "installCount": 0,
            "isBuiltIn": true,
            "itemType": "plugin",
            "lastScanId": "91440cd3-860d-4453-9f74-1faafdebf5f0",
            "metadata": {
                "bundle": {
                    "agents_count": 1,
                    "commands_count": 0,
                    "hook_events": [],
                    "hooks_count": 0,
                    "is_marketplace_repo": false,
                    "mcp_server_names": [],
                    "mcp_servers_count": 0,
                    "skills_count": 15,
                    "skills_namespaces": [
                        "cospowers-integration-verification:code-compliance-check",
                        "cospowers-integration-verification:contract-verification",
                        "cospowers-integration-verification:doc-quality-evaluator",
                        "cospowers-integration-verification:e2e-verification",
                        "cospowers-integration-verification:finishing-a-development-branch",
                        "cospowers-integration-verification:integration-test-runner",
                        "cospowers-integration-verification:integration-verification",
                        "cospowers-integration-verification:regression-verification",
                        "cospowers-integration-verification:release-readiness-check",
                        "cospowers-integration-verification:requesting-code-review",
                        "cospowers-integration-verification:session-context",
                        "cospowers-integration-verification:spec-commit",
                        "cospowers-integration-verification:systematic-debugging",
                        "cospowers-integration-verification:using-integration-verification-plugin",
                        "cospowers-integration-verification:verification-before-completion"
                    ]
                },
                "category": "ai-ml",
                "description": "AI 驱动的集成验证插件：执行集成测试、回归验证、契约验证、发布前检查和分支收尾。",
                "install": {
                    "marketplace": "yhangf/csc-plugins",
                    "marketplace_name": "ai-workers-integration-verification",
                    "marketplace_repo": "yhangf/csc-plugins",
                    "marketplace_verified": true,
                    "method": "plugin_marketplace",
                    "plugin_name": "cospowers-integration-verification"
                },
                "name": "cospowers-integration-verification",
                "tags": [
                    "cospowers",
                    "ai-workers"
                ]
            },
            "name": "cospowers-integration-verification",
            "previewCount": 3,
            "registry": {
                "createdAt": "2026-03-17T10:24:37.973671Z",
                "description": "Default public registry — anyone can browse and contribute",
                "externalBranch": "main",
                "externalUrl": "",
                "id": "00000000-0000-0000-0000-000000000001",
                "lastSyncLogId": null,
                "lastSyncSha": "",
                "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
                "name": "public",
                "ownerId": "system",
                "repoId": "public",
                "sourceType": "internal",
                "syncConfig": {},
                "syncEnabled": false,
                "syncInterval": 3600,
                "syncStatus": "idle",
                "updatedAt": "2026-06-04T13:17:59.260075Z"
            },
            "registryId": "00000000-0000-0000-0000-000000000001",
            "repoId": "public",
            "securityStatus": "unscanned",
            "shareUrl": "http://api-costrict-web-api.costrict-web:8080/m/store/5d54963c-9bb5-4373-9e33-721898452d78",
            "slug": "cospowers-integration-verification",
            "source": "csc-plugins",
            "sourcePath": "plugins/cospowers-integration-verification/.plugin.json",
            "sourceSha": "6d8ed1a3c13c59fc11cf699a6fa5dee79e274089e5ac3ae4a1d64fc1274c18ab",
            "sourceType": "direct",
            "status": "active",
            "updatedAt": "2026-06-22T03:45:23.957893Z",
            "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
            "version": "1.0.0"
        }
    ],
    "page": 1,
    "pageSize": 100,
    "total": 6
}
```

## 5、 plugin包含的技能

URI: /cloud-api/api/items/10a3b7d2-1af0-41bc-9d9c-9ed812e230f4
```json
{
    "id": "10a3b7d2-1af0-41bc-9d9c-9ed812e230f4",
    "registryId": "00000000-0000-0000-0000-000000000001",
    "repoId": "public",
    "slug": "cospowers-test-generation",
    "itemType": "plugin",
    "name": "cospowers-test-generation",
    "description": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。",
    "descriptions": {
        "en": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。"
    },
    "category": "testing",
    "version": "1.0.0",
    "content": "# cospowers Test Generation — 测试生成 Plugin Usage Guide\n\n## What this plugin is for\n\nThis plugin helps an agent derive test strategy, test cases, acceptance checks, regression checks, edge and exception cases, coverage reviews, and test code drafts from requirements, design documents, implementation plans, bug reports, API contracts, or existing code.\n\n## When to use it\n\nUse this plugin when the user asks to design tests, generate test cases, turn acceptance criteria into tests, protect a bug fix with regression tests, identify edge cases, review existing test coverage, or draft test code for a known language and framework.\n\n## Primary entry skill\n\nStart with `test-generation` unless the user explicitly asks for a narrower testing task. This skill is the plugin-level entry point and should decide whether the work needs strategy, cases, acceptance tests, regression tests, edge-case analysis, coverage review, test code generation, or a combination.\n\n## Skill selection guide\n\n- `test-generation`: Use as the default entry point for producing test artifacts from requirements, designs, plans, bug reports, APIs, or code.\n- `test-strategy-generation`: Use for test scope, test levels, risk areas, priorities, environments, data needs, and execution strategy.\n- `test-case-generation`: Use for detailed test cases with preconditions, steps, data, expected results, and traceability.\n- `acceptance-test-generation`: Use for user-facing acceptance checks and business-flow validation.\n- `regression-test-generation`: Use for bug fixes, changed behavior, compatibility risks, and existing behavior protection.\n- `edge-case-test-generation`: Use for boundaries, nulls, invalid input, extremes, permissions, concurrency, failures, and exceptional paths.\n- `test-coverage-review`: Use when existing tests need gap analysis against requirements, design, code, or risk areas.\n- `test-code-generator`: Use only after test scenarios are clear and target language, framework, interfaces, and execution environment are known.\n- `session-context`: Use to preserve testing-session context across multi-step work.\n- `using-test-generation-plugin`: Use when the user asks how to use this plugin or when an agent needs plugin-level workflow guidance.\n\n## Inputs to collect\n\nCollect requirement documents, design documents, implementation plans, bug reports, API or message contracts, existing test files, target language, test framework, test level, interface type, data setup, environment constraints, external dependencies, risk areas, excluded scope, and expected validation commands.\n\nIf the user provides repository code, inspect the relevant production and test code before generating executable test code. If only documents are available, produce document-level test scenarios and clearly mark assumptions.\n\n## Typical workflow\n\n1. Start from `test-generation` and identify the needed test artifact.\n2. Read requirements, designs, plans, contracts, bug reports, and relevant code or existing tests.\n3. Use `test-strategy-generation` when scope, priorities, environments, or risk-based testing need to be defined first.\n4. Use `test-case-generation`, `acceptance-test-generation`, `regression-test-generation`, and `edge-case-test-generation` to produce concrete scenarios.\n5. Use `test-coverage-review` when the task is to evaluate existing test coverage or find gaps.\n6. Use `test-code-generator` only after scenarios are stable and framework details are known.\n7. Include traceability from tests to requirements, design decisions, defects, or code paths.\n\n## Outputs to produce\n\nUse local templates under `templates/` when generating test artifacts. Typical outputs include:\n\n- `docs/tests/test-strategy.md` for test strategy and scope.\n- `docs/tests/test-cases.md` for detailed functional and non-functional test cases.\n- `docs/tests/acceptance-tests.md` for acceptance checks.\n- `docs/tests/regression-tests.md` for regression protection.\n- `docs/tests/coverage-review.md` for coverage gaps and recommendations.\n- `docs/tests/generated-test-code/` for generated test code drafts when requested.\n\nThe exact path may vary if the user requests a different location. Test code should follow local testing standards and match the repository's existing conventions.\n\n## Quality checks\n\nCheck that tests are traceable, executable or clearly marked as scenarios, meaningful, non-duplicative, and aligned with risk. Cover normal flows, boundaries, invalid inputs, permissions, failures, compatibility, concurrency, and non-functional requirements where applicable. For generated code, verify the framework, imports, setup, fixtures, and assertions match the repository context.\n\n## Handoff\n\nHand off to TDD development with generated test cases, test code drafts, target framework, expected failing/passing behavior, setup requirements, and validation commands. Hand off to integration verification when the work needs end-to-end, contract, regression, or release-readiness evidence.\n\n## Operating constraints\n\nUse bare skill names. Use only this plugin's local `skills/`, `templates/`, `rules/`, and examples unless the user provides external handoff documents or repository code. If modifying this plugin itself, read the complete `SKILL.md` for every skill being changed before editing it.\n",
    "contentMd5": "db5b17775d229f3e90eb2e24207652d8",
    "currentRevision": 1,
    "metadata": {
        "name": "cospowers-test-generation",
        "tags": [
            "cospowers",
            "ai-workers"
        ],
        "bundle": {
            "hook_events": [],
            "hooks_count": 0,
            "agents_count": 0,
            "skills_count": 10,
            "commands_count": 0,
            "mcp_server_names": [],
            "mcp_servers_count": 0,
            "skills_namespaces": [
                "cospowers-test-generation:acceptance-test-generation",
                "cospowers-test-generation:edge-case-test-generation",
                "cospowers-test-generation:regression-test-generation",
                "cospowers-test-generation:session-context",
                "cospowers-test-generation:test-case-generation",
                "cospowers-test-generation:test-code-generator",
                "cospowers-test-generation:test-coverage-review",
                "cospowers-test-generation:test-generation",
                "cospowers-test-generation:test-strategy-generation",
                "cospowers-test-generation:using-test-generation-plugin"
            ],
            "is_marketplace_repo": false
        },
        "install": {
            "method": "plugin_marketplace",
            "marketplace": "yhangf/csc-plugins",
            "plugin_name": "cospowers-test-generation",
            "marketplace_name": "ai-workers-test",
            "marketplace_repo": "yhangf/csc-plugins",
            "marketplace_verified": true
        },
        "category": "testing",
        "description": "AI 驱动的测试生成插件：从需求、设计、计划、bug 或代码生成测试策略、测试用例和测试代码草稿。"
    },
    "health": {
        "score": 100,
        "signals": {},
        "freshness_label": "active"
    },
    "evaluation": {},
    "sourcePath": "plugins/cospowers-test-generation/.plugin.json",
    "sourceSha": "4e12bf8d12615c4ce0227cfa11ef5cae3a514b7a2b1f135421c711badebef609",
    "sourceType": "direct",
    "source": "csc-plugins",
    "previewCount": 6,
    "installCount": 0,
    "favoriteCount": 0,
    "status": "active",
    "securityStatus": "unscanned",
    "lastScanId": "df7e485e-708e-40cb-848d-e26467d1ca4e",
    "createdBy": "system",
    "updatedBy": "usr_a04ab4ca-4cc7-47a2-a2b2-14fb0b456e92",
    "registry": {
        "id": "00000000-0000-0000-0000-000000000001",
        "name": "public",
        "description": "Default public registry — anyone can browse and contribute",
        "sourceType": "internal",
        "externalUrl": "",
        "externalBranch": "main",
        "syncEnabled": false,
        "syncInterval": 3600,
        "lastSyncedAt": "2026-06-04T13:17:59.259916Z",
        "lastSyncSha": "",
        "syncStatus": "idle",
        "syncConfig": {},
        "lastSyncLogId": null,
        "repoId": "public",
        "ownerId": "system",
        "createdAt": "2026-03-17T10:24:37.973671Z",
        "updatedAt": "2026-06-04T13:17:59.260075Z"
    },
    "createdAt": "2026-06-04T13:17:29.798141Z",
    "updatedAt": "2026-06-22T03:45:25.962152Z",
    "experienceScore": 100,
    "tags": [
        {
            "id": "ec30c3b9-67fd-4004-a75c-dd57b42ef7f0",
            "slug": "cospowers",
            "tagClass": "custom",
            "createdBy": "system",
            "createdAt": "2026-06-04T07:48:48.677287Z"
        },
        {
            "id": "792b2c81-2eb6-4ab0-b6dd-cbf8424b8fd1",
            "slug": "ai-workers",
            "tagClass": "custom",
            "createdBy": "system",
            "createdAt": "2026-06-04T07:48:48.678513Z"
        }
    ],
    "repoVisibility": "public",
    "repoName": "public",
    "favorited": false,
    "isBuiltIn": true,
    "currentVersionLabel": "v1",
    "forkCount": 0
}
```