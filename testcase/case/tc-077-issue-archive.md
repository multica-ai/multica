# TC-077: Issue 归档功能（OPE-1180）

## 关联信息

- **OPE 编号**: OPE-1180
- **Gitee PR**: !362
- **Commit SHA**: 075b86a95, a6044fdb9, 65a83060a, 0d56f9a42, 2f6c8d778, af20b1b98, 831478bd8, 9356214dd
- **特性摘要**: Issue 归档/取消归档功能，含自动归档、两段式搜索、列表与看板的 showArchived 过滤

## 涉及源文件

- `server/cmd/server/issue_auto_archive.go`
- `server/internal/handler/issue.go`
- `server/migrations/040_issue_archive.up.sql`
- `server/migrations/040_issue_archive.down.sql`
- `server/pkg/db/queries/issue.sql`
- `packages/views/issues/components/issues-page.tsx`
- `packages/views/issues/components/board-card.tsx`
- `packages/views/modals/archive-issue-confirm.tsx`
- `packages/views/projects/components/project-detail.tsx`

## 验证要点

1. 可手动归档单个 Issue，并能取消归档
2. 默认列表/看板不展示已归档 Issue，开启 showArchived 后可见
3. 在 project issues 页面同样支持 showArchived 过滤
4. 切换 showArchived 时列表与看板不出现闪烁
5. 自动归档任务按规则将符合条件的 Issue 归档
6. 搜索可命中已归档 Issue（两段式搜索）
