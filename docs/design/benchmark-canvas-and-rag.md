# 工作流画布与 RAG 知识库：业内对标调研报告

状态：调研报告，2026-09-19。范围：无限画布工作流编排 + RAG 知识库。本文是四阶段改进计划的第①阶段产出：先对齐业内做法，再列出我们现状与差距；优先级排序（P0/P1/P2 与 MVP 范围）在下一阶段单独成文。

## 0. 摘要

1. 画布最大短板不是"画布本身"，而是**可观测性深度**：编辑器现在已把运行详情映射回节点并对相邻运行边做动画，也能按需打开节点级明细；节点内仍只有状态和语义摘要，完整输出、attempt 历史和事件仍在运行面板。Dify/n8n/Windmill 把"节点上直接看见运行结果"当作默认体验。
2. 画布基础操作的 P0 已补齐：节点库、MiniMap、复制/删除快捷键、自动布局、服务端撤销/重做入口和连线提示已经接入；框选/剪贴板、连续拖动合并撤销与连线中点插入仍低于业界水位。
3. 连线的语义完全不可见：schema 里 `source_port`（success/approved/rework/failure）齐全，但画布渲染不区分，条件分支和失败路径看起来和普通连线一模一样；非法连线静默拒绝、无任何提示。
4. **很多"差距"其实已被自家设计文档承诺**（`agent-workflow-design.md` §3.2/§3.3 明确要求左侧节点库、条件出口标注、连线上"+"插入节点、连续拖动合并撤销），后端 API（版本历史/activate/copy-to-draft、单节点重试、接管、resolve）也已交付——大量工作是前端接线而非新造能力。
5. RAG 后端检索能力已是业界主流水平（关键词 tsvector + pgvector 两路召回 50 条、RRF 融合、Cohere 协议 rerank、文档/标签过滤）；当前页面已提供召回调试面板、处理进度、引用问答、版本/块查看与图谱入口，但 Chunk 分页检查台和批量操作仍未完成。
6. **聊天与知识库完全没打通**：chat 代码里 0 处 knowledge 引用；唯一通道是内置技能文档教智能体用 `multica knowledge` CLI 自己查。这是用户感知最直接的缺口。
7. 依赖隐患已处理：`@xyflow/react`（12.11.6）和 `elkjs`（0.12.0）已经登记在 workspace catalog、`@multica/views` manifest 与 lockfile；后续仍需关注桌面/Web 构建产物的一致性。
8. 引用体验已从纯来源列表提升为段落角标 + 来源卡片，并保留查询 ID、实际检索模式和重排状态；与 FastGPT 的原文高亮、多引用导航和 Kotaemon 的 PDF 页内定位相比，Chunk 检查台和精确页内定位仍是后续差距。

## 1. 调研范围与方法

- 调研日期：2026-09-19。所有结论基于当日实际访问的官方文档、GitHub 仓库（README / changelog / 源码文件）与官方博客；文末来源清单标注访问日期。
- 产品迭代快，本文结论有时效性；第三方后续改版不自动改变结论。个别产品官方文档为 SPA 无法抓取正文（Coze SaaS），或只有社区二手资料，均已逐条标注。
- 覆盖产品：

| 领域 | 产品 |
| --- | --- |
| 工作流画布 | Dify（v1.14）、n8n（1.10x+）、ComfyUI（前端 v1.24+）、Coze Studio 开源版（v0.5+，FlowGram 引擎）、LangSmith Studio（原 LangGraph Studio）、Windmill |
| 白板/画布库参考 | tldraw、Excalidraw、React Flow（@xyflow/react）官方示例库 |
| RAG 知识库 | RAGFlow（v0.27.2）、FastGPT（v4.17.0）、Dify Knowledge（v1.13.3）、AnythingLLM、Open WebUI、Kotaemon（低频维护）、Coze 知识库 |
| 框架层参考 | LlamaIndex（分块/检索模式） |

- "我们现状"一列来自 2026-09-19 对当前工作树的只读代码盘点（关键结论附 `文件:行号`），设计文档摘要来自 `agent-workflow-design.md` 与 `knowledge-base-technical-design.md`。
- 调研维度与任务书一致：画布 5 维（节点体验 / 连线体验 / 画布操作 / 运行与调试 / 版本与协作），RAG 5 维（文档接入 / 分块 / 检索 / 问答体验 / 管理 UI）。

## 2. 画布产品概览

| 产品 | 画布底层 | 一句话定位 | 对我们最有价值的一点 |
| --- | --- | --- | --- |
| Dify | React Flow + Yjs（1.14 起协作）[S1] | LLM 应用/工作流平台，节点粒度适中 | 完整快捷键体系、节点状态图标、连线中点"+"插入节点 |
| n8n | Vue Flow + dagre [S30] | 通用自动化，节点数千 | 快捷键最全、minimap 自动显隐、dirty node 概念、执行历史复制进编辑器 |
| ComfyUI | 自研 litegraph 分叉（Vue3 壳）[S36] | 节点粒度极细的 AI 绘图图编排 | 端口按类型着色连线校验、节点内图片预览、Subgraph 发布复用 |
| Coze Studio（开源） | FlowGram.ai 自由布局 [S37][S39] | 字节扣子开源版，Bot+工作流 | 节点卡内嵌"试运行结果条"、单节点试运行表单自动生成 |
| LangSmith Studio | 未公开（图由代码定义，画布只读渲染）[S40] | Agent IDE，调试器而非搭建器 | 断点（节点前/后 Interrupt）、checkpoint Fork/Re-run、时间旅行 |
| Windmill | Svelte Flow + d3-dag | 代码优先的自动化平台 | Test up to step / Test this step（前步输出预填）、Re-start from X、每用户独立草稿 + 冲突检测 |

白板级参考（tldraw / Excalidraw / React Flow 示例）的关键可借鉴交互在 §3 各表中按需引用，其"最值得抄的 10 个细节"见 §6。

## 3. 画布维度对比

