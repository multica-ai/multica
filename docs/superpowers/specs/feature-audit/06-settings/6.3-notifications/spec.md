# 6.3 通知设置

## 范围

- 一级模块：系统设置与个性化
- 二级能力：6.3 通知设置
- 清单来源：`docs/功能列表清单.md:190-194`

## 对照清单

- 缺失：桌面通知开关
- 缺失：声音通知开关
- 缺失：提醒声音选择
- 缺失：通知时长设置

## 当前状态

- 状态：部分完成
- 完成度：0 / 4 个对齐条目

## 证据

- `apps/workspace/src/features/settings/components/notifications-tab.tsx`：已有 ntfy URL、token 和 disabled types 配置
- `apps/workspace/src/shared/types/notification-preference.ts`：已有当前通知偏好 schema

## 缺口

- 现有通知设置解决的是 push 通道偏好，不是清单要求的桌面/声音/时长设置

## 推荐实现切片

- 先明确桌面和声音控制是浏览器本地偏好，还是账号级偏好

## 交接说明

- 不能因为已有 ntfy 配置，就把这一节判成 已完成
