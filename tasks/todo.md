# TODO

- [x] 阶段一：复核 lessons、既有 OpenSpec 蓝图与第一批范围相关代码现状
- [x] 阶段二：确认第一批实现范围为父子任务 UI、标签、依赖阻塞关系可视化
- [x] 阶段三：补齐 `openspec/changes/issue-hierarchy-labels-dependencies/` 的 proposal / design / spec / tasks
- [x] 阶段四：实现后端父子任务、标签、依赖关系 API、校验与事件补充
- [x] 阶段五：实现 `apps/workspace` 的父子任务、标签、依赖关系 UI
- [x] 阶段六：镜像 `apps/web` 的相关 issue 能力，避免双前端漂移
- [x] 阶段七：补充后端、前端与 E2E 测试覆盖
- [x] 阶段八：完成验证并记录可验证证据

## Review

- 已确认本轮只做第一批切片：父子任务 UI、标签 / Tag、依赖阻塞关系可视化。
- `openspec` CLI 当前在本地环境不可用，因此本轮将按仓库既有 OpenSpec 目录结构手工补齐 change 工件，并保持 proposal / design / spec / tasks 对齐。
- 已落地 `openspec/changes/issue-hierarchy-labels-dependencies/`，补齐 proposal、design、tasks 与 delta spec。
- 后端已新增 workspace labels API、issue labels API、issue dependencies API，并在 issue detail 中返回父任务、子任务、标签、依赖关系。
- `apps/workspace` 与 `apps/web` 均已补齐父任务选择器、标签编辑器、依赖关系编辑器与详情展示。
- 已补充后端 handler 测试、workspace mutation 测试、E2E 主流程测试。
- 验证证据：
  - `make setup-worktree`：成功安装依赖并完成 `.env.worktree` 环境初始化。
  - `pnpm typecheck`：通过。
  - `pnpm --filter @multica/workspace exec vitest run src/features/issues/mutations.test.tsx`：3 个测试通过。
  - `cd server && export $(grep '^DATABASE_URL=' ../.env.worktree) && go test ./internal/handler/... ./cmd/server/...`：通过。
  - `pnpm exec playwright test e2e/issues.spec.ts --grep "can create child issues and manage labels and dependencies"`：1 个测试通过。
