# Fork 维护指南

本仓库是 `multica-ai/multica` 的自维护 fork。源码跟上游，发行号走**自己的
42.x 线**，不跟上游 0.x / 未来 1.x 抢名字。fork 只叠我们需要的 patch（CI、
自托管优化、尚未上游的适配），rebase 到上游之上保持线性历史。

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

日常 `git fetch upstream --prune` 即可。需要对齐某个上游发行时再取那个 tag：

```sh
git fetch upstream tag v0.4.32
```

origin 只打我们的 `v42.*`。不要在 origin 上打上游同名 tag（`v0.4.32` 那次是
流水线冒烟，不当家里的 pin）。

## 版本（42.x epoch）

上游现在是 0.4.x，以后可能到 1.x。我们从 **42.0.0** 起号，避免和上游发行号
撞车，也避免 0.0.1 那种「原型」观感。

| 怎么升 | 何时 |
| --- | --- |
| `42.0.x` | 同一上游基线上的自家修订（连发功能就 +patch） |
| `42.x.0` | rebase 到上游新发行 |
| `43.0.0` | 协议或数据迁移不兼容 |

已经发布的 tag **不要 force 改 SHA**。要再发就升我们的号，不要覆盖。

### 对照表

| 我们的发行 | 上游基线 | 上游 commit | 我们多了什么 |
| --- | --- | --- | --- |
| 42.0.0 | v0.4.32 | ad64e0f800 | fork 发版 CI（CLI / Helm 发到本仓库） |

发一版就加一行。GitHub Release body 第一行也写 `Based on upstream v0.4.32`。

## 产物去哪

| 产物 | 地址 |
| --- | --- |
| backend 镜像 | `ghcr.io/xesrevinu/multica-backend:v42.0.0` |
| web 镜像 | `ghcr.io/xesrevinu/multica-web:v42.0.0` |
| Helm chart | `oci://ghcr.io/xesrevinu/charts/multica`（chart version = `42.0.0`） |
| CLI tarball | `https://github.com/xesrevinu/multica/releases/download/v42.0.0/multica-cli-42.0.0-<os>-<arch>.tar.gz` |

Electron Desktop 不发（家里用 Pake 包 Web）。Homebrew tap 不发（那是上游的）。

`GITHUB_TOKEN` 改不了 GHCR 可见性。第一次发完在 UI 点 Public：

- <https://github.com/users/xesrevinu/packages/container/multica-backend/settings>
- <https://github.com/users/xesrevinu/packages/container/multica-web/settings>
- <https://github.com/users/xesrevinu/packages/container/charts%2Fmultica/settings>

之后 k8s 才能免 token 拉镜像。

## 发一版

功能堆在 `main`。决定家里要用了，才从**上游发行 tag** 长出我们的号（不要把
`upstream/main` 上尚未 release 的 commit 算进这一发）。

```sh
git fetch upstream --prune
git fetch upstream tag v0.4.32

# 1. fork commit 已在 main 上
git log --oneline upstream/main..main

# 2. 从上游 tag 只带 fork commit
git switch -C release-v42.0.0 v0.4.32
git cherry-pick $(git log --reverse --format=%H upstream/main..main)

# 3. 推 main（日常）和我们的 tag（触发 Release workflow）
git switch main
git push origin main
git push origin "$(git rev-parse release-v42.0.0)":refs/tags/v42.0.0
```

不要 `git push origin v42.0.0` 若本地还没有这个名字；用
`git push origin <sha>:refs/tags/v42.0.0`。更不要推上游的 `v0.4.32`。

看 <https://github.com/xesrevinu/multica/actions>。verify（测试 + govulncheck）
过了才出镜像、CLI、chart。`v42.0.0` 无连字符，会打 `latest` 镜像。

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

rebase 到新的上游发行之后，下一发升 `42.x.0`（minor），对照表写新的上游 tag。

## 家里怎么吃（nixos）

配置在 `~/.config/nixos`，不跟本仓库一起提交：

| 文件 | 作用 |
| --- | --- |
| `services/k8s/multica/values.yaml` | `ghcr.io/xesrevinu/multica-{backend,web}` + `v42.x.x` |
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
VER=v42.0.0
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
- **origin 只打 `v42.*`。** 不要推上游同名 tag。
- **Helm 不要默默用本地 chart。** 未提交改动会进集群。
- **GPG 签名。** rebase 用 `git -c commit.gpgsign=false`。
- **不要把 fork 推到 `multica-ai/multica`。** `origin` 是 `xesrevinu/multica`。
