# TC-074: 回复自动通知父评论作者（OPE-856）

## 关联信息

- **OPE 编号**: OPE-856
- **Commit SHA**: c135bd530
- **特性摘要**: 当用户回复某条评论时，自动通知父评论的作者（无需显式 @mention）

## 涉及源文件

- `server/cmd/server/notification_listeners.go`
- `server/cmd/server/notification_listeners_test.go`

## 验证要点

1. AC1: 回复评论自动通知父评论作者，无需显式 @mention
2. AC2: 自己回复自己的评论不产生通知
3. AC3: 回复时同时 @mention 父评论作者，只产生一条通知（去重）
4. AC4: 回复 Agent 的评论不产生通知（仅 Member 类型收通知）
5. 使用已有的 `mentioned` 通知类型，复用所有已配置渠道（inbox/DingTalk/email/webhook）
