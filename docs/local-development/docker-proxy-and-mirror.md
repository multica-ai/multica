# 本地开发：Docker 代理与镜像加速说明

说明：在国内或受限网络环境下，`docker compose` 拉取官方或 GitHub Container Registry 镜像时可能出现超时或 `unexpected EOF` 等错误。下面提供可复现的修复步骤与验证方法，方便开发者在本地快速恢复自托管环境。

## 问题概述

- 场景：`docker compose -f docker-compose.selfhost.yml up -d` 拉取镜像失败（如 `pgvector/pgvector:pg17`、`ghcr.io/multica-ai/multica-web:latest`）
- 原因：Docker 引擎未配置合适的 registry mirror、DNS 或未正确使用本机代理；网络不稳定导致分段下载中断（`unexpected EOF`）。

## 方案（推荐顺序）

1. 在 Docker 引擎（daemon）层配置 `registry-mirrors`：

   - Windows/Docker Desktop：编辑或创建 `%programdata%\docker\config\daemon.json`（或通过 Docker Desktop 设置）
   - 示例：

   ```json
   {
     "registry-mirrors": [
       "https://registry.docker-cn.com",
       "https://hub-mirror.c.163.com"
     ],
     "dns": ["8.8.8.8", "114.114.114.114"]
   }
   ```

2. 如果使用系统代理（如 Clash、V2Ray），确保 Docker Desktop 将代理传递给 WSL2/引擎，或在 Docker Desktop 中配置 `HTTP_PROXY` 和 `HTTPS_PROXY`。

3. 对于拉取失败的镜像，先单独尝试 `docker pull`，必要时增加重试：

   ```bash
   docker pull ghcr.io/multica-ai/multica-web:latest || docker pull ghcr.io/multica-ai/multica-web:latest
   ```

4. 如果仍然失败，手动从可信来源下载镜像 tar（仅在极端受限网络下）并 `docker load`，或在 CI 中提前构建并推送到私有 registry。

## 验证方法

- 运行 `docker info`，确认 `Registry Mirrors` 已列出。
- 单独 `docker pull` 需要成功完成且镜像大小完整。
- 重新运行 `docker compose -f docker-compose.selfhost.yml up -d`，服务能正常启动且容器健康检查通过。

## 风险与注意事项

- 使用第三方镜像加速需信任镜像来源；敏感生产环境不建议长期依赖公共镜像加速器。
- 修改 DNS/registry-mirrors 可能影响镜像一致性；必要时回滚并使用官方源重新拉取确认差异。

## 我在本地的操作记录（示例）

1. 在 Windows 上编辑 `%programdata%\docker\config\daemon.json`，添加镜像加速与 DNS。
2. 重启 Docker Desktop，运行 `docker info` 验证。
3. 单独 `docker pull pgvector/pgvector:pg17`，成功后再 `docker compose -f docker-compose.selfhost.yml up -d`。

---

如果你希望我把这个文档进一步转成 PR 模板（包含 Issue 链接和变更说明），我可以继续在分支中添加 `CHANGELOG.md` 条目并发起 PR。
