# TC-084: Usage 默认范围 7d 与每日粒度（OPE-2481）

## 关联信息

- **OPE 编号**: OPE-2481
- **Gitee PR**: !372
- **Commit SHA**: db29f16ed
- **特性摘要**: 将 usage 默认时间范围改为 7d、默认粒度改为每日（daily），并将范围/粒度同步到 URL query 参数

## 涉及源文件

- `packages/views/dashboard/components/dashboard-page.tsx`
- `packages/views/runtimes/components/usage-section.tsx`
- `packages/views/dashboard/components/dashboard-page.test.tsx`
- `packages/views/runtimes/components/usage-section.test.tsx`

## 验证要点

1. 首次进入 usage 页面默认范围为最近 7 天、粒度为每日
2. 切换时间范围/粒度后，URL query 参数同步更新
3. 通过带 query 参数的 URL 进入时，范围与粒度按参数恢复
4. 单元测试覆盖默认值与 URL 同步逻辑
