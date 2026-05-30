# 3.1 项目管理设计

## 1. 目标

1. 补齐项目颜色、已完成/隐藏项目视图与统计增强。
2. 保持现有项目 CRUD 与 issue 归属关系稳定。

## 2. 非目标

- 不实现 portfolio 大盘。
- 不引入项目权限模型。
- 不重写 3.2 标签设计包。

## 3. 当前架构基线

| 证据 | 结论 |
| --- | --- |
| `apps/workspace/src/features/projects/components/projects-page.tsx` · `ProjectsPage` | 项目页已有稳定的 CRUD 页面骨架。 |
| `apps/workspace/src/shared/types/project.ts` · `Project` | 缺颜色和隐藏字段。 |
| `server/pkg/db/queries/project.sql` · `ListProjects` / `ProjectTimeStats` | 列表与统计已分离，适合渐进增强。 |

## 4. 缺口定义

- 缺项目颜色字段与 UI。
- 缺隐藏项目语义。
- 缺已完成/隐藏视图与更清晰的统计口径。

## 5. 方案与权衡

### 方案 A：把颜色、隐藏都做成前端偏好

优点：改动小。  
缺点：跨端不一致，也无法支撑项目选择器与统计过滤。

### 方案 B：在项目模型中补字段，列表与统计按字段过滤，推荐

优点：与现有 CRUD 一致；可复用后端列表查询。  
缺点：需要改 schema 和查询。

## 6. 推荐方案

采用方案 B：在 project 模型中新增 `color`、`hidden_at`（或等价隐藏标记），继续复用 `status` 表达进行中/已完成；项目页在导航层提供“全部 / 已完成 / 已隐藏”视图，统计默认排除隐藏项目和已归档 issue。

## 7. 数据模型或状态模型

- `color: string | null`
- `hidden_at: string | null`
- 视图：
  - all
  - completed（`status=done`）
  - hidden（`hidden_at != null`）

## 8. 接口契约

- `CreateProject` / `UpdateProject` 支持 `color`
- 新增 `hideProject` / `unhideProject` 或等价更新接口
- `ListProjects` 增加 `include_hidden` / `hidden_only`
- `ProjectTimeStats` 默认排除隐藏项目和已归档 issue

## 9. UI 或交互流程

- 项目创建/编辑弹层新增颜色选择器。
- 项目页新增“全部 / 已完成 / 已隐藏”切换。
- 隐藏项目默认不出现在普通项目选择器，除非显式开启“显示隐藏项目”。

### 页面交互流

```text
项目页
  -> 新建/编辑项目
  -> 选择颜色
  -> 保存
  -> 项目列表回显颜色

项目页
  -> 切换到“已隐藏”
  -> 查看隐藏项目
  -> 取消隐藏
```

### 状态机

```text
active
  -> completed
active/completed
  -> hidden
hidden
  -> active
```

### 数据变化流

```text
ProjectsPage
  -> create/update/hide project API
  -> project.sql(ListProjects/UpdateProject)
  -> project queries invalidate
  -> 列表/统计刷新
```

## 10. 权限、边界条件、异常路径

- 隐藏不是删除，隐藏项目下的 issue 仍保留 `project_id`。
- 项目颜色为空时回退到系统默认色。
- “已完成项目”被标记为低优先级 UI 优化项：原因是 `Project.status` 已存在，可先通过筛选完成，专用视图只是导航层增强。

## 11. 实现约束

- 颜色必须持久化到项目模型，不能只存在前端 local state。
- 隐藏项目与删除项目必须分开接口与文案。

## 12. 风险与对策

- 风险：隐藏项目导致项目选择器数据漂移。  
  对策：选择器默认排除隐藏项目，但支持显式显示。
- 风险：统计口径与 1.1 归档冲突。  
  对策：默认排除已归档 issue，并在查询层统一实现。

## 13. 验收检查

1. 项目可设置并持久化颜色。
2. 项目页可切换全部、已完成、已隐藏。
3. 隐藏项目不会从 issue 中丢失归属。
4. 统计默认排除隐藏项目和已归档 issue。
