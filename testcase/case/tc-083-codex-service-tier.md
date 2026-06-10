# TC-083: Codex Service Tier 设置（OPE-2421）

## 关联信息

- **OPE 编号**: OPE-2421
- **Gitee PR**: !370
- **Commit SHA**: 40eef5da2
- **特性摘要**: 为 Codex runtime 增加 service tier 设置，支持在 Agent 配置中选择服务层级

## 涉及源文件

- `server/migrations/114_agent_service_tier.up.sql`
- `server/migrations/114_agent_service_tier.down.sql`
- `server/pkg/agent/codex.go`
- `server/internal/handler/agent.go`
- `server/internal/handler/agent_service_tier_test.go`
- `packages/core/types/agent.ts`
- `packages/views/agents/components/inspector/service-tier-picker.tsx`

## 验证要点

1. Agent 配置中可设置 Codex service tier，并持久化到数据库
2. service tier 设置正确传递到 Codex runtime 启动参数
3. service-tier-picker 组件在 Agent inspector 中正确渲染与切换
4. 迁移 114 可正常 up/down
5. 单元测试覆盖 service tier 读写