我们的实现：`packages/views/workflows/workflow-canvas.tsx`（单文件画布）+ `packages/core/workflows/graph.ts`（校验）+ `workflow-editor-page.tsx` / `workflow-run-panel.tsx` / `workflow-inbox-page.tsx`。节点 8 类（start/agent/human_task/human_review/condition/parallel/merge/end），上限 200 节点 1000 边（`graph.ts:58-59`，后端镜像 `server/internal/workflow/graph.go:499`）。设计文档 §3.2/§3.3 已定义交互契约，但多数未落地。

### 3.1 节点体验

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 端口类型化 | ComfyUI 端口按数据类型着色、类型不兼容不能连 [S31]；Coze/FlowGram 变量类型引擎校验 [S39]；React Flow `isValidConnection` + handle `connectionindicator` 状态类 [S60] | schema 有 `source_port`（success/approved/rework/failure/default），图校验完整（`graph.ts:105-130`），但画布每个节点只有左右各一个同样式 Handle（`workflow-canvas.tsx:27`） | 用户完全看不见条件/失败/返工出口，"连上了但不知道连的是什么" | **采纳**：渲染命名端口并按出口类型着色；类型/拓扑不匹配的连线直接禁止并给原因 |
| 运行状态可视化 | Dify 节点状态图标：成功=绿勾、失败=警告色、运行中=蓝色旋转 loader [S2]；n8n 绿勾 + dirty 节点黄三角+边框变色 [S17]；Windmill DAG 每步实时绿/红点 [S46]；ComfyUI 出错节点标记+进度面板 [S32] | 运行详情查询会把 activation 状态映射到画布节点，节点描边、角标和运行中动画可见；状态由轮询与 WS 失效共同驱动（`workflow-editor-page.tsx`、`workflow-canvas.tsx`） | 节点内完整输出、输入和耗时仍在运行面板查看 | **已落地**；继续补节点内摘要属于 P1 |
| 节点内嵌输出预览 | ComfyUI 图片直接渲染在节点内 [S32]；Coze 节点卡带试运行结果条 [S38]；Dify 输出进面板+底部 Variable Inspector [S3][S4]；LangSmith 节点 "View LLM Runs" [S42] | 无；输出只在运行面板 Sheet 内展示（`workflow-run-panel.tsx:51`） | 跑完一次运行必须开抽屉才能知道每步产出了什么 | **改造**：节点卡片内显示最近一次输出的单行摘要（截断文本/交付字段名）；完整输出仍在面板；不做节点内完整 JSON/图片渲染 |
| 错误高亮与单节点重试 | n8n 失败标红+error workflow [S19]；Dify 失败分支橙色高亮 [S5]；Windmill 任意节点 "Re-start from X" [S46] | 后端支持失败节点重试（`retry_node` 按钮，`workflow-run-panel.tsx:53`，`mutations.ts:54`），但画布无失败高亮，重试入口藏在面板 | 失败节点在画布上不突出，重跑入口不可发现 | **采纳**：失败节点红色描边+错误摘要 tooltip+节点级重试按钮 |
| 节点折叠/展开 | ComfyUI Alt+C [S33]；n8n 组折叠 [S23]；Windmill 组折叠/子流程内联展开 [S45] | 无 | — | **不做**：分组/子流程属 W3（设计文档 §1.3），本期不动 |

### 3.2 连线体验

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 拖拽连线 | React Flow 内建，各家一致 [S58] | 已有（onConnect，`workflow-canvas.tsx:218`） | — | 保持 |
| 非法连线即时校验与提示 | React Flow `isValidConnection` 是标准做法 [S60]；Dify 按节点类型白名单校验（静默拒绝）[S7]；设计文档 §2.2 明确要求"拖线时即时解释非法连接" | 仅 6 条基础规则（空/自连/连 start/从 end 出/不存在/重复边，`workflow-canvas.tsx:29-33`），`isValidConnection` **静默拒绝**；环检测、parallel 分支数等语义规则只在保存/发布时由 `validateWorkflowGraph` 报错 | 连错线当时不知道，保存/发布才报错且要自己找节点 | **采纳**：把图校验规则接入连线时校验；拒绝时连线变红/toast 给出原因码 |
| 条件分支连线标注 | n8n IF 节点 true/false 双输出 [S20]；Dify IF/ELIF/ELSE 多出口 [S8]；设计文档 §3.2 要求"条件出口显示规则名称、异常连接虚线+失败文字" | 边的 `label/sourcePort` 数据在 schema 里，画布**不渲染**（仅 smoothstep+箭头，`workflow-canvas.tsx:155`） | 条件分支、返工、失败路径在画布上与普通连线无差别，直接违背"连线不允许隐藏语义"的设计承诺 | **采纳**（P0 候选）：渲染端口名/分支标签；rework 边虚线+图标；failure 边虚线+文字 |
| 删线交互 | n8n 悬停连线出现 Delete [S16]；React Flow 选中+Delete 即可 | 选中边后 Backspace/Delete + `onEdgesDelete`（`workflow-canvas.tsx:207,217`） | 基本达标 | 保持 |
| 连线中点"+"插入节点 | Dify 连线中点号直接插入下一节点 [S6]；设计文档 §3.3"连线上的 +，插入后自动重连" | 无 | 加"中间步骤"必须拖两次线 | **采纳**（P1）：边上加号，插入后自动重连并打开配置 |
| 连线动画 | React Flow 动画边（SVG animateMotion / CSS offset-path）[S62]；Dify 运行中节点连线有流动感 | 无 | 运行中路径在画布上无动态指示 | **改造**（P1，随运行状态一起做）：运行中路径的边播放流动动画 |

