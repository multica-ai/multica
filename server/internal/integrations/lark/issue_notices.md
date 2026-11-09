# Issue 状态回传到来源飞书群

飞书群内的聊天任务可以让智能体创建 Issue。聊天回复结束后，Issue 的后续执行仍可能继续。本功能把根 Issue 的完成、阻塞及执行失败通知发回创建需求的群，并提及该次需求的发起人。

## 范围

- 来源必须是 `agent_create`，由创建该 Issue 的智能体所执行的聊天任务提供可信的 `channel_task_delivery`。
- 仅处理飞书群，不向私聊、其他渠道或另一个工作区补推消息。
- 根 Issue 进入 `blocked` 时通知阻塞；从未完成类别进入 `done` 类别时通知完成，包括自定义的完成状态。
- Issue 最新执行失败后等待 30 秒。期间出现新的执行或 Issue 状态变化时取消过期通知。执行完成本身不等于 Issue 完成。
- `/issue` 命令创建的来源目前记录为 `lark_chat` 和会话 ID，缺少逐 Issue 的冻结触发消息及发起人。本次不从会话的最新消息推测来源，因此不覆盖该入口。
- 不回填升级前已经结束的 Issue，不重新打开已经结束的 `channel_reply_delivery` 聊天投递。

## 持久性和来源验证

数据库触发器与 Issue / 执行状态变更在同一事务写入 `channel_issue_notice`。服务重启后工作进程继续读取该队列，不依赖进程内事件是否送达，也不轮询业务表来推断历史状态。

投递保存原始群、消息、线程、发起人、安装和绑定。发送前再次校验工作区成员资格、渠道账号绑定、智能体调用权限、应用安装代际、智能体和会话状态。安装替换、权限撤销或来源缺失时取消投递，不改发其他群或个人。通知只包含智能体名、Issue 编号、标题、状态和站内链接，不转发私有评论、附件或运行时错误。

多个服务实例通过行锁、`SKIP LOCKED` 和带所有权令牌的 45 秒租约领取通知。只有当前所有者能够记录结果。成功必须具有飞书返回的消息 ID；不明确的网络结果继续使用同一个通知 UUID。

单条通知最多尝试 3 次，失败间隔为 `15 * 2^attempt + [0,10]` 秒。首次尝试后的 4 分钟内完成全部尝试，过期后停止自动补发，以保持在飞书 UUID 去重窗口内。最终失败保留在队列中供运维查询。每次投递结果记录结构化日志，字段仅含通知 ID、状态、尝试次数和回执 ID。

工作进程随 API 的生命周期启动和取消，复用现有飞书凭据解密、区域主机选择、原消息引用和线程寻址。不增加第三方依赖或部署服务。删除工作区和 Issue 时清理对应通知。数据库迁移先于新服务启动；回退前应停止工作进程，并按新增迁移逆序回滚，未发送通知也会随表移除。

## 定向验证

在受管理的开发环境里执行 `make sqlc`，然后进入 `server/`：

```sh
go test ./internal/integrations/lark -run 'TestIssueNoticeText|TestHTTPClientSendTextIdempotencyKey' -count=1
go test ./internal/integrations/lark -run '^TestIssueNoticeDatabase$' -count=1 -v
go vet ./internal/integrations/lark
go test ./cmd/server -run '^$'
```

数据库用例要求显式 `DATABASE_URL` 指向可丢弃且具备 `CREATEDB` 权限的测试服务。用例通过官方 `testutil.IsolatedDB` 新建空数据库并执行正常迁移，结束后销毁，不消费现有数据库的通知。飞书传输使用本地 HTTP 替身，不发送真实群消息，也不调用用户配置的智能体 CLI。
