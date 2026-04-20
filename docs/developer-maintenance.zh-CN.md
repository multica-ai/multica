# Multica 开发者维护文档（中文）

本文档面向 `mindverse-ltd/multica` 的维护者，重点说明：

- 如何同步上游开源仓库
- 如何在公司 fork 上开发新功能
- 如何提交 PR 与合并
- 如何避免把公司定制逻辑直接污染上游同步流程

## 1. 仓库关系建议

当前项目建议明确区分 3 个 Git remote，避免把公司交付分支误推到社区仓库：

- `origin`：个人 fork，用于个人备份、中转和临时验证
- `company`：公司 fork `mindverse-ltd/multica`，用于正式协作与 PR
- `upstream`：开源主仓库 `multica-ai/multica`，仅用于同步社区更新

配置示例：

```bash
git remote set-url origin git@github.com:<your-user>/multica.git
git remote add company git@github.com:mindverse-ltd/multica.git
git remote add upstream git@github.com:multica-ai/multica.git
```

检查：

```bash
git remote -v
```

预期：

```text
origin   git@github.com:<your-user>/multica.git
company  git@github.com:mindverse-ltd/multica.git
upstream git@github.com:multica-ai/multica.git
```

### 1.1 当前推荐推送策略

- 日常开发分支先推 `origin`
- 需要团队协作、验收、合并时，再推 `company`
- **不要默认把公司定制分支推到 `upstream`**
- 只有确认是通用能力、且已从公司私有上下文中抽离后，才考虑单独整理社区 PR

## 2. 分支策略建议

### 2.1 长期分支

- `main`：公司 fork 的稳定主分支

### 2.2 短期分支

- `feat/*`：功能开发
- `fix/*`：问题修复
- `docs/*`：文档更新
- `chore/*`：维护性改动

示例：

```bash
git checkout -b feat/feishu-enterprise-auth
git checkout -b docs/deployment-guide-zh
```

## 3. 与上游同步的标准流程

### 3.1 拉取最新信息

```bash
git fetch origin
git fetch upstream
```

### 3.2 在本地切回主分支

```bash
git checkout main
```

### 3.3 先备份当前主分支（可选但推荐）

```bash
git branch backup/main-$(date +%Y%m%d-%H%M%S)
```

### 3.4 将上游主分支快进或合并到公司主分支

如果公司主分支没有额外提交，优先用快进：

```bash
git merge --ff-only upstream/main
```

如果公司主分支已有公司定制提交，则使用普通 merge：

```bash
git merge upstream/main
```

同步完成后建议先推到个人 fork 观察，再推到公司 fork：

```bash
git push origin main
git push company main
```

### 3.5 解决冲突

重点检查以下目录：

- `server/`：后端接口和鉴权逻辑
- `apps/web/`：登录页与回调页
- `packages/core/`：API 客户端与状态管理
- `packages/views/`：登录 UI
- `docs/`：自定义中文文档

### 3.6 运行验证

```bash
pnpm install
pnpm test
cd server && go test ./...
```

如果只做最小验证，至少执行：

```bash
cd server && go test ./...
```

以及手动验证登录流程。

### 3.7 推送到公司 fork

```bash
git push company main
```

## 4. 日常开发新功能流程

### 4.1 基于最新主分支创建功能分支

```bash
git checkout main
git pull --ff-only origin main
git checkout -b feat/your-feature-name
```

### 4.2 开发时的原则

- 公司定制逻辑尽量收敛在独立文件或独立配置项中
- 不要把公司内部地址、密钥、账号写死进仓库
- 不要直接在 `main` 上开发
- 需要改动鉴权时，优先保证上游同步成本最低

### 4.3 提交前检查

```bash
git status
pnpm test
cd server && go test ./...
```

### 4.4 推送功能分支

建议先推个人 fork，再推公司 fork：

```bash
git push -u origin feat/your-feature-name
git push -u company feat/your-feature-name
```

### 4.5 发起 PR

PR 目标仓库：

- 公司交付与验收：提到 `mindverse-ltd/multica:main`
- 如果是可回馈开源社区的通用能力，再新开一个独立分支，单独整理成上游 PR
- **不要直接拿公司交付分支向 `upstream` 发 PR**

## 5. 公司 fork 与上游的边界建议

建议把改动分成两类：

### 5.1 可上游化改动

这类改动应该尽量保持通用、可配置，未来可以反向提 PR 给 `multica-ai/multica`：

- 更好的部署兼容性
- 更好的自部署文档
- 可配置鉴权 provider 抽象
- 更稳健的登录回调处理

### 5.2 公司私有改动

这类改动建议只保留在公司 fork：

- 飞书企业身份体系对接
- 公司内网地址、SSO 域名、租户配置
- 企业内部通知、审计、权限约束
- 与公司内部平台的特定集成
- 面向当前服务器环境的临时公网入口与运维脚本

## 6. 推荐的冲突控制方式

### 6.1 尽量避免直接改上游核心流程的硬编码

例如：

- 不要把 Google OAuth 直接替换成飞书 OAuth
- 应改成“多 provider 可选”，再把飞书作为新增 provider 接进去

### 6.2 新增而不是覆盖

推荐：

- 新增 `FeishuLogin` handler
- 新增 provider 配置项
- 新增用户字段或身份映射表

不推荐：

- 直接删除现有邮箱验证码登录
- 直接删除现有 Google 登录流程

## 7. 建议的 PR 模板内容

每个 PR 最少写清楚：

- 改了什么
- 为什么改
- 是否影响登录/鉴权
- 是否影响数据库 schema
- 如何验证
- 是否影响上游同步

示例：

