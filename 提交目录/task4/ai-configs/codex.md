# Codex 配置

## 使用目标

让 Codex 在这个仓库里优先保持以下行为：

1. 先读根目录 `CLAUDE.md`
2. 先检查 package 边界，再动代码
3. 共享逻辑优先抽到 `packages/core/`、`packages/ui/`、`packages/views/`
4. 不要为了省事跨边界直连平台 API
5. 修改前先看现有测试和约束

## 典型提示

- “先读仓库规则，再补实现。”
- “如果要改 task 调度，先补测试。”
- “不要直接写 UI 层里应该归 core 的逻辑。”

