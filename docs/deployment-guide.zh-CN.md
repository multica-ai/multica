# Multica 部署文档（中文）

本文档面向维护者，记录当前 `mindverse-ltd/multica` 在目标服务器上的推荐部署方式、端口规划、验证步骤，以及后续暴露公网访问时需要补做的事项。

## 1. 部署目标

- 项目：`multica`
- 目标服务器：`root@115.190.235.210:51365`
- 代码目录：`/opt/multica`
- 当前约束：
  - 不能直接开放 `80` / `443`
  - 服务器上的老版本 Docker Compose 不兼容仓库里的 `name:` 字段
  - Docker Hub 拉镜像存在超时，不适合依赖全量容器化首发部署

基于以上约束，当前优先采用：

- **PostgreSQL 使用宿主机安装版**
- **后端使用 Go 原生构建运行**
- **前端使用 Node.js + Next.js 原生构建运行**
- **先以内网回环地址启动服务，再通过 SSH 隧道访问验证**

## 2. 当前部署拓扑

### 2.1 组件规划

- PostgreSQL：`127.0.0.1:5432`
- Multica Backend：`127.0.0.1:13080`
- Multica Frontend：`127.0.0.1:13030`

### 2.2 为什么不用默认端口

默认的 `3000` / `8080` 更容易与机器上的已有服务冲突，因此统一改为：

- 前端：`13030`
- 后端：`13080`

### 2.3 当前访问方式

在未开放新公网端口前，通过本地 SSH 隧道访问：

```bash
ssh -L 13030:127.0.0.1:13030 -L 13080:127.0.0.1:13080 -p 51365 root@115.190.235.210
```

建立隧道后，本地浏览器访问：

- 前端：`http://127.0.0.1:13030`
- 后端健康检查：`http://127.0.0.1:13080/health`

## 3. 服务器初始化步骤

### 3.1 连接服务器

```bash
ssh -p 51365 root@115.190.235.210
```

### 3.2 拉取代码

```bash
mkdir -p /opt
cd /opt

git clone https://github.com/mindverse-ltd/multica.git
cd multica
```

如果目录已经存在：

```bash
cd /opt/multica
git fetch origin
git checkout main
git pull --ff-only origin main
```

### 3.3 安装依赖

```bash
apt-get update
apt-get install -y postgresql postgresql-contrib curl ca-certificates xz-utils
```

服务器已自带 Node.js 22，可直接复用。

### 3.4 安装 Go 1.26.1

```bash
curl -L https://golang.google.cn/dl/go1.26.1.linux-amd64.tar.gz -o /tmp/go1.26.1.tar.gz
rm -rf /opt/go1.26.1 /opt/go
mkdir -p /opt
tar -C /opt -xzf /tmp/go1.26.1.tar.gz
mv /opt/go /opt/go1.26.1

export PATH=/opt/go1.26.1/bin:$PATH
go version
```

## 4. 数据库初始化

### 4.1 启动 PostgreSQL

由于该机器不是 systemd 启动环境，不能使用 `systemctl start postgresql`，改用：

```bash
pg_ctlcluster 14 main start
```

### 4.2 创建数据库用户与库

```bash
su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname = 'multica'\"" | grep -q 1 || \
  su - postgres -c "psql -c \"CREATE ROLE multica LOGIN PASSWORD 'multica';\""

su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname = 'multica'\"" | grep -q 1 || \
  su - postgres -c "psql -c \"CREATE DATABASE multica OWNER multica;\""
```

## 5. 环境变量配置

在 `/opt/multica/.env` 中至少保证以下值正确：

