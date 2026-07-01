# 评论/描述编辑器 — 有序列表光标在 issue 切换后被强制跳行

> Status: Proposed
> Last updated: 2026-07-01
> Owner: 需求官 on behalf of wu.nerd
> Reviewers: 编排官, wu.nerd

## TL;DR

- **问题**：在评论框输入 `1.` 生成有序列表、光标停在第一行末尾后，切换到别的 issue 再切回来，光标被强制跳到第二行行首；手动移回第一行，又立即被弹回第二行。
- **确认结论**：这是**应用自身缺陷**，不是浏览器插件。评论框（`CommentInput`）在 issue 切换时按 `key={id}` 整体重挂载并从草稿恢复内容，共用的 `ContentEditor` 里一段"外部内容同步" effect 被误触发，`setContent` 重建文档后用 `Math.min` 钳位 `setTextSelection`，对有序列表这类多层嵌套结构会把光标落到非预期位置。
- **期望行为（方案 B）**：切回后**不做强制跳转**，光标落在草稿内容**末尾**，且不再出现"移回第一行又被弹回"的现象。
- **范围**：修复落在**共用的 `ContentEditor`**，评论框与描述框同源受益（本次以评论框为主验收场景）。
- **文档说明**：本项目为开源仓库、无 EARS spec 体系，本文按项目 `docs/` 既有 RFC/plan 风格编写，不产出独立 spec-delta 文件（依 issue 发起人 wu.nerd 指示）。

---

## 1. 背景与技术栈

Multica 的 issue 评论框与描述框都基于 **TipTap 3.27.1（ProseMirror）** 富文本编辑器。相关文件：

| 角色 | 文件 |
|---|---|
| 评论输入组件 | `packages/views/issues/components/comment-input.tsx` |
| 编辑器内核（评论/描述共用） | `packages/views/editor/content-editor.tsx` |
| 草稿持久化 store | `packages/core/issues/stores/comment-draft-store.ts`（按 `new:<issueId>` 存 localStorage） |
| issue 详情页挂载点 | `packages/views/issues/components/issue-detail.tsx` |

`1.` 自动转有序列表是 TipTap `StarterKit` 内置 input rule 的标准行为，符合预期，本身无需改动。

## 2. 复现步骤

1. 打开任意 issue A，在评论框中输入 `1.`，编辑器自动生成有序列表，光标停在第一行列表项末尾。
2. 不提交，切换到另一个 issue B。
3. 切回 issue A。
4. **现象一**：光标被强制跳到第二行行首（非有序列表行）。
5. 手动把光标移回第一行有序列表项。
6. **现象二**：光标立即又自动弹回第二行。

## 3. 根因分析

复现链路：

1. `issue-detail.tsx:2223` 用 `<CommentInput key={id} …>`，在 issue 切换时强制**卸载并重挂载**评论框（注释说明这是为避免草稿在不同 issue 间串写）。
2. 用户输入 `1.` 后，含有序列表的草稿被 debounce 写入 localStorage。
3. 切回时评论框重新挂载，从草稿把内容作为 `defaultValue` 灌回 `ContentEditor`。
4. `content-editor.tsx:410-479` 的"外部内容同步" effect（本意服务于 WebSocket 推送的描述更新）在此重挂载场景下也会触发：执行 `setContent` 重建文档后，用
   ```ts
   editor.commands.setTextSelection({
     from: Math.min(from, docSize),
     to: Math.min(to, docSize),
   });
   ```
   （`content-editor.tsx:452-476`）把光标"钳"回旧偏移。对有序列表这种 `orderedList > listItem > paragraph` 多层结构，`Math.min` 得到的偏移会落到非预期位置，表现为第二行行首。

**为什么排除浏览器插件**：光标被移动是应用自身 `setTextSelection` 调用的结果，链路完全在我们代码内（重挂载 → setContent → setTextSelection），无需任何外部插件参与。

> ⚠️ 边界说明：上述 effect 能干净解释"切回后光标初次跳到第二行"。而"手动移回第一行、又立即被弹回"这一**反复**现象，可能还存在第二个触发点（如某段随选区/渲染重跑的逻辑）。根因主线已明确，精确定位"反复跳"留待**技术方案阶段**钉死。本文验收标准按**可观测行为**编写，不依赖内部实现细节，故不受此不确定性影响。

## 4. 期望行为（方案 B）

切回 issue 后：

- 草稿内容（含有序列表）**保持不变**；
- 光标落在草稿内容的**末尾**（用户上次大概率所在处），落点必须是**合法文本位置**，不得落到有序列表的结构性非文本位置；
- 不再出现"强制跳到第二行"或"移回第一行又被弹回"的现象。

不采用方案 A（精确恢复上次光标偏移）：其需额外持久化光标位置、改动与风险更大，收益相对方案 B 有限。

## 5. 影响范围

- 修复落在**共用的 `ContentEditor`**（`content-editor.tsx` 的同步 effect / 光标钳位逻辑），因此**评论框与描述框同源受益**。
- 本次**验收以评论框场景为主**；描述框顺带修复但不额外扩展验收范围。
- 不改动 `1.` → 有序列表的 input rule 等既有正常行为。

## 6. 验收标准

以下为可观测、可测的验收条件（对应自动化测试路径由技术方案/开发阶段补齐，暂标 TBD）：

- **AC1**：在评论框输入 `1.` 生成有序列表并停在第一行，切换到其他 issue 再切回，草稿内容不变，且光标**不被强制跳转到第二行行首**。（TBD：`packages/views` 编辑器切换用例）
- **AC2**：切回后手动将光标移动到有序列表第一行，光标**停留在用户放置的位置**，不自动弹回其他行。（TBD：同上）
- **AC3**：评论框因 issue 切换重挂载并从草稿恢复内容时，光标定位到**合法文本位置**（草稿内容末尾），而非有序列表结构性非文本位置。（TBD：`content-editor` 光标钳位单测）
- **AC4**：既有行为不回归——`1.` 仍能正常转有序列表；描述框（共用 `ContentEditor`）在 WebSocket 描述更新场景下的光标同步行为不被破坏。（TBD：`content-editor` 现有同步用例）

## 7. 决策与风险

- **决策**：期望行为选**方案 B**（不强制跳转 + 落草稿末尾），范围**覆盖共用 `ContentEditor`**（评论 + 描述），由 issue 发起人 wu.nerd 确认。
- **风险**：`content-editor.tsx:410-479` 的同步 effect 同时服务"WebSocket 描述更新"与"评论框草稿恢复"两个场景，修复须保证不破坏前者（见 AC4）；"反复跳"的精确机制需技术方案阶段落实，避免只修表象。

## 8. 变更历史

- 2026-07-01：需求确认，落地本提案（方案 B / 覆盖共用场景），供技术方案阶段对齐。
