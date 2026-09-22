# Multica 无限画布编排与知识库 RAG：调研与交互方案

调研日期：2026-09-13。交付入口：同目录 `index.html`，无外部脚本、字体和网络依赖，可直接打开。

## 结论

以 Dify 的知识检索链路为信息结构参考，以 Langflow 的画布操作为交互参考，结合 n8n 的运行调试、Make 的条件可读性、Flowise 的人工接续和 RAGFlow 的片段溯源。最终界面沿用 Multica 的中性表面、品牌蓝、紧凑侧栏与角色字号。

知识库作为独立工作区资源管理资料，工作流用知识检索节点引用它。RAG（检索增强生成）指先检索资料片段，再把这些片段交给模型作为回答依据。

## 样本与证据

“TOP 级别”在本次定义为有成型产品、公开一手资料，且代表通用自动化、AI 编排或 RAG 的产品。没有使用市场占有率、收入或排行榜数据，不构造名次。以下是文档研究，不是登录各产品付费后台的实测；官方功能说明不等于独立性能证明。

| 产品 | 已核对的一手能力 | 对 Multica 的借鉴 | 本次取舍 |
| --- | --- | --- | --- |
| Dify | 知识检索节点选择知识库、Top K、阈值、重排与元数据过滤；结果进入下游上下文；支持引用 | 知识库独立、流程节点引用；参数进入侧栏 | 不将全部高级字段放进节点卡片 |
| n8n | 执行列表按状态筛选、按原始或当前工作流重试、旧执行数据回到画布调试 | 将编排、运行记录、版本并列；记录节点输出 | 本原型不声称实现生产重试与幂等执行 |
| Langflow | 编辑器支持节点拖动、画布平移、缩放与适应视图；Playground、日志、JSON 导出 | 画布基础操作与试运行闭环 | 更紧凑地呈现节点信息，避免参数表堆满画布 |
| Flowise | Agentflow V2 支持人工输入节点、继续/拒绝路径、共享状态与检查点 | 运行中明确显示等待人工确认 | 只演示页面内接续，不冒充服务端持久化检查点 |
| Make | 连线过滤器控制数据通过，标签显示在编排器中 | 分支直接显示有依据/无依据 | 使用矩形节点，匹配 Multica 现有视觉结构 |
| RAGFlow | 模板分段、可视化片段、人工干预、可追溯引用、多路召回与融合重排 | 导入—片段—检索—问答—来源验证闭环 | PDF、Word、网页同步不假装解析成功 |

来源：

- [Dify：Knowledge Retrieval](https://docs.dify.ai/en/cloud/use-dify/nodes/knowledge-retrieval)，正文读取成功。
- [Dify：Knowledge](https://docs.dify.ai/en/cloud/use-dify/knowledge/readme)，正文读取成功。
- [n8n：All executions](https://docs.n8n.io/workflows/executions/all-executions/)，搜索索引返回官方正文，但直接打开已返回 Page Not Found。上述能力来自索引快照，当前页面路径与功能状态有待进一步核验；不作为最新版本保证。
- [Langflow：Use the visual editor](https://docs.langflow.org/concepts-overview)，正文读取成功，页面标注 1.12.x。
- [Flowise：Agentflow V2](https://docs.flowiseai.com/using-flowise/agentflowv2)，正文读取成功。
- [Make：Step 4. Add a filter](https://help.make.com/step-4-add-a-filter)，官方搜索索引读取。
- [RAGFlow：官方 README](https://github.com/infiniflow/ragflow#-key-features)，通过[官方原始文件](https://raw.githubusercontent.com/infiniflow/ragflow/main/README.md)读取正文；旧 retrieval_test 文档地址抓取失败。

## 视觉依据

当前 checkout 中已核对：

- `packages/ui/styles/tokens.css`：HTML 内嵌当前 `:root`、`.dark` 与 `--text-*` 字号快照。应用框架使用 `--app-shell`，画布使用 `--page-canvas`，节点/面板使用 `--surface`，选中描边使用 `--brand`。
- `packages/ui/components/common/multica-icon.tsx`：复用八角星的 clip-path 几何，不另造 Logo。
- `apps/web/app/globals.css`：沿用系统字体与中文字体回退；为离线打开不下载 Inter。
- `apps/docs/content/docs/developers/conventions.zh.mdx`：工作区、任务、运行、知识库等中文术语。
- `packages/views/workflows/workflow-editor-page.tsx`：现有编排、保存、试运行、发布、历史入口。
- `packages/views/knowledge/knowledge-list-page.tsx` 和 `docs/design/knowledge-base-technical-design.md`：知识库独立、私人/共享、资料与问答结构。

仓库当前已有大量未提交的工作流与知识库实现。本次仅创建独立原型目录，不改动这些业务实现，不把设计文档中的历史现状当作当前实现结论。

## 信息结构与核心流程

工作流：编排 / 运行记录 / 版本。知识库：资料 / 检索调试 / 知识问答 / 设置。产品调研作为原型参考页，不建议直接加入生产工作区导航。

默认流程：用户提问 → 检索团队知识 → 判断命中 → 有依据时生成回答 → 人工确认 → 返回回答；无依据时直接转人工处理并返回补充资料提示。人工节点是本模板的协作选择，不要求所有未来工作流都包含人工审批。

画布支持空白拖动平移、节点拖动、滚轮锚点缩放、缩放按钮、适应视图、缩略图定位、点击输出/输入端口连线、点击边删除、节点配置、节点删除、撤销重做、自动布局、JSON 导出。该原型使用无边界坐标与视口变换，不是固定图片；未实现大图虚拟化、多选框选、协同编辑和子流程。

运行前校验唯一输入、返回节点、孤立节点、缺失连接、条件分支和环。试运行按当前连线选择执行路径，支持人工继续/拒绝与停止；配置在运行中锁定。保存为浏览器本地草稿，发布为本地版本快照。

知识库支持创建、切换、设置名称与可见性、导入 TXT/Markdown、按设置字符数分段、片段预览、停用/启用、名称/状态筛选。本地关键词检索按查询字词匹配度排序；语义/混合检索和重排是规则模拟。回答为相关片段组合，保留可点击来源。默认资料全部是虚构演示内容，不代表真实产品承诺。

## 生产实施衔接

1. 在现有 `packages/views/workflows` 内完善画布，不建立平行业务模块；图草稿与视图状态在 core 内，服务端工作流/运行由 React Query 管理。
2. 知识库延续现有设计中的独立资料生命周期。资料解析和索引是知识库内部任务，不要求用户先编排流程才能导入资料。
3. 工作流节点只保存知识库引用与查询/召回配置。服务端在实际检索时重新校验工作区成员和真实发起者，私人库不能依靠任务 token 的用户字段直接放行。
4. 未配置向量模型时仅启用关键词检索。向量模型切换创建新索引代次，构建后原子切换；不复用不兼容旧向量。
5. 真正的运行版本、恢复点、重试策略、并发和失败恢复接现有服务端运行系统。HTML 的本地存储仅供演示。
6. 首期优先完成画布操作、可定位的失败、资料处理状态、真实混合检索与引用验证；分组、子流程、协作、证据图谱随后按实际大图需求推进。

## 验证范围

浏览器交互验收记录见同目录 `verification.md`。本次不运行仓库 Go/TypeScript 业务测试，因为没有修改业务实现。
