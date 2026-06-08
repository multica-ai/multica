# 单能力 Tasks

## 实现目标

把 `/pomodoro` page 版改造成可在运行中建立专注上下文、在页内调整高频声音设置、并在 work phase 完成时自动带草稿记录的主工作台。

## 前置依赖

- 本 design 包中的 page-only 范围已经确认
- 不新增后端接口和 schema 的约束已经确认
- 运行中草稿采用本地组件状态，不做持久化，已确认

## 任务切片

### Task 1

- 目标：为 page 版 `PomodoroTimer` 建立运行中草稿状态
- 文件：
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`
- 改动：
  - 引入 `issue / note / labels` 草稿 state
  - 引入只读 `lastCompletedDraft` 摘要 state
  - 让 page 版 `Quick capture` 在运行中即可编辑
  - 定义 pause / reset / complete / failure 后的草稿与摘要规则
  - 明确新状态只放在 `variant === "page"` 分支，或抽 page 专属子组件隔离
- 完成定义：
  - 启动番茄后无需等待完成即可编辑草稿
  - reset 会清空草稿
  - 成功完成 work phase 后会清空活动草稿并保留完成摘要
  - compact 版行为保持不变
- 验证方式：
  - 组件测试覆盖运行中草稿编辑与清空时机

### Task 2

- 目标：把完成流程改成自动带草稿的非阻塞记录
- 文件：
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`
  - `apps/workspace/src/features/time-tracking/hooks/use-pomodoro.ts`
- 改动：
  - 让 work phase 完成直接读取草稿并调用 `fireComplete`
  - 保留完成后的轻量提示，不再依赖 `completionFlow` 作为必经路径
  - 定义完成失败时停留在 `00:00` work phase 完成态，并提供重试入口
- 完成定义：
  - 完成后无需先手动点 `Link Issue / Add Note / Skip`
  - `CompletePomodoroBody` 仍能正确带上草稿内容
  - 完成失败时草稿不丢
  - 完成失败时不会进入 break，且用户可显式重试
- 验证方式：
  - 组件测试覆盖带草稿完成与失败保留草稿

### Task 3

- 目标：用可搜索 issue picker 替换 `window.prompt`
- 文件：
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`
  - 如需抽取，可新增 `apps/workspace/src/features/time-tracking/components/PomodoroIssuePicker.tsx`
- 改动：
  - 复用 `PropertyPicker` 模式
  - 支持 issue 搜索、选择、清除
- 完成定义：
  - `window.prompt` 不再出现在 `PomodoroTimer`
  - 用户可以从 issue 列表中搜索并选择 issue
- 验证方式：
  - 组件测试覆盖搜索、选择、清除行为

### Task 4

- 目标：在 `/pomodoro` 页面增加轻量声音控制与当前轮次摘要
- 文件：
  - `apps/workspace/src/features/time-tracking/pages/PomodoroPage.tsx`
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`
  - 如需拆分，可新增轻量 page 子组件
- 改动：
  - 增加 `Tick Sound`、`White Noise`、`Volume` 面板
  - 运行中展示当前 issue、labels、note 填写状态摘要
  - break 阶段展示只读完成摘要，不提供已完成记录编辑入口
- 完成定义：
  - 用户在 `/pomodoro` 页面无需跳转即可调整高频声音配置
  - 页面能看到当前轮次是否已绑定 issue、是否已填写 note、是否有 labels
  - 页面不会暗示用户可以修改已完成记录
- 验证方式：
  - 页面组件测试
  - 手动验证声音设置会立即同步生效

### Task 5

- 目标：补齐回归验证，确保 page 优化不破坏现有番茄流程
- 文件：
  - `apps/workspace/src/features/time-tracking/components/PomodoroTimer.test.tsx`
  - 必要时新增 page 相关测试文件
  - 如有必要，补充 `e2e/pomodoro.spec.ts`
- 改动：
  - 增加运行中草稿、issue picker、自动完成记录、页内声音控制的测试
- 完成定义：
  - 番茄原有完成防重入测试继续通过
  - 新交互路径有覆盖
- 验证方式：
  - `pnpm --filter @multica/workspace exec vitest run ...`
  - 如范围允许，补充对应 E2E

## 执行顺序说明

1. 先做草稿状态模型，因为后面的 issue picker、自动完成、摘要展示都依赖它
2. 再做完成提交流程，确保草稿能真正进入 `CompletePomodoroBody`
3. 再替换 issue picker，避免先做 UI 后再返工状态结构
4. 然后补声音面板和摘要展示
5. 最后统一补测试和回归验证

## 回写要求

- 实现后更新本 change 下的 `research.md` 和 `design.md`，记录最终落地与偏差
- 如果执行阶段发现 compact 版也必须改，先更新 `design.md` 和 `tasks.md`，再继续实现
- 若新增了可复用子组件，要在文档中补充其职责和复用边界
