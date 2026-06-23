# TC-209: 看板/列表中子 Issue 的视觉标识与 topLevelOnly 状态冲突处理（OPE-589）

## 关联信息

- **OPE 编号**: OPE-589
- **Gitee PR**: !423
- **Commit SHA**: c2f4ec0fc, 13270fbcd, 5ae9b4faf, 1814c4376
- **特性摘要**: 在 Issue 看板和列表中使用 CornerDownRight 图标标识子 Issue，同时拦截及纠正 swimlaneGrouping 为 parent 时 topLevelOnly 的互斥冲突。

## 涉及源文件

- `packages/core/issues/stores/view-store.ts`
- `packages/core/issues/stores/view-store.test.ts`
- `packages/views/issues/components/board-card.tsx`
- `packages/views/issues/components/board-card.test.tsx`
- `packages/views/issues/components/list-row.tsx`
- `packages/views/issues/components/issues-header.tsx`
- `packages/views/locales/en/issues.json`
- `packages/views/locales/zh-Hans/issues.json`
- `packages/views/locales/ja/issues.json`
- `packages/views/locales/ko/issues.json`

## 验证要点

1. **子 Issue 看板卡片与列表行渲染**:
   - 当 Issue 存在 `parent_issue_id`（是子 Issue），且看板的分组模式（swimlaneGrouping）不是 `parent` 时，其标题左侧应正确渲染 `CornerDownRight` 图标。
   - 当分组模式是 `parent` 时，子 Issue 不渲染此图标，避免视觉冗余。
   - 图标应附带 `aria-hidden="true"`（对辅助功能友好），且在 BoardCard 中的对齐为 `align-[-0.125em]`，在 ListRow 中包含 `shrink-0` 属性防止截断。

2. **状态互斥保护与协调 (Reconciliation)**:
   - 当通过 UI 或外部 Store API 将 `swimlaneGrouping` 设为 `parent` 时，`topLevelOnly` 过滤器应自动重置为 `false`。
   - 尝试在 `swimlaneGrouping === "parent"` 状态下调用 `toggleTopLevelOnly`，应受到 Guard 拦截而不发生状态变更。
   - 从旧版本/本地持久化恢复数据时，`mergeViewStatePersisted` 应对非法状态（`swimlaneGrouping === "parent"` 且 `topLevelOnly === true`）进行校正，将其自动合并重置。

3. **UI 按钮与 Tooltip 交互**:
   - 在看板头部，“仅一级 Issue” 过滤按钮在 `swimlaneGrouping === "parent"` 时应置灰（`disabled`）。
   - 置灰按钮的 `title`（Tooltip）显示提示文案 `Not available when grouped by parent`/`按父级分组时不可用`/`亲イシューでグループ化されているされています場合は使用できません`/`상위 이슈로 그룹화된 경우 사용할 수 없음`。

4. **单元测试与回归覆盖**:
   - `view-store.test.ts` 覆盖状态协调 and 拦截逻辑，所有测试通过。
   - `board-card.test.tsx` 覆盖卡片子 Issue 图标显隐分支逻辑。

## 备注

- 无
