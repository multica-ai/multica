# TC-212: Agent config template (OPE-3546)

## Associated Issues / PRs

- Issue: OPE-3546
- PR: !439 (Gitee)

## Feature Summary

Agent 配置模板化：系统级与个人级配置模板 CRUD、Agent 级模板绑定（跟随默认 / 跳过 / 指定模板）、claim 时三层合并（system → personal → agent），以及 Agents 页默认配置管理 UI。

## Affected Files

- `server/migrations/131_agent_config_template.up.sql`
- `server/migrations/132_agent_config_skip_flags.up.sql`
- `server/migrations/133_instructions_history_template_id.up.sql`
- `server/internal/handler/agent_config_template.go`
- `server/internal/handler/agent_config_merge.go`
- `server/internal/handler/daemon.go` (`resolveAgentConfig`)
- `packages/core/api/client.ts`
- `packages/views/agents/components/agents-page.tsx`
- `packages/views/agents/components/config-template-dialog.tsx`
- `packages/views/agents/components/defaults-detail.tsx`
- `packages/views/agents/components/template-selector.tsx`
- `packages/views/agents/components/agent-detail-inspector.tsx`

## Commit SHAs

- (maintainer fills on merge)
