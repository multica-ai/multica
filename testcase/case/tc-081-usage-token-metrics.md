# TC-081: Usage Token 指标完善与排行榜缓存口径（OPE-2309）

## 关联信息

- **OPE 编号**: OPE-2309
- **Gitee PR**: !358, !379
- **Commit SHA**: 83fd6b5cd, 4b6bb4584
- **特性摘要**: 完善 Claude usage token 统计与 Token 排行榜缓存口径，并采纳 Usage Review 建议优化指标展示

## 涉及源文件

- `server/pkg/agent/claude_sdk_go.go`
- `server/pkg/agent/usage_parse.go`
- `server/pkg/agent/claude_test.go`
- `packages/views/dashboard/components/dashboard-page.tsx`
- `packages/views/dashboard/utils.ts`
- `packages/views/runtimes/utils.ts`

## 验证要点

1. Claude usage token 指标解析正确（input/output/cache 等口径）
2. Token 排行榜缓存口径与实际统计一致
3. Dashboard 正确展示 usage 指标
4. 单元测试覆盖 usage 解析与排行榜口径