```text
## 变更说明
- 新增飞书企业登录入口
- 保留原邮箱验证码登录
- 新增用户企业身份映射表

## 风险
- 影响登录链路
- 涉及数据库迁移

## 验证
- 本地验证码登录通过
- 飞书授权回调通过
- 旧用户仍可正常登录
```

## 8. 当前服务器运维注意事项

当前测试/交付服务器：

- SSH：`root@115.190.235.210:51365`
- 公网入口：`http://115.190.235.210:14000`
- 代码目录：`/opt/multica`

### 8.1 当前运行方式

当前服务器不是标准 systemd 启动环境，Multica 采用“宿主机 PostgreSQL + 原生 backend/frontend + Nginx 反代”的方式运行：

- PostgreSQL：本机 `127.0.0.1:5432`
- Backend：本机 `127.0.0.1:13080`
- Frontend：本机 `127.0.0.1:13030`
- Nginx 公网入口：`0.0.0.0:14000`

### 8.2 重启后恢复机制

服务器重启后，宿主机 PostgreSQL 不会自动恢复到 Multica 所需状态，因此已经补了：

- 启动脚本：`scripts/selfhost-native-bootstrap.sh`
- 远端定时项：`@reboot /opt/multica/scripts/selfhost-native-bootstrap.sh >> /opt/multica/logs/bootstrap.log 2>&1`

如果重启后再次出现 `502 Bad Gateway`，优先检查：

```bash
pg_lsclusters
curl -I http://127.0.0.1:13080/health
curl -I http://127.0.0.1:13030/login
curl -I http://127.0.0.1:14000/login
```

### 8.3 飞书回调路由注意事项

公网入口下，飞书回调页必须走前端页面，而不是后端 API。当前 Nginx 必须满足：

- `location = /auth/callback` -> `127.0.0.1:13030/auth/callback`
- `location /auth/` -> `127.0.0.1:13080/auth/`

如果把 `/auth/callback` 一并转发到后端，会出现飞书授权后回跳 `404 page not found`。

## 9. 推荐的同步节奏

- 上游活跃阶段：每周同步 1 次
- 鉴权改造进行中：每次大改前先同步一次
- 上线前：做一次完整同步 + 回归测试

## 10. 故障排查建议

### 10.1 同步后登录坏了

优先检查：

- `server/internal/handler/auth.go`
- `apps/web/app/(auth)/login/page.tsx`
- `apps/web/app/auth/callback/page.tsx`
- `packages/views/auth/login-page.tsx`
- `packages/core/api/client.ts`

### 10.2 同步后前端编译失败

优先检查：

- `pnpm-lock.yaml`
- `package.json`
- `apps/web/package.json`
- `packages/*/package.json`

### 9.3 同步后后端编译失败，但业务源码看起来没问题

优先检查是否是“生成代码不同步”而不是业务逻辑本身出错：

- `server/pkg/db/generated/*.go`
- `server/pkg/db/queries/*.sql`
- `sqlc.yaml` 或对应生成配置
- 最近新增的 migration 是否已经在本地重新生成过 sqlc 产物

这次 Multica 远端重建时就出现了类似情况：远端源码树与本地开发分支不一致，导致某些 generated model 与 query 期望字段对不上。遇到这种情况不要先怀疑飞书逻辑本身，应该先确认源码版本与生成产物是否成套。

### 9.4 远端修复的优先顺序

如果远端服务已经在跑，但远端源码无法立即重建，推荐按以下顺序恢复：

1. 保住数据库和 `.env`
2. 先用本地验证通过的后端 Linux 二进制替换远端 `server/bin/server`
3. 再同步前端生产构建产物
4. 最后再处理远端源码树与上游同步问题

### 9.5 数据库迁移冲突

优先检查：

- `server/migrations/`
- 是否重复定义字段
- 是否迁移顺序编号冲突

## 10. 飞书企业登录开发时的特别要求

在做飞书企业登录改造时，建议额外遵守以下规则：

- 保留邮箱验证码登录作为兜底入口
- 用户表不要只靠邮箱唯一标识企业身份
- 优先新增“外部身份映射”而不是污染现有用户主表语义
- 所有飞书配置必须走环境变量
- 前后端都要支持 provider 可扩展，而不是只支持单一飞书

## 11. 推荐命令清单

### 同步上游

```bash
git fetch upstream
git checkout main
git merge upstream/main
git push origin main
```

### 开发新功能

```bash
git checkout main
git pull --ff-only origin main
git checkout -b feat/your-feature
```

### 提交代码

```bash
git add .
git commit -m "feat: add your feature"
git push -u origin feat/your-feature
```

### 远端原生部署常用命令

```bash
make selfhost-native-backend
make selfhost-native-frontend
make selfhost-native-check
make selfhost-native-stop
make selfhost-feishu-configure FEISHU_APP_ID=... FEISHU_APP_SECRET=... FEISHU_REDIRECT_URI=... NEXT_PUBLIC_FEISHU_APP_ID=...
make selfhost-native-stop
make selfhost-native-backend
make selfhost-native-frontend
make selfhost-feishu-preflight
# 预检会同时检查 env、前端 bundle 是否包含飞书登录 UI，以及 /auth/feishu 是否仍返回 503
# 如果补了 NEXT_PUBLIC_FEISHU_APP_ID 但按钮还没出现，通常需要重新构建或重新发布前端 .next 产物
```

## 12. 维护结论

对于 `mindverse-ltd/multica`，最佳实践不是“长期魔改 main”，而是：

- 主分支尽量贴近上游
- 企业定制尽量模块化
- 登录体系尽量 provider 化
- 文档、部署、企业集成在公司 fork 中沉淀

这样后续同步上游成本最低，也最适合持续维护。