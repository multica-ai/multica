# 7.3 笔记功能

## 范围

- 一级模块：辅助功能
- 二级能力：7.3 笔记功能
- 清单来源：`docs/功能列表清单.md:217-221`

## 对照清单

- 缺失：全局笔记
- 部分完成：任务关联笔记
- 缺失：笔记 Markdown 支持
- 缺失：笔记搜索

## 当前状态

- 状态：部分完成
- 完成度：1 / 4

## 证据

- `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`：番茄完成流程中的 note 字段
- `server/internal/handler/pomodoro.go`：接收这个可选 note

## 缺口

- 没有 note 实体、列表、阅读器、搜索、Markdown 编辑，也没有明确 issue note 面

## 推荐实现切片

- 先决定笔记应该属于辅助功能、issue 详情，还是后续知识管理工作流

## 交接说明

- 当前 note 支持只是一段番茄会话残片
