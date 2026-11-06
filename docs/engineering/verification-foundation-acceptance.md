# 自动化验证基础能力：实现与验收

本清单对应[接口契约](verification-foundation-contracts.md)。代码、局部测试和干净 Mac 整机验收分别认定；没有原生执行证据时不宣布完整交付通过。

## 实现对应

| 要求 | 实现位置 | 已形成的约束 |
| --- | --- | --- |
| 只提供操作和取数 | `packages/verification/src/mcp`、`state`、`page` | 无检查器、验证计划、结论或报告；状态、事件和页面读取原样返回来源数据 |
| 接口互不依赖，异步为 start/query | `src/mcp/server.ts`、`src/host/jobs.ts` | 18 个工具各自独立；`env.start`、`env.stop`、`data.snapshot`、`data.restore` 经任务账本，其余同步返回 |
| 设备自检与环境准备 | `setup/inspect.ts`、`device-inspect.ts`、`prerequisites.ts`、`environments.ts` | 工具、磁盘、图形会话、端口/数据库目标、构建前提、现存 Desktop；登录前任务为 awaitingUser |
| 固定版本与构建复用 | `setup/source-identity.ts`、`build-cache.ts` | SHA/工作变更、锁文件/工具/构建环境缓存键、同键互斥、原子工件清单与校验；启动前拒绝源码漂移 |
| 设备互斥与中断 | `setup/device-lease.ts`、`environments.ts`、`read-only-web.ts` | 跨项目设备锁；宿主重启后环境和任务标为 interrupted；清理未确认时保留占用 |
| 真实数据与模板 | `data/service.ts`、`workspace.ts`、`manifest.ts`，Go `verificationworkspace`、`handler/verification_dev.go` | 选定会话/项目内容导出、关系和附件清单、按命名空间幂等导入、ID 映射；两个版本化模板 |
| 事件读取 | Go `daemon/localhost/verification_dev.go`、Desktop `main/verification-dev` | 按 cursor 分页，原样返回 generation、cursor、保留范围；无推送通道 |
| 页面操作 | `page/service.ts`、`read-only.ts` | 定位器或坐标，多匹配要求 index；只读页面拦截写请求、WebSocket 和外部网络 |
| dev/生产边界 | Desktop 构建 alias、Go build tags、`scripts/verification/check-production.mjs` | 生产排除 main/preload/renderer 与 Go 验证实现；环境变量不能重新打开新增路由 |

## 实际执行记录

| 检查 | 结果 |
| --- | --- |
| 验证包 TypeScript、lint | 通过 |
| `test:manual` | 22 项通过 |
| `test:integration` | 8 项中 7 项通过，覆盖真实 Go service/SQLite/Local API、受控 provider 和 Chromium 页面操作。会话历史复制用例失败：SQLite 导出器只识别 schema 4、5，而会话库已升级到 7；未修改的 origin/main 上同样失败 |
| Go `verificationdev` 测试（`internal/daemon/localhost`） | 通过 |
| Desktop `typecheck:node` | 通过 |
| `test:desktop` | 未运行；需要专用干净 Mac |

## 正式交付仍需的实测

在专用干净 Mac 运行[准备流程](../../packages/verification/README.md#准备一台-mac)，再执行：

```sh
pnpm --filter @multica/verification test:desktop
```

完成测试应用的正常登录与工作区选择，以该次 `desktop-acceptance.json` 和截图判定实际状态。还需记录冷启动安装与构建、原生权限和完整 GUI 链路。
