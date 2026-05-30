# 单能力 Spec

## 背景

- 证据：`docs/功能列表清单.md` `5.4 多端同步`；结论：该能力要求多设备同步、WebDAV、本地文件同步、手动触发同步、同步状态显示。
- 证据：`apps/workspace/src/features/realtime/provider.tsx` `RealtimeProvider` 与 `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`；结论：仓库已有 websocket 实时刷新，但这只覆盖“服务端数据变化后前端刷新”，不等于真正多端同步。

## 范围

- 本次覆盖：当前 websocket 刷新边界、真正多端同步所需前提、阶段判断。
- 本次不覆盖：当前阶段实现 WebDAV、本地文件桥接、离线优先架构。

## 当前状态

- 证据：`RealtimeProvider`；结论：已有 websocket 连接与订阅。
- 证据：`useTimeTrackingSync`；结论：当前只做 query invalidation。
- 证据：代码搜索 `indexeddb|localforage|sync queue|conflict resolution|vector clock|offline first|CRDT|多端同步|离线优先|冲突解决`；结论：未找到匹配，说明真正同步基础设施缺失。

## 证据

- `RealtimeProvider`：内部实时事件连接。
- `WSEventType`：实体变更事件类型。
- `useTimeTrackingSync`：事件消费逻辑是前端刷新，不是双向同步。
- 搜索 `data sync|...|backup|restore`：没有同步设置或状态入口。

## 缺口

1. 没有本地状态与同步队列。
2. 没有冲突模型与人工处理入口。
3. 没有 WebDAV/本地文件同步能力。
4. 没有同步状态 UI。

## 交接说明

- 5.4 被标记为非当前阶段目标。
- websocket 内部刷新不能算作该能力完成。
- 若未来要启动 5.4，必须先升级产品阶段并补本地状态/冲突模型设计，再谈实现。
