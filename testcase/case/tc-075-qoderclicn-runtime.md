# TC-075: Qoderclicn Runtime 支持

## 关联信息

- **OPE 编号**: 无（内部特性）
- **Commit SHA**: 270af9a86
- **特性摘要**: 新增 Qoderclicn 作为一等 runtime provider，包含前后端完整支持

## 涉及源文件

- `server/pkg/agent/qoderclicn.go`
- `server/pkg/agent/qoderclicn_test.go`
- `server/pkg/agent/agent.go`
- `server/pkg/agent/models.go`
- `server/pkg/agent/models_test.go`
- `server/pkg/agent/capability.go`
- `server/internal/daemon/config.go`
- `server/internal/daemon/config_test.go`
- `packages/core/agents/mcp-support.ts`
- `packages/views/runtimes/components/provider-logo.tsx`
- `packages/views/runtimes/components/runtime-detail.tsx`
- `packages/views/runtimes/components/shared.tsx`

## 验证要点

1. Runtime 列表中出现 Qoderclicn provider 及其 logo
2. Runtime 详情页正确展示 Qoderclicn 配置
3. Daemon 配置正确识别和加载 Qoderclicn runtime
4. Agent 能力模型正确注册 Qoderclicn 支持
5. 单元测试全部通过
