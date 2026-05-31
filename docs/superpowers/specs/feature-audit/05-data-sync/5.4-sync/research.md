# 单能力 Research

## 调研目标

- 确认仓库里已有的 websocket 实时刷新到底覆盖到什么程度。
- 明确 websocket 内部刷新为什么不等于真正多端同步。
- 判断 5.4 是否属于当前产品阶段目标。

## 现状链路

1. 入口  
   - 证据：`apps/workspace/src/features/realtime/provider.tsx` `RealtimeProvider`；结论：前端已有统一 websocket provider。
2. 数据流  
   - 证据：`apps/workspace/src/shared/types/events.ts` `WSEventType`；结论：当前事件类型覆盖 issue、time_entry、pomodoro 等业务实体变更。
   - 证据：`apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`；结论：收到 websocket 事件后，前端主要做 query invalidation 与局部状态刷新。
3. 缺失部分  
   - 证据：代码搜索 `apps/`、`server/`，关键词 `indexeddb|localforage|sync queue|conflict resolution|vector clock|offline first|CRDT|多端同步|离线优先|冲突解决`；结论：未找到匹配，说明仓库没有本地缓存、冲突解决、离线队列等真正多端同步基础设施。
   - 证据：代码搜索 `apps/workspace/src/router.tsx`、`apps/workspace/src/features/layout/navigation.ts`、`apps/workspace/src/features/settings`，关键词 `data sync|data export|data import|backup|restore`；结论：未找到匹配，说明也没有同步状态页或同步设置入口。

## 关键代码证据

| 路径 | 符号 | 结论 |
| --- | --- | --- |
| `apps/workspace/src/features/realtime/provider.tsx` | `RealtimeProvider` | 当前 websocket 是“连接与订阅”层。 |
| `apps/workspace/src/shared/types/events.ts` | `WSEventType` | 当前实时事件表达的是服务端业务变更。 |
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` | `useTimeTrackingSync` | 当前处理方式是 query invalidation，不是双向同步协议。 |
| 代码搜索 `apps/`、`server/` | `rg(indexeddb|localforage|sync queue|conflict resolution|vector clock|offline first|CRDT|多端同步|离线优先|冲突解决)` | 未找到匹配，说明缺少离线/冲突基础设施。 |
| 代码搜索 `router/navigation/settings` | `rg(data sync|data export|data import|backup|restore)` | 未找到匹配，说明没有同步状态入口。 |

## 数据模型或状态流

- 当前状态模型  
  - 证据：`RealtimeProvider`；结论：前端只维护 socket 连接状态和消息订阅。
- 当前变化模型  
  - 证据：`useTimeTrackingSync`；结论：收到事件后触发缓存失效或本地 store 微调。
- 缺少的模型  
  - 证据：搜索 `sync queue|conflict resolution|vector clock` 未找到匹配；结论：当前没有同步队列、版本向量、冲突状态模型。

## 边界条件

- 权限边界  
  - 当前 websocket 仍依赖服务端鉴权，并以服务端为单一事实源。
- 空状态  
  - websocket 断开时最多是实时刷新丢失，不存在本地离线待同步状态。
- 错误路径  
  - 断线后重新连接只恢复订阅，不处理未同步本地修改。

## 未决问题

- 若未来启动真正多端同步，是走本地优先还是文件桥接；该项超出当前阶段，在 `design.md` 中被明确降为未来前置课题。
