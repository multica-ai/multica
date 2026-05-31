# 1.2 任务属性配置

## 范围

- 一级模块：任务管理
- 二级能力：1.2 任务属性配置
- 清单来源：`docs/功能列表清单.md:14-24`

## 对照清单

- 已完成：任务标题
- 已完成：任务备注（Markdown）
- 已完成：任务优先级
- 缺失：预估工时
- 已完成：截止日期
- 缺失：重复任务规则
- 部分完成：任务附件/链接
- 已完成：任务标签绑定
- 已完成：任务项目绑定

## 当前状态

- 状态：部分完成
- 完成度：6 / 9

## 证据

- `apps/workspace/src/features/issues/components/issue-detail.tsx`：`TitleEditor`、`ContentEditor`、优先级、截止日期、`ProjectPicker`、`LabelPicker`
- `apps/workspace/src/features/issues/components/attachment-list.tsx`：已有附件相关 UI，但还不是完整统一的“附件/链接属性面”

## 缺口

- 缺少预估工时字段
- 缺少重复规则模型
- 附件/链接目前更像散落能力，不是完整属性配置面

## 推荐实现切片

- 先决定预估工时属于 issue 主字段，还是属于 worklog 维度

## 交接说明

- 附件能力后续需要回查后端 schema，再决定是否能从 部分完成 提升到 已完成