```env
POSTGRES_PORT=5432
DATABASE_URL=postgres://multica:multica@127.0.0.1:5432/multica?sslmode=disable
PORT=13080
FRONTEND_PORT=13030
FRONTEND_ORIGIN=http://127.0.0.1:13030
MULTICA_APP_URL=http://127.0.0.1:13030
LOCAL_UPLOAD_BASE_URL=http://127.0.0.1:13080
NEXT_PUBLIC_API_URL=http://127.0.0.1:13080
NEXT_PUBLIC_WS_URL=ws://127.0.0.1:13080/ws
GOOGLE_REDIRECT_URI=http://127.0.0.1:13030/auth/callback
FEISHU_APP_ID=<飞书应用 app id>
FEISHU_APP_SECRET=<飞书应用 secret>
FEISHU_REDIRECT_URI=http://127.0.0.1:13030/auth/callback
NEXT_PUBLIC_FEISHU_APP_ID=<飞书应用 app id>
JWT_SECRET=<随机 64 位 hex>
```

如果要直接用脚本写入飞书配置，可执行：

```bash
cd /opt/multica
make selfhost-feishu-configure \
  FEISHU_APP_ID='<飞书应用 app id>' \
  FEISHU_APP_SECRET='<飞书应用 secret>' \
  FEISHU_REDIRECT_URI='http://127.0.0.1:13030/auth/callback' \
  NEXT_PUBLIC_FEISHU_APP_ID='<飞书应用 app id>'

make selfhost-native-stop
make selfhost-native-backend
make selfhost-native-frontend
```

如果只是第一次补充 `NEXT_PUBLIC_FEISHU_APP_ID`，而前端登录页仍未出现飞书按钮，说明当前 `.next` 产物还是旧 bundle，需要重新执行前端构建，或从本地把新的 `apps/web/.next` 运行产物重新发布到远端，再重启前端。

说明：

- 当前未接入正式邮件服务时，登录可使用开发验证码 `888888`
- `JWT_SECRET` 不能保留默认值 `change-me-in-production`

## 6. 安装前端依赖

```bash
cd /opt/multica
corepack enable
npm config set registry https://registry.npmmirror.com
corepack prepare pnpm@10.28.2 --activate
pnpm install --registry https://registry.npmmirror.com
```

## 7. 迁移数据库并构建服务

### 7.1 执行数据库迁移

```bash
cd /opt/multica/server
/opt/go1.26.1/bin/go run ./cmd/migrate up
```

### 7.2 构建后端

```bash
cd /opt/multica/server
/opt/go1.26.1/bin/go build -o bin/server ./cmd/server
```

### 7.3 构建前端

```bash
cd /opt/multica
pnpm --filter @multica/web build
```

### 7.4 如远端源码无法稳定重建，可改为构建产物发布

本次服务器实际落地时，发现 `/opt/multica` 里的远端源码与本地开发分支不完全一致，直接在远端重编会撞到生成代码不匹配的问题。

因此可采用以下替代方案：

1. 本地先编译 Linux amd64 后端二进制
2. 本地先执行 Web 构建
3. 将后端二进制和前端 `.next` 运行时产物同步到远端
4. 远端只负责读取 `.env` 并启动服务

后端本地构建示例：

```bash
cd multica/server
GOOS=linux GOARCH=amd64 GOTOOLCHAIN=auto go build -o /tmp/multica-server-linux-amd64 ./cmd/server
```

前端运行时目录同步时，建议只同步生产运行所需内容，不要把 `.next/dev` 一并传到服务器。

## 8. 启动服务

### 8.1 启动后端

推荐直接使用仓库内脚本：

```bash
cd /opt/multica
make selfhost-native-backend
```

等价的底层命令是：

```bash
cd /opt/multica
export PATH=/opt/go1.26.1/bin:$PATH
set -a
. ./.env
set +a
exec ./server/bin/server
```

### 8.2 启动前端

推荐直接使用仓库内脚本：

```bash
cd /opt/multica
make selfhost-native-frontend
```

等价的底层命令是：

```bash
cd /opt/multica/apps/web
HOSTNAME=127.0.0.1 PORT=13030 exec pnpm exec next start
```

## 9. 验证步骤

### 9.1 后端健康检查

```bash
curl http://127.0.0.1:13080/health
```

预期返回：

```json
{"status":"ok"}
```

### 9.2 前端首页

```bash
curl -I http://127.0.0.1:13030
```

