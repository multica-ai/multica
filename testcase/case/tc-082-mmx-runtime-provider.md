# TC-082: MiniMax CLI (mmx) daemon runtime provider（OPE-2318）

## 关联信息

- **OPE 编号**: OPE-2318
- **Gitee PR**: !359, !378
- **Commit SHA**: c451ce1eb, b0f4beab3, 61d455138
- **特性摘要**: 支持 MiniMax CLI（mmx）作为 daemon runtime provider，并提供 provider logo 与显示名

## 涉及源文件

- `server/pkg/agent/mmx.go`
- `server/pkg/agent/mmx_test.go`
- `server/pkg/agent/agent.go`
- `server/pkg/agent/capability.go`
- `server/internal/daemon/config.go`

## 验证要点

1. mmx 作为 runtime provider 可被 daemon 识别并启动
2. mmx 参数构建正确，且过滤被禁止的自定义参数
3. provider logo 与显示名（MiniMax）在前端正确展示
4. 单元测试覆盖 mmx 参数构建与过滤逻辑（含 TestBuildMmxArgsFiltersBlockedCustomArgs）
