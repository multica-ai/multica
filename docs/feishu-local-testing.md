# Multica 本地启动与飞书（Feishu）频道验收指南

本文档面向需要在本地拉起 Multica **后端 + Web**，并通过 **飞书机器人** 验证 inbound 指令、群绑定、个人私聊通知与 slash 命令等能力的开发与测试同学。v1 主路径暂不接入 LLM fallback 和附件上传。

仓库根目录下文默认指 **`multica` monorepo 根目录**（包含 `server/`、`apps/web/`、`Makefile`）。

---

## 与本仓库当前实现的对应关系（必读）

1. **启动与健康检查**：`Makefile`、`scripts/dev.sh`、`docker-compose.yml` 路径与本仓库一致；下文命令均以当前 tree 为准。
2. **飞书凭证**：飞书凭证不再通过环境变量配置。请在 **Settings → Integrations → Channel Connections** 中新增或编辑 Feishu connection；后端从数据库中的 enabled connection 读取 App ID / App Secret / Encrypt Key / Verify Token 并接线适配器。未配置 connection 时仍会启动 HTTP 服务，只是不会连接 Feishu。
3. **事件接收方式**：Feishu 适配器使用官方 SDK 的 **长连接（WebSocket）** 接收 `im.message.receive_v1`（见 `server/internal/channel/adapter/feishu/real_client.go`）。**不要求**飞书向你的本地 HTTP 回调 URL 推送事件，因此多数场景下 **不需要** ngrok 穿透也能收消息。
4. **端到端手工验收**：当前主路径为 `accept/dedup → normalize → identity-bind → chat-bind-command → chat-settings-filter → slash_expand → rule/agent intent → authz → dispatch → reply`。`listen_mode=mentions` 时普通群聊消息会被过滤，`/bind`、slash 指令和 @ 机器人消息仍会继续处理。
5. **主动推送范围**：评论提及、指派、inbox、状态相关通知会发给目标用户的飞书 **个人私聊**（`open_id`），不发 workspace primary 群。

---

## 1. 环境准备

### 1.1 依赖版本（与仓库脚本一致）

| 组件 | 说明 |
|------|------|
| **Go** | `server/go.mod` 声明 **1.26.1+** |
| **Node.js** | `scripts/dev.sh` 要求 **v20+** |
| **pnpm** | 根目录 `package.json`：`pnpm@10.28.2`（Corepack 或全局安装均可） |
| **Docker** | 用于本地 PostgreSQL 容器（`docker-compose.yml`） |
| **PostgreSQL** | 通过 Compose 启动；或使用自建实例并改写 `DATABASE_URL` |
| **Redis** | **可选**。未设置 `REDIS_URL` 时，Realtime 使用进程内 hub（单节点开发足够） |

### 1.2 获取代码并安装前端依赖

```bash
cd /path/to/multica
pnpm install
```

### 1.3 基础环境变量文件

```bash
cp .env.example .env
```

至少确认以下键（数据库默认值与 `.env.example` 一致即可本地联通）：