预期返回 `200` 或 `307` / `308` 等正常页面跳转状态。

### 9.3 登录验证

浏览器打开前端地址后：

- 输入任意邮箱
- 验证码输入 `888888`
- 成功进入系统即说明鉴权链路可用

### 9.4 飞书登录链路验证

当 `.env` 中已配置以下变量后：

- `FEISHU_APP_ID`
- `FEISHU_APP_SECRET`
- `FEISHU_REDIRECT_URI`
- `NEXT_PUBLIC_FEISHU_APP_ID`

可先执行：

```bash
cd /opt/multica
make selfhost-feishu-preflight
```

应额外检查：

- 浏览器实际打开 `/login` 时可看到 `Continue with Feishu`
- `make selfhost-feishu-preflight` 不再提示缺少环境变量，并能识别前端 bundle 已包含飞书登录 UI
- `POST /auth/feishu` 不再返回 `503 Feishu login is not configured`
- 完成授权后可正常回到 `/auth/callback`
- Desktop 登录入口可通过 `provider=feishu` 走同一套回调链路

## 10. 建议的后台守护方式

当前建议使用 `tmux` 或 `nohup` 先保证可运行；后续再补 `supervisord` 或 `pm2`。

### 10.1 使用 tmux

```bash
tmux new -s multica-backend
# 在会话里启动 backend

tmux new -s multica-frontend
# 在会话里启动 frontend
```

### 10.2 查看日志

```bash
ps -ef | grep multica
ss -lntp | grep -E '13030|13080|5432'
```

### 10.3 快速健康检查

```bash
cd /opt/multica
make selfhost-native-check
```

### 10.4 停止当前原生服务

```bash
cd /opt/multica
make selfhost-native-stop
```

### 10.5 飞书配置写入与预检

```bash
cd /opt/multica
make selfhost-feishu-configure \
  FEISHU_APP_ID='<飞书应用 app id>' \
  FEISHU_APP_SECRET='<飞书应用 secret>' \
  FEISHU_REDIRECT_URI='http://127.0.0.1:13030/auth/callback' \
  NEXT_PUBLIC_FEISHU_APP_ID='<飞书应用 app id>'

make selfhost-native-stop
make selfhost-native-backend
make selfhost-native-frontend
make selfhost-feishu-preflight
```

## 11. 反向代理与公网暴露建议

### 11.1 当前状态

当前服务器已经新增了一条 **Multica 专用的 Nginx 公网入口**：

- 公网入口：`http://115.190.235.210:14000`
- Nginx 配置文件：`/etc/nginx/conf.d/multica-14000.conf`
- 反向代理规则：
  - `/` -> `127.0.0.1:13030`
  - `/api/` -> `127.0.0.1:13080/api/`
  - `/auth/` -> `127.0.0.1:13080/auth/`
  - `/uploads/` -> `127.0.0.1:13080/uploads/`
  - `/ws` -> `127.0.0.1:13080/ws`

当前机器上已有业务占用 `13789`（OpenClaw / Macaron Hub），因此 Multica **不要复用**该端口。

### 11.2 当前公网方案的适用范围

这次落地的是 **临时 HTTP 公网方案**，主要用于：

- 让 Web 端不再依赖 SSH 隧道访问
- 验证单端口反代是否工作正常
- 为后续 HTTPS 与正式飞书回调改造铺路

已同步到远端 `.env` 的关键值：

```env
FRONTEND_ORIGIN=http://115.190.235.210:14000
MULTICA_APP_URL=http://115.190.235.210:14000
LOCAL_UPLOAD_BASE_URL=http://115.190.235.210:14000/uploads
ALLOWED_ORIGINS=http://115.190.235.210:14000,http://127.0.0.1:13030
REMOTE_API_URL=http://127.0.0.1:13080
NEXT_PUBLIC_API_URL=
NEXT_PUBLIC_WS_URL=
FEISHU_REDIRECT_URI=http://115.190.235.210:14000/auth/callback
```

其中：

