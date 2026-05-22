# Multica 本地开发部署排障指南

## 问题现象

在中国大陆网络环境下，本地开发部署 multica 项目时，依次遇到以下问题：
1. Docker Hub 拉取 pgvector 镜像超时（`i/o timeout`）
2. pnpm install 长时间挂起无响应
3. Go 服务端不加载 `.env` 文件中的环境变量
4. 验证码登录失败（`invalid or expired code`）
5. 配置 `~/.docker/daemon.json` 镜像加速无效

## 背景

multica 是 Go 后端 + Next.js 前端项目，本地开发模式需要：
- Docker 仅运行 PostgreSQL（pgvector/pgvector:pg17）
- Go 后端和 Node 前端在宿主机直接运行
- 通过邮件验证码登录，本地开发可使用固定验证码绕过

## 问题一：Docker Hub 镜像拉取超时

### 根因

中国大陆网络无法直连 Docker Hub（`dial tcp 157.240.2.36:443: i/o timeout`），需要配置镜像加速。

### ⚠️ 注意事项

**Colima 的 Docker daemon 不读宿主机 `~/.docker/daemon.json`！**

Colima 运行在独立虚拟机中，拥有自己的 Docker daemon 配置。修改宿主机的 `~/.docker/daemon.json` 不会生效。

### 修复方式

必须 SSH 进 Colima 虚拟机修改其内部配置：

```bash
# 1. SSH 进入 Colima VM
colima ssh

# 2. 编辑 Docker daemon 配置
sudo tee /etc/docker/daemon.json << 'EOF'
{
  "registry-mirrors": [
    "https://docker.1ms.run",
    "https://docker.xuanyuan.me",
    "https://docker.mirrors.ustc.edu.cn/",
    "https://hub-mirror.c.163.com/"
  ]
}
EOF

# 3. 重启 Docker
sudo systemctl restart docker

# 4. 退出 VM
exit
```

镜像加速地址可能随时失效，如遇超时需更换可用镜像。推荐优先尝试 `docker.1ms.run` 和 `docker.xuanyuan.me`。

### 排查步骤

1. `docker pull pgvector/pgvector:pg17` 观察是否超时
2. 在 Colima VM 内 `cat /etc/docker/daemon.json` 确认配置生效
3. `docker info | grep -A5 "Registry Mirrors"` 验证镜像列表

---

## 问题二：Go 服务端不加载 .env 文件

### 根因

Go 的 `go run` **不会自动加载 `.env` 文件**。与 Node.js 生态（Next.js、dotenv）不同，Go 程序只读系统环境变量，不会扫描同目录的 `.env`。

### ⚠️ 注意事项

**即使 `.env` 文件就在项目根目录，且 `go run` 在 `server/` 子目录执行，环境变量也不会被注入。** 必须显式 source 并 export。

### 修复方式

```bash
cd server
set -a && source ../.env && set +a && go run ./cmd/server
```

- `set -a`：自动 export 所有变量
- `source ../.env`：加载 .env 中的变量到当前 shell
- `set +a`：取消自动 export

也可用 `make server`（Makefile 内已处理环境变量加载）。

### 排查步骤

1. 启动 server 后观察日志，如 `MULTICA_DEV_VERIFICATION_CODE is enabled` 说明变量已加载
2. 若日志显示 `[DEV] Verification code for xxx: 298372`（随机6位数），说明变量未加载
3. 在启动命令前加 `env | grep MULTICA_DEV` 确认变量存在

---

## 问题三：验证码登录失败（invalid or expired code）

### 根因

这是问题二的直接后果。`MULTICA_DEV_VERIFICATION_CODE=888888` 未被加载进服务端进程，导致：
- 服务端生成随机验证码（打印到 stdout）
- 用户输入 888888 时，与随机码不匹配，返回 `invalid or expired code`

### 修复方式

同问题二，确保环境变量正确加载。确认标志是日志中出现：
```
MULTICA_DEV_VERIFICATION_CODE is enabled
```

### ⚠️ 注意事项

- `MULTICA_DEV_VERIFICATION_CODE` 仅在 `APP_ENV` 非生产环境时生效
- 无 `RESEND_API_KEY` 时，验证码会打印到 server stdout，可从终端查看

---

## 问题四：go run 在项目根目录报错

### 根因

`go run` 必须在包含 `go.mod` 的目录下执行。项目根目录有 `.git/config` 但没有 `go.mod`，Go 无法确定模块边界。

```
go: cannot find main module, but found .git/config
```

### 修复方式

```bash
cd server && go run ./cmd/server
# 而不是从项目根目录：go run ./server/cmd/server
```

---

## 问题五：pnpm install 挂起

### 现象

`pnpm install` 长时间无输出，可能卡在依赖下载。

### 修复方式

1. `Ctrl+C` 或 `kill` 终止进程
2. 重新执行 `pnpm install`
3. 如反复卡住，尝试设置 npm 镜像：`pnpm config set registry https://registry.npmmirror.com`

---

## 快速启动命令参考

```bash
# 1. 启动 PostgreSQL（首次需先 colima start）
make db-up

# 2. 启动后端（从 server/ 目录，加载 .env）
cd server && set -a && source ../.env && set +a && go run ./cmd/server

# 3. 启动前端（从项目根目录）
pnpm dev:web

# 4. 构建 CLI 并配置 Agent
make build
server/bin/multica config set server_url http://localhost:8080
server/bin/multica config set app_url http://localhost:3000
server/bin/multica login
server/bin/multica daemon start
```
