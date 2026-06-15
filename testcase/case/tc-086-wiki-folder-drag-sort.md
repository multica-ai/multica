# TC-086: 知识库一级文件夹与拖动排序（OPE-2661）

## 关联信息

- **OPE 编号**: OPE-2661
- **Gitee PR**: !384
- **Commit SHA**: f30c8b7d0, 8b9474e07
- **特性摘要**: 知识库 Wiki 支持一级文件夹（page/folder 类型），可通过 dnd-kit 拖拽排序、将页面拖入/拖出文件夹，并支持把页面移出文件夹回到顶层（清空 parent_id）

## 涉及源文件

- `packages/views/wiki/components/wiki-page.tsx`
- `packages/core/wiki/tree.ts`
- `packages/core/types/wiki.ts`
- `packages/core/types/index.ts`
- `server/internal/handler/wiki_page.go`
- `server/pkg/db/queries/wiki_page.sql`
- `server/pkg/db/generated/wiki_page.sql.go`
- `server/migrations/117_wiki_page_type.up.sql`
- `server/migrations/117_wiki_page_type.down.sql`

## 验证要点

1. 可创建文件夹（type=folder）并显示文件夹图标；文件夹不可嵌套（服务端校验拦截）
2. 通过拖拽对页面/文件夹排序，拖拽时有放置区高亮与拖拽预览反馈
3. 可将页面拖入文件夹，也可拖出文件夹回到顶层；非法放置时页面回弹
4. 将页面移出文件夹时 parent_id 被正确清空（clear_parent_id=true，避免 COALESCE 保留旧值）
5. UpdateWikiPage / ReorderWikiPage 接口正确处理 parent_id 设置与清空
6. 迁移 117_wiki_page_type 可正常 up/down