### 3.3 画布操作

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| Minimap 小地图 | React Flow MiniMap 可 pannable/zoomable+按节点类型上色 [S64]；n8n 操作时自动出现、1s 无操作隐藏（源码确认）[S22]；Coze 内建 minimap-plugin [S37] | 已接入 React Flow MiniMap，并按节点类型着色；与 Background/Controls 一起提供画布导航（`workflow-canvas.tsx`） | — | **已落地**；保持 |
| 网格与吸附 | n8n snap-to-grid 默认开（16px）[S22]；ComfyUI 默认关、Shift 临时吸附 [S32]；tldraw 默认关、Cmd 临时开、8px 屏幕像素阈值+等距 gap 提示 [S56] | 只有点阵背景，无吸附 | 手动排版对不齐 | **改造**（P1）：吸附默认关、按住修饰键临时吸附（对齐 tldraw 模式）；网格吸附可后置 |
| 对齐辅助线 | tldraw 对齐边/中心/角+间距度量线 [S56] | 无 | — | **不做**（P2）：成本高收益低 |
| 框选多选 | React Flow `selectionOnDrag`/`selectionKey` [S58]；n8n Ctrl+A、方向键选相邻 [S21] | 未配置 selectionOnDrag/selectionKey | 无法快速框选一片节点 | **采纳**（S）：开启框选+全选快捷键 |
| 复制/粘贴/快捷键 | Dify 完整体系：Del、Mod+C/V/D、Mod+Z/Y、V/H/C 模式切换、Mod+O 自动整理、Mod+1 适配 [S9]；n8n 更全：N 节点面板、Ctrl+K 命令栏、Shift+S 便签、Ctrl+G 分组 [S21] | 已有节点库、搜索、N/Ctrl+D/Delete/Escape 等高频快捷键；服务端 undo/redo 已接入按钮，键盘撤销与连续拖动合并仍待补齐 | 快捷键体系和撤销粒度仍低于成熟产品 | **部分落地**；Ctrl+Z/Y 与保存合并列为 P1 |
| 撤销/重做 | tldraw mark+diff 事务模型：交互打标、连续变更自动合并为一个撤销单元，选择操作不清空 redo 栈 [S55]；React Flow Pro 示例为快照双栈（付费）[S67] | **服务端 undo/redo 已存在**（`POST /history/{undo|redo}` + canUndo/canRedo，`workflow-editor-page.tsx:145-146`，`mutations.ts:22`），但无键盘快捷键，且每次拖动/改动都产生新 revision，撤销粒度过细 | 能力有、体验没有；粒度违背设计文档"连续拖动合并"承诺 | **改造**（P0）：Ctrl+Z/Y 接服务端 API；保存防抖合并（设计文档 §3.3 的 800ms 方案）使一次连续拖动=一个 revision=一步撤销 |
| 自动布局 | Dify Mod+O Organize [S9]；n8n tidy up 可只整理选中 [S21]；Windmill d3-dag [S30] | 已有：elkjs layered/RIGHT 按钮触发，布局结果持久化到服务端 graph（`workflow-canvas.tsx:168-180`） | 基本达标 | 保持；"只整理选中"P2 |
| 分组/子流程 | ComfyUI group+Subgraph 可发布为 Blueprint 复用 [S34]；n8n Ctrl+G 组 [S23]；Windmill 组折叠+子流程内联展开 [S45] | 无 | — | **不做**（本期，W3 范围） |
| 画布便签 | n8n 7 色+Markdown+图片 [S24]；Windmill GFM+锁定+明暗主题适配 [S51] | 无 | 流程说明只能写在节点描述里 | **改造**（P2）：React Flow 自定义注释节点，成本低；设计文档未强制 |
| 节点面板 | Dify block-selector：搜索+Tab 分类+插件市场 [S11]；n8n N 键唤起 [S21]；设计文档 §3.2 要求左侧"步骤库"（智能体执行/人工办理/人工审核/按条件选择/同时进行） | 只有左上"任务/并行"两个按钮（`workflow-canvas.tsx:191-197`） | 新用户发现不了 8 种节点类型；i18n 里已有 `add_condition/add_review/add_agent` 等未使用键 | **采纳**（P0）：左侧节点库（搜索+按"人/智能体/控制"分类+点击/拖入） |
| 官方模板库 | Dify 模板市场 [S15]；n8n 模板库 [S21] | 已有：创建时选择模板（`workflow-list-page.tsx:21,65-74`） | 基本达标 | 保持 |

### 3.4 运行与调试

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 整体运行 | 通用 | 已有：Run 按钮+输入对话框；后端支持 manual/simulation/test 三种模式（`workflow-editor-page.tsx:151,163`） | 达标 | 保持 |
| 单节点试运行 | Dify step run：面板填测试值→Run [S3]；n8n Execute step [S17]；Windmill Test this step（前步输出自动预填）/ Test up to step [S46]；Coze 节点 test run 表单自动生成 [S38] | 无 | 调试一个节点要跑完整条流程 | **改造**（P1）：复用后端 test run + 按 node_id 索引的 simulation fixtures，实现"从选定节点开始模拟" |
| 逐步执行/断点 | LangSmith：Interrupt 指定节点前/后暂停、Continue 恢复、debug mode 逐步走查 [S41][S43] | 无 | — | **不做**（P2）：与人工审核节点的"等待"语义已有重叠，优先级低 |
| 运行历史列表 | n8n Executions 标签页：按状态/时间筛选，历史执行可 "Copy to editor" 调试 [S25] | 已有：面板内 run 徽章列表+事件流水，2s/3s 轮询+WS 失效（`workflow-run-panel.tsx:16-17`） | 缺状态筛选；入口只有"历史"按钮 | **改造**（P1）：加状态筛选（成功/失败/等待/需要处理） |
| 节点输入/输出/耗时/日志 | Dify Last run 面板+Tracing 每节点耗时 [S12]；Windmill 步骤级 inputs/result/logs 实时更新 [S46]；Coze 试运行面板三页签 [S37] | 运行面板展示状态、attempt、错误、输出、人工工单与事件流水；按节点展开时按需读取 `workflowRunNodeOptions`，展示 instructions、attempts、failures、outputs | 单节点运行入口和耗时可视化仍待补齐 | **已落地基础明细**；单节点运行/耗时列为 P1 |
| 失败重跑 | n8n "Retry with currently saved/original workflow" [S25]；Windmill "Re-start from X" 任意节点续跑 [S46] | 有失败节点 retry_node（新 activation）；无运行级"再次运行"按钮（后端语义支持：再次运行=新 run，设计 §8.4） | 失败后只能单点重试，不能方便地"改完重跑整条" | **改造**（P1）：补"再次运行"入口（预填上次输入） |
| 人工审批（human-in-the-loop） | Dify Human Input：表单字段+自定义按钮+超时分支+邮件送达 [S13]；n8n Wait on Form Submitted [S26]；Windmill suspend+审批 URL+Slack/Teams 弹窗+禁止发起人自批 [S48] | 运行面板按 `form_snapshot.fields` 动态渲染文本、数字、布尔、JSON/附件字段，并保留无 Schema 时的 JSON 兼容入口；独立收件箱仍以 JSON 兼容入口为主 | 转交、延期、接管和超时处理入口仍待补齐 | **已落地动态表单基础**；收件箱复用与管理动作列为 P1 |

