# 2.1 工时追踪

## 范围

- 一级模块：时间管理
- 二级能力：2.1 工时追踪
- 清单来源：`docs/功能列表清单.md:72-80`

## 对照清单

- 已完成：手动开始/停止计时
- 已完成：暂停/继续计时
- 缺失：空闲自动暂停计时
- 已完成：手动添加工时记录
- 已完成：编辑已有工时记录
- 已完成：删除工时记录
- 已完成：工时记录关联任务
- 已完成：跨任务切换计时

## 当前状态

- 状态：部分完成
- 完成度：7 / 8

## 证据

- `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`：开始、停止、切换任务
- `apps/workspace/src/features/time-tracking/components/TimeEntryCreateSheet.tsx`：手动创建
- `apps/workspace/src/features/time-tracking/components/TimeEntryEditSheet.tsx`：编辑
- `apps/workspace/src/features/time-tracking/components/TimeEntryDeleteDialog.tsx`：删除

## 缺口

- 没有 idle detection 和 auto-pause 路径

## 推荐实现切片

- 先梳理当前 timer ownership，再插入 idle monitor

## 交接说明

- “暂停/继续”当前部分证据来自 pomodoro 相关链路，后续如果要求纯手动 timer pause，需要再核查
