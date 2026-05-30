# 1.1 基础任务操作

## 范围

- 一级模块：任务管理
- 二级能力：1.1 基础任务操作
- 清单来源：`docs/功能列表清单.md:5-12`

## 对照清单

- 已完成：新建任务
- 已完成：编辑任务
- 已完成：删除任务
- 已完成：标记完成/未完成
- 缺失：归档任务
- 缺失：恢复已归档任务
- 缺失：永久删除已归档任务

## 当前状态

- 状态：部分完成
- 完成度：4 / 6

## 证据

- `apps/workspace/src/features/issues/mutations.ts`：已有 create、update、delete issue mutations
- `apps/workspace/src/features/issues/components/issue-detail.tsx`：状态切换支持完成/未完成
- `apps/workspace/src/features/issues/components/batch-action-toolbar.tsx`：批量状态更新和删除

## 缺口

- 当前 issue 流程中没有归档状态或归档专属生命周期
- 没有已归档任务的恢复和永久删除路径

## 推荐实现切片

- 先决定归档是独立状态，还是删除链路的一部分

## 交接说明

- 删除已实现，但不能视为归档支持
