# 发布约定

- 项目: Multica (fork)
- 仓库: wujie-agent/multica
- 默认发布分支: `main`

## 基本原则

1. **发布版本只有一个来源**：每次生产发布开始时先确定唯一的 `PROJECT_VERSION`，后续 backend、frontend、CLI、Gitee Release 都必须使用或校验同一个版本。
2. **Jenkins 负责有副作用的生产动作**：生产构建、推镜像、K3S rollout、CLI artifacts 发布到 OBS 都应由 Jenkins Job 执行；Agent 负责 plan、触发、等待、校验、汇总。
3. **Release 最后创建**：只有 backend、frontend、CLI 三个组件都发布成功，并且版本校验通过后，才能创建或更新 Gitee Release。
4. **不要让下游步骤自行猜版本**：尤其是 CLI Jenkins Job 不应只靠“最新 tag”推导发布版本；如果 Jenkins Job 暂未支持显式版本参数，发布流程必须在 Release 前校验 Jenkins 实际产物版本。

## 组件

### backend

- Jenkins job: `multica-backend-prod-pipeline`
- 部署目标: K3S
- 产出要求: Jenkins build URL、部署结果、镜像 tag、代码 revision

### frontend

- Jenkins job: `multica-frontend-prod-pipeline`
- 部署目标: K3S
- 产出要求: Jenkins build URL、部署结果、镜像 tag、代码 revision

### cli

- Jenkins job: `Multica-CLI`
- 部署目标: OBS (`https://multica.obs.cn-east-3.myhuaweicloud.com/cli/manifest.json`)
- 产出要求: Jenkins build URL、CLI version、manifest URL、artifact/checksum 发布结果

## 生产发布流程

顺序固定为：

```text
pre. 同步 main + 确定 PROJECT_VERSION
A. backend/frontend Jenkins prod pipeline 发布服务端到 K3S
C. Multica-CLI Jenkins Job 构建并发布 CLI artifacts 到 OBS
B. 汇总 A+C 的结果，创建/更新 Gitee Release
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

### C. 发布 CLI artifacts 到 OBS

触发 `Multica-CLI` 并等待 `SUCCESS`。

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

### B. Gitee Release

只有 A 和 C 都成功，且版本校验通过后，才创建或更新 Gitee Release。

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

## AutoPilot / Agent 职责边界

AutoPilot 可以作为发布入口，但它不应该内置一套独立发布逻辑。它的职责应收敛为：

1. 读取 webhook/body 中的 `environment`、`branch` 等少量输入。
2. 按本文件定位项目发布约定。
3. 调度合适的 OPS Agent / Jenkins 能力执行 plan、触发、等待、校验、汇总。
4. 把最终结果回写给用户。

OPS Agent 是发布编排的主要执行体：它应结合本文件、Jenkins skill、项目特定 workflow 来驱动发布。AutoPilot 不应绕过 OPS Agent 自行决定 tag、Jenkins 参数或 Release 内容。

## 待 OPS 沉淀项

以下改动属于流程能力建设，不在本文件内直接实现：

1. 为 `Multica-CLI` Jenkins Job 增加显式版本参数，如 `CLI_VERSION` / `ReleaseTag`。
2. 调整 Jenkins Job 内部逻辑：生产发布优先使用显式版本；不再只用 `git tag --sort=-v:refname | head -n 1` 猜版本。
3. 在 OPS Agent instruction / skills 中沉淀 Multica 发布 A → C → B 编排和版本校验。
4. 如有必要，新增 Multica-specific release workflow，例如 `multica-release plan/trigger-services/trigger-cli/publish-gitee-release`。
5. 明确 Gitee Release 阶段由 OPS/Jenkins 能力处理，并消费 A+C 的机器可读发布元数据。
