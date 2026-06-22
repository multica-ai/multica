# TC-085: Issue/评论 Markdown 预览与导出（OPE-2658）

## 关联信息

- **OPE 编号**: OPE-2658
- **Gitee PR**: !383
- **Commit SHA**: 778b0a26c, c454a0bb8, 2328b06fe
- **特性摘要**: 为 Issue 与评论提供 Markdown 预览（居中模态窗口，max-w-6xl）以及基于 HTML 的导出/PDF 能力，使导出效果与预览一致

## 涉及源文件

- `packages/views/issues/components/markdown-preview-drawer.tsx`
- `packages/views/issues/components/comment-card.tsx`
- `packages/views/issues/components/issue-detail.tsx`
- `packages/views/issues/components/index.ts`
- `packages/core/api/client.ts`
- `server/internal/handler/issue_export.go`
- `server/internal/handler/issue_export_test.go`
- `server/cmd/server/router.go`

## 验证要点

1. 在 Issue 详情页可打开 Markdown 预览，呈现为居中模态窗口（宽度 max-w-6xl），渲染样式与导出一致
2. 在评论卡片上可触发 Markdown 预览，正确渲染评论正文
3. 导出接口可生成与预览一致的 HTML/PDF 文档，内容与样式匹配
4. 导出后端路由已在 router 注册，权限与 Issue 访问控制一致
5. 单元测试覆盖导出 handler（issue_export_test.go）的正常与边界场景
