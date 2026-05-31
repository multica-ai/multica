# 2.3 休息与提醒执行任务

## 1. 实现目标

- 建立统一 reminder registry，覆盖休息提醒、截止提醒和本地通知投递。

## 2. 前置依赖

- 2.1 提供 running timer 生命周期与 auto-paused 事件。
- 2.2 番茄钟继续作为提醒源之一。

## 3. 任务切片

### 切片 A：抽统一 reminder registry

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/`
- 完成定义：
  - running timer、pomodoro、deadline 都可注册提醒源。
  - registry 支持 armed/fired/dismissed/snoozed 状态。
- 验证方式：
  - 单测覆盖多 source 注册与去重。

### 切片 B：接本地投递层

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/hooks/use-sound-system.ts`
  - 时间管理相关组件目录
- 完成定义：
  - 声音、桌面通知、toast 三种投递方式可组合降级。
- 验证方式：
  - 手动验证权限允许/拒绝两条路径。

### 切片 C：补设置与截止提醒查询

- 目标文件 / 目录：
  - `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
  - `apps/workspace/src/features/settings/components/notifications-tab.tsx` 或时间管理设置入口
  - issue 查询目录
- 完成定义：
  - 用户可配置休息间隔和截止提前量。
  - deadline source 可从 issue query 获得待提醒数据。
- 验证方式：
  - 联动验证计时提醒、pomodoro 提醒、截止提醒。

## 4. 回写要求

1. 若后续接入 ntfy 推送，先更新本设计包与 `overview.md`。
2. 实现完成后回写 `spec.md` 的完成状态。
