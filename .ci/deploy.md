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
- 部署目标: OBS (`https://obs-multica.wujieai.com/cli/manifest.json`)
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
PROJECT_VERSION=$(git describe --tags --long \
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

#### 版本号倒退校验（强制 gate）

```bash
# 提取 base version（v0.3.6-845-gb05b01d1c → 0.3.6）
BASE_VERSION=$(echo "$PROJECT_VERSION" | grep -oP '^v?\K\d+\.\d+\.\d+')
PREV_RELEASE=$(git tag --sort=-v:refname \
  --list 'v[0-9]*.[0-9]*.[0-9]*-[0-9]*-g*' \
  --merged origin/main | head -1)
PREV_BASE=$(echo "$PREV_RELEASE" | grep -oP '^v?\K\d+\.\d+\.\d+')

if [ -n "$PREV_BASE" ] && [ "$(printf '%s\n' "$PREV_BASE" "$BASE_VERSION" | sort -V | tail -1)" != "$BASE_VERSION" ]; then
  echo "ERROR: PROJECT_VERSION base ($BASE_VERSION) < previous release ($PREV_BASE). Version regression detected."
  echo "This means git describe chose an older base tag. Check that --exclude patterns are applied."
  exit 1
fi
```

校验不通过 → 立即停止发布，禁止进入后续 Jenkins 触发阶段。

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
curl -fsSL https://obs-multica.wujieai.com/cli/manifest.json | jq -r .version
```

校验要求：

- Jenkins 实际构建的 CLI version 必须等于 `PROJECT_VERSION`。
- OBS `manifest.json.version` 必须等于 `PROJECT_VERSION`。
- 不相等时，停止流程，不创建/更新 Gitee Release；应转入修复 Jenkins Job 或重新触发 CLI 构建。

### C. Gitee Release

只有 A 和 B 都成功，且版本校验通过后，才创建或更新 Gitee Release。

Release 内容必须至少包含：

- 版本摘要（人话总结本次 release 的核心变化、对用户意味着什么；如果包含官方上游合入，必须总结覆盖范围内相关官方 Changelog，并带上本次 release 对应/覆盖到的官方基线版本锚点）
- `PROJECT_VERSION`
- `FULL_SHA`
- backend Jenkins build URL / build number / result / image tag / code revision
- frontend Jenkins build URL / build number / result / image tag / code revision
- CLI Jenkins build URL / build number / result / CLI version / code revision
- CLI manifest URL 和实际 version
- previous release tag
- 官方上游变更摘要（如果本次发布包含官方版本合入）
- Fork 独有变更摘要（每条尽量包含 Gitee PR 和 Multica Issue）
- 基础设施 / 发布流程变更（如果本次发布包含 ENV、K8S、Jenkins、OBS、backfill 等变化）
- 下载与安装信息（CLI / desktop / mobile 等客户端产物；没有客户端产物时明确写暂无）

测试环境发布不创建 Gitee Release。

## Release 规范

- 区分「官方上游变更」和「Fork 独有变更」。
- 每条 Fork 独有变更尽量挂 Gitee PR 链接和 Multica Issue 链接。
- previous release 取 Gitee Releases API 中最近一个有效 release tag；如果某个 release body 为空或明显是历史占位，需要继续向前找。
- Release 版本号必须等于发布流程 pre 阶段确定的 `PROJECT_VERSION`。
- Release 在 backend、frontend、cli 三个组件都发布成功后再创建或更新。
- Release 不是用来修正发布事实的地方；如果 Jenkins/OBS 实际版本和 `PROJECT_VERSION` 不一致，先修发布产物，再写 Release。
- Release 只能记录当前 release tag 覆盖范围内的 PR / commit；不要把 tag 之后合入的 PR 写进去。
- Release 是人类 changelog + 发布事实记录，不是原始流水日志；保留可追溯链接和关键校验结果，避免粘贴大段 console log。

### Release 撰写防呆规则

**必须遵守：**

- **禁止在正文中使用 `@` 前缀词。** Gitee 会将 Markdown 中的 `@xxx` 解析为 mention 通知，误触无关人员。如需提及 mention 功能，写作 `mention`（不带 `@`）。
- **所有 URL 使用 CDN 域名。** manifest 链接使用 `https://obs-multica.wujieai.com/cli/manifest.json`，不要使用旧 OBS 直链。
- **Release 正文生成后，逐行扫描确认无 `@` 字符**（代码块内除外）。如有漏网，替换后重新发布。

### Release 标准结构

生产 Release 默认使用以下结构；除非某节确实不适用，否则不要删节。

```markdown
# Multica Release <PROJECT_VERSION>

- **PROJECT_VERSION**: `<PROJECT_VERSION>`
- **FULL_SHA**: `<FULL_SHA>`
- **source branch**: `main`
- **previous release**: [`<PREVIOUS_RELEASE>`](https://gitee.com/wujie-agent/multica/releases/tag/<PREVIOUS_RELEASE>)

## 版本摘要

用 1-3 段人话说明本次 release 的核心变化和用户可感知价值，不要只罗列 PR 标题。Issue/PR 级别明细保留在后续 section，本节负责先让读者快速理解「这一版发生了什么」。

如果本次 release 覆盖官方上游版本合入：

- 明确本次 release 对应/覆盖到的官方基线版本，例如 `v0.3.8`。
- 总结该官方版本 Changelog 中和本次发布相关的核心变化。
- 带上对应官方基线版本锚点，例如 `[官方 Changelog](https://multica.ai/changelog#release-0-3-8)`。
- 注意：锚点必须对应本次 release 实际覆盖到的官方基线版本，不是官方网站当前最新版本。比如本次只覆盖到 `v0.3.8`，即使官方已经发布 `v0.3.12`，也只能链接 `#release-0-3-8`。

如果本次包含 Fork 独有变化，也在摘要中用一句话说明主要方向，例如发布可靠性、Runtime/Skills、私有 Gitee/Codex 集成等。

## 组件交付

| 组件 | Jenkins Build | 结果 | 产物版本 | 代码 Revision |
|------|---------------|------|----------|---------------|
| backend | [#<N>](<backend-build-url>) | ✅ SUCCESS | `<backend-image>:<tag>` | `<FULL_SHA>` |
| frontend | [#<N>](<frontend-build-url>) | ✅ SUCCESS | `<frontend-image>:<tag>` | `<FULL_SHA>` |
| CLI | [#<N>](<cli-build-url>) | ✅ SUCCESS | `<PROJECT_VERSION>` | `<short-sha>` |

## 发布验证

- backend/frontend checkout: `<FULL_SHA>` (`main`)
- backend rollout: ✅ `<rollout-result>` in namespace `<namespace>`
- frontend rollout: ✅ `<rollout-result>` in namespace `<namespace>`
- backend/frontend image tag: `<image-tag>`
- CLI manifest: [`<manifest-url>`](<manifest-url>)
- CLI manifest version: `<actual-version>` ✅ 与 `PROJECT_VERSION` 一致
- CLI OBS artifacts: ✅ `<platform-count>` platform packages published, checksums included in manifest, asset HEAD checks passed
- 生产入口: [`https://multica.wujieai.com`](https://multica.wujieai.com) ✅

## 官方上游变更

仅当本次发布包含官方上游合入时填写。按领域分组列 GitHub PR 链接和一句话摘要；本节保留 PR/commit 级别追溯，不替代顶部「版本摘要」。来源优先级：
1. `git log <previous-release-tag>..<PROJECT_VERSION>` 中的 GitHub PR merge commits
2. 官方 release/changelog
3. GitHub PR title/body

必须同时确认顶部「版本摘要」已经包含：
- 本次 release 覆盖到的官方基线版本
- 对应官方 Changelog 锚点（例如 `https://multica.ai/changelog#release-0-3-8`）
- 官方 Changelog 的人话总结

## Fork 独有变更

- **<变更标题>**
  - Issue: [OPE-XXX](https://multica.wujieai.com/openharness/issues/OPE-XXX)
  - PR: [!N](https://gitee.com/wujie-agent/multica/pulls/N)
  - 摘要: <一句话说明用户可感知变化或技术影响>

没有 Issue 的 PR 可以省略 Issue，但不能省略 PR。多个 PR / Issue 用逗号分隔。

## 基础设施 / 发布流程变更

仅当本次发布包含 ENV、K8S、Jenkins、OBS、backfill、release pipeline 等变化时填写。至少记录：

- ENV refreshed: `<yes/no>`
- source: `<Jenkins credentials ID / controlled env source>`
- namespace: `<namespace>`
- secret: `<secret-name> <configured/skipped>`
- deployment spec: `<applied/skipped>`
- rollout: `<result>`

## 下载与安装

- CLI manifest: [`<manifest-url>`](<manifest-url>)
- CLI version: `<PROJECT_VERSION>`
- Install:

```bash
curl -fsSL https://multica.wujieai.com/install.sh | sh
```

- Published CLI artifacts:
  - `darwin/amd64`
  - `darwin/arm64`
  - `linux/amd64`
  - `linux/arm64`
  - `windows/amd64`
  - `windows/arm64`
- Checksums: included in `manifest.json`
```

### Release 撰写前校验

写 Release 前必须做这几步，避免把错误事实固化到发布记录里：

```bash
# 1. 确认 tag 与 FULL_SHA 一致
git rev-parse <PROJECT_VERSION>
git rev-parse <FULL_SHA>

# 2. 列出当前 release 覆盖范围内的 commits / PR，严禁写入 tag 之后的 PR
git log --oneline <PREVIOUS_RELEASE>..<PROJECT_VERSION> --reverse

# 3. Fork PR 清单以 Gitee PR API / merge commit 为准
# 每个 Fork 变更都尽量补 PR + OPE issue；不能只凭记忆写。

# 4. 官方上游变更以 GitHub PR merge commits / 官方 changelog 为准
# 如果本次没有官方合入，明确跳过「官方上游变更」。
# 如果本次包含官方合入，确认顶部「版本摘要」带有本次覆盖到的官方基线版本锚点，
# 例如只覆盖到 v0.3.8 时使用 https://multica.ai/changelog#release-0-3-8，
# 不要误用官方当前最新版本锚点。

# 5. CLI manifest 必须 live 校验
curl -fsSL https://obs-multica.wujieai.com/cli/manifest.json | jq -r .version
```

最低验收：

- `PROJECT_VERSION`、Gitee release tag、CLI manifest version 三者一致。
- 组件交付表包含 backend / frontend / CLI 的 build URL、build number、result、产物版本、code revision。
- Release 顶部有「版本摘要」，用人话说明核心变化和用户可感知价值；不能只有 PR/Issue 明细。
- 如果包含官方上游合入，「版本摘要」已总结相关官方 Changelog，并链接到本次 release 覆盖到的官方基线版本锚点。
- 「官方上游变更」和「Fork 独有变更」已分开；无官方合入时说明跳过。
- Fork 变更不漏当前 tag 内的重要 PR，不混入 tag 之后的 PR。
- ENV / K8S / Jenkins / OBS / backfill 等基础设施变化有独立 section。
- 下载 section 只写真实已发布客户端产物；checksum 信息来自 manifest。