| 变量 | 作用 |
|------|------|
| `DATABASE_URL` | Postgres 连接串，默认 `postgres://multica:multica@localhost:5432/multica?sslmode=disable` |
| `POSTGRES_*` | `scripts/ensure-postgres.sh` / Compose 使用 |
| `PORT` | 后端监听端口，默认 **8080** |
| `JWT_SECRET` | 鉴权密钥；本地勿留空长期运行（`make selfhost` 会生成随机值） |
| `MULTICA_APP_URL` / `FRONTEND_ORIGIN` | 前端地址，默认 `http://localhost:3000` |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REDIRECT_URI` | Web 登录（Google）时需要 |
| `RESEND_API_KEY` | 邮件验证码；本地可留空，验证码会打印在后端日志 |

飞书 App ID / App Secret / Encrypt Key / Verify Token 不写入 `.env`。启动后请通过 Web UI 或 `/api/channel-connections` 创建 DB-backed Channel Connection。

### 1.4 数据库初始化（迁移）

一键（推荐，含 Postgres 检测）：

```bash
make migrate-up
```

或手动（等价核心步骤）：

```bash
docker compose up -d postgres   # 或: make db-up
bash scripts/ensure-postgres.sh .env
cd server && go run ./cmd/migrate up
```

迁移失败时：确认 `DATABASE_URL` 指向的主机/端口可达，且用户有建表权限。

---

## 2. 后端启动

### 2.1 推荐方式（与 CI/文档一致）

```bash
make server
```

等价命令（需在 **`server` 目录内**执行）：

```bash
cd server
go run ./cmd/server
```

> 说明：Issue 模板里的 `go run ./cmd/server/` 若以仓库根为 cwd 会找不到模块；**正确做法是 `cd server` 后再运行**，或使用 `make server`。

### 2.2 启动所需配置摘要

后端启动只需要 `.env` 中的数据库、鉴权、登录等基础配置。飞书联调需要先启动后端和 Web，再在 **Settings → Integrations** 创建 enabled Feishu Channel Connection。

### 2.3 如何确认后端就绪

```bash
curl -sf http://localhost:${PORT:-8080}/health && echo OK
```

- **`GET /health`**：存活探测。
- **`GET /readyz` / `GET /healthz`**：就绪（含 DB ping）。
- 创建并启用 Feishu Channel Connection 后，可在 **Settings → Integrations** 点击 test；成功时 connection 状态应变为 connected，且后端不应出现连续 SDK 认证失败。

---

## 3. 前端启动

### 3.1 推荐（仓库根目录）

```bash
pnpm dev:web
```

等价于 Turbo 过滤 `@multica/web`。

### 3.2 仅在 `apps/web` 下开发

需保证已在**仓库根**执行过 `pnpm install`（workspace 依赖在根目录）：

```bash
cd apps/web
pnpm dev
```

Next 实际监听端口由环境变量 **`FRONTEND_PORT`** 控制（默认 **3000**），见 `apps/web/package.json` 中脚本。

### 3.3 前端如何代理到本地后端

`apps/web/next.config.ts` 将浏览器访问的 **`/api/*`、`/ws`、`/auth/*`、`/uploads/*`** 反向代理到 **`REMOTE_API_URL`**，默认 **`http://localhost:8080`**。

因此本地开发通常 **无需** 设置 `NEXT_PUBLIC_API_URL`，保持 `.env.example` 中空即可，由同源路径走 rewrite。

### 3.4 确认前端启动成功

浏览器打开 **`http://localhost:3000`**（或你的 `FRONTEND_ORIGIN`）；登录流程依赖后端与 OAuth 配置。

---

## 4. 飞书开放平台与应用配置

### 4.1 创建应用与机器人

1. 登录 [飞书开放平台](https://open.feishu.cn/) → 创建企业自建应用。
2. 在应用中启用 **机器人（Bot）** 能力。
3. 发布版本并在目标租户内安装（测试可用开发者自己租户）。

### 4.2 凭证与 Channel Connection 映射

在应用「凭证与基础信息」页拿到以下值，然后填入 **Settings → Integrations → Add/Edit Channel Connection**：

- **App ID** → `App ID`
- **App Secret** → `App Secret`

若启用事件订阅加密：

- **Encrypt Key** → `Encrypt Key`
- **Verification Token** → `Verify Token`（主要用于 HTTP 回调链路；长连接模式可按控制台要求填写）

### 4.3 权限（scope）

至少保证机器人能接收消息并使用通讯录/OpenAPI（以开放平台当前菜单为准），例如：

- 读取用户发给机器人的单聊与群聊消息（对应事件 **`im.message.receive_v1`**）

保存权限变更后需 **重新发布应用版本** 并在群内重新授权。

### 4.4 事件订阅：长连接 vs HTTP 回调

| 模式 | 与本仓库关系 |
|------|----------------|
| **长连接（推荐）** | 与 `RealClient` 一致：服务端主动维持与飞书的连接，**无需填写 Request URL** |
| **HTTP 回调** | 飞书 POST 到你的公网 URL；**当前仓库主线未提供对应的 HTTP webhook 路由**，不作为默认验收路径 |

若控制台强制填写加密相关字段，请保证 Channel Connection 中的 **Encrypt Key / Verify Token** 与开放平台控制台完全一致。

### 4.5 本地 HTTP 穿透（ngrok / Cloudflare Tunnel）

在「长连接」模式下 **通常不必** 穿透；下列场景仍可能需要暴露本地 **`PORT`（默认 8080）**：

- 调试必须通过公网访问后端的其它能力（例如自定义反向代理、第三方 OAuth 回调到非 localhost 等）。

**ngrok 示例：**

```bash
ngrok http 8080
```

**Cloudflare Tunnel（quick tunnel）示例：**

```bash
cloudflared tunnel --url http://localhost:8080
```

将输出的 HTTPS URL 填到需要公网可达的外部系统配置中（**不是**飞书默认消息订阅 URL，除非你自己在前面接了网关）。

---

## 5. 群与工作区绑定（channel_chat_binding）

授权模型中有两层概念：

1. **飞书用户 ↔ Multica 用户**：`channel_user_binding` — `user_identity` token，只通过个人私聊投递；如果私聊失败，群里只提示用户先私聊机器人，不会群发 token。
2. **群 ↔ Multica Workspace**：`channel_chat_binding` — `chat_workspace` token 记录发起人的飞书用户与当前群信息；Web 端消费时会校验登录用户已经绑定到这个发起人，避免其他群成员抢绑。

### 5.1 群内 `/bind` 入口

在飞书群里发送固定命令：

```text
/bind
```

- 如果发送者还没有完成 **用户身份绑定**，机器人会优先私聊发送 `/bind?kind=user...` 链接；群里不会出现 user token。
- 如果发送者已经完成用户身份绑定，机器人会生成 **群绑定** 链接 `/bind?kind=chat...`。优先私聊发送；如果私聊失败，允许在群里回退发送 chat token 链接。
- 打开 `/bind?kind=chat...` 后，Web 页面会列出当前用户可访问的 workspaces，选择后调用 workspace channel-binding API 完成绑定。

### 5.2 Web UI：Integrations

路径：**Settings → Integrations**（组件：`packages/views/settings/components/integrations-tab.tsx`）。

- 绑定成功后，此页列出 **Channel Bindings**（可将某一绑定设为 **Primary**、解绑、更新默认项目、监听范围和固定 Agent）。
- 群内指令按当前群的 binding 解析；primary 与非 primary binding 都可以进入 inbound command dispatch。是否处理普通群聊消息由该 binding 的 **Listen scope** 控制：`mentions` 仅处理 @ 机器人，`all` 处理所有群消息。

### 5.3 HTTP API（开发者或自动化）

消费口令：`POST /api/workspaces/{workspaceId}/channel-bindings`

```json
{
  "token": "<一次性明文 token>",
  "provider": "feishu"
}
```

该接口只接受 `chat_workspace` token。需携带已登录用户的会话/PAT，且必须满足：

- 当前用户是目标 workspace member。
- 当前用户已经绑定到 token 中记录的发起人 `external_user_id`。
- token 中的 `external_chat_id/chat_type/name` 会作为真实群信息写入 `channel_chat_binding`。
- 同一个群已绑定到同一 workspace 时返回现有 binding；已绑定到其他 workspace 时返回 `409`。

### 5.4 验收「群已绑定」

- Integrations 列表中出现对应 Feishu 群绑定；默认项目、监听范围和固定 Agent 符合预期。**Primary** 只表示该 workspace + connection 的首选绑定，不再限制入站指令处理。
- 若群发消息收到 **`[WS_NOT_BOUND] 当前群尚未绑定工作区，请先完成绑定。`**（见 `server/internal/channel/inbound/authz.go`），说明当前群的 **chat 级绑定** 未建立、已删除，或消息来自另一个 channel connection。

---

## 6. 功能测试 checklist（M3a / 飞书频道）

以下表格中 **「STA-X」** 请替换为真实 Issue 编号（如 `STA-77`）；**输入** 均在飞书群内发送（文本消息或 slash，以机器人实际解析为准）。

| 功能 | 测试步骤（输入） | 预期结果 |
|------|------------------|----------|
| 创建 Issue | `/create 测试标题` 或自然语句「帮我记一个 测试标题」 | Bot 回复创建成功，并包含新 Issue 编号 |
| 加评论 | `STA-X 评论：测试评论` | Bot 确认评论已写入对应 Issue |
| 查状态 | `STA-X 现在状态` | Bot 返回当前工作流状态 |
| 改状态 | `/done STA-X` 或「STA-X 完成了」 | Bot 确认状态变更；Web 端 Issue 为 **done** |
| 改 assignee | `/assign STA-X @某位同事` | Bot 确认 assignee 变更（需同事在成员体系中可解析） |
| 改 priority | `/priority STA-X high` | Bot 确认优先级变更 |
| 加/去标签 | `/label STA-X +bug` / `-bug` | Bot 确认标签变更 |
| 状态变更通知 | 在 Web 将 Issue 改为 **in_review / done / blocked** | 目标用户的飞书个人私聊收到卡片通知（受 **Settings → Notifications → Feishu** 布尔开关影响，`issues` / `comments` / `mentions` 等） |
| 附件上传 | 发送图片或文件，附带 `STA-X` | v1 暂未接入生产主路径；不要作为本轮上线验收项 |
| Slash 帮助 | `/help` | Bot 返回可用命令列表（含自定义 **slash_aliases**，若在工作区偏好中配置） |

---

## 7. 常见问题排查

### 7.1 后端启动失败

| 现象 | 处理 |
|------|------|
| `dial tcp ...5432: connection refused` | 执行 `docker compose up -d postgres` 或 `make db-up`，再 `make migrate-up` |
| 端口占用 | 修改 `.env` 中 `PORT`，同时更新前端 `REMOTE_API_URL` 或 `NEXT_PUBLIC_*`（若未走 rewrite） |
| migration 报错 | 清空错误库或 `make db-reset`（**仅本地库**）后重跑 `go run ./cmd/migrate up` |

### 7.2 「飞书事件收不到」

| 现象 | 处理 |
|------|------|
| 后台无 Feishu connected 状态 | 检查 Settings → Integrations 中是否存在 enabled Feishu Channel Connection；点击 test 查看错误；确认权限是否已发布、机器人是否已进群 |
| SDK 侧认证失败 | 核对 App Secret 是否粘贴完整；系统时间与 NTP；必要时轮换密钥 |
| Encrypt / Verify 不匹配 | 控制台启用加密时，Channel Connection 中的 Encrypt Key、Verify Token 必须与开放平台一致 |

### 7.3 「Bot 不回复」或 `[WS_NOT_BOUND]`

| 现象 | 处理 |
|------|------|
| `WS_NOT_BOUND` | 建立 **群 ↔ workspace** 绑定；确认消息来自已绑定的 channel connection |
| 身份无法解析 / authz 失败 | 完成 **用户绑定**（identity token 链接）；确认发送者在 Multica workspace 中为成员 |
| 私聊里发送普通指令 | v1 私聊只支持绑定与接收通知；普通指令会返回 `PRIVATE_UNSUPPORTED` |
| 静默无回显 | 查看后端日志 inbound pipeline；确认进程内已接线 registry + pipeline（参见本文开头「必读」） |

### 7.4 日志在哪里看

- **源码直接跑**：终端标准输出（`go run ./cmd/server`）。
- **Compose 自托管**：`docker compose -f docker-compose.selfhost.yml logs -f`（若使用官方镜像栈）。

---

## 8. 一键拉起（可选）

```bash
make dev
```

等同于：`pnpm install`（如需）→ `ensure-postgres` → `go run ./cmd/migrate up` → 并行启动 **`go run ./cmd/server`** 与 **`pnpm dev:web`**（见 `scripts/dev.sh`）。

---

## 9. 回归命令（可选）

在配置好基础 `.env`、数据库，并通过 Settings → Integrations 创建 Feishu Channel Connection 后：

```bash
make check
```

将运行类型检查、单测与 E2E（耗时较长）；飞书相关细粒度行为还可执行：

```bash
cd server && go test ./internal/channel/... ./internal/handler/... ./cmd/server/...
```

---

文档路径：`docs/feishu-local-testing.md`。
