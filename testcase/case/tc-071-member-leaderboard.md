# TC-071: 用量页面成员排行榜（Member Leaderboard）

## 关联信息

- **OPE 编号**: OPE-2266
- **Commit SHA**: bef61f1ed
- **特性摘要**: 用量 Dashboard 增加成员排行榜 Tab，支持按成员聚合 Agent 用量并展开明细

## 涉及源文件

- `packages/views/dashboard/components/dashboard-page.tsx`
- `packages/views/dashboard/utils.ts`
- `packages/views/locales/en/usage.json`
- `packages/views/locales/ko/usage.json`
- `packages/views/locales/zh-Hans/usage.json`

## 验证要点

1. Dashboard Leaderboard 区域出现「智能体排行」和「成员排行」两个 Tab
2. 成员排行按 ownerId 聚合，无 ownerId 的 Agent 归入"系统"分组
3. 点击成员行可展开折叠面板，展示该成员名下的 Agent 明细
4. 中/英/韩三语 i18n 均正确显示
