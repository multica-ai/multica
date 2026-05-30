# 单能力 Design

## 目标

- 明确 7.3 当前为何应降级为低优先级能力。
- 固定番茄 note 到独立笔记域的迁移边界，避免未来破坏时间记录事实源。

## 非目标

- 不在当前阶段实现完整独立笔记系统。
- 不把 `time_entry.description` 直接重命名成 note 表示“问题已解决”。
- 不承诺 Markdown、全文检索、标签等知识管理能力。

## 当前架构基线

- 当前入口：无 notes 路由。
- 当前核心逻辑：番茄完成流程可提交 `note`。
- 当前存储或状态：`time_entry.description` 是唯一持久化落点。
- 当前 UI 或接口：只有番茄完成弹层输入框，没有 notes 页面或 API。

### 代码证据

- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` `noteInputValue`：说明番茄 note 已有输入残片。
- `apps/workspace/src/shared/types/pomodoro.ts` `CompletePomodoroBody`：说明 note 已进入请求模型。
- `server/internal/handler/pomodoro.go` `completePomodoro`：说明 note 当前写入时间记录描述。
- `server/pkg/db/generated/models.go` `TimeEntry`：说明尚未存在 note domain。

## 缺口定义

- 7.3 当前缺的不是“一个 notes 页面”，而是独立 note 实体、来源标识、查询入口和迁移规则。
- 如果直接把现有时间记录描述暴露成 notes 列表，会混淆工作日志与笔记语义。

## 方案与权衡

### 方案 A：直接把 `time_entry.description` 当成笔记列表来源

- 做法：基于时间记录描述做 notes 页面。
- 优点：利用现有数据最快。
- 风险：工作日志与笔记语义混淆，历史默认文案会污染笔记域。

### 方案 B：建立独立 note 域，但当前阶段只保留迁移边界，推荐

- 做法：未来引入 `note` 实体，番茄 note 作为一种 source；当前阶段先把迁移规则写清，不进入实现。
- 优点：既保留现有残片价值，也不破坏时间记录审计链路。
- 风险：短期无法交付独立笔记功能，但边界清晰。

### 方案 C：永久维持番茄 note 为时间记录描述的一部分

- 做法：彻底放弃独立笔记域。
- 优点：最省事。
- 风险：会锁死未来工作笔记能力，且与清单目标不一致。

## 推荐方案

选择方案 B。

7.3 当前应标为 **P2 / 低优先级，非当前阶段实现目标**。唯一现成起点是番茄 note 残片，但它只能作为未来 note 域的一个输入来源，而不是独立笔记域本身。

## 数据模型或状态模型

未来独立 note 域建议最小模型：

```text
note
├─ id
├─ source_type(pomodoro | issue | manual)
├─ source_ref
├─ workspace_id
├─ author_id
├─ content
└─ created_at
```

### 迁移边界

1. 在独立 note 域实现前，番茄 note 的事实源仍是 `time_entry.description`。
2. 未来迁移时，只回填 **用户显式输入的番茄 note**；默认文案 `"Pomodoro 专注"` 不应迁移成独立 note。
3. 回填后的独立 note 必须保留 `source_type = pomodoro` 与原始 `time_entry_id` 反向引用。
4. 迁移后不删除原 `time_entry.description`，它仍作为工作日志审计字段保留。

## 接口契约

### 输入

- 当前阶段只有番茄完成接口输入 `note`。
- 未来独立 note 域若重启，应新增 note CRUD / list / source linkage 接口。

### 输出

- 当前阶段输出仍是时间记录描述。
- 未来输出应区分“工作日志描述”和“独立 note 实体”。

## UI 或交互流程

### 页面交互流

```text
Pomodoro complete dialog
  -> 输入 note
  -> completePomodoro(note)
  -> 写入 time_entry.description

未来若重启 notes：
  -> 从 pomodoro source 创建独立 note
  -> Notes list / detail 展示
```

### 状态机

```text
[pomodoro-note-only]
   -> [migration-rules-defined]
   -> [note-domain-approved]
   -> [dual-write-or-backfill]
```

### 数据变化流

```text
PomodoroTimer.noteInputValue
   -> CompletePomodoroBody.note
   -> server completePomodoro
   -> time_entry.description
   -> (future backfill) note.source_type = pomodoro
```

## 权限、边界条件、异常路径

- 谁可以使用：当前只有番茄完成流程能写 note 残片。
- 哪些输入非法：空白 note、仅默认文案、不带 source backlink 的迁移记录。
- 失败时如何处理：未来回填失败时，必须保留原时间记录描述，不得丢失历史内容。

## 实现约束

- 不要把默认文案 `"Pomodoro 专注"` 当成真实笔记迁移。
- 不要在没有 note domain 的情况下宣称“笔记功能已完成”。
- 若未来进入实现，必须先处理 source backlink 与审计保留。

## 风险与对策

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 把工作日志直接当笔记 | 数据语义混乱 | 独立 note 域必须保留 source_type/source_ref |
| 迁移时丢失历史描述 | 审计链路断裂 | 迁移后保留 `time_entry.description` 原值 |
| 过早扩到 Markdown / 搜索 | 范围失控 | 当前阶段只写迁移边界，不扩知识管理能力 |

## 验收检查

1. 7.3 被明确标为低优先级、非当前阶段实现目标。
2. 文档写清番茄 note 当前事实源、未来 note 域最小模型与迁移边界。
3. 明确默认 `"Pomodoro 专注"` 不参与独立 note 回填。
4. 不出现超出当前范围的 Markdown / 检索 / 标签承诺。
