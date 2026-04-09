# TODO

- [x] 阶段一：完成 VPS + OpenResty 部署方案的现状分析
- [x] 阶段二：输出仅包含 workspace + 后端 的部署功能点与目标拓扑，等待用户确认
- [x] 阶段三：输出以打包方式和必须配置文件为核心的风险、关键决策与实施边界，等待用户确认
- [x] 阶段四：在用户显式确认 Spec 后开始实现部署相关改动
- [x] 阶段五：执行部署验证并记录可验证证据

## Review

- 已完成现状分析，用户将范围收敛为 workspace + 后端，后续方案不再包含 marketing。
- 已确认推荐拓扑：域名使用 multica.ali.chenjiaming.org，由 OpenResty 统一处理请求，workspace 走静态构建产物，后端走 Go server。
- 用户进一步明确：OpenResty 配置由用户自行处理，本次方案与后续实现应重点覆盖打包产物、运行目录和必须配置文件。
- 已新增发布构建脚本、发布目录组装脚本和生产环境模板文件。
- 已执行 `bash scripts/build-release.sh`，确认 workspace、server、migrate 三类产物可以成功构建。
- 已执行 `bash scripts/package-release.sh` 和 `SKIP_BUILD=1 make release-package RELEASE_OUTPUT_DIR=dist/release-make`，确认发布目录可成功组装。
- 用户后续将采用 VPS 直接 clone 源码并本机编译的路径，发布包组装脚本保留为可选方案，主路径切换为源码部署。

## Current Task

- [ ] 阶段一：确认 workspace 构建产物位置与 server 前端分发入口
- [ ] 阶段二：将 workspace 静态产物改为通过 `go:embed` 内嵌到 server
- [ ] 阶段三：验证 server 在存在内嵌产物时可正常构建
