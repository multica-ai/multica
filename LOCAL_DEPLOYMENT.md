# 完全本地 Docker 部署

这套配置从当前仓库构建 Multica 前端和后端，并把 PostgreSQL、Redis、登录邮件和上传文件全部保存在本机。默认使用 Mailpit 捕获邮件，不配置 PostHog、S3、Google OAuth 或任何远程 LLM API；需要真实发信时可以切换到 Resend。

## 本地组件

| 组件 | 用途 | 本机入口 |
| --- | --- | --- |
| Multica Web | Web 前端 | `http://localhost:3000` |
| Multica API | Go 后端和 WebSocket | `http://localhost:8080` |
| PostgreSQL + pgvector | 主数据库 | 仅 Docker 内网 |
| Redis | 限流、缓存和实时事件 | 仅 Docker 内网 |
| Mailpit | 捕获登录验证码邮件 | `http://localhost:8025` |
| 本地文件卷 | 头像和附件 | 通过 API 访问 |
| Ollama（可选） | 服务端辅助 LLM | `http://localhost:11434` |

所有对宿主机开放的端口都绑定在 `127.0.0.1`。

## 一键启动

环境要求：Docker Desktop 或 Docker Engine，并启用 Docker Compose v2。

### Windows PowerShell

```powershell
.\scripts\local-stack.ps1 up
```

### Linux / macOS

```bash
./scripts/local-stack.sh up
```

首次启动会执行以下操作：

1. 从 `.env.local.example` 创建不会提交到 Git 的 `.env.local`。
2. 随机生成 PostgreSQL 密码、JWT 密钥和 VCS 加密密钥。
3. 从当前 checkout 构建 `multica-backend:local` 和 `multica-web:local`。
4. 创建数据库、Redis、邮件和上传文件的持久化 Docker volume。
5. 等待所有服务通过健康检查。

打开 `http://localhost:3000`，输入邮箱登录。验证码邮件会进入 `http://localhost:8025`，无需外部邮件服务。

## 使用 Resend 发信

先在 Resend 中验证发信域名或发件地址，然后修改不会提交到 Git 的 `.env.local`：

```dotenv
RESEND_API_KEY=re_your_api_key
RESEND_FROM_EMAIL=Multica <noreply@example.com>
SMTP_HOST=
```

`SMTP_HOST` 必须为空，因为 SMTP 配置存在时会优先使用 SMTP。保存后重建后端：

```powershell
.\scripts\local-stack.ps1 restart
```

```bash
./scripts/local-stack.sh restart
```

检查后端日志，启动信息应包含 `EmailService: Resend API`：

```powershell
.\scripts\local-stack.ps1 logs
```

切回完全本地的 Mailpit：

```dotenv
RESEND_API_KEY=
RESEND_FROM_EMAIL=noreply@multica.local
SMTP_HOST=mailpit
```

## 启用本地 LLM

可选 overlay 会启动 Ollama，拉取 `.env.local` 中 `OLLAMA_MODEL` 指定的模型，并把 Multica 的服务端 LLM 请求指向 `http://ollama:11434/v1`。

```powershell
.\scripts\local-stack.ps1 up-ai
```

```bash
./scripts/local-stack.sh up-ai
```

默认模型是 `qwen3:4b`。可在 `.env.local` 中修改：

```dotenv
OLLAMA_MODEL=qwen3:4b
```

基础 stack 不启动 Ollama，也不发起服务端 LLM 请求；聊天标题等辅助功能会使用项目内置的无 LLM 回退逻辑。

## 运行本机智能体

守护进程需要调用宿主机上安装的 Codex、Claude Code、Cursor 等 CLI，因此继续运行在宿主机，而不是应用容器内。stack 启动后执行：

```bash
multica setup self-host
multica daemon status
```

## 常用命令

Windows：

```powershell
.\scripts\local-stack.ps1 ps
.\scripts\local-stack.ps1 logs
.\scripts\local-stack.ps1 restart
.\scripts\local-stack.ps1 down
.\scripts\local-stack.ps1 reset -Yes
```

Linux / macOS：

```bash
./scripts/local-stack.sh ps
./scripts/local-stack.sh logs
./scripts/local-stack.sh restart
./scripts/local-stack.sh down
./scripts/local-stack.sh reset --yes
```

`down` 保留数据；`reset` 会删除 `pgdata`、`redisdata`、`backend_uploads`、`mailpitdata` 和可选的 `ollama_data`。

## 直接使用 Docker Compose

也可以不使用脚本：

```bash
cp .env.local.example .env.local
docker compose --env-file .env.local -f docker-compose.local.yml up -d --build --wait
```

带本地 LLM：

```bash
docker compose --env-file .env.local \
  -f docker-compose.local.yml \
  -f docker-compose.local-ai.yml \
  up -d --build --wait
```

手动复制模板时，应先替换 `.env.local` 中的三个 `replace-with-...` 值；一键脚本会自动生成这些值。

## 数据备份

列出 volume：

```bash
docker volume ls --filter label=com.docker.compose.project=multica-local
```

数据库逻辑备份：

```bash
docker compose --env-file .env.local -f docker-compose.local.yml \
  exec -T postgres pg_dump -U multica multica > multica-backup.sql
```

恢复数据库：

```bash
docker compose --env-file .env.local -f docker-compose.local.yml \
  exec -T postgres psql -U multica multica < multica-backup.sql
```
