# 单能力 Design

## 目标

- 明确当前 websocket 内部刷新与真正多端同步的边界，并把 5.4 固定为非当前阶段目标。

## 非目标

- 不在当前阶段实现 WebDAV、本地文件同步、离线优先。
- 不把 websocket invalidation 包装成“同步完成”。
- 不在没有本地状态模型的情况下设计具体冲突合并 UI。

## 当前架构基线

- 当前入口：  
  - `RealtimeProvider` 负责 websocket 连接。  
  - 仓库没有数据同步设置页或状态页。
- 当前核心逻辑：  
  - `WSEventType` 定义业务实体事件。  
  - `useTimeTrackingSync` 收到事件后失效缓存。
- 当前存储或状态：  
  - 搜索 `indexeddb|localforage|sync queue|conflict resolution|vector clock|offline first|CRDT` 未找到匹配，说明没有本地同步状态与冲突模型。

### 代码证据

- `apps/workspace/src/features/realtime/provider.tsx` `RealtimeProvider`：当前 websocket 的职责是“订阅服务端事件”。
- `apps/workspace/src/shared/types/events.ts` `WSEventType`：当前事件表达的是“服务端已发生的变化”。
- `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` `useTimeTrackingSync`：当前消费动作是刷新缓存，不是把本地待同步修改推送到其他端。
- 搜索 `data sync|data export|data import|backup|restore`：没有同步入口和状态 UI。

## 缺口定义

- 真正同步至少需要本地状态、副本版本、冲突策略、同步状态 UI、手动触发与恢复机制，而这些当前全部缺失。
- 当前 websocket 只解决“服务器事件通知”，不解决“离线改动如何排队、如何对账、如何冲突处理”。

## 方案与权衡

### 方案 A：把 websocket 扩成“同步”

- 做法：继续沿用 websocket + invalidation，并在文案中叫做 sync。
- 优点：短期看起来最省事。
- 风险：会误导产品与工程判断，让用户以为支持离线/冲突/多设备一致性。

### 方案 B：明确分阶段，当前只保留实时刷新，未来再立真正同步项目

- 做法：当前阶段把 websocket 定义为“内部实时刷新”；若未来需要多端同步，单独立项并先补本地状态与冲突模型。
- 优点：阶段边界清晰，不会把现有架构过度承诺成同步能力。
- 风险：5.4 在当前阶段没有可交付代码，只能先冻结设计前提。

## 推荐方案

- 推荐方案 B。
- 当前阶段定义：  
  - `RealtimeProvider` + 各 feature sync hook = 内部实时刷新。  
  - 这不是多端同步。  
- 未来真正 5.4 的前置条件：  
  1. 本地副本存储；  
  2. 同步队列；  
  3. 版本向量或等价冲突模型；  
  4. 同步状态 UI；  
  5. 手动同步/重试/冲突处理入口。  

## 数据模型或状态模型

- 当前只有 `socket connected / disconnected` 状态。
- 未来真正同步至少需要：
  - `local_revision`
  - `remote_revision`
  - `sync_queue[]`
  - `conflicts[]`
  - `last_sync_at`
- 这些模型当前都不存在，因此 5.4 不能进入实现。

## 接口契约

### 当前阶段

- websocket：只负责事件通知与缓存刷新。

### 未来阶段前提

- `sync/status`
- `sync/pull`
- `sync/push`
- `sync/resolve-conflict`

> 以上仅为未来阶段占位契约，不属于当前实现范围。

## UI 或交互流程

1. 当前阶段：用户操作后，其他页面/标签页收到 websocket 事件并刷新。
2. 未来真正同步：用户会看到同步状态、待同步条目、冲突提示，并可手动触发同步。

### 页面交互流

```text
[当前端 A 写入]
      |
      v
[服务端持久化]
      |
      v
[websocket 广播]
      |
      v
[端 B 刷新缓存]
```

### 状态机

```text
当前阶段:
[connected] <--> [disconnected]

未来同步阶段:
[idle] -> [syncing] -> [in-sync]
              |
              +--> [conflicted]
              |
              +--> [failed]
```

### 数据变化流

```text
当前阶段:
[server event] -> [websocket] -> [query invalidation]

未来同步阶段:
[local change] -> [sync queue] -> [push/pull] -> [conflict resolution] -> [in-sync]
```

## 权限、边界条件、异常路径

- 谁可以使用  
  - 当前 websocket 仍受服务端鉴权控制。
- 哪些输入非法  
  - 当前阶段不存在手动 sync 输入，因此不存在相关 UI 校验。
- 失败时如何处理  
  - 断线只会影响实时刷新，不会留下本地待同步数据。

## 实现约束

- 当前阶段不得把 websocket 文案升级成“同步成功/同步失败”。
- 若未来正式启动 5.4，必须先更新 module overview 的阶段判断。
- 真正同步开始前，不能跳过本地状态与冲突模型设计。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 把实时刷新误写成同步 | 误导路线图 | 文档中明确二者不是一回事 |
| 没有本地状态就做 WebDAV/文件同步 | 架构不成立 | 将 5.4 定义为未来独立项目 |
| 断线重连被误解为“同步恢复” | 用户预期错误 | 保持当前术语为 reconnect / refresh，而非 sync |

## 验收检查

1. 文档明确指出 websocket 内部刷新不等于真正多端同步。
2. 5.4 被标注为非当前阶段目标。
3. 未来若启动 5.4，执行 Agent 可以直接按“前置条件清单”判断是否具备准入条件。
