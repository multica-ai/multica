# 无限画布工作流与知识库 RAG：落地优先级与验收

> 日期：2026-09-20
> 上游调研：[benchmark-canvas-and-rag.md](./benchmark-canvas-and-rag.md)
> 目的：把对标结果收敛成一组能在 Multica 当前架构中交付、可回归验证的产品切片。

## 结论

这次先交付一条“可编排、可观察、可解释”的最小闭环：画布能快速找到节点、连接关系能表达语义、运行状态能回到节点上；知识库能在检索时显式选择模式与结果数，并把服务端实际生效的模式、重排状态、查询 ID 和索引警告保留下来，资料处理任务显示可解释的进度。

这组能力直接回应调研中的 C1/C2/C3/C4/C5/C14 与 K2/K4/K5，且不要求先扩展服务端协议。块/Chunk 管理、收件箱复用人工表单 Schema、版本时间线、父子 Chunk、OCR 和流式问答仍然保留为下一阶段，避免在前端先展示服务端没有承诺的分数、阈值或字段。

## 已落地的 P0 切片

| 对标差距 | 交付内容 | 代码位置 | 验收证据 |
| --- | --- | --- | --- |
| C1 运行状态不可见 | 运行详情通过同一条 run query 映射到节点；节点按运行中、成功、失败、等待人工显示图标/色彩，运行中的相邻边动画 | `packages/views/workflows/workflow-editor-page.tsx`、`packages/views/workflows/workflow-canvas.tsx` | `pnpm --filter @multica/views typecheck` 通过；打开运行历史并选择一条 run 后，节点状态来自 `WorkflowNodeRun.status` |
| C2 节点发现与快速操作不足 | 节点库支持搜索和一键添加；`N` 打开节点库，`⌘/Ctrl+D` 复制节点，`Delete/Backspace` 删除节点，`Escape` 关闭弹层 | `packages/views/workflows/workflow-canvas.tsx` | 键盘操作不依赖鼠标；输入框获得焦点时不会误触发画布快捷键 |
| C3 缺少全局定位 | 加入 React Flow `MiniMap`，沿用 Multica 主题变量，节点状态同步到缩略图颜色 | `packages/views/workflows/workflow-canvas.tsx` | MiniMap 与画布一起渲染，支持拖拽和缩放 |
| C4 连线只表达“相连” | `flow/rework/compensation` 使用不同虚线/颜色；边使用 `label`、`sourcePort` 或 kind 作为语义标签；运行中的边动画 | `packages/views/workflows/workflow-canvas.tsx` | 语义来自现有 `WorkflowEdge` 类型，不新增虚构字段 |
| C5 非法连线反馈弱 | 自环、重复边、开始节点入边、结束节点出边等规则集中判断，并在连接失败时给出具体提示 | `packages/views/workflows/workflow-canvas.tsx`、`packages/views/locales/*/workflows.json` | 规则函数与 UI 共用；提示同时进入可见提示区和 toast |
| C14 画布依赖未声明 | 把 `@xyflow/react`、`elkjs` 纳入 workspace catalog 与 `@multica/views` 依赖，锁文件同步 | `pnpm-workspace.yaml`、`packages/views/package.json`、`pnpm-lock.yaml` | `pnpm install --lockfile-only` 成功，避免依赖只在本机 hoist 后才可用 |
| K2 检索调试面板不足 | 检索表单增加 hybrid/keyword/semantic、结果数量；结果回显实际生效模式、重排状态、查询 ID、警告 | `packages/views/knowledge/knowledge-base-page.tsx`、`packages/views/locales/*/knowledge.json` | 请求只使用现有 `SearchKnowledgeRequest` 字段；没有把不存在的 score/threshold 伪装成 UI 参数 |
| K4 处理进度不可见 | 任务列表按服务端 `progress.percent` 优先，否则按 stage 给出保守进度；失败任务显示 `error_code`，仍保留取消入口 | `packages/views/knowledge/knowledge-base-page.tsx` | 任务 query 已有 3 秒轮询；进度条带 `role=progressbar` 和数值属性 |
| K5 引用只在结果列表出现 | 回答按段落渲染；若服务端返回 `answer_paragraph`/`paragraph`，在段末显示可点击的 `[n]` 标记，并与下方来源卡片编号一致 | `packages/views/knowledge/knowledge-ask-page.tsx`、`packages/views/locales/*/knowledge.json` | 没有段落定位时保留来源列表，不伪造定位；每个标记都链接到对应文档 |

## 为什么这批优先

