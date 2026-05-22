# 发布约定

- 项目: Multica (fork)
- 仓库: wujie-agent/multica
- 默认发布分支: `main`

## 基本原则

1. **发布版本只有一个来源**：每次生产发布开始时先确定唯一的 `PROJECT_VERSION`，后续 backend、frontend、CLI、Gitee Release 都必须使用或校验同一个版本。
2. **Jenkins 负责执行已沉淀的CI/CD流程**：生产构建、推镜像、K3S rollout、CLI artifacts 发布到 OBS 都应由 Jenkins Job 执行；Agent 负责 plan、触发、等待、校验、汇总。
3. **Release 最后创建**：只有 backend、frontend、CLI 三个组件都发布成功，并且版本校验通过后，才能创建或更新 Gitee Release。
4. **不要让下游步骤自行猜版本**：尤其是 CLI Jenkins Job 不应只靠“最新 tag”推导发布版本；如果 Jenkins Job 暂未支持显式版本参数，发布流程必须在 Release 前校验 Jenkins 实际产物版本。


## Source of truth：仓库配置、ENV 与 Jenkins

1. **OPS / Agent 发布必须优先遵循本文件**：执行 Multica 发布前先阅读 `.ci/deploy.md`，再触发 Jenkins 或修改 K8S。不要绕过本文件直接根据历史命令发布。
2. **`k8s/bot/*.yaml` 管 desired baseline**：namespace、service、ingress、PVC、probes、resources、env 引用、secret key template 等基础配置由仓库维护。
3. **Jenkins 管 release artifact**：backend/frontend 的线上 image tag 由 Jenkins 发布流程构建并通过 `kubectl set image` 注入。仓库 manifest 里的 image tag 是模板/默认值，不代表线上当前版本。不要把 `0.BUILD_NUMBER`、rollout timestamp、deployment revision、resourceVersion、live deploy annotations 等 transient 字段同步回仓库。
4. **ENV 注入应显式流程化**：如果某次发布需要依赖 `.env.bot` 中声明的个性化 ENV，应在发布流程中先渲染 `k8s/bot/secret.yaml` 并 apply 到目标 namespace，再 rollout backend/frontend。Agent 不需要理解每个 ENV 的业务含义，但必须保证 `.env.bot` → `multica-bot-secrets` 的注入步骤可追踪、可验证。
5. **Secret value 不进仓库**：仓库只保存 `secret.yaml` 的 key template；真实值来自 Jenkins credentials / 发布环境的 `.env.bot`。
6. **live 经验回流要走小 diff**：如果线上手动调整被确认为 desired state（例如资源 limit），只同步对应字段，并说明原因；不要整份 live dump 覆盖仓库 YAML。

### ENV 注入建议流程

当发布需要刷新环境变量时，Jenkins 应执行等价流程：

```bash
set -a
source .env.bot
set +a
envsubst < k8s/bot/secret.yaml | kubectl apply -n multica-bot -f -
kubectl rollout restart deployment/backend -n multica-bot
kubectl rollout status deployment/backend -n multica-bot --timeout=180s
```

要求：

- `.env.bot` 由 Jenkins credentials 或受控发布环境提供，不提交到 Git。
- `secret.yaml` 新增/删除 key 必须走 PR。
- 发布报告需要记录是否刷新 ENV、使用的 Jenkins credentials ID / 环境来源、目标 namespace，以及 rollout 结果。
- 如果只发布镜像且 ENV 未变化，可以跳过 ENV apply，但要在发布报告中说明“ENV 未刷新”。

## 组件

### backend

- Jenkins job: `multica-backend-prod-pipeline`（prod环境）
- Jenkins job: `multica-backend-test-pipeline`（test环境）
- 部署目标: K3S
- 产出要求: Jenkins build URL、部署结果、镜像 tag、代码 revision

### frontend

- Jenkins job: `multica-frontend-prod-pipeline`（prod环境）
- Jenkins job: `multica-frontend-test-pipeline`（test环境）
- 部署目标: K3S
- 产出要求: Jenkins build URL、部署结果、镜像 tag、代码 revision

### cli

- Jenkins job: `Multica-CLI-Prod`（生产环境）
- Jenkins job: `Multica-CLI-Test`（测试环境）
- 部署目标: OBS (`https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json`)
- 产出要求: Jenkins build URL、CLI version、manifest URL、artifact/checksum 发布结果

#### 环境隔离

| 环境 | Jenkins Job | OBS Prefix | Manifest |
|------|------------|-----------|----------|
| test | `Multica-CLI-Test` | `cli-test` | `cli-test/manifest.json` |
| prod | `Multica-CLI-Prod` | `cli` | `cli/manifest.json` |

约束：

