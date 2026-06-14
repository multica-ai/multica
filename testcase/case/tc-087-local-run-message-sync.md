# TC-087: Local Run 消息同步可靠性（OPE-2701）

## 关联信息

- **OPE 编号**: OPE-2701
- **Gitee PR**: !385
- **Commit SHA**: 43e402b40
- **特性摘要**: 为 local run（本地 CLI 运行）引入持久化消息 outbox，使 Agent 消息同步具备可靠投递能力，避免本地运行过程中消息丢失

## 涉及源文件

- `server/internal/handler/local_cli_outbox.go`
- `server/internal/handler/local_cli_run.go`
- `server/internal/handler/local_cli_run_test.go`
- `server/cmd/multica/cmd_run.go`
- `server/cmd/multica/cmd_local_run.go`
- `server/cmd/multica/cmd_run_test.go`
- `server/cmd/multica/main.go`
- `server/cmd/server/main.go`
- `server/migrations/117_local_cli_message_outbox.up.sql`
- `server/migrations/117_local_cli_message_outbox.down.sql`

## 验证要点

1. local run 过程中产生的消息写入持久化 outbox 表，进程或网络中断后仍可恢复投递
2. 消息按序、且不重复地同步到服务端（幂等投递）
3. CLI 端 `cmd_run` / `cmd_local_run` 正确读取并提交 outbox 中的待发送消息
4. 服务端 outbox handler 正确处理消息的写入、拉取与确认
5. 迁移 117_local_cli_message_outbox 可正常 up/down
6. 单元测试覆盖 outbox 持久化与同步逻辑（local_cli_run_test.go、cmd_run_test.go）
