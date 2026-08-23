# Fork 维护指南

本仓库是 `multica-ai/multica` 的自维护 fork。版本号**跟上游走**，不另开产品线。
fork 只叠我们需要的 patch（CI、自托管优化、尚未上游的适配），rebase 到上游
之上保持线性历史。

家里的控制平面 / 执行节点 pin 在 `~/.config/nixos`，不在本仓库。本仓库负责
**出包**：GitHub Release（CLI）和 GHCR（backend / web / Helm chart）。

## 仓库布局

| Remote | 用途 |
| --- | --- |
| `upstream` | `multica-ai/multica`，同步来源 |
| `origin` | `xesrevinu/multica`，自维护 fork |

本地 `main` 跟踪 `upstream/main`，我们的 commit 永远 rebase 到上游之上，不做
merge 上游。

查看当前定制（没有 commit 时输出为空）：

```sh
git log --oneline upstream/main..main
```

日常不要 `git fetch upstream --tags`。上游 tag 与 origin 同名（`v0.4.32`）但
SHA 不同（我们的 tag 含 fork commit）。拉取上游请：

```sh
git fetch upstream --prune
# 需要对齐某个上游发行时再取那个 tag：
git fetch upstream tag v0.4.32
```

## 版本

- 发行 tag **与上游相同**：`v0.4.32`、`v0.4.33`。
- 不要发明 `v0.4.33` 或 `v0.4.32-kee.1` 这种独立号。
- 每个 fork 发行 = 上游该 tag 的树 + 当时的 fork commit。
- origin 上的 `v0.4.32` 指向我们的 commit，不是上游的同名 tag。
- 已经发布过的 tag **不要 force 改 SHA**（GHCR digest、nix hash、CLI 校验都会坏）。
  同一上游版本必须再发一版时，用包装修订 `v0.4.32-1`（semver pre-release），
  这不是独立产品版本，只是「同一上游号的第 N 次重建」。能等上游下一号就等。

## 产物去哪

| 产物 | 地址 |
| --- | --- |
| backend 镜像 | `ghcr.io/xesrevinu/multica-backend:<tag>` |
| web 镜像 | `ghcr.io/xesrevinu/multica-web:<tag>` |
| Helm chart | `oci://ghcr.io/xesrevinu/charts/multica`（chart version = tag 去掉 `v`） |
| CLI tarball | `https://github.com/xesrevinu/multica/releases/download/<tag>/multica-cli-<ver>-<os>-<arch>.tar.gz` |

`<ver>` 是 tag 去掉 `v`。例如 tag `v0.4.32` →
`multica-cli-0.4.32-darwin-arm64.tar.gz`、`multica-cli-0.4.32-linux-amd64.tar.gz`。

Electron Desktop 不发（家里用 Pake 包 Web）。Homebrew tap 不发（那是上游的）。

第一次 GHCR 推送默认 private。workflow 会尝试改 public；`GITHUB_TOKEN` 往往没
权限，需在 UI 点一次：

- <https://github.com/users/xesrevinu/packages/container/multica-backend/settings>
- <https://github.com/users/xesrevinu/packages/container/multica-web/settings>
- <https://github.com/users/xesrevinu/packages/container/charts%2Fmultica/settings>

Package visibility → Public。之后 k8s 才能免 token 拉镜像。

## 发一版

先让 `main` 含当前 fork commit，再切到**上游 tag** 把 fork commit cherry-pick
上去（不要把 `upstream/main` 上尚未 release 的 commit 算进这个号）。

```sh
git fetch upstream --prune
git fetch upstream tag v0.4.32

# 1. fork commit 已在 main 上
git log --oneline upstream/main..main

# 2. 从上游 tag 长出本号发行（只带 fork commit，不要夹带未 release 的 upstream/main）
git switch -C release-v0.4.32 v0.4.32
git cherry-pick $(git log --reverse --format=%H upstream/main..main)

# 3. 推 main（日常）和 tag（触发 Release workflow）
git switch main
git push origin main
git push origin "$(git rev-parse release-v0.4.32)":refs/tags/v0.4.32
```

不要 `git push origin v0.4.32`：本地这个名字通常还指向上游 SHA，会把「没
有 fork CI」的树推上去，GoReleaser / Helm 不会跑。

