# 成员个人飞书 Bot 上线手册

## 目标

每位工作区成员自行扫码创建并绑定一个个人飞书 Bot。该 Bot 的入站消息以该成员的 Multica 身份创建 Issue、发表评论和触发任务；工作区管理员统一配置哪些收件箱事件需要推送。

## 发布前检查

1. 确认已配置 `MULTICA_LARK_SECRET_KEY`，且所有服务实例使用同一把密钥。
2. 确认 `MULTICA_PUBLIC_URL` 是用户可访问的 HTTPS 地址；它用于飞书内的绑定/跳转链接。
3. 执行数据库迁移至 `128_lark_member_personal_bots`。
4. 确认飞书开放平台允许 PersonalAgent 设备扫码创建，且服务可访问飞书/Lark Open Platform。

## 发布步骤

1. 先发布包含迁移的后端版本并执行迁移。
2. 发布 Web 与桌面端。
3. 重启所有后端实例。启动时会重新加载活跃的 Lark installations 并建立事件连接。
4. 由工作区管理员在“设置 → 飞书”选择收件箱事件类型并保存。
5. 每位成员在同一页面点击“连接飞书”，扫码完成自己的 Bot 创建。扫码成功后页面会轮询到 `success`，个人 Bot 即可收发消息。

## 验收

1. 用成员 A 扫码创建 Bot A，再用成员 B 扫码创建 Bot B；两条 `lark_installation` 记录均为 `installation_kind='member'`，且 `member_user_id` 分别为 A、B。
2. 从 Bot A 发送“创建一个测试 Issue”，创建者必须为 A；从 Bot B 发送同样消息，创建者必须为 B。
3. 给 A 产生一个已启用的收件箱事件：只向 Bot A 推送，不向 B 推送。
4. 管理员关闭该事件后，A、B 都不再收到该类推送。
5. 成员 A 的设置列表不应出现 B 的个人 Bot。

## 回滚

1. 先停止新前端入口或回滚应用版本。
2. 如必须回滚数据库，执行 128 的 down migration；它会删除成员 Bot 记录及其绑定，但不会删除原有 agent/notification Bot。
3. 回滚后，旧的工作区 notification Bot 仍使用迁移 127 的事件字段；请核对原有配置是否仍需保留。

## 运行排障

- 扫码后一直 pending：检查后端到飞书设备授权服务的网络、设备码是否过期及后端日志中的 `lark registration`。
- 扫码成功但没有推送：确认该成员已经有 active 的 member installation、`lark_user_binding` 存在，以及工作区事件策略包含该事件。
- 飞书消息身份错误：检查该 installation 的 `member_user_id` 与 `lark_user_binding.multica_user_id` 是否相同；二者必须匹配。
