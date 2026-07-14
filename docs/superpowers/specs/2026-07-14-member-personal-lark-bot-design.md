# 成员 PersonalAgent 飞书 Bot 设计

## 目标

每位 workspace 成员自行在 Multica 扫码创建并绑定一只飞书 PersonalAgent。该 Bot 是成员在飞书侧的 Multica 身份入口：入站消息以该成员身份创建 Issue、发表评论并触发任务；出站收件箱通知通过该成员自己的 Bot 私信。

## 核心模型

- 新增 `lark_installation.installation_kind = 'member'` 与 `member_user_id`。
- 同一 `workspace_id + member_user_id` 仅允许一条成员 Bot 安装记录。
- 成员 Bot 的设备流安装必须由当前成员本人发起；成功回调中把飞书 `open_id` 与成员身份原子绑定。
- 既有 `agent` Bot 保持不变。旧 `notification` Bot 不再承担成员通知投递，但保留历史记录与已发送消息的回复路由。

## 通知策略

通知事件类型是 workspace 管理员的统一策略，不属于任何成员 Bot。策略迁移到 workspace 级持久化记录，并从已有 notification Bot 的配置回填；没有旧配置时使用现有五项默认事件。

## 交互与权限

- Workspace 设置中的飞书页面增加“我的飞书 Bot”卡片，所有成员可见且只操作自己的安装。
- 管理员继续配置 workspace 通知事件；成员不能修改全局事件策略。
- 成员不得查看或撤销其他成员的 Bot；管理员可查看安装状态并撤销异常安装。

## 验收

1. Fan 等新成员无需看见安装者的 Bot；自己扫码后拥有独立 PersonalAgent。
2. 成员向自己的 Bot 发消息，Issue/评论/任务发起人是该成员。
3. 管理员启用的收件箱事件会从每位收件人自己的 Bot 发出。
4. 成员退出 workspace 后，其绑定不再可用于消息处理或通知投递。
