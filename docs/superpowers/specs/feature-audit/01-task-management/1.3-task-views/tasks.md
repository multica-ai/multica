# 1.3 任务视图执行任务

## 1. 实现目标

- 基于现有 issue 路由体系补齐 completed、archived、quadrant、timeline 四类视图。

## 2. 前置依赖

- `archived` 依赖 1.1 的归档字段与接口。
- 筛选与排序行为依赖 1.4 的边界定义。

## 3. 任务切片

### 切片 A：补服务端视图参数

- 目标文件 / 目录：
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
  - `apps/workspace/src/shared/types/api.ts`
- 完成定义：
  - completed / archived 的查询参数确定并可复用。
- 验证方式：
  - handler 测试覆盖完成视图与归档视图。

### 切片 B：补路由与列表型视图

- 目标文件 / 目录：
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/issues/components/issue-list-page.tsx`
- 完成定义：
  - completed / archived 路由可访问。
  - 列表页能根据视图加载不同 queryParams。
- 验证方式：
  - 手动验证 URL 切换、刷新恢复与详情返回。

### 切片 C：补四象限与时间轴组件

- 目标文件 / 目录：
  - `apps/workspace/src/features/issues/components/`
- 完成定义：
  - 新增独立的 quadrant / timeline 展示组件。
  - 每个任务都可进入详情页。
- 验证方式：
  - 视觉回归 + 手动交互验证。

## 4. 回写要求

1. 若视图命名或路由变更，必须同步更新 `overview.md`。
2. 实现完成后回写 `spec.md` 的完成度与交接说明。