看 <https://github.com/xesrevinu/multica/actions>。verify（测试 + govulncheck）
过了才出镜像、CLI、chart。带连字符的 tag（`v0.4.32-1`）不会打 `latest` 镜像。

## 同步上游

```sh
git fetch upstream --prune
git log --oneline upstream/main ^main
git rev-list --left-right --count upstream/main...main

git diff --name-only upstream/main..main | sort > /tmp/ours.txt
git diff --name-only $(git merge-base upstream/main main)..upstream/main | sort > /tmp/theirs.txt
comm -12 /tmp/ours.txt /tmp/theirs.txt

git branch -f main-prerebase-backup main
git -c commit.gpgsign=false rebase --empty=drop upstream/main
```

上游若落地了等价功能，对应 commit 丢掉。确认后：

```sh
git branch -D main-prerebase-backup
git push --force-with-lease origin main
```

## 家里怎么吃（nixos）

配置在 `~/.config/nixos`，不跟本仓库一起提交：

| 文件 | 作用 |
| --- | --- |
| `services/k8s/multica/values.yaml` | `ghcr.io/xesrevinu/multica-{backend,web}` + tag |
| `services/k8s/multica/deploy.sh` | Helm upgrade；默认拉 fork 的 OCI chart |
| `packages/multica-cli-selfhost.nix` | Mac CLI：fork Release tarball + SRI hash |
| `modules/home/terminal.nix` | LaunchAgent `org.nix-community.home.multica-daemon` |
| `network/vmiss-hk/scripts/deploy-multica-client.sh` | VMISS CLI URL（已登录后不要重跑，只换二进制） |
| `network/vmiss-hk/systemd/multica-daemon.service` | VMISS unit（应设 `MULTICA_DAEMON_AUTO_UPDATE=false`） |

升级顺序：先 k8s（backend 会 `migrate up`），再 Mac / VMISS CLI。postgres
镜像不要顺手改。官方制品和 fork 制品不要混协议。

生产 deploy 用已发布的 OCI chart，不要默认吃 `~/Code/wip/multica` 工作树。
调试 chart 时：`MULTICA_USE_LOCAL_CHART=1 ./deploy.sh`。

## 本机构建（应急，CI 挂了才用）

OrbStack 与 Mac 共用 Docker，本机 `linux/arm64` 即可，不必先推 GHCR。

```sh
VER=v0.4.32
COMMIT=$(git rev-parse --short HEAD)

docker build --platform linux/arm64 \
  --build-arg VERSION="$VER" --build-arg COMMIT="$COMMIT" \
  -t "ghcr.io/xesrevinu/multica-backend:$VER" -f Dockerfile .

docker build --platform linux/arm64 \
  --build-arg NEXT_PUBLIC_APP_VERSION="$VER" \
  -t "ghcr.io/xesrevinu/multica-web:$VER" -f Dockerfile.web .
```

CLI：

```sh
cd server
go build -ldflags "-s -w -X main.version=${VER#v} -X main.commit=$COMMIT" \
  -o bin/multica ./cmd/multica
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.version=${VER#v} -X main.commit=$COMMIT" \
  -o bin/multica-linux-amd64 ./cmd/multica
```

## 我们准备改什么

有 patch 再按主题写到这里，并标冲突风险。候选方向（尚未落地）：

- OpenCode V2 适配：CLI 参数、错误事件、MCP schema 与上游仍不兼容。改动会落在
  `server/pkg/agent/opencode.go`、`server/pkg/agent/opencode_mcp.go`、
  `server/internal/daemon/agents_probe.go`。

拆 commit 时保持「schema / 协议 → backend → CLI」顺序。

## 已知陷阱

- **官方版与 fork 版不要混协议。** 改过 daemon API 就必须同时换 backend 和两边 CLI。
- **不要对已登录的 VMISS 重跑 `deploy-multica-client.sh`。** 只换二进制并重启 unit。
- **不要 `git push origin vX.Y.Z` 若本地 tag 仍指向上游 SHA。** 用
  `git push origin <fork-sha>:refs/tags/vX.Y.Z`。
- **Helm 不要默默用本地 chart。** `deploy.sh` 若发现工作树就 upgrade，未提交改动会进集群。
- **GPG 签名。** rebase 用 `git -c commit.gpgsign=false`。
- **不要把 fork 推到 `multica-ai/multica`。** `origin` 是 `xesrevinu/multica`。
