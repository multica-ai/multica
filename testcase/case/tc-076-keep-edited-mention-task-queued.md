# TC-076: 编辑后的 @mention 任务保持排队（OPE-697）

## 关联信息

- **OPE 编号**: OPE-697
- **Gitee PR**: !365
- **Commit SHA**: 8c14a9c4d
- **特性摘要**: 编辑包含 @mention 的评论后，对应的 Agent 任务应保持排队状态，不被取消或丢弃

## 涉及源文件

- `server/internal/handler/comment.go`
- `server/internal/handler/handler_test.go`

## 验证要点

1. 创建一条 @某 Agent 的评论后，任务进入排队
2. 编辑该评论（仍保留同一 @mention）后，原排队任务不被取消
3. 编辑评论增删 @mention 时，任务入队/出队行为符合预期
4. 单元测试覆盖编辑后任务保持排队的场景