- `NEXT_PUBLIC_API_URL` 留空，表示浏览器走同源 `/api` 与 `/auth`
- `NEXT_PUBLIC_WS_URL` 留空，表示前端按页面 origin 自动推导 `/ws`
- Web 前端已经按上述公网设置重新 build 并重新发布到远端

### 11.3 还需要手工完成的网络项

虽然服务器上的 Nginx 已监听 `14000`，但从本地直接访问：

```bash
curl -I http://115.190.235.210:14000/login
```

仍然超时，说明 **火山云安全组或上层网络策略还没有放行 `14000/TCP`**。

因此还需要在云侧补做：

- 安全组入站放行：`TCP 14000`
- 如有额外网络 ACL / 防火墙策略，也要同步放行 `14000`

只要云侧放行完成，当前 Nginx 配置即可直接对公网提供访问。

### 11.4 后续升级为正式 HTTPS 的推荐方案

如果后续要正式接飞书公网回调，建议从当前 `14000` 临时方案升级为：

- 使用稳定域名
- 配置可信 TLS 证书
- 将入口切换到 HTTPS（可以继续保留高位端口，也可以后续迁移到标准入口）

升级后需要同步更新：

- `FRONTEND_ORIGIN`
- `MULTICA_APP_URL`
- `LOCAL_UPLOAD_BASE_URL`
- `ALLOWED_ORIGINS`
- `FEISHU_REDIRECT_URI`
- 前端 `.next` 构建产物

## 12. 已知坑位

### 12.1 Docker Compose 版本过旧

服务器上的 Compose 实现不支持 `docker-compose.selfhost.yml` 中的顶层 `name:` 字段，直接执行会报错：

```text
Additional property name is not allowed
```

### 12.2 Docker Hub 拉镜像超时

镜像拉取过程中会出现超时，例如：

```text
error pulling image configuration: download failed after attempts=6
```

因此首发部署不建议依赖上游 Docker Compose 流程。

### 12.3 该机不是 systemd 启动环境

不能使用：

```bash
systemctl start postgresql
```

应改为：

```bash
pg_ctlcluster 14 main start
```

### 12.4 macOS 打包同步时会带出 AppleDouble 文件

如果用 macOS `tar` 或 Finder 相关流程把构建产物同步到服务器，可能在远端生成 `._*` 文件，数据库迁移会把它们误识别为 SQL 文件。

清理方式：

```bash
cd /opt/multica
find . -name '._*' -delete
```

### 12.5 端口占用时不要误判成构建失败

如果后台旧进程还在，新的 backend / frontend 会直接报：

- `listen tcp :13080: bind: address already in use`
- `listen EADDRINUSE: address already in use :::13030`

先清理旧进程，再重启：

```bash
ss -lntp | grep -E '13030|13080'
kill <old_backend_pid> <old_frontend_pid>
```

## 13. 正式切换公网前的待办清单

- [x] 确认可用公网端口：`14000`
- [x] 配置 Nginx 反向代理：`/etc/nginx/conf.d/multica-14000.conf`
- [ ] 在火山云安全组放行 `14000/TCP`
- [ ] 配置 TLS 证书（当前仍是临时 HTTP 方案）
- [x] 将 `.env` 中的 `FRONTEND_ORIGIN`、`MULTICA_APP_URL`、`NEXT_PUBLIC_API_URL`、`NEXT_PUBLIC_WS_URL` 切换为公网方案
- [ ] 根据正式 HTTPS 地址再次调整 `FEISHU_REDIRECT_URI`
- [ ] 接入正式邮件服务或企业鉴权
- [ ] 关闭开发验证码依赖
- [ ] 补充守护进程配置与日志轮转

## 14. 回滚方式

如果构建失败或服务异常，可按以下方式回退：

```bash
pkill -f '/opt/multica/server/bin/server' || true
pkill -f 'next start' || true
cd /opt/multica
git checkout main
git pull --ff-only origin main
```

如果只是前端或后端单独更新失败，优先回退对应进程，不要直接清空数据库。
