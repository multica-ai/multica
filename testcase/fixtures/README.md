# Multica Regression Fixtures

This document describes reusable test data setup for browser regression cases. Keep project-specific fixture recipes here instead of embedding them in individual Tester agent instructions.

## Local profile daemon

For local/self-host regression, use the isolated `local` profile:

```bash
multica --profile local daemon status --output json
multica --profile local daemon restart
```

Restart the daemon before marking a runtime-dependent case BLOCKED when the only blocker is missing/offline local runtime.

## Agent task run fixture

Use this fixture for cases that require execution logs, retry actions, agent comments, plan mode, task failure handling, or notification delivery.

### Preconditions

- A test workspace is available.
- A test issue exists or can be created.
- A test agent exists and is backed by an online runtime.

### Create a task run

1. Create or choose a disposable issue.
2. Add a markdown mention comment. Plain `@agent_name` is not enough.

```markdown
[@agent_name](mention://agent/<agent_uuid>) 请回复一句简短测试消息，用于生成回归测试 task run。
```

3. Wait until the task completes.
4. Verify with CLI when needed:

```bash
multica issue runs <issue_id_or_identifier> --output json --full-id
multica issue comment list <issue_id_or_identifier> --output json
```

### Create multiple runs

Some cases, especially TC-018, require multiple runs. Use one or more of:

```bash
multica issue rerun <issue_id_or_identifier> --output json
```

or add additional mention comments, optionally with different test agents.

### Create failed runs

For cases that need failure states, prefer a dedicated test agent or harmless prompt/runtime setup that reliably fails without destructive side effects. Record the exact failure mechanism in the report.

Do not damage shared agents, credentials, workspaces, or runtime configuration just to create a failure.

## Multi-user permission fixture

Permission cases such as TC-011 require distinct identities:

- agent owner
- workspace admin / owner who is not the agent owner
- regular member who is not the agent owner

If only one auth state is available, browser verification of non-owner behavior is BLOCKED. Static code review can be included as supporting evidence, but it does not convert the browser case to PASS unless the user explicitly accepts that limitation.

## Notification fixture

Notification cases may require external bindings such as WeChat, DingTalk, email, or custom webhook endpoints. If real delivery cannot be verified, execute all visible settings/UI assertions that are reachable and mark delivery-only checks BLOCKED with the missing binding/runtime/channel.

## Safety

- Do not use production customer data for fixture creation.
- Do not mutate agents/runtimes owned by unrelated or unclear users.
- Do not delete Docker volumes, databases, images, or local profile state unless explicitly asked.
- Attach reports/screenshots to the Multica Issue when the task runs on Multica.
