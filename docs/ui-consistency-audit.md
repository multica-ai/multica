# Multica UI 一致性问题与页面模式层治理

> Status: Draft
> Owner: TBD
> Last updated: 2026-07-11
> Related implementation: [PR #5263](https://github.com/multica-ai/multica/pull/5263)

## TL;DR

Multica 当前的主要 UI 问题不是缺少颜色 token、Button 或 Card，而是缺少一层被明确命名、文档化和持续执行的 **Page Pattern Layer（页面模式层）**。

现有基础大致分成三层：

1. `packages/ui/styles/` 已经提供颜色、surface、间距等设计 token。
2. `packages/ui/components/` 已经提供 Button、Input、Card、Tabs、Empty 等基础组件。
3. `packages/views/` 中也已经出现 `CollectionPageHeader`、`CollectionPageState`、`SettingsTab`、`SettingsSection`、`SettingsCard`、`SettingsRow` 等页面模式的早期实现。

但第三层仍然是局部的、未完成的：没有覆盖所有主要页面类型，没有清晰的使用边界，也没有 review checklist 或迁移计划。因此业务页面仍然会自行决定标题、导航、卡片、分割线、表单、空状态和保存反馈，最终形成“基础组件相同，但页面体验不同”的局面。

Agent 详情页重构是页面模式层的一次试点，不应停留在单页优化。下一步应该把其中可复用的结构提炼为明确的页面模式，再逐步迁移其他页面。

---

## 1. 文档目的

本文档用于：

- 记录本轮 UI review 中发现的问题，避免问题只存在于对话和截图中。
- 明确 UI 不一致的架构根因，而不是继续逐页修补 CSS。
- 区分 Design Token、基础组件、页面模式和业务页面各自的职责。
- 记录 Agent 详情页试点中已经处理或仍需验证的事项。
- 给出下一阶段 UI 治理的优先级、验收标准和开放问题。

本文档不是新的视觉规范。颜色、字体、surface、间距和基础交互仍以 [`docs/design.md`](./design.md) 为准。

---

## 2. 核心判断：缺失的是页面模式层

### 2.1 当前 UI 分层

| 层级 | 回答的问题 | 当前载体 | 当前状态 |
| --- | --- | --- | --- |
| Design Token | 颜色、间距、圆角、阴影应该是什么 | `packages/ui/styles/`、`docs/design.md` | 基本完整 |
| UI Primitive | Button、Input、Card、Tabs 如何表现 | `packages/ui/components/` | 基本完整 |
| Page Pattern | 页面标题、分区、导航、列表、设置表单、状态页如何组合 | `packages/views/layout/`、`packages/views/settings/components/settings-layout.tsx` 等 | **局部存在，但未固化** |
| Business Page | Agent、Issue、Runtime 等具体业务如何呈现 | `packages/views/<domain>/` | 大量自行组合 |

Token 和 Primitive 只能保证局部视觉相似，不能自动保证页面一致。它们不会回答以下问题：

- 列表页标题、数量、描述和主操作应该放在哪里？
- 设置页应该用卡片还是平铺？一行可以放几个设置？
- 二级导航应该横向还是纵向？什么时候需要 URL 深链？
- Detail 页面如何区分状态概览、工作记录和配置？
- Empty、Loading、Error、Not Found 是否共享尺寸和文案结构？
- 可编辑列表是持续显示多个 Input，还是列表 + 单个 composer？
- 保存状态、未保存提醒和离开保护如何统一？

如果这些问题没有标准答案，每个业务页面就会重新设计一次。

### 2.2 根因

当前问题可以概括为：

> 底层 token 和基础组件已经比较完整，但中间的“页面模式层”没有被明确固化，也没有被持续执行。

具体表现为：

1. **模式没有被命名。** 开发者知道有 Card、Tabs 和 Empty，但不知道当前页面属于 Collection、Settings 还是 Workbench Detail。
2. **模式没有完整覆盖。** Collection 和 Settings 已经有早期抽象，Detail、Editable List、Inspector、Split Pane 等仍主要由页面自行实现。
3. **模式没有使用边界。** 什么时候用 Card、什么时候只用 spacing、什么时候允许 divider，仍依赖个人判断。
4. **模式没有持续执行。** 缺少新页面 checklist、视觉回归样例和跨页面 review，局部 PR 很容易重新引入平行实现。
5. **复用多停留在 JSX。** 相似页面会复制布局 class，而不是复用带语义的页面组件；一旦规范变化，需要逐页修改。

---

## 3. 已发现的问题

### 3.1 全局问题

| 类别 | 当前问题 | 影响 |
| --- | --- | --- |
| 页面 Header | 标题高度、左右 padding、数量、描述和操作区组合不一致 | 页面切换时节奏跳变 |
| 二级导航 | 同类配置有的横向、有的纵向，有的只存在本地 state | 信息架构和深链行为不稳定 |
| Section 分组 | Card、divider、背景色和纯 spacing 混用，缺少明确优先级 | 页面过重或分组不清 |
| 表单布局 | 单行单项、双列设置、label/value 比例和保存区位置不一致 | 扫读和操作成本增加 |
| 列表编辑 | 有的持续显示 Input，有的用 dialog，有的就地编辑 | 同类操作需要重新学习 |
| 状态页面 | Empty、Loading、Error、Not Found 各自实现 | 图标、留白、文案和操作不一致 |
| 保存反馈 | Toast、inline status、disabled button、dirty guard 组合不统一 | 用户无法稳定判断是否已保存 |
| 内容约束 | 长描述、代码、ID 和用户输入的换行/截断策略不统一 | 容易溢出或出现密集文本墙 |
| 响应式与无障碍 | icon-only action、focus、移动端折叠依赖页面自行处理 | 容易产生不可访问或小屏失效的页面 |

### 3.2 Agent 详情页问题记录

状态说明：

- **已实现**：当前 Agent 详情页重构分支已有对应实现。
- **试点中**：已在 Agent 页面采用，但尚未抽象或推广为全局模式。
- **待验证**：需要在合并前进行完整视觉或交互回归。
- **开放**：仍需要产品或架构决策。

| 区域 | 问题 | 处理方向 | 状态 |
| --- | --- | --- | --- |
| 整体信息架构 | 原详情页更像后端字段检查器，观察、工作和配置混在一起 | 重组为 Overview、Work、Capabilities、Settings | 试点中 |
| 二级导航 | Capabilities 与 Settings 的二级选项使用横向菜单，扩展后拥挤 | 桌面端采用稳定的纵向 section nav，并保持 URL 深链 | 已实现 |
| Integrations | 本地 feature flag 默认关闭，Capabilities 中缺少 Integrations | 根据系统配置显示 Integrations，并纳入 Capabilities | 已实现 |
| Overview / Attention | “Tasks Need Attention”价值不明确、优先级过高 | 无明确干预价值时移除，不制造伪告警 | 已实现 |
| Overview / Recent Work | 条目过少；Issue 维度还是 Session 维度没有明确 | 增加可见条目；产品语义见开放问题 §8.1 | 试点中 |
| Overview / Skills | 多个 skill 时 divider 与下方横线粘连 | 优先用 spacing 分组，避免连续 divider | 待验证 |
| Instructions | System Prompt 被设计成单行 Input | 使用支持长文本、Markdown 和合理高度的 Textarea | 已实现 |
| MCP | 描述过长，缺少换行和信息层级，呈现为密集文本墙 | 缩短主文案，把说明拆成辅助信息并约束宽度 | 已实现 |
| General | 信息展示感强，不像配置页；Visibility 等交互不顺手 | 使用标准 Settings section/row，明确 label、description、control | 试点中 |
| Settings 分组 | 页面缺少稳定分组，或使用不符合 surface 规范的 Card | 独立设置组使用白色 SettingsCard；组间使用 section spacing | 已实现 |
| Settings 行布局 | 一行放两个 setting，扫读和响应式表现差 | 默认单行单项；除非字段天然组成一个原子输入 | 已实现 |
| Access | Shared 等权限不是简单 toggle，现有交互不能表达复杂规则 | Access 独立为 Settings 子 Tab，使用专门权限模型 | 试点中 |
| Details | Owner、Created、Updated 等只读元数据被放进 Settings | 从可编辑设置中移出，放到 Overview 或 inspector | 已实现 |
| Environment | 当前结构可暂时保留 | 后续迁移到统一 editable key-value pattern | 开放 |
| Custom Args | 横线过多；每个参数永久显示为 Input；新增一项就增加一个输入框和 divider | 改为有序 token 列表；只有新增/编辑项进入 composer；独立命令预览 | 已实现 |

### 3.3 Custom Args 暴露出的通用模式问题

Custom Args 并不只是一个局部样式问题，它暴露了 “Editable List” 页面模式缺失：

- 数据在静止状态下应该首先是列表，不是表单。
- 编辑态应该是临时状态，不应该让所有条目永久显示为 Input。
- 新增操作应该把内容加入列表，而不是无限扩张表单结构。
- 参数顺序有语义，应明确显示顺序。
- 每个列表项对应一个 argv token；包含空格的 token 不应被前端自行拆分。
- 命令预览属于结果反馈，应和编辑列表分开呈现。

这个模式未来可以复用于：Custom Args、Environment Variables、Webhook Headers、Repository Rules、Token/Secret 列表等，但不同数据类型需要保留各自的验证和敏感信息策略。

---

## 4. 建议固化的页面模式

### 4.1 Collection Page

适用于 Agents、Projects、Skills、Runtimes、Squads、Autopilots 等实体集合。

应统一：

- Header 的 icon、title、count、description 和 actions。
- 列表/网格容器的 page padding 和滚动策略。
- Empty、Loading、Error、No Results 状态。
- 主操作在小屏下的收缩方式。
- 筛选、搜索和视图切换的位置。

现有基础：

- `packages/views/layout/collection-page.tsx`
- `CollectionPageHeader`
- `CollectionPageHeaderAction`
- `CollectionPageState`

下一步不是再造一套组件，而是补齐使用文档、样例和迁移覆盖。

### 4.2 Settings Page

适用于 Workspace Settings、Account Settings、Agent Settings 和其他配置型页面。

应统一：

- `SettingsTab → SettingsSection → SettingsCard → SettingsRow` 的层级。
- 默认单行单项；复杂 compound control 必须说明原因。
- label、description 和 control 的宽度与对齐。
- auto-save 与 explicit save 两种模式的反馈。
- dirty state、离开保护、error 和 success 状态。
- 权限不足时的只读表现。

现有基础：

- `packages/views/settings/components/settings-layout.tsx`
- `SettingsTab`
- `SettingsSection`
- `SettingsCard`
- `SettingsRow`
- `SettingsSaveState`

需要决定这些组件是否继续归属于 Settings domain，还是迁移到更通用的 `packages/views/layout/`。

### 4.3 Workbench Detail Page

适用于 Agent、Runtime、Squad、Member 等同时包含身份、状态、历史和配置的详情页。

应统一：

- Identity header：名称、状态、关键 metadata 和主操作。
- 一级导航：Overview、Work/Activity、Capabilities、Settings。
- 二级导航：当选项超过 2–3 个或文案较长时使用纵向 section nav。
- Overview 只呈现判断和行动所需的信息，不复制全部字段。
- Settings 只放可编辑配置；只读 metadata 不混入设置。
- URL 必须反映 tab/section 状态，支持刷新和分享。
- 不为不存在的健康度或进度制造伪指标。

Agent 详情页是此模式的第一个试点，但不能直接把 Agent 业务组件当作全局抽象。应先识别 Runtime、Squad、Member 的共同结构，再定义最小公共 API。

### 4.4 Editable List

适用于用户维护一个有序或无序的小型配置集合。

默认交互：

1. 静止状态显示列表项。
2. 点击 Add 后只出现一个 composer。
3. 点击 Edit 后只让目标项进入编辑态。
4. Enter 提交，Escape 取消；新增/编辑时焦点进入输入区。
5. 删除先更新本地 draft；外部持久化由统一 Save 或 auto-save 策略负责。
6. 空状态仍保留清晰的主操作。
7. 长值必须支持截断、换行或展开查看。

不应把所有 Editable List 都抽成一个万能业务组件。可以共享布局、状态和 action slot，但验证、字段结构和安全策略留在 domain 层。

### 4.5 Page State

所有页面至少需要明确以下状态：

- Initial loading
- Background refreshing
- Empty
- No search results
- Permission denied
- Not found
- Recoverable error
- Fatal error

这些状态应复用一致的尺寸、语气和 action 位置，但不能用同一条空状态文案覆盖不同原因。

---

## 5. 组件归属原则

| 内容 | 推荐位置 | 约束 |
| --- | --- | --- |
| 无业务语义的原子组件 | `packages/ui/` | 不得依赖 `@multica/core` |
| 跨 domain 的页面模式 | `packages/views/layout/` | 只定义布局、slot 和交互契约 |
| 某类页面的共享模式 | `packages/views/<domain-or-pattern>/` | 允许有限业务语义，但不得绑定平台路由 |
| 具体业务页面 | `packages/views/<domain>/` | 组合模式并负责数据、权限和文案 |
| Next.js / Electron 接线 | app platform layer | 不进入 shared views |

判断一个结构是否应该抽取时，优先问：

1. 它表达的是视觉细节，还是稳定的页面语义？
2. 第二个真实使用者是谁？
3. 抽取后能否减少业务页面对 spacing、divider、responsive class 的重复决策？
4. 是否能通过 slot 保持业务差异，而不是制造大量 boolean prop？

---

## 6. 下一步实施计划

### Phase 0：建立基线

- [ ] 为 Collection、Settings、Workbench Detail、Editable List、Page State 各补一个 canonical example。
- [ ] 建立页面清单，标记每个页面当前采用的模式和偏差。
- [x] 给 `docs/design.md` 增加本文件入口。
- [ ] 确认 Agent 详情页试点的视觉和交互验收结果。

### Phase 1：固化现有模式

- [ ] 补齐 Collection Page 的 loading、filter toolbar 和 responsive 规范。
- [ ] 明确 Settings explicit-save / auto-save 两套状态契约。
- [ ] 从 Agent 详情页提炼最小 Workbench Detail shell，而不是复制整页。
- [ ] 提炼 Editable List 的布局契约，并用第二个真实页面验证抽象。
- [ ] 为 pattern components 增加结构和无障碍测试。

### Phase 2：迁移高频页面

建议优先级：

1. Agents / Skills / Runtimes / Projects 等 collection 页面。
2. Workspace Settings 与 Agent Settings。
3. Runtime / Squad / Member detail 页面。
4. Environment、Custom Args 和其他 editable configuration lists。

每次迁移应是小 PR，不在同一 PR 中同时改变业务行为和全局视觉规范。

### Phase 3：持续执行

- [ ] 在 PR template 中加入 Page Pattern checklist。
- [ ] 新页面必须声明采用的 page pattern；偏离时说明原因。
- [ ] 给 canonical examples 增加 light/dark、中文/英文和窄屏视觉回归。
- [ ] 定期删除迁移后遗留的平行组件和重复 class 组合。
- [ ] 将审计从一次性项目变成每个季度的 UI consistency review。

---

## 7. 验收标准

页面模式层达到可用状态，需要同时满足：

1. 新页面可以先选择页面模式，再填充业务内容，而不是从空白 `<div>` 开始。
2. 同类页面的 header、导航、section、empty/error 和 action 位置一致。
3. 业务页面不再重复决定基础 padding、divider、card 和保存反馈。
4. 模式组件支持 keyboard、focus、overflow、responsive 和 i18n 长文案。
5. light/dark、中文/英文、desktop/web 至少有 canonical visual coverage。
6. 模式允许业务差异，但偏差是显式决定，不是无意复制。
7. 新抽象至少由两个真实页面验证，避免只为单页包装 JSX。

---

## 8. 开放问题

### 8.1 Agent Recent Work 使用 Issue 还是 Session 维度

需要先确认该区块回答的问题：

- 如果回答“这个 Agent 最近交付了什么”，Issue/Task 是更接近用户心智的单位。
- 如果回答“这个 Agent 最近每次执行发生了什么”，Session 是更准确的诊断单位。

建议不要在同一个平铺列表中混合两个粒度。可以默认使用 Issue/Task 维度，把最新 Session 状态作为行内 metadata；完整 Session 历史留在 Work 页面。

### 8.2 Settings pattern 的归属

当前实现位于 `packages/views/settings/components/settings-layout.tsx`，但 Agent Settings 已经复用它。需要决定：

- 保持当前位置，把 Settings 当作共享 pattern package；或
- 迁移到 `packages/views/layout/settings-*`，让归属与实际用途一致。

在迁移前不要再创建第二套 Settings layout。

### 8.3 Card 与 flat section 的边界

默认遵循 `docs/design.md` 的容器优先级：spacing → 单条 divider → 背景变化 → Card。

建议补充的判断是：

- 独立设置组、可整体操作的信息块可以使用 Card。
- 连续阅读内容、元数据清单和普通列表不应每项一张 Card。
- Card 内不要再通过多层 border 制造“卡片套卡片”。

### 8.4 Detail pattern 的最小公共部分

Agent、Runtime、Squad 和 Member 的详情页并不完全相同。抽象前需要确认真正共享的是 shell、header、navigation 和 section layout，还是还包括 inspector。原则上优先抽取稳定外壳，业务内容通过 slot 注入。

---

## 9. 新页面 Review Checklist

- [ ] 这个页面属于哪一种 page pattern？
- [ ] 是否复用了现有 Header、Section、State 和 Action 模式？
- [ ] 是否出现了业务页面自定义的 Card、divider 或 empty state？为什么？
- [ ] 设置是否默认单行单项？复合字段是否真的是一个原子输入？
- [ ] URL 是否反映 tab、filter、pagination 或 section 等可分享状态？
- [ ] Loading、Empty、Error、Permission 和 Not Found 是否被分别处理？
- [ ] 长文案、长 ID、用户输入和窄屏是否不会溢出？
- [ ] icon-only button 是否有 accessible name？键盘 focus 是否可见？
- [ ] light/dark 和至少一种 CJK locale 是否完成视觉检查？
- [ ] 是否新增了一个只服务当前页面、但其实表达通用模式的本地组件？

---

## 10. 相关文件

- [`docs/design.md`](./design.md)
- `packages/ui/styles/tokens.css`
- `packages/ui/components/`
- `packages/views/layout/collection-page.tsx`
- `packages/views/layout/page-header.tsx`
- `packages/views/settings/components/settings-layout.tsx`
- `packages/views/agents/components/agent-detail-page.tsx`
- `packages/views/agents/components/agent-overview-pane.tsx`
- `packages/views/agents/components/tabs/custom-args-tab.tsx`
