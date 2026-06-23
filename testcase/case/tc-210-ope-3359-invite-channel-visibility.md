# TC-210: 邀请制频道对非成员可见化（OPE-3359）

## 关联信息

- **OPE 编号**: OPE-3359
- **Gitee PR**: 待提交
- **Commit SHA**: 待提交后填写
- **特性摘要**: 反转邀请制频道的访问模型——从"非成员完全不可见（列表过滤 + loadChannelContext 403）"改为"非成员可见（频道进列表、消息可读），发言由 handler 层 `canPost()` 门控（非成员 403）"。对齐产品设计"邀请制频道 = 频道及消息都可见，非被邀成员无法参与讨论"。

## 涉及源文件

- `server/pkg/db/queries/channel.sql` — `ListChannels` 去掉 `access_mode = 'open' OR cm.user_id IS NOT NULL` 过滤，邀请制频道进入列表
- `server/pkg/db/generated/channel.sql.go` — sqlc 重生成（`make sqlc`）
- `server/internal/handler/channel.go` — `loadChannelContext` 删除可见性 403 门，访问控制下沉到各 handler 的 `canPost()`/`canManage()`
- `server/internal/handler/channel_test.go` — `TestChannelInviteOnlyAccess` 反转：outsider GetChannel/ListChannelMessages→200、ListChannels 含 invite 频道且 `is_member=false`、SendChannelMessage→403

## 验证要点

1. **可见性放开（读）**:
   - 非成员（普通 workspace member）的 `ListChannels` 响应**包含**邀请制频道，且 `is_member=false`、`has_unread=false`。
   - 非成员 `GetChannel`（直链）→ 200（原 403）。
   - 非成员 `ListChannelMessages` / `GetMessageThread` → 200，能读到历史消息与回复。

2. **发言拦截保留（写）**:
   - 非成员 `SendChannelMessage` → 403（`canPost()` 因 `member == nil` 拒绝）。
   - 非成员 `ReplyToMessage` / `CreateChannelThread` → 403（同样走 `canPost()`）。
   - 频道 owner/成员 → 正常发言（`canPost()` 因 `member != nil` 通过）。

3. **管理操作仍受限**:
   - 非成员改/删频道、加减成员 → 403（`canManage()` 仅 wsAdmin/channelOwn）。
   - 非成员 `JoinChannel` 主动加入邀请制频道 → 403（`access_mode != "open" && !wsAdmin`，channel.go `JoinChannel` 自有门）。

4. **前端激活**:
   - 邀请制频道在侧栏带锁标记（`channels-page.tsx` Lock 图标，原为死代码）。
   - 非成员点进频道：消息可读，输入框禁用 + 显示"仅邀请"提示（`canPost = access_mode === "open" || is_member`）。

5. **回归测试**:
   - `TestChannelInviteOnlyAccess` 覆盖上述 1+2 断言，`go test ./internal/handler/ -run TestChannelInviteOnlyAccess` 通过。

## 备注

- 成员名单（`ListChannelMembers`）按产品决定对非成员**可见**（不加门）。
- 不在本次范围（pre-existing）：open 频道非成员前端 `canPost=true` 与后端 `canPost()=false` 不一致，本次不修，PR 描述注明。
- PR !434 此前对 OPE-3359 的前端 `.filter(g => g.channels.length > 0)` 空分组修复保留——防御性，无害。
