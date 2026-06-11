# TC-080: 描述并发编辑冲突检测（OPE-2294）

## 关联信息

- **OPE 编号**: OPE-2294
- **Gitee PR**: !367
- **Commit SHA**: 9f4d92f8f, c16084053
- **特性摘要**: Issue 描述并发编辑时检测冲突，并在编辑器同步前保留冲突基线

## 涉及源文件

- `server/internal/handler/issue.go`
- `server/internal/handler/handler_test.go`
- `packages/core/issues/mutations.ts`
- `packages/core/types/api.ts`
- `packages/views/editor/content-editor.tsx`
- `packages/views/issues/components/issue-detail.tsx`

## 验证要点

1. 两个会话同时编辑同一 Issue 描述时，后提交者收到冲突提示
2. 冲突基线在编辑器完成同步前被保留，不被覆盖
3. 无并发时正常保存不触发冲突逻辑
4. 单元测试覆盖并发编辑冲突检测场景