### 3.5 版本与协作

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 草稿 vs 已发布 | n8n Publish 锁定版本供生产执行 [S28]；Windmill 每用户独立草稿+Deploy，冲突检测 [S49] | 数据模型完整（draft_revision / published_release_id / 不可变 release / 测试运行固定草稿快照，`schemas.ts:97-99`），**UI 无草稿/已发布徽标** | 用户分不清正在编辑的是草稿还是生产版本 | **采纳**（S）：头部显示"草稿 r12 / 已发布 v3"徽标 |
| 版本历史与回滚 | n8n Workflow history：版本列表+画布预览+Restore/Clone+命名+Diff（1.108.0）[S29]；Windmill History+Restore as fork+草稿与部署版 diff [S50]；Dify 命名版本+release notes+Restore [S14] | 后端 API 全有（`GET /releases`、`activate`、`copy-to-draft`，`WorkflowReleaseListSchema` 已定义）但**前端零 UI**；头部"历史"按钮打开的是运行历史不是版本历史 | 版本管理能力"有背无脸" | **采纳**（P0/P1）：版本历史抽屉（列表+查看+设为发布版+复制为草稿） |
| 多人同时编辑 | Dify 1.14 Yjs 实时协作+评论 [S15]；ComfyUI yjs CRDT 开发中 [S36]；n8n 同一时间仅一人编辑（其余只读）[S28]；Windmill 独立草稿+保存冲突检测 [S49] | expected_revision CAS+冲突保留本地草稿（设计 §3.3）；无实时协作 | 无实时协同，但 CAS 防覆盖已有 | **不做**（P2）：小团队场景 CAS+保存冲突提示足够；CRDT 成本与收益不匹配；Windmill 式"每用户草稿"可后置评估 |

### 3.6 画布差距清单（供第②阶段排序）

- **C1** 节点运行状态可视化（含运行中连线动画）——已落地；节点内摘要与精准 patch 仍可增强
- **C2** 节点面板（搜索/分类/拖入）+框选+复制粘贴/全选/Delete 快捷键——基础能力已落地；完整剪贴板与框选体验仍可增强
- **C3** minimap（内建组件，低成本）——已落地
- **C4** 条件/返工/失败连线的端口与标签渲染
- **C5** 非法连线即时校验与原因提示
- **C6** Ctrl+Z/Y 接服务端 undo/redo + 保存合并粒度
- **C7** 节点输入/输出/日志明细面板——已落地基础明细；耗时与单节点运行仍待补齐
- **C8** 人工审批动态表单（替代 JSON 文本框）——已落地；转交/延期入口仍待补齐
- **C9** 版本历史 UI（草稿/发布徽标+版本列表+回滚/复制为草稿）
- **C10** 失败节点画布高亮+节点级重试按钮；"再次运行"
- **C11** 单节点试运行（复用 simulation fixtures）
- **C12** 连线中点"+"插入节点
- **C13** 吸附（默认关+修饰键临时开）
- **C14** 幽灵依赖修复（`@xyflow/react`/`elkjs` 登记 package.json + catalog）——已落地
- **C15** WS 事件粗粒度失效 → 运行状态精准 patch（C1 的性能前置）

## 4. RAG 产品概览

| 产品 | 一句话定位 | 对我们最有价值的一点 |
| --- | --- | --- |
| RAGFlow（v0.27.2）[S69] | 深度文档理解型 RAG | 10 种分块模板+分块编辑器+**召回测试面板参数全部文档化**+文档批量管理 |
| FastGPT（v4.17.0）[S74] | 知识库+工作流平台 | **引用阅读器**（浮窗原文高亮+7/10 导航+评分标签+授权修正） |
| Dify Knowledge（v1.13.3）[S78] | 平台内知识模块 | 检索参数文档化最好（TopK 3/阈值 0.5/权重滑杆）、父子分块、元数据过滤最完整 |
| AnythingLLM [S85] | 轻量本地 RAG | 分块参数默认值惯例（1000/20）、workspace 挂库模式 |
| Open WebUI [S88] | 自托管 LLM 前端 | 8 种可插拔解析引擎、混合检索参数最全（BM25 权重 0.5+cross-encoder）、`#` 引用知识库 |
| Kotaemon [S91] | 开源 RAG UI（25.8k star，低频维护：v0.12.0 2026-05） | PDF 浏览器内高亮引用+相关度分数+点击定位原文 |
| Coze 知识库 | 扣子知识库（官方文档 SPA，细节来自二手资料，未逐条核验） | 分段预览+重新分段、表格库索引列+NL2SQL |
| LlamaIndex（框架）[S92] | 分块/检索模式参考 | 层级分块+small-to-big/auto-merging、语义分块、reranker 生态 |

## 5. RAG 维度对比

我们的实现：后端 `server/internal/knowledge/`（解析/分块/检索/问答/图谱/worker 队列），18 张 `knowledge_*` 表（迁移 471），前端 `packages/views/knowledge/` 6 页 + `packages/core/knowledge/`。

