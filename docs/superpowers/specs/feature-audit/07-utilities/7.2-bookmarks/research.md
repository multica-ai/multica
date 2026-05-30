# 单能力 Research

## 调研目标

1. 确认仓库里是否已有书签相关实现。
2. 判断 7.2 是独立收藏能力，还是应收敛为已有对象的“保存视图 / 收藏对象”。
3. 为低优先级判断提供证据。

## 现状链路

1. 入口：`apps/workspace/src/router.tsx` 没有 bookmarks 路由。
2. 搜索结果：前后端均未找到 bookmark 相关实体、接口或页面。
3. 相关起点：`apps/workspace/src/features/issues/stores/view-store.ts` 会本地持久化 issue 视图筛选，是最接近“保存视图”的残片。
4. 输出结果：7.2 目前仍属于产品边界假设，但若未来重启，最合理起点不是通用收藏夹，而是围绕现有对象的个人保存视图/书签。

## 关键证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/router.tsx` | `routeTree` | 当前没有 bookmarks 页面或导航入口。 |
| `apps/workspace/src/features/issues/stores/view-store.ts` | `useIssueViewStore` | 当前最接近书签的能力是 issue 视图筛选的本地持久化。 |
| `product-overview.md` | `MMF-2` | 知识管理与二次整理不是当前阶段主轴，因此通用书签域应降级。 |

## 空搜索证据

| 路径 | 符号 / 搜索关键词 | 结论 |
| --- | --- | --- |
| `apps/workspace/src`、`server` | `rg(bookmark|bookmarks)` | 未找到匹配，说明没有书签实体、API 或页面。 |
| `apps/workspace/src`、`server` | `rg(书签|收藏夹|保存视图)` | 未找到匹配，说明中文命名路径下也无实现残片。 |

## 数据模型或状态流

- 当前没有 bookmark model。
- `useIssueViewStore` 只保存个人视图筛选状态，不能表达可命名、可分享、可排序的书签对象。

## 边界条件

- 证据：`apps/workspace/src/features/issues/stores/view-store.ts` `persist`；结论：现有“保存”能力停留在本地视图状态，不等于独立书签域。
- 证据：`product-overview.md` `MMF-2`；结论：若未来重启，7.2 也应优先服务当前工作流对象，而不是扩展成知识管理中心。

## 未决问题

1. 书签目标对象是 issue 视图、项目、仓库，还是外部 URL；当前没有产品定义。
2. 是否允许 workspace 共享书签；现有最接近的证据只支持用户个人本地保存。
3. 是否需要搜索与分组；当前没有对象模型，不应由执行 Agent 自行补完。
