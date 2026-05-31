# 1.4 任务筛选与排序

## 范围

- 一级模块：任务管理
- 二级能力：1.4 任务筛选与排序
- 清单来源：`docs/功能列表清单.md:34-44`

## 对照清单

- 已完成：按项目筛选
- 已完成：按标签筛选
- 已完成：按优先级筛选
- 已完成：按完成状态筛选
- 已完成：按截止日期筛选
- 已完成：按创建时间排序
- 缺失：按修改时间排序
- 已完成：按优先级排序
- 已完成：按截止日期排序

## 当前状态

- 状态：部分完成
- 完成度：8 / 9

## 证据

- `apps/workspace/src/features/issues/stores/view-store.ts`：筛选和排序状态
- `apps/workspace/src/features/issues/utils/filter.ts`：筛选逻辑
- `apps/workspace/src/features/issues/utils/sort.ts`：已支持的排序策略

## 缺口

- 没有确认存在 `updated_at` 排序策略

## 推荐实现切片

- 先确认 issue 列表响应是否稳定提供更新时间，再补这一项

## 交接说明

- 标签筛选能力后续还应结合主 issue list UI 再验证一次