### 5.1 文档接入

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 上传格式 | FastGPT 4.16 起覆盖 DOC/PPT/ODT/EPUB/RTF 等 [S74]；RAGFlow 支持图片/扫描件 [S69]；Dify 内嵌图片抽取为分块附件 [S78] | md/txt/html/csv/docx/xlsx/pdf 内置解析，其余仅保存不索引；100MiB 上限（`parser.go:44-95`） | 缺 pptx/epub/图片类 | **改造**（P2）：按需求逐步增格式；解析契约（规范化 blocks+locator）已就绪 |
| OCR 与版面解析 | RAGFlow DeepDoc（OCR+表格识别 TSR+版面识别 DLR）默认，可切 Docling/MinerU/VLM [S70]；Open WebUI 8 种解析引擎可按扩展名路由 [S88]；LlamaParse 版面感知 SaaS [S94] | 无传统 OCR；已有"解析增强"：vision 模型栅格化 PDF 页面产出 `model_derived` 块、人工确认后入库（`enhancement.go:44-70`）；支持外挂 parser 服务（`ParserURL`，失败回退内置） | 扫描件完全不可用；但增强骨架与外挂契约方向与业界一致 | **改造**（P1）：短期用 vision 增强覆盖扫描 PDF（补默认引导），长期接 Docling 类解析服务 |
| 网页抓取 | FastGPT 站点同步（同域子页+CSS 选择器）[S77]；Dify Firecrawl/Jina 整站 [S78]；AnythingLLM 粘贴链接+单文件 watch 同步 [S85] | 单页 URL 导入（worker fetch 阶段，`worker.go:455-524`） | 无整站/定时刷新 | **不做**（P2）：产品定位是工作区知识库，整站爬取非核心；定时刷新后置 |
| 第三方同步 | RAGFlow Confluence/S3/Notion/GDrive/Azure DevOps [S69]；FastGPT 飞书/钉钉/语雀；Coze Notion/飞书 | 无 | — | **不做**（P2） |
| 表格解析 | RAGFlow "Excel to HTML"+列角色（索引/元数据）[S70] | xlsx 按 sheet、CSV 编码检测；分块不跨 sheet；公式不重算（与设计 §7.3 一致） | 达标 | 保持 |

### 5.2 分块（chunking）

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 策略 | RAGFlow 10 模板（General/QA/Book/Laws/Paper/Table/One/Tag/Manual/Presentation）+子块检索开关 [S70]；Dify General vs **Parent-child**（小块召回返回大父块）[S79]；FastGPT 直接分段+QA 拆分+一条数据多向量 [S75]；LlamaIndex 层级分块+small-to-big/auto-merging（叶子小块检索、命中超阈值向上合并父块）[S92] | 单一策略：结构块聚合+token 预算（标题/段落/表格行分组），长块按码点硬切 | 只有"通用分块"；缺父子分块导致的典型问题：小块命中后上下文残缺 | **改造**（P1）：优先补**父子分块**（子块向量检索、父块进上下文）；QA 拆分与多模板后置 |
| 可配置项 | Dify 分隔符/最大长度/重叠+清洗规则 [S79]；RAGFlow 推荐大小/分隔符/重叠%/auto-keyword/auto-question [S70]；AnythingLLM 默认 1000/20 [S85]；Open WebUI 默认 1000/100+三种 splitter [S89] | **零暴露**：800/1200/100 tokens 硬编码（`chunker.go:9-11`） | 用户无法按资料特点调整 | **采纳**（P1）：库级设置暴露 chunk 大小/重叠/分隔符，高级默认折叠；默认值维持 800/1200/100 |
| 分块预览/编辑 | RAGFlow 分块列表：双击编辑内容/关键词/问题、手动加分块、点正文跳原文、按状态过滤 [S71]；Dify 分块增删改启停+Edited 标记 [S84]；Coze 分段预览+重新分段（二手资料） | **无 chunk UI**；解析 blocks 预览有（文档页），chunk API 存在（`router.go:2091`）但前端零消费 | 用户看不到"文档被切成了什么"，检索质量问题时无从下手 | **采纳**（P0 候选）：分块查看器（列表+启停+跳原文定位）先行；增删改编辑后置（P1） |
| 分块级增强 | RAGFlow auto-keyword/auto-question 每块生成关键词/问题增强召回 [S70] | 无 | — | **不做**（P2） |

### 5.3 检索与召回测试

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 混合融合方式 | FastGPT 显式 RRF [S75]；RAGFlow 关键词/向量加权（示例 0.3/0.7）+PageRank 加成 [S70]；Dify 权重滑杆或 rerank 二选一 [S80]；Open WebUI BM25 权重默认 0.5 加权 [S90] | 关键词（tsvector GIN）+向量（pgvector 余弦）两路各召回 50 条，**RRF（k=60）**融合（`logic.go:242-244`、`search.go:339`）；模式 hybrid/keyword/semantic | 与 FastGPT 同款主流方案；无加权模式 | **保持**；加权融合 P2 再评估 |
| Rerank | 各家均支持；Open WebUI cross-encoder、Dify Cohere/Jina、LlamaIndex 10+ reranker 生态 [S93] | 已支持 `cohere_compatible` rerank provider，`rerank_applied` 标记（`search.go:354-372`） | 达标；默认关闭与业界一致 | 保持 |
| 可调参数 | Dify TopK 3 / Score Threshold 0.5（rerank 阶段生效）[S80]；RAGFlow 相似度阈值 0.2/向量权重/TopN [S72]；FastGPT 最低相关度+按 tokens 的引用上限 [S75] | 仅 limit（默认 10、上限 50，`search.go:252-255`）；**score 不返回前端**（只有 rank+retrieval_channels，`types.go:233-243`）；RRF k 不可调 | 无法按需调召回量与过滤阈值；得分黑盒 | **采纳**（P1）：暴露 top_k/相似度阈值（模式生效范围与 Dify 对齐）；score 仅管理员诊断视图返回 |
| 元数据过滤 | Dify 最完整：内置字段+自定义（String/Number/Time）+Automatic/Manual 模式 [S81]；RAGFlow 元数据过滤 [S72] | document_ids / tags / source_kind（`search.go:67-100`） | 无自定义元数据键值过滤 | **改造**（P2）：tags 已覆盖多数场景；自定义元数据后置 |
| **召回测试面板** | RAGFlow：数据集页"Retrieval testing"，输入查询+临时调参（阈值/权重/TopN/rerank）→结果区展示命中分块+相关性+来源文档，可按文件过滤，参数仅本次生效 [S72]；Dify：侧栏"Retrieval Testing"+**Records 记录全部检索事件**（含生产调用）[S82]；FastGPT 手动搜索测试（官方 issue 确认）[S74] | 知识库页已提供查询、hybrid/keyword/semantic 模式、limit、rerank 与结果元数据（mode、query_id、warnings、channels）；当前仍缺分数/阈值调节与分块检查器 | 参数化调优和可视化分数仍有缺口 | **已落地基础测试面板**；score/threshold/chunk inspector 列为 P1 |

