# 发布约定

- 项目: Multica (fork)
- 仓库: wujie-agent/multica

## 组件

### backend
- Jenkins job: multica-backend-prod-pipeline
- 部署目标: k3s

### frontend
- Jenkins job: multica-frontend-prod-pipeline
- 部署目标: k3s

### cli
- Jenkins job: Multica-CLI
- 部署目标: obs

## 发布流程

1. 同步 main：`git fetch origin main && git pull --ff-only origin main`
2. 打 tag：用 `git describe --tags --long` 生成版本号，执行 `git tag -a` 后 push tag 到 `origin`
3. 触发 backend pipeline：触发 `multica-backend-prod-pipeline`，轮询 Jenkins 构建结果，等待 `SUCCESS`
4. 触发 frontend pipeline：触发 `multica-frontend-prod-pipeline`，轮询 Jenkins 构建结果，等待 `SUCCESS`
5. 触发 CLI 构建：触发 `Multica-CLI`，轮询 Jenkins 构建结果，等待 `SUCCESS`
6. Gitee Release：汇总变更，按本文件的 Release 规范创建或更新 Release （注意测试环境发布时不需要Release过程）

## Release 规范

- 区分「官方上游变更」和「Fork 独有变更」
- 每条变更挂 PR 链接和 Issue 链接
- previous release 取 Gitee Releases API 中最近一个有 body 的 release
- Release 版本号对应发布流程第 2 步创建的 tag
- Release 在 backend、frontend、cli 三个组件都发布成功后再创建或更新
