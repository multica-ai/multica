## 1. OpenSpec 与范围收敛

- [x] 1.1 创建并对齐本 change 的 proposal / design / spec / tasks 工件。

## 2. 后端：父子任务、标签、依赖 API

- [x] 2.1 扩展 issue create / update 的父任务校验，支持 `parent_issue_id` 更新、防自引用、防环、同 workspace 校验。
- [x] 2.2 增加 workspace labels 与 issue labels 的查询、创建、关联、移除能力。
- [x] 2.3 增加 issue dependencies 的查询、创建、移除能力，并按当前 issue 视角归一化返回 `blocks` / `blocked_by` / `related`。
- [x] 2.4 扩展 issue detail / child issue 读取能力，为前端提供父任务、子任务、标签、依赖关系所需数据。
- [x] 2.5 补充必要事件负载与 handler 测试。

## 3. apps/workspace：主产品体验

- [x] 3.1 扩展 shared types、query keys、API client、React Query 查询与 mutation 辅助。
- [x] 3.2 在 issue 创建 / 编辑流程中增加父任务选择能力。
- [x] 3.3 在 issue 详情中展示父任务、子任务、标签、依赖阻塞关系，并支持增删改。
- [x] 3.4 补充 `apps/workspace` 单元测试。

## 4. 验证

- [x] 4.1 补充 E2E，证明用户可通过主 UI 创建父子任务、打标签、建立依赖关系。
- [x] 4.2 运行相关后端、前端、E2E 验证并记录证据。