### 5.4 问答与引用体验

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 引用标注样式 | **FastGPT 引用阅读器**：点击引用弹浮窗显示完整原文并高亮被引片段，右上角 7/10 多引用导航，评分标签悬停看详情，授权用户可标注修正，可导出全文 [S76]；Kotaemon：PDF 浏览器内高亮引用+相关度分数+点击定位 [S91]；Dify "Citation and Attribution" 开关 [S83] | ask 页已提供答案中的段落级引用标记、引用来源卡片、chunk 回链与文档入口；服务端严格校验引用 ID 必须来自本次检索结果 | 精确页码/段落定位和浮层原文预览仍可增强 | **已落地基础引用体验**；精确 locator/原文浮层列为 P1 |
| 流式输出 | 各家聊天默认流式 | ask 接口非流式（设计 §8.6 首期决策），界面显示"正在查找/生成" | 等待感强 | **改造**（P2）：SSE 流式，需评估引用校验时序 |
| 聊天/智能体挂知识库 | Dify Chatbot Context 挂库+Chatflow Knowledge Retrieval 节点（多库）[S83]；Open WebUI 聊天输入 `#` 引用 KB、模型绑定 KB [S88]；AnythingLLM workspace 级嵌入+chat/query 模式 [S86]；FastGPT dataset_search 节点 [S75] | **聊天完全没打通**：`packages/views/chat/` 无 knowledge 引用；服务端 chat/agent 工具无原生 knowledge 能力；唯一通道是内置技能文档教智能体用 `multica knowledge search/read` CLI（`builtin_skills/multica-platform/references/knowledge.md:13-21`） | 用户最直接的期待（在聊天里问、答带引用）落空 | **采纳**（P0 候选）：聊天内引用知识库并渲染引用来源（方案二选一：聊天侧显式挂库 / agent 原生 knowledge 工具+引用回传；第③阶段定） |

### 5.5 知识库管理 UI

| 子项 | 业界做法 | 我们现状 | 差距 | 建议 |
| --- | --- | --- | --- | --- |
| 文档列表与索引状态 | RAGFlow 列（Name/Size/Enabled/Chunks/Parse/Status），状态 waiting/running/completed/failed/canceled，按状态/启用/来源过滤 [S73]；Dify 启停/归档/删除+自动停用策略 [S84] | 任务列表展示 stage/status、可取消 queued/running/waiting_config，并以进度条和阶段文案呈现 worker 进度；version 状态 processing/ready/unsupported/failed/cancelled 也有展示 | 完整多阶段 stepper、失败原因分层和批量操作仍可增强 | **已落地基础进度可视化**；完整 stepper/批量操作列为 P1 |
| 批量操作 | RAGFlow 批量启停/解析/取消/元数据编辑/删除 [S73] | 无 | 多文档管理效率低 | **采纳**（P1）：批量删除/重新处理 |
| 分块查看器 | 见 §5.2 | 无 | — | 同 §5.2 |
| 权限隔离 | AnythingLLM Admin/Manager/Default+workspace 级 [S87]；Open WebUI KB 级访问控制 [S88] | private/workspace 可见性+创建者集中管理（创建时选择，`knowledge-list-page.tsx:28,66`）；权限校验在服务端逐请求执行 | 符合产品定位（设计 §6.1） | 保持 |

### 5.6 RAG 差距清单（供第②阶段排序）

- **K1** 聊天内引用知识库+引用渲染（chat 与 knowledge 打通）
- **K2** 召回测试面板——基础查询、模式、limit、rerank、query_id 与 warnings 已落地；score/threshold/chunk inspector 仍待增强
- **K3** 分块查看器（列表+启停+跳原文；chunk API 已有）
- **K4** 文档处理进度可视化——基础进度条、stage/status 与取消已落地；完整 stepper/批量操作仍待增强
- **K5** 引用体验升级——段落角标、来源卡片和 chunk 回链已落地；精确 locator/原文浮层仍待增强
- **K6** 分块参数化（大小/重叠/分隔符库级设置）
- **K7** 父子分块（小块检索、父块上下文）
- **K8** 检索参数暴露（top_k/相似度阈值；score 管理员可见）
- **K9** 批量操作（删除/重新处理）
- **K10** 扫描件 OCR（vision 增强路径完善 + Docling 接入评估）
- **K11** 流式问答输出
- **K12** 自定义元数据过滤、加权融合、分块级 auto-keyword（远期）

## 6. 跨产品最值得借鉴的细节

画布侧：

