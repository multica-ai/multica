# 钉钉 OAuth 登录（自托管）

> [English](DINGTALK_OAUTH.md) | 中文

钉钉登录让**应用所属组织的成员**扫码即可登录自托管的 Multica 实例，不需要邮箱验证码。登录账号取自该组织通讯录里成员的**企业邮箱**：已用邮箱验证码注册过的老用户按地址自动匹配；新用户则按钉钉资料（姓名 + 头像）自动创建。

应用必须建在需要登录的那个组织内，且组织要给成员分配企业邮箱。**个人组织下的应用无法使用此登录**——见下文"登录时如何解析出账号邮箱"。

配置是实例级的（一个部署对应一个钉钉应用），与 `GOOGLE_CLIENT_ID` 的模式一致——认证发生在选择工作区之前，所以登录方式的配置没法按工作区划分。

## 1. 创建 / 配置钉钉应用

在[钉钉开放平台](https://open.dingtalk.com/)创建一个**企业内部应用**（应用类型选"H5微应用"即可），然后：

1. **登录回调** — 应用详情 → **钉钉登录与分享** → 添加回调 URL：

   ```
   https://<你的-multica-域名>/auth/dingtalk/callback
   ```

   钉钉要求每次授权请求的 `redirect_uri` 与这条配置在**协议、域名、端口**三者上完全一致。如果 Multica 走非默认端口（比如反代挂在 `:6443`），配置里必须带上端口，否则扫码页会报 `redirect_uri参数错误`。

2. **权限** — 应用详情 → 权限管理。以下两个都是**必需**，缺了会拒绝所有
   用户登录：

   - **成员信息读**（`qyapi_get_member`，通讯录管理分组）：`getbyunionid`
     和 `topapi/v2/user/get` 都要校验它。缺失时两个接口报 errcode 88 /
     subcode 60011。
   - **企业员工手机号信息和邮箱等个人信息**（敏感权限，通讯录管理分组）：
     没有它 `v2/user/get` 返回的成员记录会**整体省略** `email` 和
     `org_email` 字段。
   - **通讯录个人信息读权限**（个人权限）：扫码拿到的用户凭证调用
     `GET /v1.0/contact/users/me` 时校验（用于昵称、头像、unionId）。

3. 记下 **Client ID**（AppKey）和 **Client Secret**（AppSecret）。

## 2. 配置 Multica

在自托管部署的 `.env` 里：

```ini
DINGTALK_CLIENT_ID=dingxxxxxxxx
DINGTALK_CLIENT_SECRET=<应用密钥>

# 建议：首次用钉钉登录的新用户，只有账号邮箱域名在白名单里才会被创建，
# 与 ALLOW_SIGNUP 的设置无关。
ALLOWED_EMAIL_DOMAINS=你的公司域名.com
```

`docker-compose.selfhost.yml` 会把这两个 `DINGTALK_*` 变量透传给后端。改完重启生效：

```bash
docker compose -f docker-compose.selfhost.yml up -d
```

登录页会自动出现"钉钉登录"按钮：后端只在配置了 Client ID 时才会在公开的
`/api/config` 响应里返回 `dingtalk_client_id`，前端运行时读取该字段决定按钮
显隐——不需要重新构建前端镜像。

## 登录时如何解析出账号邮箱

```
浏览器 → https://login.dingtalk.com/oauth2/auth
         （scope: "openid corpid" —— 必须如此，见排查表）
       ← 回调 ?authCode=...&state=...
前端   → POST /auth/dingtalk {auth_code}
后端   → POST /v1.0/oauth2/userAccessToken          授权码换 token
        → GET  /v1.0/contact/users/me               只取昵称/头像/unionId
        → 应用 token → getbyunionid → topapi/v2/user/get
                       （app 所属组织的成员记录：org_email）
        → findOrCreateUser(邮箱) → 签发会话 JWT + cookie
```

登录邮箱只有一个来源：**app 所属组织成员记录上的 `org_email`（企业邮箱）
字段**——管理员分配的企业地址。成员资料里的个人邮箱字段刻意不读。不是该
组织成员、或记录上没有 `org_email`，一律拒绝登录。设计上不存在任何兜底
邮箱来源。

为什么不用 `me.email`？授权接口返回的是**跨组织的账号级数据**（实测验证）：
把应用挂在一个只有 1 个人的无关个人组织下，`me` 仍然返回了用户公司组织的
企业邮箱，外加明文手机号。用它登录意味着 (a) 任何个人组织的应用都能靠别
的组织的邮箱完成登录，(b) 对在多个组织都有企业邮箱的账号，取哪个由钉钉
内部逻辑决定、无文档、可能变化。所以 `me` 只用于昵称、头像和 unionId。

实际影响：

- **企业部署**（app 建在公司组织内 + 上述权限）：正常工作。邮箱是确定性
  的——永远是该组织对这名成员的记录，且只有该组织成员能登录。
- **个人组织应用**（个人项目 / 未认证组织）：无法工作。组织没有企业邮箱，
  成员记录上没有邮箱字段，所有登录都会被 403 拒绝。要让谁能登录，就把
  应用建在谁的组织里。

已存在的用户（比如之前用邮箱验证码注册的）按地址直接匹配登录。

## 排查表

下表每一条都在真实部署里踩过。后端会把钉钉返回的原始错误体打进日志
（`docker logs <backend> | grep -i dingtalk`），排查先看日志。

| 现象 | 原因 | 解法 |
|---|---|---|
| 扫码页报 `redirect_uri参数错误` | 回调 URL 的协议 / 域名 / **端口**与配置不一致 | 在「钉钉登录与分享」里配成完全一致的 URL（含端口） |
| 回调页 404 | `/auth/dingtalk/callback` 是前端页面；把所有 `/auth/*` 都转发到后端的反代/rewrite 会打断它 | 保证该路径留在前端（见 `apps/web/config/runtime-urls.ts` 的 `isBackendAuthPath`） |
| `DingTalk account has no unionId` | 授权 URL 只带了 `scope=openid`；钉钉按 scope 决定是否返回 `unionId` | 登录页构建的授权 URL 必须是 `scope=openid corpid`（空格编码为 `%20`）——确认前端构建包含此修复 |
| 后端日志：`缺少参数：x-acs-dingtalk-access-token` | 钉钉新版 API 用自定义请求头，不认 `Authorization: Bearer` | 实现已处理（`x-acs-dingtalk-access-token` 头） |
| `DingTalk login is unavailable ... contact sensitive permission` | `getbyunionid`/`v2/user/get` 被拒：缺「成员信息读」（`qyapi_get_member`）权限，或该用户不是 app 所属组织的成员 | 开通权限（后端日志里带一键申请链接）；非组织成员本来就不在登录范围内 |
| `DingTalk login is unavailable: the app's organization has no email on record` | 成员记录上没有邮箱——典型原因是应用挂在个人组织下（没有企业邮箱），或敏感权限没开导致字段被省略 | 把应用建在分配企业邮箱的组织内；开通「企业员工手机号信息和邮箱等个人信息」 |
| `不合法的临时授权码` | 一次性的 `authCode` 被重放（比如刷新了回调页） | 不是 bug；从登录页重新发起登录即可 |

## 安全说明

- Client Secret 只存在于后端容器内；前端只会通过 `/api/config` 拿到公开的
  Client ID（与 Google OAuth 的 client_id 同等公开级别）。
- `/auth/dingtalk` 与 `/auth/send-code` 挂同一个限流中间件（`authRL`）。
  未配置 Redis 的部署里这个后端限流是空转的——和其他认证端点一样，在 nginx
  层对 `/auth/` 加 `limit_req` 兜底（见 `SELF_HOSTING.md`）。
- `ALLOW_SIGNUP=false` 时推荐配合 `ALLOWED_EMAIL_DOMAINS`：老用户始终能
  登录，新用户只有账号邮箱域名在白名单内才会被创建。

## 实现索引

| 部分 | 文件 |
|---|---|
| OAuth 换 token + 邮箱解析 + 会话签发 | `server/internal/handler/dingtalk_login.go` |
| 路由 `POST /auth/dingtalk` | `server/cmd/server/router.go` |
| `/api/config` 暴露 `dingtalk_client_id` | `server/internal/handler/config.go` |
| 环境变量透传 | `docker-compose.selfhost.yml` |
| 登录页按钮 + 授权 URL 构造 | `packages/views/auth/login-page.tsx` |
| 回调页 | `apps/web/app/auth/dingtalk/callback/page.tsx` |
| 回调页的前端路由例外 | `apps/web/config/runtime-urls.ts` |
