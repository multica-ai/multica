# Multica 飞书企业登录改造方案（中文）

本文档用于说明 `mindverse-ltd/multica` 下一阶段的鉴权改造方向：在保留现有登录兜底能力的前提下，新增飞书企业登录，逐步打通公司统一身份体系。

## 1. 改造目标

当前 Multica 已支持：

- 邮箱验证码登录
- Google OAuth 登录

公司版本下一步目标是新增：

- **飞书企业登录**
- **与公司飞书身份体系打通**
- **将飞书身份映射到 Multica 用户与工作区成员**

## 2. 改造原则

### 2.1 不直接删除现有登录方式

第一阶段不建议直接删除：

- 邮箱验证码登录
- Google 登录

原因：

- 便于开发环境与应急兜底
- 可以降低首次切换风险
- 避免飞书配置异常时整个平台无法登录

### 2.2 采用“新增 provider”而不是“强替换”

推荐做法：

- 保留现有 `auth.go` 结构
- 新增飞书登录 handler
- 前端登录页新增“飞书登录”入口
- 后端新增飞书 code 换 token、换用户信息流程

### 2.3 用户体系不要只靠邮箱硬绑定

飞书企业身份应该是“外部身份源”，不能简单等价成邮箱。推荐引入：

- provider：`feishu`
- provider_user_id：飞书用户唯一标识（如 `open_id` 或企业侧稳定 ID）
- union_id / tenant_key 等企业维度信息（按实际场景保留）

## 3. 当前代码里的登录入口位置

### 3.1 后端

主要文件：

- `server/internal/handler/auth.go`
- `server/internal/middleware/auth.go`
- `server/internal/auth/cookie.go`
- `server/internal/auth/jwt.go`

当前后端能力：

- `/auth/send-code`
- `/auth/verify-code`
- `/auth/google`
- `/auth/logout`
- `/api/cli-token`

### 3.2 前端

主要文件：

- `apps/web/app/(auth)/login/page.tsx`
- `apps/web/app/auth/callback/page.tsx`
- `packages/views/auth/login-page.tsx`
- `packages/core/auth/store.ts`
- `packages/core/api/client.ts`

当前前端能力：

- 邮箱验证码登录表单
- Google OAuth 登录按钮
- Google OAuth 回调页
- CLI 登录回传
- Desktop 平台登录回调

## 4. 推荐的后端改造方向

### 4.1 新增身份映射表

推荐新增一张表，例如：

- `external_identity`

建议字段：

- `id`
- `user_id`
- `provider`（`feishu`）
- `provider_user_id`
- `union_id`
- `tenant_key`
- `email`
- `name`
- `avatar_url`
- `raw_profile`
- `created_at`
- `updated_at`

这样可以做到：

- 一个 Multica 用户可绑定多个外部身份源
- 不强耦合用户主表结构
- 未来扩展企业微信、OIDC、CAS 也更自然

### 4.2 新增飞书登录接口

建议新增接口：

- `POST /auth/feishu`

请求参数类似：

```json
{
  "code": "授权码",
  "redirect_uri": "回调地址"
}
```

后端流程：

1. 用授权码向飞书换 access token
2. 调飞书用户信息接口获取用户身份
3. 根据 `provider + provider_user_id` 查找已绑定用户
4. 若无绑定，再按企业规则决定：
   - 是否允许按邮箱合并旧用户
   - 是否创建新用户
5. 写入或更新外部身份映射表
6. 下发 Multica 自己的 JWT / Cookie

### 4.3 保留现有 JWT 与 Cookie 机制

飞书只负责“身份确认”，不应替代 Multica 自己的会话机制。

仍建议沿用：

- Multica JWT
- HttpOnly Cookie
- CLI token 下发接口

这样可以最大程度复用现有：

- Web 登录态
- CLI 登录回调
- Desktop 登录态

## 5. 推荐的前端改造方向

### 5.1 登录页新增飞书按钮

在 `packages/views/auth/login-page.tsx` 中新增：

- “使用飞书登录”按钮

但不要直接覆盖 Google 按钮的逻辑，应抽象成 provider 入口。

### 5.2 新增飞书回调页逻辑

可以继续复用现有回调页模式：

- 浏览器跳转到飞书授权页
- 飞书回调到 `/auth/callback` 或新的 `/auth/feishu/callback`
- 前端拿到 `code`
- 前端调用 `/auth/feishu`
- 后端返回 token 与用户信息

### 5.3 保持 CLI / Desktop 登录兼容

当前 Multica 登录已经同时兼顾：

- 普通 Web 用户
- CLI 浏览器回调登录
- Desktop 外部浏览器回跳

飞书登录改造时，必须确认：

- CLI 是否还能完成浏览器授权回调
- Desktop 是否还能通过系统浏览器完成登录
- `state` 参数是否需要承载 `platform` / `nextUrl` / `cli_callback`

## 6. 与企业身份打通的两种模式

### 6.1 宽松模式（推荐第一阶段）

规则：

- 飞书成功登录即可进入 Multica
- 第一次登录时自动创建用户
- 再由管理员邀请进工作区

优点：

- 实现简单
- 风险小
- 适合快速上线验证

### 6.2 严格模式（推荐第二阶段）

规则：

- 必须属于指定飞书租户或指定组织单元
- 必须在预置成员名单中
- 必须与企业邮箱域一致

优点：

- 更符合企业权限控制
- 更适合正式生产环境

## 7. 数据迁移建议

第一阶段建议新增迁移，不要破坏现有用户模型。

推荐：

- 新增 `external_identity` 表
- 给 `provider + provider_user_id` 加唯一索引
- 保留 `user.email` 唯一约束

不推荐：

- 直接在 `user` 表上塞大量飞书专属字段
- 让 `user.email` 失去唯一性

## 8. 风险点

### 8.1 同一人多身份合并问题

如果同一个人：

- 以前用邮箱验证码登录过
- 现在又用飞书登录

需要明确合并策略：

- 按邮箱自动合并
- 或必须管理员手工绑定

### 8.2 CLI 登录链路可能受影响

当前 CLI 登录依赖浏览器回跳与 `cli_callback`。如果飞书授权流程改造不慎，最容易出问题的就是 CLI。

### 8.3 Desktop 回调链路也可能受影响

桌面端目前依赖浏览器跳转再回到 `multica://` deep link。飞书登录必须验证这一段仍然通。

### 8.4 企业环境配置复杂

正式接入前需要确认：

- 飞书开放平台应用类型
- 回调域名
- 租户范围
- 用户唯一标识选择
- 内部应用还是网页应用

## 9. 推荐分阶段实施

### 阶段 1：打基础

- 新增身份映射表
- 新增飞书登录后端接口
- 新增飞书登录按钮
- 保留邮箱验证码登录兜底
- 仅验证 Web 登录

### 阶段 2：打通完整链路

- 验证 CLI 登录
- 验证 Desktop 登录
- 完善 `state` 透传与回跳

### 阶段 3：与企业成员体系联动

- 首次登录自动加入指定工作区（如有需要）
- 根据飞书身份自动映射角色
- 接入更严格的组织边界控制

## 10. 当前建议的结论

对于 `mindverse-ltd/multica`，最稳妥的方式不是“把现有登录全推翻”，而是：

- 保留现有邮箱验证码登录作为兜底
- 新增飞书登录 provider
- 引入外部身份映射层
- 后续再逐步收紧企业登录规则

这条路线对上游同步最友好，对当前项目风险也最低。