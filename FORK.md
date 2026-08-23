# Fork 维护指南

`xesrevinu/multica` 是 `multica-ai/multica` 的自用 fork。源码跟上游 rebase，
发行号走 **42.x**，不跟上游 0.x / 未来 1.x 抢名字。

本仓库只负责：rebase、叠 patch、打 `v42.*` tag、让 Release workflow 出 CLI /
容器镜像 / Helm chart。怎么消费这些制品不在这里记录。

以后若要给上游提 PR，另开一个干净 fork，从本仓库 cherry-pick 可贡献的
commit。不要从这里直接开到 `multica-ai/multica`。

## Remotes

| Remote | 用途 |
| --- | --- |
| `upstream` | `multica-ai/multica` |
| `origin` | `xesrevinu/multica` |

`main` 跟踪 `upstream/main`，我们的 commit 永远 rebase 上去，不 merge 上游。

```sh
git log --oneline upstream/main..main    # 当前 fork commit；空 = 没有独有改动
git fetch upstream --prune
git fetch upstream tag v0.4.32           # 需要对齐某个上游发行时
```

origin 只打 `v42.*`。不要推上游同名 tag。

## 版本

| 怎么升 | 何时 |
| --- | --- |
| `42.0.x` | 同一上游基线上的修订 |
| `42.x.0` | rebase 到上游新发行 |
| `43.0.0` | 协议或数据迁移不兼容 |

已发布的 tag 不要 force 改 SHA。要再发就升号。

### 对照表

| 发行 | 上游基线 | 上游 commit | 多了什么 |
| --- | --- | --- | --- |
| 42.0.0 | v0.4.32 | ad64e0f800 | fork 发版 CI（CLI / Helm 发到本仓库） |

发一版加一行。Release body 第一行写 `Based on upstream v0.4.32`。

## 产物

| 产物 | 地址 |
| --- | --- |
| backend | `ghcr.io/xesrevinu/multica-backend:<tag>` |
| web | `ghcr.io/xesrevinu/multica-web:<tag>` |
| Helm | `oci://ghcr.io/xesrevinu/charts/multica`（chart version = tag 去 `v`） |
| CLI | `https://github.com/xesrevinu/multica/releases/download/<tag>/multica-cli-<ver>-<os>-<arch>.tar.gz` |

Electron Desktop 和上游 Homebrew tap 不发。

`v42.0.0` 无连字符，workflow 会打 `latest` 镜像。`GITHUB_TOKEN` 改不了 GHCR
可见性；package 若仍是 private，在 GitHub Packages UI 设成 Public。

## 发一版

功能堆在 `main`。从**上游发行 tag** 长出我们的号，不要把 `upstream/main`
上尚未 release 的 commit 算进来。

```sh
git fetch upstream --prune
git fetch upstream tag v0.4.32

git log --oneline upstream/main..main

git switch -C release-v42.0.1 v0.4.32
git cherry-pick $(git log --reverse --format=%H upstream/main..main)

git switch main
git push origin main
git push origin "$(git rev-parse release-v42.0.1)":refs/tags/v42.0.1
```

用 `git push origin <sha>:refs/tags/v42.0.1`，不要在本地 tag 仍指向上游 SHA
时 `git push origin v0.4.32`。

<https://github.com/xesrevinu/multica/actions> — verify 过了才出镜像、CLI、chart。

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

rebase 到新的上游发行后，下一发升 `42.x.0`，对照表写新的上游 tag。

## 本机构建（CI 挂了才用）

```sh
VER=v42.0.1
COMMIT=$(git rev-parse --short HEAD)

docker build --platform linux/arm64 \
  --build-arg VERSION="$VER" --build-arg COMMIT="$COMMIT" \
  -t "ghcr.io/xesrevinu/multica-backend:$VER" -f Dockerfile .

docker build --platform linux/arm64 \
  --build-arg NEXT_PUBLIC_APP_VERSION="$VER" \
  -t "ghcr.io/xesrevinu/multica-web:$VER" -f Dockerfile.web .

cd server
go build -ldflags "-s -w -X main.version=${VER#v} -X main.commit=$COMMIT" \
  -o bin/multica ./cmd/multica
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.version=${VER#v} -X main.commit=$COMMIT" \
  -o bin/multica-linux-amd64 ./cmd/multica
```

## 准备改什么

有 patch 再写到这里。候选（尚未落地）：

- OpenCode V2：`server/pkg/agent/opencode.go`、`opencode_mcp.go`、
  `server/internal/daemon/agents_probe.go`

拆 commit：schema / 协议 → backend → CLI。能给上游的单独成 commit，方便以后
cherry-pick 到干净 fork。

## 陷阱

- 改过 daemon API 就必须同一发行里同时出 backend 和 CLI。
- origin 只打 `v42.*`。
- rebase 用 `git -c commit.gpgsign=false`。
- 不要 push 到 `multica-ai/multica`。