1. **先让用户知道系统正在做什么。** 工作流的节点状态、边动画和知识库处理进度共同解决“点击运行/上传之后失去上下文”的问题。
2. **先提高成功率，再扩展表达力。** 节点库、快捷键、MiniMap 和连线原因能降低编排成本；它们不改变运行时协议，适合在现有图模型上交付。
3. **先把可验证信息显示出来。** 检索模式、实际生效模式、重排状态、查询 ID 和警告来自现有响应；不向用户承诺后端当前没有返回的分数、阈值或 Chunk 详情。
4. **依赖声明是交付门槛。** 画布和自动布局已经在代码中使用，依赖必须进入 workspace 解析与 lockfile，才能让 CI、桌面端和新 checkout 得到相同结果。

## 下一阶段 P1

### 工作流

- **C6/C9：版本与恢复。** 把服务端 undo/redo、草稿 revision、发布状态和运行快照放入一个可读的版本时间线；目前编辑器已提供 undo/redo、保存和发布入口，下一步补历史差异和恢复前确认。
- **C7/C10/C11：节点级可观测性。** 运行面板已按需调用 `workflowRunNodeOptions` 展示节点指令、attempt、失败原因和历史产出；单节点重跑仍沿用运行面板的失败节点动作，耗时和单节点运行入口留待下一阶段。
- **C8：动态人工步骤。** 运行详情已根据 work item 的 `form_snapshot.fields` 渲染文本、数字、布尔、JSON/附件字段，并保留无 Schema 时的 JSON 兜底；待办中心仍需复用该表单并补转派、延期、超时策略和动作审计。
- **C12/C13：编辑效率。** 边中点插入、吸附和批量选择/复制，需先把 React Flow 的 selection 状态与草稿合并规则统一。

### 知识库 RAG

- **K1：回答与工作流联动。** 把 `KnowledgeAnswerResponse` 的引用上下文作为工作流输入快照，支持回答节点直接回链到同一批证据。
- **K3/K6/K8：Chunk 检查台。** 增加 Chunk 列表、启用/禁用、原文跳转、chunk 参数和检索参数快照。当前 API 已有单块读取能力，但还没有稳定的分页列表契约。
- **K9：批量操作。** 在文档列表和 Chunk 检查台提供批量重处理、启停和重新索引，所有操作保持幂等并显示 job ID。
- **K10/K11：解析与问答体验。** 在解析增强能力稳定后接入 OCR/视觉结果；在回答服务提供事件或 SSE 契约后再接入流式回答，避免前端先模拟“已流式”。

## 暂缓项与原因

| 能力 | 暂缓原因 | 进入条件 |
| --- | --- | --- |
| 多人实时协作与评论 | 需要明确操作冲突、presence 和权限模型，单靠画布 UI 无法可靠实现 | 草稿 patch/冲突协议稳定，且有协作审计需求 |
| 父子 Chunk、跨编码器重排、阈值调优 | 需要索引 schema、检索请求和评估集一起升级 | 后端返回 score、参数快照和可重复评测结果 |
| OCR/复杂 PDF 版面 | 解析质量需要样本集和人工验收 | 解析 job 能返回页级 locator、失败重试和质量指标 |
| 全量在线画布/知识库联动 | 会同时扩大权限、引用、运行时和缓存边界 | K1/K5 引用契约与工作流输入引用先稳定 |

## 验收门槛

- 前端类型检查：`pnpm --filter @multica/views typecheck`。
- 独立原型回归：在 `outputs/workflow-rag-prototype-2026-09-13/` 执行 `node verify.mjs`，结果为 `35 passed, 0 errors`；浏览器复核了节点库弹窗、画布缩略图、检索调试面板和一次检索结果。
- 依赖可复现：`pnpm install --lockfile-only` 成功，`@xyflow/react` 和 `elkjs` 在 workspace catalog、package manifest、lockfile 三处一致。
- 交互可访问：节点库按钮有 `aria-expanded/aria-controls`，搜索字段有 label，进度条有 `role=progressbar`、`aria-valuenow`，状态变化使用 `role=status` 或 `role=alert`。
- 数据边界：UI 只呈现当前 API 已返回的字段；未落地的 score、threshold、Chunk 分页和收件箱管理动作不以静态假数据冒充完成。
- 每次修改画布节点状态、边样式或检索结果布局后，需重新做一次浏览器视觉检查，并记录截图/运行结果，避免只靠类型检查判断可用性。

## 与独立原型的关系

`outputs/workflow-rag-prototype-2026-09-13/` 仍是市场调研和离线交互验证的独立原型，适合演示完整的画布/RAG概念；本文件记录的是把其中值得做的 P0 取舍落入 Multica 生产页面后的边界。两者不共用运行时数据，也不把原型中的模拟能力当作生产能力。