- test job 硬编码 `--prefix cli-test`，禁止写入 `cli/` prefix
- prod job 硬编码 `--prefix cli`，禁止从非 main/tag 发布
- test 发布不创建 Gitee Release
- 安装脚本通过 `--channel test` 参数或 `MULTICA_CHANNEL=test` 环境变量切换到测试通道
- prod job 发布后校验 `cli/manifest.json` version == CLI_VERSION

## 发布流程

顺序固定为：

```text
pre. 同步 main + 确定 PROJECT_VERSION
A. backend/frontend Jenkins prod pipeline 发布服务端到 K3S
B. Multica-CLI Jenkins Job 构建并发布 CLI artifacts 到 OBS
C. 汇总 A+B 的结果，创建/更新 Gitee Release
```

### pre. 同步 main + 确定 PROJECT_VERSION

```bash
cd ~/Desktop/harness/multica
git fetch origin main --tags
git checkout main
git pull --ff-only origin main
FULL_SHA=$(git rev-parse HEAD)
```

工作区如果有未提交变更，停止发布，不要自动 stash/reset。

生成项目版本时要避免 nested git-describe tag。示例：

```bash
PROJECT_VERSION=v$(git describe --tags --long \
  --match 'v[0-9]*' \
  --exclude 'v[0-9]*-[0-9]*-g*' \
  --exclude '*-k3s-*' \
  --exclude '*-wj*' \
  "$FULL_SHA")
```

规则：

- `PROJECT_VERSION` 必须对应本次发布的 `FULL_SHA`。
- 如果 `refs/tags/$PROJECT_VERSION` 不存在，创建 annotated tag 并 push 到 `origin`。
- 如果 tag 已存在，必须确认它指向同一个 `FULL_SHA`，否则停止发布。
- 不要基于旧的 git-describe-style 发布 tag 再 describe 出嵌套版本，例如 `v0.3.2-...-100-gxxxx-1-gyyyy`。

### A. 发布 backend/frontend 到 K3S

触发并等待：

1. `multica-backend-prod-pipeline`
2. `multica-frontend-prod-pipeline`

要求：

- 两个 job 都必须 `SUCCESS`。
- 记录 backend/frontend build URL、build number、镜像 tag、checkout revision。
- 如果任一服务端发布失败，停止流程，不触发 CLI，不创建 Release。

### B. 发布 CLI artifacts 到 OBS

触发 `Multica-CLI-Prod` 并等待 `SUCCESS`。

当前 live job 至少接受参数：

```text
Branch=main
```

目标状态：Jenkins Job 应支持显式传入发布版本，例如 `CLI_VERSION=$PROJECT_VERSION` 或 `ReleaseTag=$PROJECT_VERSION`。如果已支持，发布触发必须传入该参数，禁止让 Jenkins 自行猜版本。

如果 job 暂未支持显式版本参数，发布流程必须做强校验：

Jenkins console 中的版本需要出现并等于 `PROJECT_VERSION`，例如：

```text
version:   $PROJECT_VERSION
Building CLI artifacts ... $PROJECT_VERSION
```

OBS manifest 中的版本也必须等于 `PROJECT_VERSION`：

```bash
curl -fsSL https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json | jq -r .version
```

校验要求：

- Jenkins 实际构建的 CLI version 必须等于 `PROJECT_VERSION`。
- OBS `manifest.json.version` 必须等于 `PROJECT_VERSION`。
- 不相等时，停止流程，不创建/更新 Gitee Release；应转入修复 Jenkins Job 或重新触发 CLI 构建。

### C. Gitee Release

只有 A 和 B 都成功，且版本校验通过后，才创建或更新 Gitee Release。

Release 内容必须至少包含：

- `PROJECT_VERSION`
- `FULL_SHA`
- backend Jenkins build URL / build number / result
- frontend Jenkins build URL / build number / result
- CLI Jenkins build URL / build number / result
- CLI manifest URL 和实际 version
- previous release tag
- 主要变更摘要

测试环境发布不创建 Gitee Release。

## Release 规范

- 区分「官方上游变更」和「Fork 独有变更」。
- 每条 Fork 独有变更尽量挂 Gitee PR 链接和 Multica Issue 链接。
- previous release 取 Gitee Releases API 中最近一个有效 release tag；如果某个 release body 为空或明显是历史占位，需要继续向前找。
- Release 版本号必须等于发布流程 pre 阶段确定的 `PROJECT_VERSION`。
- Release 在 backend、frontend、cli 三个组件都发布成功后再创建或更新。
- Release 不是用来修正发布事实的地方；如果 Jenkins/OBS 实际版本和 `PROJECT_VERSION` 不一致，先修发布产物，再写 Release。