1. **节点状态图标规范**（Dify [S2]）：成功绿勾 / 失败红警 / 运行中蓝色 spinner，放节点右上角；n8n 的 dirty node（输出过期变黄三角+边框变色）[S17] 值得随后跟进。
2. **React Flow MiniMap**（[S64]）：`pannable/zoomable` + `nodeColor` 按类型上色，小地图从"展示"升级为"导航工具"；官方内建组件，成本最低。
3. **tldraw 的 mark+diff 撤销模型**（[S55]）：交互开始打标、期间变更自动合并为一个撤销单元，"一次拖拽=一步撤销"不用额外状态机；与我们服务端 revision 模型天然契合。
4. **连线中点"+"插入节点**（Dify [S6]）：配合"插入后自动重连+打开配置"，是加步骤的最短路径。
5. **Easy Connect 思路**（[S61]）：降低"瞄准小端口"的失败率——对我们可先做"端口命中区域放大+连线吸附"。
6. **Windmill 的 Test up to step / Test this step**（[S46]）：前步真实输出自动预填下游测试输入，是单节点试运行的黄金体验。
7. **Dify Human Input 表单化审批**（[S13]）：审批按钮即分支（产出 action_id 驱动路由）+超时走 timeout 出口；直接对应我们 human_review 的 approve/rework。
8. **n8n 版本历史**（[S29]）：版本列表+画布预览+Restore/Clone+Diff；我们 API 已齐，只差 UI。
9. **节点面板**（Dify [S11] / n8n [S21]）：搜索+分类+拖入/点击；配合快捷键 N 唤起。
10. **Open WebUI 的 `#` 引用知识库**（[S88]）：聊天输入框内最低成本的挂库交互，值得作为聊天打通的候选方案。

RAG 侧：

1. **RAGFlow 召回测试面板**（[S72]）：查询+临时参数+命中列表+按文件过滤；参数"仅本次生效，不回写配置"的边界设计值得照抄。
2. **FastGPT 引用阅读器**（[S76]）：浮窗原文高亮+多引用导航（7/10）+评分标签；是引用体验的业界标杆。
3. **分块管理三件套**（RAGFlow [S71] / Dify [S84]）：列表+启停+跳原文定位；"被编辑过的分块标 Edited"的诚信设计。
4. **父子分块**（Dify Parent-child [S79] / LlamaIndex small-to-big [S92]）：小块精准命中、大块完整上下文，是检索质量提升性价比最高的一步。
5. **文档状态可视化**（RAGFlow [S73]）：状态列+批量操作+按状态过滤；我们 stage 字段现成，只差 UI。
6. **可插拔解析引擎**（Open WebUI 8 引擎 [S88] / RAGFlow DeepDoc/Docling/MinerU [S70]）：我们 `ParserURL` 外挂契约方向正确，坚持解析与业务解耦。
7. **默认值惯例**：chunk 800-1000 / overlap 20-100（AnythingLLM/Open WebUI/我们的设计一致）；检索 TopK 3-10、阈值 0.2-0.5（Dify/RAGFlow）。
8. **Records 检索记录**（Dify [S82]）：召回测试面板同时记录生产检索事件，便于排查"线上为什么没命中"——P2 借鉴。

## 7. 来源清单

所有链接访问日期均为 **2026-09-19**。标注（源码）的条目为 GitHub 源码文件；标注（二手）的条目未能打开官方原文，来自搜索摘要或社区资料，可信度较低。

**Dify**
- [S1] https://github.com/langgenius/dify/blob/main/web/package.json
- [S2] （源码）https://github.com/langgenius/dify/blob/main/web/app/components/workflow/nodes/_base/components/node-status-icon.tsx
- [S3] https://docs.dify.ai/en/cloud/use-dify/debug/step-run.md
- [S4] https://docs.dify.ai/en/cloud/use-dify/debug/variable-inspect.md
- [S5] https://docs.dify.ai/en/cloud/use-dify/build/predefined-error-handling-logic.md
- [S6] （源码）https://github.com/langgenius/dify/blob/main/web/app/components/workflow/custom-edge.tsx
- [S7] （源码）https://github.com/langgenius/dify/blob/main/web/app/components/workflow/hooks/use-workflow.ts
- [S8] https://docs.dify.ai/en/cloud/use-dify/nodes/ifelse.md
- [S9] （源码）https://github.com/langgenius/dify/blob/main/web/app/components/workflow/shortcuts/definitions.ts
- [S10] （源码）https://github.com/langgenius/dify/blob/main/web/app/components/workflow/operator/control.tsx
- [S11] （源码）https://github.com/langgenius/dify/tree/main/web/app/components/workflow/block-selector
- [S12] https://docs.dify.ai/en/cloud/use-dify/debug/history-and-logs.md
- [S13] https://docs.dify.ai/en/cloud/use-dify/nodes/human-input.md
- [S14] https://docs.dify.ai/en/cloud/use-dify/build/version-control.md
- [S15] https://dify.ai/blog/dify-1.14.1-workflows-become-a-team-asset

**n8n**
- [S16] https://docs.n8n.io/build/understand-workflows/workflow-components/connect-nodes-together.md
- [S17] https://docs.n8n.io/build/understand-workflows/understand-executions/understand-dirty-nodes.md
- [S18] https://docs.n8n.io/build/work-with-data/pin-and-mock-data.md
- [S19] https://docs.n8n.io/build/flow-logic/handle-errors-gracefully.md
- [S20] https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.if.md
- [S21] https://docs.n8n.io/build/keyboard-shortcuts.md
- [S22] （源码）https://github.com/n8n-io/n8n/blob/master/packages/frontend/editor-ui/src/features/workflows/canvas/components/Canvas.vue
- [S23] https://docs.n8n.io/build/understand-workflows/workflow-components/canvas-groups.md
- [S24] https://docs.n8n.io/build/understand-workflows/workflow-components/add-notes-and-documentation.md
- [S25] https://docs.n8n.io/build/understand-workflows/understand-executions/view-executions-for-a-single-workflow.md
- [S26] https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-base.wait.md
- [S27] https://docs.n8n.io/build/integrate-ai/ai-examples/human-in-the-loop-for-tools.md
- [S28] https://docs.n8n.io/build/understand-workflows/save-and-publish-workflows.md
- [S29] https://docs.n8n.io/build/manage-workflows/view-change-history.md
- [S30] （源码）https://github.com/n8n-io/n8n/blob/master/packages/frontend/editor-ui/package.json

