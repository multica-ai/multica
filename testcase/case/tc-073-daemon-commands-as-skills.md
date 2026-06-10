# TC-073: Daemon 扫描 Claude Commands 为可导入 Skills（OPE-1408）

## 关联信息

- **OPE 编号**: OPE-1408
- **Commit SHA**: 14a225a1d
- **特性摘要**: Daemon 扫描 ~/.claude/commands/*.md 目录，将 Markdown 命令文件作为可导入的 skills 展示

## 涉及源文件

- `server/internal/daemon/local_skills.go`
- `server/internal/daemon/local_skills_test.go`

## 验证要点

1. Daemon 启动时扫描 `~/.claude/commands/` 目录下的 `.md` 文件
2. 每个 command 文件映射为 skill summary/bundle 结构
3. 使用 `commands/` key 前缀避免与常规 skills 命名冲突
4. 单元测试覆盖扫描逻辑和边界情况