**ComfyUI / Coze Studio / FlowGram**
- [S31] https://docs.comfy.org/basic-concepts/workflow
- [S32] https://docs.comfy.org/interface/settings/lite-graph
- [S33] https://docs.comfy.org/interface/shortcuts
- [S34] https://docs.comfy.org/interface/features/subgraph
- [S35] https://docs.comfy.org/interface/overview
- [S36] https://github.com/Comfy-Org/ComfyUI_frontend
- [S37] https://github.com/coze-dev/coze-studio
- [S38] https://github.com/coze-dev/coze-studio/wiki/10.-Add-new-workflow-node-types-(frontend)
- [S39] https://github.com/bytedance/flowgram.ai

**LangSmith Studio / Windmill**
- [S40] https://docs.langchain.com/langsmith/studio
- [S41] https://docs.langchain.com/langsmith/use-studio
- [S42] https://docs.langchain.com/langsmith/observability-studio
- [S43] https://www.langchain.com/blog/langgraph-studio-the-first-agent-ide
- [S44] https://docs.langchain.com/langsmith/human-in-the-loop-time-travel
- [S45] https://www.windmill.dev/docs/flows/flow_editor
- [S46] https://www.windmill.dev/docs/flows/test_flows
- [S47] https://www.windmill.dev/docs/core_concepts/monitor_past_and_future_runs
- [S48] https://www.windmill.dev/docs/flows/flow_approval
- [S49] https://www.windmill.dev/docs/core_concepts/draft_and_deploy
- [S50] https://www.windmill.dev/docs/core_concepts/versioning
- [S51] https://www.windmill.dev/docs/flows/sticky_notes

**tldraw / Excalidraw / React Flow**
- [S52] https://tldraw.dev/sdk-features/selection
- [S53] https://tldraw.dev/sdk-features/groups
- [S54] https://tldraw.dev/sdk-features/clipboard
- [S55] https://tldraw.dev/sdk-features/history
- [S56] https://tldraw.dev/sdk-features/snapping
- [S57] https://tldraw.dev/sdk-features/camera
- [S58] https://reactflow.dev/examples/overview
- [S59] https://reactflow.dev/examples/nodes/node-toolbar
- [S60] https://reactflow.dev/examples/interaction/validation
- [S61] https://reactflow.dev/examples/nodes/easy-connect
- [S62] https://reactflow.dev/examples/edges/edge-label-renderer
- [S63] https://reactflow.dev/examples/interaction/drag-and-drop
- [S64] https://reactflow.dev/api-reference/components/minimap
- [S65] https://reactflow.dev/examples/grouping/sub-flows
- [S66] https://reactflow.dev/examples/layout/elkjs
- [S67] https://reactflow.dev/pro
- [S68] https://github.com/xyflow/xyflow

**RAGFlow / FastGPT / Dify Knowledge**
- [S69] https://github.com/infiniflow/ragflow
- [S70] https://ragflow.io/docs/dataset_configuration
- [S71] https://ragflow.io/docs/chunk_parsing_results_and_knowledge_fragment_management
- [S72] https://ragflow.io/docs/retrieval_testing
- [S73] https://ragflow.io/docs/files_dataset_document_management
- [S74] https://github.com/labring/FastGPT/releases
- [S75] https://doc.fastgpt.io/zh-CN/guide/dataset/dataset_engine
- [S76] https://doc.fastgpt.io/zh-CN/guide/chat/quoteList
- [S77] https://doc.fastgpt.io/zh-CN/guide/dataset/websync
- [S78] https://docs.dify.ai/en/cloud/use-dify/knowledge/create-knowledge/import-text-data/readme.md
- [S79] https://docs.dify.ai/en/cloud/use-dify/knowledge/create-knowledge/chunking-and-cleaning-text.md
- [S80] https://docs.dify.ai/en/cloud/use-dify/knowledge/create-knowledge/setting-indexing-methods.md
- [S81] https://docs.dify.ai/en/cloud/use-dify/knowledge/metadata.md
- [S82] https://docs.dify.ai/en/cloud/use-dify/knowledge/test-retrieval
- [S83] https://docs.dify.ai/en/cloud/use-dify/knowledge/integrate-knowledge-within-application.md
- [S84] https://docs.dify.ai/en/cloud/use-dify/knowledge/manage-knowledge/maintain-knowledge-documents.md

**AnythingLLM / Open WebUI / Kotaemon / Coze / LlamaIndex**
- [S85] https://docs.anythingllm.com/setup/embedder-configuration/text-splitting
- [S86] https://docs.anythingllm.com/chatting-with-documents/introduction
- [S87] https://docs.anythingllm.com/features/security-and-access
- [S88] https://docs.openwebui.com/features/workspace/knowledge
- [S89] https://docs.openwebui.com/features/chat-conversations/rag
- [S90] https://docs.openwebui.com/reference/env-configuration
- [S91] https://github.com/Cinnamon/kotaemon
- [S92] https://developers.llamaindex.ai/python/framework/module_guides/loading/node_parsers/modules/
- [S93] https://developers.llamaindex.ai/python/framework/module_guides/models/rerankers/
- [S94] https://developers.llamaindex.ai/python/cloud/llamaparse/overview
- [S95] （二手）https://github.com/coze-dev/coze-studio/issues/162

### 覆盖度与局限说明

- 未查到公开资料的格子已在表格中如实标注，主要包括：Dify 删线交互/分支连线样式、n8n 对齐辅助线、ComfyUI 单节点试运行与节点耗时面板、Coze SaaS 文档（SPA 抓取失败，相关行标注二手）、各家"文档索引状态枚举"（仅 RAGFlow 完整公开）。
- "我们现状"为静态代码盘点，未做运行时验证；以代码为准，实施前需再次核对。
